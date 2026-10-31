package service

import (
	"fmt"
	"regexp"
	"strings"
)

// SketchFrame 一帧里的一个镜头：首帧图的构图描述、视频提示词的站位都按它取。
type SketchFrame struct {
	No   int    // 帧序号（只用于提示词里的编号）
	Size string // 景别，如 中近景
	Desc string // 画面描述：谁在哪、在做什么
	// Label 镜号，如「1-3」= 第 1 段第 3 镜。
	// 一帧可能含多个镜头，镜号必须跟着「段-镜」走，不能按图里的格序临时编。
	Label string
}

// colorWords 颜色词，按「长的在前」排列 —— 必须先替「深蓝色」再替「蓝色」，
// 否则「深蓝色笔记本」会被替换成「深笔记本」。
var colorWords = []string{
	"深蓝色", "浅蓝色", "深灰色", "浅灰色", "深棕色", "浅棕色",
	"深褐色", "浅褐色", "浅米色", "米白色", "银灰色", "酒红色", "卡其色",
	"深蓝", "浅蓝", "深灰", "浅灰", "深棕", "浅棕", "深褐", "浅褐",
	"米色", "黑色", "白色", "红色", "蓝色", "绿色", "黄色", "灰色", "棕色",
	"粉色", "紫色", "橙色", "银色", "金色", "青色", "咖啡色", "杏色",
}

// StripColorWords 把描述里的颜色词拿掉。
//
// 出图提示词里颜色由参考图负责，文字再写一遍容易和参考图打架；
// 服装有独立的锁定块，描述词本身不需要颜色。
func StripColorWords(s string) string {
	for _, c := range colorWords {
		// 「深蓝色的笔记本」要整体替掉，否则留下一个孤零零的「的」
		s = strings.ReplaceAll(s, c+"的", "")
		s = strings.ReplaceAll(s, c, "")
	}
	return s
}

// shotHeadRe 匹配镜头头：｜镜头1｜ 0–1秒｜中景｜固定机位｜
var shotHeadRe = regexp.MustCompile(`^｜镜头\s*(\d+)｜\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*秒｜\s*([^｜]*)(?:｜\s*([^｜]*))?`)

// ParseFrameShots 把一帧的描述词拆成「镜头列表」。
//
// 一帧 = 一段，段里可能有 1–3 个镜头。每格描述只取该镜头的【动作】块：
// 台词不进来（图里不写字），姿态/站位变化都在动作里。
func ParseFrameShots(vp string) []SketchFrame {
	if strings.TrimSpace(vp) == "" {
		return nil
	}
	// H3 结构（【Shot N｜起止秒｜…】）优先
	if out := parseShotBlocks(vp); len(out) > 0 {
		return out
	}
	// 上一版平铺格式（镜号/景别/运镜/情绪动作/台词/音效/时长）
	if out := parseFieldShots(vp); len(out) > 0 {
		return out
	}
	var shots []SketchFrame
	cur := -1
	inAction := false

	for _, raw := range strings.Split(vp, "\n") {
		t := strings.TrimSpace(raw)
		if m := shotHeadRe.FindStringSubmatch(t); m != nil {
			shots = append(shots, SketchFrame{No: len(shots) + 1, Size: strings.TrimSpace(m[4])})
			cur = len(shots) - 1
			inAction = false
			continue
		}
		if cur < 0 {
			continue
		}
		switch {
		case strings.HasPrefix(t, "【动作】"):
			inAction = true
			rest := strings.TrimSpace(strings.TrimPrefix(t, "【动作】"))
			// 【动作】（按台词顺序推进…）这一行是说明，不是画面内容
			if rest != "" && !strings.HasPrefix(rest, "（") {
				shots[cur].Desc += rest
			}
		case strings.HasPrefix(t, "【"):
			inAction = false
		case inAction && t != "":
			shots[cur].Desc += t
		}
	}

	for i := range shots {
		d := strings.ReplaceAll(shots[i].Desc, "@", "")
		shots[i].Desc = StripColorWords(strings.Join(strings.Fields(d), " "))
	}
	return shots
}

// FrameScene 取一帧的【素材与场景状态】（人物站位与空间）。
//
// 首帧图的构图描述、视频提示词的站位约束都从这里取：
// 它写的是「这一段开始时谁在哪」，是空间关系唯一的文字来源。
func FrameScene(vp string) string {
	lines := strings.Split(vp, "\n")
	// 新格式：站位并进了「地点：@场景名 站位描述」
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "地点：") {
			rest := strings.TrimSpace(strings.TrimPrefix(t, "地点："))
			// 去掉开头的 @场景名（可能有多个，用「、」连着）
			for strings.HasPrefix(rest, "@") {
				i := strings.IndexAny(rest, " 、，")
				if i < 0 {
					rest = ""
					break
				}
				rest = strings.TrimSpace(strings.TrimPrefix(rest[i:], "、"))
			}
			if rest == "" {
				continue
			}
			d := strings.ReplaceAll(rest, "@", "")
			return StripColorWords(strings.Join(strings.Fields(d), " "))
		}
	}
	// 老格式：【素材与场景状态】的下一行
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "【素材与场景状态】") && i+1 < len(lines) {
			d := strings.ReplaceAll(strings.TrimSpace(lines[i+1]), "@", "")
			return StripColorWords(strings.Join(strings.Fields(d), " "))
		}
	}
	return ""
}

