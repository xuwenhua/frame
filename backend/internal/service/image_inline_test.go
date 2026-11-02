package service

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 视觉模型要的是 data URL：我们的图存在本机 /static 下，远端服务下载不到，
// 只能压成 base64 内联。这里验证压缩后确实是合法 data URL 且体积可控。
func TestInlineImage(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("环境里没有 ffmpeg，跳过")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")

	// 造一张 1024×1536 的图（和实际资产图尺寸同量级）
	mk := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=red:s=1024x1536", "-frames:v", "1", src)
	if err := mk.Run(); err != nil {
		t.Fatalf("生成测试图失败: %v", err)
	}

	dataURL, err := InlineImage("ffmpeg", src, 768)
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/jpeg;base64,") {
		t.Errorf("前缀不对: %.40s", dataURL)
	}
	// base64 会把体积放大 1.33 倍。单张超过 400KB 说明没压住，
	// 一次传 3-6 张就会把请求体撑到几 MB
	if n := len(dataURL); n > 400*1024 {
		t.Errorf("单张 data URL 太大：%d 字节", n)
	}
	t.Logf("data URL 长度 %d 字节", len(dataURL))
}
