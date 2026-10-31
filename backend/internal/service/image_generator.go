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

// ImageGenerator 图像生成器（RunningHub / 豆包通用）
type ImageGenerator struct {
	cfg    *model.ModelConfig
	client *http.Client
	// OnExternalTaskID 上游任务 ID 回调（rh-proxy 才有），用于落库以支持重启续查
	OnExternalTaskID func(string)
	// ExistingTaskID 非空时续查该上游任务，不重新提交（重启恢复场景）
	ExistingTaskID string
	// LocalRoot 本地静态目录，参考图是本机文件时先上传给网关
	LocalRoot string
}

func NewImageGenerator(cfg *model.ModelConfig) *ImageGenerator {
	return &ImageGenerator{
		cfg:    cfg,
		client: aiclient.NewHTTPClient(5 * time.Minute),
	}
}

// Generate 生成图片，返回 URL
func (g *ImageGenerator) Generate(ctx context.Context, prompt string) (string, string, error) {
	return g.GenerateWithRefs(ctx, prompt, nil)
}

// GenerateWithRefs 生成图片，可带参考图（仅部分工作流支持）
func (g *ImageGenerator) GenerateWithRefs(ctx context.Context, prompt string, refs []string) (string, string, error) {
	if g.cfg == nil {
		return "", "", fmt.Errorf("未配置图像模型")
	}
	// 没配密钥就直接报错，绝不能拿占位图冒充「生成成功」：
	// 那样用户会看到任务成功、积分被扣、但资产图是空的，比直接失败更难排查
	if strings.TrimSpace(g.cfg.APIKey) == "" {
		return "", "", fmt.Errorf("模型「%s」未配置 API Key / 网关 Token，请到管理端补齐后再生成", g.cfg.DisplayName)
	}

	switch g.cfg.Provider {
	case "rh-proxy":
		gen := NewRHGatewayGenerator(g.cfg)
		gen.OnTaskID = g.OnExternalTaskID
		gen.WithExistingTaskID(g.ExistingTaskID)
		gen.WithLocalRoot(g.LocalRoot)
		return gen.GenerateImage(ctx, prompt, refs)
	case "runninghub":
		return g.callRunningHub(ctx, prompt)
	case "doubao", "openai", "custom":
		return g.callOpenAIImage(ctx, prompt)
	default:
		return g.callOpenAIImage(ctx, prompt)
	}
}

// callOpenAIImage 走 OpenAI /images/generations 协议（豆包也兼容）
func (g *ImageGenerator) callOpenAIImage(ctx context.Context, prompt string) (string, string, error) {
	body := map[string]interface{}{
		"model":  g.cfg.ModelID,
		"prompt": prompt,
		"n":      1,
		"size":   g.imageSize(),
	}
	raw, _ := json.Marshal(body)

	url := g.imageEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("图像模型请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("图像模型返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}

	var parsed struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", "", fmt.Errorf("解析图像响应失败: %s", truncateStr(string(data), 200))
	}
	if len(parsed.Data) == 0 {
		return "", "", fmt.Errorf("图像模型未返回结果")
	}

	out, _ := json.Marshal(parsed)
	return parsed.Data[0].URL, string(out), nil
}

// imageEndpoint 兼容两种填法：BaseURL 只填到 /api/v3，或直接贴完整端点。
// 管理端把 .../images/generations 整段贴进来是最自然的操作，
// 这里不能盲目再拼一次，否则会变成 .../generations/images/generations 直接 404。
func (g *ImageGenerator) imageEndpoint() string {
	u := strings.TrimRight(g.cfg.BaseURL, "/")
	if strings.HasSuffix(u, "/images/generations") {
		return u
	}
	return u + "/images/generations"
}

// imageSize 决定出图尺寸。优先级：参数里写死的 size > 画幅比例换算 > 3:4 默认。
// 豆包/OpenAI 协议只认 size（如 1024x1536），不认 aspectRatio 这种字段。
func (g *ImageGenerator) imageSize() string {
	p := map[string]interface{}{}
	if g.cfg.Params != "" {
		_ = json.Unmarshal([]byte(g.cfg.Params), &p)
	}
	if s, ok := p["size"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	for _, k := range []string{"aspectRatio", "aspect"} {
		if a, ok := p[k].(string); ok {
			if s := SizeForAspect(a); s != "" {
				return s
			}
		}
	}
	return "1024x1536"
}

// SizeForAspect 把 9:16 这类画幅换算成生图接口认的像素尺寸。
// 只覆盖常用档位，换算不出来就返回空串让调用方回退默认值。
func SizeForAspect(aspect string) string {
	switch strings.TrimSpace(aspect) {
	case "9:16":
		return "1024x1792"
	case "3:4":
		return "1024x1536"
	case "2:3":
		return "1024x1536"
	case "16:9":
		return "1792x1024"
	case "4:3":
		return "1536x1024"
	case "1:1":
		return "1024x1024"
	}
	return ""
}

// callRunningHub RunningHub 的图生图/文生图接口
func (g *ImageGenerator) callRunningHub(ctx context.Context, prompt string) (string, string, error) {
	// RunningHub 需要 workflow 或 workflowId，这里用通用的 task 提交方式
	body := map[string]interface{}{
		"apiKey": g.cfg.APIKey,
		"prompt": prompt,
		"model":  g.cfg.ModelID,
	}
	raw, _ := json.Marshal(body)

	// 用户可在 BaseURL 里直接写完整端点
	url := strings.TrimRight(g.cfg.BaseURL, "/")
	if !strings.Contains(url, "/task") && !strings.Contains(url, "/generate") {
		url += "/task/openapi/create"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("RunningHub 请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("RunningHub 返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal(data, &parsed)
	out, _ := json.Marshal(parsed)

	// 尝试多种可能的字段
	for _, key := range []string{"url", "imageUrl", "fileUrl"} {
		if v, ok := parsed[key].(string); ok && v != "" {
			return v, string(out), nil
		}
	}
	if d, ok := parsed["data"].(map[string]interface{}); ok {
		for _, key := range []string{"url", "imageUrl", "fileUrl"} {
			if v, ok := d[key].(string); ok && v != "" {
				return v, string(out), nil
			}
		}
	}

	return "", string(out), fmt.Errorf("未能从响应中提取图片 URL")
}



func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
