package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/queue"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
	"jikeai-clone/pkg/idgen"
)

// FirstFrameHandler 首帧图分组：规划 → 生成 → 视频取图
//
// 为什么单独一个 handler：规划要 LLM（分镜 handler 有），生成要队列和扣费
// （生成 handler 有），两边依赖不重合，拆开比往任一 handler 里塞更清楚。
type FirstFrameHandler struct {
	provider    *service.ModelProvider
	ffRepo      *repository.FirstFrameRepository
	boardRepo   *repository.StoryboardRepository
	episodeRepo *repository.EpisodeRepository
	projectRepo *repository.ProjectRepository
	charRepo    *repository.CharacterRepository
	sceneRepo   *repository.SceneRepository
	itemRepo    *repository.ItemRepository
	taskRepo    *repository.TaskRepository
	pointsRepo  *repository.PointsRepository
	settingRepo *repository.SettingRepository
	queue       *queue.Queue
	// resultDir / ffmpegPath：首帧规划要把资产图压缩后喂给视觉模型，
	// 需要把 /static/results/xxx 映射到磁盘并调用 ffmpeg 缩图
	resultDir  string
	ffmpegPath string
}

func NewFirstFrameHandler(
	provider *service.ModelProvider,
	ffRepo *repository.FirstFrameRepository,
	boardRepo *repository.StoryboardRepository,
	episodeRepo *repository.EpisodeRepository,
	projectRepo *repository.ProjectRepository,
	charRepo *repository.CharacterRepository,
	sceneRepo *repository.SceneRepository,
	itemRepo *repository.ItemRepository,
	taskRepo *repository.TaskRepository,
	pointsRepo *repository.PointsRepository,
	settingRepo *repository.SettingRepository,
	q *queue.Queue,
	resultDir, ffmpegPath string,
) *FirstFrameHandler {
	return &FirstFrameHandler{
		provider: provider, ffRepo: ffRepo, boardRepo: boardRepo,
		episodeRepo: episodeRepo, projectRepo: projectRepo,
		charRepo: charRepo, sceneRepo: sceneRepo, itemRepo: itemRepo,
		taskRepo: taskRepo, pointsRepo: pointsRepo, settingRepo: settingRepo,
		queue: q, resultDir: resultDir, ffmpegPath: ffmpegPath,
	}
}

// ownedEpisode 校验剧集归属
func (h *FirstFrameHandler) ownedEpisode(episodeID, userID string) (*model.Episode, error) {
	ep, err := h.episodeRepo.GetByID(episodeID)
	if err != nil {
		return nil, err
	}
	if !h.projectRepo.Exists(ep.ProjectID, userID) {
		return nil, errors.New("无权访问")
	}
	return ep, nil
}

// planAssets 收集这一集可发给规划模型的场景图与角色图（已压成 data URL）。
//
// 顺序上场景优先：空间布局是这张全景图最核心的信息。
// 只收有图的资产；单张转换失败就跳过，不让一张坏图拖垮整个规划。
// 上限 6 张：再多请求体会明显变大，模型也看不过来。
func (h *FirstFrameHandler) planAssets(episodeID string) []service.PlanAsset {
	const maxAssets = 6
	out := make([]service.PlanAsset, 0, maxAssets)

	add := func(name, kind, refURL string) {
		if len(out) >= maxAssets || strings.TrimSpace(name) == "" {
			return
		}
		refURL = strings.TrimSpace(refURL)
		if refURL == "" || strings.Contains(refURL, "placeholder://") {
			return
		}
		// 资产图可能是本机 /static 路径，也可能是上游 COS 外链，两种都支持
		src := refURL
		if !strings.HasPrefix(refURL, "http://") && !strings.HasPrefix(refURL, "https://") {
			local, ok := h.localStaticPath(refURL)
			if !ok {
				return
			}
			src = local
		}
		dataURL, err := service.InlineImage(h.ffmpegPath, src, 768)
		if err != nil {
			log.Printf("首帧规划：跳过「%s」的参考图（%v）", name, err)
			return
		}
		out = append(out, service.PlanAsset{Name: name, Kind: kind, DataURL: dataURL})
	}

	if scenes, err := h.sceneRepo.ListByEpisodeID(episodeID); err == nil {
		for _, s := range scenes {
			add(s.Name, "scene", s.ReferenceImage)
		}
	}
	if chars, err := h.charRepo.ListByEpisodeID(episodeID); err == nil {
		for _, c := range chars {
			add(c.Name, "character", c.ReferenceImage)
		}
	}
	return out
}

