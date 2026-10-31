package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/pkg/idgen"
)

// UploadHandler 用户上传（角色参考图 / 图生图垫图 / 参考音频）
//
// 与生成产物分开目录存放，避免清理产物时误删用户素材。
type UploadHandler struct {
	rootDir string // 上传文件根目录（<resultDir>/uploads）
	prefix  string // 对外前缀（/static/uploads）
}

func NewUploadHandler(uploadDir, prefix string) *UploadHandler {
	return &UploadHandler{
		rootDir: uploadDir,
		prefix:  strings.TrimRight(prefix, "/"),
	}
}

// 允许的扩展名：图片用于参考图，音频用于角色音色参考，视频用于分镜首帧
var (
	uploadImageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true}
	uploadAudioExt = map[string]bool{".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".ogg": true}
	uploadVideoExt = map[string]bool{".mp4": true, ".mov": true, ".webm": true, ".m4v": true}
)

const (
	maxImageSize = 20 << 20  // 20MB
	maxAudioSize = 30 << 20  // 30MB
	maxVideoSize = 200 << 20 // 200MB
)

// Upload POST /api/upload  表单字段 file，可选 kind=image|audio|video（不传按扩展名判断）
func (h *UploadHandler) Upload(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请选择要上传的文件"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	isImage := uploadImageExt[ext]
	isAudio := uploadAudioExt[ext]
	isVideo := uploadVideoExt[ext]
	if !isImage && !isAudio && !isVideo {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "只支持图片（png/jpg/webp/gif）、音频（mp3/wav/m4a/aac/ogg）或视频（mp4/mov/webm）",
		})
		return
	}

	kind := c.PostForm("kind")
	if kind == "audio" && !isAudio {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "该字段需要音频文件"})
		return
	}
	if kind == "image" && !isImage {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "该字段需要图片文件"})
		return
	}
	if kind == "video" && !isVideo {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "该字段需要视频文件"})
		return
	}

	limit := int64(maxImageSize)
	if isAudio {
		limit = maxAudioSize
	}
	if isVideo {
		limit = maxVideoSize
	}
	if file.Size > limit {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("文件过大，上限 %dMB", limit>>20),
		})
		return
	}

	sub := "images"
	kindName := "image"
	if isAudio {
		sub = "audio"
		kindName = "audio"
	}
	if isVideo {
		sub = "videos"
		kindName = "video"
	}
	dir := filepath.Join(h.rootDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建目录失败: " + err.Error()})
		return
	}

	// 文件名带用户前缀，便于排查归属；用时间戳 + 随机串避免覆盖
	name := fmt.Sprintf("%s-%d-%s%s", shortID(userID), time.Now().UnixNano(), idgen.New()[:8], ext)
	dst := filepath.Join(dir, name)

	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存文件失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "上传成功",
		"data": gin.H{
			"url":      h.prefix + "/" + sub + "/" + name,
			"name":     file.Filename,
			"size":     file.Size,
			"kind":     kindName,
			"mimeType": file.Header.Get("Content-Type"),
		},
	})
}

// shortID 取用户 ID 前 8 位，仅用于文件名可读性
func shortID(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "anon"
	}
	return id
}
