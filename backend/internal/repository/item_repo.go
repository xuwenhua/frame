package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

// ItemRepository 物品数据访问
type ItemRepository struct {
	db *gorm.DB
}

func NewItemRepository(db *gorm.DB) *ItemRepository {
	return &ItemRepository{db: db}
}

func (r *ItemRepository) ListByEpisodeID(episodeID string) ([]model.Item, error) {
	var items []model.Item
	err := r.db.Where("episode_id = ?", episodeID).Find(&items).Error
	return items, err
}

// ListByProjectID 项目物品库：该项目下所有剧集里的物品（新的在前）
func (r *ItemRepository) ListByProjectID(projectID string) ([]model.Item, error) {
	var list []model.Item
	sub := r.db.Model(&model.Episode{}).Select("id").Where("project_id = ?", projectID)
	err := r.db.Where("episode_id IN (?)", sub).Order("updated_at desc").Find(&list).Error
	return list, err
}

// ListByUserID 总物品库：该用户所有项目里的物品
func (r *ItemRepository) ListByUserID(userID string) ([]model.Item, error) {
	var list []model.Item
	sub := r.db.Model(&model.Episode{}).Select("episodes.id").
		Joins("JOIN projects ON projects.id = episodes.project_id").
		Where("projects.user_id = ?", userID)
	err := r.db.Where("episode_id IN (?)", sub).Order("updated_at desc").Find(&list).Error
	return list, err
}

func (r *ItemRepository) GetByID(id string) (*model.Item, error) {
	var it model.Item
	err := r.db.First(&it, "id = ?", id).Error
	return &it, err
}

func (r *ItemRepository) CreateBatch(items []model.Item) error {
	return r.db.Create(&items).Error
}

func (r *ItemRepository) Update(i *model.Item) error {
	return r.db.Save(i).Error
}

func (r *ItemRepository) Delete(id string) error {
	return r.db.Delete(&model.Item{}, "id = ?", id).Error
}

func (r *ItemRepository) DeleteByEpisodeID(episodeID string) error {
	return r.db.Unscoped().Where("episode_id = ?", episodeID).Delete(&model.Item{}).Error
}
