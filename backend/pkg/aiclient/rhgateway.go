package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RHGateway RunningHub 中转网关客户端
// 协议：POST /task/add → POST /task/status 轮询 → POST /task/outputs 取结果
// 统一响应 {"code":200,"msg":"success","data":...}，code<0 表示各类失败
type RHGateway struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewRHGateway(baseURL, token string) *RHGateway {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "http://localhost"
	}
	return &RHGateway{
		BaseURL: base,
		Token:   token,
		// 单次 HTTP 调用超时，不包含排队时间（排队由 queueTimeoutMs 控制）
		HTTP: NewHTTPClient(120 * time.Second),
	}
}

// TaskResult 网关返回的单条产物
type TaskResult struct {
	URL        string `json:"url"`
	NodeID     string `json:"nodeId"`
	OutputType string `json:"outputType"` // png / mp4 / ...
}

// FlexString 兼容「字符串 / 数字 / 对象 / null」四种写法的字段
//
// 为什么需要：网关的 errorCode 有时是 200 有时是 ""，
// failedReason 有时是 "" 有时是 {}，consumeCoins 有时是 "2" 有时是 2。
// 用强类型会让整个 JSON 解析失败 —— 而解析失败在轮询里会被当成「暂时取不到状态」，
// 于是任务永远停在 QUEUED 直到超时（这是一个真实踩过的坑）。
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	// 字符串直接取
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	// 数字、对象、数组：保留原始文本，不要因为它们让整包解析失败
	var any interface{}
	if err := json.Unmarshal(b, &any); err != nil {
		*f = FlexString(string(b))
		return nil
	}
	if m, ok := any.(map[string]interface{}); ok && len(m) == 0 {
		*f = "" // 空对象（如 failedReason: {}）视为无内容
		return nil
	}
	*f = FlexString(string(b))
	return nil
}

// TaskStatus 任务状态
type TaskStatus struct {
	TaskID    string       `json:"taskId"`
	Status    string       `json:"status"` // QUEUED / RUNNING / SUCCESS / FAILED
	ErrorCode FlexString   `json:"errorCode"`
	ErrorMessage string    `json:"errorMessage"`
	FailedReason FlexString `json:"failedReason"`
	Results   []TaskResult `json:"results"`
	Usage     struct {
		ConsumeCoins FlexString `json:"consumeCoins"`
		TaskCostTime FlexString `json:"taskCostTime"`
	} `json:"usage"`
}

// AddTaskResp 提交任务的返回
type AddTaskResp struct {
	TaskID  string `json:"taskId"`
	Account string `json:"account"`
	Waiting int    `json:"waiting"`
}

// addTaskReq 提交任务请求体
type addTaskReq struct {
	Workflow       string                 `json:"workflow"`
	Inputs         map[string]interface{} `json:"inputs"`
	QueueTimeoutMs int                    `json:"queueTimeoutMs,omitempty"`
}

// envelope 统一响应壳
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// call 发送请求并解包统一响应
func (g *RHGateway) call(ctx context.Context, method, path string, payload interface{}, out interface{}) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接中转网关失败: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("中转网关返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("解析网关响应失败: %s", truncate(string(raw), 200))
	}
	if env.Code != 200 {
		return &GatewayError{Code: env.Code, Msg: env.Msg}
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("解析网关数据失败: %s", truncate(string(raw), 200))
		}
	}
	return nil
}

// GatewayError 网关业务错误（code<0）
type GatewayError struct {
	Code int
	Msg  string
}

func (e *GatewayError) Error() string {
	reason := map[int]string{
		-1: "参数错误",
		-2: "Token 无效",
		-3: "任务不存在",
		-4: "队列已满",
		-5: "排队超时",
		-6: "上游执行错误",
	}
	if r, ok := reason[e.Code]; ok {
		return fmt.Sprintf("%s（code %d）: %s", r, e.Code, e.Msg)
	}
	return fmt.Sprintf("网关返回 code %d: %s", e.Code, e.Msg)
}

