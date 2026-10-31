package service

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"jikeai-clone/internal/model"
)

// FrameDialogue 一句台词：谁说、什么语气、说什么
type FrameDialogue struct {
	Role string `json:"role"` // 角色名
	Tone string `json:"tone"` // 语气提示，如「满带欣喜，语调轻快而笃定」
	Line string `json:"line"` // 台词正文，逐字
}

// FrameShot 一帧内的单个镜头：参考站的一条视频描述词
// 往往包含多个 ｜镜头N｜，时间累计（0–5秒、5–8秒、8–13秒）。
type FrameShot struct {
	Seconds  int    `json:"seconds"` // 时长权重，2-5 秒
	ShotSize string `json:"shot_size"`
	Camera   string `json:"camera"`
	// State 这一镜开始时每个人在哪、什么姿态（承接上一镜的结果）。
	// 必须逐镜给：只写帧首姿态的话，草图和视频都会把整段画成同一个姿势 ——
	// 实测出现过「台词说『坐』，但三格草图里学生一直站着」。
	State     string          `json:"state"`
	Action    string          `json:"action"`
	Dialogues []FrameDialogue `json:"dialogues"`
	SFX       string          `json:"sfx"`
}

// characterLockLines 拼「@名字：身份 | 外貌 | 服装」的锁定描述。
//
// 这三段是角色档案里的固定信息，由程序原样注入每一帧 ——
// 交给 LLM 每帧重写的话措辞会漂移（这帧「深蓝卫衣」、下帧「蓝色外套」），
// 模型就当成两套衣服，人物一致性直接崩。
func characterLockLines(roles []model.Character) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		parts := nonEmpty(r.IdentityAnchor, r.AppearanceCues, r.CostumePrompt)
		if len(parts) == 0 {
			// 老数据只填了出图提示词，退回用它
			parts = nonEmpty(r.ImagePrompt)
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, "@"+r.Name+"："+strings.Join(parts, " | "))
	}
	return out
}

// shotSpeakSeconds 这一镜把台词念完需要多久（秒）。
// 4.5 字/秒是中文口语常态，0.6 秒留给起句和收尾。
func shotSpeakSeconds(sh FrameShot) float64 {
	chars := 0
	pauses := 0
	for _, dl := range sh.Dialogues {
		line := strings.TrimSpace(dl.Line)
		chars += len(speakCharRe.FindAllString(line, -1))
		pauses += len(pauseMarkRe.FindAllString(line, -1))
	}
	if chars == 0 {
		return 0
	}
	// 3.8 字/秒（不是教科书上的 4.5）：实测 H3 生成的中文口语就在 3.5–4 字/秒，
	// 按 4.5 算会把时长估短。踩过的坑：35 字估成 9 秒，实际音频念到 8.5 秒才说到
	// 第三句的「先说说」，后面整句被 9 秒的片子截掉；同时画面嘴型也和声音错开
	// （声音 0 秒就开口，嘴到 3 秒才动）。宁可估长一点，多出来的时间正好当气口。
	// 每个「——」是明确的气口，按 0.8 秒额外加时。
	return float64(chars)/3.8 + 0.6 + float64(pauses)*0.8
}

// pauseMarkRe 匹配台词里的停顿标记：中文破折号「——」或连写的英文横线
var pauseMarkRe = regexp.MustCompile(`——|—{2,}|-{2,}`)

// speakCharRe 只数真正要念出来的字：汉字、字母、数字。
// 标点不占发音时间，按整串字符算会把时长算长，台词反而被压着念。
var speakCharRe = regexp.MustCompile(`[\p{Han}A-Za-z0-9]`)

// frameDurationNeed 这一帧不夹上限时到底需要多少秒。
// 拆帧判断用它：需求超过 15 秒（上游上限）就该拆，而不是只看台词字数。
func frameDurationNeed(d FrameDraft) float64 {
	return shotsDurationNeed(normalizeShots(d))
}

// shotsDurationNeed 一组镜头需要多少秒（不夹 15 秒上限）
//
// 必须和 BuildFrameVideoPrompt 的分配口径完全一致：逐镜取 max(台词念完所需, 底线)，
// 再相加。之前是「镜头数 × 2.5 与台词总需求取大」，两套口径对不上 ——
// 3 个镜头实际要 11.5 秒，这里只算出 9.8 秒，按 10 秒分配时
// 4.55 秒的台词镜头只分到 4 秒，生成出来就是话没说完就切镜。
func shotsDurationNeed(shots []FrameShot) float64 {
	total := 0.0
	spoken := false
	for _, sh := range shots {
		n := shotSpeakSeconds(sh)
		if n > 0 {
			spoken = true
			// 有台词：念完所需秒数，最低 2.5 秒（1 秒的镜头模型来不及进入说话状态）
			if n < 2.5 {
				n = 2.5
			}
		} else {
			// 纯动作/空镜：底线 2 秒
			n = 2
		}
		total += n
	}
	if spoken {
		total += 0.4 // 全帧头尾各留一点呼吸
	}
	return total
}

// EstimateFrameDuration 按这一帧的内容估算该多长（秒）
//
// 依据两条：
//   - 台词按中文口语 4.5 字/秒折算，前后各留一点呼吸，+1 秒
//   - 每个镜头至少要起幅和落幅，按 2.5 秒/镜兜底
//
// 两者取大，向上取整，夹在 [3, 15]（上游时长节点支持的范围）。
func EstimateFrameDuration(d FrameDraft) int {
	shots := normalizeShots(d)
	n := len(shots)
	if n == 0 {
		n = 1
	}

	est := frameDurationNeed(d)

	sec := int(math.Ceil(est))
	if sec < 3 {
		sec = 3
	}
	if sec > 15 {
		sec = 15
	}
	return sec
}

