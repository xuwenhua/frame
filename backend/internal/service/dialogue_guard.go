package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"jikeai-clone/internal/model"
)

// 分镜生成后的一道校验：剧本里写死的对白，有没有被忠实落到分镜上。
//
// 为什么需要：分镜完全交给 LLM，实测会出现三类问题 ——
//  1. 漏台词：剧本写了三句，分镜里只出现两句
//  2. 重复：同一句被塞进两个段落（多半是两套提示词串了）
//  3. 拆断：一句台词被拆到相邻两段（「又怕」+「就是去工地」）
//
// LLM 自己很难自查这三件事，所以在代码里比对一遍，把问题作为 warning
// 返回给前端。这里只做检测、不自动改写 —— 改台词的风险比漏台词更高。

// scriptDialogueRe 剧本里的对白行：`**老师：** 台词` 或 `老师：台词`
var scriptDialogueRe = regexp.MustCompile(`^\s*\*{0,2}\s*@?([^*：:\n]{1,20}?)\s*[：:]\s*\*{0,2}\s*(.+)$`)

// dialogueNorm 比对用的净文：去掉空白和标点。
//
// 直接按字面比会把「AI 只改了个逗号」当成漏台词，误报率高到没法用。
func dialogueNorm(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t\r\n，。！？、…—～·,.!?;；:：\"“”'‘’（）()【】《》[]", r) {
			return -1
		}
		return r
	}, s)
}

// ExtractScriptDialogues 从剧本里抽「必须逐字保留」的对白原文。
//
// 只认「角色：内容」这种行，长度下限 2 个字，避免把场景标题当台词。
func ExtractScriptDialogues(script string) []string {
	out := make([]string, 0, 16)
	seen := map[string]bool{}
	for _, line := range strings.Split(script, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		m := scriptDialogueRe.FindStringSubmatch(t)
		if len(m) < 3 {
			continue
		}
		body := strings.TrimSpace(strings.Trim(strings.TrimSpace(m[2]), "“”\""))
		key := dialogueNorm(body)
		if len([]rune(key)) < 2 || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, body)
	}
	return out
}

type dialogueSegment struct {
	index int
	lines []string // 归一化，用于比对
	raw   []string // 原文，用于判断断点位置
}

// DialogueIssues 比对分镜与剧本对白，返回问题清单（空表示没问题）
func DialogueIssues(frames []model.Storyboard, script string) []string {
	src := ExtractScriptDialogues(script)
	if len(src) == 0 {
		return nil
	}

	segs := make([]dialogueSegment, 0, len(frames))
	for _, f := range frames {
		ds := ParsePromptDialogues(f.VideoPrompt)
		lines := make([]string, 0, len(ds))
		raw := make([]string, 0, len(ds))
		for _, d := range ds {
			if k := dialogueNorm(d.Line); k != "" {
				lines = append(lines, k)
				raw = append(raw, d.Line)
			}
		}
		segs = append(segs, dialogueSegment{index: f.FrameIndex, lines: lines, raw: raw})
	}

	issues := make([]string, 0, 4)
	for _, s := range src {
		key := dialogueNorm(s)
		if key == "" {
			continue
		}
		// 分两种命中：
		//   strict  —— 某段的台词**完整包含**这句原文（正常落地）
		//   partial —— 只包含其中一部分（可能是拆断，也可能是 AI 压缩了说法）
		// 先看 strict 再谈 partial：否则「前半 + 后半分居两段」会被误判成"重复"。
		var strict, partial []int
		for _, sg := range segs {
			for _, l := range sg.lines {
				if strings.Contains(l, key) {
					strict = append(strict, sg.index)
					break
				}
				if len([]rune(l)) >= 4 && strings.Contains(key, l) {
					partial = append(partial, sg.index)
					break
				}
			}
		}
		switch {
		case len(strict) > 1:
			issues = append(issues, fmt.Sprintf("台词在第 %s 段重复出现：%s", joinInts(strict), s))
		case len(strict) == 1:
			// 正常
		case len(partial) > 0:
			// 一句原文被拆成多句是正常的（一镜一句），只要这些片段拼起来
			// 能覆盖原文就算完整 —— 以前只看单个片段，把合法拆分全成了"只说了一半"。
			if coversAfterJoin(segs, partial, key) {
				continue
			}
			if len(partial) > 1 {
				issues = append(issues, fmt.Sprintf("台词被拆到第 %s 段且拼不回原句：%s",
					joinInts(partial), s))
			} else {
				issues = append(issues, fmt.Sprintf("台词在第 %d 段只说了一半：%s", partial[0], s))
			}
		default:
			issues = append(issues, "漏台词："+s)
		}
	}
	return issues
}

// coversAfterJoin 把若干段的台词拼起来（按段号顺序、去重），看能否覆盖整句原文。
//
// 一句长台词被拆成「那就具备基本条件。」「就业不只工地：设计院…」「考公…流动性大。」
// 落到三个镜头里是正常写法 —— 一个镜头一句；只要拼回去是完整的就没问题。
func coversAfterJoin(segs []dialogueSegment, indexes []int, key string) bool {
	sorted := append([]int(nil), indexes...)
	sort.Ints(sorted)

	var sb strings.Builder
	for _, idx := range sorted {
		for _, sg := range segs {
			if sg.index != idx {
				continue
			}
			for _, l := range sg.lines {
				sb.WriteString(l)
			}
			break
		}
	}
	return strings.Contains(sb.String(), key)
}

// endsAtSentence 某一段的台词是否以句末标点收尾（说明是按句拆段，不是拦腰截断）
func endsAtSentence(segs []dialogueSegment, index int) bool {
	for _, sg := range segs {
		if sg.index != index {
			continue
		}
		for _, r := range sg.raw {
			t := strings.TrimSpace(r)
			if t == "" {
				continue
			}
			last := []rune(t)[len([]rune(t))-1]
			if strings.ContainsRune("。！？.!?", last) {
				return true
			}
		}
	}
	return false
}

func joinInts(v []int) string {
	parts := make([]string, 0, len(v))
	for _, n := range v {
		parts = append(parts, fmt.Sprintf("%d", n))
	}
	return strings.Join(parts, "、")
}
