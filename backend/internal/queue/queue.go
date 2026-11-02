package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
)

// Handler 任务处理器：由业务层实现具体生成逻辑
type Handler func(ctx context.Context, task *model.GenerationTask) (resultURL string, resultJSON string, err error)

// Queue 任务执行器（单进程版）
//
// 执行模型：**一个任务一个协程**，不设固定 worker 池。
// 批量出图时 N 个任务会同时 /task/add 提交给上游，各自独立轮询，
// 不再出现「3 个 worker 逐个跑、30 张图排两小时」的情况。
// 上游自己的排队（queueTimeoutMs）由网关处理，本地不节流。
//
// 生产环境若要做多机分发，再换 Redis / RabbitMQ。
type Queue struct {
	db         *gorm.DB
	pointsRepo *repository.PointsRepository
	handlers   map[string]Handler
	results    *service.ResultStore

	wg   sync.WaitGroup
	quit chan struct{}
	once sync.Once

	// 并发上限：仅作为防失控的兜底（比如误触发上千任务），远高于正常批量规模
	maxConcurrent int

	// 已提交但还没跑完的任务 ID（防重复提交 / 重复执行）
	inflight sync.Map
	// 正在执行的数量（用于监控展示）
	running int64
}

func New(db *gorm.DB, pointsRepo *repository.PointsRepository, maxConcurrent int) *Queue {
	if maxConcurrent <= 0 {
		maxConcurrent = 200
	}
	return &Queue{
		db:            db,
		pointsRepo:    pointsRepo,
		handlers:      map[string]Handler{},
		quit:          make(chan struct{}),
		maxConcurrent: maxConcurrent,
	}
}

// WithResultStore 注入产物转存器（可为 nil，表示不转存）
func (q *Queue) WithResultStore(s *service.ResultStore) *Queue {
	q.results = s
	return q
}

// Register 注册某类任务的处理器
func (q *Queue) Register(taskType string, h Handler) {
	q.handlers[taskType] = h
}

// Start 启动后台清扫协程（任务本身按需起协程，无需常驻 worker）
func (q *Queue) Start() {
	go q.sweeper()
	log.Printf("任务执行器已启动，并发上限: %d", q.maxConcurrent)
}

// sweeper 定期把「落库了但没有协程在处理」的 pending 任务重新捞起来
// （服务重启会丢内存中的执行协程，不捞就会一直卡在 pending）
func (q *Queue) sweeper() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-q.quit:
			return
		case <-ticker.C:
			q.requeueStale()
		}
	}
}

func (q *Queue) requeueStale() {
	cutoff := time.Now().Add(-2 * time.Minute)
	var ids []string
	q.db.Model(&model.GenerationTask{}).
		Where("status = ? AND updated_at < ?", "pending", cutoff).
		Limit(500).Pluck("id", &ids)
	for _, id := range ids {
		q.Enqueue(id)
	}
	if len(ids) > 0 {
		log.Printf("重新入队 %d 个滞留任务", len(ids))
	}
}

// Stop 优雅关闭
func (q *Queue) Stop() {
	q.once.Do(func() {
		close(q.quit)
		q.wg.Wait()
		log.Println("任务执行器已停止")
	})
}

// Stats 运行态（管理端实时监控用）
type Stats struct {
	// Running 正在执行的任务数
	Running int `json:"running"`
	// Max 并发上限
	Max int `json:"max"`
	// Workers 兼容旧字段，等于 Max
	Workers int `json:"workers"`
}

func (q *Queue) Stats() Stats {
	return Stats{Running: q.runningCount(), Max: q.maxConcurrent, Workers: q.maxConcurrent}
}

func (q *Queue) runningCount() int {
	n := 0
	q.inflight.Range(func(_, _ interface{}) bool {
		n++
		return true
	})
	return n
}

