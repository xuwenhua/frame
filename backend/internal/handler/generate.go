package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
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

type GenerateHandler struct {
	taskRepo    *repository.TaskRepository
	projectRepo *repository.ProjectRepository
	episodeRepo *repository.EpisodeRepository
	charRepo    *repository.CharacterRepository
	sceneRepo   *repository.SceneRepository
	itemRepo    *repository.ItemRepository
	boardRepo   *repository.StoryboardRepository
	ffRepo      *repository.FirstFrameRepository
	pointsRepo  *repository.PointsRepository
	settingRepo *repository.SettingRepository
	queue       *queue.Queue
	// resultDir 产物目录：重新生成草图前要清掉上一批整图，否则镜数变少会残留过期图
	resultDir string
}

func NewGenerateHandler(
	taskRepo *repository.TaskRepository,
	projectRepo *repository.ProjectRepository,
	episodeRepo *repository.EpisodeRepository,
	charRepo *repository.CharacterRepository,
	sceneRepo *repository.SceneRepository,
	itemRepo *repository.ItemRepository,
	boardRepo *repository.StoryboardRepository,
	ffRepo *repository.FirstFrameRepository,
	pointsRepo *repository.PointsRepository,
	settingRepo *repository.SettingRepository,
	q *queue.Queue,
	resultDir string,
) *GenerateHandler {
	return &GenerateHandler{
		taskRepo: taskRepo, projectRepo: projectRepo, episodeRepo: episodeRepo,
		charRepo: charRepo, sceneRepo: sceneRepo, itemRepo: itemRepo,
		boardRepo: boardRepo, ffRepo: ffRepo, pointsRepo: pointsRepo, settingRepo: settingRepo,
		queue: q, resultDir: resultDir,
	}
}

