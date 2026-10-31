package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jikeai-clone/pkg/aiclient"
)

// ResultStore 生成产物转存
//
// 为什么必须转存：RunningHub 网关明确说明「结果 URL 24 小时失效，SUCCESS 后立即下载转存」。
// 直接把上游临时 URL 存库，第二天资产图就全是裂图；
// 而且部分对象存储直链带防盗链 / 需要签名，浏览器端不一定能加载。
//
// 转存后库里存的是本站相对路径（默认 /static/results/xxx.png），
// 同源访问，不依赖上游时效。
type ResultStore struct {
	dir    string
	prefix string
	client *http.Client
}

func NewResultStore(dir, prefix string) *ResultStore {
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join(os.TempDir(), "jikeai-results")
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = "/static/results"
	}
	_ = os.MkdirAll(dir, 0o755)
	return &ResultStore{
		dir:    dir,
		prefix: strings.TrimRight(prefix, "/"),
		client: aiclient.NewHTTPClient(2 * time.Minute),
	}
}

// Dir 本地目录（用于挂静态路由）
func (s *ResultStore) Dir() string { return s.dir }

// Prefix 对外访问前缀
func (s *ResultStore) Prefix() string { return s.prefix }

// extFromURL 从 URL 路径推断扩展名
func extFromURL(u string) string {
	p := u
	if i := strings.IndexAny(p, "?#"); i > 0 {
		p = p[:i]
	}
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".mp4", ".webm", ".mov", ".mp3", ".wav":
		return ext
	}
	return ""
}

// Save 把上游产物下载到本地，返回可直接给前端用的 URL
// 下载失败时返回原始 URL（不阻断任务，至少让用户还能点开原始链接）
func (s *ResultStore) Save(ctx context.Context, taskID, srcURL string) (string, error) {
	srcURL = strings.TrimSpace(srcURL)
	if srcURL == "" {
		return "", fmt.Errorf("产物 URL 为空")
	}
	// 本地/占位地址不需要转存
	if strings.Contains(srcURL, "placeholder://") || strings.HasPrefix(srcURL, s.prefix) {
		return srcURL, nil
	}
	if !strings.HasPrefix(srcURL, "http://") && !strings.HasPrefix(srcURL, "https://") {
		return srcURL, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return srcURL, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return srcURL, fmt.Errorf("下载产物失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return srcURL, fmt.Errorf("下载产物失败: HTTP %d", resp.StatusCode)
	}

	ext := extFromURL(srcURL)
	if ext == "" {
		switch strings.ToLower(resp.Header.Get("Content-Type")) {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/webp":
			ext = ".webp"
		case "video/mp4":
			ext = ".mp4"
		case "video/webm":
			ext = ".webm"
		case "audio/mpeg":
			ext = ".mp3"
		default:
			ext = ".bin"
		}
	}

	name := taskID + ext
	dst := filepath.Join(s.dir, name)

	tmp, err := os.CreateTemp(s.dir, taskID+"-*.part")
	if err != nil {
		return srcURL, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath)
	}()

	// 产物可能较大（视频），限 512MB，避免异常响应打满磁盘
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return srcURL, fmt.Errorf("写入产物失败: %w", err)
	}
	if n == 0 {
		return srcURL, fmt.Errorf("下载到的产物为空")
	}
	if err := tmp.Close(); err != nil {
		return srcURL, err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return srcURL, fmt.Errorf("保存产物失败: %w", err)
	}

	return s.prefix + "/" + name, nil
}