// BuildFrameVideoPrompt 按参考站的格式拼「视频描述词」
//
// 结构（顺序即重要程度）：
//
//	[出镜角色-物品-场景]   →  @资产 清单，与传入的参考图一一对应
//	角色-声线绑定           →  谁用什么嗓音
//	【素材与场景状态】      →  时间、地点、人物站位
//	｜镜头1｜ 0–5秒｜景别｜  →  动作 + 台词 + 音效（可多镜，时间累计）
//	【表演】                →  情绪线
//	【关键锁定】            →  站位/道具归属不能漂移
//	【视觉风格】            →  整体质感（画幅由工具栏决定，不写进提示词）
//	全局执行规则            →  一行分号连接
//
// 造型细节不在这里写：参考站的做法是把资产图当参考图一起传，
// 模型看图比读文字准，堆一大段外貌描述反而会互相打架。
// frameSceneHeader 拼「场次 / 地点 / 时间 / 人物」四行。
//
// 这一版按参考站的写法：场次 + 地点 + 时间 + 人物 打头，后面每个镜头一段
// 「镜号 / 景别 / 运镜 / 情绪动作 / 台词 / 音效 / 时长」。
// 之前那种【人物锁定】【视觉风格】【全局执行规则】的长段落全砍掉了 ——
// 外观由参考图锁、空间由全景参考图锁，文字只负责「这一镜拍什么」。
func frameSceneHeader(d FrameDraft) string {
	var b strings.Builder

	epNo := d.EpisodeNo
	if epNo <= 0 {
		epNo = 1
	}
	fmt.Fprintf(&b, "场次：%d-%d\n", epNo, d.FrameIndex)

	// 地点：场景名 + 帧首站位（站位并进这一行，省掉原来的【素材与场景状态】块，
	// 首帧图也靠这行取人物位置）
	place := "未指定场景"
	if len(d.Scenes) > 0 {
		names := make([]string, 0, len(d.Scenes))
		for _, sc := range d.Scenes {
			names = append(names, "@"+sc.Name)
		}
		place = strings.Join(names, "、")
	}
	sit := strings.TrimSpace(d.Situation)
	if sit != "" {
		place += " " + sit
	}
	// 多人同场时补一句朝向锁定。situation 由 LLM 写，实测它经常只写「谁在画面左侧」
	// 就收笔，不写两人朝哪边 —— 模型于是按角色参考图把人摆成正对镜头。
	// 这行同时会被 FrameScene 读去做全景参考图，一处补两处生效。
	if len(d.Roles) >= 2 && !hasFacingWords(sit) {
		place += "（空间锁定：@" + d.Roles[0].Name + " 与 @" + d.Roles[1].Name +
			" 互相面向、视线落在对方身上，不要都正对镜头）"
	}
	b.WriteString("地点：" + place + "\n")

	// 时间：优先用场景里的时段，其次从场景名猜；内外景同理
	tod, inout := "日", "内"
	if len(d.Scenes) > 0 {
		tod, inout = sceneTimeOf(d.Scenes[0])
	}
	b.WriteString("时间：" + tod + " / " + inout + "\n")

	// 人物：名字 + 一句身份（长档案只取第一句），声音参考只在有样本时写
	if len(d.Roles) > 0 {
		// 人物行也按名字排序，和「声音」行的顺序一致 ——
		// 两行顺序不同时，读起来像两组人
		sorted := sortedRoles(d.Roles)
		people := make([]string, 0, len(sorted))
		for _, r := range sorted {
			one := "@" + r.Name
			if brief := briefIdentity(r.IdentityAnchor); brief != "" {
				one += "（" + brief + "）"
			}
			people = append(people, one)
		}
		b.WriteString("人物：" + strings.Join(people, "；") + "\n")

		// 声音设定：把「谁 = 哪个说话人标记 = 什么音色」写死。
		// 取自 H3 官方的 (S1)/(S2) 写法 —— 多人对话时只写「台词：老师：…」，
		// 模型得自己把名字映射到音色，两个人声音容易串；给了标记就不会错。
		// 本项目没有传 ref_audios（音色参考音频），所以标记后面接角色档案里的音色描述。
		if v := voiceSettingLine(d.Roles); v != "" {
			b.WriteString(v + "\n")
		}
	}

	return b.String()
}

