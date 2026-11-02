package service

import (
	"strings"
	"testing"

	"jikeai-clone/internal/model"
)

// 规则引擎不能截断台词。
//
// 病根：buildVideoPrompt 里先 condense(raw, 60) 再从中拆台词，
// condense 是给「画面动作描述」限长的，拿来截台词会把后半句直接砍掉
// （实测 4 段台词全断在 53 字）。用户看到的就是「最后一句台词说不完」。
//
// 正确行为：台词完整保留；一段装不下（> 15 秒）就按句末标点拆成多帧，
// 每帧各出一片，而不是砍内容或把语速催到 5 字/秒。
func TestRuleEngineKeepsFullDialogue(t *testing.T) {
	svc := NewStoryboardService()
	line := "三个问题：看到桥和楼会不会想它怎么受力？能不能接受规范多、责任重？能不能接受部分岗位跟着项目走？想清楚这三点，你就知道适不适合。"
	script := "**老师：** " + line

	drafts := svc.Generate(script, []model.Character{{Name: "老师"}}, nil, nil)
	if len(drafts) == 0 {
		t.Fatal("没有生成分镜")
	}

	var got strings.Builder
	for _, d := range drafts {
		if d.Duration > 15 {
			t.Errorf("第 %d 帧时长 %d 秒，超过 H3 单段 15 秒上限", d.FrameIndex, d.Duration)
		}
		if d.Duration <= 0 {
			t.Errorf("第 %d 帧时长没算出来", d.FrameIndex)
		}
		for _, sh := range normalizeShots(d) {
			for _, dl := range sh.Dialogues {
				got.WriteString(dl.Line)
			}
		}
	}

	// 台词里的字一个都不能少（只忽略标点差异）
	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			if strings.ContainsRune("：:，,。.？！?！、；;", r) {
				return -1
			}
			return r
		}, s)
	}
	want, gotChars := strip(line), strip(got.String())
	if gotChars != want {
		t.Errorf("台词被截断或改写\n  期望: %s\n  实际: %s", want, gotChars)
	}

	// 这么长的台词必须拆帧，不能压进一帧
	if len(drafts) < 2 {
		t.Errorf("超长台词应该拆成多帧，实际只有 %d 帧", len(drafts))
	}
}

// 短台词不该被无谓拆开
func TestRuleEngineKeepsShortFrameIntact(t *testing.T) {
	svc := NewStoryboardService()
	drafts := svc.Generate("**学生：** 还行，不算吃力。", []model.Character{{Name: "学生"}}, nil, nil)
	if len(drafts) != 1 {
		t.Fatalf("短台词应保持 1 帧，实际 %d 帧", len(drafts))
	}
	if drafts[0].Duration > 15 || drafts[0].Duration < 3 {
		t.Errorf("时长应在 3–15 秒，实际 %d", drafts[0].Duration)
	}
}