// GenerateImage 生成单张资产参考图（支持图生图：传 refImages 与图生图模型）
func (h *GenerateHandler) GenerateImage(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		AssetType  string   `json:"assetType" binding:"required"` // character / scene / item
		AssetID    string   `json:"assetId" binding:"required"`
		Prompt     string   `json:"prompt"`
		ModelID    string   `json:"modelId"`   // 指定图像模型（为空则用默认）
		RefImages  []string `json:"refImages"` // 图生图参考图 URL 列表
		CharStyle  string   `json:"charStyle"`
		SceneStyle string   `json:"sceneStyle"`
		Resolution string   `json:"resolution"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 assetType 或 assetId"})
		return
	}

	// 取资产内容 + 校验归属
	prompt, episodeID, ok := h.resolveAsset(userID, req.AssetType, req.AssetID, req.Prompt)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "资产不存在"})
		return
	}

	// 参考图只允许用本集资产已生成的图，避免越权引用
	refs := h.filterOwnRefs(userID, episodeID, req.RefImages)

	cost := pointCost(h.settingRepo, "points_image", 2)

	sp := h.styleOf(episodeID)
	opts := service.GenOptions{
		CharStyle: req.CharStyle, SceneStyle: req.SceneStyle, Resolution: req.Resolution,
	}.Normalize()

	// 工具栏样式直接改写提示词尾部的构图约束（三视图 → 单人全身 / 半身等）
	prompt = service.ApplyGenOptions(prompt, req.AssetType, sp, opts)
	params := buildImageParams(req.AssetType, sp, opts, refs)

	task, err := h.createTask(userID, "image", req.AssetType, req.AssetID, episodeID, req.ModelID, prompt, params, cost)
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientPoints) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code": 402, "message": "积分不足，本次需要 " + itoa(cost) + " 积分",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	h.queue.Enqueue(task.ID)

	balance, _ := h.pointsRepo.Balance(userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "已加入生成队列",
		"data":    gin.H{"task": task, "cost": cost, "balance": balance},
	})
}

// GenerateSketch 给整集分镜生成缩略速写草图（只当构图参考）。
//
// 一张图装 8 格（竖版 4×2 / 横版 2×4），格内左上角带镜号，成功后按格裁开写回对应帧。
// 草图的作用是给模型一个「机位/景别/站位」的视觉锚点 ——
// 纯文字写「中近景｜固定机位」模型只能猜，有草图就照着摆。
func (h *GenerateHandler) GenerateVideo(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		StoryboardID string `json:"storyboardId" binding:"required"`
		ModelID      string `json:"modelId"`
		// 下面三项来自分镜工具栏，为空时沿用模型配置里的默认值
		Duration    int    `json:"duration"`    // 秒
		AspectRatio string `json:"aspectRatio"` // 16:9 / 9:16
		// SceneRef 场景参考用哪一份素材：
		//   image（默认）= 场景原图；panorama = 柱形全景图；video = 360 全景视频
		// 选 panorama/video 时，场景图位换成全景图；选 video 还会把全景视频
		// 作为参考视频传给 H3（节点 272），让它看清整个房间。
		SceneRef string `json:"sceneRef"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 storyboardId"})
		return
	}

	// 取分镜 + 校验归属
	board, err := h.boardRepo.GetByID(req.StoryboardID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分镜不存在"})
		return
	}
	ep, err := h.episodeRepo.GetByID(board.EpisodeID)
	if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分镜不存在"})
		return
	}

	// 前端没指定就沿用生成分镜时定下的场景参考 ——
	// 提示词是按那个选择写的详略，出片换成另一种素材会自相矛盾
	if strings.TrimSpace(req.SceneRef) == "" {
		req.SceneRef = board.SceneRef
	}

	cost := pointCost(h.settingRepo, "points_video", 35)

	// 发给模型的提示词：默认用原文，有参考图时把 @资产名 换成 图N（模型只认图位序号）。
	// EnsureSpeechRate 给老分镜补「语气：约 X 字/秒」—— 旧描述词里没有语速行，
	// 模型只能靠字数÷时长自己猜，念快了嘴跟不上、念慢了片子被截。
	// 就地补而不是重跑分镜：重跑会清掉已绑定的全景参考图，还要重新出图花积分。
	basePrompt := service.EnsureSpeechRate(board.VideoPrompt)
	modelPrompt := basePrompt

	// 工具栏选择落到任务参数里，由队列在出片时覆盖模型默认值
	params := ""
	{
		p := map[string]interface{}{}
		// 时长：工具栏没指定（自动模式）就沿用这一帧自己的 duration
		dur := req.Duration
		if dur <= 0 && board.Duration > 0 {
			dur = int(board.Duration)
		}
		if dur > 0 {
			p["duration"] = dur
		}
		if req.AspectRatio != "" {
			p["aspectRatio"] = req.AspectRatio
		}
		if req.SceneRef != "" {
			p["sceneRef"] = req.SceneRef
		}
		// 选了 360 全景视频：视频本身也要带下去，出片时进参考视频位。
		// 必须先 FillRefs —— board.Scenes 是从 scenesJson 还原出来的，
		// 不先填就是空的，videoUrl 永远查不到。
		if req.SceneRef == "video" {
			board.FillRefs()
			for _, sc := range board.Scenes {
				if sc.ID == "" {
					continue
				}
				if full, e := h.sceneRepo.GetByID(sc.ID); e == nil && strings.TrimSpace(full.PanoramaVideo) != "" {
					p["videoUrl"] = full.PanoramaVideo
					break
				}
			}
		}
		// 优先用「所属分组」的首帧图：同机位的所有段共用一张，空间和人物位置才锁得住。
		// 没有规划结果时退回帧级 first_frame_image（按帧单独合成的那套）。
		if h.ffRepo != nil {
			if groups, e := h.ffRepo.ListByEpisodeID(board.EpisodeID); e == nil {
				for _, g := range groups {
					if strings.TrimSpace(g.Image) == "" {
						continue
					}
					var idxs []int
					_ = json.Unmarshal([]byte(g.FrameIndexes), &idxs)
					for _, idx := range idxs {
						if idx == board.FrameIndex {
							board.FirstFrameImage = g.Image
							break
						}
					}
				}
			}
		}

		// 选了 360 视频：地点行整行换成「在参考视频 1 的场景中 + 人物站位」。
		//
		// 为什么连家具描述一起删：空间已由视频给出，文字再写一遍只会和视频里的
		// 实际排布打架，模型还得分神判断该信谁。人物位置必须留 —— 视频里只有房间，
		// 没有人站哪儿。位置从分镜的 [开场站位] 取（situation 里那部分旧分镜是空的）。
		if req.SceneRef == "video" {
			sceneName := ""
			for _, sc := range board.Scenes {
				if strings.TrimSpace(sc.Name) != "" {
					sceneName = sc.Name
					break
				}
			}
			if sceneName == "" {
				sceneName = "场景"
			}
			basePrompt = replaceSceneLine(basePrompt, sceneName, openingFromText(board.Text))
		}

		// 视频工作流几乎都要求参考图（首帧/角色图）。
		// 以前这里什么都不传，9 个图位全是 null，上游直接返回 errorCode=803 JsonNull。
		if refs := h.frameRefSlots(board, req.SceneRef); len(refs) > 0 {
			urls := make([]string, 0, len(refs))
			names := make([]string, 0, len(refs))
			kinds := make([]string, 0, len(refs))
			for _, r := range refs {
				urls = append(urls, r.URL)
				names = append(names, r.Name)
				kinds = append(kinds, r.Kind)
			}
			p["refImages"] = urls
			modelPrompt = service.RewriteMentionsForModel(basePrompt, names, kinds)
		}
		if len(p) > 0 {
			if raw, e := json.Marshal(p); e == nil {
				params = string(raw)
			}
		}
	}

	task, err := h.createTask(userID, "video", "frame", board.ID, board.EpisodeID, req.ModelID, modelPrompt, params, cost)
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientPoints) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code": 402, "message": "积分不足，本次需要 " + itoa(cost) + " 积分",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	h.queue.Enqueue(task.ID)

	balance, _ := h.pointsRepo.Balance(userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "已加入生成队列",
		"data":    gin.H{"task": task, "cost": cost, "balance": balance},
	})
}

