package service

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"jikeai-clone/internal/model"
)

// 这些用例断言的是「场次/镜号」旧格式，跑测试时把六段式关掉。
// 六段式有独立的 ref2va_prompt_test.go 覆盖。
func TestMain(m *testing.M) {
	UseRef2VA = false
	os.Exit(m.Run())
}

func sampleDraft() FrameDraft {
	return FrameDraft{
		FrameIndex: 1,
		Roles: []model.Character{
			{Name: "女主", IdentityAnchor: "女性，28岁", VoiceTone: "亲切年轻女音", Personality: "热情"},
			{Name: "男主", IdentityAnchor: "男性，30岁", VoiceTone: "松弛年轻男音"},
		},
		Scenes: []model.Scene{{Name: "家居客厅-白天", TimeOfDay: "白天", SpatialAnchor: "浅灰布艺沙发与木质茶几"}},
		Items:  []model.Item{{Name: "面霜", Prompt: "玻璃罐装面霜"}},
		Situation: "白天，室内，@家居客厅-白天。阳光从窗帘洒入客厅，柔和漫射在浅灰布艺沙发与木质茶几上。" +
			"@女主 坐在沙发靠茶几的一端（画面左侧）；@男主 坐在沙发另一端（画面右侧）。",
		ShotSize:    "双人中景",
		Camera:      "缓慢推镜",
		Action:      "柔和日光漫洒在客厅中。@女主 伸手拿起 @面霜，举到能看清的高度，笑着看向 @男主。",
		SFX:         "拿起玻璃罐面霜的轻微触碰声",
		Performance: "@女主 起始欣喜、满意；@男主 起初好奇、半信半疑。",
		Locking:     "@女主 始终在画面左侧，@男主 始终在画面右侧。",
		Dialogues: []FrameDialogue{
			{Role: "女主", Tone: "满带欣喜，语调轻快而笃定", Line: "这个面霜真的好用，我用了一个月，皮肤白了一个度。"},
		},
	}
}

// sampleMultiShotDraft 对应参考站真实结构：一帧三镜，时间累计到 13 秒
func sampleMultiShotDraft() FrameDraft {
	d := sampleDraft()
	d.Shots = []FrameShot{
		{
			Seconds:  5,
			ShotSize: "双人中景",
			Action:   "@女主 拿起 @面霜，举到画面右侧，笑着看向 @男主。",
			Dialogues: []FrameDialogue{
				{Role: "女主", Tone: "满带欣喜，语调轻快而笃定", Line: "这个面霜真的好用，我用了一个月，皮肤白了一个度。"},
			},
			SFX: "拿起玻璃罐面霜的轻微触碰声",
		},
		{
			Seconds:  3,
			ShotSize: "中近景",
			Action:   "@男主 的视线从 @面霜 移向 @女主 的脸颊，身子微前倾，眉头微扬。",
			Dialogues: []FrameDialogue{
				{Role: "男主", Tone: "带着好奇与轻微怀疑，语速稍快", Line: "真的假的？我也试试。"},
			},
		},
		{
			Seconds:  5,
			ShotSize: "双人中景",
			Camera:   "缓慢推镜",
			Action:   "@女主 把 @面霜 放回茶几，拿起手机转过屏幕朝向 @男主。",
			Dialogues: []FrameDialogue{
				{Role: "女主", Tone: "自信笃定，语气自然流畅", Line: "当然是真的，你看这对比图。"},
			},
			SFX: "面霜放回茶几的轻微磕碰声",
		},
	}
	return d
}

func TestBuildFrameVideoPromptStructure(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleDraft(), 10)

	// 分镜脚本字段格式：场次/地点/时间/人物/物品 + 每镜 镜号/景别/运镜/情绪动作/台词/音效/时长
	order := []string{
		"场次：1-1",
		"地点：@家居客厅-白天",
		"时间：白天",
		"人物：@女主",
		"物品：@面霜",
		"镜号：1",
		"景别：双人中景",
		"运镜：",
		"情绪/动作：",
		"台词：@女主",
		"音效：",
		"时长：",
		"【执行要点】",
	}
	pos := -1
	for _, seg := range order {
		i := strings.Index(got, seg)
		if i < 0 {
			t.Fatalf("缺少段落 %q\n---\n%s", seg, got)
		}
		if i < pos {
			t.Errorf("段落顺序错乱：%q 出现得太早", seg)
		}
		pos = i
	}
}

