package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

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
	Type    string            `json:"type"` // outline / frame / done / error
	Count   int               `json:"count,omitempty"`
	Index   int               `json:"index,omitempty"`
	Total   int               `json:"total,omitempty"`
	Frame   *FrameDraft       `json:"frame,omitempty"`
	Outline []OutlineItem     `json:"outline,omitempty"`
	Seats   map[string]string `json:"seats,omitempty"`
	Message string            `json:"message,omitempty"`
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

// sceneRefHint 按场景参考类型告诉模型「空间细节该写多细」。
//
// 用 360 视频/全景图时，房间已经被素材锁死，再逐条罗列家具反而会和素材里的
// 实际布局打架（文字说书架在左、视频里在右，模型只能挑一个）；
// 用场景原图时只有正前方视角，才需要把家具相对位置写全，帮模型补全画面。
func sceneRefHint(sceneRef string) string {
	switch sceneRef {
	case "video":
		return "【场景参考】出片时会把「360 全景视频」作为空间参考传给模型，房间的墙面、家具、陈设已由视频锁定。" +
			"situation 只写「人物在画面哪一侧、坐着还是站着、朝向谁」就够了，" +
			"不要逐条罗列家具和房间结构 —— 重复描述会和视频里的实际布局打架。"
	case "panorama":
		return "【场景参考】出片时用「柱形全景图」当空间参考，房间四周都看得到。" +
			"situation 写清人物位置和朝向即可，房间细节不用逐条罗列。"
	default:
		return "【场景参考】出片时用「场景原图」当参考，只有正前方视角。" +
			"situation 要把人物站位、朝向以及关键家具的相对位置写清楚，帮模型补全画面。"
	}
}

