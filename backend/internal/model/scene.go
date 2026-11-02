package model

import (
	"time"

	"gorm.io/gorm"
)

// Scene 场景（识别结果）
type Scene struct {
	ID             string `json:"id" gorm:"primaryKey;type:uuid"`
	EpisodeID      string `json:"episodeId" gorm:"not null;index"`
	Name           string `json:"name" gorm:"not null"`            // 场景名，如 "家居客厅-白天"
	SpatialAnchor  string `json:"spatialAnchor" gorm:"type:text"`  // 空间锚定：布局、家具
	LightingPrompt string `json:"lightingPrompt" gorm:"type:text"` // 光照 prompt
	TimeOfDay      string `json:"timeOfDay"`                       // 白天 / 夜晚 / 黄昏
	ImagePrompt    string `json:"imagePrompt" gorm:"type:text"`    // 出图提示词
	ReferenceImage string `json:"referenceImage"`
	// PanoramaImage 柱形全景图（equirectangular），由 ReferenceImage 经图片编辑模型扩展而来
	PanoramaImage string `json:"panoramaImage" gorm:"type:text"`
	// PanoramaVideo 由柱形全景图转出的 360 全景视频 —— 用来看清整个房间，
	// 也可以作为参考视频喂给 H3，让模型理解空间全貌
	PanoramaVideo string `json:"panoramaVideo" gorm:"type:text"`
	// 图生图垫图（可选），用户在编辑弹窗里单独上传
	PadImage  string         `json:"padImage"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (Scene) TableName() string {
	return "scenes"
}