func TestBuildFrameVideoPromptMultiShotTimeline(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleMultiShotDraft(), 13)

	// 三个 Shot 块依次出现，起止秒首尾相接
	blocks := shotBlocksOf(got)
	if len(blocks) != 3 {
		t.Fatalf("应拆出 3 个镜头块，实际 %d\n%s", len(blocks), got)
	}
	for i, want := range []string{"镜号：1", "镜号：2", "镜号：3"} {
		if !strings.Contains(blocks[i], want) {
			t.Errorf("第 %d 块缺少 %q\n%s", i+1, want, blocks[i])
		}
	}
	// 时间轴必须连续：镜2 起点 = 镜1 终点
	if b0, b1 := shotBlocksOf(got)[0], shotBlocksOf(got)[1]; shotEndOf(b0) != shotStartOf(b1) {
		t.Errorf("时间轴不连续：镜1 结束 %d，镜2 开始 %d", shotEndOf(b0), shotStartOf(b1))
	}

	// 台词必须落在各自镜头下面
	if !strings.Contains(blocks[1], "<d>[Chinese] 真的假的？我也试试。</d>") {
		t.Errorf("第二镜台词不对：\n%s", blocks[1])
	}
	if !strings.Contains(blocks[2], "<d>[Chinese] 当然是真的，你看这对比图。</d>") {
		t.Errorf("第三镜台词不对：\n%s", blocks[2])
	}
}

// 工具栏上限比内容需求短时不能压缩台词镜头 —— 压了就说不完、口型跟着错
func TestBuildFrameVideoPromptKeepsDialogueDuration(t *testing.T) {
	short := BuildFrameVideoPrompt(sampleMultiShotDraft(), 6)
	long := BuildFrameVideoPrompt(sampleMultiShotDraft(), 30)
	if a, b := sumShotSeconds(short), sumShotSeconds(long); a != b {
		t.Errorf("台词镜头时长不该随工具栏上限变化：6 秒档 %d，30 秒档 %d", a, b)
	}
	blocks := shotBlocksOf(short)
	if len(blocks) != 3 {
		t.Fatalf("应拆出 3 个镜头块，实际 %d", len(blocks))
	}
	// 镜1 有 21 个字，按 3.8 字/秒要 5.5 秒 + 起句，至少 6 秒
	if s := shotSecondsOf(blocks[0]); s < 6 {
		t.Errorf("21 个字的台词镜头至少 6 秒，实际 %d\n%s", s, blocks[0])
	}
}

func TestBuildFrameVideoPromptCastList(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleDraft(), 10)
	for _, want := range []string{
		"人物：@女主（女性，28岁）",
		"地点：@家居客厅-白天",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q\n---\n%s", want, got)
		}
	}
	// 地点行带上场景的空间布局，模型据此摆家具
	if !strings.Contains(got, "浅灰布艺沙发与木质茶几") {
		t.Errorf("地点行没带上空间描述：\n%s", got)
	}
}

func TestBuildFrameVideoPromptKeepsDialogue(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleDraft(), 10)
	// 台词走 H3 官方标记：<d>[Chinese] 正文</d>，前面带 (S1) 说话人标记
	if !strings.Contains(got, "台词：@女主（满带欣喜") {
		t.Errorf("台词语气丢了:\n%s", got)
	}
	if want := "<d>[Chinese] 这个面霜真的好用，我用了一个月，皮肤白了一个度。</d>"; !strings.Contains(got, want) {
		t.Errorf("台词正文被改写或丢了标点，期望 %q:\n%s", want, got)
	}
	// 音色写在人物行里（示例是「音色参考@音频1」，我们没有音频资产，用文字音色）
	if !strings.Contains(got, "人物：@女主（女性，28岁），亲切年轻女音") {
		t.Errorf("人物行没带音色:\n%s", got)
	}
	// 解析回读时不能把 <d> 标签当台词
	lines := ParsePromptDialogues(got)
	if len(lines) != 1 {
		t.Fatalf("解析出台词 %d 句，期望 1 句:\n%s", len(lines), got)
	}
	if lines[0].Role != "女主" {
		t.Errorf("角色名带上了说话人标记：%q", lines[0].Role)
	}
	if strings.Contains(lines[0].Line, "<d>") || strings.Contains(lines[0].Line, "中文") {
		t.Errorf("台词里残留标签：%q", lines[0].Line)
	}
	if lines[0].Line != "这个面霜真的好用，我用了一个月，皮肤白了一个度。" {
		t.Errorf("台词正文不对：%q", lines[0].Line)
	}
}

func TestBuildFrameVideoPromptOmitsEmpty(t *testing.T) {
	d := sampleDraft()
	d.Dialogues = nil
	d.SFX = ""
	got := BuildFrameVideoPrompt(d, 5)
	if !strings.Contains(got, "台词：无") {
		t.Errorf("没有台词时应明确「本镜无台词」:\n%s", got)
	}
	// 参考站格式不占位：没音效就不写这一行
	if strings.Contains(got, "【音效】：") {
		t.Errorf("没有音效时不该输出【音效】行:\n%s", got)
	}
}

