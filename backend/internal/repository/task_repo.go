package repository

import (
	"time"

	"gorm.io/gorm"

	"jikeai-clone/internal/model"
)

type TaskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Create(t *model.GenerationTask) error {
	return r.db.Create(t).Error
}

func (r *TaskRepository) GetByID(id, userID string) (*model.GenerationTask, error) {
	var t model.GenerationTask
	err := r.db.First(&t, "id = ? AND user_id = ?", id, userID).Error
	return &t, err
}

func (r *TaskRepository) List(userID, episodeID, status string, limit int) ([]model.GenerationTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var list []model.GenerationTask
	q := r.db.Where("user_id = ?", userID)
	if episodeID != "" {
		q = q.Where("episode_id = ?", episodeID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListByStoryboard 按分镜查任务历史（预览列的「历史」tab 用）
//
// 只返回有产出的成功任务，按时间倒序 —— 用户要在历史里挑一版回填到分镜上。
func (r *TaskRepository) ListByStoryboard(userID, storyboardID, taskType string, limit int) ([]model.GenerationTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []model.GenerationTask
	q := r.db.Where("user_id = ? AND storyboard_id = ? AND status = ?", userID, storyboardID, "succeeded")
	if taskType != "" {
		q = q.Where("type = ?", taskType)
	}
	err := q.Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *TaskRepository) Update(t *model.GenerationTask) error {
	return r.db.Save(t).Error
}

// ---------- 管理端 ----------

// AdminTaskRow 管理端列表行：任务 + 归属用户
type AdminTaskRow struct {
	model.GenerationTask
	UserEmail string `json:"userEmail"`
	UserNick  string `json:"userNick"`
	// 由 handler 回填（角色/场景/道具/分镜的名字）
	AssetName string `json:"assetName"`
}

// AdminTaskFilter 管理端筛选条件
type AdminTaskFilter struct {
	Status    string // pending / running / succeeded / failed
	Type      string // image / video
	UserID    string
	Keyword   string // 匹配用户邮箱 / 昵称 / 资产 ID / 任务 ID
	OnlyToday bool
	Page      int
	Size      int
}

// ListAdmin 跨用户任务列表
func (r *TaskRepository) ListAdmin(f AdminTaskFilter) ([]AdminTaskRow, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Size <= 0 || f.Size > 200 {
		f.Size = 20
	}

	base := r.db.Table("generation_tasks").
		Joins("LEFT JOIN users ON users.id = generation_tasks.user_id").
		Where("generation_tasks.deleted_at IS NULL")

	if f.Status != "" {
		base = base.Where("generation_tasks.status = ?", f.Status)
	}
	if f.Type != "" {
		base = base.Where("generation_tasks.type = ?", f.Type)
	}
	if f.UserID != "" {
		base = base.Where("generation_tasks.user_id = ?", f.UserID)
	}
	if f.OnlyToday {
		base = base.Where("generation_tasks.created_at >= ?", startOfToday())
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		base = base.Where(
			`(users.email LIKE ? OR users.nickname LIKE ? OR generation_tasks.asset_id = ? OR generation_tasks.id = ?)`,
			kw, kw, f.Keyword, f.Keyword,
		)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]AdminTaskRow, 0, f.Size)
	err := base.
		Select("generation_tasks.*, users.email AS user_email, users.nickname AS user_nick").
		Order("generation_tasks.created_at DESC").
		Offset((f.Page - 1) * f.Size).
		Limit(f.Size).
		Scan(&rows).Error
	return rows, total, err
}

// GetByIDAdmin 管理端取单条（不限用户）
func (r *TaskRepository) GetByIDAdmin(id string) (*AdminTaskRow, error) {
	var row AdminTaskRow
	err := r.db.Table("generation_tasks").
		Select("generation_tasks.*, users.email AS user_email, users.nickname AS user_nick").
		Joins("LEFT JOIN users ON users.id = generation_tasks.user_id").
		Where("generation_tasks.id = ? AND generation_tasks.deleted_at IS NULL", id).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

// AdminTaskStats 管理端实时概览
type AdminTaskStats struct {
	Pending   int64 `json:"pending"`
	Running   int64 `json:"running"`
	TodayDone int64 `json:"todayDone"`
	TodayFail int64 `json:"todayFail"`
	TodayAll  int64 `json:"todayAll"`
	// 今日成功任务的平均耗时（毫秒）
	AvgCostMs int64 `json:"avgCostMs"`
	// 近 1 小时成功 / 失败，用于判断"当前是否在正常出片"
	HourDone int64 `json:"hourDone"`
	HourFail int64 `json:"hourFail"`
}

// Stats 统计
func (r *TaskRepository) Stats() AdminTaskStats {
	s := AdminTaskStats{}
	today := startOfToday()
	hourAgo := time.Now().Add(-time.Hour)

	r.db.Model(&model.GenerationTask{}).Where("status = ?", "pending").Count(&s.Pending)
	r.db.Model(&model.GenerationTask{}).Where("status = ?", "running").Count(&s.Running)
	r.db.Model(&model.GenerationTask{}).Where("created_at >= ?", today).Count(&s.TodayAll)
	r.db.Model(&model.GenerationTask{}).
		Where("created_at >= ? AND status = ?", today, "succeeded").Count(&s.TodayDone)
	r.db.Model(&model.GenerationTask{}).
		Where("created_at >= ? AND status = ?", today, "failed").Count(&s.TodayFail)
	r.db.Model(&model.GenerationTask{}).
		Where("created_at >= ? AND status = ?", hourAgo, "succeeded").Count(&s.HourDone)
	r.db.Model(&model.GenerationTask{}).
		Where("created_at >= ? AND status = ?", hourAgo, "failed").Count(&s.HourFail)

	// 平均耗时：只算今天成功的（SQLite 用 julianday 求差，单位换算成毫秒）
	var avg *float64
	r.db.Model(&model.GenerationTask{}).
		Where("created_at >= ? AND status = ? AND started_at IS NOT NULL AND finished_at IS NOT NULL", today, "succeeded").
		Select("AVG((julianday(finished_at) - julianday(started_at)) * 86400000)").
		Scan(&avg)
	if avg != nil && *avg > 0 {
		s.AvgCostMs = int64(*avg)
	}
	return s
}

// AssetNames 批量取资产显示名：kind = character / scene / item / frame
func (r *TaskRepository) AssetNames(kind string, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	var rows []struct {
		ID   string
		Name string
	}
	switch kind {
	case "character":
		r.db.Table("characters").Select("id, name").Where("id IN ?", ids).Scan(&rows)
	case "scene":
		r.db.Table("scenes").Select("id, name").Where("id IN ?", ids).Scan(&rows)
	case "item":
		r.db.Table("items").Select("id, name").Where("id IN ?", ids).Scan(&rows)
	case "frame":
		r.db.Table("storyboards").Select("id, ('镜' || frame_index) AS name").Where("id IN ?", ids).Scan(&rows)
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out
}

// ResetForRetry 把失败任务恢复成待执行（重试）
func (r *TaskRepository) ResetForRetry(id string) error {
	now := time.Now()
	return r.db.Model(&model.GenerationTask{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":      "pending",
		"progress":    0,
		"error_msg":   "",
		"started_at":  nil,
		"finished_at": nil,
		"updated_at":  now,
	}).Error
}

func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}
