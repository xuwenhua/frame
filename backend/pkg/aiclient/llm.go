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
	// Format 协议格式：
	//   ""（默认）OpenAI 兼容 /chat/completions
	//   "gemini"  Google 原生 generateContent（opencode.ai 的中转就是这种，
	//            没有 OpenAI 兼容端点，必须走原生格式）
	Format string
	// SessionHeader Gemini 中转要求的会话头（x-opencode-session），留空则不发送
	SessionHeader string
	// UserAgent 有些中转按 UA 限流/放行
	UserAgent string
}

func NewLLMClient(baseURL, apiKey, model string) *LLMClient {
	return &LLMClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    NewHTTPClient(120 * time.Second),
	}
}

// ChatMessage 一条对话消息。
//
// Content 用 any：OpenAI 兼容协议里它可以是纯字符串，
// 也可以是多模态数组（文本 + 图片）。视觉模型必须走后者 ——
// 只能传 string 的话，模型永远看不到参考图。
type ChatMessage struct {
	Role    string `json:"role"` // system / user / assistant
	Content any    `json:"content"`
}

// ContentPart 多模态消息里的一段内容
type ContentPart struct {
	Type     string    `json:"type"` // text / image_url
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 图片来源：http(s) 公网地址，或 data:image/jpeg;base64,xxx
type ImageURL struct {
	URL string `json:"url"`
}

// TextPart 构造一段文本
func TextPart(s string) ContentPart {
	return ContentPart{Type: "text", Text: s}
}

// ImagePart 构造一段图片。url 传 data URL 最稳：
// 我们的图存在本机 /static 下，远端模型服务下载不到这个相对路径。
func ImagePart(url string) ContentPart {
	return ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: url}}
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

	if c.Format == "gemini" {
		return c.chatGemini(ctx, messages, temperature, maxTokens)
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

// ---- Gemini 原生协议 ----

type geminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *geminiBlob `json:"inlineData,omitempty"`
}

type geminiBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig *struct {
		Temperature     float64 `json:"temperature,omitempty"`
		MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	} `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// chatGemini 走 Google 原生 generateContent。
//
// opencode.ai 的中转只暴露原生端点，没有 /v1/chat/completions，
// 所以 OpenAI 那套 body 发过去是 404。这里做格式转换：
//
//	messages(role/content) → contents(role/parts)
//	image_url(data URL) → inlineData(base64)
//	candidates[0].content.parts[*].text → ChatResult.Content
func (c *LLMClient) chatGemini(ctx context.Context, messages []ChatMessage, temperature float64, maxTokens int) (*ChatResult, error) {
	var contents []geminiContent
	for _, m := range messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		if role != "user" && role != "model" {
			// system 提示词 Gemini 走 systemInstruction，这里简单并入首个 user 段
			role = "user"
		}

		var parts []geminiPart
		switch v := m.Content.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				parts = append(parts, geminiPart{Text: v})
			}
		case []ContentPart:
			for _, cp := range v {
				if cp.Type == "text" && strings.TrimSpace(cp.Text) != "" {
					parts = append(parts, geminiPart{Text: cp.Text})
					continue
				}
				if cp.Type == "image_url" && cp.ImageURL != nil {
					// data:image/jpeg;base64,xxxx
					u := cp.ImageURL.URL
					if i := strings.Index(u, "base64,"); i > 0 {
						mime := strings.TrimPrefix(u[:i], "data:")
						mime = strings.TrimSuffix(mime, ";")
						parts = append(parts, geminiPart{InlineData: &geminiBlob{
							MimeType: mime,
							Data:     u[i+len("base64,"):],
						}})
					}
				}
			}
		}
		if len(parts) > 0 {
			contents = append(contents, geminiContent{Role: role, Parts: parts})
		}
	}

	body := geminiRequest{Contents: contents}
	if temperature > 0 || maxTokens > 0 {
		body.GenerationConfig = &struct {
			Temperature     float64 `json:"temperature,omitempty"`
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		}{Temperature: temperature, MaxOutputTokens: maxTokens}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/v1beta/models/" + c.Model + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)
	if c.SessionHeader != "" {
		req.Header.Set("x-opencode-session", c.SessionHeader)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型返回 %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if gr.Error != nil {
		return nil, fmt.Errorf("模型报错: %s", gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("模型未返回内容")
	}

	var sb strings.Builder
	for _, p := range gr.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}

	return &ChatResult{
		Content:      sb.String(),
		PromptTokens: gr.UsageMetadata.PromptTokenCount,
		OutputTokens: gr.UsageMetadata.CandidatesTokenCount,
		TotalTokens:  gr.UsageMetadata.TotalTokenCount,
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}
