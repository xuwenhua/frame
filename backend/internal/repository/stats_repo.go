package repository

import (
	"gorm.io/gorm"

	"jikeai-clone/internal/model"
)

// StatsRepository 个人中心统计
type StatsRepository struct {
	db *gorm.DB
}

func NewStatsRepository(db *gorm.DB) *StatsRepository {
	return &StatsRepository{db: db}
}

// UserStats 用户维度的统计
type UserStats struct {
	ProjectCount    int64 `json:"projectCount"`
	EpisodeCount    int64 `json:"episodeCount"`
	FrameCount      int64 `json:"frameCount"`
	ImageCount      int64 `json:"imageCount"`
	VideoCount      int64 `json:"videoCount"`
	CharacterCount  int64 `json:"characterCount"`
	SceneCount      int64 `json:"sceneCount"`
	ItemCount       int64 `json:"itemCount"`
}

// GetUserStats 汇总该用户的所有创作数据
func (r *StatsRepository) GetUserStats(userID string) (*UserStats, error) {
	stats := &UserStats{}

	// 项目
	if err := r.db.Model(&model.Project{}).Where("user_id = ?", userID).Count(&stats.ProjectCount).Error; err != nil {
		return nil, err
	}

	// 取该用户所有剧集 ID
	var episodeIDs []string
	r.db.Model(&model.Episode{}).
		Where("project_id IN (?)", r.db.Model(&model.Project{}).Select("id").Where("user_id = ?", userID)).
		Pluck("id", &episodeIDs)
	stats.EpisodeCount = int64(len(episodeIDs))

	if len(episodeIDs) > 0 {
		r.db.Model(&model.Storyboard{}).Where("episode_id IN ?", episodeIDs).Count(&stats.FrameCount)
		r.db.Model(&model.Character{}).Where("episode_id IN ?", episodeIDs).Count(&stats.CharacterCount)
		r.db.Model(&model.Scene{}).Where("episode_id IN ?", episodeIDs).Count(&stats.SceneCount)
		r.db.Model(&model.Item{}).Where("episode_id IN ?", episodeIDs).Count(&stats.ItemCount)

		// 已出图 / 已出片的数量
		r.db.Model(&model.Character{}).
			Where("episode_id IN ? AND reference_image != ''", episodeIDs).Count(&stats.ImageCount)
	}

	// 已生成视频数
	r.db.Model(&model.Storyboard{}).
		Where("episode_id IN (?) AND video_url != ''",
			r.db.Model(&model.Episode{}).Select("id").
				Where("project_id IN (?)", r.db.Model(&model.Project{}).Select("id").Where("user_id = ?", userID))).
		Count(&stats.VideoCount)

	return stats, nil
}
