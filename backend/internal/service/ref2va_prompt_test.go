package service

import (
	"strings"
	"testing"

	"jikeai-clone/internal/model"
)

// 六段式的结构必须完整、字段顺序固定、时间码连续。
func TestRef2VASections(t *testing.T) {
	d := FrameDraft{
		FrameIndex: 1,
		Roles: []model.Character{
			{Name: "老师", IdentityAnchor: "男，四十二岁，资深教师", CostumePrompt: "深黑色针织短袖，深灰直筒西裤"},
			{Name: "学生", IdentityAnchor: "男，十八岁，高三学生", CostumePrompt: "白色校服衬衫"},
		},
		Scenes:    []model.Scene{{Name: "教师办公室-白天", SpatialAnchor: "中央办公桌，两侧各一把椅子"}},
		Situation: "两人隔桌对坐。",
		SFX:       "窗外隐约车流声",
		Shots: []FrameShot{
			{Seconds: 3, ShotSize: "中近景", Camera: "固定", State: "@老师 坐在桌后",
				Dialogues: []FrameDialogue{{Role: "老师", Line: "坐。"}}},
			{Seconds: 4, ShotSize: "双人中景", Camera: "缓慢推镜", State: "@学生 坐在桌前",
				Dialogues: []FrameDialogue{{Role: "学生", Line: "好。"}}},
		},
	}

	got := BuildRef2VAPrompt(d, 7)

	for _, sec := range []string{
		"subject_definitions:", "summary:", "retention_analysis:",
		"detailed_description:", "overall_soundscape:", "non_diegetic_music:",
	} {
		if !strings.Contains(got, sec) {
			t.Fatalf("缺少段落 %s\n%s", sec, got)
		}
	}
	if !strings.Contains(got, "<Subject 1>") || !strings.Contains(got, "<Picture 1>") {
		t.Fatalf("Subject 未绑定图位\n%s", got)
	}
	if !strings.Contains(got, "[Shot 1] At 00:00.000") {
		t.Fatalf("镜头 1 时间码不对\n%s", got)
	}
	if !strings.Contains(got, "[Shot 2] At 00:03.000") {
		t.Fatalf("镜头 2 时间码不连续\n%s", got)
	}
	if !strings.Contains(got, "<d>[Chinese] 坐。</d>") {
		t.Fatalf("台词格式不对\n%s", got)
	}
	if !strings.Contains(got, "non_diegetic_music:/nN/A") {
		t.Fatalf("无 BGM 应写 N/A\n%s", got)
	}
	// 段落顺序必须固定
	order := []string{"subject_definitions:", "summary:", "retention_analysis:",
		"detailed_description:", "overall_soundscape:", "non_diegetic_music:"}
	last := -1
	for _, sec := range order {
		i := strings.Index(got, sec)
		if i < last {
			t.Fatalf("段落顺序错乱：%s 出现在前面\n%s", sec, got)
		}
		last = i
	}
}