// localStaticPath 把 /static/results/xxx.png 映射到磁盘绝对路径
func (h *FirstFrameHandler) localStaticPath(url string) (string, bool) {
	const prefix = "/static/results/"
	if !strings.HasPrefix(url, prefix) {
		return "", false // 外链或别的目录，本地读不到
	}
	rel := strings.TrimPrefix(url, prefix)
	if rel == "" || strings.Contains(rel, "..") {
		return "", false
	}
	return filepath.Join(h.resultDir, filepath.FromSlash(rel)), true
}

// Plan 让 LLM 把这一集的分镜归并成若干首帧图分组，并整批覆盖旧分组。
//
// 这一步就是「分析这一集需要几张首帧图」——不是按段一张，
// 而是按「场景 + 机位」归并，同一机位的所有段共用一张。
func (h *FirstFrameHandler) Plan(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	episodeID := c.Param("id")

	if _, err := h.ownedEpisode(episodeID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	frames, err := h.boardRepo.ListByEpisodeID(episodeID)
	if err != nil || len(frames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "这一集还没有分镜，先生成分镜"})
		return
	}

	client, cfg, cerr := h.provider.LLM()
	if cerr != nil || client == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "没有可用的文本模型，请到管理端配置"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	// 把这一集的场景图和角色图一起喂给模型。
	// 不看图的话，它写出的「桌子横在房间中部」是凭空想象的，
	// 和场景资产图的实际布局对不上 —— 生成的首帧图自然「座位不对」。
	assets := h.planAssets(episodeID)
	if len(assets) > 0 {
		log.Printf("首帧规划：附上 %d 张资产参考图（%s）", len(assets), cfg.DisplayName)
	}

	groups, err := service.PlanFirstFrames(ctx, client, frames, assets)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	// 场景名 → 场景 ID，后面出图要按场景取参考图
	scenes, _ := h.sceneRepo.ListByEpisodeID(episodeID)
	sceneID := make(map[string]string, len(scenes))
	for _, s := range scenes {
		sceneID[s.Name] = s.ID
	}

	list := make([]model.FirstFrame, 0, len(groups))
	for i, g := range groups {
		idx, _ := json.Marshal(g.Frames)
		list = append(list, model.FirstFrame{
			ID:           idgen.New(),
			EpisodeID:    episodeID,
			SceneID:      sceneID[g.Scene],
			SceneName:    g.Scene,
			Description:  g.Description,
			FrameIndexes: string(idx),
			SortOrder:    i + 1,
		})
	}
	if err := h.ffRepo.ReplaceForEpisode(episodeID, list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"message": fmt.Sprintf("已规划 %d 组首帧图（共 %d 段）",
			len(list), len(frames)),
		"data": gin.H{"groups": list, "model": service.Describe(cfg)},
	})
}

// List 这一集的首帧图分组
// ffGroupView 首帧分组 + 最近一次生成任务的状态。
// 前端靠 taskStatus 判断「失败」并结束等待 —— 只看 image 是否为空的话，
// 任务失败后图永远是空的，界面会一直转圈到轮询超时。
type ffGroupView struct {
	model.FirstFrame
	TaskStatus string `json:"taskStatus,omitempty"`
	TaskError  string `json:"taskError,omitempty"`
}

