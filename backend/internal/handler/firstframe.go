package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
) *FirstFrameHandler {
	return &FirstFrameHandler{
		provider: provider, ffRepo: ffRepo, boardRepo: boardRepo,
		episodeRepo: episodeRepo, projectRepo: projectRepo,
		charRepo: charRepo, sceneRepo: sceneRepo, itemRepo: itemRepo,
		taskRepo: taskRepo, pointsRepo: pointsRepo, settingRepo: settingRepo,
		queue: q,
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

	groups, err := service.PlanFirstFrames(ctx, client, frames)
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
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
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
