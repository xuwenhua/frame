package service

import (
	"strings"
	"testing"
)

var charStyles = []string{CharStyleAsset, CharStyleQuad, CharStyleConcept, CharStyleCloseup}
var sceneStyles = []string{SceneStyleSingle, SceneStyleMulti, SceneStyleConcept, SceneStyleWide}

// 存库的提示词必须是「通用提示」：只描述资产本身，不含任何视图/构图要求
func TestBuildPromptIsGeneric(t *testing.T) {
	sp := DetectStyle("竖版9:16，日式水彩插画风格。人物不露正脸。")

	charPrompt := RuleCharacterPrompt(RoleDraft{
		Name: "林晚", IdentityAnchor: "女性，34岁", CostumePrompt: "深灰针织开衫",
	}, sp)
	scenePrompt := RuleScenePrompt(SceneDraft{Name: "诊室走廊", TimeOfDay: "夜晚"}, sp)
	itemPrompt := RuleItemPrompt(ItemDraft{Name: "保温饭盒", Category: "家居"}, sp)

	for _, c := range []struct {
		kind   string
		prompt string
	}{
		{"character", charPrompt},
		{"scene", scenePrompt},
		{"item", itemPrompt},
	} {
		for _, banned := range []string{"三视图", "四视图", "资产图", "概念图", "空镜头", "白底", "单件居中", "白色背景", "纯净无字"} {
			if strings.Contains(c.prompt, banned) {
				t.Fatalf("%s 的通用提示里不该出现构图要求 %q：%s", c.kind, banned, c.prompt)
			}
		}
	}
}

// 视图要求只在提交生成时追加，且四种角色样式各追加各的
func TestCharacterStyleAppendedOnSubmit(t *testing.T) {
	sp := DefaultStyle()
	base := "女性，34岁，身形匀称。穿深蓝色古装长袍"

	seen := map[string]string{}
	for _, style := range charStyles {
		out := ApplyGenOptions(base, "character", sp, GenOptions{CharStyle: style})
		if !strings.HasPrefix(out, base) {
			t.Fatalf("通用提示应原样保留在最前面，style=%s out=%s", style, out)
		}
		if !strings.Contains(out, "纯净无字画面") {
			t.Fatalf("缺少无字声明，style=%s", style)
		}
		if n := strings.Count(out, sp.TailStyle()); n != 1 {
			t.Fatalf("画风出现 %d 次，style=%s out=%s", n, style, out)
		}
		seen[style] = out
	}
	if len(seen) != 4 {
		t.Fatal("四种样式应产出四条不同提示词")
	}

	// 人物资产图必须明确「正面无头」
	asset := seen[CharStyleAsset]
	for _, need := range []string{"头部被裁切", "背面全身", "正面胸像"} {
		if !strings.Contains(asset, need) {
			t.Fatalf("人物资产图缺少 %q：%s", need, asset)
		}
	}
	quad := seen[CharStyleQuad]
	if !strings.Contains(quad, "人物四视图") || !strings.Contains(quad, "正面脸部特写") {
		t.Fatalf("人物四视图不对：%s", quad)
	}
}

// 同一份通用提示 + 同一个样式，必须产出同一结果（可重放）
func TestApplyIsDeterministic(t *testing.T) {
	sp := DetectStyle("日式水彩插画风格，水彩纸纹理")
	base := "女性，34岁。穿深灰针织开衫"
	o := GenOptions{CharStyle: CharStyleConcept, Resolution: Res2K}

	a := ApplyGenOptions(base, "character", sp, o)
	b := ApplyGenOptions(base, "character", sp, o)
	if a != b {
		t.Fatalf("两次调用结果不一致：\n%s\n%s", a, b)
	}
	if !strings.Contains(a, "高分辨率细节") {
		t.Fatalf("2K 应追加高精度细节：%s", a)
	}
	// 画风已在通用提示里，不应再补一遍
	if n := strings.Count(a, "日式手绘水彩插画风格"); n > 1 {
		t.Fatalf("画风重复 %d 次：%s", n, a)
	}
}

