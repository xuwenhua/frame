package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

// StoryboardRepository 分镜数据访问
type StoryboardRepository struct {
	db *gorm.DB
}

func NewStoryboardRepository(db *gorm.DB) *StoryboardRepository {
	return &StoryboardRepository{db: db}
}

func (r *StoryboardRepository) ListByEpisodeID(episodeID string) ([]model.Storyboard, error) {
	var frames []model.Storyboard
	err := r.db.Where("episode_id = ?", episodeID).Order("frame_index ASC").Find(&frames).Error
	return frames, err
}

func (r *StoryboardRepository) GetByID(id string) (*model.Storyboard, error) {
	var f model.Storyboard
	err := r.db.First(&f, "id = ?", id).Error
	return &f, err
}

func (r *StoryboardRepository) CreateBatch(frames []model.Storyboard) error {
	return r.db.Create(&frames).Error
}

func (r *StoryboardRepository) Update(f *model.Storyboard) error {
	return r.db.Save(f).Error
}

func (r *StoryboardRepository) Delete(id string) error {
	return r.db.Delete(&model.Storyboard{}, "id = ?", id).Error
}

// DeleteByEpisodeID 重新生成前清空该集旧分镜
func (r *StoryboardRepository) DeleteByEpisodeID(episodeID string) error {
	return r.db.Unscoped().Where("episode_id = ?", episodeID).Delete(&model.Storyboard{}).Error
}
