package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/aiclient"
)

// RHGatewayGenerator 通过 RunningHub 中转网关出图 / 出片
// 流程：/task/add 拿 taskId → 每 5 秒 /task/status 轮询 → SUCCESS 后取 outputs
type RHGatewayGenerator struct {
	cfg    *model.ModelConfig
	gw     *aiclient.RHGateway
	params map[string]interface{}
	// OnTaskID 拿到上游 taskId 时回调，用于把它落库。
	// 为什么需要：进程重启后 Recover() 会把 running 任务重新跑一遍，
	// 不知道上游 ID 就只能重新 /task/add，白烧一次额度。
	OnTaskID func(externalID string)
	// existingTaskID 非空时走续查，不重新提交（重启恢复场景）
	existingTaskID string
	// LocalRoot 本地静态目录（对应 /static）。参考图是本机文件时要先上传给网关，
	// 否则远端拿到的只是一个它访问不到的路径。
	LocalRoot string
}

func NewRHGatewayGenerator(cfg *model.ModelConfig) *RHGatewayGenerator {
	g := &RHGatewayGenerator{
		cfg:    cfg,
		gw:     aiclient.NewRHGateway(cfg.BaseURL, cfg.APIKey),
		params: map[string]interface{}{},
	}
	if strings.TrimSpace(cfg.Params) != "" {
		_ = json.Unmarshal([]byte(cfg.Params), &g.params)
	}
	return g
}

// 取参数：字符串
func (g *RHGatewayGenerator) str(key, def string) string {
	if v, ok := g.params[key].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// 取参数：整数
func (g *RHGatewayGenerator) num(key string, def int) int {
	if v, ok := g.params[key].(float64); ok && v > 0 {
		return int(v)
	}
	return def
}

// Workflow 当前配置使用的工作流
func (g *RHGatewayGenerator) Workflow() *RHWorkflowSpec {
	name := g.str("workflow", "")
	if name != "" {
		if spec := RHWorkflowByName(name); spec != nil {
			return spec
		}
	}
	return nil
}

// GenerateImage 出图，refs 为参考图 URL 列表（可空）
func (g *RHGatewayGenerator) GenerateImage(ctx context.Context, prompt string, refs []string) (string, string, error) {
	if g.cfg.APIKey == "" {
		return "", "", fmt.Errorf("RH 中转未配置 Token")
	}

	spec := g.Workflow()
	if spec == nil {
		spec = RHDefaultWorkflow("image")
	}
	if spec == nil || spec.Kind != "image" {
		return "", "", fmt.Errorf("当前工作流「%s」不是图像工作流，请到管理端重新选择", spec.Name)
	}

	// 参考图是本机文件（/static/results/xxx.png）时必须先上传换 download_url：
	// 网关跑在远端，读不到我们的磁盘路径，会直接报
	// 「Invalid image file: /static/results/xxx.png」。
	// 视频那条链路一直有这步，出图这条漏了 —— 之前资产图用的是公网 URL 所以没暴露，
	// 首帧图传的是本地图，一跑就炸。
	refs, err := g.resolveRefs(ctx, refs)
	if err != nil {
		return "", "", err
	}
	inputs, err := BuildRHInputs(spec, prompt, g.str("aspectRatio", "9:16"), refs, "", nil, 0)
	if err != nil {
		return "", "", err
	}
	return g.runResumable(ctx, spec, inputs, g.existingTaskID)
}

// WithExistingTaskID 指定已有上游任务 ID，走「续查」而不是重新提交
func (g *RHGatewayGenerator) WithExistingTaskID(id string) *RHGatewayGenerator {
	g.existingTaskID = strings.TrimSpace(id)
	return g
}

// WithLocalRoot 指定本地静态目录（/static 映射的磁盘目录）
func (g *RHGatewayGenerator) WithLocalRoot(root string) *RHGatewayGenerator {
	g.LocalRoot = strings.TrimRight(root, "/")
	return g
}

/**
 * resolveRefs 把本地静态资源换成网关口可访问的 download_url
 *
 * 为什么必须做：网关跑在远端，我们库里存的是 /static/results/xxx.png，
 * 它既不是绝对 URL 也下载不到；直接丢过去上游会报输入非法。
 * 走一次 /upload 换成 1 天有效的 download_url 才能真正被读到。
 */
func (g *RHGatewayGenerator) resolveRefs(ctx context.Context, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		// 已经是公网地址，直接用
		if strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") {
			out = append(out, r)
			continue
		}
		// 本站静态路径 → 上传
		if strings.HasPrefix(r, "/static/") {
			local, err := g.localPathOf(r)
			if err != nil {
				return nil, err
			}
			up, err := g.gw.Upload(ctx, local)
			if err != nil {
				return nil, fmt.Errorf("参考图上传网关失败（%s）: %w", r, err)
			}
			out = append(out, up)
			continue
		}
		// 其余原样（可能是网关侧文件名）
		out = append(out, r)
	}
	return out, nil
}

