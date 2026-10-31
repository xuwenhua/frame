package service

import (
	"strings"
	"testing"
)

// 音色兜底要能区分性别、年龄、性格三档，且不能出现明显的错配
func TestSuggestVoiceTone(t *testing.T) {
	cases := []struct {
		name     string
		identity string
		persona  string
		wantIn   []string
		wantNot  []string
	}{
		{
			name:     "年轻男性",
			identity: "男性，29岁，设计师",
			persona:  "话少但细心",
			wantIn:   []string{"男", "清朗"},
			wantNot:  []string{"女声", "慈和"},
		},
		{
			name:     "年长女性",
			identity: "女性，62岁，退休教师",
			persona:  "温和",
			wantIn:   []string{"女", "中低音域", "语速偏慢"},
			wantNot:  []string{"男声"},
		},
		{
			name:     "年轻女性",
			identity: "女性，22岁，实习生",
			persona:  "活泼开朗",
			wantIn:   []string{"女", "中高音域", "轻快"},
			wantNot:  []string{"男声"},
		},
		{
			name:     "年长男性",
			identity: "男性，58岁，门卫",
			persona:  "话少",
			wantIn:   []string{"男", "低音域"},
			wantNot:  []string{"女声"},
		},
		{
			name:     "性别未知不瞎猜",
			identity: "身份不明",
			persona:  "",
			wantIn:   []string{"自然中声线"},
			wantNot:  []string{"男声", "女声"},
		},
	}

	for _, c := range cases {
		got := SuggestVoiceTone(c.identity, c.persona)
		if got == "" {
			t.Fatalf("%s: 不应为空", c.name)
		}
		for _, w := range c.wantIn {
			if !strings.Contains(got, w) {
				t.Errorf("%s: 期望含「%s」，实际 %q", c.name, w, got)
			}
		}
		for _, w := range c.wantNot {
			if strings.Contains(got, w) {
				t.Errorf("%s: 不应含「%s」，实际 %q", c.name, w, got)
			}
		}
		// 竞品格式是「音色定位：音域，质感，语速」——至少要有冒号和句号
		if !strings.Contains(got, "：") || !strings.HasSuffix(got, "。") {
			t.Errorf("%s: 格式应形如「xxx：xxx，xxx。」，实际 %q", c.name, got)
		}
	}
}
