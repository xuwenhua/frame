package service

import (
	"strings"
	"testing"

	"jikeai-clone/internal/model"
)

// 首帧提示词要短：模型看图比读文字准，把每个道具的颜色材质都复述一遍
// 会把提示词撑到两千多字，模型反而抓不住重点。
// 只保留「谁是谁 + 保持资产一致 + 姿态按文字 + 朝向」。
func TestGroupFirstFramePromptStaysShort(t *testing.T) {
	roles := []model.Character{{Name: "老师"}, {Name: "学生"}}
	scenes := []model.Scene{{Name: "教师办公室-白天"}}
	items := []model.Item{
		{Name: "深蓝色保温杯", ImagePrompt: strings.Repeat("哑光烤漆不锈钢外壳，深蓝色，直筒圆柱形，高约22厘米，", 4)},
		{Name: "专业介绍册", ImagePrompt: strings.Repeat("铜版纸印刷，骑马钉装订，封面为白色底配蓝色与橙色横向色块，", 4)},
		{Name: "桁架桥梁模型", ImagePrompt: strings.Repeat("手工桁架桥梁模型，由浅色桐木条粘接而成，长约50厘米，", 4)},
	}
	desc := "教师办公室内，一张办公桌横在房间中部，老师坐在办公桌后侧的布面椅上、身体朝向对面，" +
		"学生坐在办公桌前侧的椅子上、身体朝老师，两人隔着办公桌相对而坐、视线交汇；右侧有窗，自然光斜入。"

	p := BuildGroupFirstFramePrompt(desc, "", roles, scenes, items)
	n := len([]rune(p))
	t.Logf("提示词 %d 字", n)

	if n > 700 {
		t.Errorf("提示词太长（%d 字），应该只讲一致性，不复述每个道具的外观", n)
	}
	// 关键约束不能丢
	for _, must := range []string{"{{ref 1}}=老师", "{{ref 4}}=深蓝色保温杯", "相对而坐", "不要照抄参考图里的人像站姿"} {
		if !strings.Contains(p, must) {
			t.Errorf("缺少关键约束 %q", must)
		}
	}
	// 道具的详细外观不该出现在提示词里
	if strings.Contains(p, "哑光烤漆不锈钢") {
		t.Error("道具外观描述不该复述，交给参考图")
	}
}
