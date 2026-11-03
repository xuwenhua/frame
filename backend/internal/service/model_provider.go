package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/pkg/aiclient"
)

// ModelProvider 按分类取管理端配置的默认模型，构造对应客户端
type ModelProvider struct {
	modelRepo *repository.ModelConfigRepository
}

func NewModelProvider(r *repository.ModelConfigRepository) *ModelProvider {
	return &ModelProvider{modelRepo: r}
}

// LLM 取默认文本模型
//
// 默认模型没配 Key 时要自动换一个可用的，不能直接报错：
// 上层（分镜生成）拿到 error 会静默降级到规则引擎，
// 用户看到的是「生成成功」但内容全是抠句子 —— 比直接失败更难排查。
func (p *ModelProvider) LLM() (*aiclient.LLMClient, *model.ModelConfig, error) {
	cfg, err := p.modelRepo.GetDefault("llm")
	if err == nil && cfg != nil && usableLLM(cfg) {
		return buildLLMClient(cfg), cfg, nil
	}

	list, lerr := p.modelRepo.List("llm")
	if lerr == nil {
		for i := range list {
			if c := &list[i]; usableLLM(c) {
				return buildLLMClient(c), c, nil
			}
		}
	}
	return nil, cfg, errors.New("没有可用的文本模型（需要已启用，且配好 Base URL 与 API Key）")
}

// buildLLMClient 按 provider 选协议。
//
// 绝大多数中转是 OpenAI 兼容（/chat/completions），但 opencode.ai 的 Gemini
// 只有 Google 原生 generateContent，没有兼容端点 —— 用同一套 body 发过去是 404。
// 这里根据 provider 标记切到对应实现，调用方不用关心。
func buildLLMClient(cfg *model.ModelConfig) *aiclient.LLMClient {
	c := aiclient.NewLLMClient(cfg.BaseURL, cfg.APIKey, cfg.ModelID)
	if strings.Contains(strings.ToLower(cfg.Provider), "gemini") ||
		strings.Contains(strings.ToLower(cfg.Provider), "google") ||
		strings.Contains(strings.ToLower(cfg.Provider), "opencode") {
		c.Format = "gemini"
		c.SessionHeader = "ses_" + cfg.ID[:8]
		c.UserAgent = "opencode/2.2.0"
	}
	return c
}

func usableLLM(c *model.ModelConfig) bool {
	return c != nil && c.Enabled &&
		strings.TrimSpace(c.APIKey) != "" &&
		strings.TrimSpace(c.BaseURL) != ""
}

// LLMByID 按指定 ID 取文本模型（用于测试连通性）
func (p *ModelProvider) LLMByID(id string) (*aiclient.LLMClient, *model.ModelConfig, error) {
	cfg, err := p.modelRepo.GetByID(id)
	if err != nil {
		return nil, nil, errors.New("模型不存在")
	}
	if cfg.APIKey == "" {
		return nil, cfg, errors.New("未配置 API Key")
	}
	if cfg.BaseURL == "" {
		return nil, cfg, errors.New("未配置 Base URL")
	}
	return buildLLMClient(cfg), cfg, nil
}

// TestLLM 真实发起一次轻量请求验证连通性
func (p *ModelProvider) TestLLM(ctx context.Context, id string) (map[string]interface{}, error) {
	client, cfg, err := p.LLMByID(id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	start := time.Now()
	res, err := client.Chat(ctx, []aiclient.ChatMessage{
		{Role: "user", Content: "回复两个字：可用"},
	}, 0, 20)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		_ = p.modelRepo.IncrementCall(id, false)
		return nil, err
	}

	_ = p.modelRepo.IncrementCall(id, true)

	return map[string]interface{}{
		"ok":        true,
		"provider":  cfg.Provider,
		"modelId":   cfg.ModelID,
		"latencyMs": latency,
		"reply":     truncateRunes(res.Content, 50),
		"tokens":    res.TotalTokens,
	}, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Describe 给前端展示的模型摘要
func Describe(cfg *model.ModelConfig) string {
	if cfg == nil {
		return ""
	}
	return fmt.Sprintf("%s (%s)", cfg.DisplayName, cfg.ModelID)
}
