package model

import (
	"time"

	"gorm.io/gorm"
)

// FirstFrame 一集里的「首帧图分组」。
//
// 为什么不按帧存：16 段如果每段单独合成一张首帧图，模型每次对这个房间的理解
// 都略有不同，越画越漂。正确做法是按「场景 + 机位」归并 ——
// 同一空间、同一机位方向、人物位置一致的段共用一张首帧图，
// 空间和人物位置就锁死了，图的数量也从「段数」降到「机位数」。
//
// 对应 RunningHub 画布里「先出几张首帧场景图，再拿它们生成所有镜头」的做法。
type FirstFrame struct {
	ID        string `json:"id" gorm:"primaryKey;type:uuid"`
	EpisodeID string `json:"episodeId" gorm:"not null;index"`
	SceneID   string `json:"sceneId" gorm:"index"`
	SceneName string `json:"sceneName"`

	// Description 这一组的机位与构图（谁在画面哪一侧、景别、机位方向），
	// 既用于出图提示词，也给用户核对归并对不对
	Description string `json:"description" gorm:"type:text"`

	// FrameIndexes 这一组覆盖哪几段（段号数组的 JSON，如 [1,3,6,7]）
	FrameIndexes string `json:"frameIndexes" gorm:"type:text"`

	// Image 生成好的首帧图 URL（空 = 还没生成）
	Image string `json:"image"`

	SortOrder int `json:"sortOrder"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `json:"deletedAt" gorm:"index"`
}

func (FirstFrame) TableName() string {
	return "first_frames"
}
