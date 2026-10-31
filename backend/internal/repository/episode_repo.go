package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

// EpisodeRepository 剧集数据访问
type EpisodeRepository struct {
	db *gorm.DB
}

func NewEpisodeRepository(db *gorm.DB) *EpisodeRepository {
	return &EpisodeRepository{db: db}
}

func (r *EpisodeRepository) ListByProjectID(projectID string) ([]model.Episode, error) {
	var episodes []model.Episode
	err := r.db.Where("project_id = ?", projectID).Order("episode_number ASC").Find(&episodes).Error
	return episodes, err
}

func (r *EpisodeRepository) GetByID(id string) (*model.Episode, error) {
	var e model.Episode
	err := r.db.First(&e, "id = ?", id).Error
	return &e, err
}

func (r *EpisodeRepository) Create(e *model.Episode) error {
	return r.db.Create(e).Error
}

func (r *EpisodeRepository) Update(e *model.Episode) error {
	return r.db.Save(e).Error
}
