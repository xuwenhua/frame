package repository

import (
	"gorm.io/gorm"

	"jikeai-clone/internal/model"
)

// ProjectRepository 项目数据访问（所有方法都要求 userID，保证数据隔离）
type ProjectRepository struct {
	db *gorm.DB
}

func NewProjectRepository(db *gorm.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) List(userID string, page, size int) ([]model.Project, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var total int64
	r.db.Model(&model.Project{}).Where("user_id = ?", userID).Count(&total)

	var list []model.Project
	err := r.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&list).Error

	return list, total, err
}

// CountByUser 某用户的项目总数（管理端用）
func (r *ProjectRepository) CountByUser(userID string) (int64, error) {
	var n int64
	err := r.db.Model(&model.Project{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// GetByID 带归属校验，别人的项目查不到
func (r *ProjectRepository) GetByID(id, userID string) (*model.Project, error) {
	var p model.Project
	err := r.db.First(&p, "id = ? AND user_id = ?", id, userID).Error
	return &p, err
}

func (r *ProjectRepository) Create(p *model.Project) error {
	return r.db.Create(p).Error
}

func (r *ProjectRepository) Update(p *model.Project) error {
	return r.db.Save(p).Error
}

// Delete 带归属校验
func (r *ProjectRepository) Delete(id, userID string) error {
	return r.db.Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.Project{}).Error
}

// Exists 判断项目是否属于该用户
func (r *ProjectRepository) Exists(id, userID string) bool {
	var n int64
	r.db.Model(&model.Project{}).Where("id = ? AND user_id = ?", id, userID).Count(&n)
	return n > 0
}
