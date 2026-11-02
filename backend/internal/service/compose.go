package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// VideoComposer 用 ffmpeg 把一集的多个分镜片段拼成一条完整视频。
//
// 为什么用 concat demuxer + -c copy：同一工作流出品的片段编码参数一致，
// 直接拼流不重编码 —— 十几秒的片子几百毫秒就出来。重编码要几秒到几十秒，
// 而且每重编码一次就掉一次画质。
//
// 只有参数不一致时（中途换过模型、混了不同分辨率/帧率）才降级重编码，
// 否则 -c copy 会报 "Non-monotonous DTS" 或直接产出花屏。
type VideoComposer struct {
	ffmpeg  string
	ffprobe string
	dir     string
	prefix  string
}

func NewVideoComposer(ffmpegPath, resultDir, urlPrefix string) *VideoComposer {
	fp := strings.TrimSpace(ffmpegPath)
	if fp == "" {
		fp = "ffmpeg" // 走系统 PATH
	}
	// ffprobe 与 ffmpeg 同目录；配的是裸命令名时也让它走 PATH
	probe := "ffprobe"
	if strings.ContainsAny(fp, `/\`) {
		probe = filepath.Join(filepath.Dir(fp), "ffprobe"+exeSuffix())
	}
	return &VideoComposer{
		ffmpeg:  fp,
		ffprobe: probe,
		dir:     resultDir,
		prefix:  strings.TrimRight(urlPrefix, "/"),
	}
}

func exeSuffix() string {
	if filepath.Separator == '\\' {
		return ".exe"
	}
	return ""
}

// Available 检查 ffmpeg 是否可用，管理端/接口可以先探一下给出人话错误
func (c *VideoComposer) Available() bool {
	_, err := exec.LookPath(c.ffmpeg)
	return err == nil
}

// Compose 按给定顺序拼接片段，返回可访问的 URL。
//
// name 是不带扩展名的输出名（调用方用 episode-<id>-<unix> 保证唯一）。
func (c *VideoComposer) Compose(ctx context.Context, name string, inputs []string) (string, error) {
	if len(inputs) == 0 {
		return "", fmt.Errorf("没有可合成的片段")
	}
	if !c.Available() {
		return "", fmt.Errorf("找不到 ffmpeg（%s），请先安装或配置 app.ffmpeg_path", c.ffmpeg)
	}
	for _, in := range inputs {
		if _, err := os.Stat(in); err != nil {
			return "", fmt.Errorf("片段不存在：%s", filepath.Base(in))
		}
	}

	outPath := filepath.Join(c.dir, name+".mp4")
	listPath := filepath.Join(c.dir, name+".concat.txt")

	// concat 列表的路径必须用正斜杠：Windows 的反斜杠会被 ffmpeg 当成转义符，
	// 表现为 "No such file or directory"，但路径肉眼看上去完全正确。
	var b strings.Builder
	for _, in := range inputs {
		p := filepath.ToSlash(in)
		// 单引号是 concat 的定界符，路径里出现就得按 ffmpeg 的规则转义
		p = strings.ReplaceAll(p, "'", `'\''`)
		fmt.Fprintf(&b, "file '%s'\n", p)
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("写 concat 列表失败: %w", err)
	}
	defer os.Remove(listPath)

	// 1. 先试不重编码（快、无损）
	copyErr := c.run(ctx, "-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy", "-movflags", "+faststart", outPath)
	if copyErr == nil {
		return c.prefix + "/" + name + ".mp4", nil
	}

	// 2. 参数不一致才重编码。清掉半成品，避免留下一个坏文件
	_ = os.Remove(outPath)
	reErr := c.run(ctx, "-f", "concat", "-safe", "0", "-i", listPath,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart", outPath)
	if reErr != nil {
		_ = os.Remove(outPath)
		return "", fmt.Errorf("合成失败：拼流 %v；重编码 %v", copyErr, reErr)
	}
	return c.prefix + "/" + name + ".mp4", nil
}

// ProbeDuration 取视频时长（秒），取不到返回 0（不影响主流程）
func (c *VideoComposer) ProbeDuration(ctx context.Context, path string) float64 {
	out, err := exec.CommandContext(ctx, c.ffprobe,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return 0
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return d
}

// run 执行一次 ffmpeg，失败时把 stderr 尾部带出来（ffmpeg 的报错只在这里）
func (c *VideoComposer) run(ctx context.Context, args ...string) error {
	full := append([]string{"-y", "-hide_banner", "-loglevel", "error"}, args...)
	cmd := exec.CommandContext(ctx, c.ffmpeg, full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len([]rune(msg)) > 400 {
			msg = string([]rune(msg)[:400]) + "…"
		}
		if msg == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}