func TestBuildFrameVideoPromptNoAspectHardcode(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleMultiShotDraft(), 13)
	for _, bad := range []string{"竖屏 9:16", "横屏 16:9", "9:16", "16:9"} {
		if strings.Contains(got, bad) {
			t.Errorf("提示词里不该写死画幅 %q:\n%s", bad, got)
		}
	}
}

// 提交前对齐：@资产 不能被删，但也不该凭空追加画幅
func TestSanitizeOnBuiltPrompt(t *testing.T) {
	built := BuildFrameVideoPrompt(sampleDraft(), 5)
	got := SanitizeVideoPrompt(built, "16:9", 10)

	if !strings.Contains(got, "@女主") {
		t.Errorf("@资产 引用被删了")
	}
	if strings.Contains(got, "横屏 16:9") {
		t.Errorf("不该向提示词追加画幅:\n%s", got)
	}
}

// 已写入错误画幅的旧提示词要被改正
func TestSanitizeFixesStaleAspect(t *testing.T) {
	got := SanitizeVideoPrompt(realVideoPrompt, "9:16", 10)
	if !strings.Contains(got, "竖屏 9:16") {
		t.Errorf("旧画幅没改成实际比例:\n%s", got)
	}
}

// 自动时长：按台词字数（4.5 字/秒）与镜头数（2.5 秒/镜）取大，夹在 [3,15]
func TestEstimateFrameDuration(t *testing.T) {
	cases := []struct {
		name  string
		draft FrameDraft
		want  int
	}{
		{
			name:  "单镜无台词走 3 秒底线",
			draft: FrameDraft{Shots: []FrameShot{{Action: "空镜推近"}}},
			want:  3,
		},
		{
			name:  "三镜无台词按 2 秒/镜",
			draft: FrameDraft{Shots: []FrameShot{{Action: "a"}, {Action: "b"}, {Action: "c"}}},
			want:  6,
		},
		{
			name: "台词 28 字约 9 秒",
			draft: FrameDraft{Shots: []FrameShot{{
				Action:    "说话",
				Dialogues: []FrameDialogue{{Line: "这个面霜真的好用我用了一个月皮肤白了一个度皮肤也更细腻了"}},
			}}},
			want: 9,
		},
		{
			name: "台词超长夹到 15 秒",
			draft: FrameDraft{Shots: []FrameShot{{
				Dialogues: []FrameDialogue{{Line: strings.Repeat("这是一句很长的台词", 12)}},
			}}},
			want: 15,
		},
	}
	for _, c := range cases {
		if got := EstimateFrameDuration(c.draft); got != c.want {
			t.Errorf("%s: 期望 %d 秒，实际 %d 秒", c.name, c.want, got)
		}
	}
}

// duration 传 0 时描述词的时间轴也要按估算值收尾，而不是各镜建议秒数合计
func TestBuildFrameVideoPromptAutoDuration(t *testing.T) {
	d := sampleDraft()
	d.Shots = []FrameShot{
		{Seconds: 3, Action: "起幅"},
		{Seconds: 3, Action: "推进"},
	}
	out := BuildFrameVideoPrompt(d, 0)
	// 两镜各 3 秒（LLM 建议值大于 2 秒底线）→ 6 秒
	if sum := sumShotSeconds(out); sum != 6 {
		t.Errorf("自动时长应为 6 秒，实际 %d\n%s", sum, out)
	}
}

func TestSplitInlineDialogues(t *testing.T) {
	action := "生涯规划@老师给高中@学生讲解大学专业 **@老师：** 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么…"
	gotAction, lines := splitInlineDialogues(action)
	if len(lines) != 1 {
		t.Fatalf("应拆出 1 条台词，实际 %d 条：%+v", len(lines), lines)
	}
	if lines[0].Role != "老师" {
		t.Errorf("角色名应去掉 @，实际 %q", lines[0].Role)
	}
	if !strings.Contains(lines[0].Line, "你说对土木有兴趣") {
		t.Errorf("台词正文不对：%q", lines[0].Line)
	}
	if strings.Contains(gotAction, "**") || strings.Contains(gotAction, "坐。") {
		t.Errorf("动作里应已去掉台词标记，实际 %q", gotAction)
	}

	// 没有标记时原样返回
	same, none := splitInlineDialogues("老师抬手示意学生坐下")
	if same != "老师抬手示意学生坐下" || len(none) != 0 {
		t.Errorf("无标记不应改动：%q %+v", same, none)
	}
}

// 拆出台词后，时长要按台词字数算，而不是「1 镜 × 2.5 秒 = 3 秒」
func TestInlineDialogueAffectsDuration(t *testing.T) {
	action := "老师讲解 **老师：** 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么…"
	_, lines := splitInlineDialogues(action)
	d := FrameDraft{Shots: []FrameShot{{Action: action, Dialogues: lines}}}
	got := EstimateFrameDuration(d)
	if got <= 3 {
		t.Fatalf("有 35 字台词不该只估 3 秒，实际 %d 秒", got)
	}
	if got < 8 || got > 11 {
		t.Errorf("35 字台词应在 8-11 秒之间，实际 %d 秒", got)
	}
}