// sortedRoles 按角色名排序。
//
// 人物行、声音行、S 编号全走这一个口径 —— LLM 输出的 role_names 顺序不固定，
// 同一集里老师这帧排第一、下帧排第二，不排序的话编号和文字会互相打架。
func sortedRoles(roles []model.Character) []model.Character {
	out := make([]model.Character, len(roles))
	copy(out, roles)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// speakerTag 返回角色在这一帧里的说话人标记，如 (S1)。
//
// 编号按**角色名排序**后的顺序，不按 d.Roles 的原始顺序：
// LLM 输出的 role_names 顺序不固定，同一集里老师这帧排第一、下帧排第二，
// 编号跟着变的话台词行和「声音」行就对不上（这帧 S1 是老师，下帧 S1 成了学生）。
// 每段视频是单独生成的，只要本段内一致就够。
func speakerTag(d FrameDraft, role string) string {
	role = strings.TrimSpace(role)
	if role == "" || len(d.Roles) == 0 {
		return ""
	}
	for i, r := range sortedRoles(d.Roles) {
		if r.Name == role {
			return fmt.Sprintf("(S%d)", i+1)
		}
	}
	return ""
}

// voiceSettingLine 拼「声音：@老师 (S1) 亲切年轻女音；@学生 (S2) 松弛年轻男音」。
//
// 排序口径必须和 speakerTag 一致，否则台词行的 (S1) 会和这里写的角色对不上。
// 角色没有音色描述时也照样输出，至少把 S 编号标出来。
func voiceSettingLine(roles []model.Character) string {
	if len(roles) == 0 {
		return ""
	}
	parts := make([]string, 0, len(roles))
	for i, r := range sortedRoles(roles) {
		tone := strings.TrimSpace(r.VoiceTone)
		if tone == "" {
			tone = strings.TrimSpace(SuggestVoiceTone(r.IdentityAnchor, r.Personality))
		}
		one := fmt.Sprintf("@%s (S%d)", r.Name, i+1)
		if tone != "" {
			one += " " + tone
		}
		parts = append(parts, one)
	}
	return "声音：" + strings.Join(parts, "；") + "（S 编号本段内固定，同一个人始终用同一个）"
}

// hasFacingWords situation 里是否已经写过朝向：写了就不再补，
// 免得同一句「互相面向」在一行里出现两次，反而稀释重点。
func hasFacingWords(s string) bool {
	for _, k := range []string{"相对", "面向", "朝向", "对视", "目光", "视线"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// sceneTimeOf 从场景档案里取「日/夜」与「内/外」
func sceneTimeOf(sc model.Scene) (string, string) {
	tod := strings.TrimSpace(sc.TimeOfDay)
	name := sc.Name
	if tod == "" {
		switch {
		case strings.Contains(name, "夜"):
			tod = "夜"
		case strings.Contains(name, "黄昏"), strings.Contains(name, "傍晚"):
			tod = "黄昏"
		case strings.Contains(name, "清晨"), strings.Contains(name, "早晨"):
			tod = "晨"
		default:
			tod = "日"
		}
	}
	switch tod {
	case "白天", "日间", "日", "白天/日":
		tod = "日"
	case "夜晚", "夜间", "夜":
		tod = "夜"
	}
	inout := "内"
	for _, k := range []string{"户外", "室外", "街", "操场", "公园", "广场", "天台", "门口"} {
		if strings.Contains(name, k) {
			inout = "外"
			break
		}
	}
	return tod, inout
}

// briefIdentity 把长档案压成一句：取第一个句号前的内容，超过 20 字截断
func briefIdentity(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "。；;"); i > 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > 20 {
		return string(r[:20]) + "…"
	}
	return s
}

// shotSubject 取这一镜的主体（景别写成「中景/老师」里的那个「老师」）：
// 动作里第一个被 @ 的人是主体，没有就退回第一个角色。
func shotSubject(sh FrameShot, d FrameDraft) string {
	if m := mentionRe.FindStringSubmatch(sh.Action); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	if len(d.Roles) > 0 {
		return d.Roles[0].Name
	}
	return ""
}

// mentionRe 匹配动作里的第一个 @名字
var mentionRe = regexp.MustCompile(`@([^\s，。；、,.;]+)`)

// facingLine 拼这一镜的朝向约束。
//
// 为什么单独一行：只靠 action 里一句「目光落在 @学生 脸上」压不住模型 ——
// 单人近景（「中近景/老师」）在模型眼里就是「拍老师的正面近景」，
// 它会照着角色参考图的正面像把人物摆成正对镜头，于是第二段起老师一直不看学生。
// 把朝向提到「景别」同一层级单独写一行，模型才当构图指令执行。
func facingLine(sh FrameShot, d FrameDraft) string {
	if len(d.Roles) < 2 {
		return ""
	}
	sub := shotSubject(sh, d)
	other := ""
	for _, r := range d.Roles {
		if r.Name != sub {
			other = r.Name
			break
		}
	}
	if sub == "" || other == "" {
		return ""
	}
	// 双人/过肩/全景镜头里两个人都入画，写「相对」就够；
	// 单人镜头必须点明「四分之三侧脸」，否则模型默认正脸朝镜头。
	// 不要写死「隔着桌子」—— 两人坐沙发两端、隔着柜台、一站一坐都可能，
	// 写死了反而给出错误的空间指令。
	size := sh.ShotSize
	if strings.Contains(size, "双人") || strings.Contains(size, "过肩") || strings.Contains(size, "全景") {
		return fmt.Sprintf("@%s 与 @%s 面对面、身体互相朝向、视线交汇，不要并排看镜头", sub, other)
	}
	return fmt.Sprintf("@%s 身体朝向 @%s 所在方向，面部呈四分之三侧面，视线落在 @%s 脸上，不要正面朝向镜头", sub, other, other)
}

// BuildFrameVideoPrompt 按参考站格式拼「视频描述词」：
//
//	场次 / 地点 / 时间 / 人物
//	镜号 / 景别 / 运镜 / 情绪动作 / 台词 / 音效 / 时长
//
// 为什么换成这个写法：之前每帧 1200 字，堆了人物锁定、视觉风格、全局规则，
// 模型反而抓不住「这一镜拍什么」。参考站的 8 行结构在同样的模型上更稳。
func BuildFrameVideoPrompt(d FrameDraft, duration int) string {
	shots := normalizeShots(d)

	// 时长：优先用调用方给的；传 0 表示「自动」，按内容估算
	if duration <= 0 {
		duration = EstimateFrameDuration(d)
	}

	var b strings.Builder
	b.WriteString(frameSceneHeader(d))
	b.WriteString("\n")

	// ---- 每镜秒数 ----
	// 铁律：有台词的镜头必须够念完。
	// 之前是「按权重把总时长等比切开」—— 内容需要 11.5 秒、工具栏给 10 秒时，
	// 4.55 秒的台词镜头只分到 4 秒，生成出来就是话没说完就切镜、口型跟着错。
	// 现在改成：台词镜头按需求给足，富余时间只补给纯动作镜头。
	secs := make([]int, len(shots))
	sum := 0
	for i := range shots {
		n := shotSpeakSeconds(shots[i])
		if n <= 0 {
			// 纯动作镜头：LLM 建议几秒就几秒，底线 2 秒
			n = float64(shots[i].Seconds)
			if n < 2 {
				n = 2
			}
		} else if n < 2.5 {
			// 有台词最短 2.5 秒：1 秒的镜头模型来不及进入说话状态，
			// 实测会出现「音频 0.02 秒就开口、画面第 3 秒才动嘴」
			n = 2.5
		}
		secs[i] = int(math.Ceil(n))
		sum += secs[i]
	}
	if len(secs) == 0 {
		secs = []int{3}
		sum = 3
	}

	// 工具栏时长比需求长：把富余时间补给纯动作镜头（每镜最多 +2 秒）。
	// 台词镜头不加 —— 加长了模型会把语速拖慢，反而更容易和口型错开。
	// 全是台词镜头时余量不给：给谁都会稀释那一镜的语速。
	if extra := duration - sum; extra > 0 {
		for i := range shots {
			if extra <= 0 {
				break
			}
			if shotSpeakSeconds(shots[i]) > 0 {
				continue
			}
			add := 2
			if add > extra {
				add = extra
			}
			secs[i] += add
			sum += add
			extra -= add
		}
	}

	// H3 单段上限 15 秒，超了只能压：先压纯动作镜头，压完还超就从最长的台词镜头里扣。
	// 单镜一段时这是硬限制（拆不了），语速会落到「偏快」，模型自己会赶。
	if sum > 15 {
		over := sum - 15
		for pass := 0; pass < 2 && over > 0; pass++ {
			for i := range shots {
				if over <= 0 {
					break
				}
				if pass == 0 && shotSpeakSeconds(shots[i]) > 0 {
					continue // 第一轮不动台词镜头
				}
				cut := secs[i] - 2
				if cut > over {
					cut = over
				}
				if cut > 0 {
					secs[i] -= cut
					over -= cut
					sum -= cut
				}
			}
		}
	}

	// ---- 官方 Ref2VA 结构：summary 一句话概括 → detailed_description 装镜头 ----
	sceneName := "未指定场景"
	if len(d.Scenes) > 0 {
		sceneName = d.Scenes[0].Name
	}
	epNo := d.EpisodeNo
	if epNo <= 0 {
		epNo = 1
	}
	b.WriteString("summary:\n")
	if len(shots) == 1 {
		fmt.Fprintf(&b, "Ref2VA 目标视频：第 %d-%d 场，%s，单镜头 %d 秒。\n", epNo, d.FrameIndex, sceneName, sum)
	} else {
		fmt.Fprintf(&b, "Ref2VA 目标视频：第 %d-%d 场，%s，%d 个镜头共 %d 秒。\n",
			epNo, d.FrameIndex, sceneName, len(shots), sum)
	}
	b.WriteString("\ndetailed_description:\n")

	// ---- 逐镜输出，结构照 H3 官方 ----
	// [Shot N] 起止秒，景别，主体 +【主体】【动作】【镜头】【音效】【约束】
	// 用绝对起止秒而不是相对「时长」：模型对「0.00–3.00s」这种时间轴比「3s」敏感，
	// 也更容易把台词压在段内念完。
	cursor := 0
	for i, sh := range shots {
		start := cursor
		end := start + secs[i]
		cursor = end

		size := fallbackText(sh.ShotSize, "中景")
		sub := shotSubject(sh, d)

		// 官方时间轴写法：[Shot 1] 0.00–3.00s，用绝对秒而不是「3s」，
		// 模型对「从 0.00 到 3.00」这种区间比对一个相对时长更敏感
		head := fmt.Sprintf("[Shot %d] %.2f–%.2fs，%s", i+1, float64(start), float64(end), size)
		if sub != "" {
			// 主体带 @：RewriteMentionsForModel 会换成 <Picture N>
			head += "，主体 @" + sub
		}
		b.WriteString(head + "\n")

		// 【主体】谁是主体 + 这一镜开始时每个人在哪（承接上一镜的结果）
		if st := strings.TrimSpace(sh.State); st != "" {
			if sub != "" {
				b.WriteString("【主体】@" + sub + " 为主体；起始：" + strings.TrimRight(st, "。；，、 ") + "\n")
			} else {
				b.WriteString("【主体】起始：" + strings.TrimRight(st, "。；，、 ") + "\n")
			}
		} else if sub != "" {
			b.WriteString("【主体】@" + sub + " 为主体\n")
		}

		// 【动作】动作和台词绑在一起（官方写法：台词是时间线动作的一部分）
		lines := make([]FrameDialogue, 0, len(sh.Dialogues))
		for _, dl := range sh.Dialogues {
			if strings.TrimSpace(dl.Line) != "" {
				lines = append(lines, dl)
			}
		}

		act := strings.TrimRight(strings.TrimSpace(fallbackText(sh.Action, "延续人物当前动作，画面自然推进")), "。；")
		b.WriteString("【动作】" + act)
		if len(lines) == 0 {
			// 明说「不开口」：只说「台词：无」模型有时还是会自己编一句
			b.WriteString("（本镜无台词，不要开口）\n")
		} else {
			// 「…，温和平稳、语速平稳地说：」——怎么说和做什么写在同一句里
			b.WriteString("，" + speechCue(lines, secs[i]) + "：\n")
			for _, dl := range lines {
				role := "@" + fallbackText(dl.Role, "角色")
				// 语言标签用官方的 [Chinese]（不是 [中文]）：H3 按方括号里的
				// 语言名决定用哪种语言发音，写中文名有些版本识别不到
				if tag := speakerTag(d, dl.Role); tag != "" {
					fmt.Fprintf(&b, "%s %s <d>[Chinese] %s</d>\n", tag, role, strings.TrimSpace(dl.Line))
				} else {
					fmt.Fprintf(&b, "%s <d>[Chinese] %s</d>\n", role, strings.TrimSpace(dl.Line))
				}
			}
			// 短句必须点明「说完就停」：只给 1 个字却分到 2 秒时，
			// 不写这句模型会把那个字拖满整镜，正好赶上切镜
			if dialogueChars(lines) <= 3 {
				b.WriteString("说完即停，剩下的时间留出动作，不要拖长音。\n")
			}
		}

		// 【镜头】景别 + 运镜 + 朝向
		shotLine := "【镜头】" + size
		if sub != "" {
			shotLine += "，主体 @" + sub
		}
		shotLine += "，" + fallbackText(sh.Camera, "固定机位")
		if face := facingLine(sh, d); face != "" {
			shotLine += "；" + face
		}
		b.WriteString(shotLine + "\n")

		b.WriteString("【音效】" + fallbackText(strings.TrimSpace(sh.SFX), "无") + "\n")

		// 【约束】每个镜头都写：口型同步是硬要求，写一次模型会忘
		b.WriteString("【约束】五官稳定，面部不扭曲，口型与台词同步，画面无跳变；")
		b.WriteString("人物外观与服装前后一致，光线一致")
		if i == 0 {
			b.WriteString("；人物位置按【主体】里的站位，全程不换座、不绕桌走动")
		}
		b.WriteString("\n")

		if i < len(shots)-1 {
			b.WriteString("\n")
		}
	}

	// 全片约束：只留跨镜头才会出错的几条，镜头级的都写进各自的【约束】里
	b.WriteString("\n【全片约束】台词只念 <d>[Chinese]</d> 里的正文，标签本身和方括号里的语言名不念；")
	b.WriteString("每句台词在所在 Shot 的起止秒内念完，念完才切下一个 Shot；")
	b.WriteString("只有正在说话的角色动嘴，其他人闭口；")
	b.WriteString("台词里要求的动作（如「坐」「过来」）说到时才发生，不得提前；")
	b.WriteString("不生成字幕、水印、logo，不要 BGM，只保留同期声。\n")

	// ---- overall_soundscape：只写场景世界内部真实存在的声音 ----
	// 按官方规范，这一栏不放背景音乐；各镜的【音效】去重后汇总到这里，
	// 让模型对整个片段的声音底有一个整体印象，而不是逐镜零散地听
	sfx := make([]string, 0, len(shots))
	seenSfx := map[string]bool{}
	for _, sh := range shots {
		s := strings.TrimSpace(sh.SFX)
		if s == "" || s == "无" || seenSfx[s] {
			continue
		}
		seenSfx[s] = true
		sfx = append(sfx, s)
	}
	b.WriteString("\noverall_soundscape:\n")
	if len(sfx) > 0 {
		b.WriteString(strings.Join(sfx, "；") + "。\n")
	} else {
		b.WriteString("场景环境底噪，无额外音效。\n")
	}
	// 我们出片后再配 BGM，生成阶段不加，按官方写法填 N/A
	b.WriteString("\nnon_diegetic_music: N/A\n")

	return b.String()
}

// EnsureSpeechRate 把老描述词升级到当前的出片格式：台词包上 H3 的 <d> 标记，再补语速行。
//
// 为什么需要：<d>[中文] …</d> 和「语气：约 X 字/秒」都是后加的，库里已有的分镜是旧格式 ——
// 台词是纯文本、没有语速，视频模型只能靠上下文猜该不该念、念多快
// （之前「嘴动了但没声音」「第 3 秒开口第 4 秒才出声」的根子就在这里）。
// 重跑一遍分镜能拿到，但重跑会清空已绑定的全景参考图、还要重新出图花积分，
// 只为了改两行不划算。这里就地补：纯文本处理，不动其他内容。
//
// 已经是 H3 结构（【Shot N｜…】）或没有台词时原样返回。
func EnsureSpeechRate(vp string) string {
	// 已经是官方时间轴结构（[Shot N] / 上一版【Shot N｜…】）或补过语气行的就跳过。
	// 几个标志都要判：老格式补完 <d> 和语气行之后仍然是老格式，只看一个会重复插入。
	if strings.TrimSpace(vp) == "" ||
		strings.Contains(vp, "[Shot ") || strings.Contains(vp, "【Shot ") ||
		strings.Contains(vp, "语气：") {
		return vp
	}

	lines := strings.Split(vp, "\n")
	out := make([]string, 0, len(lines)+8)
	chars := 0
	insertAt := -1 // 最后一个「台词：」行的下一行位置

	for _, raw := range lines {
		t := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(t, "镜号："):
			chars, insertAt = 0, -1
			out = append(out, raw)
		case strings.HasPrefix(t, "台词："):
			// 台词：角色：内容 —— 只数正文，标签和标点都不占发音时间
			if m := promptLineRe.FindStringSubmatch(t); len(m) > 2 {
				if body := cleanDialogueLine(m[2]); body != "" && body != "无" {
					chars += len(speakCharRe.FindAllString(body, -1))
					// 老格式没有 H3 的台词标记，就地包上；已经是 <d> 的不动
					if !strings.Contains(t, "<d>") {
						t = "台词：" + strings.TrimSpace(m[1]) + "：<d>[Chinese] " + body + "</d>"
					}
				}
			}
			out = append(out, t)
			insertAt = len(out)
		case strings.HasPrefix(t, "时长："):
			sec := 0
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(t, "时长：")), "%ds", &sec)
			if chars > 0 && sec > 0 && insertAt >= 0 {
				// 短句不写语速（speechRateLabel 返回空），只补 <d> 标记就够
				if label := speechRateLabel(float64(chars) / float64(sec)); label != "" {
					out = append(out, "")
					copy(out[insertAt+1:], out[insertAt:])
					out[insertAt] = "语气：" + label
				}
			}
			out = append(out, raw)
		default:
			out = append(out, raw)
		}
	}
	return strings.Join(out, "\n")
}

