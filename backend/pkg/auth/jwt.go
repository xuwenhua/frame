package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT 载荷
type Claims struct {
	UserID   string `json:"uid"`
	Email    string `json:"email"`
	Role     string `json:"role"` // user / admin
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	expire time.Duration
}

func NewManager(secret string, expireSeconds int) *Manager {
	if expireSeconds <= 0 {
		expireSeconds = 86400 * 7
	}
	return &Manager{
		secret: []byte(secret),
		expire: time.Duration(expireSeconds) * time.Second,
	}
}

// Generate 签发 token
func (m *Manager) Generate(userID, email, role string) (string, time.Time, error) {
	expAt := time.Now().Add(m.expire)
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "jikeai-clone",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	return signed, expAt, err
}

// Parse 校验并解析 token
func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("签名算法不匹配")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("token 无效")
	}
	return claims, nil
}
