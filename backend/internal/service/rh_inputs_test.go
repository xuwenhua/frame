package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// 网关只接受「有值」的节点。
// 2026-10-27 对 124.222.61.207 实测：
//
//	只传 {263,252}                    → 200 有 taskId
//	传 {263,252,259} + 9 个图位 null  → code -6，errorCode=803 JsonNull
//	传 {263,252,51:<公网图>}          → 200
//
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
		"minimax-h3-final":         "259",
		"minimax-h3-remake":        "73",
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
	const root = "/data/results"
	g := NewRHGatewayGenerator(cfgWithParams("")).WithLocalRoot(root)

	// 期望值必须用 filepath.Join 拼，不能写死正斜杠：
	// localPathOf 返回的是「给本机读盘/上传用的原生路径」，
	// 在 Windows 上分隔符是反斜杠。写死 "/data/results/abc.png"
	// 在 macOS 能过、到 Windows 必挂（实测踩过）。
	cases := map[string]string{
		"/static/results/abc.png":             filepath.Join(root, "abc.png"),
		"/static/uploads/images/u-1.png":      filepath.Join(root, "uploads", "images", "u-1.png"),
		"/static/results/uploads/audio/a.wav": filepath.Join(root, "uploads", "audio", "a.wav"),
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

// 7 图工作流（minimax-h3-i2v-7img）的拼装规则。
//
// 这条工作流最容易踩的坑：**259 号节点是「图位」，不是「时长节点」**。
// minimax-h3-final 那套里 259 是 value（时长），这里 259 是第 5 张参考图。
// 一旦有人照着 final 的 spec 复制粘贴，时长数字就会顶掉第 5 张图，
// 而且不报错 —— 只是那一张参考图凭空消失，出片时人物一致性崩掉。
//
// 画幅走 115（默认 "9:16 (Portrait Widescreen)"），时长走 132（默认 3 秒）。
func TestH3SevenImageWorkflow(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-i2v-7img")
	if spec == nil {
		t.Fatal("找不到 minimax-h3-i2v-7img 工作流")
	}
	if spec.Kind != "video" {
		t.Errorf("kind 期望 video，实际 %s", spec.Kind)
	}
	if spec.DurationNode != "132" {
		t.Errorf("时长节点期望 132，实际 %q", spec.DurationNode)
	}
	if spec.AspectNode != "115" {
		t.Errorf("画幅节点期望 115，实际 %q", spec.AspectNode)
	}
	if spec.PromptNode != "265" {
		t.Errorf("提示词节点期望 265，实际 %q", spec.PromptNode)
	}

	refs := []string{"u1", "u2", "u3", "u4", "u5", "u6", "u7"}
	// 故意传 aspect 和 duration，验证它们不会污染 inputs
	inputs, err := BuildRHInputs(spec, "结构化提示词", "9:16", refs, "", 15)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	// 7 张图按顺序进 7 个图位
	slots := []string{"139", "256", "257", "258", "259", "260", "270"}
	for i, node := range slots {
		got, _ := inputs[node].(string)
		if got != refs[i] {
			t.Errorf("图位 %s 期望 %s，实际 %v", node, refs[i], inputs[node])
		}
	}

	// 提示词进 265
	if got, _ := inputs["265"].(string); got != "结构化提示词" {
		t.Errorf("提示词节点 265 期望「结构化提示词」，实际 %v", inputs["265"])
	}

	// 时长进 132（这里传的是 15）
	if got, _ := inputs["132"].(int); got != 15 {
		t.Errorf("时长节点 132 期望 15，实际 %v", inputs["132"])
	}

	// 画幅进 115，且必须转成完整枚举：
	// 裸 "9:16" 会让 ResolutionSelector 类节点抛 KeyError → 上游 805「工作流运行失败」
	if got, _ := inputs["115"].(string); got != "9:16 (Portrait Widescreen)" {
		t.Errorf("画幅节点 115 期望「9:16 (Portrait Widescreen)」，实际 %v", inputs["115"])
	}

	// 不该出现 final 那套的画幅节点 252
	if _, ok := inputs["252"]; ok {
		t.Error("不该出现画幅节点 252（那是 minimax-h3-final 的节点）")
	}

	// 传了 duration=15 也不能把它塞进 259 —— 那里是图位
	if _, isNum := inputs["259"].(int); isNum {
		t.Errorf("259 是图位，被时长数字覆盖了：%v", inputs["259"])
	}

	// 音频位没值就不该出现（传 null/None 上游会报 803）
	for _, n := range []string{"267", "268", "269"} {
		if v, ok := inputs[n]; ok {
			t.Errorf("音频位 %s 没有值却出现在 inputs 里：%v", n, v)
		}
	}

	// 至少 1 张图，一张都没有要报错
	if _, err := BuildRHInputs(spec, "p", "", nil, "", 0); err == nil {
		t.Error("一张参考图都没有时应当报错")
	}

	// 超过 7 张要报错，不能静默丢弃
	eight := append(append([]string{}, refs...), "u8")
	if _, err := BuildRHInputs(spec, "p", "", eight, "", 0); err == nil {
		t.Error("超过 7 张参考图时应当报错")
	}
}

// 画幅简写 → 完整枚举的转换必须覆盖横竖屏，且不传时有默认值兜底。
// 这条工作流的 115 节点默认值就是 "9:16 (Portrait Widescreen)"，
// 说明节点认的是完整写法；只测竖屏会漏掉横屏写法的差异
// （"16:9 (Widescreen)" 而不是 "16:9 (Landscape Widescreen)"，多一个词就报错）。
func TestH3SevenImageAspectVariants(t *testing.T) {
	spec := RHWorkflowByName("minimax-h3-i2v-7img")
	if spec == nil {
		t.Fatal("找不到 minimax-h3-i2v-7img 工作流")
	}
	refs := []string{"u1"}

	cases := []struct {
		want   string // 前端传的简写
		expect string // 期望落到 115 的值
	}{
		{"9:16", "9:16 (Portrait Widescreen)"},
		{"16:9", "16:9 (Widescreen)"},
		{"1:1", "1:1 (Square)"},
		{"3:4", "3:4 (Portrait Standard)"},
		{"", "9:16 (Portrait Widescreen)"}, // 不传 → DefaultAspect 兜底
	}
	for _, c := range cases {
		inputs, err := BuildRHInputs(spec, "p", c.want, refs, "", 0)
		if err != nil {
			t.Errorf("画幅 %q 构建失败: %v", c.want, err)
			continue
		}
		if got, _ := inputs["115"].(string); got != c.expect {
			t.Errorf("画幅 %q 期望 %q，实际 %q", c.want, c.expect, got)
		}
		// 不传时长时用工作流默认 3 秒
		if got, _ := inputs["132"].(int); got != 3 {
			t.Errorf("画幅 %q 时默认时长期望 3，实际 %v", c.want, inputs["132"])
		}
	}
}

// 115 节点是 COMBO 类型，网关侧原样定义：
//
//	["COMBO", {"default": "1:1 (Square)",
//	           "options": ["1:1 (Square)", "2:3 (Portrait Photo)", "3:2 (Photo)",
//	                       "3:4 (Portrait Standard)", "4:3 (Standard)",
//	                       "9:16 (Portrait Widescreen)", "16:9 (Widescreen)",
//	                       "21:9 (Ultrawide)"],
//	           "multiselect": false}]
//
// RHComfyAspectOptions 必须与之逐项一致：少一项、多一项、改一个词，
// NormalizeAspect 都会把合法画幅转成非法值，上游直接 805「工作流运行失败」。
// 这个测试同时验证 8 个选项的「简写 ⇄ 完整写法」往返都能对上。
func TestComfyAspectOptionsMatchGatewayCombo(t *testing.T) {
	want := []string{
		"1:1 (Square)",
		"2:3 (Portrait Photo)",
		"3:2 (Photo)",
		"3:4 (Portrait Standard)",
		"4:3 (Standard)",
		"9:16 (Portrait Widescreen)",
		"16:9 (Widescreen)",
		"21:9 (Ultrawide)",
	}
	if len(RHComfyAspectOptions) != len(want) {
		t.Fatalf("画幅选项数量期望 %d，实际 %d", len(want), len(RHComfyAspectOptions))
	}
	for i := range want {
		if RHComfyAspectOptions[i] != want[i] {
			t.Errorf("第 %d 项期望 %q，实际 %q", i, want[i], RHComfyAspectOptions[i])
		}
	}

	spec := &RHWorkflowSpec{Name: "aspect-test", AspectOptions: RHComfyAspectOptions}
	for _, opt := range want {
		// "9:16 (Portrait Widescreen)" → 简写 "9:16"
		short := strings.SplitN(opt, " ", 2)[0]
		if got := NormalizeAspect(spec, short); got != opt {
			t.Errorf("简写 %q 期望映射到 %q，实际 %q", short, opt, got)
		}
		// 完整写法必须原样返回，不能被二次改写
		if got := NormalizeAspect(spec, opt); got != opt {
			t.Errorf("完整写法 %q 应原样返回，实际 %q", opt, got)
		}
	}
}
