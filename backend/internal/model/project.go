package model

import (
	"time"

	"gorm.io/gorm"
)

// Project 短剧项目
type Project struct {
	ID          string         `json:"id" gorm:"primaryKey;type:uuid"`
	UserID      string         `json:"userId" gorm:"index"` // 归属用户
	Name        string         `json:"name" gorm:"not null"`
	Description string         `json:"description"`
	Status      string         `json:"status" gorm:"default:draft"` // draft / in_progress / completed
	CoverURL    string         `json:"coverUrl"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (Project) TableName() string {
	return "projects"
}