// speechRateLabel 把「字数 ÷ 秒数」翻译成模型能执行的语速提示。
//
// **不写精确数字**。之前写「约 0.5 字/秒」这种值，模型会真的把那一个字
// 拖满整镜 2 秒，然后正好赶上切镜 —— 比不写还糟。语速只给区间，短句干脆不给。
func speechRateLabel(rate float64) string {
	switch {
	case rate <= 0:
		return ""
	case rate > 4.5:
		return "语速偏快，句间不留停顿"
	case rate > 3.8:
		return "语速稍快"
	case rate >= 2.5:
		return "语速平稳"
	default:
		// 低于 2.5 字/秒多半是短句或给动作留了时间，不写语速，
		// 让模型自然说完就停（短句另有「说完即停」兜底）
		return ""
	}
}

// dialogueChars 这一镜要念出来的字数（标点、标签都不算）
func dialogueChars(lines []FrameDialogue) int {
	n := 0
	for _, dl := range lines {
		n += len(speakCharRe.FindAllString(dl.Line, -1))
	}
	return n
}

// speechCue 拼「怎么说这句」：LLM 给的情绪语气 + 语速区间，如「温和平稳、语速平稳地说」。
//
// 顺序照 H3 官方：先说情绪/音量，再说语速。
func speechCue(lines []FrameDialogue, sec int) string {
	parts := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, dl := range lines {
		if t := strings.TrimSpace(dl.Tone); t != "" && !seen[t] {
			seen[t] = true
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "语气自然")
	}
	if s := speechRateLabel(float64(dialogueChars(lines)) / float64(sec)); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "、") + "地说"
}

