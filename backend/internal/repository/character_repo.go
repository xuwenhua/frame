package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

// CharacterRepository 角色数据访问
type CharacterRepository struct {
	db *gorm.DB
}

func NewCharacterRepository(db *gorm.DB) *CharacterRepository {
	return &CharacterRepository{db: db}
}

func (r *CharacterRepository) ListByEpisodeID(episodeID string) ([]model.Character, error) {
	var chars []model.Character
	err := r.db.Where("episode_id = ?", episodeID).Find(&chars).Error
	return chars, err
}

// ListByProjectID 项目角色库：该项目下所有剧集里的角色（新的在前）
func (r *CharacterRepository) ListByProjectID(projectID string) ([]model.Character, error) {
	var list []model.Character
	sub := r.db.Model(&model.Episode{}).Select("id").Where("project_id = ?", projectID)
	err := r.db.Where("episode_id IN (?)", sub).Order("updated_at desc").Find(&list).Error
	return list, err
}

// ListByUserID 总角色库：该用户所有项目里的角色
func (r *CharacterRepository) ListByUserID(userID string) ([]model.Character, error) {
	var list []model.Character
	sub := r.db.Model(&model.Episode{}).Select("episodes.id").
		Joins("JOIN projects ON projects.id = episodes.project_id").
		Where("projects.user_id = ?", userID)
	err := r.db.Where("episode_id IN (?)", sub).Order("updated_at desc").Find(&list).Error
	return list, err
}

func (r *CharacterRepository) GetByID(id string) (*model.Character, error) {
	var c model.Character
	err := r.db.First(&c, "id = ?", id).Error
	return &c, err
}

func (r *CharacterRepository) CreateBatch(chars []model.Character) error {
	return r.db.Create(&chars).Error
}

func (r *CharacterRepository) Update(c *model.Character) error {
	return r.db.Save(c).Error
}

func (r *CharacterRepository) Delete(id string) error {
	return r.db.Delete(&model.Character{}, "id = ?", id).Error
}

func (r *CharacterRepository) DeleteByEpisodeID(episodeID string) error {
	return r.db.Unscoped().Where("episode_id = ?", episodeID).Delete(&model.Character{}).Error
}
