package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

type ModelConfigRepository struct {
	db *gorm.DB
}

func NewModelConfigRepository(db *gorm.DB) *ModelConfigRepository {
	return &ModelConfigRepository{db: db}
}

func (r *ModelConfigRepository) List(category string) ([]model.ModelConfig, error) {
	var list []model.ModelConfig
	q := r.db.Order("category ASC, sort_order ASC, created_at ASC")
	if category != "" {
		q = q.Where("category = ?", category)
	}
	err := q.Find(&list).Error
	return list, err
}

func (r *ModelConfigRepository) GetByID(id string) (*model.ModelConfig, error) {
	var m model.ModelConfig
	err := r.db.First(&m, "id = ?", id).Error
	return &m, err
}

// GetDefault 取某分类下启用的默认模型
func (r *ModelConfigRepository) GetDefault(category string) (*model.ModelConfig, error) {
	var m model.ModelConfig
	err := r.db.Where("category = ? AND enabled = ? AND is_default = ?", category, true, true).
		Order("sort_order ASC").First(&m).Error
	if err != nil {
		// 没有标默认的，取第一个启用的
		err = r.db.Where("category = ? AND enabled = ?", category, true).
			Order("sort_order ASC").First(&m).Error
	}
	return &m, err
}

func (r *ModelConfigRepository) Create(m *model.ModelConfig) error {
	return r.db.Create(m).Error
}

func (r *ModelConfigRepository) Update(m *model.ModelConfig) error {
	return r.db.Save(m).Error
}

func (r *ModelConfigRepository) Delete(id string) error {
	return r.db.Delete(&model.ModelConfig{}, "id = ?", id).Error
}

// ClearDefault 同分类下取消其他默认标记
func (r *ModelConfigRepository) ClearDefault(category, exceptID string) error {
	return r.db.Model(&model.ModelConfig{}).
		Where("category = ? AND id != ?", category, exceptID).
		Update("is_default", false).Error
}

// IncrementCall 累计调用次数
func (r *ModelConfigRepository) IncrementCall(id string, success bool) error {
	q := r.db.Model(&model.ModelConfig{}).Where("id = ?", id).
		Update("call_count", gorm.Expr("call_count + 1"))
	if !success {
		q = r.db.Model(&model.ModelConfig{}).Where("id = ?", id).
			Update("failed_count", gorm.Expr("failed_count + 1"))
	}
	return q.Error
}

// ---------- SystemSetting ----------

type SettingRepository struct {
	db *gorm.DB
}

func NewSettingRepository(db *gorm.DB) *SettingRepository {
	return &SettingRepository{db: db}
}

func (r *SettingRepository) ListByGroup(group string) ([]model.SystemSetting, error) {
	var list []model.SystemSetting
	q := r.db.Order("`group` ASC, `key` ASC")
	if group != "" {
		q = q.Where("`group` = ?", group)
	}
	err := q.Find(&list).Error
	return list, err
}

func (r *SettingRepository) Get(key string) (string, error) {
	var s model.SystemSetting
	err := r.db.First(&s, "`key` = ?", key).Error
	return s.Value, err
}

func (r *SettingRepository) Upsert(s *model.SystemSetting) error {
	return r.db.Save(s).Error
}

func (r *SettingRepository) BulkUpsert(list []model.SystemSetting) error {
	return r.db.Save(&list).Error
}
