package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InlineImage 把图片压到指定宽度并转成 data URL（JPEG base64）。
// src 可以是本地磁盘路径，也可以是 http(s) 外链。
//
// 为什么必须压：原图 2–4MB，base64 后体积再涨 33%，一次传 3 张就是 10MB+
// 的请求体，视觉模型接口基本都会拒。压到 768px 宽、q=5（约 80 分）
// 后单张 60–120KB，模型看清楚「什么家具、几个人、什么朝向」完全够用。
//
// 为什么用 data URL 而不是直接把地址给模型：资产图多半存在本机
// /static/results 下，是个相对路径，远端服务既访问不到也不是合法 URL。
func InlineImage(ffmpegPath, src string, maxWidth int) (string, error) {
	if maxWidth <= 0 {
		maxWidth = 768
	}
	fp := strings.TrimSpace(ffmpegPath)
	if fp == "" {
		fp = "ffmpeg" // 走系统 PATH
	}

	path := src
	// 外链先落盘：让 ffmpeg 直接读 http 会走系统代理，沙箱里经常被拦，
	// 失败时只报一句 "Server returned 403"，很难查。
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		tmp, err := downloadTemp(src)
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp)
		path = tmp
	}

	cmd := exec.Command(fp,
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", path,
		"-vf", fmt.Sprintf("scale=%d:-2", maxWidth),
		"-q:v", "5",
		"-f", "image2",
		"-c:v", "mjpeg",
		"pipe:1",
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		msg := strings.TrimSpace(stderr.String())
		if len([]rune(msg)) > 200 {
			msg = string([]rune(msg)[:200]) + "…"
		}
		return "", fmt.Errorf("压缩图片失败 %s: %v %s", filepath.Base(src), err, msg)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

// downloadTemp 把外链图片下载到临时文件，调用方负责删除
func downloadTemp(url string) (string, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
		// 直连：对象存储（COS 等）不需要代理，走代理解析反而容易失败
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("下载参考图失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载参考图返回 %d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "inline-*.img")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