// 台词装不下 15 秒的帧要按镜头拆开，别硬塞
func TestSplitOverlongFrames(t *testing.T) {
	mk := func(n int) []FrameDialogue {
		return []FrameDialogue{{Role: "老师", Line: strings.Repeat("字", n)}}
	}
	d := FrameDraft{
		FrameIndex: 1,
		Text:       "老师讲解土木专业",
		Shots: []FrameShot{
			{Seconds: 3, Action: "a", Dialogues: mk(30)},
			{Seconds: 3, Action: "b", Dialogues: mk(30)},
			{Seconds: 3, Action: "c", Dialogues: mk(30)},
		},
	}
	out := splitOverlongFrames([]FrameDraft{d}, 15)
	// 每镜 30 字按 3.8 字/秒要 8.5 秒，两镜就 17 秒超上限 → 一帧只能装一镜，拆 3 帧
	if len(out) != 3 {
		t.Fatalf("90 字应拆成 3 帧（每帧 1 镜），实际 %d 帧", len(out))
	}
	for i, f := range out {
		if f.FrameIndex != i+1 {
			t.Errorf("帧号应重排为 %d，实际 %d", i+1, f.FrameIndex)
		}
		if need := frameDurationNeed(f); need > 15 {
			t.Errorf("第 %d 帧需要 %.1f 秒，超过单帧上限", i+1, need)
		}
	}
	for i := range out {
		if got := len(normalizeShots(out[i])); got != 1 {
			t.Errorf("第 %d 帧应只含 1 镜，实际 %d 镜", i+1, got)
		}
	}
	if !strings.Contains(out[1].Text, "（续）") {
		t.Errorf("续帧文案应带标记，实际 %q", out[1].Text)
	}

	// 台词装得下就不拆
	small := FrameDraft{
		FrameIndex: 1,
		Text:       "短对话",
		Shots: []FrameShot{
			{Seconds: 3, Action: "a", Dialogues: mk(10)},
			{Seconds: 3, Action: "b", Dialogues: mk(10)},
		},
	}
	if got := splitOverlongFrames([]FrameDraft{small}, 15); len(got) != 1 {
		t.Errorf("20 字不该拆帧，实际 %d 帧", len(got))
	}
}

// 长台词那镜必须分到足够的时间，不能和短台词镜平均分
// （用户实际踩到的：4-9 秒里塞 33 字，老师说没说完学生就开口了）
func TestTimelineGivesLongDialogueEnoughTime(t *testing.T) {
	d := FrameDraft{
		Shots: []FrameShot{
			{Seconds: 4, ShotSize: "双人中景", Action: "抬手示意", Dialogues: []FrameDialogue{{Role: "老师", Line: "坐。"}}},
			{Seconds: 5, ShotSize: "中近景", Action: "前倾讲解", Dialogues: []FrameDialogue{{Role: "老师", Line: "你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？"}}},
			{Seconds: 4, ShotSize: "特写", Action: "低头思考", Dialogues: []FrameDialogue{{Role: "学生", Line: "盖房子、修路架桥吧，听说毕业常驻工地。"}}},
		},
	}
	dur := EstimateFrameDuration(d)
	got := BuildFrameVideoPrompt(d, dur)
	blocks := shotBlocksOf(got)
	if len(blocks) != 3 {
		t.Fatalf("应拆出 3 个镜头块，实际 %d\n%s", len(blocks), got)
	}
	// 台词 33 个字符，但只有 28 个汉字要念（标点不占发音时间）
	need := int(math.Ceil(28.0 / 4.5))
	if s2 := shotSecondsOf(blocks[1]); s2 < need {
		t.Errorf("镜头2 有 28 个汉字台词，至少要 %d 秒，实际 %d 秒\n%s", need, s2, blocks[1])
	}
	if s1 := shotSecondsOf(blocks[0]); s1 > 3 {
		t.Errorf("镜头1 只有「坐。」两字，不该占 %d 秒", s1)
	}
}

func TestParsePromptDialogues(t *testing.T) {
	vp := "｜镜头1｜ 0–1秒｜双人中景｜\n" +
		"【老师台词】（平和亲切）：“坐。”\n" +
		"｜镜头2｜ 1–10秒｜中近景｜\n" +
		"【老师台词】（语气笃定）：“你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？”\n" +
		"｜镜头3｜ 10–15秒｜近景｜\n" +
		"【学生台词】（带着犹豫）：“盖房子、修路架桥吧。”\n"
	lines := ParsePromptDialogues(vp)
	if len(lines) != 3 {
		t.Fatalf("应抽出 3 句台词，实际 %d 句：%+v", len(lines), lines)
	}
	if lines[0].Role != "老师" || lines[0].Line != "坐。" {
		t.Errorf("第一句不对：%+v", lines[0])
	}
	// 内层引号不能把台词截断
	if !strings.Contains(lines[1].Line, "先说说你印象里的土木是什么") {
		t.Errorf("第二句被截断了：%q", lines[1].Line)
	}
	if lines[2].Role != "学生" {
		t.Errorf("第三句角色不对：%+v", lines[2])
	}
}

