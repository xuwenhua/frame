package service

import (
	"fmt"
	"strings"
)

// RHWorkflowSpec 中转网关的一个工作流定义（节点 ID 来自网关文档）
type RHWorkflowSpec struct {
	Name            string   `json:"name"`  // 传给 /task/add 的 workflow
	Label           string   `json:"label"` // 中文名
	Kind            string   `json:"kind"`  // image / video
	Desc            string   `json:"desc"`
	PromptNode      string   `json:"promptNode"`
	AspectNode      string   `json:"aspectNode"`    // 画幅比例节点，空表示不支持
	ImageNodes      []string `json:"imageNodes"`    // 参考图节点（按需填充，其余置 nil）
	AudioNodes      []string `json:"audioNodes"`    // 音频位节点（用不上也要显式传 nil，缺了上游会报 803）
	RequiredImg     int      `json:"requiredImg"`   // 必填参考图数量
	VideoNode       string   `json:"videoNode"`     // 输入视频节点
	RequiredVideo   bool     `json:"requiredVideo"` // 输入视频是否必填
	DurationNode    string   `json:"durationNode"`
	AspectOptions   []string `json:"aspectOptions"` // 画幅节点的合法枚举（原样传给上游）
	DefaultAspect   string   `json:"defaultAspect"`
	DefaultDuration int      `json:"defaultDuration"`
}

// RHComfyAspectOptions ComfyUI ResolutionSelector 节点的合法枚举
//
// 注意：必须传这种「比例 (描述)」的完整写法，传裸 "16:9" 会让节点抛
// KeyError: '16:9'，表现为上游 errorCode=805「工作流运行失败」。
// 而且横屏的写法是 "16:9 (Widescreen)"，不是 "16:9 (Landscape Widescreen)"。
var RHComfyAspectOptions = []string{
	"1:1 (Square)",
	"2:3 (Portrait Photo)",
	"3:2 (Photo)",
	"3:4 (Portrait Standard)",
	"4:3 (Standard)",
	"9:16 (Portrait Widescreen)",
	"16:9 (Widescreen)",
	"21:9 (Ultrawide)",
}

// NormalizeAspect 把前端传来的简写比例（16:9）映射成节点要求的完整枚举
//
// 认不出就退回工作流默认值，避免把非法枚举丢给上游。
func NormalizeAspect(spec *RHWorkflowSpec, want string) string {
	want = strings.TrimSpace(want)
	if spec == nil || len(spec.AspectOptions) == 0 {
		return want
	}
	// 已经是完整写法，直接校验通过
	for _, o := range spec.AspectOptions {
		if o == want {
			return want
		}
	}
	// 简写 → 找前缀匹配的那一项
	if want != "" {
		for _, o := range spec.AspectOptions {
			if strings.HasPrefix(o, want+" ") {
				return o
			}
		}
	}
	// 兜底：默认比例，再不行取第一项
	if spec.DefaultAspect != "" {
		for _, o := range spec.AspectOptions {
			if strings.HasPrefix(o, spec.DefaultAspect+" ") || o == spec.DefaultAspect {
				return o
			}
		}
	}
	return spec.AspectOptions[0]
}

