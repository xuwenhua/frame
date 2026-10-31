package service

import (
	"regexp"
	"strings"

	"jikeai-clone/internal/model"
)

// atMentionRe 匹配正文里的 @资产名
// 名字允许中文、字母、数字、下划线、连字符，遇到空白和常见中文标点就结束
var atMentionRe = regexp.MustCompile(`@([\p{Han}A-Za-z0-9_\-]+)`)

// DropUnknownMentions 把正文里「@了但资产清单里没有」的名字降级成普通文字
//
// 为什么需要：
//   - LLM 偶尔会照着剧本自己造资产名（剧本写「面霜」，项目里其实没有「面霜」这个道具），
//     这种 @名 在前端会被渲染成 chip、参与参考图对应，但它没有对应图片，
//     提交给上游时也是纯文字 —— 反而会让模型以为有一张不存在的参考图。
//   - 参考站的做法是资产清单严格受限，正文只用清单内的名字。
//
// known 是允许保留的名字集合（角色 + 场景 + 道具）。
func DropUnknownMentions(text string, known map[string]bool) string {
	if strings.TrimSpace(text) == "" || len(known) == 0 {
		return text
	}
	return atMentionRe.ReplaceAllStringFunc(text, func(m string) string {
		name := strings.TrimPrefix(m, "@")
		if known[name] {
			return m
		}
		return name
	})
}

// KnownAssetNames 收集一帧允许出现的资产名
func KnownAssetNames(roles []model.Character, scenes []model.Scene, items []model.Item) map[string]bool {
	known := make(map[string]bool, len(roles)+len(scenes)+len(items))
	for _, r := range roles {
		if n := strings.TrimSpace(r.Name); n != "" {
			known[n] = true
		}
	}
	for _, s := range scenes {
		if n := strings.TrimSpace(s.Name); n != "" {
			known[n] = true
		}
	}
	for _, it := range items {
		if n := strings.TrimSpace(it.Name); n != "" {
			known[n] = true
		}
	}
	return known
}

// SanitizeMentions 对一帧的所有文本字段统一做 @名 校验
//
// 只处理正文（动作/台词/音效/表演/锁定/场景状态），
// 出镜清单一栏由 BuildFrameVideoPrompt 从实体生成，本身就是准的。
func SanitizeMentions(d *FrameDraft, known map[string]bool) {
	if d == nil || len(known) == 0 {
		return
	}
	d.Situation = DropUnknownMentions(d.Situation, known)
	d.Action = DropUnknownMentions(d.Action, known)
	d.SFX = DropUnknownMentions(d.SFX, known)
	d.Performance = DropUnknownMentions(d.Performance, known)
	d.Locking = DropUnknownMentions(d.Locking, known)
	for i := range d.Dialogues {
		d.Dialogues[i].Line = DropUnknownMentions(d.Dialogues[i].Line, known)
	}
	for i := range d.Shots {
		d.Shots[i].Action = DropUnknownMentions(d.Shots[i].Action, known)
		d.Shots[i].SFX = DropUnknownMentions(d.Shots[i].SFX, known)
		for j := range d.Shots[i].Dialogues {
			d.Shots[i].Dialogues[j].Line = DropUnknownMentions(d.Shots[i].Dialogues[j].Line, known)
		}
	}
}