// Enqueue 提交任务：立刻起一个协程去跑，不做本地排队
// 同一任务不会重复提交；超并发上限时退回，交给 sweeper 稍后再捞。
func (q *Queue) Enqueue(taskID string) {
	if _, loaded := q.inflight.LoadOrStore(taskID, struct{}{}); loaded {
		return
	}
	if atomic.LoadInt64(&q.running) >= int64(q.maxConcurrent) {
		q.inflight.Delete(taskID)
		log.Printf("并发已达上限 %d，任务 %s 稍后由清扫协程重试", q.maxConcurrent, taskID)
		return
	}

	q.wg.Add(1)
	go func(id string) {
		defer q.wg.Done()
		defer q.inflight.Delete(id)
		defer atomic.AddInt64(&q.running, -1)

		select {
		case <-q.quit:
			return
		default:
		}
		atomic.AddInt64(&q.running, 1)
		q.process(id)
	}(taskID)
}

func (q *Queue) process(taskID string) {
	var task model.GenerationTask
	if err := q.db.First(&task, "id = ?", taskID).Error; err != nil {
		log.Printf("任务 %s 不存在: %v", taskID, err)
		return
	}
	if task.Status != "pending" {
		return
	}

	// 原子领取：并发/重复入队时只有一个 worker 能把 pending 改成 running，
	// 避免同一个任务被跑两遍（会白烧上游额度）
	now := time.Now()
	claim := q.db.Model(&model.GenerationTask{}).
		Where("id = ? AND status = ?", taskID, "pending").
		Updates(map[string]interface{}{
			"status":     "running",
			"progress":   10,
			"started_at": now,
			"updated_at": now,
		})
	if claim.Error != nil || claim.RowsAffected == 0 {
		return
	}

	h, ok := q.handlers[task.Type]
	if !ok {
		q.fail(&task, fmt.Errorf("未注册任务类型: %s", task.Type))
		return
	}

	// 上游任务可能排队很久，视频给足 45 分钟，图片 15 分钟
	timeout := 15 * time.Minute
	if task.Type == "video" {
		timeout = 45 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resultURL, resultJSON, err := h(ctx, &task)
	if err != nil {
		q.fail(&task, err)
		return
	}

	// 上游结果 URL 24 小时失效，SUCCESS 后立刻转存到本地，库里存本站路径
	if q.results != nil && resultURL != "" {
		if local, e := q.results.Save(ctx, task.ID, resultURL); e != nil {
			log.Printf("任务 %s 产物转存失败（保留原始 URL）: %v", task.ID, e)
		} else if local != "" {
			resultURL = local
		}
	}

	finish := time.Now()
	q.db.Model(&model.GenerationTask{}).Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      "succeeded",
			"progress":    100,
			"result_url":  resultURL,
			"result_json": resultJSON,
			"finished_at": finish,
			"updated_at":  finish,
		})

	// 回写业务表
	q.writeBack(&task, resultURL)
}

// fail 任务失败：标记失败 + 退还积分
func (q *Queue) fail(task *model.GenerationTask, err error) {
	finish := time.Now()

	// 退积分（只退一次）
	if task.Credits > 0 && !task.CreditsRefunded {
		if _, e := q.pointsRepo.Grant(task.UserID, task.Credits, "refund", "生成失败退还", task.ID, "task"); e == nil {
			q.db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
				Update("credits_refunded", true)
		}
	}

	q.db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
		Updates(map[string]interface{}{
			"status":      "failed",
			"error_msg":   err.Error(),
			"finished_at": finish,
			"updated_at":  finish,
		})

	log.Printf("任务 %s 失败: %v", task.ID, err)
}