// localPathOf 把 /static/xxx 映射成磁盘路径
func (g *RHGatewayGenerator) localPathOf(ref string) (string, error) {
	if g.LocalRoot == "" {
		return "", fmt.Errorf("未配置本地静态目录，无法上传参考图 %s", ref)
	}
	rel := strings.TrimPrefix(ref, "/static/")
	// 映射关系：
	//   /static/results/xxx  → <root>/xxx            （产物根目录）
	//   /static/uploads/xxx  → <root>/uploads/xxx    （用户上传，保留 uploads/ 前缀）
	if strings.HasPrefix(rel, "results/") {
		rel = strings.TrimPrefix(rel, "results/")
	}
	return filepath.Join(g.LocalRoot, filepath.FromSlash(rel)), nil
}

// GenerateVideo 出片，firstFrame 作为首帧参考图，srcVideo 作为驱动视频
func (g *RHGatewayGenerator) GenerateVideo(ctx context.Context, prompt, firstFrame, srcVideo string) (string, string, error) {
	var refs []string
	if strings.TrimSpace(firstFrame) != "" {
		refs = []string{firstFrame}
	}
	return g.GenerateVideoWithRefs(ctx, prompt, refs, srcVideo, nil)
}

// GenerateVideoWithRefs 出片，refs 依次填入图位（第 1 张进 51 号首帧位）
//
// 为什么需要多张：分镜提示词里 @ 了角色 / 场景 / 道具，
// 只传真第一张的话，模型对其他资产完全没有视觉参照，
// 只能靠文字描述现编，形象一致性会崩。
func (g *RHGatewayGenerator) GenerateVideoWithRefs(ctx context.Context, prompt string, refs []string, srcVideo string, audios []string) (string, string, error) {
	if g.cfg.APIKey == "" {
		return "", "", fmt.Errorf("RH 中转未配置 Token")
	}

	spec := g.Workflow()
	if spec == nil {
		spec = RHDefaultWorkflow("video")
	}
	if spec == nil || spec.Kind != "video" {
		return "", "", fmt.Errorf("当前工作流「%s」不是视频工作流，请到管理端重新选择", spec.Name)
	}

	refs, err := g.resolveRefs(ctx, refs)
	if err != nil {
		return "", "", err
	}
	video, err := g.resolveVideoRef(ctx, srcVideo)
	if err != nil {
		return "", "", err
	}
	// 音频也要换成网关能读的地址（本地 /static/uploads 它读不到）
	if len(audios) > 0 {
		audios, err = g.resolveRefs(ctx, audios)
		if err != nil {
			return "", "", fmt.Errorf("音色样本上传失败: %w", err)
		}
	}
	inputs, err := BuildRHInputs(spec, prompt, g.str("aspectRatio", ""), refs, video, audios, g.num("duration", 0))
	if err != nil {
		return "", "", err
	}
	// 出片后核对「提示词 @ 了几个资产 vs 上游收到几张图」就看这行日志
	log.Printf("提交「%s」：%d 张参考图 → 图位 %v", spec.Label, len(refs), imageNodeKeys(inputs, spec))
	return g.runResumable(ctx, spec, inputs, g.existingTaskID)
}

// imageNodeKeys 列出本次真正填了图的节点，便于日志核对
func imageNodeKeys(inputs map[string]interface{}, spec *RHWorkflowSpec) []string {
	out := make([]string, 0, len(spec.ImageNodes))
	for _, n := range spec.ImageNodes {
		if v, ok := inputs[n]; ok && v != "" {
			out = append(out, n)
		}
	}
	return out
}

