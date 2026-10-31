package model

import (
	"time"

	"gorm.io/gorm"
)

// Episode 剧集（短剧的某一集）
type Episode struct {
	ID            string `json:"id" gorm:"primaryKey;type:uuid"`
	ProjectID     string `json:"projectId" gorm:"not null;index"`
	EpisodeNumber int    `json:"episodeNumber"`
	Title         string `json:"title"`
	ScriptText    string `json:"scriptText" gorm:"type:text"` // 原始剧本文本
	Status        string `json:"status" gorm:"default:script"` // script / identified / storyboard / video
	// 出片配置：同一集内各镜共用的画幅与默认视频模型，跟着剧集走（刷新 / 换设备都保持）
	AspectRatio string         `json:"aspectRatio" gorm:"size:16"` // 16:9 / 9:16
	VideoModel  string         `json:"videoModel" gorm:"size:64"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (Episode) TableName() string {
	return "episodes"
}
