package service

import (
	"regexp"
	"strconv"
	"strings"
)

// ageRe 从「29岁」「62岁」这类写法里取年龄数字
var ageRe = regexp.MustCompile(`(\d{1,3})\s*岁`)

// parseAge 取文本里第一个年龄，取不到返回 0
func parseAge(text string) int {
	m := ageRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 || n > 120 {
		return 0
	}
	return n
}

// SuggestVoiceTone 按性别与性格给一个音色描述兜底
//
// 为什么需要兜底：音色描述是「编辑角色」里的可选项，但用户打开弹窗时
// 空白输入框等于让他从零开始写。识别阶段先给一版合理的默认值，
// 用户改两个字就能用。
//
// 竞品的写法是「音质定位：音域，音色质感，语速」，这里保持一致。
func SuggestVoiceTone(identityAnchor, personality string) string {
	text := identityAnchor + " " + personality

	// 性别判定：先找明确的性别词，找不到就不猜
	female := strings.Contains(text, "女")
	male := strings.Contains(text, "男")
	if !female && !male {
		// 无法判定就不猜性别，只给通用描述，避免给错
		return "自然中声线：中音域，音色干净，语速适中。"
	}

	// 年龄影响音色厚度：优先读「N岁」，读不到再退回关键词
	age := parseAge(text)
	var band int // 0 中年 / -1 年轻 / 1 年长
	switch {
	case age > 0 && age <= 30:
		band = -1
	case age >= 55:
		band = 1
	case age > 0:
		band = 0
	default:
		switch {
		case containsAny(text, "老", "花白", "退休"):
			band = 1
		case containsAny(text, "年轻", "少年", "少女", "学生", "实习"):
			band = -1
		}
	}

	// 性格影响语速与语气
	gentle := containsAny(text, "温柔", "安静", "内敛", "克制", "话少", "寡言", "细腻")
	lively := containsAny(text, "活泼", "开朗", "热情", "健谈", "话多", "外向")
	cold := containsAny(text, "冷", "高冷", "疏离", "强势", "严肃")

	var b strings.Builder
	switch {
	case female && band > 0:
		b.WriteString("慈和女声：中低音域，音色温厚略带沙哑，语速偏慢")
	case female && band < 0:
		b.WriteString("清亮女声：中高音域，音色干净通透，语速轻快")
	case female:
		b.WriteString("温和女声：中音域，音色柔和细腻，语速适中")
	case male && band > 0:
		b.WriteString("沉稳男声：低音域，音色厚重略带颗粒感，语速偏慢")
	case male && band < 0:
		b.WriteString("松弛年轻男音：中音域，清朗温厚，语速稍快")
	default:
		b.WriteString("沉稳男声：中低音域，音色厚实，语速适中")
	}

	switch {
	case gentle:
		b.WriteString("，语气轻缓，句尾收得干净。")
	case lively:
		b.WriteString("，语气明快，重音清晰。")
	case cold:
		b.WriteString("，语气平稳克制，起伏小。")
	default:
		b.WriteString("，语气自然，起伏适度。")
	}

	return b.String()
}
