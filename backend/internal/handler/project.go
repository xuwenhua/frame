package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/pkg/idgen"
)

// ProjectHandler 项目处理器
type ProjectHandler struct {
	repo *repository.ProjectRepository
}

func NewProjectHandler(repo *repository.ProjectRepository) *ProjectHandler {
	return &ProjectHandler{repo: repo}
}

func (h *ProjectHandler) List(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	page := atoiSafe(c.DefaultQuery("page", "1"), 1)
	size := atoiSafe(c.DefaultQuery("size", "20"), 20)

	list, total, err := h.repo.List(userID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    gin.H{"list": list, "total": total, "page": page, "size": size},
	})
}

func (h *ProjectHandler) Create(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "项目名称不能为空"})
		return
	}

	now := time.Now()
	p := &model.Project{
		ID:          idgen.New(),
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		Status:      "draft",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.repo.Create(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": p})
}

func (h *ProjectHandler) Get(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	p, err := h.repo.GetByID(c.Param("id"), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": p})
}

func (h *ProjectHandler) Update(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	p, err := h.repo.GetByID(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
		CoverURL    string `json:"coverUrl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Description != "" {
		p.Description = req.Description
	}
	if req.Status != "" {
		p.Status = req.Status
	}
	if req.CoverURL != "" {
		p.CoverURL = req.CoverURL
	}
	p.UpdatedAt = time.Now()

	if err := h.repo.Update(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "更新成功", "data": p})
}

func (h *ProjectHandler) Delete(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	if err := h.repo.Delete(c.Param("id"), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func atoiSafe(s string, def int) int {
	n := 0
	has := false
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		has = true
	}
	if !has {
		return def
	}
	return n
}