// 老数据：提示词尾部当年写死的构图要在提交时被剥掉，不能新旧两套共存
func TestStripLegacyTail(t *testing.T) {
	sp := DefaultStyle()
	legacy := []string{
		"女性，34岁，身形匀称，同一角色的三视图：左侧为正面全身、中间为侧面全身、右侧为背面全身，白色背景，纯净无字画面，无任何文字与水印，写实风格，8K超清",
		"女性，34岁，身形匀称，人物资产图：画面横向分为三部分，左侧为正面全身但头部被裁切，白色背景，写实风格，8K超清",
	}
	for _, old := range legacy {
		out := ApplyGenOptions(old, "character", sp, GenOptions{CharStyle: CharStyleCloseup})
		// 旧的「同一角色的三视图 / 人物资产图」必须消失（新拼的「面部特写三视图」是本次选择的结果）
		if strings.Contains(out, "同一角色的三视图") || strings.Contains(out, "人物资产图：画面横向") {
			t.Fatalf("旧尾句未剥离：%s", out)
		}
		if !strings.Contains(out, "面部特写三视图") {
			t.Fatalf("未追加新视图要求：%s", out)
		}
	}

	legacyScene := "现代医院走廊，夜晚，顶灯偏冷，空镜头，无人物，画面中空无一人，地面清晰可见，平视广角构图，中央区域留白，写实风格，8K超清"
	out := ApplyGenOptions(legacyScene, "scene", sp, GenOptions{SceneStyle: SceneStyleWide})
	if strings.Contains(out, "中央区域留白") {
		t.Fatalf("旧场景尾句未剥离：%s", out)
	}
	if !strings.Contains(out, "超广角全景构图") {
		t.Fatalf("未追加广角要求：%s", out)
	}
}

// LLM 自己写进 image_prompt 的视图描述，无论用什么标点都要能剥掉
func TestStripFramingAnyPunctuation(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "分号开头（真实踩到的例子）",
			input: "男性，48岁，外科医生，气质沉稳。穿外罩及膝纯白棉质医生白大褂，前襟系扣，左胸口袋插一支黑色签字笔，深灰直筒长裤，白色软底平底鞋；同一角色的三视图：左侧为侧面全身、中间为侧后方全身、右侧为背面全身，三视图等距并排、比例一致、服装与发型完全一致，白色背景，均不需正脸，纯净无字画面，竖版 9:16，8K超清",
			want:  "男性，48岁，外科医生，气质沉稳。穿外罩及膝纯白棉质医生白大褂，前襟系扣，左胸口袋插一支黑色签字笔，深灰直筒长裤，白色软底平底鞋",
		},
		{
			name:  "句号分隔",
			input: "女性，34岁，身形清瘦。穿深灰针织开衫。同一角色的四视图：正脸、侧面、正面全身、背面全身。白色背景。8K超清",
			want:  "女性，34岁，身形清瘦。穿深灰针织开衫",
		},
		{
			name:  "没有前缀直接写空格分格",
			input: "女性，34岁，穿米色风衣，画面分为四格，上排正面、下排背面",
			want:  "女性，34岁，穿米色风衣",
		},
		{
			name:  "场景空镜收尾",
			input: "现代医院走廊，左侧诊室门与灯箱，右侧墙面指示牌，夜晚，顶部日光灯偏冷；空镜头，无人物，画面中空无一人，地面清晰可见，平视广角构图，中央区域留白，纯净无字画面，8K超清",
			want:  "现代医院走廊，左侧诊室门与灯箱，右侧墙面指示牌，夜晚，顶部日光灯偏冷",
		},
	}

	for _, c := range cases {
		got := StripFraming(c.input, "character")
		if got != c.want {
			t.Fatalf("%s:\n want %q\n  got %q", c.name, c.want, got)
		}
		// 剥完不能再含任何视图词
		for _, bad := range []string{"三视图", "四视图", "白色背景", "纯净无字", "8K", "竖版", "空镜头"} {
			if strings.Contains(got, bad) {
				t.Fatalf("%s 剥离不干净（%s）：%s", c.name, bad, got)
			}
		}
	}
}

// 不能让「白色背景墙」这种正常描述被误切
func TestStripFramingKeepsLegitContent(t *testing.T) {
	input := "现代简约客厅，白色背景墙搭配浅灰布艺沙发，左侧落地窗与米色窗帘，地面浅色木地板"
	got := StripFraming(input, "scene")
	if got != input {
		t.Fatalf("正当内容被误切：\n want %q\n  got %q", input, got)
	}

	item := "一只白色陶瓷马克杯，哑光釉面，杯身印有细金边"
	if got := StripFraming(item, "item"); got != item {
		t.Fatalf("正当内容被误切：%s", got)
	}
}

