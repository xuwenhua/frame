package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// AssetRef 分镜里引用的资产（角色/场景/物品）
type AssetRef struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// Storyboard 分镜
type Storyboard struct {
	ID          string         `json:"id" gorm:"primaryKey;type:uuid"`
	EpisodeID   string         `json:"episodeId" gorm:"not null;index"`
	FrameIndex  int            `json:"frameIndex"`                   // 第几帧
	Text        string         `json:"text" gorm:"type:text"`        // 人读脚本（~400字）
	VideoPrompt string         `json:"videoPrompt" gorm:"type:text"` // 机读 prompt（~1200字）
	// @-mention 引用的角色/场景/物品（存 JSON 数组）
	RolesJSON   string         `json:"rolesJson" gorm:"type:text"`   // [{"name":"女主","id":"xxx"}]
	ScenesJSON  string         `json:"scenesJson" gorm:"type:text"`  // [{"name":"家居客厅-白天","id":"xxx"}]
	ItemsJSON   string         `json:"itemsJson" gorm:"type:text"`   // [{"name":"面霜","id":"xxx"}]
	VideoModel  string         `json:"videoModel"`                   // doubao-seedance-2.0-mini
	VideoURL    string         `json:"videoUrl"`                     // 生成后的视频 URL
	// SketchImage 分镜草图（简笔速写，只给模型当构图/机位参考）。
	// 必须画得足够简：一旦有清晰人脸，模型会照着草图的脸生成，
	// 参考图里的人物就被盖掉了 —— 这是实测过的坑。
	SketchImage string         `json:"sketchImage"`
	// FirstFrameImage 首帧图：用「角色图 + 场景图 + 构图描述」合成的真实画面。
	// 视频生成时进首帧位（51），空间与人物长相都由它锁定 ——
	// 只靠文字描述的话，模型每次重建房间的位置都不一样，座位会漂。
	FirstFrameImage string `json:"firstFrameImage"`
	Duration    float64        `json:"duration"`                     // 视频时长（秒）
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `json:"deletedAt" gorm:"index"`

	// 解析后的引用列表（不落库，直接给前端用）
	// 前端一直读 frame.roles，但库里只有 rolesJson 字符串 —— 之前标签一直是空的，
	// 所以在这里统一解析，避免每个调用点各写一遍。
	Roles  []AssetRef `json:"roles" gorm:"-"`
	Scenes []AssetRef `json:"scenes" gorm:"-"`
	Items  []AssetRef `json:"items" gorm:"-"`
}

func (Storyboard) TableName() string {
	return "storyboards"
}

// FillRefs 把 *_json 字符串解析成结构化的 Roles/Scenes/Items
func (s *Storyboard) FillRefs() {
	s.Roles = parseAssetRefs(s.RolesJSON)
	s.Scenes = parseAssetRefs(s.ScenesJSON)
	s.Items = parseAssetRefs(s.ItemsJSON)
}

func parseAssetRefs(raw string) []AssetRef {
	if raw == "" {
		return []AssetRef{}
	}
	var out []AssetRef
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []AssetRef{}
	}
	if out == nil {
		return []AssetRef{}
	}
	return out
}

// SetRefs 把结构化的引用写回 *_json 字符串（保存时用）
func (s *Storyboard) SetRefs(roles, scenes, items []AssetRef) {
	if b, err := json.Marshal(roles); err == nil {
		s.RolesJSON = string(b)
	}
	if b, err := json.Marshal(scenes); err == nil {
		s.ScenesJSON = string(b)
	}
	if b, err := json.Marshal(items); err == nil {
		s.ItemsJSON = string(b)
	}
	s.FillRefs()
}