// panoramaPrompt 柱形全景图的编辑指令。
//
// 要点：不是「扩展成 2:1」这么简单 —— 必须强调左右两端是同一个位置（无缝），
// 否则转 360 视频时接缝处会裂开一条明显的缝。
const panoramaPrompt = "根据参考图生成柱形全景图：360 度等距柱状投影，横向展开，" +
	"左右两端首尾相接、能无缝衔接；保持参考图里的房间布局、家具、材质、配色和光线完全一致；" +
	"不要出现人物、文字、水印、边框。"

// GeneratePanorama 把场景参考图扩展成柱形全景图。
//
// 用图片编辑模型（图生图），不是文生图 —— 场景图已经定了这间屋子长什么样，
// 要让模型「往外延伸」而不是「重新画一个房间」。
func (h *GenerateHandler) GeneratePanorama(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	var req struct {
		SceneID string `json:"sceneId" binding:"required"`
		ModelID string `json:"modelId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 sceneId"})
		return
	}
	sc, err := h.sceneRepo.GetByID(req.SceneID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "场景不存在"})
		return
	}
	if strings.TrimSpace(sc.ReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "这个场景还没有参考图，先生成场景图"})
		return
	}
	if strings.TrimSpace(req.ModelID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请选择图片编辑模型（图生图）"})
		return
	}

	cost := pointCost(h.settingRepo, "points_image", 2)
	params, _ := json.Marshal(map[string]interface{}{
		// 只传一张：这个工作流默认必传一张（476），477 及 490-496 都是可选位。
		// 之前把同一张填进两个必填位，上游直接报 805 工作流执行失败。
		"refImages": []string{sc.ReferenceImage},
		// 全景图要宽幅，9:16 竖图转 360 会严重变形；工作流没有 2:1，16:9 最接近
		"aspect": "16:9",
	})
	task, cerr := h.createTask(userID, "panorama", "scene", sc.ID, sc.EpisodeID,
		req.ModelID, panoramaPrompt, string(params), cost)
	if cerr != nil {
		if errors.Is(cerr, repository.ErrInsufficientPoints) {
			c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "message": "积分不足，本次需要 " + itoa(cost) + " 积分"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": cerr.Error()})
		return
	}
	h.queue.Enqueue(task.ID)

	balance, _ := h.pointsRepo.Balance(userID)
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "已开始生成柱形全景图",
		"data": gin.H{"task": task, "cost": cost, "balance": balance},
	})
}

// GeneratePanoramaVideo 把柱形全景图转成 360 全景视频（panorama-to-360video 工作流）。
//
// 这个工作流只有一个输入图位，没有提示词/时长/画幅节点。
func (h *GenerateHandler) GeneratePanoramaVideo(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	var req struct {
		SceneID string `json:"sceneId" binding:"required"`
		ModelID string `json:"modelId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 sceneId 或 modelId"})
		return
	}
	sc, err := h.sceneRepo.GetByID(req.SceneID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "场景不存在"})
		return
	}
	if strings.TrimSpace(sc.PanoramaImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "还没有柱形全景图，先生成全景图"})
		return
	}

	cost := pointCost(h.settingRepo, "points_video", 35)
	params, _ := json.Marshal(map[string]interface{}{
		"refImages": []string{sc.PanoramaImage},
	})
	task, cerr := h.createTask(userID, "panorama_video", "scene", sc.ID, sc.EpisodeID,
		req.ModelID, "", string(params), cost)
	if cerr != nil {
		if errors.Is(cerr, repository.ErrInsufficientPoints) {
			c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "message": "积分不足，本次需要 " + itoa(cost) + " 积分"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": cerr.Error()})
		return
	}
	h.queue.Enqueue(task.ID)

	balance, _ := h.pointsRepo.Balance(userID)
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "已开始生成 360 全景视频",
		"data": gin.H{"task": task, "cost": cost, "balance": balance},
	})
}

