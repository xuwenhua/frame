package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
	"jikeai-clone/pkg/aiclient"
)

type AdminHandler struct {
	modelRepo   *repository.ModelConfigRepository
	settingRepo *repository.SettingRepository
	provider    *service.ModelProvider
}

func NewAdminHandler(
	modelRepo *repository.ModelConfigRepository,
	settingRepo *repository.SettingRepository,
	provider *service.ModelProvider,
) *AdminHandler {
	return &AdminHandler{modelRepo: modelRepo, settingRepo: settingRepo, provider: provider}
}

// ==================== 模型配置 ====================

// ListModels 模型列表（可按 category 过滤）
func (h *AdminHandler) ListModels(c *gin.Context) {
	category := c.Query("category")
	list, err := h.modelRepo.List(category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	// 脱敏：API Key 只返回后 4 位
	type item struct {
		model.ModelConfig
		APIKeyMasked string `json:"apiKeyMasked"`
		HasKey       bool   `json:"hasKey"`
	}
	out := make([]item, 0, len(list))
	for _, m := range list {
		it := item{ModelConfig: m}
		if len(m.APIKey) > 4 {
			it.APIKeyMasked = "****" + m.APIKey[len(m.APIKey)-4:]
			it.HasKey = true
		} else if m.APIKey != "" {
			it.APIKeyMasked = "****"
			it.HasKey = true
		}
		it.ModelConfig.APIKey = ""
		out = append(out, it)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": out})
}

// CreateModel 新建模型配置
func (h *AdminHandler) CreateModel(c *gin.Context) {
	var req model.ModelConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if req.DisplayName == "" || req.Category == "" || req.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "名称、分类、厂商必填"})
		return
	}

	req.ID = uuid.NewString()
	req.CreatedAt = time.Now()
	req.UpdatedAt = time.Now()

	if err := h.modelRepo.Create(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	// 若是默认，清掉同分类其他默认
	if req.IsDefault {
		_ = h.modelRepo.ClearDefault(req.Category, req.ID)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": req})
}

// UpdateModel 更新模型配置
func (h *AdminHandler) UpdateModel(c *gin.Context) {
	id := c.Param("id")
	m, err := h.modelRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "模型不存在"})
		return
	}

	var req struct {
		Provider       string `json:"provider"`
		Category       string `json:"category"`
		DisplayName    string `json:"displayName"`
		ModelID        string `json:"modelId"`
		BaseURL        string `json:"baseUrl"`
		APIKey         string `json:"apiKey"`
		Params         string `json:"params"`
		CreditsPerCall *int   `json:"creditsPerCall"`
		CreditsPerUnit *int   `json:"creditsPerUnit"`
		IsDefault      *bool  `json:"isDefault"`
		Enabled        *bool  `json:"enabled"`
		SortOrder      *int   `json:"sortOrder"`
		Remark         string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	if req.Provider != "" {
		m.Provider = req.Provider
	}
	if req.Category != "" {
		m.Category = req.Category
	}
	if req.DisplayName != "" {
		m.DisplayName = req.DisplayName
	}
	if req.ModelID != "" {
		m.ModelID = req.ModelID
	}
	if req.BaseURL != "" {
		m.BaseURL = req.BaseURL
	}
	// API Key 传空字符串表示不改；传 "CLEAR" 表示清空
	if req.APIKey == "CLEAR" {
		m.APIKey = ""
	} else if req.APIKey != "" {
		m.APIKey = req.APIKey
	}
	if req.Params != "" {
		m.Params = req.Params
	}
	// 计费用指针：只传 enabled 之类的局部更新时不清空原有计费
	if req.CreditsPerCall != nil {
		m.CreditsPerCall = *req.CreditsPerCall
	}
	if req.CreditsPerUnit != nil {
		m.CreditsPerUnit = *req.CreditsPerUnit
	}
	if req.IsDefault != nil {
		m.IsDefault = *req.IsDefault
	}
	if req.Enabled != nil {
		m.Enabled = *req.Enabled
	}
	if req.SortOrder != nil {
		m.SortOrder = *req.SortOrder
	}
	if req.Remark != "" {
		m.Remark = req.Remark
	}
	m.UpdatedAt = time.Now()

	if err := h.modelRepo.Update(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	if m.IsDefault {
		_ = h.modelRepo.ClearDefault(m.Category, m.ID)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": m})
}

// DeleteModel 删除
func (h *AdminHandler) DeleteModel(c *gin.Context) {
	id := c.Param("id")
	if err := h.modelRepo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// SetDefault 设为默认
func (h *AdminHandler) SetDefault(c *gin.Context) {
	id := c.Param("id")
	m, err := h.modelRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "模型不存在"})
		return
	}
	m.IsDefault = true
	m.Enabled = true
	m.UpdatedAt = time.Now()
	_ = h.modelRepo.Update(m)
	_ = h.modelRepo.ClearDefault(m.Category, m.ID)

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已设为默认"})
}