func expandUserPrompt(item OutlineItem, total int, script, prevEnd, baseSituation, sceneRef string, scenes []model.Scene) string {
	var b strings.Builder

	b.WriteString(sceneRefHint(sceneRef) + "\n\n")

	// 把场景的空间布局喂进去：只要求「写绝对位置」但模型不知道书架在哪边，
	// 它就只能写「画面左侧」—— 而画面左右随机位翻转，过肩一反打就全反了。
	if len(scenes) > 0 {
		b.WriteString("【场景布局参考】写站位时用这里的绝对位置（哪一侧有什么），不要写「画面左/右」：\n")
		for _, sc := range scenes {
			anchor := strings.TrimSpace(sc.SpatialAnchor)
			if anchor == "" {
				anchor = sc.Name
			}
			fmt.Fprintf(&b, "- %s：%s\n", sc.Name, anchor)
			b.WriteString("注意：过肩镜头里那个「背对镜头、只露出肩部」的人，仍然坐在原来的座位上，" +
				"前景/背景只是构图，不代表他换了位置。写站位时按座位写，不要按画面里的前后左右写。\n")
		}
		b.WriteString("\n")
	}

	// 上一段的尾帧放在**最前面**：模型对开头最敏感，藏在中间容易被忽略。
	// 实测就出过这种：段 8 尾帧写「@学生 保持坐姿」，段 9 却写成「站在椅子旁」，
	// 人物凭空站了起来 —— 后续每一段都跟着错。
	if strings.TrimSpace(prevEnd) != "" {
		b.WriteString("【上一段结束时的画面 —— 这一段必须从这里接着演】\n")
		b.WriteString(prevEnd + "\n")
		b.WriteString("人物在哪一侧、坐着还是站着、手里拿着什么、桌子在两之间的位置，全部沿用上面的状态。\n")
		b.WriteString("**坐谈戏的默认状态就是「不动」**：两个人坐着说话，整段结束还是原样坐着，" +
			"不会因为切了个镜头、换了个景别就换座位 —— 这是常识，观众也不接受。\n")
		b.WriteString("所以：**本段 situation 的站位，除了台词明确要求的情况，必须逐字沿用上一段的写法**" +
			"（包括「靠窗那一侧、离门口最近的那把椅子」这种定位方式）。\n")
		b.WriteString("**只有台词明确要求时才能改姿态或换座**（例如角色说「走吧」才起身、" +
			"「你坐这边」才换位），而且必须在该镜头的 action 里写出触发动作；" +
			"台词没要求就不许改姿态、不许换座、不许改朝向。\n\n")
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
	script, prevEnd, baseSituation, sceneRef, retryHint string,
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
		{Role: "user", Content: expandUserPrompt(item, total, script, prevEnd, baseSituation, sceneRef, scenes) +
			retryHint},
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
	sceneRef string,
	preset []OutlineItem,
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

	// 先定「座位表」：每个角色坐哪把椅子，一次定死，后面每段都用它。
	//
	// 为什么不让模型每段自己写：实测它每段都换个说法（一会儿「靠窗那侧」、
	// 一会儿「离门最近那把」），同一个座位在不同段落里叫法不同，模型就当成
	// 不同位置，人物在整集里来回漂。座位表定下来后，站位由代码拼，
	// 模型只负责给动作。
	seatMap := s.PlanSeats(ctx, chars, scenes)
	if len(seatMap) > 0 {
		emit(StreamEvent{Type: "seats", Seats: seatMap})
	}

	// 用户在确认弹窗里看过（也可能改过）的大纲直接用，不重新规划 ——
	// 重新跑一遍会得到不同拆法，用户刚确认的东西就白确认了。
	outline := preset
	if len(outline) == 0 {
		var err error
		outline, err = s.PlanOutline(ctx, script, chars, scenes, items)
		if err != nil {
			return nil, err
		}
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
		d, err := s.GenerateOne(ctx, item, len(outline), script, prevEnd, "", sceneRef, "", chars, scenes, items)
		d.SeatMap = seatMap

		// 站位必须写明「坐哪一侧」—— 每段都是独立生成的视频，模型看不到上一段，
		// 只写「坐在对面」它就只能自己摆，整集座位来回换。缺了就重试一次。
		if err == nil && !hasSeatWords(d.Situation) {
			log.Printf("分镜第 %d 段站位缺方位，重试：%s", item.Index, truncateStr(d.Situation, 60))
			d, err = s.GenerateOne(ctx, item, len(outline), script, prevEnd, "", sceneRef,
				"\n\n【上一版的问题】situation 里没有写清「人物坐在哪一侧」，只有「坐在对面」这种描述。"+
					"这一段是独立生成的视频，模型看不到别的段落，必须在这一段里把位置写死："+
					"主角坐在「靠 XX 一侧的 XX 椅子」上，对方坐在「靠 XX 一侧的 XX 椅子」上；"+
					"位置和椅子外观都要写，让人一眼能认出是哪一把。",
				chars, scenes, items)
			d.SeatMap = seatMap
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

// hasSeatWords 判断站位是否「唯一可指认」：位置 + 家具特征都要有。
//
// 只写「坐在那一侧的椅子上」不够 —— 同一侧可能不止一把椅子，
// 模型不知道坐哪把，会自己摆一把出来。所以要求同时出现：
//
//	位置词（靠 X 一侧 / 桌后 …）
//	家具特征（椅子 / 转椅 / 靠背椅 / 凳子 / 沙发 …）
func hasSeatWords(s string) bool {
	hasPos := false
	for _, k := range []string{"靠", "一侧", "一边", "左侧", "右侧", "东侧", "西侧", "内侧", "外侧", "桌后", "桌前"} {
		if strings.Contains(s, k) {
			hasPos = true
			break
		}
	}
	hasFurniture := false
	for _, k := range []string{"椅", "凳", "沙发", "座位", "座上"} {
		if strings.Contains(s, k) {
			hasFurniture = true
			break
		}
	}
	return hasPos && hasFurniture
}

// PlanSeats 给每个角色分配一把唯一的椅子，返回「角色名 → 座位描述」。
//
// 座位描述要具体到「能一眼认出来是哪一把」：位置 + 参照物 + 椅子外观。
// 这一步只跑一次，之后每段的站位都由它拼出来，不再让模型自由发挥 ——
// 否则同一个座位在不同段落里会被叫成不同名字，模型以为换了位置。
func (s *LLMStoryboardService) PlanSeats(ctx context.Context, chars []model.Character, scenes []model.Scene) map[string]string {
	if len(chars) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("看下面这张场景图，先如实说出图里实际存在的椅子：有几把、什么样式（转椅/靠背椅/长椅）、什么颜色材质、分别在房间的什么位置。\n")
	sb.WriteString("然后给每个角色分配一把，每个角色对应一把，不要重复。\n\n")
	sb.WriteString("角色：\n")
	for _, c := range chars {
		fmt.Fprintf(&sb, "- %s（%s）\n", c.Name, briefIdentity(c.IdentityAnchor))
	}
	if len(scenes) > 0 {
		sb.WriteString("\n场景已有的文字描述（只作参考，以图为准）：\n")
		for _, sc := range scenes {
			fmt.Fprintf(&sb, "- %s：%s\n", sc.Name, strings.TrimSpace(sc.SpatialAnchor))
		}
	}
	sb.WriteString("\n座位写法用**画面左右**：「画面左侧的椅子（背靠窗、正对镜头）」「画面右侧的椅子（靠门、侧对镜头）」。\n")
	sb.WriteString("为什么可以用左右：首帧图是正面平视、桌子横置，画面左右就是场景的左右，整集不变。\n")
	sb.WriteString("椅子外观必须是你在这张图里**真的看到的**，不要凭「办公室一般有什么」猜。\n")
	sb.WriteString("只输出 JSON，不要解释：{\"老师\":\"画面左侧的椅子（背靠窗、正对镜头）\",\"学生\":\"画面右侧的椅子（靠门、侧对镜头）\"}\n")

	// 带上场景图：模型看着图说话，才不会编出「木质靠背椅」这种场景里根本没有的东西
	var content any = sb.String()
	for _, sc := range scenes {
		src := strings.TrimSpace(sc.ReferenceImage)
		if src == "" {
			continue
		}
		if dataURL, e := InlineImage("", src, 1024); e == nil {
			content = []aiclient.ContentPart{
				aiclient.TextPart(sb.String()),
				aiclient.ImagePart(dataURL),
			}
			break
		}
	}

	res, err := s.client.Chat(ctx, []aiclient.ChatMessage{
		{Role: "system", Content: "你是短剧分镜的场记，擅长看图说话。只输出 JSON。"},
		{Role: "user", Content: content},
	}, 0.2, 1200)
	if err != nil {
		log.Printf("座位表生成失败（继续用模型自由写法）：%v", err)
		return nil
	}

	raw := extractJSON(res.Content)
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	if e := json.Unmarshal([]byte(raw), &out); e != nil {
		log.Printf("座位表解析失败：%v", e)
		return nil
	}
	clean := map[string]string{}
	for k, v := range out {
		if strings.TrimSpace(v) != "" {
			clean[k] = strings.TrimSpace(v)
		}
	}
	log.Printf("座位表：%v", clean)
	return clean
}