// RHWorkflows 网关支持的工作流清单（与 /workflows 一致，内置一份便于离线配置）
var RHWorkflows = []RHWorkflowSpec{
	{
		Name: "qwen-text2img", Label: "Qwen 文生图", Kind: "image",
		Desc:       "纯文生图，最快最省，适合资产参考图",
		PromptNode: "497", AspectNode: "13", AspectOptions: RHComfyAspectOptions,
		DefaultAspect: "9:16",
	},
	{
		// 纯文生图：只有提示词（25）和画幅（19），没有任何图片输入位。
		Name: "moss-image", Label: "Moss Image", Kind: "image",
		Desc:       "纯文生图，提示词 25、画幅 19（1:1/2:3/3:2/3:4/4:3/9:16/16:9/21:9）",
		PromptNode: "25", AspectNode: "19", AspectOptions: RHComfyAspectOptions,
		DefaultAspect: "9:16",
	},
	{
		Name: "qwen-image-2.1-pe", Label: "Qwen 图像编辑 2.1", Kind: "image",
		Desc:       "图生图/换装换背景；默认必传 1 张参考图（476 必填，477 及 490-496 可选）",
		PromptNode: "497", AspectNode: "13", AspectOptions: RHComfyAspectOptions,
		ImageNodes:  []string{"476", "477", "490", "491", "492", "493", "494", "495", "496"},
		RequiredImg: 1, DefaultAspect: "9:16",
	},
	{
		Name: "krea2-text2img", Label: "Krea2 文生图", Kind: "image",
		Desc:       "支持中文多行场景描述，质感偏写实",
		PromptNode: "55", AspectNode: "74", AspectOptions: RHComfyAspectOptions,
		DefaultAspect: "9:16",
	},
	{
		Name: "minimax-h3-final", Label: "MiniMax H3 说话视频", Kind: "video",
		Desc:       "图/音/视频参考生成说话视频，首帧图走 51 号位",
		PromptNode: "263", AspectNode: "252", AspectOptions: RHComfyAspectOptions,
		ImageNodes: []string{"51", "49", "50", "43", "19", "23", "199", "200", "201"},
		// 音频位：用不上也要显式传 None，缺了上游会报 803 JsonNull
		AudioNodes:  []string{"48", "14", "15"},
		RequiredImg: 1,
		// 259 是网关里的 value 节点（数值位）。不传时工作流用自己的默认值，
		// 实测只出 1.3 秒的片子；补上它才能真正控制时长。
		DurationNode:  "259",
		DefaultAspect: "9:16", DefaultDuration: 10,
	},
	{
		// 和 minimax-h3-final 是同一套节点（aiAppId 不同：2116465280192204802），
		// 只去掉了一个提示词优化节点 —— 描述词原样进 H3，不被二次改写。
		// 我们用 Ref2VA 六段式写描述词，优化节点反而会把结构揉散，
		// 所以这条才是「按自己写的 prompt 出片」的版本。
		Name: "minimax-h3-final-noopt", Label: "MiniMax H3 说话视频（无提示词优化）", Kind: "video",
		Desc:       "与 minimax-h3-final 相同，去掉提示词优化节点，描述词原样送进 H3",
		PromptNode: "263", AspectNode: "252", AspectOptions: RHComfyAspectOptions,
		ImageNodes:    []string{"51", "49", "50", "43", "19", "23", "199", "200", "201"},
		AudioNodes:    []string{"48", "14", "15"},
		RequiredImg:   1,
		DurationNode:  "259",
		DefaultAspect: "9:16", DefaultDuration: 10,
	},
	{
		// 7 图 3 音频 + Ref2VA 结构化提示词 + 时长 + 画幅版（aiAppId 2116514530440753153）。
		//
		// 和 minimax-h3-final 那套节点完全不是一回事，不能混用：
		//   - 图位 7 个：139 / 256 / 257 / 258 / 259 / 260 / 270
		//   - 音频位 3 个：267 / 268 / 269
		//   - 提示词走 265（注意 final 是 263，这里错一位就整段丢失）
		//   - 时长走 132（秒，工作流自身默认 3）
		//   - 画幅走 115，COMBO 类型，选项与 RHComfyAspectOptions 逐项一致
		//     （1:1 / 2:3 / 3:2 / 3:4 / 4:3 / 9:16 / 16:9 / 21:9）。
		//
		//     注意默认值有两个说法：115 节点自身的 default 是 "1:1 (Square)"，
		//     而网关文档 desc 写的是「默认 9:16」。这里 DefaultAspect 取 9:16 ——
		//     本项目是短剧平台，前端工具栏只会传 9:16 / 16:9，方图没有意义；
		//     而且 BuildRHInputs 只要 AspectNode 非空就一定会显式填值，
		//     节点自身的 1:1 默认永远不会生效，不受这个分歧影响。
		//
		// ⚠️ 259 在这条工作流里是「图位」，不是「时长节点」：
		// final 那套的 259 是 value(时长)，这里 259 是第 5 张参考图。
		// 照着 final 的 spec 复制粘贴，时长数字会顶掉第 5 张图且不报错。
		//
		// 画幅一律走 NormalizeAspect 转成「比例 (描述)」的完整写法：
		// 115 节点的默认值就是这个格式，说明节点认完整枚举；
		// 传裸 "9:16" 在同类 ResolutionSelector 节点上会抛 KeyError → 上游 805。
		//
		// 提示词建议按 Ref2VA 六段式写（subject_definitions / 场次 / summary /
		// retention_analysis / detailed_description），这是这条工作流的设计用法。
		Name: "minimax-h3-i2v-7img", Label: "MiniMax H3 图生视频（7 图）", Kind: "video",
		Desc:       "7 图 + 3 音频 + 结构化提示词；时长走 132，画幅走 115",
		PromptNode: "265",
		ImageNodes: []string{"139", "256", "257", "258", "259", "260", "270"},
		AudioNodes: []string{"267", "268", "269"},
		// 参考视频位 272：喂 360 全景视频，模型能看清整个空间，
		// 不用再从单张场景图猜房间其余角度
		VideoNode:   "272",
		RequiredImg: 1,
		AspectNode:  "115", AspectOptions: RHComfyAspectOptions,
		DurationNode:  "132",
		DefaultAspect: "9:16", DefaultDuration: 3,
	},
	{
		// 多参工作流：9 图 + 3 音频 + **3 个参考视频位**，比 7img 多两路视频。
		// 时长 1–15 秒走 259，画幅走 252，提示词 263。
		Name: "minimax-h3-multi", Label: "MiniMax H3 多参生成", Kind: "video",
		Desc:        "9 图位 + 3 音频 + 3 参考视频；提示词 263、时长 259、画幅 252",
		PromptNode:  "263",
		ImageNodes:  []string{"51", "49", "50", "43", "19", "23", "199", "200", "201"},
		AudioNodes:  []string{"48", "14", "15"},
		VideoNode:   "27", // 主参考视频位；25/26 暂未使用
		RequiredImg: 1,
		AspectNode:  "252", AspectOptions: RHComfyAspectOptions,
		DurationNode:  "259",
		DefaultAspect: "9:16", DefaultDuration: 10,
	},
	{
		// 柱形全景图转 360 全景视频。只有一个输入图位（2），
		// 没有提示词 / 时长 / 画幅节点 —— 这三项留空，BuildRHInputs 就不会硬塞。
		Name: "panorama-to-360video", Label: "柱形全景图转 360 视频", Kind: "video",
		Desc:        "输入一张柱形全景图（equirectangular），输出 360 全景视频",
		ImageNodes:  []string{"2"},
		RequiredImg: 1,
	},
	{
		Name: "minimax-h3-remake", Label: "MiniMax H3 视频复刻", Kind: "video",
		Desc:       "视频翻拍/二创/换角色，需角色图 + 原视频",
		PromptNode: "79", VideoNode: "102", RequiredVideo: true,
		ImageNodes:  []string{"72", "112"},
		RequiredImg: 1, DurationNode: "73", DefaultDuration: 10,
	},
	{
		Name: "minimax-h3-digital-human", Label: "MiniMax H3 数字人", Kind: "video",
		Desc:       "换人换产品换背景、保留原语音，需角色图 + 产品图 + 原视频",
		PromptNode: "79", AspectNode: "78", VideoNode: "102", RequiredVideo: true, AspectOptions: RHComfyAspectOptions,
		ImageNodes:  []string{"72", "104", "119"},
		RequiredImg: 2, DurationNode: "73", DefaultAspect: "9:16", DefaultDuration: 10,
	},
	{
		Name: "video-to-depth", Label: "视频转深度", Kind: "video",
		Desc:      "把视频转成深度图序列，需原视频",
		VideoNode: "1", RequiredVideo: true,
	},
}