func TestSceneStyles(t *testing.T) {
	sp := DetectStyle("竖版9:16，手绘插画风格")
	base := "现代医院诊疗空间，左侧为诊室门"

	seen := map[string]bool{}
	for _, style := range sceneStyles {
		out := ApplyGenOptions(base, "scene", sp, GenOptions{SceneStyle: style})
		if !strings.Contains(out, "空镜头") {
			t.Fatalf("%s 应保持空镜无人：%s", style, out)
		}
		if n := strings.Count(out, "空镜头"); n > 1 {
			t.Fatalf("%s 空镜约束重复 %d 次：%s", style, n, out)
		}
		if seen[out] {
			t.Fatalf("样式 %s 与其它样式结果重复", style)
		}
		seen[out] = true
	}

	concept := ApplyGenOptions(base, "scene", sp, GenOptions{SceneStyle: SceneStyleConcept})
	if !strings.Contains(concept, "场景概念图") {
		t.Fatalf("场景概念图尾句缺失：%s", concept)
	}
}

// 不露正脸的剧本：任何样式都不能要求画脸
func TestNoFaceNeverAsksForFace(t *testing.T) {
	sp := DetectStyle("竖版9:16，日式手绘水彩插画风格。人物不露正脸，多用背影与手部特写。")
	base := "女性，34岁，身形清瘦。体态自然，画面中看不到面部。穿深灰针织开衫。"

	for _, style := range charStyles {
		out := ApplyGenOptions(base, "character", sp, GenOptions{CharStyle: style})
		for _, forbidden := range []string{"正面胸像", "正面脸部特写", "面部特写三视图", "清晰呈现面部五官"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("no-face 剧本不该要求画脸（%s），style=%s out=%s", forbidden, style, out)
			}
		}
	}
}

func TestApplyGenOptionsResolution(t *testing.T) {
	sp := DefaultStyle()
	base := "一只保温饭盒，单层，扣盖式"
	k1 := ApplyGenOptions(base, "item", sp, GenOptions{Resolution: Res1K})
	k2 := ApplyGenOptions(base, "item", sp, GenOptions{Resolution: Res2K})
	if strings.Contains(k1, "高分辨率细节") {
		t.Fatalf("1K 不该加高精度细节：%s", k1)
	}
	if !strings.Contains(k2, "高分辨率细节") {
		t.Fatalf("2K 应加高精度细节：%s", k2)
	}
	if !strings.Contains(k1, "白色背景，单件居中") {
		t.Fatalf("道具图要白底单件：%s", k1)
	}
}

func TestAspectFor(t *testing.T) {
	real := DefaultStyle()
	vertical := DetectStyle("竖版9:16，写实")

	cases := []struct {
		name  string
		asset string
		sp    StyleProfile
		opts  GenOptions
		want  string
	}{
		{"人物资产图", "character", vertical, GenOptions{CharStyle: CharStyleAsset}, "16:9"},
		{"人物四视图", "character", real, GenOptions{CharStyle: CharStyleQuad}, "3:4"},
		{"人物概念图", "character", real, GenOptions{CharStyle: CharStyleConcept}, "16:9"},
		{"特写三视图", "character", real, GenOptions{CharStyle: CharStyleCloseup}, "16:9"},
		{"场景单视角", "scene", real, GenOptions{SceneStyle: SceneStyleSingle}, "4:3"},
		{"竖版剧本场景", "scene", vertical, GenOptions{SceneStyle: SceneStyleSingle}, "9:16"},
		{"场景多角度", "scene", real, GenOptions{SceneStyle: SceneStyleMulti}, "16:9"},
		{"场景概念图", "scene", real, GenOptions{SceneStyle: SceneStyleConcept}, "16:9"},
		{"场景广角", "scene", real, GenOptions{SceneStyle: SceneStyleWide}, "16:9"},
		{"道具方图", "item", real, GenOptions{}, "1:1"},
	}
	for _, c := range cases {
		if got := c.opts.AspectFor(c.asset, c.sp); got != c.want {
			t.Errorf("%s: want %s got %s", c.name, c.want, got)
		}
	}
}

func TestGenOptionsNormalize(t *testing.T) {
	o := GenOptions{CharStyle: "乱填", SceneStyle: "", Resolution: "4K"}.Normalize()
	if o.CharStyle != CharStyleAsset || o.SceneStyle != SceneStyleSingle || o.Resolution != Res1K {
		t.Fatalf("归一化失败：%+v", o)
	}
	// 旧值（sheet/single/half）也要落回默认，避免老请求打到不存在的分支
	for _, legacy := range []string{"sheet", "single", "half"} {
		if got := (GenOptions{CharStyle: legacy}).Normalize().CharStyle; got != CharStyleAsset {
			t.Fatalf("旧值 %s 未归一化，得到 %s", legacy, got)
		}
	}
}