// BatchGenerateImages 批量生成某集的所有资产图
// 支持与单张生成一致的工具栏选项（模型 / 分辨率 / 角色样式 / 场景样式）
func (h *GenerateHandler) BatchGenerateImages(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		EpisodeID  string `json:"episodeId" binding:"required"`
		ModelID    string `json:"modelId"`
		CharStyle  string `json:"charStyle"`
		SceneStyle string `json:"sceneStyle"`
		Resolution string `json:"resolution"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	ep, err := h.episodeRepo.GetByID(req.EpisodeID)
	if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	chars, _ := h.charRepo.ListByEpisodeID(req.EpisodeID)
	scenes, _ := h.sceneRepo.ListByEpisodeID(req.EpisodeID)
	items, _ := h.itemRepo.ListByEpisodeID(req.EpisodeID)

	total := len(chars) + len(scenes) + len(items)
	if total == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "没有可生成的资产，请先识别"})
		return
	}

	unit := pointCost(h.settingRepo, "points_image", 2)
	totalCost := unit * total

	// 先检查余额，不足则整批拒绝
	balance, _ := h.pointsRepo.Balance(userID)
	if balance < totalCost {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code":    402,
			"message": "积分不足，本批需要 " + itoa(totalCost) + " 积分（当前 " + itoa(balance) + "）",
		})
		return
	}

	sp := h.styleOf(req.EpisodeID)
	opts := service.GenOptions{
		CharStyle: req.CharStyle, SceneStyle: req.SceneStyle, Resolution: req.Resolution,
	}.Normalize()

	created := make([]model.GenerationTask, 0, total)
	failures := make([]string, 0)

	enqueue := func(assetType, assetID string) {
		prompt, _, ok := h.resolveAsset(userID, assetType, assetID, "")
		if !ok {
			failures = append(failures, assetID+"：资产不存在")
			return
		}
		prompt = service.ApplyGenOptions(prompt, assetType, sp, opts)
		params := buildImageParams(assetType, sp, opts, nil)
		t, err := h.createTask(userID, "image", assetType, assetID, req.EpisodeID, req.ModelID, prompt, params, unit)
		if err != nil {
			// 不能静默跳过：用户会以为全部提交了，实际少了几张
			log.Printf("批量出图创建任务失败 asset=%s/%s: %v", assetType, assetID, err)
			failures = append(failures, assetID+"："+err.Error())
			return
		}
		h.queue.Enqueue(t.ID)
		created = append(created, *t)
	}

	for _, ch := range chars {
		enqueue("character", ch.ID)
	}
	for _, sc := range scenes {
		enqueue("scene", sc.ID)
	}
	for _, it := range items {
		enqueue("item", it.ID)
	}

	newBalance, _ := h.pointsRepo.Balance(userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "已提交 " + itoa(len(created)) + " 个生成任务",
		"data": gin.H{
			"tasks":    created,
			"count":    len(created),
			"expected": total,
			"failures": failures,
			// 按实际创建数计费，避免「扣了 5 个只建了 3 个」
			"cost":    unit * len(created),
			"balance": newBalance,
		},
	})
}

// TaskStatus 查询任务状态
func (h *GenerateHandler) TaskStatus(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	task, err := h.taskRepo.GetByID(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": task})
}

// ListTasks 任务列表（可按 episodeId / status 过滤）
func (h *GenerateHandler) ListTasks(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	list, err := h.taskRepo.List(userID, c.Query("episodeId"), c.Query("status"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": list})
}

// FrameTaskHistory 按分镜查成功任务历史（预览列「历史」tab）
//
// GET /api/tasks/frame/:id/history?type=video
// 前端在历史 tab 里列出该分镜所有成功产出的视频，点一条回填到分镜的 videoUrl。
func (h *GenerateHandler) FrameTaskHistory(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	list, err := h.taskRepo.ListByStoryboard(userID, c.Param("id"), c.Query("type"), 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": list})
}

// ---------- 内部 ----------

// taskReason 生成积分流水的说明文案
func taskReason(taskType, assetType string) string {
	switch taskType {
	case "image":
		switch assetType {
		case "character":
			return "生成角色参考图"
		case "scene":
			return "生成场景参考图"
		case "item":
			return "生成道具参考图"
		}
		return "生成参考图"
	case "video":
		return "生成分镜视频"
	case "tts":
		return "生成配音"
	}
	return "生成内容"
}

// createTask 扣积分 + 建任务
func (h *GenerateHandler) createTask(userID, taskType, assetType, assetID, episodeID, modelID, prompt, params string, cost int) (*model.GenerationTask, error) {
	// 先扣费（流水说明用中文，避免中英混排）
	if _, err := h.pointsRepo.Consume(userID, cost, taskType, taskReason(taskType, assetType), assetID, assetType); err != nil {
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
	// 分镜视频任务同时写 storyboard_id：「历史」tab 按 storyboard_id 查，
	// 而 assetID 对图片/视频任务含义不同（图片=资产ID，视频=分镜ID），不能混用。
	// 首帧图同样按分镜落库：队列成功后要凭 StoryboardID 回写 first_frame_image，
	// 不设的话回写语句匹配不到任何行，图生成了但帧上还是空的
	if (taskType == "video" || taskType == "firstframe") && assetType == "frame" {
		task.StoryboardID = assetID
	}

	if err := h.taskRepo.Create(task); err != nil {
		// 建任务失败要退还积分
		_, _ = h.pointsRepo.Grant(userID, cost, "refund", "任务创建失败退还", task.ID, "task")
		return nil, err
	}
	return task, nil
}

// styleOf 读该集剧本的视觉风格约束（画风 / 画幅 / 无正脸）
func (h *GenerateHandler) styleOf(episodeID string) service.StyleProfile {
	ep, err := h.episodeRepo.GetByID(episodeID)
	if err != nil || ep.ScriptText == "" {
		return service.DefaultStyle()
	}
	return service.DetectStyle(ep.ScriptText)
}

// buildImageParams 组装任务参数：参考图 / 画幅 / 分辨率
// 画幅由工具栏样式决定（三视图横向、单人竖版、多角度宽幅…）
func buildImageParams(assetType string, sp service.StyleProfile, opts service.GenOptions, refs []string) string {
	p := map[string]interface{}{}
	if len(refs) > 0 {
		p["refImages"] = refs
	}
	if aspect := opts.AspectFor(assetType, sp); aspect != "" {
		p["aspect"] = aspect
	}
	if opts.Resolution != "" {
		p["resolution"] = opts.Resolution
	}
	if len(p) == 0 {
		return ""
	}
	raw, _ := json.Marshal(p)
	return string(raw)
}

// filterOwnRefs 只保留本集资产已生成的真实图片，最多 4 张
// 避免用户把任意外链塞给网关（也算一层越权与滥用防护）
func (h *GenerateHandler) filterOwnRefs(userID, episodeID string, refs []string) []string {
	if len(refs) == 0 || episodeID == "" {
		return nil
	}
	ep, err := h.episodeRepo.GetByID(episodeID)
	if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
		return nil
	}

	// 收集本集所有资产的现有图
	allowed := map[string]bool{}
	if list, err := h.charRepo.ListByEpisodeID(episodeID); err == nil {
		for _, c := range list {
			if c.ReferenceImage != "" {
				allowed[c.ReferenceImage] = true
			}
		}
	}
	if list, err := h.sceneRepo.ListByEpisodeID(episodeID); err == nil {
		for _, s := range list {
			if s.ReferenceImage != "" {
				allowed[s.ReferenceImage] = true
			}
		}
	}
	if list, err := h.itemRepo.ListByEpisodeID(episodeID); err == nil {
		for _, it := range list {
			if it.ReferenceImage != "" {
				allowed[it.ReferenceImage] = true
			}
		}
	}

	out := make([]string, 0, len(refs))
	// 用户自己上传的垫图文件名以「用户ID前8位-」开头，只认自己的
	ownUploadPrefix := "/static/uploads/images/" + shortID(userID) + "-"
	for _, r := range refs {
		// 占位标记不是真实图片，传给网关只会失败
		if strings.Contains(r, "placeholder://") {
			continue
		}
		if allowed[r] || strings.HasPrefix(r, ownUploadPrefix) {
			out = append(out, r)
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}

// resolveAsset 取资产 prompt 并校验归属
// 优先级：调用方传入的 prompt > 识别阶段存下的 imagePrompt > 现场按字段拼
func (h *GenerateHandler) resolveAsset(userID, assetType, assetID, customPrompt string) (prompt, episodeID string, ok bool) {
	switch assetType {
	case "character":
		ch, err := h.charRepo.GetByID(assetID)
		if err != nil {
			return "", "", false
		}
		ep, err := h.episodeRepo.GetByID(ch.EpisodeID)
		if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
			return "", "", false
		}
		if customPrompt != "" {
			return customPrompt, ch.EpisodeID, true
		}
		if ch.ImagePrompt != "" {
			return ch.ImagePrompt, ch.EpisodeID, true
		}
		return service.BuildCharacterPrompt(service.RoleDraft{
			Name: ch.Name, IdentityAnchor: ch.IdentityAnchor,
			CostumePrompt: ch.CostumePrompt, AppearanceCues: ch.AppearanceCues,
		}, h.styleOf(ch.EpisodeID)), ch.EpisodeID, true

	case "scene":
		sc, err := h.sceneRepo.GetByID(assetID)
		if err != nil {
			return "", "", false
		}
		ep, err := h.episodeRepo.GetByID(sc.EpisodeID)
		if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
			return "", "", false
		}
		if customPrompt != "" {
			return customPrompt, sc.EpisodeID, true
		}
		if sc.ImagePrompt != "" {
			return sc.ImagePrompt, sc.EpisodeID, true
		}
		return service.BuildScenePrompt(service.SceneDraft{
			Name: sc.Name, SpatialAnchor: sc.SpatialAnchor,
			LightingPrompt: sc.LightingPrompt, TimeOfDay: sc.TimeOfDay,
		}, h.styleOf(sc.EpisodeID)), sc.EpisodeID, true

	case "item":
		it, err := h.itemRepo.GetByID(assetID)
		if err != nil {
			return "", "", false
		}
		ep, err := h.episodeRepo.GetByID(it.EpisodeID)
		if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
			return "", "", false
		}
		if customPrompt != "" {
			return customPrompt, it.EpisodeID, true
		}
		if it.ImagePrompt != "" {
			return it.ImagePrompt, it.EpisodeID, true
		}
		return service.BuildItemPrompt(service.ItemDraft{
			Name: it.Name, Category: it.Category, Prompt: it.Prompt,
		}, h.styleOf(it.EpisodeID)), it.EpisodeID, true
	}
	return "", "", false
}

// frameRefSlot 一个参考图位：图片地址 + 对应资产名
// 名字用来把提示词里的 @老师 换成 图1 —— 上游只拿到一串图片，不认识「老师」是谁
type frameRefSlot struct {
	URL  string
	Name string
	// Kind 这张图是什么：scene / character / item。
	// subject_definitions 要按类型写「保留什么」—— 场景图说「只参考空间结构」，
	// 人物图说「面部、发型、服装完整保留」，道具图说「颜色、材质、形状」。
	Kind string
}

// sortRefSlotsByName 组内按名字排序，保证同一批资产在不同帧里的图位顺序一致
func sortRefSlotsByName(slots []frameRefSlot) {
	sort.Slice(slots, func(i, j int) bool { return slots[i].Name < slots[j].Name })
}

// frameRefSlots 取该分镜可用的参考图（角色优先，其次场景，最后道具）
//
// 为什么要做：视频工作流的图位必须有图，否则上游返回 803 JsonNull。
// 分镜本身只存了资产引用（id/name），真正能用的图在资产表里 ——
// 占位图（placeholder://）要跳过。
//
// 顺序有含义：第 1 张进首帧位（51），其余依次进 49/50/43… 参考位。
// 以前只取第一张，结果提示词里 @ 了 5 个资产、上游只收到 1 张图，
// 模型自然不知道其他角色/道具长什么样。
func (h *GenerateHandler) frameRefSlots(board *model.Storyboard, sceneRef string) []frameRefSlot {
	if board == nil {
		return nil
	}
	board.FillRefs()

	usable := func(u string) bool {
		u = strings.TrimSpace(u)
		return u != "" && !strings.Contains(u, "placeholder://")
	}

	// 工作流最多 9 个图位，首帧占 1 个，其余 8 个给参考图
	const maxRefs = 9

	out := make([]frameRefSlot, 0, maxRefs)
	seen := map[string]bool{}
	push := func(u, name, kind string) {
		u = strings.TrimSpace(u)
		if !usable(u) || seen[u] || len(out) >= maxRefs {
			return
		}
		seen[u] = true
		out = append(out, frameRefSlot{URL: u, Name: name, Kind: kind})
	}

	// 整集资产：一是拿场景名，二是给正文兜底用
	allChars, _ := h.charRepo.ListByEpisodeID(board.EpisodeID)
	allScenes, _ := h.sceneRepo.ListByEpisodeID(board.EpisodeID)
	allItems, _ := h.itemRepo.ListByEpisodeID(board.EpisodeID)

	// 场景名：优先分镜自己标的，没有就用整集唯一的场景名
	sceneName := ""
	if len(board.Scenes) > 0 {
		sceneName = strings.TrimSpace(board.Scenes[0].Name)
	}
	if sceneName == "" && len(allScenes) == 1 {
		sceneName = strings.TrimSpace(allScenes[0].Name)
	}

	// 首帧图（= 定稿的场景全景）占第一张，并且**顶替**场景资产图。
	//
	// 为什么用场景名当它的名字：提示词里写的是「地点：@教师办公室-白天」，
	// 只有名字对上，RewriteMentionsForModel 才会把它换成 <Picture 1>。
	// 之前固定叫「场景全景参考」，提示词里的 @场景名 匹配不到，
	// 首帧图虽然传了却没人引用。
	//
	// 为什么不再传场景资产图：同一间房的两个版本（一个空房、一个有人的定稿），
	// 模型会纠结以谁为准；首帧图已经锁死了布局、光线和家具位置。
	hasFirstFrame := false
	// 选 360 全景视频时连首帧图也不传：首帧图本质还是「一张房间图」，
	// 留着它，提示词里 <Picture 1> 又会被写成「教师办公室-白天」，
	// 等于场景图以另一种形式混了进来，和全景视频仍是两套空间参考。
	if sceneRef != "video" {
		if u := strings.TrimSpace(board.FirstFrameImage); usable(u) {
			name := sceneName
			if name == "" {
				name = "场景"
			}
			push(stripQuery(u), name, "scene")
			hasFirstFrame = true
		}
	}

	// 角色优先（说话视频靠角色形象驱动），再场景，最后道具。
	// 每组先收集再按名字排序：LLM 输出的 roles 顺序不固定，
	// 同一批人在不同帧里可能一个排前一个排后，图位（图1/图2）跟着变，
	// 人工核对时对不上号。按名字排序后，只要这一帧用的人一样，编号就一样。
	var roles, scenes, items []frameRefSlot
	for _, r := range board.Roles {
		if r.ID == "" {
			continue
		}
		if ch, err := h.charRepo.GetByID(r.ID); err == nil {
			roles = append(roles, frameRefSlot{URL: ch.ReferenceImage, Name: ch.Name, Kind: "character"})
		}
	}
	sortRefSlotsByName(roles)
	for _, r := range roles {
		push(r.URL, r.Name, r.Kind)
	}

	// 正文兜底：@ 到、但 rolesJson 里没标的角色也要进图位。
	// LLM 生成分镜时经常只在正文里写「@学生」，roleNames 却没给 ——
	// 结果是学生在这段里说着话，图位里却只有老师，模型只能凭空编一张脸。
	knownChar := map[string]bool{}
	for _, r := range board.Roles {
		knownChar[r.Name] = true
	}
	for _, c := range allChars {
		if knownChar[c.Name] {
			continue
		}
		if strings.Contains(board.VideoPrompt, "@"+c.Name) {
			push(c.ReferenceImage, c.Name, "character")
		}
	}

	// 选了 360 全景视频时**不再传场景图**：一张单视角图 + 一段全景视频，
	// 两套空间参考会互相打架（图说房间长这样、视频说另一个样），
	// 模型会挑一个照抄，机位和布局都跟着乱。视频本身就是最完整的空间参考。
	if !hasFirstFrame && sceneRef != "video" {
		for _, s := range board.Scenes {
			if s.ID == "" {
				continue
			}
			if sc, err := h.sceneRepo.GetByID(s.ID); err == nil {
				scenes = append(scenes, frameRefSlot{URL: pickSceneImage(sc, sceneRef), Name: sc.Name, Kind: "scene"})
			}
		}
		sortRefSlotsByName(scenes)
		for _, s := range scenes {
			push(s.URL, s.Name, s.Kind)
		}
		// 正文兜底：场景名在提示词里出现但 scenesJson 没标
		knownScene := map[string]bool{}
		for _, s := range board.Scenes {
			knownScene[s.Name] = true
		}
		for _, sc := range allScenes {
			if knownScene[sc.Name] {
				continue
			}
			if strings.Contains(board.VideoPrompt, "@"+sc.Name) {
				push(sc.ReferenceImage, sc.Name, "scene")
			}
		}
	}

	for _, it := range board.Items {
		if it.ID == "" {
			continue
		}
		if i, err := h.itemRepo.GetByID(it.ID); err == nil {
			items = append(items, frameRefSlot{URL: i.ReferenceImage, Name: i.Name, Kind: "item"})
		}
	}
	sortRefSlotsByName(items)
	for _, it := range items {
		push(it.URL, it.Name, it.Kind)
	}
	// 道具兜底：正文里 @ 到但 itemsJson 没标
	knownItem := map[string]bool{}
	for _, it := range board.Items {
		knownItem[it.Name] = true
	}
	for _, it := range allItems {
		if knownItem[it.Name] {
			continue
		}
		if strings.Contains(board.VideoPrompt, "@"+it.Name) {
			push(it.ReferenceImage, it.Name, "item")
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// sketchSlotName 草图在图位对照表里的名字：说明是几格、按什么顺序看
func (h *GenerateHandler) GenerateFirstFrame(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		EpisodeID string   `json:"episodeId" binding:"required"`
		FrameIDs  []string `json:"frameIds"` // 空 = 整集
		ModelID   string   `json:"modelId"`  // 空 = 自动挑支持参考图的图像模型
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	ep, err := h.episodeRepo.GetByID(req.EpisodeID)
	if err != nil || !h.projectRepo.Exists(ep.ProjectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	frames, err := h.boardRepo.ListByEpisodeID(req.EpisodeID)
	if err != nil || len(frames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "这一集还没有分镜，先生成分镜"})
		return
	}
	if len(req.FrameIDs) > 0 {
		want := make(map[string]bool, len(req.FrameIDs))
		for _, id := range req.FrameIDs {
			want[id] = true
		}
		picked := make([]model.Storyboard, 0, len(req.FrameIDs))
		for _, f := range frames {
			if want[f.ID] {
				picked = append(picked, f)
			}
		}
		if len(picked) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "指定的帧不属于这一集"})
			return
		}
		frames = picked
	}

	aspect := strings.TrimSpace(ep.AspectRatio)
	if aspect == "" {
		aspect = "9:16"
	}
	cost := pointCost(h.settingRepo, "points_image", 2)

	tasks := make([]*model.GenerationTask, 0, len(frames))
	skipped := 0
	for i := range frames {
		f := &frames[i]
		f.FillRefs()

		roles, scenes := h.frameAssets(f)
		// 道具也要给参考图，否则模型会自己编桌面上的东西
		items := make([]model.Item, 0, len(f.Items))
		for _, it := range f.Items {
			if it.ID == "" {
				continue
			}
			if obj, e := h.itemRepo.GetByID(it.ID); e == nil {
				items = append(items, *obj)
			}
		}
		refs := make([]string, 0, len(roles)+len(scenes)+len(items))
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
		if len(refs) > 9 {
			refs = refs[:9]
		}
		// 图生图至少要 2 张参考图，凑不齐就跳过 —— 硬跑只会得到一张跟角色无关的图
		if len(refs) < 2 {
			skipped++
			continue
		}

		params, _ := json.Marshal(map[string]interface{}{
			"refImages": refs,
			"aspect":    aspect,
		})
		task, cerr := h.createTask(userID, "firstframe", "frame", f.ID, req.EpisodeID,
			req.ModelID, service.BuildFirstFramePrompt(f.VideoPrompt, roles, scenes, items), string(params), cost)
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
		msg += fmt.Sprintf("（%d 帧缺角色或场景参考图，已跳过）", skipped)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": msg,
		"data": gin.H{
			"tasks": tasks, "cost": cost * len(tasks),
			"frameCount": len(frames), "skipped": skipped,
		},
	})
}

// frameAssets 取这一帧引用的角色与场景实体（顺序与 Roles/Scenes 一致）
func (h *GenerateHandler) frameAssets(f *model.Storyboard) ([]model.Character, []model.Scene) {
	roles := make([]model.Character, 0, len(f.Roles))
	for _, r := range f.Roles {
		if r.ID == "" {
			continue
		}
		if ch, err := h.charRepo.GetByID(r.ID); err == nil {
			roles = append(roles, *ch)
		}
	}
	scenes := make([]model.Scene, 0, len(f.Scenes))
	for _, s := range f.Scenes {
		if s.ID == "" {
			continue
		}
		if sc, err := h.sceneRepo.GetByID(s.ID); err == nil {
			scenes = append(scenes, *sc)
		}
	}
	return roles, scenes
}

// stripQuery 去掉 ?v=缓存参数：本地图要按路径读盘/上传，带 query 找不到文件
func stripQuery(u string) string {
	if i := strings.Index(u, "?"); i > 0 {
		return u[:i]
	}
	return u
}

// pickSceneImage 按用户选的场景参考类型挑图。
//
// 选全景 / 360 视频时场景位换成柱形全景图 —— 全景图能看到房间四周，
// 比单张正视图更能约束环境；全景图还没生成就退回原场景图，不让生成卡住。
func pickSceneImage(sc *model.Scene, sceneRef string) string {
	if sc == nil {
		return ""
	}
	if sceneRef == "panorama" || sceneRef == "video" {
		if strings.TrimSpace(sc.PanoramaImage) != "" {
			return sc.PanoramaImage
		}
	}
	return sc.ReferenceImage
}

// replaceSceneLine 把地点行整行换成「在参考视频 1 的场景中 + 人物站位」。
//
// 改造前：地点：@教师办公室-白天 左侧靠墙深棕木书架…；中央深棕木纹办公桌…
// 改造后：地点：在参考视频 1 的场景中（@教师办公室-白天） @老师 坐在办公桌后，@学生 站在桌前
//
// 只动「地点：」这一行，其余段落一个字符都不碰。
func replaceSceneLine(prompt, sceneName, opening string) string {
	i := strings.Index(prompt, "地点：")
	if i < 0 {
		return prompt
	}
	lineEnd := strings.Index(prompt[i:], "\n")
	if lineEnd < 0 {
		lineEnd = len(prompt) - i
	}
	// 不写场景名：名字会引导模型脑补（办公室→黑板）
	newLine := "地点：在参考视频 1 的场景中，严格保持场景一致性"
	if o := strings.TrimSpace(opening); o != "" {
		newLine += " " + strings.TrimSuffix(o, "。")
	}
	return prompt[:i] + newLine + prompt[i+lineEnd:]
}

// openingFromText 从人读文案里取 [开场站位] 那一行 —— 这是人物位置的可靠来源。
func openingFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[开场站位]：") {
			return strings.TrimSpace(strings.TrimPrefix(line, "[开场站位]："))
		}
	}
	return ""
}
