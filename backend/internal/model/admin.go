package model

import (
	"time"

	"gorm.io/gorm"
)

// ModelConfig 模型配置（LLM / 图像 / 视频）
type ModelConfig struct {
	ID       string `json:"id" gorm:"primaryKey;type:uuid"`
	Provider string `json:"provider" gorm:"not null;index"` // openai / runninghub / doubao / minimax / custom
	Category string `json:"category" gorm:"not null;index"` // llm / image / video / tts

	// 展示信息
	DisplayName string `json:"displayName" gorm:"not null"` // 「GPT-4o」「Seedance Mini」
	ModelID     string `json:"modelId" gorm:"not null"`     // 实际请求用的 model 名

	// 接入参数
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey" gorm:"type:text"` // 生产环境应加密存储

	// 能力参数（JSON，不同类型字段不同）
	// llm:   {"temperature":0.7,"maxTokens":4096}
	// image: {"width":1024,"height":1024,"steps":30}
	// video: {"duration":13,"resolution":"720p","fps":24}
	Params string `json:"params" gorm:"type:text"`

	// NeedRefImages 图生图至少需要几张参考图（0 = 纯文生图）。
	// 首帧图/换装这类任务必须挑支持参考图的模型，
	// 前端原来读 m.needRefImages 但后端没这个字段，一直读到 undefined → 挑不出 i2i 模型。
	NeedRefImages int `json:"needRefImages" gorm:"default:0"`

	// 计费
	CreditsPerCall int `json:"creditsPerCall"` // 单次调用消耗积分
	CreditsPerUnit int `json:"creditsPerUnit"` // 按单位计费（如每张图、每秒视频）

	// 状态
	IsDefault bool   `json:"isDefault" gorm:"default:false"` // 同类中默认使用
	Enabled   bool   `json:"enabled" gorm:"default:true"`
	SortOrder int    `json:"sortOrder" gorm:"default:0"`
	Remark    string `json:"remark"`

	// 调用统计
	CallCount  int64      `json:"callCount" gorm:"default:0"`
	FailedCount int64     `json:"failedCount" gorm:"default:0"`
	LastUsedAt *time.Time `json:"lastUsedAt"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (ModelConfig) TableName() string {
	return "model_configs"
}

// SystemSetting 系统设置（键值对）
type SystemSetting struct {
	Key       string    `json:"key" gorm:"primaryKey"`
	Value     string    `json:"value" gorm:"type:text"`
	Group     string    `json:"group" gorm:"index"` // points / storage / general
	Label     string    `json:"label"`
	ValueType string    `json:"valueType"` // string / int / bool / json
	Remark    string    `json:"remark"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (SystemSetting) TableName() string {
	return "system_settings"
}

// AdminUser 管理员
type AdminUser struct {
	ID           string     `json:"id" gorm:"primaryKey;type:uuid"`
	Username     string     `json:"username" gorm:"uniqueIndex;not null"`
	PasswordHash string     `json:"-" gorm:"not null"`
	Nickname     string     `json:"nickname"`
	Role         string     `json:"role" gorm:"default:admin"` // admin / operator
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	Enabled      bool       `json:"enabled" gorm:"default:true"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func (AdminUser) TableName() string {
	return "admin_users"
}
