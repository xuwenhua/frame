package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
)

// ==================== 用户管理 ====================

// AdminUserHandler 管理端用户管理
type AdminUserHandler struct {
	userRepo    *repository.UserRepository
	pointsRepo  *repository.PointsRepository
	projectRepo *repository.ProjectRepository
}

func NewAdminUserHandler(
	userRepo *repository.UserRepository,
	pointsRepo *repository.PointsRepository,
	projectRepo *repository.ProjectRepository,
) *AdminUserHandler {
	return &AdminUserHandler{userRepo: userRepo, pointsRepo: pointsRepo, projectRepo: projectRepo}
}

// adminUserRow 列表行（用户 + 积分 + 项目数）
type adminUserRow struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Nickname     string     `json:"nickname"`
	Avatar       string     `json:"avatar"`
	Status       string     `json:"status"`
	Balance      int        `json:"balance"`
	TotalIn      int        `json:"totalIn"`
	TotalOut     int        `json:"totalOut"`
	ProjectCount int64      `json:"projectCount"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// List 用户列表
func (h *AdminUserHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	keyword := c.Query("keyword")
	status := c.Query("status")

	users, total, err := h.userRepo.ListAdmin(page, size, keyword, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	rows := make([]adminUserRow, 0, len(users))
	for _, u := range users {
		row := adminUserRow{
			ID: u.ID, Email: u.Email, Nickname: u.Nickname, Avatar: u.Avatar,
			Status: u.Status, LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
		}
		// 余额拿不到就按 0 显示，不阻断列表
		if acc, e := h.pointsRepo.GetOrCreateAccount(u.ID); e == nil {
			row.Balance, row.TotalIn, row.TotalOut = acc.Balance, acc.TotalIn, acc.TotalOut
		}
		if n, e := h.projectRepo.CountByUser(u.ID); e == nil {
			row.ProjectCount = n
		}
		rows = append(rows, row)
	}

	stats, _ := h.userRepo.Stats()
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "success",
		"data": gin.H{"list": rows, "total": total, "page": page, "size": size, "stats": stats},
	})
}

// Detail 用户详情（含最近积分流水与项目）
func (h *AdminUserHandler) Detail(c *gin.Context) {
	id := c.Param("id")
	u, err := h.userRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	acc, _ := h.pointsRepo.GetOrCreateAccount(u.ID)
	txs, _, _ := h.pointsRepo.ListTransactions(u.ID, 1, 8)
	projects, projectTotal, _ := h.projectRepo.List(u.ID, 1, 5)

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "success",
		"data": gin.H{
			"user":         u,
			"points":       acc,
			"transactions": txs,
			"projects":     projects,
			"projectTotal": projectTotal,
		},
	})
}

// AdjustPoints 调整积分（正数加、负数扣）
func (h *AdminUserHandler) AdjustPoints(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Amount int    `json:"amount" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请填写非 0 的积分数值"})
		return
	}
	if req.Amount > 100000 || req.Amount < -100000 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "单次调整不能超过 10 万积分"})
		return
	}

	u, err := h.userRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}
	// 确保账户存在
	if _, err := h.pointsRepo.GetOrCreateAccount(u.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	reason := req.Reason
	if reason == "" {
		if req.Amount > 0 {
			reason = "后台赠送"
		} else {
			reason = "后台扣减"
		}
	}
	txType := "admin_add"
	if req.Amount < 0 {
		txType = "admin_deduct"
	}

	var acc *model.PointsAccount
	if req.Amount > 0 {
		acc, err = h.pointsRepo.Grant(u.ID, req.Amount, txType, reason, "", "admin")
	} else {
		acc, err = h.pointsRepo.Consume(u.ID, -req.Amount, txType, reason, "", "admin")
	}
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientPoints) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "扣减后余额为负，操作已取消"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "已调整",
		"data": gin.H{"balance": acc.Balance, "amount": req.Amount},
	})
}

// SetStatus 封禁 / 解封
func (h *AdminUserHandler) SetStatus(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"` // active / banned
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 status"})
		return
	}
	if req.Status != "active" && req.Status != "banned" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "status 只能是 active 或 banned"})
		return
	}

	u, err := h.userRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}
	// 不允许封禁默认管理员
	if u.Email == "admin@frame.local" && req.Status == "banned" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "不能封禁默认管理员账号"})
		return
	}

	if err := h.userRepo.SetStatus(u.ID, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	msg := "已解封"
	if req.Status == "banned" {
		msg = "已封禁"
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": msg})
}
