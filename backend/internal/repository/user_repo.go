package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/idgen"
)

var (
	ErrInsufficientPoints = errors.New("积分不足")
	ErrAccountNotFound    = errors.New("积分账户不存在")
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetByID(id string) (*model.User, error) {
	var u model.User
	err := r.db.First(&u, "id = ?", id).Error
	return &u, err
}

func (r *UserRepository) GetByEmail(email string) (*model.User, error) {
	var u model.User
	err := r.db.First(&u, "email = ?", email).Error
	return &u, err
}

func (r *UserRepository) EmailExists(email string) bool {
	var n int64
	r.db.Model(&model.User{}).Where("email = ?", email).Count(&n)
	return n > 0
}

func (r *UserRepository) Create(u *model.User) error {
	return r.db.Create(u).Error
}

func (r *UserRepository) Update(u *model.User) error {
	return r.db.Save(u).Error
}

func (r *UserRepository) TouchLogin(id string) error {
	now := time.Now()
	return r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]interface{}{"last_login_at": now}).Error
}

// DB 暴露给需要跨仓储事务的场景
func (r *UserRepository) DB() *gorm.DB {
	return r.db
}

// GetStatus 只取状态字段（鉴权中间件用，避免整行查询）
func (r *UserRepository) GetStatus(id string) (string, error) {
	var u model.User
	err := r.db.Select("status").First(&u, "id = ?", id).Error
	return u.Status, err
}

// ListAdmin 管理端用户列表（支持关键词与状态筛选，分页）
func (r *UserRepository) ListAdmin(page, size int, keyword, status string) ([]model.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}

	q := r.db.Model(&model.User{})
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("email LIKE ? OR nickname LIKE ?", like, like)
	}
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.User
	err := q.Order("created_at DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&list).Error
	return list, total, err
}

// PlatformUserStats 平台维度用户统计
type PlatformUserStats struct {
	Total        int64 `json:"total"`
	Active       int64 `json:"active"`
	Banned       int64 `json:"banned"`
	TotalBalance int64 `json:"totalBalance"`
	NewToday     int64 `json:"newToday"`
}

func (r *UserRepository) Stats() (PlatformUserStats, error) {
	var s PlatformUserStats
	db := r.db.Model(&model.User{})

	if err := db.Count(&s.Total).Error; err != nil {
		return s, err
	}
	_ = r.db.Model(&model.User{}).Where("status = ?", "active").Count(&s.Active)
	_ = r.db.Model(&model.User{}).Where("status = ?", "banned").Count(&s.Banned)

	var sum struct{ V sql.NullInt64 }
	_ = r.db.Model(&model.PointsAccount{}).Select("SUM(balance) as v").Scan(&sum)
	s.TotalBalance = sum.V.Int64

	today := time.Now().Truncate(24 * time.Hour)
	_ = r.db.Model(&model.User{}).Where("created_at >= ?", today).Count(&s.NewToday)
	return s, nil
}

// SetStatus 启用 / 封禁
func (r *UserRepository) SetStatus(id, status string) error {
	return r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]interface{}{"status": status, "updated_at": time.Now()}).Error
}

// ==================== 积分账户 ====================

type PointsRepository struct {
	db *gorm.DB
}

func NewPointsRepository(db *gorm.DB) *PointsRepository {
	return &PointsRepository{db: db}
}

// GetOrCreateAccount 取账户，不存在则创建
func (r *PointsRepository) GetOrCreateAccount(userID string) (*model.PointsAccount, error) {
	var acc model.PointsAccount
	err := r.db.First(&acc, "user_id = ?", userID).Error
	if err == nil {
		return &acc, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	acc = model.PointsAccount{
		ID:        userID + "-acc",
		UserID:    userID,
		Balance:   0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := r.db.Create(&acc).Error; err != nil {
		// 并发下可能已被创建，重试读取
		if e := r.db.First(&acc, "user_id = ?", userID).Error; e == nil {
			return &acc, nil
		}
		return nil, err
	}
	return &acc, nil
}

// Balance 只读余额
func (r *PointsRepository) Balance(userID string) (int, error) {
	acc, err := r.GetOrCreateAccount(userID)
	if err != nil {
		return 0, err
	}
	return acc.Balance, nil
}

// Grant 增加积分（注册赠送、充值、退款）
func (r *PointsRepository) Grant(userID string, amount int, txType, reason, refID, refType string) (*model.PointsAccount, error) {
	if amount <= 0 {
		return nil, errors.New("增加积分必须为正数")
	}
	return r.change(userID, amount, txType, reason, refID, refType)
}

// Consume 扣减积分（余额不足返回 ErrInsufficientPoints）
func (r *PointsRepository) Consume(userID string, amount int, txType, reason, refID, refType string) (*model.PointsAccount, error) {
	if amount <= 0 {
		return nil, errors.New("扣减积分必须为正数")
	}
	return r.change(userID, -amount, txType, reason, refID, refType)
}

// change 原子修改余额 + 写流水，同一事务内完成
func (r *PointsRepository) change(userID string, delta int, txType, reason, refID, refType string) (*model.PointsAccount, error) {
	var result model.PointsAccount

	err := r.db.Transaction(func(tx *gorm.DB) error {
		// 行级锁读取账户
		var acc model.PointsAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&acc).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAccountNotFound
			}
			return err
		}

		newBalance := acc.Balance + delta
		if newBalance < 0 {
			return ErrInsufficientPoints
		}

		updates := map[string]interface{}{
			"balance":    newBalance,
			"updated_at": time.Now(),
			"version":    acc.Version + 1,
		}
		if delta > 0 {
			updates["total_in"] = acc.TotalIn + delta
		} else {
			updates["total_out"] = acc.TotalOut - delta
		}

		if err := tx.Model(&model.PointsAccount{}).
			Where("id = ?", acc.ID).
			Updates(updates).Error; err != nil {
			return err
		}

		// 写流水
		rec := model.PointsTransaction{
			ID:        idgen.New(),
			UserID:    userID,
			Amount:    delta,
			Balance:   newBalance,
			Type:      txType,
			Reason:    reason,
			RefID:     refID,
			RefType:   refType,
			CreatedAt: time.Now(),
		}
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}

		acc.Balance = newBalance
		result = acc
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTransactions 积分流水（分页）
func (r *PointsRepository) ListTransactions(userID string, page, size int) ([]model.PointsTransaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var total int64
	r.db.Model(&model.PointsTransaction{}).Where("user_id = ?", userID).Count(&total)

	var list []model.PointsTransaction
	err := r.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&list).Error

	return list, total, err
}
