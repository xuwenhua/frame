package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
	"jikeai-clone/pkg/idgen"
)

// pointCost 从系统设置读扣费标准
func pointCost(settingRepo *repository.SettingRepository, key string, def int) int {
	v, err := settingRepo.Get(key)
	if err != nil {
		return def
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

type IdentifyHandler struct {
	charRepo    *repository.CharacterRepository
	sceneRepo   *repository.SceneRepository
	itemRepo    *repository.ItemRepository
	episodeRepo *repository.EpisodeRepository
	projectRepo *repository.ProjectRepository
	pointsRepo  *repository.PointsRepository
	settingRepo *repository.SettingRepository
	provider    *service.ModelProvider
	ruleSvc     *service.IdentifyService
}

func NewIdentifyHandler(
	charRepo *repository.CharacterRepository,
	sceneRepo *repository.SceneRepository,
	itemRepo *repository.ItemRepository,
	episodeRepo *repository.EpisodeRepository,
	projectRepo *repository.ProjectRepository,
	pointsRepo *repository.PointsRepository,
	settingRepo *repository.SettingRepository,
	provider *service.ModelProvider,
) *IdentifyHandler {
	return &IdentifyHandler{
		charRepo:    charRepo,
		sceneRepo:   sceneRepo,
		itemRepo:    itemRepo,
		episodeRepo: episodeRepo,
		projectRepo: projectRepo,
		pointsRepo:  pointsRepo,
		settingRepo: settingRepo,
		provider:    provider,
		ruleSvc:     service.NewIdentifyService(),
	}
}

// All 一次性识别三类资产（扣积分 → 调 LLM → 落库）
func (h *IdentifyHandler) All(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		EpisodeID string `json:"episodeId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	ep, err := h.ownedEpisode(req.EpisodeID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	if ep.ScriptText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "剧本为空，请先填写"})
		return
	}

	// 1. 先扣积分（失败则整单不执行）
	cost := pointCost(h.settingRepo, "points_identify", 14)
	if _, err := h.pointsRepo.Consume(userID, cost, "identify", "资产识别", req.EpisodeID, "episode"); err != nil {
		if errors.Is(err, repository.ErrInsufficientPoints) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code": 402, "message": "积分不足，本次需要 " + itoa(cost) + " 积分",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	// 2. 识别（LLM 优先，失败降级规则引擎）
	client, cfg, modelErr := h.provider.LLM()
	if modelErr != nil {
		client = nil
	}

	// 输出上限从模型配置读（默认 8192），避免写死导致长剧本 JSON 被截断
	maxTokens := 8192
	if cfg != nil && cfg.Params != "" {
		var p struct {
			MaxTokens int `json:"maxTokens"`
		}
		if json.Unmarshal([]byte(cfg.Params), &p) == nil && p.MaxTokens > 0 {
			maxTokens = p.MaxTokens
		}
	}

	svc := service.NewLLMIdentifyService(client, maxTokens)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	roleDrafts, sceneDrafts, itemDrafts, llmErr := svc.Identify(ctx, ep.ScriptText)

	now := time.Now()

	// 3. 落库（先删后插，用事务保证一致性）
	_ = h.charRepo.DeleteByEpisodeID(req.EpisodeID)
	_ = h.sceneRepo.DeleteByEpisodeID(req.EpisodeID)
	_ = h.itemRepo.DeleteByEpisodeID(req.EpisodeID)

	chars := make([]model.Character, 0, len(roleDrafts))
	for _, d := range roleDrafts {
		voice := d.VoiceTone
		if voice == "" {
			// 规则引擎路径没有音色，按身份兜一版
			voice = service.SuggestVoiceTone(d.IdentityAnchor, d.Personality)
		}
		chars = append(chars, model.Character{
			ID: idgen.New(), EpisodeID: req.EpisodeID, Name: d.Name,
			IdentityAnchor: d.IdentityAnchor, CostumePrompt: d.CostumePrompt,
			Personality: d.Personality, AppearanceCues: d.AppearanceCues,
			ImagePrompt: d.ImagePrompt,
			VoiceTone:   voice,
			CreatedAt:   now, UpdatedAt: now,
		})
	}
	scenes := make([]model.Scene, 0, len(sceneDrafts))
	for _, d := range sceneDrafts {
		scenes = append(scenes, model.Scene{
			ID: idgen.New(), EpisodeID: req.EpisodeID, Name: d.Name,
			SpatialAnchor: d.SpatialAnchor, LightingPrompt: d.LightingPrompt,
			TimeOfDay: d.TimeOfDay, ImagePrompt: d.ImagePrompt,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	items := make([]model.Item, 0, len(itemDrafts))
	for _, d := range itemDrafts {
		items = append(items, model.Item{
			ID: idgen.New(), EpisodeID: req.EpisodeID, Name: d.Name,
			Category: d.Category, Prompt: d.Prompt, ImagePrompt: d.ImagePrompt,
			CreatedAt: now, UpdatedAt: now,
		})
	}

	if len(chars) > 0 {
		if err := h.charRepo.CreateBatch(chars); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
	}
	if len(scenes) > 0 {
		if err := h.sceneRepo.CreateBatch(scenes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
	}
	if len(items) > 0 {
		if err := h.itemRepo.CreateBatch(items); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
	}

	if ep.Status == "script" {
		ep.Status = "identified"
		ep.UpdatedAt = now
		_ = h.episodeRepo.Update(ep)
	}

	balance, _ := h.pointsRepo.Balance(userID)

	resp := gin.H{
		"characters": chars,
		"scenes":     scenes,
		"items":      items,
		"cost":       cost,
		"balance":    balance,
		"engine":     "rule",
	}
	if client != nil && llmErr == nil {
		resp["engine"] = "llm"
		resp["model"] = service.Describe(cfg)
	}
	if llmErr != nil {
		resp["warning"] = llmErr.Error()
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "识别完成", "data": resp})
}

// List 拉取已存资产
func (h *IdentifyHandler) List(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	episodeID := c.Query("episodeId")
	if episodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	if _, err := h.ownedEpisode(episodeID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	chars, _ := h.charRepo.ListByEpisodeID(episodeID)
	scenes, _ := h.sceneRepo.ListByEpisodeID(episodeID)
	items, _ := h.itemRepo.ListByEpisodeID(episodeID)

	// 老数据在识别阶段把「三视图 / 空镜」写进了 image_prompt，
	// 读出来先剥掉，保证前端看到、编辑的都是通用描述（视图要求提交时才拼）
	for i := range chars {
		chars[i].ImagePrompt = service.StripFraming(chars[i].ImagePrompt, "character")
	}
	for i := range scenes {
		scenes[i].ImagePrompt = service.StripFraming(scenes[i].ImagePrompt, "scene")
	}
	for i := range items {
		items[i].ImagePrompt = service.StripFraming(items[i].ImagePrompt, "item")
	}

	// 把剧本探测出的视觉约束一并返回：前端据此显示画幅（竖版/横版）与「不露正脸」提示，
	// 避免前端各自再实现一套 detectStyle 逻辑
	style := service.DefaultStyle()
	if ep, err := h.episodeRepo.GetByID(episodeID); err == nil && ep.ScriptText != "" {
		style = service.DetectStyle(ep.ScriptText)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"characters": chars,
			"scenes":     scenes,
			"items":      items,
			"style": gin.H{
				"vertical": style.Vertical,
				"noFace":   style.NoFace,
				"desc":     style.Desc,
			},
		},
	})
}

// assetPatch 「编辑资产」的可改字段
//
// 全部用指针：只有请求里出现的字段才会被覆盖，
// 避免「只改音色」的请求把提示词清空（或反过来）。
type assetPatch struct {
	Name           *string `json:"name"`
	ImagePrompt    *string `json:"imagePrompt"`
	ReferenceImage *string `json:"referenceImage"`
	PadImage       *string `json:"padImage"`
	// 角色专属：这几个以前漏了，导致「改了年龄却只改了出图提示词」——
	// 视频描述词读的是 identityAnchor，两边不一致
	IdentityAnchor *string `json:"identityAnchor"`
	CostumePrompt  *string `json:"costumePrompt"`
	Personality    *string `json:"personality"`
	AppearanceCues *string `json:"appearanceCues"`
	VoiceTone      *string `json:"voiceTone"`
	VoiceSample    *string `json:"voiceSample"`
}

// UpdateAsset 编辑单个资产：名称 / 出图提示词 / 参考图 / 音色 / 参考音频
func (h *IdentifyHandler) UpdateAsset(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	assetType := c.Param("type")
	id := c.Param("id")

	var req assetPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	// 名称不能改成空串，否则列表里会变成空白行
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "名称不能为空"})
		return
	}
	// 参考图只允许本站路径或 http(s)，防止存进奇怪的 scheme
	if req.ReferenceImage != nil {
		if err := checkMediaURL(*req.ReferenceImage); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
			return
		}
	}
	if req.VoiceSample != nil {
		if err := checkMediaURL(*req.VoiceSample); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
			return
		}
	}
	if req.PadImage != nil {
		if err := checkMediaURL(*req.PadImage); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
			return
		}
	}

	now := time.Now()

	switch assetType {
	case "character":
		ch, err := h.charRepo.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "角色不存在"})
			return
		}
		if _, err := h.ownedEpisode(ch.EpisodeID, userID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "角色不存在"})
			return
		}
		if req.Name != nil {
			ch.Name = strings.TrimSpace(*req.Name)
		}
		if req.ImagePrompt != nil {
			ch.ImagePrompt = strings.TrimSpace(*req.ImagePrompt)
		}
		if req.ReferenceImage != nil {
			ch.ReferenceImage = *req.ReferenceImage
			if *req.ReferenceImage != "" {
				ch.ReferenceNote = "uploaded"
			}
		}
		if req.IdentityAnchor != nil {
			ch.IdentityAnchor = strings.TrimSpace(*req.IdentityAnchor)
		}
		if req.CostumePrompt != nil {
			ch.CostumePrompt = strings.TrimSpace(*req.CostumePrompt)
		}
		if req.Personality != nil {
			ch.Personality = strings.TrimSpace(*req.Personality)
		}
		if req.AppearanceCues != nil {
			ch.AppearanceCues = strings.TrimSpace(*req.AppearanceCues)
		}
		if req.VoiceTone != nil {
			ch.VoiceTone = strings.TrimSpace(*req.VoiceTone)
		}
		if req.VoiceSample != nil {
			ch.VoiceSample = *req.VoiceSample
		}
		if req.PadImage != nil {
			ch.PadImage = *req.PadImage
		}
		ch.UpdatedAt = now
		if err := h.charRepo.Update(ch); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": ch})

	case "scene":
		sc, err := h.sceneRepo.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "场景不存在"})
			return
		}
		if _, err := h.ownedEpisode(sc.EpisodeID, userID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "场景不存在"})
			return
		}
		if req.Name != nil {
			sc.Name = strings.TrimSpace(*req.Name)
		}
		if req.ImagePrompt != nil {
			sc.ImagePrompt = strings.TrimSpace(*req.ImagePrompt)
		}
		if req.ReferenceImage != nil {
			sc.ReferenceImage = *req.ReferenceImage
		}
		if req.PadImage != nil {
			sc.PadImage = *req.PadImage
		}
		sc.UpdatedAt = now
		if err := h.sceneRepo.Update(sc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": sc})

	case "item":
		it, err := h.itemRepo.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "道具不存在"})
			return
		}
		if _, err := h.ownedEpisode(it.EpisodeID, userID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "道具不存在"})
			return
		}
		if req.Name != nil {
			it.Name = strings.TrimSpace(*req.Name)
		}
		if req.ImagePrompt != nil {
			it.ImagePrompt = strings.TrimSpace(*req.ImagePrompt)
		}
		if req.ReferenceImage != nil {
			it.ReferenceImage = *req.ReferenceImage
		}
		if req.PadImage != nil {
			it.PadImage = *req.PadImage
		}
		it.UpdatedAt = now
		if err := h.itemRepo.Update(it); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": it})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "type 只能是 character / scene / item"})
	}
}

// DeleteAsset 删除资产（角色 / 场景 / 道具）
//
// DELETE /api/assets/:type/:id
// 软删除（GORM DeletedAt），分镜里的 @引用不动 —— 重新生成分镜时自然消失。
func (h *IdentifyHandler) DeleteAsset(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	assetType := c.Param("type")
	id := c.Param("id")

	switch assetType {
	case "character":
		ch, err := h.charRepo.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "角色不存在"})
			return
		}
		if _, err := h.ownedEpisode(ch.EpisodeID, userID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "角色不存在"})
			return
		}
		if err := h.charRepo.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})

	case "scene":
		sc, err := h.sceneRepo.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "场景不存在"})
			return
		}
		if _, err := h.ownedEpisode(sc.EpisodeID, userID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "场景不存在"})
			return
		}
		if err := h.sceneRepo.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})

	case "item":
		it, err := h.itemRepo.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "道具不存在"})
			return
		}
		if _, err := h.ownedEpisode(it.EpisodeID, userID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "道具不存在"})
			return
		}
		if err := h.itemRepo.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "type 只能是 character / scene / item"})
	}
}

// checkMediaURL 只接受空值、本站静态路径、http(s) 绝对地址
func checkMediaURL(u string) error {
	u = strings.TrimSpace(u)
	if u == "" {
		return nil
	}
	if strings.HasPrefix(u, "/static/") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return nil
	}
	return errBadURL
}

func (h *IdentifyHandler) ownedEpisode(episodeID, userID string) (*model.Episode, error) {
	ep, err := h.episodeRepo.GetByID(episodeID)
	if err != nil {
		return nil, err
	}
	if !h.projectRepo.Exists(ep.ProjectID, userID) {
		return nil, errNotFound
	}
	return ep, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [12]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