// shotHeaderRe 匹配官方时间轴写法的镜头头：[Shot 1] 0.00–3.00s，双人中景，主体 @老师
var shotHeaderRe = regexp.MustCompile(`^\[Shot\s*(\d+)\]\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*s[，,]\s*([^，,]*)`)

// shotHeaderLegacyRe 匹配上一版的中文镜头头：【Shot 1｜0–2秒｜双人中景·@老师】
var shotHeaderLegacyRe = regexp.MustCompile(`^【Shot\s*(\d+)｜\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*秒｜\s*([^】]*)】`)

// dialogueTagRe 匹配整段 <d>…</d>：从画面描述里剥台词时用，
// 首帧图只画人，不画「他说了什么」
var dialogueTagRe = regexp.MustCompile(`(?s)<d>.*?</d>`)

// parseShotBlocks 解析官方时间轴结构的镜头块：
//
//	[Shot 1] 0.00–3.00s，双人中景，主体 @老师
//	【主体】…【动作】…【镜头】…【音效】…【约束】…
//
// 只取景别和「主体 + 动作」两样：首帧图靠景别定构图、靠动作定人物姿态；
// 运镜和约束是给视频模型的，首帧图用不上。
func parseShotBlocks(vp string) []SketchFrame {
	var shots []SketchFrame
	cur := -1
	for _, raw := range strings.Split(vp, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		m := shotHeaderRe.FindStringSubmatch(t)
		legacy := false
		if m == nil {
			m = shotHeaderLegacyRe.FindStringSubmatch(t)
			legacy = true
		}
		if m != nil {
			no := 0
			fmt.Sscanf(m[1], "%d", &no)
			if no <= 0 {
				no = len(shots) + 1
			}
			size := strings.TrimSpace(m[4])
			if !legacy {
				// 新写法：景别是第一个逗号前那段，「主体 @老师」在后面
				size = strings.TrimSpace(strings.TrimPrefix(size, "景别 "))
			}
			// 「双人中景·@老师」→ 景别只取「·」前那半
			if i := strings.Index(size, "·"); i > 0 {
				size = strings.TrimSpace(size[:i])
			}
			shots = append(shots, SketchFrame{No: no, Size: size})
			cur = len(shots) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		switch {
		case strings.HasPrefix(t, "【主体】"):
			shots[cur].Desc += strings.TrimSpace(strings.TrimPrefix(t, "【主体】"))
		case strings.HasPrefix(t, "【动作】"):
			shots[cur].Desc += strings.TrimSpace(strings.TrimPrefix(t, "【动作】"))
		}
	}
	for i := range shots {
		d := dialogueTagRe.ReplaceAllString(shots[i].Desc, "")
		d = strings.ReplaceAll(d, "@", "")
		shots[i].Desc = StripColorWords(strings.Join(strings.Fields(d), " "))
	}
	return shots
}

// parseFieldShots 解析「镜号：N / 景别：X/Y / 运镜：Z / 情绪动作：… / 台词：… / 音效：… / 时长：Ns」这种分段写法。
//
// 这是上一版 BuildFrameVideoPrompt 的输出格式；库里已有的分镜还是这个格式，
// 换结构后不能读不出镜头，所以保留。
func parseFieldShots(vp string) []SketchFrame {
	var shots []SketchFrame
	cur := -1
	for _, raw := range strings.Split(vp, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "镜号：") {
			no := 0
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(t, "镜号：")), "%d", &no)
			if no <= 0 {
				no = len(shots) + 1
			}
			shots = append(shots, SketchFrame{No: no})
			cur = len(shots) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		switch {
		case strings.HasPrefix(t, "景别："):
			// 「中景/老师」→ 景别只取斜杠前的部分
			v := strings.TrimSpace(strings.TrimPrefix(t, "景别："))
			if i := strings.Index(v, "/"); i > 0 {
				v = v[:i]
			}
			shots[cur].Size = v
		case strings.HasPrefix(t, "情绪/动作："), strings.HasPrefix(t, "情绪动作："):
			v := t
			if i := strings.Index(v, "："); i > 0 {
				v = v[i+len("："):]
			}
			shots[cur].Desc = strings.TrimSpace(v)
		}
	}
	for i := range shots {
		d := strings.ReplaceAll(shots[i].Desc, "@", "")
		shots[i].Desc = StripColorWords(strings.Join(strings.Fields(d), " "))
	}
	return shots
}
