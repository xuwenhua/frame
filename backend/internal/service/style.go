package service

import (
	"regexp"
	"strings"
)

// StyleProfile 从剧本里读出来的视觉风格约束
// 分镜稿往往自己写明了画风（日式水彩 / 深夜食堂质感）、画幅（竖版9:16）、
// 甚至是「无面部 / 背影 / 手部特写」这类镜头约束，出图提示词必须跟着走，
// 不能一律套「写实 8K 三视图」。
type StyleProfile struct {
	// 风格描述，如「日式手绘水彩插画风格，水彩纸纹理，可见铅笔线稿」
	Desc string
	// 写实类（默认）
	Realistic bool
	// 竖版画幅（剧本里写了 竖版 / 9:16 / 竖屏）
	Vertical bool
	// 不露正脸（无面部 / 无脸 / 避正脸 / 背影 / 手部特写）
	NoFace bool
	// 分镜稿（有 镜1｜xxx 这类镜头标题）
	ShotList bool
}

// DetectStyle 扫描剧本，提取风格约束
func DetectStyle(script string) StyleProfile {
	sp := StyleProfile{Realistic: true}

	// 1. 画风
	switch {
	case containsAny(script, "水彩插画", "手绘水彩", "水彩纸纹理"):
		sp.Desc = "日式手绘水彩插画风格，水彩纸纹理，可见铅笔线稿，柔和晕染"
		sp.Realistic = false
	case containsAny(script, "插画", "手绘", "绘本"):
		sp.Desc = "手绘插画风格，柔和线条与平涂色块，纸面纹理"
		sp.Realistic = false
	case containsAny(script, "赛博朋克", "霓虹"):
		sp.Desc = "赛博朋克风格，霓虹光源与冷色调，高对比"
		sp.Realistic = false
	case containsAny(script, "3D", "三维", "CG"):
		sp.Desc = "3D 渲染风格，柔和全局光照，细腻材质"
		sp.Realistic = false
	case containsAny(script, "国风", "古装", "水墨"):
		sp.Desc = "中式国风风格，水墨质感，留白构图"
		sp.Realistic = false
	}

	// 2. 质感补充（剧集/电影参考）
	if containsAny(script, "深夜食堂") {
		if sp.Desc != "" {
			sp.Desc += "，深夜食堂片头质感"
		} else {
			sp.Desc = "日式电视剧风格，深夜食堂片头质感"
			sp.Realistic = false
		}
	} else if containsAny(script, "日式", "日剧") && sp.Desc != "" {
		sp.Desc += "，日式电视剧质感"
	}

	// 3. 画幅
	if containsAny(script, "竖版", "竖屏", "9:16", "9：16") {
		sp.Vertical = true
	}

	// 4. 不露正脸
	if containsAny(script, "无面部", "无脸", "避正脸", "不露脸", "不要正脸", "人物正脸", "面部特写", "背影", "手部特写") {
		sp.NoFace = true
	}

	// 5. 分镜稿
	sp.ShotList = len(shotTitleRe.FindAllString(script, -1)) >= 2

	return sp
}

// DefaultStyle 没有风格线索时的默认值（写实现代剧）
func DefaultStyle() StyleProfile {
	return StyleProfile{Realistic: true}
}

// TailStyle 风格收尾句（拼在提示词末尾）
func (sp StyleProfile) TailStyle() string {
	if sp.Desc != "" {
		return sp.Desc + "，高细节"
	}
	return "写实风格，8K超清"
}

// 说明：三视图 / 空镜 / 白底这类「构图收尾」不在这里定义。
// 它们取决于用户在资产页选的视图样式（人物资产图 / 四视图 / 概念图 / 特写…），
// 由 GenOptions.framing() 在提交生成时追加，见 gen_options.go。

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// ==================== 分镜稿解析 ====================

// 镜1｜傍晚院区外景（城市入场）
var shotTitleRe = regexp.MustCompile(`(?m)^\s*镜\s*\d+\s*[｜|]\s*(.+)$`)

// 镜头标题里的地点词（按优先级，长词在前避免「诊室门口」被「诊室」抢先）
var placeWords = []string{
	"院区外景", "医院院区", "诊室门口", "走廊尽头", "三楼走廊", "二楼走廊", "医院走廊",
	"诊室", "病房", "走廊", "大厅", "电梯厅", "电梯", "候诊区", "挂号处", "药房",
	"街道", "路口", "天台", "停车场", "楼梯间", "大厅门口", "门口",
}

// timeWords 时间线索
var timeWords = []struct {
	word string
	val  string
}{
	{"傍晚", "傍晚"}, {"黄昏", "傍晚"},
	{"深夜", "深夜"}, {"夜晚", "夜晚"}, {"夜里", "夜晚"}, {"夜色", "夜晚"}, {"晚上", "夜晚"},
	{"清晨", "清晨"}, {"早晨", "早晨"}, {"上午", "白天"}, {"中午", "白天"}, {"下午", "白天"}, {"白天", "白天"},
}