func (h *FirstFrameHandler) List(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	episodeID := c.Param("id")
	if _, err := h.ownedEpisode(episodeID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	list, err := h.ffRepo.ListByEpisodeID(episodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	ids := make([]string, 0, len(list))
	for _, g := range list {
		ids = append(ids, g.ID)
	}
	latest, _ := h.taskRepo.LatestByAssetIDs("firstframe", "group", ids)

	out := make([]ffGroupView, 0, len(list))
	for _, g := range list {
		v := ffGroupView{FirstFrame: g}
		if t := latest[g.ID]; t != nil {
			v.TaskStatus = t.Status
			v.TaskError = t.ErrorMsg
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// UpdateGroup 手动改一组的描述或覆盖的段（规划结果不理想时用）
func (h *FirstFrameHandler) UpdateGroup(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	id := c.Param("id")

	g, err := h.ffRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分组不存在"})
		return
	}
	if _, err := h.ownedEpisode(g.EpisodeID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分组不存在"})
		return
	}

	var req struct {
		Description  *string `json:"description"`
		FrameIndexes *[]int  `json:"frameIndexes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数不合法"})
		return
	}
	if req.Description != nil {
		g.Description = strings.TrimSpace(*req.Description)
	}
	if req.FrameIndexes != nil {
		idx, _ := json.Marshal(*req.FrameIndexes)
		g.FrameIndexes = string(idx)
	}
	if err := h.ffRepo.Update(g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": g})
}

// Generate 按分组出图：每组一张，角色图 + 场景图 + 该组构图描述。
func (h *FirstFrameHandler) Generate(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	episodeID := c.Param("id")

	ep, err := h.ownedEpisode(episodeID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	var req struct {
		GroupIDs []string `json:"groupIds"` // 空 = 全部组
		ModelID  string   `json:"modelId"`
	}
	_ = c.ShouldBindJSON(&req)

	list, err := h.ffRepo.ListByEpisodeID(episodeID)
	if err != nil || len(list) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "还没有首帧规划，先点「首帧规划」"})
		return
	}
	if len(req.GroupIDs) > 0 {
		want := make(map[string]bool, len(req.GroupIDs))
		for _, id := range req.GroupIDs {
			want[id] = true
		}
		picked := make([]model.FirstFrame, 0, len(req.GroupIDs))
		for _, g := range list {
			if want[g.ID] {
				picked = append(picked, g)
			}
		}
		if len(picked) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "指定的分组不存在"})
			return
		}
		list = picked
	}

	// 段号 → 帧实体，用来取角色/场景
	frames, _ := h.boardRepo.ListByEpisodeID(episodeID)
	byIndex := make(map[int]*model.Storyboard, len(frames))
	for i := range frames {
		byIndex[frames[i].FrameIndex] = &frames[i]
	}

	aspect := strings.TrimSpace(ep.AspectRatio)
	if aspect == "" {
		aspect = "9:16"
	}
	cost := pointCost(h.settingRepo, "points_image", 2)

	tasks := make([]*model.GenerationTask, 0, len(list))
	skipped := 0
	for _, g := range list {
		var idxs []int
		_ = json.Unmarshal([]byte(g.FrameIndexes), &idxs)
		if len(idxs) == 0 {
			skipped++
			continue
		}
		// 用这组第一段当代表：取它的角色/场景引用
		rep := byIndex[idxs[0]]
		if rep == nil {
			skipped++
			continue
		}
		rep.FillRefs()

		roles := make([]model.Character, 0, len(rep.Roles))
		for _, r := range rep.Roles {
			if r.ID == "" {
				continue
			}
			if ch, e := h.charRepo.GetByID(r.ID); e == nil {
				roles = append(roles, *ch)
			}
		}
		scenes := make([]model.Scene, 0, len(rep.Scenes))
		for _, s := range rep.Scenes {
			if s.ID == "" {
				continue
			}
			if sc, e := h.sceneRepo.GetByID(s.ID); e == nil {
				scenes = append(scenes, *sc)
			}
		}
		// 分镜漏标场景时兜底：这一集只有一个场景资产，就认定是它。
		//
		// 为什么会漏标：分镜走规则引擎降级时（LLM 没配 key 或调用失败），
		// 场景是按「剧本里有没有出现场景名」做关键词匹配的。
		// 剧本写「老师办公室」、资产叫「教师办公室-白天」，就一条都匹配不上，
		// 于是整段 scenes 为空 → 首帧凑不到 2 张参考图 → 整组被跳过，
		// 用户看到「已提交 0 张首帧图」，但场景图其实一直在库里。
		//
		// 只有一个场景时不存在歧义，直接用；多个场景才跳过让人工确认。
		if len(scenes) == 0 {
			if all, e := h.sceneRepo.ListByEpisodeID(episodeID); e == nil && len(all) == 1 {
				scenes = all
			}
		}

		// 道具也要进参考图：只给角色和场景时，模型会按自己的理解画桌面上的东西
		// （实测把深灰保温杯画成白色、牛皮纸笔记本画成蓝色）
		items := make([]model.Item, 0, len(rep.Items))
		for _, it := range rep.Items {
			if it.ID == "" {
				continue
			}
			if obj, e := h.itemRepo.GetByID(it.ID); e == nil {
				items = append(items, *obj)
			}
		}

		refs := make([]string, 0, len(roles)+len(scenes)+len(items)+1)

		// 空间基准图排第一：优先用「已生成的任意一组首帧」，没有才用场景图。
		//
		// 为什么：每段独立出图时，模型对「桌子长什么样、有几把椅子、窗户在哪」
		// 每次理解都略有不同 —— 十段拼起来就像换了十个房间。
		// 拿一张已经定稿的首帧当第一参考，家具直接从那张图抄，
		// 一致性比单纯给场景资产图强得多（场景图是空房渲染，风格也和成片不一样）。
		baseline := ""
		for _, other := range list {
			if other.ID == g.ID {
				continue
			}
			if u := strings.TrimSpace(other.Image); u != "" && !strings.Contains(u, "placeholder://") {
				baseline = stripQuery(u)
				break
			}
		}
		if baseline != "" {
			refs = append(refs, baseline)
		}

		for _, r := range roles {
			if u := strings.TrimSpace(r.ReferenceImage); u != "" && !strings.Contains(u, "placeholder://") {
				refs = append(refs, stripQuery(u))
			}
		}
		for _, sc := range scenes {
			if u := strings.TrimSpace(sc.ReferenceImage); u != "" && !strings.Contains(u, "placeholder://") {
				refs = append(refs, stripQuery(u))
			}
		}
		for _, it := range items {
			if u := strings.TrimSpace(it.ReferenceImage); u != "" && !strings.Contains(u, "placeholder://") {
				refs = append(refs, stripQuery(u))
			}
		}
		// 工作流只有 9 个图位，超了上游会报错
		if len(refs) > 9 {
			refs = refs[:9]
		}
		if len(refs) < 2 {
			skipped++
			continue
		}

		prompt := service.BuildGroupFirstFramePrompt(g.Description, rep.VideoPrompt, roles, scenes, items)
		params, _ := json.Marshal(map[string]interface{}{
			"refImages": refs,
			"aspect":    aspect,
			"groupKey":  g.ID,
		})
		task, cerr := h.createFFTask(userID, "firstframe", "group", g.ID, episodeID,
			req.ModelID, prompt, string(params), cost)
		if cerr != nil {
			if errors.Is(cerr, repository.ErrInsufficientPoints) {
				c.JSON(http.StatusPaymentRequired, gin.H{
					"code": 402, "message": "积分不足，本次需要 " + itoa(cost) + " 积分",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": cerr.Error()})
			return
		}
		h.queue.Enqueue(task.ID)
		tasks = append(tasks, task)
	}

	msg := fmt.Sprintf("已提交 %d 张首帧图", len(tasks))
	if skipped > 0 {
		msg += fmt.Sprintf("（%d 组缺角色或场景参考图，已跳过）", skipped)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": msg,
		"data": gin.H{"tasks": tasks, "cost": cost * len(tasks), "skipped": skipped},
	})
}

// createFFTask 建首帧图任务并扣费（与 GenerateHandler.createTask 同构，
// 区别是 assetType=group 时按分组 ID 落库，队列成功后回写 first_frames.image）
func (h *FirstFrameHandler) createFFTask(userID, taskType, assetType, assetID, episodeID, modelID, prompt, params string, cost int) (*model.GenerationTask, error) {
	if _, err := h.pointsRepo.Consume(userID, cost, taskType, "生成首帧图", assetID, assetType); err != nil {
		return nil, err
	}
	now := time.Now()
	task := &model.GenerationTask{
		ID: idgen.New(), UserID: userID, Type: taskType,
		EpisodeID: episodeID, AssetType: assetType, AssetID: assetID,
		Prompt: prompt, ModelID: modelID, Params: params,
		Status: "pending", Progress: 0, Credits: cost,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := h.taskRepo.Create(task); err != nil {
		_, _ = h.pointsRepo.Grant(userID, cost, "refund", "任务创建失败退还", task.ID, "task")
		return nil, err
	}
	return task, nil
}
