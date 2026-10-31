package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/idgen"
)

// LibAsset 资产库统一返回结构（角色/场景/物品共用，前端一张卡片一套渲染）
type LibAsset struct {
	ID             string `json:"id"`
	EpisodeID      string `json:"episodeId"`
	Name           string `json:"name"`
	ImagePrompt    string `json:"imagePrompt"`
	ReferenceImage string `json:"referenceImage"`
	VoiceTone      string `json:"voiceTone,omitempty"`
	VoiceSample    string `json:"voiceSample,omitempty"`
	Category       string `json:"category,omitempty"`
	TimeOfDay      string `json:"timeOfDay,omitempty"`
	// Added 仅总库用：该项目里是否已有同名资产
	Added bool `json:"added,omitempty"`
}

// uniqueByName 同名只留一条（列表已按 updated_at desc 排序，保留最新的那个）
func uniqueByName[T any](in []T, key func(T) string) []T {
	seen := make(map[string]bool, len(in))
	out := make([]T, 0, len(in))
	for _, v := range in {
		k := strings.TrimSpace(key(v))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}

func charsToLib(list []model.Character) []LibAsset {
	out := make([]LibAsset, 0, len(list))
	for _, c := range list {
		out = append(out, LibAsset{
			ID: c.ID, EpisodeID: c.EpisodeID, Name: c.Name,
			ImagePrompt: c.ImagePrompt, ReferenceImage: c.ReferenceImage,
			VoiceTone: c.VoiceTone, VoiceSample: c.VoiceSample,
		})
	}
	return out
}

func scenesToLib(list []model.Scene) []LibAsset {
	out := make([]LibAsset, 0, len(list))
	for _, s := range list {
		out = append(out, LibAsset{
			ID: s.ID, EpisodeID: s.EpisodeID, Name: s.Name,
			ImagePrompt: s.ImagePrompt, ReferenceImage: s.ReferenceImage,
			TimeOfDay: s.TimeOfDay,
		})
	}
	return out
}

func itemsToLib(list []model.Item) []LibAsset {
	out := make([]LibAsset, 0, len(list))
	for _, it := range list {
		out = append(out, LibAsset{
			ID: it.ID, EpisodeID: it.EpisodeID, Name: it.Name,
			ImagePrompt: it.ImagePrompt, ReferenceImage: it.ReferenceImage,
			Category: it.Category,
		})
	}
	return out
}

// ProjectAssets 项目资产库：项目下所有剧集的角色 / 场景 / 物品（同名去重）
//
// GET /api/projects/:id/assets
func (h *IdentifyHandler) ProjectAssets(c *gin.Context) {
	projectID := c.Param("id")
	userID := middleware.CurrentUserID(c)
	if !h.projectRepo.Exists(projectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	chars, _ := h.charRepo.ListByProjectID(projectID)
	scenes, _ := h.sceneRepo.ListByProjectID(projectID)
	items, _ := h.itemRepo.ListByProjectID(projectID)

	chars = uniqueByName(chars, func(x model.Character) string { return x.Name })
	scenes = uniqueByName(scenes, func(x model.Scene) string { return x.Name })
	items = uniqueByName(items, func(x model.Item) string { return x.Name })

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{
		"characters": charsToLib(chars),
		"scenes":     scenesToLib(scenes),
		"items":      itemsToLib(items),
	}})
}

