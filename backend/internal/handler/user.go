package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/pkg/auth"
	"jikeai-clone/pkg/idgen"
)

type UserHandler struct {
	userRepo   *repository.UserRepository
	pointsRepo *repository.PointsRepository
	settings   *repository.SettingRepository
	jwt        *auth.Manager
}

func NewUserHandler(
	userRepo *repository.UserRepository,
	pointsRepo *repository.PointsRepository,
	settings *repository.SettingRepository,
	jwtMgr *auth.Manager,
) *UserHandler {
	return &UserHandler{
		userRepo:   userRepo,
		pointsRepo: pointsRepo,
		settings:   settings,
		jwt:        jwtMgr,
	}
}

// Register 注册
func (h *UserHandler) Register(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "邮箱或密码格式不正确（密码至少 6 位）"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if h.userRepo.EmailExists(email) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "该邮箱已注册"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "密码加密失败"})
		return
	}

	nickname := strings.TrimSpace(req.Nickname)
	if nickname == "" {
		nickname = strings.Split(email, "@")[0]
	}

	now := time.Now()
	user := &model.User{
		ID:           idgen.New(),
		Email:        email,
		PasswordHash: string(hash),
		Nickname:     nickname,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.userRepo.Create(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "注册失败"})
		return
	}

	// 创建积分账户
	_, _ = h.pointsRepo.GetOrCreateAccount(user.ID)

	// 注册赠送积分（读管理端设置，默认 100）
	bonus := 100
	if v, err := h.settings.Get("points_register_bonus"); err == nil {
		if n := parseInt(v); n > 0 {
			bonus = n
		}
	}
	if bonus > 0 {
		_, _ = h.pointsRepo.Grant(user.ID, bonus, "register", "注册赠送", user.ID, "user")
	}

	token, expAt, err := h.jwt.Generate(user.ID, user.Email, "user")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "登录凭证生成失败"})
		return
	}

	balance, _ := h.pointsRepo.Balance(user.ID)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "注册成功",
		"data": gin.H{
			"token":     token,
			"expiresAt": expAt,
			"user":      user,
			"points":    balance,
		},
	})
}

// Login 登录
func (h *UserHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请填写邮箱和密码"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	user, err := h.userRepo.GetByEmail(email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "邮箱或密码错误"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "邮箱或密码错误"})
		return
	}

	if user.Status == "banned" {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "账号已被封禁"})
		return
	}

	_ = h.userRepo.TouchLogin(user.ID)

	// 管理员账号识别（目前用邮箱白名单，后续可加 role 字段）
	role := "user"
	if user.Email == "admin@frame.local" {
		role = "admin"
	}

	token, expAt, err := h.jwt.Generate(user.ID, user.Email, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "登录凭证生成失败"})
		return
	}

	balance, _ := h.pointsRepo.Balance(user.ID)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "登录成功",
		"data": gin.H{
			"token":     token,
			"expiresAt": expAt,
			"user":      user,
			"points":    balance,
		},
	})
}

// Profile 当前用户信息
func (h *UserHandler) Profile(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	balance, _ := h.pointsRepo.Balance(userID)
	acc, _ := h.pointsRepo.GetOrCreateAccount(userID)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"user":   user,
			"points": balance,
			"account": gin.H{
				"balance":  acc.Balance,
				"totalIn":  acc.TotalIn,
				"totalOut": acc.TotalOut,
			},
		},
	})
}

// PointsBalance 积分余额
func (h *UserHandler) PointsBalance(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	acc, err := h.pointsRepo.GetOrCreateAccount(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"balance":  acc.Balance,
			"totalIn":  acc.TotalIn,
			"totalOut": acc.TotalOut,
		},
	})
}

// PointsTransactions 积分流水
func (h *UserHandler) PointsTransactions(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	page := parseInt(c.DefaultQuery("page", "1"))
	size := parseInt(c.DefaultQuery("size", "20"))

	list, total, err := h.pointsRepo.ListTransactions(userID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"list":  list,
			"total": total,
			"page":  page,
			"size":  size,
		},
	})
}

func parseInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
