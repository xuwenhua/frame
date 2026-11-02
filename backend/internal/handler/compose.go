package handler

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
)

// ComposeHandler 把一集里已出片的分镜片段按顺序拼成一条完整视频
type ComposeHandler struct {
	episodeRepo    *repository.EpisodeRepository
	projectRepo    *repository.ProjectRepository
	storyboardRepo *repository.StoryboardRepository
	composer       *service.VideoComposer
	resultDir      string
}

func NewComposeHandler(
	episodeRepo *repository.EpisodeRepository,
	projectRepo *repository.ProjectRepository,
	storyboardRepo *repository.StoryboardRepository,
	composer *service.VideoComposer,
	resultDir string,
) *ComposeHandler {
	return &ComposeHandler{
		episodeRepo:    episodeRepo,
		projectRepo:    projectRepo,
		storyboardRepo: storyboardRepo,
		composer:       composer,
		resultDir:      resultDir,
	}
}

// Compose POST /api/episodes/:id/compose
//
// 同步执行：concat + -c copy 对一集十几段来说是秒级，没必要走任务队列。
// 前端点一下转个圈就拿到成片，比轮询任务状态体验好。
func (h *ComposeHandler) Compose(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	epID := c.Param("id")

	ep, err := h.episodeRepo.GetByID(epID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	if !h.projectRepo.Exists(ep.ProjectID, userID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	frames, err := h.storyboardRepo.ListByEpisodeID(epID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	// 只拼已出片的段，顺序即 frame_index（仓储已按此排序）。
	// 中间缺段时也照拼 —— 用户可能只想要现有这几条，
	// 具体拼了几段在响应里返回，前端可以提示。
	inputs := make([]string, 0, len(frames))
	skipped := make([]int, 0)
	for _, f := range frames {
		if strings.TrimSpace(f.VideoURL) == "" {
			skipped = append(skipped, f.FrameIndex)
			continue
		}
		local, ok := h.localVideoPath(f.VideoURL)
		if !ok {
			skipped = append(skipped, f.FrameIndex)
			continue
		}
		inputs = append(inputs, local)
	}
	if len(inputs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "这一集还没有已出片的片段，先生成视频再合成"})
		return
	}

	// 合成 30 段以内的片子通常几秒；给足 5 分钟防上游 COS 慢
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	name := fmt.Sprintf("episode-%s-%d", epID, time.Now().Unix())
	url, err := h.composer.Compose(ctx, name, inputs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	duration := 0.0
	if abs, ok := h.localVideoPath(url); ok {
		duration = h.composer.ProbeDuration(ctx, abs)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "合成完成", "data": gin.H{
		"url":      url,
		"segments": len(inputs),
		"skipped":  skipped,
		"duration": duration,
	}})
}

// localVideoPath 把 /static/results/xxx.mp4 映射到磁盘绝对路径
func (h *ComposeHandler) localVideoPath(url string) (string, bool) {
	const prefix = "/static/results/"
	if !strings.HasPrefix(url, prefix) {
		return "", false // 外链或别的目录，本地拼不了
	}
	rel := strings.TrimPrefix(url, prefix)
	if rel == "" || strings.Contains(rel, "..") {
		return "", false
	}
	return filepath.Join(h.resultDir, filepath.FromSlash(rel)), true
}
