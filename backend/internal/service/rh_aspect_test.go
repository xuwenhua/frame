package service

import "testing"

// NormalizeAspect：前端传简写，节点只认完整枚举
//
// 2026-10-27 实测（node 252 ResolutionSelector）：
//
//	"16:9"                          → KeyError: '16:9'
//	"16:9 (Landscape Widescreen)"   → KeyError
//	"16:9 (Landscape)"              → KeyError
//	"16:9 (Horizontal)"             → KeyError
//	"16:9 (Widescreen)"             → 进入 RUNNING ✅
func TestNormalizeAspect(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-final")

	cases := []struct{ want, expect string }{
		{"16:9", "16:9 (Widescreen)"},
		{"9:16", "9:16 (Portrait Widescreen)"},
		{"1:1", "1:1 (Square)"},
		{"3:4", "3:4 (Portrait Standard)"},
		{"21:9", "21:9 (Ultrawide)"},
		// 已经是对的就原样返回
		{"16:9 (Widescreen)", "16:9 (Widescreen)"},
		{"9:16 (Portrait Widescreen)", "9:16 (Portrait Widescreen)"},
		// 空格要能容忍
		{"  16:9  ", "16:9 (Widescreen)"},
		// 认不出的简写退回默认值（该工作流 DefaultAspect=9:16）
		{"4:5", "9:16 (Portrait Widescreen)"},
		{"  ", "9:16 (Portrait Widescreen)"},
		{"", "9:16 (Portrait Widescreen)"},
	}
	for _, c := range cases {
		if got := NormalizeAspect(spec, c.want); got != c.expect {
			t.Errorf("NormalizeAspect(%q) 期望 %q，实际 %q", c.want, c.expect, got)
		}
	}
}

// 工作流没配枚举时（例如不支持画幅的老工作流）不该乱改用户传的值
func TestNormalizeAspectNoOptions(t *testing.T) {
	spec := RHWorkflowByName("video-to-depth")
	if spec == nil {
		t.Fatal("找不到 video-to-depth")
	}
	if got := NormalizeAspect(spec, "16:9"); got != "16:9" {
		t.Errorf("未配置枚举时应原样返回，实际 %q", got)
	}
	if got := NormalizeAspect(nil, "9:16"); got != "9:16" {
		t.Errorf("spec 为 nil 时应原样返回，实际 %q", got)
	}
}

// 每个挂了画幅节点的工作流都必须配全枚举，否则一定 805
func TestAspectNodeMustHaveOptions(t *testing.T) {
	for _, w := range RHWorkflows {
		if w.AspectNode == "" {
			continue
		}
		if len(w.AspectOptions) == 0 {
			t.Errorf("工作流 %s 有画幅节点 %s 却没配 AspectOptions，上游会 805", w.Name, w.AspectNode)
			continue
		}
		if w.DefaultAspect == "" {
			t.Errorf("工作流 %s 缺 DefaultAspect", w.Name)
			continue
		}
		// 默认值本身也要能折算成功，不能掉到第一项
		if got := NormalizeAspect(&w, w.DefaultAspect); got != NormalizeAspect(&w, "") {
			t.Errorf("工作流 %s 的 DefaultAspect=%s 折算异常：%q", w.Name, w.DefaultAspect, got)
		}
	}
}

// 端到端：BuildRHInputs 出来的画幅必须是枚举里的完整写法
func TestBuildRHInputsAspectIsFullEnum(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-final")
	inputs, err := BuildRHInputs(spec, "p", "16:9", []string{"/a.png"}, "", nil, 0)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	got, _ := inputs["252"].(string)
	valid := false
	for _, o := range RHComfyAspectOptions {
		if o == got {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("画幅 %q 不在合法枚举里，上游会 KeyError", got)
	}
}