// addTaskWithRetry 提交任务，遇到「队列满」(-4) 退避重试
// 批量出图会一瞬间把所有任务都提交上去，网关账号池可能瞬时排队满，
// 这种情况不该让任务直接失败，等几秒再提即可。
func (g *RHGatewayGenerator) addTaskWithRetry(ctx context.Context, spec *RHWorkflowSpec, inputs map[string]interface{}, queueTimeout int) (*aiclient.AddTaskResp, error) {
	delays := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second}

	for attempt := 0; ; attempt++ {
		resp, err := g.gw.AddTask(ctx, spec.Name, inputs, queueTimeout)
		if err == nil {
			return resp, nil
		}

		var ge *aiclient.GatewayError
		if !errors.As(err, &ge) || ge.Code != -4 || attempt >= len(delays) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delays[attempt]):
		}
	}
}

// run 提交任务并轮询到出结果
func (g *RHGatewayGenerator) run(ctx context.Context, spec *RHWorkflowSpec, inputs map[string]interface{}) (string, string, error) {
	return g.runResumable(ctx, spec, inputs, "")
}

// runResumable 提交（或续查已有）任务并轮询到出结果
//
// existingID 非空时不再 /task/add，而是直接拿它去 /task/status 续查。
// 用途：进程重启后恢复任务，避免同一笔上游任务被重复提交、白烧额度。
func (g *RHGatewayGenerator) runResumable(ctx context.Context, spec *RHWorkflowSpec, inputs map[string]interface{}, existingID string) (string, string, error) {
	// 排队上限默认 3 分钟：上游公共队列堵住时，用户干等 15 分钟才看到失败体验极差。
	// 失败会自动退积分，用户可立即重试；确需长排队的模型可在管理端把 queueTimeoutMs 调大。
	queueTimeout := g.num("queueTimeoutMs", 180000)

	var upstreamID string
	if existingID != "" {
		upstreamID = existingID
		log.Printf("续查已有上游任务 %s（不重新提交）", upstreamID)
	} else {
		added, err := g.addTaskWithRetry(ctx, spec, inputs, queueTimeout)
		if err != nil {
			return "", "", fmt.Errorf("提交「%s」任务失败: %w", spec.Label, err)
		}
		upstreamID = added.TaskID
		// 立刻把上游 taskId 交给调用方落库：进程若在此后被杀，
		// 重启时能凭它续查结果，而不是重新提交一遍。
		if g.OnTaskID != nil && upstreamID != "" {
			g.OnTaskID(upstreamID)
		}
	}

	// 排队等待 + 上游运行的兜底上限
	runCap := 12 * time.Minute
	if spec.Kind == "video" {
		runCap = 35 * time.Minute
	}
	deadline := time.Now().Add(time.Duration(queueTimeout)*time.Millisecond + runCap)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastStatus := "QUEUED"
	// 连续查询失败计数：单次抖动可以容忍，但一直失败必须暴露出来。
	// 之前这里是无声 continue，导致「JSON 解析失败」这种持续性错误被完全掩盖，
	// 任务会一直显示 QUEUED 直到超时，排查时完全看不出原因。
	statusErrs := 0
	var lastErr error

	for {
		select {
		case <-ctx.Done():
			msg := fmt.Sprintf("生成被取消（任务 %s，最后状态 %s）", upstreamID, lastStatus)
			if lastErr != nil && statusErrs > 0 {
				msg += fmt.Sprintf("；期间状态查询连续失败 %d 次，最后一次错误：%v", statusErrs, lastErr)
			}
			return "", "", errors.New(msg)
		case <-ticker.C:
			if time.Now().After(deadline) {
				if strings.EqualFold(lastStatus, "QUEUED") {
					extra := ""
					if statusErrs > 0 {
						extra = fmt.Sprintf("；期间状态查询连续失败 %d 次，最后一次错误：%v", statusErrs, lastErr)
					}
					return "", "", fmt.Errorf(
						"上游排队超时（任务 %s，等待 %d 分钟仍未开始执行）。可能是网关账号池繁忙，稍后重试或换一个模型；本次积分已退回%s",
						upstreamID, int(time.Duration(queueTimeout)/time.Minute), extra)
				}
				return "", "", fmt.Errorf("等待超时（任务 %s，最后状态 %s）", upstreamID, lastStatus)
			}

			st, err := g.gw.Status(ctx, upstreamID)
			if err != nil {
				statusErrs++
				lastErr = err
				log.Printf("查询上游状态失败（第 %d 次）任务 %s: %v", statusErrs, upstreamID, err)
				// 连续失败 6 次（约 30 秒）说明不是网络抖动，而是接口/解析层面坏了，
				// 继续等下去只会耗到超时，直接失败并说明原因更好排查
				if statusErrs >= 6 {
					return "", "", fmt.Errorf("无法获取任务状态（连续 %d 次失败，任务 %s）：%w",
						statusErrs, upstreamID, err)
				}
				continue
			}
			statusErrs = 0
			lastErr = nil
			lastStatus = st.Status

			switch strings.ToUpper(st.Status) {
			case "SUCCESS":
				return g.pickResult(ctx, spec, upstreamID, st)
			case "FAILED":
				reason := st.ErrorMessage
				if reason == "" {
					reason = string(st.FailedReason)
				}
				if reason == "" {
					reason = "上游未返回失败原因"
				}
				// 805 是 RunningHub 工作流「跑到一半失败」，多为节点内部错误：
				// 缺模型 / 显存不足 / 入参类型不符。这类问题改我们这边没用，要查工作流本身。
				if string(st.ErrorCode) == "805" || reason == "工作流运行失败" {
					return "", "", fmt.Errorf(
						"「%s」工作流执行失败（errorCode=%s）：上游节点内部报错，与提示词无关。"+
							"常见原因是该工作流的模型/显存/入参类型不满足，建议在 RunningHub 后台单独跑一次该工作流定位，或换一个可用的工作流",
						spec.Label, displayOr(string(st.ErrorCode), "805"))
				}
				return "", "", fmt.Errorf("「%s」执行失败（errorCode=%s）: %s",
					spec.Label, displayOr(string(st.ErrorCode), "-"), truncateStr(reason, 200))
			case "QUEUED", "RUNNING":
				continue
			case "":
				// 空状态 + 错误码 = 上游已经把这条任务丢了：
				// 在 RunningHub 后台手动取消、任务被清理、或超过保留期都会这样
				// （errorCode 1004「Task not found」）。
				//
				// 以前这里和 QUEUED/RUNNING 一样 continue，于是任务永远卡在
				// 「生成中」——用户明明在网关取消了，本地还在傻等。
				if code := string(st.ErrorCode); code != "" {
					reason := st.ErrorMessage
					if reason == "" {
						reason = "上游未返回原因"
					}
					return "", "", fmt.Errorf("「%s」任务已在上游失效（errorCode=%s）：%s",
						spec.Label, code, truncateStr(reason, 200))
				}
				// 空状态但没错误码：偶发的状态抖动，继续等下一轮
				continue
			default:
				continue
			}
		}
	}
}