// AddTask 提交任务，返回 taskId
func (g *RHGateway) AddTask(ctx context.Context, workflow string, inputs map[string]interface{}, queueTimeoutMs int) (*AddTaskResp, error) {
	req := addTaskReq{Workflow: workflow, Inputs: inputs}
	if queueTimeoutMs > 0 {
		req.QueueTimeoutMs = queueTimeoutMs
	}
	var out AddTaskResp
	if err := g.call(ctx, http.MethodPost, "/task/add", req, &out); err != nil {
		return nil, err
	}
	if out.TaskID == "" {
		return nil, fmt.Errorf("网关未返回 taskId")
	}
	return &out, nil
}

// Status 查询任务状态
func (g *RHGateway) Status(ctx context.Context, taskID string) (*TaskStatus, error) {
	var out TaskStatus
	if err := g.call(ctx, http.MethodPost, "/task/status", map[string]string{"taskId": taskID}, &out); err != nil {
		return nil, err
	}
	if out.TaskID == "" {
		out.TaskID = taskID
	}
	return &out, nil
}

// Outputs 取结果产物（SUCCESS 后调用）
func (g *RHGateway) Outputs(ctx context.Context, taskID string) ([]TaskResult, error) {
	var out []TaskResult
	if err := g.call(ctx, http.MethodPost, "/task/outputs", map[string]string{"taskId": taskID}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Cancel 取消任务
func (g *RHGateway) Cancel(ctx context.Context, taskID string) error {
	var out struct {
		Canceled bool `json:"canceled"`
	}
	return g.call(ctx, http.MethodPost, "/task/cancel", map[string]string{"taskId": taskID}, &out)
}

// Health 账号池状态（用于管理端的连通性测试）
type Health struct {
	OK       bool            `json:"ok"`
	Accounts json.RawMessage `json:"accounts"`
	Raw      string          `json:"raw"`
}

func (g *RHGateway) HealthCheck(ctx context.Context) (*Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.BaseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接中转网关失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("网关健康检查返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 160))
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("解析健康检查响应失败: %s", truncate(string(raw), 160))
	}
	if env.Code != 200 {
		return nil, &GatewayError{Code: env.Code, Msg: env.Msg}
	}

	// /health 的 data 通常是 {ok:true, accounts:[...]}
	var wrap struct {
		OK       bool            `json:"ok"`
		Accounts json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(env.Data, &wrap); err == nil && len(wrap.Accounts) > 0 {
		return &Health{OK: wrap.OK, Accounts: wrap.Accounts, Raw: truncate(string(raw), 400)}, nil
	}
	return &Health{OK: true, Accounts: env.Data, Raw: truncate(string(raw), 400)}, nil
}

// Workflow 工作流定义
type Workflow struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// Workflows 拉取网关可用工作流清单
func (g *RHGateway) Workflows(ctx context.Context) ([]Workflow, error) {
	var raw json.RawMessage
	if err := g.call(ctx, http.MethodGet, "/workflows", nil, &raw); err != nil {
		return nil, err
	}
	// 兼容 ["name"] 与 [{"name":...}] 两种返回
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		out := make([]Workflow, 0, len(names))
		for _, n := range names {
			out = append(out, Workflow{Name: n})
		}
		return out, nil
	}
	var list []Workflow
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("解析工作流清单失败: %s", truncate(string(raw), 160))
	}
	return list, nil
}

// Upload 上传本地文件，返回 1 天有效的 download_url
func (g *RHGateway) Upload(ctx context.Context, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/upload", buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var env struct {
		Code int `json:"code"`
		Data struct {
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("解析上传响应失败: %s", truncate(string(raw), 160))
	}
	if env.Code != 0 && env.Code != 200 {
		return "", fmt.Errorf("上传返回 code %d: %s", env.Code, truncate(string(raw), 160))
	}
	if env.Data.DownloadURL == "" {
		return "", fmt.Errorf("上传未返回 download_url")
	}
	return env.Data.DownloadURL, nil
}

