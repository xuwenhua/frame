package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLMClient OpenAI 兼容协议的对话客户端
// 国内主流厂商（DeepSeek、通义、豆包、Moonshot）都兼容这套接口，
// 只需在管理端配置不同的 BaseURL 与模型 ID。
type LLMClient struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func NewLLMClient(baseURL, apiKey, model string) *LLMClient {
	return &LLMClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    NewHTTPClient(120 * time.Second),
	}
}

type ChatMessage struct {
	Role    string `json:"role"` // system / user / assistant
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatResult 调用结果
type ChatResult struct {
	Content      string
	PromptTokens int
	OutputTokens int
	TotalTokens  int
	LatencyMs    int64
}

// Chat 发起一次对话补全
func (c *LLMClient) Chat(ctx context.Context, messages []ChatMessage, temperature float64, maxTokens int) (*ChatResult, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("未配置 API Key")
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("未配置 Base URL")
	}

	body := chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      false,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求模型失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	latency := time.Since(start).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型返回 %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("解析响应失败: %s", truncate(string(respBody), 200))
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("模型错误: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("模型未返回内容")
	}

	return &ChatResult{
		Content:      parsed.Choices[0].Message.Content,
		PromptTokens: parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		TotalTokens:  parsed.Usage.TotalTokens,
		LatencyMs:    latency,
	}, nil
}

// Ping 连通性测试：发一句最短请求
func (c *LLMClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := c.Chat(ctx, []ChatMessage{
		{Role: "user", Content: "回复 OK"},
	}, 0, 16)
	return err
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