// LLM 重写时漏台词，要按顺序补回去
func TestApplyDialoguesFillsMissing(t *testing.T) {
	d := FrameDraft{
		Text: "老师讲解",
		Shots: []FrameShot{
			{Seconds: 3, ShotSize: "双人中景", Action: "抬手示意"},
			{Seconds: 3, ShotSize: "中近景", Action: "前倾讲解"},
			{Seconds: 3, ShotSize: "近景", Action: "低头思考"},
		},
	}
	want := []FrameDialogue{
		{Role: "老师", Line: "坐。"},
		{Role: "老师", Line: "你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？"},
		{Role: "学生", Line: "盖房子、修路架桥吧，听说毕业常驻工地。"},
	}
	out := ApplyDialogues(d, want)
	shots := normalizeShots(out)
	if len(shots) != 3 {
		t.Fatalf("镜头数应保持 3，实际 %d", len(shots))
	}
	for i, sh := range shots {
		if len(sh.Dialogues) != 1 {
			t.Fatalf("第 %d 镜应有 1 句台词，实际 %d", i+1, len(sh.Dialogues))
		}
	}
	if shots[1].Dialogues[0].Line != want[1].Line {
		t.Errorf("第二镜台词不对：%q", shots[1].Dialogues[0].Line)
	}
	// 时长按补回去的台词重算：1 + 28 + 16 = 45 个汉字（标点不计）→ 约 12 秒
	if out.Duration != 15 {
		t.Errorf("补台词后时长应重算为 15 秒，实际 %d", out.Duration)
	}
	// 时长：镜头2 要分到足够念完 28 个汉字的时间；
	// 三镜需求合计超过 H3 单段 15 秒上限，从最短的镜头1（「坐。」）扣回来
	blocks := shotBlocksOf(out.VideoPrompt)
	if len(blocks) != 3 {
		t.Fatalf("应拆出 3 个镜头块，实际 %d\n%s", len(blocks), out.VideoPrompt)
	}
	if s2 := shotSecondsOf(blocks[1]); s2 < 7 {
		t.Errorf("镜头2 应分到 7 秒以上，实际 %d\n%s", s2, blocks[1])
	}
	if sum := sumShotSeconds(out.VideoPrompt); sum != 15 {
		t.Errorf("各镜时长合计应为 15 秒（H3 单段上限），实际 %d", sum)
	}
}

