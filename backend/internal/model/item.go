package model

import (
	"time"

	"gorm.io/gorm"
)

// Item 物品/道具（识别结果）
type Item struct {
	ID             string         `json:"id" gorm:"primaryKey;type:uuid"`
	EpisodeID      string         `json:"episodeId" gorm:"not null;index"`
	Name           string         `json:"name" gorm:"not null"` // 物品名，如 "面霜"
	Category       string         `json:"category"`             // 分类：护肤品/食品/电子产品
	Prompt         string         `json:"prompt" gorm:"type:text"` // 生图 prompt
	ImagePrompt    string         `json:"imagePrompt" gorm:"type:text"` // 出图提示词
	ReferenceImage string         `json:"referenceImage"`
	// 图生图垫图（可选），用户在编辑弹窗里单独上传
	PadImage string `json:"padImage"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	DeletedAt      gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (Item) TableName() string {
	return "items"
}
