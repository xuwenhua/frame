package handler

import "testing"

// 同一批角色在不同帧里被 LLM 以不同顺序带出来时，图位必须一致
// （否则这一帧「图1=老师」，下一帧「图1=学生」，人工核对对不上号）
func TestSortRefSlotsByName(t *testing.T) {
	cases := [][]string{
		{"老师", "学生"},
		{"学生", "老师"},
	}
	var first []string
	for i, names := range cases {
		slots := make([]frameRefSlot, 0, len(names))
		for _, n := range names {
			slots = append(slots, frameRefSlot{URL: "/img/" + n + ".png", Name: n})
		}
		sortRefSlotsByName(slots)

		got := make([]string, 0, len(slots))
		for _, s := range slots {
			got = append(got, s.Name)
		}
		if i == 0 {
			first = got
			continue
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("输入顺序不同但排序结果不一致：%v vs %v", first, got)
			}
		}
	}

	// 三个角色也要稳定
	slots := []frameRefSlot{{Name: "学生"}, {Name: "医生"}, {Name: "老师"}}
	sortRefSlotsByName(slots)
	want := []string{"医生", "学生", "老师"}
	for i, s := range slots {
		if s.Name != want[i] {
			t.Fatalf("排序结果 %v，期望 %v", slots, want)
		}
	}
}