// TestModel 测试连通性（文本模型走真实请求）
func (h *AdminHandler) TestModel(c *gin.Context) {
	id := c.Param("id")
	m, err := h.modelRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "模型不存在"})
		return
	}

	if m.APIKey == "" {
		msg := "未配置 API Key"
		if m.Provider == "rh-proxy" {
			msg = "未配置网关 Token"
		}
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": msg, "data": gin.H{"ok": false}})
		return
	}
	if m.BaseURL == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": "未配置 Base URL", "data": gin.H{"ok": false}})
		return
	}

	// RH 中转：打 /health 看账号池，比"配置齐全"更有意义
	if m.Provider == "rh-proxy" {
		gw := aiclient.NewRHGateway(m.BaseURL, m.APIKey)
		start := time.Now()
		health, err := gw.HealthCheck(c.Request.Context())
		latency := time.Since(start).Milliseconds()
		if err != nil {
			_ = h.modelRepo.IncrementCall(id, false)
			c.JSON(http.StatusOK, gin.H{
				"code":    500,
				"message": "中转网关连通失败: " + err.Error(),
				"data":    gin.H{"ok": false},
			})
			return
		}
		_ = h.modelRepo.IncrementCall(id, true)
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "中转网关正常",
			"data": gin.H{
				"ok":        true,
				"provider":  "rh-proxy",
				"modelId":   m.ModelID,
				"latencyMs": latency,
				"accounts":  health.Accounts,
			},
		})
		return
	}

	// 文本模型：真实发一次轻量请求
	if m.Category == "llm" {
		info, err := h.provider.TestLLM(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"code":    500,
				"message": "连通性测试失败: " + err.Error(),
				"data":    gin.H{"ok": false},
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "连通正常", "data": info})
		return
	}

	// 图像/视频/语音模型：暂只做配置校验
	_ = h.modelRepo.IncrementCall(id, true)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "配置完整（该类模型的真实测试将在首次生成时验证）",
		"data":    gin.H{"ok": true, "provider": m.Provider, "modelId": m.ModelID},
	})
}

// ==================== 系统设置 ====================

// ListSettings 设置列表
func (h *AdminHandler) ListSettings(c *gin.Context) {
	group := c.Query("group")
	list, err := h.settingRepo.ListByGroup(group)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": list})
}

// SaveSettings 批量保存
func (h *AdminHandler) SaveSettings(c *gin.Context) {
	var req []struct {
		Key   string `json:"key" binding:"required"`
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	now := time.Now()
	list := make([]model.SystemSetting, 0, len(req))
	for _, r := range req {
		// 保留原有元信息
		exist, _ := h.settingRepo.ListByGroup("")
		var meta model.SystemSetting
		for _, e := range exist {
			if e.Key == r.Key {
				meta = e
				break
			}
		}
		meta.Key = r.Key
		meta.Value = r.Value
		meta.UpdatedAt = now
		list = append(list, meta)
	}

	if err := h.settingRepo.BulkUpsert(list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存"})
}

// ==================== 概览 ====================

// Overview 管理端首页统计
func (h *AdminHandler) Overview(c *gin.Context) {
	models, _ := h.modelRepo.List("")

	var llmCount, imageCount, videoCount, enabledCount int
	var totalCalls, totalFails int64
	for _, m := range models {
		switch m.Category {
		case "llm":
			llmCount++
		case "image":
			imageCount++
		case "video":
			videoCount++
		}
		if m.Enabled {
			enabledCount++
		}
		totalCalls += m.CallCount
		totalFails += m.FailedCount
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"modelTotal":   len(models),
			"modelEnabled": enabledCount,
			"llmCount":     llmCount,
			"imageCount":   imageCount,
			"videoCount":   videoCount,
			"totalCalls":   totalCalls,
			"totalFails":   totalFails,
		},
	})
}