// normalizeShots 归一化镜头列表：没有多镜头数据时用平铺字段合成一个，
// 兜底规则引擎路径与旧数据。
func normalizeShots(d FrameDraft) []FrameShot {
	if len(d.Shots) > 0 {
		return d.Shots
	}
	return []FrameShot{{
		ShotSize:  d.ShotSize,
		Camera:    d.Camera,
		Action:    d.Action,
		Dialogues: d.Dialogues,
		SFX:       d.SFX,
	}}
}

func roleNames(rs []model.Character) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func itemNames(its []model.Item) []string {
	out := make([]string, 0, len(its))
	for _, it := range its {
		out = append(out, it.Name)
	}
	return out
}

// mentionList 拼 @名，逗号分隔
func mentionList(names []string, empty string) string {
	clean := make([]string, 0, len(names))
	for _, n := range names {
		if s := strings.TrimSpace(n); s != "" {
			clean = append(clean, "@"+s)
		}
	}
	if len(clean) == 0 {
		return empty
	}
	return strings.Join(clean, "，")
}

// voiceBindingLine 拼「@A→亲切年轻女音；@B→松弛年轻男音」
func voiceBindingLine(roles []model.Character) string {
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		tone := strings.TrimSpace(r.VoiceTone)
		if tone == "" {
			tone = SuggestVoiceTone(r.IdentityAnchor, r.Personality)
		}
		if tone == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("@%s→%s", r.Name, tone))
	}
	return strings.Join(parts, "；")
}