// pickResult 从 SUCCESS 的任务里挑出对应类型的产物
func (g *RHGatewayGenerator) pickResult(ctx context.Context, spec *RHWorkflowSpec, taskID string, st *aiclient.TaskStatus) (string, string, error) {
	results := st.Results
	if len(results) == 0 {
		// status 里没带产物时再查一次 outputs
		if outs, err := g.gw.Outputs(ctx, taskID); err == nil {
			results = outs
		}
	}
	if len(results) == 0 {
		return "", "", fmt.Errorf("任务成功但未返回产物（taskId %s）", taskID)
	}

	wantVideo := spec.Kind == "video"
	picked := ""
	for _, r := range results {
		isVideo := strings.EqualFold(r.OutputType, "mp4") || strings.Contains(strings.ToLower(r.URL), ".mp4")
		if isVideo == wantVideo {
			picked = r.URL
			break
		}
	}
	if picked == "" {
		picked = results[0].URL
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"provider": "rh-proxy",
		"workflow": spec.Name,
		"taskId":   taskID,
		"status":   st.Status,
		"usage":    st.Usage,
		"results":  results,
	})
	return picked, string(raw), nil
}

// resolveVideoRef 输入视频同样要能上传（本地 mp4 网关也读不到）
func (g *RHGatewayGenerator) resolveVideoRef(ctx context.Context, src string) (string, error) {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return src, nil
	}
	if !strings.HasPrefix(src, "/static/") {
		return src, nil
	}
	local, err := g.localPathOf(src)
	if err != nil {
		return "", err
	}
	up, err := g.gw.Upload(ctx, local)
	if err != nil {
		return "", fmt.Errorf("输入视频上传网关失败: %w", err)
	}
	return up, nil
}

// displayOr 空值兜底
func displayOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
