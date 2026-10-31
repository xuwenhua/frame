package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/repository"
)

// AccountHandler 个人中心
type AccountHandler struct {
	userRepo   *repository.UserRepository
	pointsRepo *repository.PointsRepository
	statsRepo  *repository.StatsRepository
}

func NewAccountHandler(
	userRepo *repository.UserRepository,
	pointsRepo *repository.PointsRepository,
	statsRepo *repository.StatsRepository,
) *AccountHandler {
	return &AccountHandler{
		userRepo:   userRepo,
		pointsRepo: pointsRepo,
		statsRepo:  statsRepo,
	}
}

// Stats 账户统计（项目数、镜头数、已出图数等）
func (h *AccountHandler) Stats(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	stats, err := h.statsRepo.GetUserStats(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	acc, _ := h.pointsRepo.GetOrCreateAccount(userID)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"stats": stats,
			"points": gin.H{
				"balance":  acc.Balance,
				"totalIn":  acc.TotalIn,
				"totalOut": acc.TotalOut,
			},
		},
	})
}

// UpdateProfile 修改昵称、头像
func (h *AccountHandler) UpdateProfile(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	var req struct {
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	if req.Nickname != "" {
		if len([]rune(req.Nickname)) > 20 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "昵称不能超过 20 个字"})
			return
		}
		user.Nickname = req.Nickname
	}
	if req.Avatar != "" {
		user.Avatar = req.Avatar
	}
	user.UpdatedAt = time.Now()

	if err := h.userRepo.Update(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": user})
}

// ChangePassword 修改密码（改完要求重新登录）
func (h *AccountHandler) ChangePassword(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	var req struct {
		OldPassword string `json:"oldPassword" binding:"required"`
		NewPassword string `json:"newPassword" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "原密码必填，新密码至少 6 位"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "原密码不正确"})
		return
	}

	if req.OldPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "新密码不能与原密码相同"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "密码加密失败"})
		return
	}

	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now()

	if err := h.userRepo.Update(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "密码已修改，请重新登录"})
}

// Overview 个人中心首屏聚合数据
func (h *AccountHandler) Overview(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	stats, _ := h.statsRepo.GetUserStats(userID)
	acc, _ := h.pointsRepo.GetOrCreateAccount(userID)

	// 近期流水（取 5 条）
	txs, _, _ := h.pointsRepo.ListTransactions(userID, 1, 5)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"user":   user,
			"stats":  stats,
			"points": gin.H{
				"balance":  acc.Balance,
				"totalIn":  acc.TotalIn,
				"totalOut": acc.TotalOut,
			},
			"recentTransactions": txs,
		},
	})
}
