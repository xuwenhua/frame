package idgen

import "github.com/google/uuid"

// New 生成 UUID
func New() string {
	return uuid.NewString()
}
