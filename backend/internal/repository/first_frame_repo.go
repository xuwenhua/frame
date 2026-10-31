package repository

import (
	"jikeai-clone/internal/model"

	"gorm.io/gorm"
)

// FirstFrameRepository 首帧图分组数据访问
type FirstFrameRepository struct {
	db *gorm.DB
}

func NewFirstFrameRepository(db *gorm.DB) *FirstFrameRepository {
	return &FirstFrameRepository{db: db}
}

func (r *FirstFrameRepository) ListByEpisodeID(episodeID string) ([]model.FirstFrame, error) {
	var list []model.FirstFrame
	err := r.db.Where("episode_id = ?", episodeID).Order("sort_order ASC").Find(&list).Error
	return list, err
}

func (r *FirstFrameRepository) GetByID(id string) (*model.FirstFrame, error) {
	var f model.FirstFrame
	err := r.db.First(&f, "id = ?", id).Error
	return &f, err
}

// ReplaceForEpisode 用新的分组覆盖这一集的旧分组。
//
// 重新规划时必须整批替换：留着旧组会让同一段同时属于两个组，
// 视频生成时就不知道该取哪张首帧图。
func (r *FirstFrameRepository) ReplaceForEpisode(episodeID string, groups []model.FirstFrame) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("episode_id = ?", episodeID).Delete(&model.FirstFrame{}).Error; err != nil {
			return err
		}
		if len(groups) == 0 {
			return nil
		}
		return tx.Create(&groups).Error
	})
}

func (r *FirstFrameRepository) Update(f *model.FirstFrame) error {
	return r.db.Save(f).Error
}

// UpdateImage 只更新生成的图（队列里回写用，避免覆盖用户刚改的分组描述）
func (r *FirstFrameRepository) UpdateImage(id, url string) error {
	return r.db.Model(&model.FirstFrame{}).Where("id = ?", id).
		Update("image", url).Error
}
