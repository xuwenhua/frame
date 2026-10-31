package model

import (
	"time"

	"gorm.io/gorm"
)

// Character 角色（识别结果）
type Character struct {
	ID             string         `json:"id" gorm:"primaryKey;type:uuid"`
	EpisodeID      string         `json:"episodeId" gorm:"not null;index"`
	Name           string         `json:"name" gorm:"not null"`            // 角色名，如 "女主"
	IdentityAnchor string         `json:"identityAnchor" gorm:"type:text"` // 身份锚定：年龄、职业、外貌
	CostumePrompt  string         `json:"costumePrompt" gorm:"type:text"`   // 服装 prompt
	Personality    string         `json:"personality" gorm:"type:text"`     // 性格描述
	AppearanceCues string        `json:"appearanceCues" gorm:"type:text"`  // 外貌细节：发型、五官等
	ImagePrompt    string         `json:"imagePrompt" gorm:"type:text"`    // 出图提示词（可直接用于生图）
	ReferenceImage string         `json:"referenceImage"`                  // 参考图 URL（生成或用户上传）
	// 图生图垫图（可选）：用户在「编辑角色」里单独上传的一张图，
	// 走图生图时会自动带上，用来锁定脸型 / 服装版型
	PadImage string `json:"padImage"`
	// 以下是「编辑角色」里的可选项，对齐竞品：
	// 音色描述参与 TTS 配音，参考音频给声音克隆用
	VoiceTone     string `json:"voiceTone" gorm:"type:text"` // 角色音色描述，如「松弛年轻男音：中音域，清朗温厚，语速稍快」
	VoiceSample   string `json:"voiceSample"`               // 角色参考音频 URL（最长 15 秒）
	ReferenceNote string `json:"referenceNote"`             // 参考图来源标记：generated / uploaded
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	DeletedAt      gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (Character) TableName() string {
	return "characters"
}
