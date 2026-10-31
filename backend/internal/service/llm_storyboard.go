package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/aiclient"
)

// LLMStoryboardService 用 LLM 生成分镜：两次调用架构
// 第一次生成 text（人读脚本），第二次把 text + 资产 metadata 拼成 video_prompt（机读）
type LLMStoryboardService struct {
	client   *aiclient.LLMClient
	fallback *StoryboardService
}

func NewLLMStoryboardService(client *aiclient.LLMClient) *LLMStoryboardService {
	return &LLMStoryboardService{
		client:   client,
		fallback: NewStoryboardService(),
	}
}

const storyboardSystemPrompt = `你是专业的影视导演。给你剧本，你帮我做分镜，每一段最长 15 秒。

严格要求：
1. 只输出 JSON，不要解释文字、不要 markdown 代码块
2. **一段只有一个镜头**：shots 数组里只放 1 个元素，这个镜头 2–6 秒。
   想换景别、换机位、换场景，都**另起一段** —— 一段里塞多个镜头时，
   模型对段内的切镜时机和台词分配都不稳，实测台词念不完、口型错位都出在多镜段上。
   **上一段的台词必须说完才能结束**：台词还没念完就结束，
   观众看到的就是口型对不上、话被掐断
3. 按剧本叙述顺序拆，不要合并或跳过情节。
   剧本里的一段戏，15 秒演不完就顺延到下一段，**不要压缩台词、不要删句子**：
   宁可多拆一段，也不要把 40 个字塞进 15 秒
4. 一个镜头内景别固定，不要在同一个镜头里换景别
5. 段数视剧本长度而定，通常比多镜写法更多（8-25 段）；一段对话里有几个人说话就至少拆几段
6. 所有指代人物、道具的地方必须写成 @角色名 / @道具名（例如 @女主 拿起 @面霜），
   不要写「她」「那个东西」这类代词，也不要写成「女生」「女士」这种泛称
7. **只能 @输入清单里真实存在的资产名**。剧本里提到但清单里没有的东西，
   直接当普通名词写（不加 @），不要自造资产名 —— 自造的 @名没有参考图，模型会画错
8. 剧本里有对白就必须原样放进对应镜头的 dialogues，line 逐字照抄，
   不要改写、缩写或省略；一句台词放到它被说出的那个镜头里。
   **一段里所有台词加起来不要超过 50 字**（中文口语约 13 秒，还要留动作时间），
   台词多就多拆一段，不要硬塞 —— 塞满 15 秒的段模型一定念不完
9. 不要把台词写进 action：action 只写动作、走位、视线，台词一律走 dialogues
10. 没有台词就 dialogues 留空数组；没有明显音效时 sfx 留空字符串
11. **每一段的场景和人物站位必须与上一段接得上，整集不能错**：
    谁坐桌子哪一侧、谁站哪一侧，从第一段到最后一段不能变；
    situation 只写姿态变化（站→坐、前倾、转头），不要写「换到另一侧」「走到窗边」这类位移，
    除非剧本明确要求他移动
12. 服装和发型一律不写（有单独的锁定块），situation 和 action 里只写位置、姿态、动作、视线
13. **每一段都必须写清人物朝向，这是整集最容易崩的一项**：
    实测只写「谁在画面左侧」时，生成出来的两个人各看各的、说话的人正对镜头念台词，
    像在跟观众说话。所以：
    - situation 里除了左右，必须写「谁面向谁」，两人同处一室就写
      「两人隔着桌子相对而坐、身体互相朝向、视线交汇」，不要只写左右就收笔
    - 每个镜头的 action 里，说话人的视线要落在具体的人身上，写「看向 @对方」，
      不要写「抬眼看」「望向前方」这种没有落点的句子
    - 单人镜头（中近景 / 近景 / 特写）必须写明「身体朝向 @对方 所在方向、侧脸或四分之三侧脸」，
      否则模型会把人物画成正对镜头

输出 JSON 结构：
{
  "frames": [
    {
      "scene_name": "场景名，必须与输入的场景列表完全一致",
      "role_names": ["出场角色名，必须是输入角色列表里的"],
      "item_names": ["出镜道具名，没有就空数组"],
      "situation": "素材与场景状态：写这一段「开始时」的时间、地点、人物姿态与站位（谁站在哪、谁坐着、手里拿着什么），2-3 句，用 @角色名 指代，说明谁在画面左侧、谁在右侧，**并写明谁面向谁**（如「两人隔着桌子相对而坐，@老师 面向 @学生、@学生 面向 @老师，视线交汇」）。只写起始状态，不要写这一帧里将要发生的动作；不要写服装和发型（有单独的锁定块），避免两处描述不一致",
      "performance": "各角色的情绪线，用 @名 指代，如「@女主 起始欣喜，被质疑时毫不迟疑」",
      "locking": "整帧不能变的要素：谁始终在画面哪侧、道具最终在谁手上、朝向关系",
      "shots": [
        {
          "seconds": 3,
          "shot_size": "景别，如 双人中景 / 中近景 / 特写 / 全景",
          "camera": "运镜，如 缓慢推镜 / 手持跟随 / 固定机位；固定机位就留空字符串",
          "state": "这一镜开始时**每个出场人物**在哪、什么姿态，一句话用 @名 指代，一个人都不能漏（漏掉的人会被画成帧首那个姿势）。必须承接上一镜的结果：上一镜说了『落座』，这一镜就要写『@学生 已坐在椅子上』。实测教训：台词说『坐』但 state 没写『已坐』，三格草图里学生就一直站着",
          "action": "这一镜的动作（25 字以内，用 @名 指代）。**说话时必须带视线落点**（如「@老师 身体朝向 @学生，目光落在 @学生 脸上」），单人镜头要写「侧脸/四分之三侧脸朝向 @对方」，不要写成正面朝镜头；若这个动作是被台词触发的，必须写明触发点，如「说到『坐』时抬手示意 @学生 落座」；动作要和 situation 里的起始姿态接得上（他本来就坐着，就别再写落座）；只写动作，不要写服装发型；不要堆连续动作 —— 动作太多模型会先演完再开口",
          "dialogues": [
            {"role": "角色名", "tone": "语气与语速，如「满带欣喜，语调轻快而笃定」", "line": "逐字台词"}
          ],
          "sfx": "这一镜的环境音或动作音效，如「拿起玻璃罐的轻微触碰声」；没有就空字符串"
        }
      ],
      "opening": "开场站位：谁在哪里做什么，一句话",
      "visual": "整帧的视频画面概括，一句话",
      "end_frame_character": "尾帧人物状态",
      "end_frame_env": "尾帧环境状态",
      "end_frame_prop": "尾帧关键道具",
      "emotion": "整体表演情绪，如「紧张克制」"
    }
  ]
}`