// situationOf 没有 LLM 给的描述时，用场景锚点拼一段素材状态
func situationOf(d FrameDraft) string {
	var b strings.Builder
	if len(d.Scenes) > 0 {
		sc := d.Scenes[0]
		if sc.TimeOfDay != "" {
			b.WriteString(sc.TimeOfDay + "，")
		}
		b.WriteString("场景 " + sc.Name + "。")
		parts := nonEmpty(sc.SpatialAnchor, sc.LightingPrompt)
		if len(parts) > 0 {
			b.WriteString(strings.Join(parts, "，") + "。")
		}
	} else {
		b.WriteString("沿用前序镜头的场景与光线。")
	}
	return b.String()
}

// defaultPerformance 没有情绪线时退回到角色性格
func defaultPerformance(roles []model.Character) string {
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		if p := strings.TrimSpace(r.Personality); p != "" {
			parts = append(parts, fmt.Sprintf("@%s %s", r.Name, p))
		}
	}
	if len(parts) == 0 {
		return "自然克制，不夸张"
	}
	return strings.Join(parts, "；")
}

func fallbackText(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

// promptDialogueRe 匹配描述词里的台词行：【@老师台词】（语气）：“……”
// 正文用贪婪 .+ 而不是 [^”]*：台词里本身可能带中文引号（又怕“就是去工地”），
// 用 [^”]* 会在一半就截断，导致台词丢失、时长跟着算短。
var promptDialogueRe = regexp.MustCompile(`【([^】]*)台词】\x{FF08}([^\x{FF09}]*)\x{FF09}：“(.+)”`)

// promptLineRe 新格式的台词行：台词：(S1) @老师：<d>[中文] 内容</d>
// 括号里的 (S1) 和 <d> 标记都可能不出现，所以两边都按可选处理。
var promptLineRe = regexp.MustCompile(`(?m)^台词：(?:\(S\d+\)\s*)?([^：:\n]+)：(.+)$`)

// speakerTagRe 匹配角色名前面的说话人标记：(S1) / (s2)
var speakerTagRe = regexp.MustCompile(`^\s*\((?:S|s)\d+\)\s*`)

// dialogueBodyRe 匹配 H3 的台词标记：<d>[中文] 内容</d>
var dialogueBodyRe = regexp.MustCompile(`(?s)<d>\s*(?:\[[^\]]*\]\s*)?(.*?)\s*</d>`)

// cleanSpeaker 从「(S1) @老师」里取回「老师」
func cleanSpeaker(s string) string {
	s = speakerTagRe.ReplaceAllString(strings.TrimSpace(s), "")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@"))
}

// cleanDialogueLine 剥掉 H3 的台词标记，只留要念的正文。
//
// 前端和「重新生成」都靠这段文本：留着 <d>[中文] 会把标签当成台词显示，
// 还会被字数统计算进去（「中文」两个字白占时长）。
func cleanDialogueLine(s string) string {
	s = strings.TrimSpace(s)
	if m := dialogueBodyRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	// 标签没闭合时的兜底：去掉开头的 <d> 和 [语言] 标记
	s = strings.TrimSpace(strings.TrimPrefix(s, "<d>"))
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(s, "</d>"))
}

