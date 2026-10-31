package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// 网关只接受「有值」的节点。
// 2026-10-27 对 124.222.61.207 实测：
//   只传 {263,252}                    → 200 有 taskId
//   传 {263,252,259} + 9 个图位 null  → code -6，errorCode=803 JsonNull
//   传 {263,252,51:<公网图>}          → 200
// 所以组装 inputs 时绝不能塞 null 占位。
func TestBuildRHInputsNoNullPlaceholders(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-final")
	if spec == nil {
		t.Fatal("找不到 minimax-h3-final")
	}

	inputs, err := BuildRHInputs(spec, "一个人说话", "9:16", []string{"/static/results/a.png"}, "", 0)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	// 有值的图位要填
	if got, _ := inputs["51"].(string); got != "/static/results/a.png" {
		t.Errorf("51 号图位应为首帧图，实际 %v", inputs["51"])
	}
	// 没值的图位、音频位、视频位都不能出现
	for node, v := range inputs {
		if v == nil {
			t.Errorf("节点 %s 传了 null，上游会报 803 JsonNull", node)
		}
	}
	for _, node := range []string{"49", "50", "43", "19", "23", "199", "200", "201", "48", "14", "15", "27", "25", "26"} {
		if _, exists := inputs[node]; exists {
			t.Errorf("未使用的节点 %s 不应出现在 inputs 里", node)
		}
	}

	raw, _ := json.Marshal(inputs)
	if strings.Contains(string(raw), "null") {
		t.Errorf("序列化后仍有 null: %s", raw)
	}
}

// 259 是网关的 value 节点（时长）。不传就出 1.3 秒的片子。
func TestBuildRHInputsSendsDuration(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-final")
	inputs, err := BuildRHInputs(spec, "p", "9:16", []string{"/a.png"}, "", 10)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if got, _ := inputs["259"].(int); got != 10 {
		t.Errorf("时长节点 259 期望 10，实际 %v", inputs["259"])
	}

	// 不传时长时走工作流默认值，不能是 0
	inputs, _ = BuildRHInputs(spec, "p", "9:16", []string{"/a.png"}, "", 0)
	if got, _ := inputs["259"].(int); got <= 0 {
		t.Errorf("默认时长不应为 %v", inputs["259"])
	}
}

// 每个 value 类节点（网关里的数值位）都必须在 spec 里映射到 DurationNode，
// 否则该工作流的时长完全不受控。
func TestValueNodeMappedToDuration(t *testing.T) {
	want := map[string]string{
		"minimax-h3-final":        "259",
		"minimax-h3-remake":       "73",
		"minimax-h3-digital-human": "73",
	}
	for name, node := range want {
		spec := RHWorkflowByName(name)
		if spec == nil {
			t.Errorf("找不到工作流 %s", name)
			continue
		}
		if spec.DurationNode != node {
			t.Errorf("%s 的 DurationNode 期望 %s，实际 %q", name, node, spec.DurationNode)
		}
	}
}

// 多张参考图要按 ImageNodes 顺序依次填进图位。
// 只填第一张时，提示词里 @ 到的其他角色/道具对模型完全不可见，
// 形象一致性只能靠文字现编 —— 这是真实踩过的坑（视频任务 params 里
// 有 6 张 refImages，上游却只收到 1 张）。
func TestBuildRHInputsFillsAllImageNodes(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-final")
	if spec == nil {
		t.Fatal("找不到 minimax-h3-final")
	}

	refs := []string{"/a.png", "/b.png", "/c.png", "/d.png"}
	inputs, err := BuildRHInputs(spec, "p", "9:16", refs, "", 5)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	for i, want := range refs {
		node := spec.ImageNodes[i]
		if got, _ := inputs[node].(string); got != want {
			t.Errorf("%s 号图位期望 %s，实际 %v", node, want, inputs[node])
		}
	}
	// 空串不能占位
	inputs, _ = BuildRHInputs(spec, "p", "9:16", []string{"/a.png", "", "/c.png"}, "", 5)
	if _, exists := inputs["49"]; exists {
		t.Error("空参考图不应占用 49 号图位")
	}
	if got, _ := inputs["50"].(string); got != "/c.png" {
		t.Errorf("跳过的空位之后，50 号应填第三张图，实际 %v", inputs["50"])
	}
}

// 必填图不足要拦在前端侧，别把无效请求丢给上游
func TestBuildRHInputsRequiresImage(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-final")
	if _, err := BuildRHInputs(spec, "p", "9:16", nil, "", 0); err == nil {
		t.Error("缺少参考图时应当报错")
	}
}

// 数字人：时长走 73、画幅走 78，输入视频必填
func TestBuildRHInputsDigitalHuman(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-digital-human")
	if spec == nil {
		t.Fatal("找不到 minimax-h3-digital-human")
	}
	inputs, err := BuildRHInputs(spec, "p", "16:9", []string{"/a.png", "/b.png"}, "https://x/v.mp4", 7)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if got, _ := inputs["73"].(int); got != 7 {
		t.Errorf("时长节点 73 期望 7，实际 %v", inputs["73"])
	}
	if got, _ := inputs["78"].(string); got != "16:9 (Widescreen)" {
		t.Errorf("画幅节点 78 期望 16:9 (Widescreen)，实际 %v", inputs["78"])
	}
	if got, _ := inputs["102"].(string); got == "" {
		t.Error("输入视频 102 未填")
	}

	// 缺输入视频必须报错（以前会静默传空串）
	if _, err := BuildRHInputs(spec, "p", "16:9", []string{"/a.png", "/b.png"}, "", 0); err == nil {
		t.Error("数字人缺少输入视频时应当报错")
	}
}

// /static/xxx → 磁盘路径映射：产物与用户上传都在同一根目录下
func TestLocalPathOf(t *testing.T) {
	g := NewRHGatewayGenerator(cfgWithParams("")).WithLocalRoot("/data/results")

	cases := map[string]string{
		"/static/results/abc.png":            "/data/results/abc.png",
		"/static/uploads/images/u-1.png":     "/data/results/uploads/images/u-1.png",
		"/static/results/uploads/audio/a.wav": "/data/results/uploads/audio/a.wav",
	}
	for ref, want := range cases {
		got, err := g.localPathOf(ref)
		if err != nil {
			t.Fatalf("%s: 意外错误 %v", ref, err)
		}
		if got != want {
			t.Errorf("%s 期望 %s，实际 %s", ref, want, got)
		}
	}

	// 没配本地目录时要明确报错，而不是拼出个错误路径
	empty := NewRHGatewayGenerator(cfgWithParams(""))
	if _, err := empty.localPathOf("/static/results/a.png"); err == nil {
		t.Error("未配置 LocalRoot 时应当报错")
	}
}
