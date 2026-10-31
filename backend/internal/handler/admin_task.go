package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/queue"
	"jikeai-clone/internal/repository"
)

// ==================== 任务生成管理 ====================

// AdminTaskHandler 管理端任务监控
type AdminTaskHandler struct {
	taskRepo *repository.TaskRepository
	queue    *queue.Queue
}

func NewAdminTaskHandler(taskRepo *repository.TaskRepository, q *queue.Queue) *AdminTaskHandler {
	return &AdminTaskHandler{taskRepo: taskRepo, queue: q}
}

// List 跨用户任务列表
func (h *AdminTaskHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	f := repository.AdminTaskFilter{
		Status:    c.Query("status"),
		Type:      c.Query("type"),
		UserID:    c.Query("userId"),
		Keyword:   c.Query("keyword"),
		OnlyToday: c.Query("today") == "1",
		Page:      page,
		Size:      size,
	}

	rows, total, err := h.taskRepo.ListAdmin(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	h.fillAssetNames(rows)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"list":  rows,
			"total": total,
			"page":  f.Page,
			"size":  f.Size,
		},
	})
}

// Stats 实时概览（队列 + 今日数据）
func (h *AdminTaskHandler) Stats(c *gin.Context) {
	s := h.taskRepo.Stats()
	qs := h.queue.Stats()
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"task":  s,
			"queue": qs,
		},
	})
}

// Detail 单条任务详情
func (h *AdminTaskHandler) Detail(c *gin.Context) {
	row, err := h.taskRepo.GetByIDAdmin(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "任务不存在"})
		return
	}
	rows := []repository.AdminTaskRow{*row}
	h.fillAssetNames(rows)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": rows[0]})
}

// Retry 重试失败任务
// 说明：失败时积分已自动退还，所以重试不再扣费，属于运营补偿动作。
func (h *AdminTaskHandler) Retry(c *gin.Context) {
	row, err := h.taskRepo.GetByIDAdmin(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "任务不存在"})
		return
	}
	switch row.Status {
	case "failed":
	case "pending", "running":
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "任务还在队列中，不必重试"})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "只能重试失败的任务"})
		return
	}

	if err := h.taskRepo.ResetForRetry(row.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	h.queue.Enqueue(row.ID)

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已重新入队，本次不扣积分"})
}

// fillAssetNames 回填资产名（角色/场景/道具/分镜分表批量查）
func (h *AdminTaskHandler) fillAssetNames(rows []repository.AdminTaskRow) {
	// 按 assetType 分组收集 ID，避免逐行查库
	buckets := map[string][]string{}
	for _, r := range rows {
		if r.AssetID == "" || r.AssetType == "" {
			continue
		}
		buckets[r.AssetType] = append(buckets[r.AssetType], r.AssetID)
	}
	names := map[string]string{}
	for kind, ids := range buckets {
		for id, name := range h.taskRepo.AssetNames(kind, ids) {
			names[kind+"|"+id] = name
		}
	}
	for i := range rows {
		if n, ok := names[rows[i].AssetType+"|"+rows[i].AssetID]; ok {
			rows[i].AssetName = n
		}
	}
}