// llmDialogueRaw LLM 返回的一句台词
type llmDialogueRaw struct {
	Role string `json:"role"`
	Tone string `json:"tone"`
	Line string `json:"line"`
}

// llmShotRaw LLM 返回的单个镜头
type llmShotRaw struct {
	Seconds  int    `json:"seconds"`
	ShotSize string `json:"shot_size"`
	Camera   string `json:"camera"`
	// State 这一镜开始时的人物姿态（承接上一镜）
	State     string           `json:"state"`
	Action    string           `json:"action"`
	SFX       string           `json:"sfx"`
	Dialogues []llmDialogueRaw `json:"dialogues"`
}

// llmFrameRaw LLM 返回的一帧
//
// 同时兼容两种写法：
//   - 新写法：镜头细节放在 shots 数组里（对应参考站的一帧多镜头）
//   - 旧写法：shot_size/camera/action/dialogues 直接挂在帧上（单镜）
type llmFrameRaw struct {
	SceneName         string           `json:"scene_name"`
	RoleNames         []string         `json:"role_names"`
	ItemNames         []string         `json:"item_names"`
	Situation         string           `json:"situation"`
	Performance       string           `json:"performance"`
	Locking           string           `json:"locking"`
	Shots             []llmShotRaw     `json:"shots"`
	Opening           string           `json:"opening"`
	Visual            string           `json:"visual"`
	EndFrameCharacter string           `json:"end_frame_character"`
	EndFrameEnv       string           `json:"end_frame_env"`
	EndFrameProp      string           `json:"end_frame_prop"`
	ShotProgression   string           `json:"shot_progression"`
	Emotion           string           `json:"emotion"`
	// 旧写法兼容
	ShotSize  string           `json:"shot_size"`
	Camera    string           `json:"camera"`
	Action    string           `json:"action"`
	SFX       string           `json:"sfx"`
	Dialogues []llmDialogueRaw `json:"dialogues"`
}

