package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 上游结果 URL 会 24 小时失效，SUCCESS 后必须立即下载到本地
func TestResultStoreSave(t *testing.T) {
	// 一张 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
		0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	defer srv.Close()

	dir := t.TempDir()
	store := NewResultStore(dir, "/static/results")

	local, err := store.Save(context.Background(), "task-1", srv.URL+"/out.png")
	if err != nil {
		t.Fatalf("转存失败: %v", err)
	}
	if local != "/static/results/task-1.png" {
		t.Fatalf("返回的 URL 不对: %s", local)
	}
	data, err := os.ReadFile(filepath.Join(dir, "task-1.png"))
	if err != nil {
		t.Fatalf("文件未落盘: %v", err)
	}
	if len(data) != len(png) {
		t.Fatalf("文件内容长度不符: %d != %d", len(data), len(png))
	}
}

// 下载失败时保留原始 URL，不能把任务搞挂
func TestResultStoreSaveFallback(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir, "/static/results")

	// 上游 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	local, err := store.Save(context.Background(), "task-2", srv.URL+"/out.png")
	if err == nil {
		t.Fatal("上游 404 应该报错")
	}
	if local != srv.URL+"/out.png" {
		t.Fatalf("失败时应回退原始 URL，实际: %s", local)
	}

	// 占位与本地地址不转存
	for _, u := range []string{"placeholder://image/abc", "/static/results/x.png"} {
		got, err := store.Save(context.Background(), "task-3", u)
		if err != nil || got != u {
			t.Fatalf("%s 不该被转存，got %s err %v", u, got, err)
		}
	}
}

// 没有 content-type 且 URL 无扩展名时按 magic 推断不可靠，至少落到 .bin 且内容完整
func TestResultStoreExtFromHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Write([]byte("fake-mp4"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	store := NewResultStore(dir, "/static/results")
	local, err := store.Save(context.Background(), "task-4", srv.URL+"/download")
	if err != nil {
		t.Fatalf("转存失败: %v", err)
	}
	if !strings.HasSuffix(local, ".mp4") {
		t.Fatalf("应按 Content-Type 推断出 .mp4，实际: %s", local)
	}
}
