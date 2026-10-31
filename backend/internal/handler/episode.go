package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/pkg/idgen"
)

type EpisodeHandler struct {
	repo        *repository.EpisodeRepository
	projectRepo *repository.ProjectRepository
}

func NewEpisodeHandler(repo *repository.EpisodeRepository, projectRepo *repository.ProjectRepository) *EpisodeHandler {
	return &EpisodeHandler{repo: repo, projectRepo: projectRepo}
}

// List 按项目拉剧集（校验项目归属）
func (h *EpisodeHandler) List(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	projectID := c.Query("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 projectId"})
		return
	}
	if !h.projectRepo.Exists(projectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	episodes, err := h.repo.ListByProjectID(projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": episodes})
}

func (h *EpisodeHandler) Create(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		ProjectID     string `json:"projectId" binding:"required"`
		EpisodeNumber int    `json:"episodeNumber"`
		Title         string `json:"title"`
		ScriptText    string `json:"scriptText"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 projectId"})
		return
	}
	if !h.projectRepo.Exists(req.ProjectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	now := time.Now()
	e := &model.Episode{
		ID:            idgen.New(),
		ProjectID:     req.ProjectID,
		EpisodeNumber: req.EpisodeNumber,
		Title:         req.Title,
		ScriptText:    req.ScriptText,
		Status:        "script",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := h.repo.Create(e); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": e})
}

// Get 获取剧集（通过所属项目校验归属）
func (h *EpisodeHandler) Get(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	e, err := h.ownedEpisode(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": e})
}

func (h *EpisodeHandler) UpdateScript(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	e, err := h.ownedEpisode(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	var req struct {
		ScriptText string `json:"scriptText"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	e.ScriptText = req.ScriptText
	e.UpdatedAt = time.Now()
	if err := h.repo.Update(e); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "剧本已更新", "data": e})
}

// UpdateConfig 保存这一集的出片配置（画幅 / 默认视频模型）
// 只传哪个字段就改哪个，避免前端一次改动把另一个字段冲掉。
func (h *EpisodeHandler) UpdateConfig(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	e, err := h.ownedEpisode(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	var req struct {
		AspectRatio *string `json:"aspectRatio"`
		VideoModel  *string `json:"videoModel"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	if req.AspectRatio != nil {
		v := strings.TrimSpace(*req.AspectRatio)
		// 只接受上游支持的两种画幅，其余忽略，避免脏值写进库
		if v == "16:9" || v == "9:16" {
			e.AspectRatio = v
		}
	}
	if req.VideoModel != nil {
		e.VideoModel = strings.TrimSpace(*req.VideoModel)
	}

	e.UpdatedAt = time.Now()
	if err := h.repo.Update(e); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": e})
}

// ownedEpisode 取剧集并校验其所属项目属于该用户
func (h *EpisodeHandler) ownedEpisode(episodeID, userID string) (*model.Episode, error) {
	e, err := h.repo.GetByID(episodeID)
	if err != nil {
		return nil, err
	}
	if !h.projectRepo.Exists(e.ProjectID, userID) {
		return nil, errNotFound
	}
	return e, nil
}
