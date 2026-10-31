package service

import (
	"encoding/json"
	"testing"

	"jikeai-clone/internal/model"
)

func cfgWithParams(p string) *model.ModelConfig {
	return &model.ModelConfig{ID: "m1", Params: p}
}

func paramInt(t *testing.T, cfg *model.ModelConfig, key string) int {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(cfg.Params), &m); err != nil {
		t.Fatalf("配置不是合法 JSON: %s", cfg.Params)
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func paramStr(t *testing.T, cfg *model.ModelConfig, key string) string {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(cfg.Params), &m); err != nil {
		t.Fatalf("配置不是合法 JSON: %s", cfg.Params)
	}
	s, _ := m[key].(string)
	return s
}

// 工具栏选的时长要覆盖模型默认值，且不能污染原对象
func TestApplyVideoOverridesDuration(t *testing.T) {
	base := cfgWithParams(`{"workflow":"minimax-h3-final","duration":10,"aspectRatio":"9:16"}`)

	out := ApplyVideoOverrides(base, `{"duration":5}`)

	if got := paramInt(t, out, "duration"); got != 5 {
		t.Errorf("duration 期望 5，实际 %d", got)
	}
	// 原配置必须保持不变：同一个 cfg 会被后续任务复用
	if got := paramInt(t, base, "duration"); got != 10 {
		t.Errorf("原配置被污染了：duration 变成 %d", got)
	}
	if out == base {
		t.Error("应当返回拷贝对象，而不是原指针")
	}
}

func TestApplyVideoOverridesAspect(t *testing.T) {
	base := cfgWithParams(`{"workflow":"minimax-h3-final","duration":10,"aspectRatio":"9:16"}`)
	out := ApplyVideoOverrides(base, `{"aspectRatio":"16:9"}`)
	if got := paramStr(t, out, "aspectRatio"); got != "16:9" {
		t.Errorf("aspectRatio 期望 16:9，实际 %s", got)
	}
	if got := paramInt(t, out, "duration"); got != 10 {
		t.Errorf("未覆盖的字段应保持原值，duration 实际 %d", got)
	}
}

// 空参数 / 非法 JSON / 全零值：原样返回，老任务行为不变
func TestApplyVideoOverridesNoop(t *testing.T) {
	base := cfgWithParams(`{"duration":10}`)

	if out := ApplyVideoOverrides(base, ""); out != base {
		t.Error("空参数应原样返回")
	}
	if out := ApplyVideoOverrides(base, "不是 json"); out != base {
		t.Error("非法 JSON 应原样返回")
	}
	if out := ApplyVideoOverrides(base, `{"duration":0}`); out != base {
		t.Error("全零值应原样返回")
	}
	if out := ApplyVideoOverrides(nil, `{"duration":5}`); out != nil {
		t.Error("nil 配置应原样返回")
	}
}

// 模型 params 为空时也要能补上覆盖值
func TestApplyVideoOverridesEmptyModelParams(t *testing.T) {
	base := cfgWithParams("")
	out := ApplyVideoOverrides(base, `{"duration":5,"aspectRatio":"16:9"}`)
	if got := paramInt(t, out, "duration"); got != 5 {
		t.Errorf("duration 期望 5，实际 %d", got)
	}
	if got := paramStr(t, out, "aspectRatio"); got != "16:9" {
		t.Errorf("aspectRatio 期望 16:9，实际 %s", got)
	}
}
