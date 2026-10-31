package service

import (
	"fmt"
	"regexp"
	"strings"

	"jikeai-clone/internal/model"
)

// StoryboardService 分镜生成服务
// 当前使用规则引擎做拆分（不依赖外部 LLM），
// 后续接入 LLM 时只需替换 buildText / buildVideoPrompt 的实现。
type StoryboardService struct{}

func NewStoryboardService() *StoryboardService {
	return &StoryboardService{}
}

// FrameDraft 生成中的分镜草稿
type FrameDraft struct {
	FrameIndex  int
	// EpisodeNo 第几集，只用于描述词里的「场次：1-3」
	EpisodeNo   int
	Text        string
	VideoPrompt string
	Roles       []model.Character
	Scenes      []model.Scene
	Items       []model.Item

	// 以下字段对应参考站的「视频描述词」结构：
	// 【素材与场景状态】/ 镜头行 / 台词 / 音效 / 表演 / 关键锁定
	// 规则引擎路径给不出这些内容时留空，由 BuildFrameVideoPrompt 兜底。
	Situation   string
	ShotSize    string
	Camera      string
	Action      string
	Dialogues   []FrameDialogue
	SFX         string
	Performance string
	Locking     string

	// Shots 一帧内的多个镜头。参考站的一条视频描述词通常包含 2-3 个 ｜镜头N｜，
	// 时间累计（0–5秒 / 5–8秒 / 8–13秒）；为空时用上面平铺的字段合成单镜。
	Shots []FrameShot

	// Duration 这一帧的出片时长（秒）。工具栏选「自动」时按内容估算，
	// 落库后前端直接读它显示，提交视频也用它。
	Duration int
}

// Generate 把剧本拆成多帧分镜
func (s *StoryboardService) Generate(
	script string,
	chars []model.Character,
	scenes []model.Scene,
	items []model.Item,
) []FrameDraft {
	// 1. 按段落拆
	segments := splitScript(script)
	if len(segments) == 0 {
		return nil
	}

	// 2. 每段匹配资产、生成 text + video_prompt
	drafts := make([]FrameDraft, 0, len(segments))
	for i, seg := range segments {
		matchedRoles := matchRoles(seg, chars)
		matchedScenes := matchScenes(seg, scenes)
		matchedItems := matchItems(seg, items)

		// 本段没匹配到场景时，沿用上一个场景
		if len(matchedScenes) == 0 && i > 0 && len(drafts) > 0 {
			matchedScenes = drafts[i-1].Scenes
		}
		// 没匹配到角色时，沿用上一个
		if len(matchedRoles) == 0 && i > 0 && len(drafts) > 0 {
			matchedRoles = drafts[i-1].Roles
		}

		draft := FrameDraft{
			FrameIndex: i + 1,
			Roles:      matchedRoles,
			Scenes:     matchedScenes,
			Items:      matchedItems,
		}
		draft.Text = buildText(draft, seg)
		var prompt string
		prompt, draft = buildVideoPrompt(draft, seg)
		draft.VideoPrompt = prompt
		// 台词拆出来后才知道这一帧该多长（规则引擎原来一律 0，前端只能显示 3 秒）
		draft.Duration = EstimateFrameDuration(draft)
		drafts = append(drafts, draft)
	}
	return drafts
}