// promptBlockLineRe H3 结构的台词行：(S1) @老师 <d>[中文] 内容</d>
// 角色名可能带 (S1) 前缀和 @，台词外面包 <d> 标签
var promptBlockLineRe = regexp.MustCompile(`(?m)^(?:\(S\d+\)\s*)?@?([^\s<]+)\s+<d>\s*(?:\[[^\]]*\]\s*)?(.+?)\s*</d>\s*$`)

// ParsePromptDialogues 从已有描述词里按顺序抽出台词。
//
// 单帧「重新生成」只拿 f.Text（画面脚本，不含台词）喂给 LLM，
// 结果新描述词里一句台词都没有 —— 用户点一次重新生成，台词就全丢了。
func ParsePromptDialogues(videoPrompt string) []FrameDialogue {
	// H3 结构优先：(S1) @老师 <d>[中文] 台词</d>
	if ms := promptBlockLineRe.FindAllStringSubmatch(videoPrompt, -1); len(ms) > 0 {
		out := make([]FrameDialogue, 0, len(ms))
		for _, m := range ms {
			role := strings.TrimSpace(m[1])
			line := strings.TrimSpace(m[2])
			if role == "" || line == "" || line == "无" {
				continue
			}
			out = append(out, FrameDialogue{Role: role, Line: line})
		}
		if len(out) > 0 {
			return out
		}
	}

	ms := promptDialogueRe.FindAllStringSubmatch(videoPrompt, -1)
	out := make([]FrameDialogue, 0, len(ms))
	// 新格式：台词：角色：内容
	if len(ms) == 0 {
		for _, m := range promptLineRe.FindAllStringSubmatch(videoPrompt, -1) {
			role := cleanSpeaker(m[1])
			line := cleanDialogueLine(m[2])
			if line == "" || line == "无" {
				continue
			}
			out = append(out, FrameDialogue{Role: role, Line: line})
		}
		return out
	}
	for _, m := range ms {
		role := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[1]), "@"))
		line := strings.TrimSpace(m[3])
		if line == "" {
			continue
		}
		if role == "" {
			role = "角色"
		}
		out = append(out, FrameDialogue{Role: role, Tone: strings.TrimSpace(m[2]), Line: line})
	}
	return out
}

// DialoguesPrompt 把台词列表拼成给 LLM 的文本
func DialoguesPrompt(lines []FrameDialogue) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n【这一帧必须逐字保留的台词，共 %d 句；每句安排一个镜头，顺序不能变】\n", len(lines))
	for i, dl := range lines {
		fmt.Fprintf(&b, "%d. %s：%s\n", i+1, dl.Role, dl.Line)
	}
	return b.String()
}

