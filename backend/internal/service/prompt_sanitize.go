package service

import (
	"fmt"
	"regexp"
	"strings"
)

// shotSecondsRe 匹配「单镜时长 3-5 秒」或「单镜时长 10 秒」
var shotSecondsRe = regexp.MustCompile(`单镜时长\s*\d+\s*(?:[-~至]\s*\d+\s*)?秒`)

// SanitizeVideoPrompt 提交给上游前的提示词对齐
//
// 只做「消矛盾」，不动 @资产 引用：
// 参考站的提示词本身就是用 @角色/@道具 指代资产的，配套把资产图当参考图一起传，
// 所以这里不能把 @ 去掉。
//
// 要做的是两件事：
//  1. 「[视觉风格]」里的画幅改成工具栏实际选中的比例，
//     否则会出现「提示词写竖屏 9:16、实际出 16:9 横屏」这种自相矛盾
//  2. 「单镜时长 3-5 秒」改成实际选择的时长
func SanitizeVideoPrompt(prompt, aspect string, duration int) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	out := alignAspect(prompt, aspect)
	out = alignShotDuration(out, duration)
	return out
}

// aspectOrientation 比例 → 中文画幅方向
func aspectOrientation(aspect string) string {
	switch strings.TrimSpace(aspect) {
	case "9:16", "3:4", "2:3":
		return "竖屏"
	case "16:9", "4:3", "3:2":
		return "横屏"
	case "1:1":
		return "方形"
	case "21:9":
		return "超宽屏"
	}
	return ""
}

// normalizeRatio 从 "16:9" / "16:9 (Widescreen)" 里取比例简写
func normalizeRatio(aspect string) string {
	ratio := strings.TrimSpace(aspect)
	if idx := strings.Index(ratio, " "); idx > 0 {
		ratio = ratio[:idx]
	}
	return ratio
}

// alignAspect 把「视觉风格」里的画幅改成实际提交的比例
//
// 参考站的做法是**不把画幅写进提示词**：比例由页面顶部开关决定，且由上游
// ResolutionSelector 节点真正生效。所以这里只做「有则改正」，绝不追加 ——
// 追加反而会让提示词和节点参数打架。
//
// 注意：视觉风格可能是「标题行 + 内容行」两行结构（【视觉风格】单独一行），
// 所以要往下找到真正写了比例的那一行，不能只看标题行。
func alignAspect(p, aspect string) string {
	ratio := normalizeRatio(aspect)
	if ratio == "" {
		return p
	}
	dir := aspectOrientation(ratio)
	if dir == "" {
		return p
	}

	ratioRe := regexp.MustCompile(`(竖屏|横屏|方形|超宽屏)?\s*\d{1,2}:\d{1,2}`)
	lines := strings.Split(p, "\n")

	// 定位「视觉风格」所在行
	head := -1
	for i, ln := range lines {
		if strings.Contains(ln, "视觉风格") {
			head = i
			break
		}
	}
	if head < 0 {
		return p
	}

	// 标题行自带比例 → 就地换
	if ratioRe.MatchString(lines[head]) {
		lines[head] = ratioRe.ReplaceAllString(lines[head], dir+" "+ratio)
		return strings.Join(lines, "\n")
	}
	// 否则在标题之后的正文行里找比例
	for i := head + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if ratioRe.MatchString(lines[i]) {
			lines[i] = ratioRe.ReplaceAllString(lines[i], dir+" "+ratio)
			return strings.Join(lines, "\n")
		}
		// 正文行本来没写画幅 → 保持原样（参考站就是不写）
		break
	}
	return p
}

// alignShotDuration 把「单镜时长 3-5 秒」改成实际选择的时长
func alignShotDuration(p string, duration int) string {
	if duration <= 0 {
		return p
	}
	return shotSecondsRe.ReplaceAllString(p, fmt.Sprintf("单镜时长 %d 秒", duration))
}

// PromptAspectLabel 给日志/调试用的画幅描述
func PromptAspectLabel(aspect string) string {
	ratio := normalizeRatio(aspect)
	if dir := aspectOrientation(ratio); dir != "" {
		return dir + " " + ratio
	}
	return ratio
}