// targetEpisode 新建资产挂到哪一集：优先 body 指定，否则项目第一集；都没有就建一集
func (h *IdentifyHandler) targetEpisode(projectID, want string) (*model.Episode, error) {
	eps, err := h.episodeRepo.ListByProjectID(projectID)
	if err != nil {
		return nil, err
	}
	if want != "" {
		for i := range eps {
			if eps[i].ID == want {
				return &eps[i], nil
			}
		}
	}
	if len(eps) > 0 {
		return &eps[0], nil
	}
	// 项目还没有剧集：建一个默认集，否则资产无处安放
	ep := &model.Episode{
		ID: idgen.New(), ProjectID: projectID, EpisodeNumber: 1,
		Title: "第一集", Status: "script", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := h.episodeRepo.Create(ep); err != nil {
		return nil, err
	}
	return ep, nil
}

// CreateProjectAsset 在项目库里新建资产
//
// POST /api/projects/:id/assets/:type   body: { episodeId?, name, imagePrompt?, referenceImage?, ... }
func (h *IdentifyHandler) CreateProjectAsset(c *gin.Context) {
	projectID := c.Param("id")
	kind := c.Param("type")
	userID := middleware.CurrentUserID(c)
	if !h.projectRepo.Exists(projectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	var req struct {
		EpisodeID      string `json:"episodeId"`
		Name           string `json:"name"`
		ImagePrompt    string `json:"imagePrompt"`
		ReferenceImage string `json:"referenceImage"`
		VoiceTone      string `json:"voiceTone"`
		VoiceSample    string `json:"voiceSample"`
		Category       string `json:"category"`
		TimeOfDay      string `json:"timeOfDay"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "资产名称不能为空"})
		return
	}
	name := strings.TrimSpace(req.Name)

	ep, err := h.targetEpisode(projectID, req.EpisodeID)
	if err != nil || ep == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "找不到可挂载的剧集"})
		return
	}

	now := time.Now()
	switch kind {
	case "character":
		obj := &model.Character{
			ID: idgen.New(), EpisodeID: ep.ID, Name: name,
			ImagePrompt: req.ImagePrompt, ReferenceImage: req.ReferenceImage,
			VoiceTone: req.VoiceTone, VoiceSample: req.VoiceSample,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := h.charRepo.CreateBatch([]model.Character{*obj}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已添加", "data": charsToLib([]model.Character{*obj})[0]})
	case "scene":
		obj := &model.Scene{
			ID: idgen.New(), EpisodeID: ep.ID, Name: name,
			ImagePrompt: req.ImagePrompt, ReferenceImage: req.ReferenceImage,
			TimeOfDay: req.TimeOfDay, CreatedAt: now, UpdatedAt: now,
		}
		if err := h.sceneRepo.CreateBatch([]model.Scene{*obj}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已添加", "data": scenesToLib([]model.Scene{*obj})[0]})
	case "item":
		obj := &model.Item{
			ID: idgen.New(), EpisodeID: ep.ID, Name: name,
			ImagePrompt: req.ImagePrompt, ReferenceImage: req.ReferenceImage,
			Category: req.Category, CreatedAt: now, UpdatedAt: now,
		}
		if err := h.itemRepo.CreateBatch([]model.Item{*obj}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已添加", "data": itemsToLib([]model.Item{*obj})[0]})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "资产类型只支持 character / scene / item"})
	}
}

// ImportProjectAsset 从总库把某个资产复制进当前项目
//
// POST /api/projects/:id/assets/:type/import   body: { assetId, episodeId? }
func (h *IdentifyHandler) ImportProjectAsset(c *gin.Context) {
	projectID := c.Param("id")
	kind := c.Param("type")
	userID := middleware.CurrentUserID(c)
	if !h.projectRepo.Exists(projectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	var req struct {
		AssetID   string `json:"assetId"`
		EpisodeID string `json:"episodeId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AssetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 assetId"})
		return
	}

	ep, err := h.targetEpisode(projectID, req.EpisodeID)
	if err != nil || ep == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "找不到可挂载的剧集"})
		return
	}
	now := time.Now()

	switch kind {
	case "character":
		src, err := h.charRepo.GetByID(req.AssetID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "源角色不存在"})
			return
		}
		obj := *src
		obj.ID = idgen.New()
		obj.EpisodeID = ep.ID
		obj.CreatedAt, obj.UpdatedAt = now, now
		if err := h.charRepo.CreateBatch([]model.Character{obj}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已添加到项目", "data": charsToLib([]model.Character{obj})[0]})
	case "scene":
		src, err := h.sceneRepo.GetByID(req.AssetID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "源场景不存在"})
			return
		}
		obj := *src
		obj.ID = idgen.New()
		obj.EpisodeID = ep.ID
		obj.CreatedAt, obj.UpdatedAt = now, now
		if err := h.sceneRepo.CreateBatch([]model.Scene{obj}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已添加到项目", "data": scenesToLib([]model.Scene{obj})[0]})
	case "item":
		src, err := h.itemRepo.GetByID(req.AssetID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "源物品不存在"})
			return
		}
		obj := *src
		obj.ID = idgen.New()
		obj.EpisodeID = ep.ID
		obj.CreatedAt, obj.UpdatedAt = now, now
		if err := h.itemRepo.CreateBatch([]model.Item{obj}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已添加到项目", "data": itemsToLib([]model.Item{obj})[0]})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "资产类型只支持 character / scene / item"})
	}
}

// GlobalLibrary 总角色场景库：该用户所有项目里的资产，标记当前项目是否已有同名
//
// GET /api/library/assets?projectId=xxx
func (h *IdentifyHandler) GlobalLibrary(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	projectID := c.Query("projectId")

	chars, _ := h.charRepo.ListByUserID(userID)
	scenes, _ := h.sceneRepo.ListByUserID(userID)
	items, _ := h.itemRepo.ListByUserID(userID)

	chars = uniqueByName(chars, func(x model.Character) string { return x.Name })
	scenes = uniqueByName(scenes, func(x model.Scene) string { return x.Name })
	items = uniqueByName(items, func(x model.Item) string { return x.Name })

	// 当前项目已有的名字，用于「已添加 / 添加」按钮
	haveChars := map[string]bool{}
	haveScenes := map[string]bool{}
	haveItems := map[string]bool{}
	if projectID != "" && h.projectRepo.Exists(projectID, userID) {
		if list, err := h.charRepo.ListByProjectID(projectID); err == nil {
			for _, x := range list {
				haveChars[x.Name] = true
			}
		}
		if list, err := h.sceneRepo.ListByProjectID(projectID); err == nil {
			for _, x := range list {
				haveScenes[x.Name] = true
			}
		}
		if list, err := h.itemRepo.ListByProjectID(projectID); err == nil {
			for _, x := range list {
				haveItems[x.Name] = true
			}
		}
	}

	outChars := charsToLib(chars)
	for i := range outChars {
		outChars[i].Added = haveChars[outChars[i].Name]
	}
	outScenes := scenesToLib(scenes)
	for i := range outScenes {
		outScenes[i].Added = haveScenes[outScenes[i].Name]
	}
	outItems := itemsToLib(items)
	for i := range outItems {
		outItems[i].Added = haveItems[outItems[i].Name]
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{
		"characters": outChars,
		"scenes":     outScenes,
		"items":      outItems,
	}})
}