// 提交给模型前，@资产名 要换成 <Picture N>，并生成 subject_definitions
func TestRewriteMentionsForModel(t *testing.T) {
	vp := "人物：@老师，@学生\n场景：@公司会议室-白天\n\n" +
		"｜镜头1｜ 0–1秒｜双人中景｜\n" +
		"【@老师台词】（平和）：“坐。”\n" +
		"@老师 抬手示意 @学生 入座。"
	out := RewriteMentionsForModel(vp, []string{"老师", "学生", "公司会议室-白天"}, []string{"character", "character", "scene"})

	// 官方结构：subject_definitions 打头，逐个说明每张图承担什么作用
	if !strings.HasPrefix(out, "subject_definitions:\n") {
		t.Errorf("缺少 subject_definitions 头：\n%s", out[:min(120, len(out))])
	}
	for _, want := range []string{
		"<Picture 1> 是「老师」",
		"<Picture 2> 是「学生」",
		"<Picture 3> 是「公司会议室-白天」：空间结构、家具布局、光线方向",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("subject_definitions 缺少 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "@老师") || strings.Contains(out, "@学生") {
		t.Errorf("@资产名 没替换干净：\n%s", out)
	}
	if !strings.Contains(out, "人物：<Picture 1>，<Picture 2>") {
		t.Errorf("人物行没换成图位：\n%s", out)
	}
	if !strings.Contains(out, "【<Picture 1>台词】（平和）：“坐。”") {
		t.Errorf("台词行角色没换：\n%s", out)
	}
	if !strings.Contains(out, "<Picture 1> 抬手示意 <Picture 2> 入座") {
		t.Errorf("正文没换：\n%s", out)
	}

	// 没有参考图时原样返回
	if got := RewriteMentionsForModel(vp, nil, nil); got != vp {
		t.Errorf("没有图位时不该改动")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 全局规则里必须有口型约束：既要「口型与台词同步」，也要「只有说话的人动嘴」
// （实测出现过老师还在念台词，对面的学生已经在动嘴）
func TestPromptHasLipSyncRule(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleDraft(), 0)
	for _, want := range []string{
		"念完才切下一个镜头",
		"只有正在说话的角色动嘴，其他人闭口",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少口型约束 %q：\n%s", want, got)
		}
	}
}

// 语速要写进【动作】行的「怎么说」里，LLM 给的 tone 也不能丢。
// 只写定性区间，不写「约 3.0 字/秒」这种精确数字 —— 模型会照着数字机械执行，
// 短句（1 个字分到 2 秒）算出来 0.5 字/秒，它会把那个字拖满整镜。
func TestPromptCarriesSpeechRate(t *testing.T) {
	got := BuildFrameVideoPrompt(sampleDraft(), 0)
	if !strings.Contains(got, "语速平稳") {
		t.Fatalf("缺少语速提示：\n%s", got)
	}
	if strings.Contains(got, "字/秒") {
		t.Errorf("不该再出现精确字/秒：\n%s", got)
	}
	// sampleDraft 的 tone 是「满带欣喜，语调轻快而笃定」
	if !strings.Contains(got, "满带欣喜") {
		t.Errorf("LLM 给的 tone 被丢掉了：\n%s", got)
	}
}

// 动作必须与台词时序对齐：场景状态只写起始姿态，动作单列一块并说明触发时机
// （实测「抬手示意他坐」被提前到第 0 秒做完，台词说「坐」时人已经坐好了）
func TestPromptTiesActionToDialogueTiming(t *testing.T) {
	d := FrameDraft{
		Roles: []model.Character{{Name: "老师"}},
		Shots: []FrameShot{{
			ShotSize:  "双人中景",
			Action:    "说到「坐」时抬手示意对方落座",
			Dialogues: []FrameDialogue{{Role: "老师", Line: "坐。"}},
		}},
	}
	got := BuildFrameVideoPrompt(d, 9)

	// 动作直接写在镜头头下面（参考站格式，不再套【动作】标签）
	if !strings.Contains(got, "说到「坐」时抬手示意对方落座") {
		t.Errorf("动作丢失：\n%s", got)
	}
	for _, want := range []string{
		"说到时才发生，不得提前",
		"全程不换座",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少时序约束 %q：\n%s", want, got)
		}
	}
}

func TestPauseMarkAddsTime(t *testing.T) {
	plain := FrameShot{Dialogues: []FrameDialogue{{Role: "老师", Line: "这是一句需要停顿的话"}}}
	paused := FrameShot{Dialogues: []FrameDialogue{{Role: "老师", Line: "这是一句——需要停顿的话"}}}

	a := shotSpeakSeconds(plain)
	b := shotSpeakSeconds(paused)
	if !(b > a) {
		t.Fatalf("带气口应更久：无气口 %.2fs，有气口 %.2fs", a, b)
	}
	// 一个气口按 0.8 秒
	if diff := b - a; math.Abs(diff-0.8) > 0.01 {
		t.Errorf("一个「——」应加 0.8 秒，实际加 %.2f 秒", diff)
	}

	// 标点不占发音时间：加句号不该变长
	withPunct := FrameShot{Dialogues: []FrameDialogue{{Role: "老师", Line: "这是一句需要停顿的话。"}}}
	if got := shotSpeakSeconds(withPunct); math.Abs(got-a) > 0.01 {
		t.Errorf("句号不该影响时长：%.2f vs %.2f", got, a)
	}
}

// 人物锁定块：角色档案里的服装 / 发型要原样进每一帧
func TestCharacterBriefInHeader(t *testing.T) {
	d := sampleDraft()
	d.Roles = []model.Character{{
		Name:           "老师",
		IdentityAnchor: "女性，38岁，生涯规划老师。身形中等偏瘦",
		AppearanceCues: "鹅蛋脸，眉形细长平直",
		CostumePrompt:  "浅蓝细条纹棉质衬衫，深灰西裤，细银色方框眼镜",
	}}
	got := BuildFrameVideoPrompt(d, 6)

	// 新格式不再写人物锁定长段：外观交给参考图，文字只留一句身份
	if !strings.Contains(got, "人物：@老师（女性，38岁）") {
		t.Errorf("人物行没带身份：\n%s", got)
	}
	if strings.Contains(got, "【人物锁定】") {
		t.Errorf("不该再输出人物锁定块：\n%s", got)
	}
	// 位置锁定规则仍在
	if !strings.Contains(got, "全程不换座、不绕桌走动") {
		t.Errorf("缺少位置锁定规则")
	}
}