// GenerateFrames 生成分镜
//
// duration 是工具栏选的出片时长（秒）：>0 时镜头时间轴按它分配并写进描述词，
// 0 表示未指定，由各镜头秒数合计兜底。
func (s *LLMStoryboardService) GenerateFrames(
	ctx context.Context,
	script string,
	chars []model.Character,
	scenes []model.Scene,
	items []model.Item,
	duration int,
) ([]FrameDraft, error) {
	if s.client == nil {
		return s.fallback.Generate(script, chars, scenes, items), nil
	}

	// 构造资产清单，让 LLM 只能用这些名字
	var names strings.Builder
	names.WriteString("【可用角色】\n")
	for _, c := range chars {
		fmt.Fprintf(&names, "- %s：%s；%s；%s\n", c.Name, c.IdentityAnchor, c.CostumePrompt, c.AppearanceCues)
	}
	names.WriteString("\n【可用场景】\n")
	for _, sc := range scenes {
		fmt.Fprintf(&names, "- %s：%s；%s\n", sc.Name, sc.SpatialAnchor, sc.LightingPrompt)
	}
	names.WriteString("\n【可用道具】\n")
	for _, it := range items {
		fmt.Fprintf(&names, "- %s：%s\n", it.Name, it.Prompt)
	}

	messages := []aiclient.ChatMessage{
		// 追加短剧生产规范（short-drama-h3-prompt skill 提炼版）：
		// 语速/气口计算、第 4 秒进对话、位置锁定、说话镜头锁人这些
		// 都是踩过坑的硬约束，光靠上面那段 JSON 说明压不住
		{Role: "system", Content: storyboardSystemPrompt + StoryboardSkillRules()},
		{Role: "user", Content: names.String() + "\n【剧本】\n" + script},
	}

	// max_tokens 要给到 32000：现在用的 deepseek-flash 是**带推理**的模型，
	// 12000 的预算会被思维链吃光 —— 实测 output_tokens=12001 但正文只有 143 字，
	// JSON 没写完就断，整集降级成规则引擎（用户看到的就是「抠句子」的分镜）。
	// 32000 留足「推理 + 完整 JSON」的空间，实测接口接受这个上限。
	// 服务商 503/429/超时是常态（实测 DeepSeek 会返回 Service is too busy），
	// 一次失败就整集降级成规则引擎太亏 —— 这类错误重试两次基本能过。
	// 不重试的错误（比如 400 参数错、401 key 错）立刻返回，重试也没用。
	var res *aiclient.ChatResult
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		res, err = s.client.Chat(ctx, messages, 0.5, 32000)
		if err == nil {
			break
		}
		if !retryableLLMErr(err) || attempt == 3 {
			break
		}
		log.Printf("分镜 LLM 第 %d 次失败（%v），2 秒后重试", attempt, err)
		select {
		case <-ctx.Done():
			return s.fallback.Generate(script, chars, scenes, items), fmt.Errorf("LLM 分镜超时，已降级: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		return s.fallback.Generate(script, chars, scenes, items), fmt.Errorf("LLM 分镜失败，已降级: %w", err)
	}
	// 输出 token 打点到日志：JSON 被截断时 unmarshal 只会报 unexpected end，
	// 看不出是「模型没写完」还是「max_tokens 顶到上限」
	log.Printf("分镜 LLM 返回: %d 字, output_tokens=%d", len([]rune(res.Content)), res.OutputTokens)

	jsonStr := extractJSON(res.Content)
	if jsonStr == "" {
		return s.fallback.Generate(script, chars, scenes, items), fmt.Errorf("LLM 未返回有效 JSON")
	}

	var parsed struct {
		Frames []llmFrameRaw `json:"frames"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return s.fallback.Generate(script, chars, scenes, items), fmt.Errorf("JSON 解析失败: %w", err)
	}
	if len(parsed.Frames) == 0 {
		return s.fallback.Generate(script, chars, scenes, items), fmt.Errorf("LLM 未生成任何镜头")
	}

	// 把名字映射回实体
	charByName := map[string]model.Character{}
	for _, c := range chars {
		charByName[c.Name] = c
	}
	sceneByName := map[string]model.Scene{}
	for _, sc := range scenes {
		sceneByName[sc.Name] = sc
	}
	itemByName := map[string]model.Item{}
	for _, it := range items {
		itemByName[it.Name] = it
	}

	drafts := make([]FrameDraft, 0, len(parsed.Frames))
	for i, f := range parsed.Frames {
		d := FrameDraft{FrameIndex: i + 1}

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

		d.Text = composeText(d, f.Opening, f.Visual, f.EndFrameCharacter, f.EndFrameEnv, f.EndFrameProp)

		// 参考站结构的字段：有就用 LLM 的，没有就交给 BuildFrameVideoPrompt 兜底
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

		// 多镜头：LLM 给 shots 就用它，否则退回首层平铺字段（单镜）
		for _, sh := range f.Shots {
			shot := FrameShot{
				Seconds:  sh.Seconds,
				ShotSize: sh.ShotSize,
				Camera:   sh.Camera,
				State:    sh.State,
				Action:   sh.Action,
				SFX:      sh.SFX,
			}
			shot.Dialogues = validDialogues(sh.Dialogues, charByName)
			// LLM 有时不按 schema 输出，把台词直接写进 action（「**老师：** 那是一部分…」），
			// dialogues 留空。这样既会让时长估算漏掉台词（估出 3 秒说不完 50 个字），
			// 也会让描述词缺少【台词】标记（视频模型不一定照着念）。这里拆回来。
			if len(shot.Dialogues) == 0 {
				if action, lines := splitInlineDialogues(shot.Action); len(lines) > 0 {
					shot.Action = action
					shot.Dialogues = lines
				}
			}
			if strings.TrimSpace(shot.Action) == "" && len(shot.Dialogues) == 0 {
				continue // 空镜头直接丢弃，避免输出只有标题的｜镜头N｜
			}
			d.Shots = append(d.Shots, shot)
		}

		d.Dialogues = validDialogues(f.Dialogues, charByName)
		// 平铺写法（旧 schema）同样兜底一次
		if len(d.Dialogues) == 0 {
			if action, lines := splitInlineDialogues(d.Action); len(lines) > 0 {
				d.Action = action
				d.Dialogues = lines
			}
		}
		if len(d.Shots) == 0 {
			// 无 shots 时把平铺台词挪进单镜，保证时间轴和台词在同一层
			if d.Action == "" && len(d.Dialogues) == 0 && d.SFX == "" {
				d.Action = f.Visual
			}
		}

		// 多镜头时把各镜台词汇总到帧级，供前端 chips 展示
		if len(d.Shots) > 0 {
			merged := make([]FrameDialogue, 0, len(d.Shots))
			for _, sh := range d.Shots {
				merged = append(merged, sh.Dialogues...)
			}
			d.Dialogues = merged
		}

		// 兜底：LLM 可能照剧本自造资产名（比如项目里没有「面霜」却写成 @面霜），
		// 这类 @名 没有对应参考图，必须降级为普通文字，否则前端高亮和参考图会错位。
		SanitizeMentions(&d, KnownAssetNames(d.Roles, d.Scenes, d.Items))

		// 时长：工具栏传的是「单段上限」而不是固定值 ——
		// 一句「坐。」也按 15 秒出片会拖得没法看。
		// 先按台词字数与镜头数估算，超上限才截到上限。
		// 时长按内容需求定，**不**被工具栏上限压缩 ——
		// 压到上限会让台词说不完（4.55 秒的需求只给 4 秒，生成出来就是
		// 话没说完就切镜、口型跟着错）。工具栏的时长只用来决定「要不要拆段」，
		// 见 splitOverlongFrames。
		d.Duration = EstimateFrameDuration(d)
		d.VideoPrompt = composeVideoPrompt(d, f.ShotProgression, d.Duration)

		drafts = append(drafts, d)
	}

	// 先按单帧上限拆超长的，再把「一段多镜」拆成「一段一镜」。
	// 顺序不能反：先拆成一镜一段的话，单镜超过 15 秒就没法再拆了。
	return explodeMultiShotFrames(splitOverlongFrames(drafts, duration)), nil
}

// explodeMultiShotFrames 把「一段多镜」拆成「一段一镜」。
//
// 为什么要拆：一段里塞 2-3 个镜头时，H3 对段内的切镜时机和台词分配都不稳 ——
// 实测「台词没说完就切」「口型对不上」几乎都出在多镜段上。
// 拆成一镜一段后，每段的时间轴只服务一句台词，模型只需要管
// 「这一镜把这句话说完」，不用同时算切镜点。
//
// 代价是段数变多、视频条数变多，所以只在分镜生成阶段做，
// 库里已有的多镜段照旧（BuildFrameVideoPrompt 仍支持多镜）。
func explodeMultiShotFrames(drafts []FrameDraft) []FrameDraft {
	out := make([]FrameDraft, 0, len(drafts))
	for _, d := range drafts {
		shots := normalizeShots(d)
		if len(shots) <= 1 {
			out = append(out, d)
			continue
		}
		for _, sh := range shots {
			nd := d
			nd.Shots = []FrameShot{sh}
			nd.Dialogues = append([]FrameDialogue(nil), sh.Dialogues...)
			// 平铺字段同步成这一镜的：兜底路径和前端都读它们
			nd.ShotSize = sh.ShotSize
			nd.Camera = sh.Camera
			nd.Action = sh.Action
			nd.SFX = sh.SFX
			out = append(out, nd)
		}
	}
	// 先重排段号，再拼描述词 —— 「场次：1-3」读的是 FrameIndex，
	// 顺序反了每段都会写回拆之前那个段号，整集全是「场次：1-1」
	for i := range out {
		out[i].FrameIndex = i + 1
		out[i].Text = renumberText(out[i].Text, i+1)
		out[i].Duration = EstimateFrameDuration(out[i])
		out[i].VideoPrompt = BuildFrameVideoPrompt(out[i], out[i].Duration)
	}
	return out
}

// renumberText 把「人读脚本」第一行的段号换成新的。
// 拆段后原来一段变三段，不换的话三条文案都顶着同一个段号。
func renumberText(text string, no int) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	head, rest, _ := strings.Cut(text, "\n")
	if _, err := strconv.Atoi(strings.TrimSpace(head)); err != nil {
		return text // 第一行不是段号，不动
	}
	return fmt.Sprintf("%d\n%s", no, rest)
}

func shotDialogueChars(sh FrameShot) int {
	n := 0
	for _, dl := range sh.Dialogues {
		n += len([]rune(strings.TrimSpace(dl.Line)))
	}
	return n
}

func frameDialogueChars(shots []FrameShot) int {
	n := 0
	for _, sh := range shots {
		n += shotDialogueChars(sh)
	}
	return n
}

func mergeShotDialogues(shots []FrameShot) []FrameDialogue {
	out := make([]FrameDialogue, 0, len(shots))
	for _, sh := range shots {
		out = append(out, sh.Dialogues...)
	}
	return out
}

// splitOverlongFrames 把台词装不下单帧上限的帧，按镜头切成多帧。
// 只处理多镜头的帧；单镜头台词超限没法切，保持原样交给上游。
//
// limit 是单帧上限（工具栏选的时长，没选就 15 秒 —— H3 单段上限）。
func splitOverlongFrames(drafts []FrameDraft, limit int) []FrameDraft {
	if limit <= 0 || limit > 15 {
		limit = 15
	}
	out := make([]FrameDraft, 0, len(drafts))
	for _, d := range drafts {
		shots := normalizeShots(d)
		// 需求超过单帧上限才拆。按「实际需要多少秒」判断比按字数准：
		// 台词不多但镜头多的帧同样装不下。
		if len(shots) < 2 || frameDurationNeed(d) <= float64(limit) {
			out = append(out, d)
			continue
		}

		var (
			group []FrameShot
			part  int
		)
		flush := func() {
			if len(group) == 0 {
				return
			}
			part++
			nd := d
			nd.Shots = append([]FrameShot(nil), group...)
			nd.Dialogues = mergeShotDialogues(group)
			if part > 1 {
				nd.Text = strings.TrimSpace(d.Text) + "（续）"
			}
			nd.Duration = EstimateFrameDuration(nd)
			nd.VideoPrompt = BuildFrameVideoPrompt(nd, nd.Duration)
			out = append(out, nd)
			group = nil
		}

		for _, sh := range shots {
			// 加进这一组会不会超过单帧上限；超了就先收尾，从下一镜开始新的一帧
			next := append(append([]FrameShot{}, group...), sh)
			if len(group) > 0 && shotsDurationNeed(next) > float64(limit) {
				flush()
			}
			group = append(group, sh)
		}
		flush()
	}

	for i := range out {
		out[i].FrameIndex = i + 1
	}
	return out
}

// validDialogues 过滤台词：只保留能对上资产的角色，避免出现「她」这种无主语段落
func validDialogues(
	raw []llmDialogueRaw,
	charByName map[string]model.Character,
) []FrameDialogue {
	out := make([]FrameDialogue, 0, len(raw))
	for _, dl := range raw {
		role := strings.TrimSpace(dl.Role)
		if role == "" || strings.TrimSpace(dl.Line) == "" {
			continue
		}
		if _, ok := charByName[role]; !ok {
			continue
		}
		out = append(out, FrameDialogue{Role: role, Tone: dl.Tone, Line: dl.Line})
	}
	return out
}

// composeText 拼装人读脚本
func composeText(d FrameDraft, opening, visual, endChar, endEnv, endProp string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d\n", d.FrameIndex)

	if len(d.Scenes) > 0 {
		fmt.Fprintf(&b, "【场景：%s】\n", d.Scenes[0].Name)
	} else {
		b.WriteString("【场景：未标注】\n")
	}

	fmt.Fprintf(&b, "[开场站位]：%s\n", fallbackText(opening, "环境空镜起幅"))
	fmt.Fprintf(&b, "[视频画面]：%s\n", fallbackText(visual, "延续前一镜动作"))

	fmt.Fprintf(&b, "[尾帧画面]：[人物状态] %s；[环境状态] %s；[关键道具] %s",
		fallbackText(endChar, "情绪收束"),
		fallbackText(endEnv, sceneEnv(d)),
		fallbackText(endProp, itemName(d)),
	)
	return b.String()
}

// composeVideoPrompt 拼装机读 prompt
//
// 结构统一交给 BuildFrameVideoPrompt，和规则引擎路径保持同一份模板，
// 避免两条路径产出两种格式、上游表现不一致。
// duration > 0 时时间轴直接按工具栏选择生成，避免「提示词 0-10 秒、实际出 5 秒」的脱节。
func composeVideoPrompt(d FrameDraft, progression string, duration int) string {
	if strings.TrimSpace(d.ShotSize) == "" && strings.TrimSpace(progression) != "" {
		// 「中景起幅 → 近景推进 → 特写落幅」取第一段当景别
		d.ShotSize = strings.TrimSpace(strings.Split(progression, "→")[0])
	}
	return BuildFrameVideoPrompt(d, duration)
}

func sceneEnv(d FrameDraft) string {
	if len(d.Scenes) > 0 && d.Scenes[0].LightingPrompt != "" {
		return d.Scenes[0].LightingPrompt
	}
	return "延续前一镜"
}

func itemName(d FrameDraft) string {
	if len(d.Items) > 0 {
		return d.Items[0].Name
	}
	return "无"
}

// inlineDialogueRe 匹配动作描述里的内嵌台词标记：「**老师：**」「**@学生:**」
var inlineDialogueRe = regexp.MustCompile(`\*\*\s*@?([^*：:\n]{1,20})\s*[：:]\s*\*\*\s*`)

// splitInlineDialogues 把「**老师：** 台词内容」从动作描述里拆出来。
//
// 正常情况下 LLM 会把台词放进 shots[].dialogues，但实测它有时直接写进 action，
// dialogues 留空。不拆的话有两个后果：
//   - 时长估算只数 dialogues，会按「1 镜 × 2.5 秒」估出 3 秒，几十个字根本说不完
//   - 描述词里没有【角色台词】行，视频模型只当成动作描述，不一定会念
//
// 没有这种标记时原样返回，不影响正常数据。
func splitInlineDialogues(action string) (string, []FrameDialogue) {
	if !strings.Contains(action, "**") {
		return action, nil
	}
	idx := inlineDialogueRe.FindAllStringSubmatchIndex(action, -1)
	if len(idx) == 0 {
		return action, nil
	}

	var (
		lines []FrameDialogue
		b     strings.Builder
		prev  int
	)
	for i, m := range idx {
		start := m[1]
		end := len(action)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		// 台词正文压成一行：描述词是按行渲染的，内部换行会打乱时间轴结构
		line := strings.Join(strings.Fields(action[start:end]), " ")
		role := strings.TrimSpace(action[m[2]:m[3]])
		if line != "" {
			lines = append(lines, FrameDialogue{Role: role, Tone: "自然", Line: line})
		}
		b.WriteString(action[prev:m[0]])
		prev = end
	}
	b.WriteString(action[prev:])
	return strings.TrimSpace(b.String()), lines
}

// retryableLLMErr 判断是不是「过一会儿再试就能成功」的错误。
// 503/429/超时/网络抖动都算；400 参数错、401 key 错重试没意义。
func retryableLLMErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, k := range []string{"503", "429", "too busy", "timeout", "timed out", "超时", "connection reset", "eof"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}
