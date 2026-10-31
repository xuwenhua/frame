package service

import (
	"strings"
	"testing"

	"jikeai-clone/internal/model"
)

// 清单外的 @名 要降级成普通文字，清单内的保持不变
func TestDropUnknownMentions(t *testing.T) {
	known := KnownAssetNames(
		[]model.Character{{Name: "生涯规划老师"}, {Name: "高中生"}},
		[]model.Scene{{Name: "公司会议室-白天"}},
		nil,
	)

	in := "@生涯规划老师 把 @面霜 举起来，@高中生 看着 @公司会议室-白天 的窗户。"
	got := DropUnknownMentions(in, known)

	if !strings.Contains(got, "@生涯规划老师") {
		t.Errorf("清单内的角色被去掉了: %s", got)
	}
	if !strings.Contains(got, "@高中生") {
		t.Errorf("清单内的角色被去掉了: %s", got)
	}
	if strings.Contains(got, "@面霜") {
		t.Errorf("清单外的 @面霜 没降级: %s", got)
	}
	if !strings.Contains(got, "面霜") {
		t.Errorf("文字本身不该丢: %s", got)
	}
}

// 名字里带连字符/数字的资产也要能正确识别边界
func TestDropUnknownMentionsBoundary(t *testing.T) {
	known := map[string]bool{"公司会议室-白天": true}
	got := DropUnknownMentions("@公司会议室-白天，光很好。@面霜-2 放着。", known)
	if !strings.Contains(got, "@公司会议室-白天") {
		t.Errorf("带连字符的合法名被截断: %s", got)
	}
	if strings.Contains(got, "@面霜-2") {
		t.Errorf("非法名没降级: %s", got)
	}
}

// 整帧清洗必须覆盖所有正文块，且不能碰出镜清单（那份由实体生成）
func TestSanitizeMentionsCoversAllBlocks(t *testing.T) {
	known := map[string]bool{"女主": true}
	d := FrameDraft{
		Situation:   "@女主 坐着，@沙发 在身后。",
		Action:      "@女主 拿起 @面霜。",
		SFX:         "@面霜 落桌的响声。",
		Performance: "@女主 很满意，@路人 旁观。",
		Locking:     "@女主 在左，@面霜 在手中。",
		Shots: []FrameShot{{
			Action: "@女主 举起 @面霜。",
			SFX:    "@面霜 的轻响。",
			Dialogues: []FrameDialogue{
				{Role: "女主", Line: "这个 @面霜 真好用，@沙发 上试试。"},
			},
		}},
	}
	SanitizeMentions(&d, known)

	for _, s := range []string{d.Situation, d.Action, d.SFX, d.Performance, d.Locking,
		d.Shots[0].Action, d.Shots[0].SFX, d.Shots[0].Dialogues[0].Line} {
		if strings.Contains(s, "@面霜") || strings.Contains(s, "@沙发") || strings.Contains(s, "@路人") {
			t.Errorf("残留非法 @名: %s", s)
		}
	}
	if !strings.Contains(d.Action, "@女主") {
		t.Errorf("合法 @名 被误删: %s", d.Action)
	}
}

// 空清单时不做任何改动（避免把整段 @ 全剥掉）
func TestDropUnknownMentionsEmptyKnown(t *testing.T) {
	in := "@女主 拿起 @面霜。"
	if got := DropUnknownMentions(in, nil); got != in {
		t.Errorf("空清单不该改动文本，实际 %s", got)
	}
}
