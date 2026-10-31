package model

import (
	"time"

	"gorm.io/gorm"
)

// User 终端用户
type User struct {
	ID           string         `json:"id" gorm:"primaryKey;type:uuid"`
	Email        string         `json:"email" gorm:"uniqueIndex;not null"`
	PasswordHash string         `json:"-" gorm:"not null"`
	Nickname     string         `json:"nickname"`
	Avatar       string         `json:"avatar"`
	Status       string         `json:"status" gorm:"default:active"` // active / banned
	LastLoginAt  *time.Time     `json:"lastLoginAt"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	DeletedAt    gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (User) TableName() string {
	return "users"
}

// PointsAccount 积分账户（与 User 一对一）
type PointsAccount struct {
	ID        string    `json:"id" gorm:"primaryKey;type:uuid"`
	UserID    string    `json:"userId" gorm:"uniqueIndex;not null"`
	Balance   int       `json:"balance"`   // 可用余额
	TotalIn   int       `json:"totalIn"`   // 累计获得
	TotalOut  int       `json:"totalOut"`  // 累计消耗
	Version   int       `json:"version"`   // 乐观锁版本号
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (PointsAccount) TableName() string {
	return "points_accounts"
}

// PointsTransaction 积分流水
type PointsTransaction struct {
	ID      string `json:"id" gorm:"primaryKey;type:uuid"`
	UserID  string `json:"userId" gorm:"not null;index"`
	Amount  int    `json:"amount"`  // 正数为收入，负数为支出
	Balance int    `json:"balance"` // 变更后余额，便于对账
	Type    string `json:"type" gorm:"not null;index"` // register / recharge / identify / image / video / refund
	Reason  string `json:"reason"`
	RefID   string `json:"refId" gorm:"index"` // 关联业务 ID（如 episodeId、storyboardId）
	RefType string `json:"refType"`            // episode / storyboard / order

	CreatedAt time.Time `json:"createdAt"`
}

func (PointsTransaction) TableName() string {
	return "points_transactions"
}
