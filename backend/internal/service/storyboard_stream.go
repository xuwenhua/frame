package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/aiclient"
)

// 分镜的「逐段生成」流程。
//
// 为什么不再一次生成整集：
//   - 一次生成时，写到后面注意力被前面 20 段挤占，细节越写越省
//   - 每段的 situation 是模型各写各的，前后是否一致全靠它记
//   - 任何一段格式出错，整集都要重跑
//
// 改成两阶段：
//  1. 先出大纲（分几段、每段讲什么、台词归谁），很快
//  2. 再逐段细化 —— 每段都带上「上一段的尾帧画面」和「全局站位基准」，
//     位置和姿态是"传下去"的，不是"各写各的"

// OutlineItem 大纲里的一段
type OutlineItem struct {
	Index int      `json:"index"`
	Scene string   `json:"scene"`
	Roles []string `json:"roles"`
	Items []string `json:"items"`
	Beat  string   `json:"beat"`
	Lines []string `json:"lines"`
}

// StreamEvent 推给前端的流式事件
type StreamEvent struct {
	Type    string        `json:"type"` // outline / frame / done / error
	Count   int           `json:"count,omitempty"`
	Index   int           `json:"index,omitempty"`
	Total   int           `json:"total,omitempty"`
	Frame   *FrameDraft   `json:"frame,omitempty"`
	Outline []OutlineItem `json:"outline,omitempty"`
	Message string        `json:"message,omitempty"`
}

const outlineSystemPrompt = `你是短剧导演。给你剧本，先拆「叙事单元大纲」，不写画面细节。

规则：
1. 一段 = 一个完整的问答、或一个连贯的动作单元。段数按内容定，通常 6–12 段
2. 每段 8–15 秒：能装 2–4 个来回的对白
3. 台词必须逐字保留：一句都不许漏、不许改、不许拆到两段
4. 只决定「分几段、每段讲什么、台词归谁」，不要写景别、运镜、画面描述
5. 场景名、角色名、道具名只能从给定清单里选

只输出 JSON，不要解释：
{"outline":[{"index":1,"scene":"场景名","roles":["角色名"],"items":["道具名"],"beat":"这一段发生什么，一句话","lines":["这段要说的台词，逐字照抄，按顺序"]}]}`

func outlineUserPrompt(script string, chars []model.Character, scenes []model.Scene, items []model.Item) string {
	var b strings.Builder
	b.WriteString("【可用场景】\n")
	for _, s := range scenes {
		fmt.Fprintf(&b, "- %s：%s\n", s.Name, fallbackText(s.SpatialAnchor, s.LightingPrompt))
	}
	b.WriteString("\n【可用角色】\n")
	for _, c := range chars {
		fmt.Fprintf(&b, "- %s：%s\n", c.Name, briefIdentity(c.IdentityAnchor))
	}
	if len(items) > 0 {
		b.WriteString("\n【可用道具】\n")
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it.Name)
		}
	}
	b.WriteString("\n【剧本】\n")
	b.WriteString(script)
	return b.String()
}

