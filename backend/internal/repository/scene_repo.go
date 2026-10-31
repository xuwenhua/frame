package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

// SceneRepository 场景数据访问
type SceneRepository struct {
	db *gorm.DB
}

func NewSceneRepository(db *gorm.DB) *SceneRepository {
	return &SceneRepository{db: db}
}

func (r *SceneRepository) ListByEpisodeID(episodeID string) ([]model.Scene, error) {
	var scenes []model.Scene
	err := r.db.Where("episode_id = ?", episodeID).Find(&scenes).Error
	return scenes, err
}

// ListByProjectID 项目场景库：该项目下所有剧集里的场景（新的在前）
func (r *SceneRepository) ListByProjectID(projectID string) ([]model.Scene, error) {
	var list []model.Scene
	sub := r.db.Model(&model.Episode{}).Select("id").Where("project_id = ?", projectID)
	err := r.db.Where("episode_id IN (?)", sub).Order("updated_at desc").Find(&list).Error
	return list, err
}

// ListByUserID 总场景库：该用户所有项目里的场景
func (r *SceneRepository) ListByUserID(userID string) ([]model.Scene, error) {
	var list []model.Scene
	sub := r.db.Model(&model.Episode{}).Select("episodes.id").
		Joins("JOIN projects ON projects.id = episodes.project_id").
		Where("projects.user_id = ?", userID)
	err := r.db.Where("episode_id IN (?)", sub).Order("updated_at desc").Find(&list).Error
	return list, err
}

func (r *SceneRepository) GetByID(id string) (*model.Scene, error) {
	var s model.Scene
	err := r.db.First(&s, "id = ?", id).Error
	return &s, err
}

func (r *SceneRepository) CreateBatch(scenes []model.Scene) error {
	return r.db.Create(&scenes).Error
}

func (r *SceneRepository) Update(s *model.Scene) error {
	return r.db.Save(s).Error
}

func (r *SceneRepository) Delete(id string) error {
	return r.db.Delete(&model.Scene{}, "id = ?", id).Error
}

func (r *SceneRepository) DeleteByEpisodeID(episodeID string) error {
	return r.db.Unscoped().Where("episode_id = ?", episodeID).Delete(&model.Scene{}).Error
}