// RHWorkflowByName 按名称查找工作流
func RHWorkflowByName(name string) *RHWorkflowSpec {
	for i := range RHWorkflows {
		if RHWorkflows[i].Name == name {
			return &RHWorkflows[i]
		}
	}
	return nil
}

// RHDefaultWorkflow 按类型取默认工作流
func RHDefaultWorkflow(kind string) *RHWorkflowSpec {
	for i := range RHWorkflows {
		if RHWorkflows[i].Kind == kind {
			return &RHWorkflows[i]
		}
	}
	return nil
}

// BuildRHInputs 按网关拼装 inputs
//
// 注意：只传「有值」的节点。曾按文档传 null 占位，上游一律 803 JsonNull。
func BuildRHInputs(spec *RHWorkflowSpec, prompt, aspect string, images []string, video string, audios []string, duration int) (map[string]interface{}, error) {
	if spec == nil {
		return nil, fmt.Errorf("未指定工作流")
	}

	inputs := map[string]interface{}{}

	// 1. 图位：只填有值的，**绝不能传 null**
	//
	// 网关文档写的是「用不上的位保持占位值（图/音=None）」，
	// 但实测传 null 会让上游返回 errorCode=803 JsonNull；
	// 只传有值的节点才正常（2026-10-27 用 curl 对 124.222.61.207 实测确认）。
	if len(images) > len(spec.ImageNodes) && len(spec.ImageNodes) > 0 {
		return nil, fmt.Errorf("「%s」最多支持 %d 张参考图，当前传入 %d 张", spec.Label, len(spec.ImageNodes), len(images))
	}
	if len(images) < spec.RequiredImg {
		return nil, fmt.Errorf("「%s」需要 %d 张参考图，当前 %d 张", spec.Label, spec.RequiredImg, len(images))
	}
	for i, img := range images {
		if i >= len(spec.ImageNodes) {
			break
		}
		if strings.TrimSpace(img) == "" {
			continue
		}
		inputs[spec.ImageNodes[i]] = img
	}

	// 2. 视频位：必填的必须给，其余有值才填
	if spec.VideoNode != "" {
		if video == "" {
			if spec.RequiredVideo {
				return nil, fmt.Errorf("「%s」需要提供输入视频", spec.Label)
			}
		} else {
			inputs[spec.VideoNode] = video
		}
	}

	// 2.5 音频位：角色传了音色样本时才会用到（音色参考）
	for i, a := range audios {
		if i >= len(spec.AudioNodes) {
			break
		}
		if strings.TrimSpace(a) == "" {
			continue
		}
		inputs[spec.AudioNodes[i]] = a
	}

	// 3. 提示词
	if spec.PromptNode != "" {
		inputs[spec.PromptNode] = prompt
	}

	// 4. 画幅：ResolutionSelector 节点只认完整枚举（如 "16:9 (Widescreen)"）
	//
	// 直接丢裸 "16:9" 会在节点里抛 KeyError: '16:9'，
	// 上游回 errorCode=805「工作流运行失败」，且 traceback 指向画幅节点。
	if spec.AspectNode != "" {
		a := NormalizeAspect(spec, aspect)
		if a == "" {
			a = "9:16"
		}
		inputs[spec.AspectNode] = a
	}

	// 5. 时长（秒）
	if spec.DurationNode != "" {
		d := duration
		if d <= 0 {
			d = spec.DefaultDuration
		}
		if d <= 0 {
			d = 10
		}
		inputs[spec.DurationNode] = d
	}

	return inputs, nil
}

// RHWorkflowBrief 给前端的工作流摘要
type RHWorkflowBrief struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Desc        string `json:"desc"`
	RequiredImg int    `json:"requiredImg"`
	HasAspect   bool   `json:"hasAspect"`
}

// RHWorkflowBriefs 返回工作流摘要列表
func RHWorkflowBriefs() []RHWorkflowBrief {
	out := make([]RHWorkflowBrief, 0, len(RHWorkflows))
	for _, w := range RHWorkflows {
		out = append(out, RHWorkflowBrief{
			Name: w.Name, Label: w.Label, Kind: w.Kind, Desc: w.Desc,
			RequiredImg: w.RequiredImg, HasAspect: w.AspectNode != "",
		})
	}
	return out
}