// ApplyDialogues 把指定台词按顺序填回分镜（一镜一句）。
// LLM 重写时经常漏台词或改措辞，这里兜底：抽到的原台词字数明显少于原文时，
// 直接按顺序覆盖回去，宁可动作描述平淡，也不能把台词弄丢。
func ApplyDialogues(d FrameDraft, want []FrameDialogue) FrameDraft {
	if len(want) == 0 {
		return d
	}
	shots := normalizeShots(d)
	if len(shots) == 0 {
		return d
	}

	got := 0
	for _, sh := range shots {
		for _, dl := range sh.Dialogues {
			got += len([]rune(strings.TrimSpace(dl.Line)))
		}
	}
	need := 0
	for _, dl := range want {
		need += len([]rune(strings.TrimSpace(dl.Line)))
	}
	if got*10 >= need*8 { // 台词基本保住了，尊重 LLM 的版本
		return d
	}

	for i := range shots {
		shots[i].Dialogues = nil
	}
	for i, dl := range want {
		idx := i
		if idx >= len(shots) {
			idx = len(shots) - 1 // 镜头不够时后面的台词并入最后一镜
		}
		shots[idx].Dialogues = append(shots[idx].Dialogues, dl)
	}

	d.Shots = shots
	d.Dialogues = mergeShotDialogues(shots)
	d.Duration = EstimateFrameDuration(d)
	d.VideoPrompt = BuildFrameVideoPrompt(d, d.Duration)
	return d
}

// RewriteMentionsForModel 把描述词里的 @角色名 换成 <Picture N>，并生成 subject_definitions。
//
// 上游只收到一串图片，它并不知道「@老师」是第几张图 ——
// 用官方标签 <Picture N> 指代、再给一份 subject_definitions，模型才能把文字和参考图对上号，
// 也才知道每张图是拿来锁脸的、锁空间的，还是锁道具的。
// names 的顺序必须与参考图提交顺序一致（<Picture 1> = names[0]）。
func RewriteMentionsForModel(videoPrompt string, names, kinds []string) string {
	type pair struct{ name, tag string }
	pairs := make([]pair, 0, len(names))
	defs := make([]string, 0, len(names))
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		kind := ""
		if i < len(kinds) {
			kind = kinds[i]
		}
		tag := fmt.Sprintf("<Picture %d>", i+1)
		pairs = append(pairs, pair{n, tag})
		defs = append(defs, subjectDefinition(i+1, n, kind))
	}
	if len(pairs) == 0 {
		return videoPrompt
	}

	// 长名字先替换：「老师的」不能被「老师」先吃掉
	sort.Slice(pairs, func(i, j int) bool { return len([]rune(pairs[i].name)) > len([]rune(pairs[j].name)) })

	out := videoPrompt
	for _, p := range pairs {
		out = strings.ReplaceAll(out, "@"+p.name, p.tag)
		// 台词行的角色也换成图位，模型才知道这句是谁说的
		out = strings.ReplaceAll(out, "【"+p.name+"台词】", "【"+p.tag+"台词】")
		out = strings.ReplaceAll(out, "【@"+p.name+"台词】", "【"+p.tag+"台词】")
	}

	var b strings.Builder
	b.WriteString("subject_definitions:\n")
	for _, d := range defs {
		b.WriteString(d + "\n")
	}
	b.WriteString("\n")
	out = b.String() + out

	// retention_analysis 按官方顺序排在 detailed_description 之前：
	// subject_definitions 说「这张图是什么」，retention_analysis 说「拿它锁什么、允许变什么」
	if i := strings.Index(out, "\ndetailed_description:"); i >= 0 {
		out = out[:i] + "\n" + retentionAnalysis(names, kinds) + out[i:]
	}
	return out
}

// retentionAnalysis 拼官方 retention_analysis：每张图用哪种保留策略。
//
// 场景走 weak_reference（只借空间和光，不照抄构图），人物和道具走 fully_preserved
// （外观锁死，姿态表情随剧情变）—— 这条分不清的话，模型会把全景图的机位当成
// 每段都要复刻的构图，或者把人物图的表情一路带到所有镜头里。
func retentionAnalysis(names, kinds []string) string {
	lines := make([]string, 0, len(names))
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		kind := ""
		if i < len(kinds) {
			kind = kinds[i]
		}
		tag := fmt.Sprintf("<Picture %d>", i+1)
		switch {
		case kind == "scene" || strings.Contains(n, "场景全景"):
			lines = append(lines, tag+"（"+n+"）：weak_reference — 只参考空间结构、家具布局与光线方向，构图与机位不照搬。")
		case kind == "item":
			lines = append(lines, tag+"（"+n+"）：fully_preserved — 颜色、材质、形状完整保留，不替换、不增减。")
		default:
			lines = append(lines, tag+"（"+n+"）：fully_preserved — 面部、发型、服装完整保留，姿态与表情随动作变化。")
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "retention_analysis:\n" + strings.Join(lines, "\n") + "\n"
}

// subjectDefinition 拼一条 subject_definitions：这张图是什么、保留什么、怎么用。
//
// 官方规范要求写明每个素材承担的作用 —— 只写「参考图 1」，模型不知道
// 该拿它的脸、它的空间，还是它的光线，几项一混画面就漂。
func subjectDefinition(n int, name, kind string) string {
	tag := fmt.Sprintf("<Picture %d>", n)
	switch {
	case kind == "scene" || strings.Contains(name, "场景全景"):
		return tag + " 是「" + name + "」：只参考空间结构、家具布局和光线方向；" +
			"构图、景别和机位不照搬这张图。"
	case kind == "item":
		return tag + " 是「" + name + "」：颜色、材质、形状完整保留，" +
			"不换成别的款式，也不要凭空替换或增减。"
	case kind == "character":
		return tag + " 是「" + name + "」：面部特征、发型、服装完整保留，" +
			"不因动作、表情或光线改变；姿态与表情随剧情变化。"
	default:
		return tag + " 是「" + name + "」：外观、身份与材质完整保留，" +
			"不因动作、表情或光线改变；姿态与表情随剧情变化。"
	}
}