func shotBlocksOf(vp string) []string {
	var out []string
	var cur strings.Builder
	started := false
	for _, line := range strings.Split(vp, "\n") {
		t := strings.TrimSpace(line)
		// 镜头头有两种写法：
		//   新格式（参考站风格）|镜头1| 0-5秒| 双人中景|
		//   旧格式（Ref2VA）    [Shot 1] 0.00–2.00s，…
		if strings.HasPrefix(t, "[Shot ") || strings.HasPrefix(t, "|镜头") || strings.HasPrefix(t, "镜号：") {
			if started {
				out = append(out, cur.String())
			}
			cur.Reset()
			started = true
		}
		if started {
			cur.WriteString(line + "\n")
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}

// shotSecondsOf 取一个镜头块的长度：从【Shot N｜a–b秒｜…】里的起止秒算
func shotSecondsOf(block string) int {
	// 当前格式把时长单列一行：时长：6s
	if m := regexp.MustCompile(`(?m)^时长：\s*(\d+)\s*s`).FindStringSubmatch(block); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	// 兼容两种紧凑镜头头写法，见 shotBlocksOf
	re := regexp.MustCompile(`\[Shot\s*\d+\]\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*s|\|镜头\s*\d+\|\s*([\d.]+)\s*[-–—~～至]\s*([\d.]+)\s*秒`)
	m := re.FindStringSubmatch(block)
	if len(m) < 5 {
		return 0
	}
	var start, end float64
	if m[1] != "" {
		start, _ = strconv.ParseFloat(m[1], 64)
		end, _ = strconv.ParseFloat(m[2], 64)
	} else {
		start, _ = strconv.ParseFloat(m[3], 64)
		end, _ = strconv.ParseFloat(m[4], 64)
	}
	if end <= start {
		return 0
	}
	return int(end - start)
}

// shotStartOf 取镜头块的起始秒（【Shot 1｜0–2秒｜…】里的 0）
func shotStartOf(block string) int {
	re := regexp.MustCompile(`\[Shot\s*\d+\]\s*([\d.]+)\s*[–—\-~～至]`)
	if m := re.FindStringSubmatch(block); len(m) > 1 {
		v, _ := strconv.ParseFloat(m[1], 64)
		return int(v)
	}
	return 0
}

// shotEndOf 取镜头块的结束秒（【Shot 1｜0–2秒｜…】里的 2）
func shotEndOf(block string) int {
	re := regexp.MustCompile(`\[Shot\s*\d+\]\s*[\d.]+\s*[–—\-~～至]\s*([\d.]+)\s*s`)
	if m := re.FindStringSubmatch(block); len(m) > 1 {
		v, _ := strconv.ParseFloat(m[1], 64)
		return int(v)
	}
	return 0
}

// sumShotSeconds 各镜时长之和
func sumShotSeconds(vp string) int {
	total := 0
	for _, b := range shotBlocksOf(vp) {
		total += shotSecondsOf(b)
	}
	return total
}

// 老分镜（改动前生成的）里没有「语气：」行，出片前要就地补上语速，
// 否则模型只能靠字数÷时长猜，念快了嘴跟不上
func TestEnsureSpeechRateFillsOldPrompt(t *testing.T) {
	old := `场次：1-2
地点：@咨询间-白天 @老师 坐在画面左侧，@学生 坐在画面右侧

镜号：1
景别：中近景/@老师
运镜：缓慢推镜
情绪/动作：@老师 身体微微前倾。
台词：老师：你说对土木有兴趣，又怕就是去工地。
音效：无
时长：5s

镜号：2
景别：近景/@学生
运镜：固定镜头
情绪/动作：@学生 低下头。
台词：无
音效：无
时长：3s
`
	got := EnsureSpeechRate(old)

	// 老格式的台词要包上 H3 的 <d> 标记，否则模型不一定把它当对白念
	if !strings.Contains(got, "台词：老师：<d>[Chinese] 你说对土木有兴趣，又怕就是去工地。</d>") {
		t.Fatalf("台词没有包上 <d> 标记：\n%s", got)
	}
	// 15 个字 / 5 秒 = 3.0 字/秒，落在「语速平稳」区间（不写精确数字）
	if !strings.Contains(got, "语气：语速平稳") {
		t.Fatalf("没有补上语速行：\n%s", got)
	}
	// 语气行必须夹在台词和音效之间，不能跑到别的镜头去
	if strings.Index(got, "语气：") < strings.Index(got, "台词：老师：") {
		t.Errorf("语气行位置不对，应在台词之后：\n%s", got)
	}
	// 第二镜没台词，不该加语气行
	if n := strings.Count(got, "语气："); n != 1 {
		t.Errorf("无台词镜头也加了语气行，共 %d 处：\n%s", n, got)
	}

	// 新格式已有语气行，原样返回（不能重复插入）
	if again := EnsureSpeechRate(got); again != got {
		t.Errorf("重复调用改动了文本：\n%s", again)
	}
}

// 改时长后描述词时间轴必须跟着缩放，否则「卡片 13 秒 / 描述词 3 秒」脱节。
func TestRescalePromptDuration(t *testing.T) {
	single := `场次：1-1
地点：现代家居客厅-白天

summary:
Ref2VA 目标视频：第 1-1 场，现代家居客厅-白天，单镜头 3 秒。

detailed_description:
[Shot 1] 0.00–3.00s，双人中景，主体 @女主
【动作】@女主 把面霜抬起半寸示意。
`
	out := RescalePromptDuration(single, 13)
	if !strings.Contains(out, "[Shot 1] 0.00–13.00s") {
		t.Errorf("单镜应缩放到 0.00–13.00s，实际:\n%s", out)
	}
	if !strings.Contains(out, "单镜头 13 秒") {
		t.Errorf("summary 里的秒数没同步，实际:\n%s", out)
	}
	if strings.Contains(out, "单镜头 3 秒") {
		t.Error("summary 里还残留旧的 3 秒")
	}

	// 多镜按原比例分配：1:2:3 共 6 秒 → 12 秒应是 2:4:6
	multi := `summary:
Ref2VA 目标视频：第 1-1 场，客厅，3 个镜头共 6 秒。

detailed_description:
[Shot 1] 0.00–1.00s，中景
[Shot 2] 1.00–3.00s，近景
[Shot 3] 3.00–6.00s，特写
`
	got := RescalePromptDuration(multi, 12)
	for _, want := range []string{
		"[Shot 1] 0.00–2.00s",
		"[Shot 2] 2.00–6.00s",
		"[Shot 3] 6.00–12.00s",
		"共 12 秒",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("多镜缩放缺少 %q，实际:\n%s", want, got)
		}
	}

	// 没有时间轴 / 非法入参：原样返回，不做破坏性改写
	if in := "没有时间轴的描述词"; RescalePromptDuration(in, 10) != in {
		t.Error("无时间轴时应原样返回")
	}
	if RescalePromptDuration(single, 0) != single {
		t.Error("total<=0 时应原样返回")
	}

	// 缩放到比镜头数还短：每段至少 1 秒，不能出现 0 或负区间
	tiny := RescalePromptDuration(multi, 2)
	if strings.Contains(tiny, "0.00–0.00s") {
		t.Errorf("出现了零长度镜头:\n%s", tiny)
	}
}

// 硬不变量：分镜卡片显示的时长 == 描述词时间轴的总长。
//
// 这两个数曾经由两套算法算出来（一个浮点累加+0.4 后统一 ceil，
// 一个逐镜 ceil 再相加），于是出现「卡片 4 秒 / 描述词 0.00–3.00s」。
// 现在两边都走 planShotSeconds，任何偏离这个断言都说明又分叉了。
func TestDurationMatchesPromptTimeline(t *testing.T) {
	cases := []struct {
		name  string
		draft FrameDraft
	}{
		{"单镜短台词", FrameDraft{Shots: []FrameShot{{
			Action: "说话", Dialogues: []FrameDialogue{{Line: "真的假的？我也试试。"}},
		}}}},
		{"单镜长台词", FrameDraft{Shots: []FrameShot{{
			Action: "说话", Dialogues: []FrameDialogue{{Line: "这个面霜真的好用我用了一个月皮肤白了一个度皮肤也更细腻了"}},
		}}}},
		{"三镜纯动作", FrameDraft{Shots: []FrameShot{{Action: "a"}, {Action: "b"}, {Action: "c"}}}},
		{"两镜按建议秒数", FrameDraft{Shots: []FrameShot{
			{Seconds: 3, Action: "起幅"}, {Seconds: 5, Action: "推进"},
		}}},
		{"台词+动作混合", FrameDraft{Shots: []FrameShot{
			{Action: "走近", Dialogues: []FrameDialogue{{Line: "你好"}}},
			{Action: "坐下"},
		}}},
		{"无 Shots 走平铺字段", FrameDraft{
			Action: "她转头看向窗外", Dialogues: []FrameDialogue{{Line: "外面下雨了。"}},
		}},
	}
	for _, c := range cases {
		dur := EstimateFrameDuration(c.draft)
		out := BuildFrameVideoPrompt(c.draft, dur)
		if sum := sumShotSeconds(out); sum != dur {
			t.Errorf("%s: duration=%d 但描述词时间轴=%d\n%s", c.name, dur, sum, out)
		}
	}
}

// 新格式自带语气括号，EnsureSpeechRate 不能重复插「语气：」行
func TestEnsureSpeechRateSkipsFieldFormat(t *testing.T) {
	in := "镜号：1\n景别：双人中景\n台词：@老师（自然，语速平稳）：<d>[Chinese] 坐。</d>\n时长：6s"
	if got := EnsureSpeechRate(in); got != in {
		t.Errorf("新格式不该被改动:\n%s", got)
	}
}