// PlanOutline 阶段一：把剧本拆成叙事单元大纲
func (s *LLMStoryboardService) PlanOutline(
	ctx context.Context,
	script string,
	chars []model.Character,
	scenes []model.Scene,
	items []model.Item,
) ([]OutlineItem, error) {
	if s.client == nil {
		return nil, fmt.Errorf("没有可用的文本模型")
	}
	res, err := s.client.Chat(ctx, []aiclient.ChatMessage{
		{Role: "system", Content: outlineSystemPrompt},
		{Role: "user", Content: outlineUserPrompt(script, chars, scenes, items)},
	}, 0.3, 8000)
	if err != nil {
		return nil, fmt.Errorf("大纲生成失败: %w", err)
	}
	jsonStr := extractJSON(res.Content)
	if jsonStr == "" {
		return nil, fmt.Errorf("大纲未返回有效 JSON")
	}
	var parsed struct {
		Outline []OutlineItem `json:"outline"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("大纲 JSON 解析失败: %w", err)
	}
	out := make([]OutlineItem, 0, len(parsed.Outline))
	for i, it := range parsed.Outline {
		it.Index = i + 1
		if strings.TrimSpace(it.Beat) == "" && len(it.Lines) == 0 {
			continue
		}
		out = append(out, it)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("大纲为空")
	}
	return out, nil
}

// expandSystemPrompt 单段细化：复用主分镜规范，只是限定「只写这一段」
func expandSystemPrompt() string {
	return storyboardSystemPrompt + StoryboardSkillRules() + `

【本次任务】只写大纲里指定的那一段，不要写别的段落，也不要重复上一段的台词。
输出 JSON 结构为单个 frame 对象（不带外层 frames 数组）：
{"scene_name":"...","role_names":[...],"item_names":[...],"situation":"...","performance":"...","locking":"...",
 "shots":[{"seconds":10,"shot_size":"...","camera":"...","state":"...","action":"...",
           "dialogues":[{"role":"...","tone":"...","line":"..."}],"sfx":"..."}],
 "opening":"...","visual":"...","end_frame_character":"...","end_frame_env":"...","end_frame_prop":"..."}`
}

func expandUserPrompt(item OutlineItem, total int, script, prevEnd, baseSituation string) string {
	var b strings.Builder

	// 上一段的尾帧放在**最前面**：模型对开头最敏感，藏在中间容易被忽略。
	// 实测就出过这种：段 8 尾帧写「@学生 保持坐姿」，段 9 却写成「站在椅子旁」，
	// 人物凭空站了起来 —— 后续每一段都跟着错。
	if strings.TrimSpace(prevEnd) != "" {
		b.WriteString("【上一段结束时的画面 —— 这一段必须从这里接着演】\n")
		b.WriteString(prevEnd + "\n")
		b.WriteString("人物在哪一侧、坐着还是站着、手里拿着什么、桌子在两之间的位置，全部沿用上面的状态。\n")
		b.WriteString("**只有在台词明确要求时才能改变姿态**（例如角色说「走吧」才起身），" +
			"而且要在对应镜头的 action 里写出触发点；台词没要求就不许改姿态、不许换座。\n\n")
	}
	if strings.TrimSpace(baseSituation) != "" {
		b.WriteString("【全局站位基准】整集不变，这一段也必须一致：\n")
		b.WriteString(baseSituation + "\n\n")
	}

	fmt.Fprintf(&b, "大纲第 %d 段 / 共 %d 段。\n", item.Index, total)
	b.WriteString("这一段要讲的是：" + fallbackText(item.Beat, "（见台词）") + "\n")
	if len(item.Lines) > 0 {
		b.WriteString("这一段必须说到的台词（逐字保留，一句不许改、不许漏、不许拆）：\n")
		for _, l := range item.Lines {
			b.WriteString("- " + l + "\n")
		}
	}
	b.WriteString("\n【剧本原文（供上下文参考）】\n")
	b.WriteString(script)
	return b.String()
}

// GenerateOne 阶段二：把大纲的一段展开成完整分镜
func (s *LLMStoryboardService) GenerateOne(
	ctx context.Context,
	item OutlineItem,
	total int,
	script, prevEnd, baseSituation string,
	chars []model.Character,
	scenes []model.Scene,
	items []model.Item,
) (FrameDraft, error) {
	empty := FrameDraft{}
	if s.client == nil {
		return empty, fmt.Errorf("没有可用的文本模型")
	}
	res, err := s.client.Chat(ctx, []aiclient.ChatMessage{
		{Role: "system", Content: expandSystemPrompt()},
		{Role: "user", Content: expandUserPrompt(item, total, script, prevEnd, baseSituation)},
	}, 0.5, 12000)
	if err != nil {
		return empty, fmt.Errorf("第 %d 段生成失败: %w", item.Index, err)
	}
	jsonStr := extractJSON(res.Content)
	if jsonStr == "" {
		return empty, fmt.Errorf("第 %d 段未返回有效 JSON", item.Index)
	}
	var raw llmFrameRaw
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return empty, fmt.Errorf("第 %d 段 JSON 解析失败: %w", item.Index, err)
	}
	return draftFromRaw(raw, item.Index, chars, scenes, items), nil
}

// GenerateStream 两阶段流式生成：先推大纲，再逐段推分镜
func (s *LLMStoryboardService) GenerateStream(
	ctx context.Context,
	script string,
	chars []model.Character,
	scenes []model.Scene,
	items []model.Item,
	duration int,
	emit func(StreamEvent),
) ([]FrameDraft, error) {
	// 没有可用模型时退回规则引擎（一次性出完）
	if s.client == nil {
		drafts := s.fallback.Generate(script, chars, scenes, items)
		emit(StreamEvent{Type: "outline", Count: len(drafts)})
		for i := range drafts {
			d := drafts[i]
			emit(StreamEvent{Type: "frame", Index: i + 1, Total: len(drafts), Frame: &d})
		}
		return drafts, nil
	}

	outline, err := s.PlanOutline(ctx, script, chars, scenes, items)
	if err != nil {
		return nil, err
	}
	emit(StreamEvent{Type: "outline", Count: len(outline), Outline: outline})

	drafts := make([]FrameDraft, 0, len(outline))
	prevEnd := ""

	for _, item := range outline {
		select {
		case <-ctx.Done():
			return drafts, ctx.Err()
		default:
		}

		// 只用「上一段的尾帧」做状态来源，**不额外传全局站位基准**。
		//
		// 以前把第一段的 situation 当全局基准传给所有段，结果第一段学生是站着的，
		// 后面每一段都被要求「学生站在椅子旁」，和上一段尾帧的「保持坐姿」直接打架，
		// 模型选了这个更强的措辞 —— 段 8 坐着、段 9 又站起来，就是这么来的。
		// 位置和姿态由 prevEnd 一段段链式传下去，才是唯一真相。
		d, err := s.GenerateOne(ctx, item, len(outline), script, prevEnd, "", chars, scenes, items)
		if err != nil {
			// 单段失败重试一次（模型偶发返回半截 JSON）
			log.Printf("分镜第 %d 段失败，重试一次：%v", item.Index, err)
			time.Sleep(1500 * time.Millisecond)
			d, err = s.GenerateOne(ctx, item, len(outline), script, prevEnd, "", chars, scenes, items)
		}
		if err != nil {
			log.Printf("分镜第 %d 段连续失败，跳过：%v", item.Index, err)
			emit(StreamEvent{Type: "error", Index: item.Index, Message: err.Error()})
			continue
		}

		d.FrameIndex = item.Index
		d.Text = renumberText(d.Text, item.Index)
		drafts = append(drafts, d)

		// 把这一段的尾帧和站位记下来，传给下一段
		if end := endFrameText(d); end != "" {
			prevEnd = end
		}

		frame := drafts[len(drafts)-1]
		emit(StreamEvent{Type: "frame", Index: item.Index, Total: len(outline), Frame: &frame})
	}

	if len(drafts) == 0 {
		return nil, fmt.Errorf("所有分段都生成失败")
	}
	// 超长段按规则再拆一次（和一次性生成保持一致的后处理）
	// 只按「单段超 15 秒」拆，不再把多镜段强制拆成一镜一段：
	// 一段里切 2-4 个镜头是做出来的画面层次，拆掉就变成了一个机位干到底。
	// 前提是每个镜头都写了机位方位（见提示词第 2 条）。
	return splitOverlongFrames(drafts, duration), nil
}

// endFrameText 取这一段的尾帧描述，作为下一段的上文
func endFrameText(d FrameDraft) string {
	parts := nonEmpty(d.EndFrameCharacter, d.EndFrameEnv, d.EndFrameProp)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "；")
}

// draftFromRaw 把模型返回的单段原始 JSON 转成 FrameDraft，并做完所有后处理
// （资产映射、台词清洗、文案、时长、描述词）。
//
// 与一次性生成里那段循环保持同一套处理，区别只是「只处理一段」。
func draftFromRaw(f llmFrameRaw, idx int, chars []model.Character, scenes []model.Scene, items []model.Item) FrameDraft {
	charByName := make(map[string]model.Character, len(chars))
	for _, c := range chars {
		charByName[c.Name] = c
	}
	sceneByName := make(map[string]model.Scene, len(scenes))
	for _, sc := range scenes {
		sceneByName[sc.Name] = sc
	}
	itemByName := make(map[string]model.Item, len(items))
	for _, it := range items {
		itemByName[it.Name] = it
	}

	d := FrameDraft{FrameIndex: idx}
	if sc, ok := sceneByName[f.SceneName]; ok {
		d.Scenes = []model.Scene{sc}
	} else if len(scenes) > 0 {
		d.Scenes = []model.Scene{scenes[0]}
	}
	for _, n := range f.RoleNames {
		if c, ok := charByName[n]; ok {
			d.Roles = append(d.Roles, c)
		}
	}
	for _, n := range f.ItemNames {
		if it, ok := itemByName[n]; ok {
			d.Items = append(d.Items, it)
		}
	}

	d.Situation = f.Situation
	d.ShotSize = f.ShotSize
	d.Camera = f.Camera
	d.Action = f.Action
	if d.Action == "" {
		d.Action = f.Visual
	}
	d.SFX = f.SFX
	d.Performance = f.Performance
	if d.Performance == "" {
		d.Performance = f.Emotion
	}
	d.Locking = f.Locking
	d.EndFrameCharacter = f.EndFrameCharacter
	d.EndFrameEnv = f.EndFrameEnv
	d.EndFrameProp = f.EndFrameProp

	for _, sh := range f.Shots {
		shot := FrameShot{
			Seconds: secOf(sh.Seconds), ShotSize: sh.ShotSize, Camera: sh.Camera,
			State: sh.State, Action: sh.Action, SFX: sh.SFX,
		}
		shot.Dialogues = validDialogues(sh.Dialogues, charByName)
		// 模型偶尔把台词直接写进 action，拆回来才能估算时长、输出【台词】行
		if len(shot.Dialogues) == 0 {
			if action, lines := splitInlineDialogues(shot.Action); len(lines) > 0 {
				shot.Action = action
				shot.Dialogues = lines
			}
		}
		if strings.TrimSpace(shot.Action) == "" && len(shot.Dialogues) == 0 {
			continue
		}
		d.Shots = append(d.Shots, shot)
	}

	d.Dialogues = validDialogues(f.Dialogues, charByName)
	if len(d.Dialogues) == 0 {
		if action, lines := splitInlineDialogues(d.Action); len(lines) > 0 {
			d.Action = action
			d.Dialogues = lines
		}
	}
	if len(d.Shots) == 0 && d.Action == "" && len(d.Dialogues) == 0 && d.SFX == "" {
		d.Action = f.Visual
	}
	if len(d.Shots) > 0 {
		merged := make([]FrameDialogue, 0, len(d.Shots))
		for _, sh := range d.Shots {
			merged = append(merged, sh.Dialogues...)
		}
		d.Dialogues = merged
	}

	// 模型可能照剧本自造资产名，降级成普通文字，否则参考图会对不上
	SanitizeMentions(&d, KnownAssetNames(d.Roles, d.Scenes, d.Items))

	// 台词已经填好，现在拼文案（[视频画面] 里动作和台词交织）
	d.Text = composeText(d, f.Opening, f.Visual, f.EndFrameCharacter, f.EndFrameEnv, f.EndFrameProp)

	d.Duration = EstimateFrameDuration(d)
	d.VideoPrompt = composeVideoPrompt(d, f.ShotProgression, d.Duration)
	return d
}
