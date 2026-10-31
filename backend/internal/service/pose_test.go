package service

import (
	"strings"
	"testing"
)

// 用户真实遇到的一条描述词：里面写了「坐着时上身微微前倾」，
// 结果出图变成了坐姿。资产图必须站姿，这里守两件事：
//  1. 姿态从句要被剥掉
//  2. 构图约束里必须明确要求站立、双脚着地
func TestCharacterAssetMustStand(t *testing.T) {
	realPrompt := "男性，17岁，高三在读学生，气质青涩、认真、略显拘谨。身高约175厘米，身形偏瘦，肩窄，坐着时上身微微前倾。脸型偏长圆，眉毛浓黑，单眼皮，眼睛偏小，鼻梁中等高度，嘴唇略厚，下颌线不明显，肤色偏黄，脸颊有淡痘痕，无妆，黑色短碎发，两侧推短，刘海自然垂在额前。身穿白色短袖校服衬衫，左胸口袋上方别一枚方形校徽，下装深蓝色校服长裤，脚穿白色系带运动鞋，肩上背深灰色双肩书包"

	sp := DetectStyle("竖版9:16，日式手绘水彩插画风格")

	for _, style := range []string{CharStyleAsset, CharStyleQuad, CharStyleConcept} {
		out := ApplyGenOptions(realPrompt, "character", sp, GenOptions{CharStyle: style})

		if strings.Contains(out, "坐着") {
			t.Errorf("style=%s: 姿态从句未剥掉: %s", style, out)
		}
		if strings.Contains(out, "前倾") {
			t.Errorf("style=%s: 仍残留「前倾」: %s", style, out)
		}
		if !strings.Contains(out, "站姿") && !strings.Contains(out, "站立") {
			t.Errorf("style=%s: 缺少站立约束: %s", style, out)
		}
		// 服装、发型这些主体信息不能被误删
		for _, keep := range []string{"白色短袖校服衬衫", "深蓝色校服长裤", "双肩书包"} {
			if !strings.Contains(out, keep) {
				t.Errorf("style=%s: 误删了主体描述「%s」: %s", style, keep, out)
			}
		}
	}
}

// 特写三视图只看脸，不该强行加站姿
func TestCloseupNoStandRequirement(t *testing.T) {
	out := ApplyGenOptions("女性，28岁，温柔", "character", DetectStyle("写实"), GenOptions{CharStyle: CharStyleCloseup})
	if strings.Contains(out, "双脚着地") {
		t.Errorf("面部特写不该要求双脚着地: %s", out)
	}
}

// 场景描述里的「坐在」是正常内容（比如「窗边放着座椅」），不能误删
func TestSceneKeepsSittingWords(t *testing.T) {
	p := "客厅，靠窗摆放一张藤编座椅，沙发上搭着毛毯"
	out := ApplyGenOptions(p, "scene", DetectStyle("写实"), GenOptions{SceneStyle: SceneStyleSingle})
	if !strings.Contains(out, "藤编座椅") {
		t.Errorf("场景描述被误删: %s", out)
	}
}
