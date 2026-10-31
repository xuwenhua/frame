package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"jikeai-clone/pkg/auth"
)

const (
	CtxUserID = "ctx_user_id"
	CtxEmail  = "ctx_email"
	CtxRole   = "ctx_role"
)

// Auth 强制鉴权：没有有效 token 直接 401
func Auth(m *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 401, "message": "未登录",
			})
			return
		}

		claims, err := m.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 401, "message": "登录已失效，请重新登录",
			})
			return
		}

		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxEmail, claims.Email)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}

// AuthOptional 可选鉴权：有 token 就解析，没有也放行
func AuthOptional(m *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := extractToken(c); token != "" {
			if claims, err := m.Parse(token); err == nil {
				c.Set(CtxUserID, claims.UserID)
				c.Set(CtxEmail, claims.Email)
				c.Set(CtxRole, claims.Role)
			}
		}
		c.Next()
	}
}

// RequireAdmin 需要管理员角色（须配合 Auth 使用）
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(CtxRole)
		if r, ok := role.(string); !ok || r != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": 403, "message": "需要管理员权限",
			})
			return
		}
		c.Next()
	}
}

// StatusChecker 用户状态查询（避免 middleware 直接依赖 repository）
type StatusChecker interface {
	GetStatus(userID string) (string, error)
}

// RequireActiveUser 已封禁用户即使 token 未过期也拒绝访问
// 须配合 Auth 使用（放在 Auth 之后）
func RequireActiveUser(checker StatusChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := CurrentUserID(c)
		if uid == "" {
			c.Next()
			return
		}
		status, err := checker.GetStatus(uid)
		if err == nil && status == "banned" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": 403, "message": "账号已被封禁",
			})
			return
		}
		c.Next()
	}
}

func extractToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(h)
}

// CurrentUserID 从上下文取当前用户 ID
func CurrentUserID(c *gin.Context) string {
	v, ok := c.Get(CtxUserID)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