// writeBack 把结果写回角色/场景/道具/分镜
func (q *Queue) writeBack(task *model.GenerationTask, url string) {
	if url == "" {
		return
	}
	switch task.AssetType {
	case "character":
		q.db.Model(&model.Character{}).Where("id = ?", task.AssetID).
			Update("reference_image", url)
	case "scene":
		// 场景有三种产出，按任务类型分流，别互相覆盖：
		//   普通出图 → reference_image
		//   柱形全景图 → panorama_image
		//   360 全景视频 → panorama_video
		switch task.Type {
		case "panorama":
			q.db.Model(&model.Scene{}).Where("id = ?", task.AssetID).
				Update("panorama_image", url)
		case "panorama_video":
			q.db.Model(&model.Scene{}).Where("id = ?", task.AssetID).
				Update("panorama_video", url)
		default:
			q.db.Model(&model.Scene{}).Where("id = ?", task.AssetID).
				Update("reference_image", url)
		}
	case "item":
		q.db.Model(&model.Item{}).Where("id = ?", task.AssetID).
			Update("reference_image", url)
	case "frame":
		// 时长绝不能用硬编码：以前写死 13（默认模型 Seedance 的时长），
		// 换了 H3 工作流后出 3/5/7 秒的片子，writeBack 却把分镜的
		// duration 统一改成 13 —— 卡片显示 13 秒、描述词写 0.00–3.00s，
		// 用户以为时长对不上，其实是这里写坏的。
		updates := map[string]interface{}{"video_url": url}
		if d := taskDuration(task.Params); d > 0 {
			updates["duration"] = d
		}
		q.db.Model(&model.Storyboard{}).Where("id = ?", task.AssetID).Updates(updates)
	}
}

// taskDuration 取任务参数里用户选定的出片时长（秒），取不到返回 0
func taskDuration(params string) int {
	if strings.TrimSpace(params) == "" {
		return 0
	}
	var p struct {
		Duration int `json:"duration"`
	}
	if json.Unmarshal([]byte(params), &p) != nil {
		return 0
	}
	return p.Duration
}

// Recover 重启后恢复中断的任务
//
// 分两种情况，不能一律重跑：
//   - 已经拿到上游 taskId 的：说明任务真的提交过。上游结果 24 小时内可取，
//     重新入队会走「续查」（处理器带 externalTaskID 直接查状态/取产物），
//     不会重新 /task/add，所以**无论卡多久都不该判失败**。
//   - 没拿到上游 taskId 的（提交都没成功）：用时间兜底判失败并退积分，
//     否则用户会看到一条永远「生成中」的僵尸任务。
//
// 曾经的 bug：一刀切「卡超过 30 分钟就判失败」，但视频上游本来就要跑
// 20–45 分钟（queue 里 video 的超时也是 45 分钟）。服务中途重启一次，
// 正在跑的视频任务全被判定「中断失败」，而上游其实早就出片了 ——
// 用户看到一片失败，实际视频在 COS 上躺到 24 小时后过期。
func (q *Queue) Recover() {
	var stuck []model.GenerationTask
	q.db.Where("status IN ?", []string{"running", "pending"}).Find(&stuck)

	if len(stuck) == 0 {
		return
	}

	// 兜底阈值必须大于视频任务超时（45 分钟），否则正常在跑的任务会被误杀
	staleAfter := 60 * time.Minute
	now := time.Now()
	resumed, expired := 0, 0

	for _, t := range stuck {
		// 已提交到上游的任务永不判失败：重新入队会走续查，能直接取回产物。
		// 只有「连上游 ID 都没拿到」的才算真丢了，用时间兜底判失败。
		hasUpstream := strings.TrimSpace(t.ExternalTaskID) != ""
		if !hasUpstream && t.Status == "running" && t.StartedAt != nil && now.Sub(*t.StartedAt) > staleAfter {
			q.fail(&t, fmt.Errorf("任务中断超过 %d 分钟且未提交到上游，已判定失败。积分已退回，可重新生成",
				int(staleAfter/time.Minute)))
			expired++
			continue
		}

		q.db.Model(&model.GenerationTask{}).Where("id = ?", t.ID).
			Updates(map[string]interface{}{"status": "pending", "progress": 0})
		q.Enqueue(t.ID)
		resumed++
	}
	log.Printf("恢复了 %d 个中断任务（含上游续查），判定 %d 个超时任务失败", resumed, expired)
}
