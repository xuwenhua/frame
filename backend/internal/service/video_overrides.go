package service

import (
	"encoding/json"

	"jikeai-clone/internal/model"
)

// VideoOverrides 分镜工具栏传下来的出片参数
type VideoOverrides struct {
	Duration    int    `json:"duration"`    // 秒，0 表示沿用模型默认
	AspectRatio string `json:"aspectRatio"` // 16:9 / 9:16，空表示沿用模型默认
}

/**
 * ApplyVideoOverrides 把任务级参数合并进模型配置
 *
 * 为什么要拷贝：cfg 是仓储里共享的对象，直接改会把「这一次的选择」
 * 永久写进后续所有任务的默认值（图像任务的画幅覆盖也踩过这个坑）。
 *
 * paramsJSON 为空或解析失败时原样返回，保证老任务行为不变。
 */
func ApplyVideoOverrides(cfg *model.ModelConfig, paramsJSON string) *model.ModelConfig {
	if cfg == nil || paramsJSON == "" {
		return cfg
	}
	var o VideoOverrides
	if err := json.Unmarshal([]byte(paramsJSON), &o); err != nil {
		return cfg
	}
	if o.Duration <= 0 && o.AspectRatio == "" {
		return cfg
	}

	merged := map[string]interface{}{}
	if cfg.Params != "" {
		_ = json.Unmarshal([]byte(cfg.Params), &merged)
	}
	if o.Duration > 0 {
		merged["duration"] = o.Duration
	}
	if o.AspectRatio != "" {
		merged["aspectRatio"] = o.AspectRatio
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return cfg
	}
	cp := *cfg
	cp.Params = string(raw)
	return &cp
}
