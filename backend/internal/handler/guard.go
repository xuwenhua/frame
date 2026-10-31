package handler

import (
	"errors"

	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
)

// 统一错误
var (
	errNotFound = errors.New("not found")
	errForbidden = errors.New("forbidden")
	errBadURL    = errors.New("地址格式不正确，只支持本站 /static 路径或 http(s) 链接")
)

// ownershipGuard 校验剧集/分镜的归属，避免越权操作
type ownershipGuard struct {
	episodeRepo *repository.EpisodeRepository
	projectRepo *repository.ProjectRepository
}

func newOwnershipGuard(e *repository.EpisodeRepository, p *repository.ProjectRepository) *ownershipGuard {
	return &ownershipGuard{episodeRepo: e, projectRepo: p}
}

// Episode 校验剧集属于该用户，返回剧集本身
func (g *ownershipGuard) Episode(episodeID, userID string) (*model.Episode, error) {
	ep, err := g.episodeRepo.GetByID(episodeID)
	if err != nil {
		return nil, errNotFound
	}
	if !g.projectRepo.Exists(ep.ProjectID, userID) {
		return nil, errForbidden
	}
	return ep, nil
}
