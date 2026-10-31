package model

import (
	"time"

	"gorm.io/gorm"
)

// GenerationTask 生成任务（图片/视频异步任务的统一载体）
type GenerationTask struct {
	ID     string `json:"id" gorm:"primaryKey;type:uuid"`
	UserID string `json:"userId" gorm:"not null;index"`
	Type   string `json:"type" gorm:"not null;index"` // image / video

	// 业务关联
	EpisodeID    string `json:"episodeId" gorm:"index"`
	StoryboardID string `json:"storyboardId" gorm:"index"`
	AssetType    string `json:"assetType"` // character / scene / item / frame
	AssetID      string `json:"assetId" gorm:"index"`

	// 入参
	Prompt  string `json:"prompt" gorm:"type:text"`
	ModelID string `json:"modelId"`
	Params  string `json:"params" gorm:"type:text"`

	// 状态机：pending → running → succeeded / failed
	Status   string `json:"status" gorm:"default:pending;index"`
	Progress int    `json:"progress"`
	ResultURL string `json:"resultUrl" gorm:"type:text"`
	ResultJSON string `json:"resultJson" gorm:"type:text"`
	ErrorMsg string `json:"errorMsg" gorm:"type:text"`

	// 计费
	Credits     int  `json:"credits"`
	CreditsRefunded bool `json:"creditsRefunded" gorm:"default:false"`

	// 外部任务
	ExternalTaskID string `json:"externalTaskId" gorm:"index"`

	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	DeletedAt  gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (GenerationTask) TableName() string {
	return "generation_tasks"
}
