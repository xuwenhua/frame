package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/aiclient"
)

// VideoGenerator 视频生成器（豆包 Seedance 异步任务模式）
type VideoGenerator struct {
	cfg    *model.ModelConfig
	client *http.Client
	// LocalRoot 本地静态目录，首帧等参考图是本机文件时先上传给网关
	LocalRoot string
	// OnExternalTaskID 拿到上游 taskId 时回调，落库后重启可续查
	OnExternalTaskID func(string)
}

func NewVideoGenerator(cfg *model.ModelConfig) *VideoGenerator {
	return &VideoGenerator{
		cfg:    cfg,
		client: aiclient.NewHTTPClient(10 * time.Minute),
	}
}

// Generate 提交任务并轮询直至完成，返回视频 URL
//
// refs 是这一镜里 @ 到的角色 / 场景 / 道具参考图（首张进首帧位）。
// 只传首帧会让模型对其他资产没有视觉参照，提示词里的 @ 就只是文字。
func (g *VideoGenerator) Generate(ctx context.Context, prompt string, refs []string) (string, string, error) {
	if g.cfg == nil {
		return "", "", fmt.Errorf("未配置视频模型")
	}
	// 同图像：没配密钥直接失败，不用占位视频假成功
	if strings.TrimSpace(g.cfg.APIKey) == "" {
		return "", "", fmt.Errorf("模型「%s」未配置 API Key / 网关 Token，请到管理端补齐后再生成", g.cfg.DisplayName)
	}

	// 清掉空值，避免把空串塞进图位
	cleaned := make([]string, 0, len(refs))
	for _, r := range refs {
		if strings.TrimSpace(r) != "" {
			cleaned = append(cleaned, r)
		}
	}
	firstFrame := ""
	if len(cleaned) > 0 {
		firstFrame = cleaned[0]
	}

	switch g.cfg.Provider {
	case "rh-proxy":
		gen := NewRHGatewayGenerator(g.cfg).WithLocalRoot(g.LocalRoot)
		gen.OnTaskID = g.OnExternalTaskID
		// 多图链路：H3 有 9 个图位，第 1 张进首帧位，其余进参考位
		return gen.GenerateVideoWithRefs(ctx, prompt, cleaned, "")
	case "doubao":
		return g.callDoubao(ctx, prompt, firstFrame)
	default:
		return g.callDoubao(ctx, prompt, firstFrame)
	}
}

// callDoubao 豆包视频生成：POST /contents/generations/tasks → GET 轮询
func (g *VideoGenerator) callDoubao(ctx context.Context, prompt string, firstFrame string) (string, string, error) {
	base := strings.TrimRight(g.cfg.BaseURL, "/")

	// 1. 提交任务
	content := []map[string]interface{}{
		{"type": "text", "text": prompt},
	}
	if firstFrame != "" {
		content = append(content, map[string]interface{}{
			"type":      "image_url",
			"image_url": map[string]string{"url": firstFrame},
		})
	}

	body := map[string]interface{}{
		"model":     g.cfg.ModelID,
		"content":   content,
		"ratio":     "9:16",
		"duration":  13,
		"watermark": false,
	}
	raw, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/contents/generations/tasks", bytes.NewReader(raw))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("提交视频任务失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("提交视频任务返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}

	var submitResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &submitResp); err != nil || submitResp.ID == "" {
		return "", "", fmt.Errorf("未能获取任务 ID: %s", truncateStr(string(data), 200))
	}

	// 2. 轮询结果
	taskID := submitResp.ID
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	deadline := time.Now().Add(9 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("视频生成超时")
		case <-ticker.C:
			if time.Now().After(deadline) {
				return "", "", fmt.Errorf("视频生成超时（任务 %s）", taskID)
			}

			url, done, err := g.queryTask(ctx, base, taskID)
			if err != nil {
				continue // 单次查询失败不算致命
			}
			if done {
				out, _ := json.Marshal(map[string]string{"taskId": taskID, "url": url})
				return url, string(out), nil
			}
		}
	}
}

// queryTask 查询任务状态
func (g *VideoGenerator) queryTask(ctx context.Context, base, taskID string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/contents/generations/tasks/"+taskID, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("查询返回 %d", resp.StatusCode)
	}

	var parsed struct {
		Status string `json:"status"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", false, err
	}

	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", false, fmt.Errorf("%s", parsed.Error.Message)
	}

	switch parsed.Status {
	case "succeeded", "success":
		if parsed.Content.VideoURL == "" {
			return "", false, fmt.Errorf("任务成功但无视频地址")
		}
		return parsed.Content.VideoURL, true, nil
	case "failed", "cancelled":
		return "", false, fmt.Errorf("任务失败")
	default:
		return "", false, nil // 仍在处理
	}
}