// splitScript 把剧本拆成可拍摄的段落
func splitScript(script string) []string {
	// 优先按「场景/镜号」这类标记拆
	sceneMark := regexp.MustCompile(`(?m)^\s*(?:场景|镜头|镜号|【|第[\d一二三四五六七八九十]+[场幕镜])`)
	if locs := sceneMark.FindAllStringIndex(script, -1); len(locs) > 1 {
		segs := make([]string, 0, len(locs))
		for i := range locs {
			start := locs[i][0]
			end := len(script)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			if s := strings.TrimSpace(script[start:end]); len(s) > 8 {
				segs = append(segs, s)
			}
		}
		if len(segs) > 0 {
			return mergeMetaBlocks(segs)
		}
	}

	// 退化为按空行分段
	blocks := strings.Split(script, "\n\n")
	segs := make([]string, 0, len(blocks))
	for _, b := range blocks {
		b = strings.TrimSpace(b)
		if len(b) > 8 {
			segs = append(segs, b)
		}
	}
	if len(segs) > 0 {
		return mergeMetaBlocks(segs)
	}

	// 再退化为按句号切，三句一组合
	sentences := regexp.MustCompile(`[。！？!?]`).Split(script, -1)
	var buf strings.Builder
	count := 0
	for _, sen := range sentences {
		sen = strings.TrimSpace(sen)
		if sen == "" {
			continue
		}
		buf.WriteString(sen)
		buf.WriteString("。")
		count++
		if count >= 3 {
			segs = append(segs, strings.TrimSpace(buf.String()))
			buf.Reset()
			count = 0
		}
	}
	if rest := strings.TrimSpace(buf.String()); len(rest) > 4 {
		segs = append(segs, rest)
	}
	return segs
}

// mergeMetaBlocks 把「纯标记段」（场景/人物/时间/地点行）并入下一段，
// 避免生成只有场景抬头、没有画面的空镜帧。
func mergeMetaBlocks(segs []string) []string {
	metaPrefix := regexp.MustCompile(`^(场景|人物|时间|地点|出场人物|道具)\s*[:：]`)
	isMetaOnly := func(s string) bool {
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if !metaPrefix.MatchString(l) {
				return false
			}
		}
		return true
	}

	out := make([]string, 0, len(segs))
	var pending []string
	for _, s := range segs {
		if isMetaOnly(s) {
			pending = append(pending, s)
			continue
		}
		if len(pending) > 0 {
			s = strings.Join(pending, "\n") + "\n" + s
			pending = nil
		}
		out = append(out, s)
	}
	// 尾部若还有纯标记，丢弃（无画面内容）
	return out
}

func matchRoles(seg string, chars []model.Character) []model.Character {
	var out []model.Character
	for _, c := range chars {
		if c.Name == "" {
			continue
		}
		if strings.Contains(seg, c.Name) || nameAliasHit(seg, c.Name) {
			out = append(out, c)
		}
	}
	return out
}

// nameAliasHit 处理「女主/男主」这类泛称与剧本具体姓名的对应
func nameAliasHit(seg, genericName string) bool {
	// 泛称映射到剧本中的可能写法
	aliasMap := map[string][]string{
		"女主": {"她", "女士", "女主角"},
		"男主": {"他", "先生", "男主角"},
		"女配": {"女配角"},
		"男配": {"男配角"},
	}
	aliases, ok := aliasMap[genericName]
	if !ok {
		return false
	}
	for _, a := range aliases {
		if strings.Contains(seg, a) {
			return true
		}
	}
	return false
}

func matchScenes(seg string, scenes []model.Scene) []model.Scene {
	var out []model.Scene
	for _, s := range scenes {
		// 场景名可能是「家居客厅-白天」，取连字符前的部分做宽松匹配
		key := s.Name
		if idx := strings.IndexAny(key, "-—_"); idx > 0 {
			key = key[:idx]
		}
		if key != "" && strings.Contains(seg, key) {
			out = append(out, s)
		}
	}
	return out
}

func matchItems(seg string, items []model.Item) []model.Item {
	var out []model.Item
	for _, it := range items {
		if it.Name != "" && strings.Contains(seg, it.Name) {
			out = append(out, it)
		}
	}
	return out
}

// buildText 生成人读脚本（[开场站位] [视频画面] [尾帧画面]）
func buildText(d FrameDraft, raw string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%d\n", d.FrameIndex)

	// 场景行
	if len(d.Scenes) > 0 {
		fmt.Fprintf(&b, "【场景：%s】\n", d.Scenes[0].Name)
	} else {
		b.WriteString("【场景：未标注】\n")
	}

	// 开场站位
	b.WriteString("[开场站位]：")
	if len(d.Roles) > 0 {
		names := make([]string, 0, len(d.Roles))
		for _, r := range d.Roles {
			names = append(names, r.Name)
		}
		fmt.Fprintf(&b, "%s入画", strings.Join(names, "、"))
		// 位置：只取场景空间锚定的第一段
		if len(d.Scenes) > 0 && d.Scenes[0].SpatialAnchor != "" {
			anchor := d.Scenes[0].SpatialAnchor
			if idx := strings.IndexAny(anchor, "，,、"); idx > 0 {
				anchor = anchor[:idx]
			}
			fmt.Fprintf(&b, "，位于%s", anchor)
		}
		if len(d.Items) > 0 {
			fmt.Fprintf(&b, "，手持%s", d.Items[0].Name)
		}
		b.WriteString("。\n")
	} else {
		b.WriteString("环境空镜起幅。\n")
	}

	// 视频画面（剧本原文提纯）
	b.WriteString("[视频画面]：")
	b.WriteString(condense(raw, 120))
	b.WriteString("\n")

	// 尾帧画面
	b.WriteString("[尾帧画面]：")
	b.WriteString("[人物状态] ")
	if len(d.Roles) > 0 {
		b.WriteString(d.Roles[0].Name)
		if d.Roles[0].Personality != "" {
			fmt.Fprintf(&b, "（%s）", d.Roles[0].Personality)
		}
	} else {
		b.WriteString("无人物")
	}
	b.WriteString("；[环境状态] ")
	if len(d.Scenes) > 0 {
		if d.Scenes[0].LightingPrompt != "" {
			b.WriteString(d.Scenes[0].LightingPrompt)
		} else {
			b.WriteString(d.Scenes[0].Name)
		}
	} else {
		b.WriteString("延续前一镜")
	}
	b.WriteString("；[关键道具] ")
	if len(d.Items) > 0 {
		b.WriteString(d.Items[0].Name)
	} else {
		b.WriteString("无")
	}

	return b.String()
}

// buildVideoPrompt 生成机读 prompt
//
// 规则引擎路径给不出台词/音效，动作描述用原文压一句兜底；
// 真正的结构由 BuildFrameVideoPrompt 统一负责，保证和 LLM 路径输出一致。
func buildVideoPrompt(d FrameDraft, raw string) (string, FrameDraft) {
	if strings.TrimSpace(d.Action) == "" {
		d.Action = condense(raw, 60)
	}
	// 剧本里的台词常写成「**老师：** 台词」，规则引擎整段抠进 Action，
	// 拆出来才能按时长估算、并输出标准的【角色台词】行
	if len(d.Dialogues) == 0 {
		if action, lines := splitInlineDialogues(d.Action); len(lines) > 0 {
			d.Action = action
			d.Dialogues = lines
		}
	}
	if strings.TrimSpace(d.ShotSize) == "" {
		d.ShotSize = "中景"
	}
	// 规则引擎是从原文里抠句子，也可能带出清单外的 @名
	SanitizeMentions(&d, KnownAssetNames(d.Roles, d.Scenes, d.Items))
	return BuildFrameVideoPrompt(d, 0), d
}

// condense 把原文压成一句画面描述
func condense(raw string, limit int) string {
	// 去掉镜头/场景/道具等标记行
	lines := strings.Split(raw, "\n")
	keep := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		// 跳过纯标记行（场景：/人物：/时间：/地点：/道具：）
		if regexp.MustCompile(`^(场景|人物|时间|地点|道具|物品|出场人物|环境|角色)\s*[:：]`).MatchString(l) {
			continue
		}
		keep = append(keep, l)
	}
	s := strings.Join(keep, " ")
	s = strings.Join(strings.Fields(s), " ")

	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
