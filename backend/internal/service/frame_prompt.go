package service

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
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
// 与 BuildFrameVideoPrompt 共用 planShotSeconds，保证「分镜卡片上的时长」
// 和「描述词里的时间轴」永远是同一个数。
func shotsDurationNeed(shots []FrameShot) float64 {
	_, sum := planShotSeconds(shots)
	return float64(sum)
}

// planShotSeconds 给每个镜头分配秒数，返回逐镜秒数与总和。
//
// 这是时长计算的**唯一口径**：EstimateFrameDuration（卡片显示的秒数）
// 和 BuildFrameVideoPrompt（描述词里的 [Shot N] 起止秒）都必须走这里。
//
// 曾经是两套算法：Estimate 用「浮点累加 + 0.4 呼吸 + 统一 ceil」，
// Build 用「逐镜 ceil 再相加」。同样 8 个字的一句台词，
// 前者算出 4 秒、后者算出 3 秒 —— 于是卡片显示 4 秒、描述词写 0.00–3.00s。
//
// 规则：
//   - 有台词：按 3.8 字/秒念完所需，最低 2.5 秒（1 秒的镜头模型来不及进入说话状态）
//   - 纯动作/空镜：用 LLM 建议的秒数，底线 2 秒
func planShotSeconds(shots []FrameShot) ([]int, int) {
	if len(shots) == 0 {
		return []int{3}, 3
	}

	need := make([]float64, len(shots))
	spoken := false
	for i := range shots {
		n := shotSpeakSeconds(shots[i])
		if n <= 0 {
			n = float64(shots[i].Seconds)
			if n < 2 {
				n = 2
			}
		} else {
			spoken = true
			if n < 2.5 {
				n = 2.5
			}
		}
		need[i] = n
	}

	// 有台词的帧在末尾留 0.4 秒呼吸：模型收句、画面落幅都需要余量。
	// 只加在最后一镜，不逐镜加 —— 三镜各加 0.4 会把 3 秒的段撑成 5 秒。
	if spoken {
		need[len(need)-1] += 0.4
	}

	secs := make([]int, len(need))
	sum := 0
	for i := range need {
		secs[i] = int(math.Ceil(need[i]))
		sum += secs[i]
	}
	return secs, sum
}

// EstimateFrameDuration 按这一帧的内容估算该多长（秒）
//
// 依据两条：
//   - 台词按中文口语 4.5 字/秒折算，前后各留一点呼吸，+1 秒
//   - 每个镜头至少要起幅和落幅，按 2.5 秒/镜兜底
//
// 两者取大，向上取整，夹在 [3, 15]（上游时长节点支持的范围）。
func EstimateFrameDuration(d FrameDraft) int {
	// 与 BuildFrameVideoPrompt 共用 planShotSeconds：
	// 卡片显示的秒数必须等于描述词时间轴的总长，否则会出现
	// 「卡片 4 秒 / 描述词 0.00–3.00s」这种自相矛盾。
	_, sum := planShotSeconds(normalizeShots(d))
	if sum < 3 {
		sum = 3
	}
	if sum > 15 {
		sum = 15
	}
	return sum
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
	sit := strings.TrimSpace(d.Situation)

	place := "未指定场景"
	if d.SceneRef == "video" {
		// 360 视频模式：房间的家具、陈设全部由参考视频给出，
		// 地点行只说明「在视频 1 的场景里」+ 人物位置，不再罗列空间细节。
		place = "在参考视频 1 的场景中"
		if len(d.Scenes) > 0 {
			place += "（@" + d.Scenes[0].Name + "）"
		}
		if sit != "" {
			place += " " + sit
		}
	} else {
		if len(d.Scenes) > 0 {
			names := make([]string, 0, len(d.Scenes))
			for _, sc := range d.Scenes {
				names = append(names, "@"+sc.Name)
			}
			place = strings.Join(names, "、")
		}
		if sit != "" {
			place += " " + sit
		}
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
	// 只取前两段（性别/年龄最关键，职业看角色名就知道）。
	// 原来按 20 字硬截，会切在词中间变成「招生咨…」，比不给还难看。
	parts := strings.Split(s, "，")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.TrimSpace(strings.Join(parts, "，"))
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
// BuildFrameVideoPrompt 拼「视频描述词」。格式对齐参考站（即刻AI）：
//
//	[出镜角色-物品-场景]:       人物 / 场景 / 物品 清单一览
//	角色-声线绑定:              谁用什么嗓音
//	【素材与场景状态】          时间、地点、光线、人物站位
//	|镜头1| 0-5秒| 双人中景|     每镜：动作 → 台词 → 音效
//
// 为什么不再用官方 Ref2VA 的 summary / detailed_description 结构：
// 这条链路的工作流（minimax-h3-i2v-7img）内部自带 Qwen H3 Prompt 优化节点，
// 会把我们给的结构化文本再加工一遍 —— 两层 schema 叠在一起反而互相打架，
// 优化节点需要的是「人读得懂的自然语言」，不是又一版 JSON 式骨架。
//
// 台词不再包 <d>[Chinese]</d>：参考站也是纯中文引号，语言交给优化节点。
// 注意：若以后切到 noopt 工作流（去掉优化节点），需要把语言标记加回去，
// 否则模型可能不知道用中文念。
// BuildFrameVideoPrompt 拼「视频描述词」，按 H3 分镜脚本的字段格式：
//
//	场次：1-1
//	地点：@教师办公室-白天 右侧推拉窗的自然光斜射入室
//	时间：日 / 内
//	人物：@老师（男，52岁），中年男中低音；@学生（男，18岁），年轻男声
//	物品：@深蓝色保温杯，@专业介绍册
//
//	镜号：1
//	景别：双人中景
//	运镜：固定机位
//	情绪/动作：@老师 抬右手示意 @学生 落座，目光落在 @学生 脸上
//	台词：@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕“就是去工地”。</d>
//	音效：椅背轻响、窗外隐约环境声
//	时长：6s
//
// 为什么用这套字段：这是 H3 工作流（minimax-h3-i2v-7img）里验证过的真实写法，
// 模型对「镜号/景别/运镜/情绪动作/台词/音效/时长」这些字段边界最清楚，
// 比紧凑的 |镜头N| 或 Ref2VA 的 [Shot N] 都稳。
//
// 人物/场景/道具一律写 @名字，**不写死 @图片N**：
// 图位顺序是「首帧图 → 角色 → 场景 → 道具」（见 frameRefSlots），
// 和示例里的「角色→角色→场景」不同，写死编号必然错位。
// 提交时 RewriteMentionsForModel 会按真实顺序换成 <Picture N>。
// UseRef2VA 是否用「短剧 H3 六段式」输出描述词。
//
// 六段式（subject_definitions / summary / retention_analysis /
// detailed_description / overall_soundscape / non_diegetic_music）是 H3 官方
// Ref2VA 写法，带绝对时间码，实测动作时序比旧的「场次/镜号」格式稳得多。
// 留这个开关是为了出问题时能一键切回旧格式（老分镜的描述词仍是旧格式）。
var UseRef2VA = true

func BuildFrameVideoPrompt(d FrameDraft, duration int) string {
	if UseRef2VA {
		return BuildRef2VAPrompt(d, duration)
	}
	shots := normalizeShots(d)

	// 时长：优先用调用方给的；传 0 表示「自动」，按内容估算
	if duration <= 0 {
		duration = EstimateFrameDuration(d)
	}

	// ---- 每镜秒数 ----
	// 铁律：有台词的镜头必须够念完。统一走 planShotSeconds，
	// 保证和 EstimateFrameDuration 算出同一个总数。
	secs, sum := planShotSeconds(shots)

	// 工具栏时长比需求长：富余时间只补给纯动作镜头（每镜最多 +2 秒）。
	// 台词镜头不加 —— 加长了模型会把语速拖慢，反而更容易和口型错开。
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

	// H3 单段上限 15 秒，超了只能压：先压纯动作镜头，压完还超再从最长台词镜头扣
	if sum > 15 {
		over := sum - 15
		for pass := 0; pass < 2 && over > 0; pass++ {
			for i := range shots {
				if over <= 0 {
					break
				}
				if pass == 0 && shotSpeakSeconds(shots[i]) > 0 {
					continue
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

	var b strings.Builder

	// 字幕约束放最前：台词用中文引号或纯文本时，模型容易把内容画成画面文字
	b.WriteString("（画面中不得出现任何字幕或文字：台词只用于配音，不要以文字形式显示）\n\n")

	epNo := d.EpisodeNo
	if epNo <= 0 {
		epNo = 1
	}
	fmt.Fprintf(&b, "场次：%d-%d\n", epNo, d.FrameIndex)
	b.WriteString("地点：" + scenePlace(d) + "\n")
	b.WriteString("时间：" + sceneTime(d) + "\n")
	b.WriteString("人物：" + peopleWithVoice(d) + "\n")
	// 站位必须单独一行、每段都写。
	//
	// 只靠首帧图锁不住位置：那张图是「空间基准」，模型构图时不会照搬，
	// 而每段的景别/机位又各不相同 —— 没有文字写死「谁在桌子哪一侧」，
	// 模型就会自由安排，于是同一场戏里两个人一会儿并排、一会儿前后、
	// 甚至多出一个背影（实测第 8/13/18/22 段全乱，第 22 段还多画了一个人）。
	if sit := standingLine(d); sit != "" {
		b.WriteString("站位：" + sit + "\n")
	}
	if len(d.Items) > 0 {
		b.WriteString("物品：" + mentionList(itemNames(d.Items), "无") + "\n")
	}

	cursor := 0
	for i, sh := range shots {
		start := cursor
		end := start + secs[i]
		cursor = end

		fmt.Fprintf(&b, "\n镜号：%d\n", i+1)
		size := fallbackText(sh.ShotSize, "中景")
		// 景别里点名了角色（「老师中近景」）就是单人镜头，另一个人不该入画 ——
		// 不点明的话模型会顺手把对面的人也画进来，看起来就像多了一个人
		if name := singleShotSubject(size, d.Roles); name != "" {
			size += "（画面里只出现 @" + name + "，其他人不入画）"
		}
		b.WriteString("景别：" + size + "\n")
		b.WriteString("运镜：" + fallbackText(sh.Camera, "固定机位") + "\n")

		act := strings.TrimRight(strings.TrimSpace(fallbackText(sh.Action, "延续人物当前动作，画面自然推进")), "。；")
		b.WriteString("情绪/动作：" + act + "。\n")

		lines := make([]FrameDialogue, 0, len(sh.Dialogues))
		for _, dl := range sh.Dialogues {
			if strings.TrimSpace(dl.Line) != "" {
				lines = append(lines, dl)
			}
		}
		if len(lines) == 0 {
			// 明说「不开口」：只留空模型会自己编一句台词
			b.WriteString("台词：无\n")
		}
		for _, dl := range lines {
			role := fallbackText(dl.Role, "角色")
			// 语气写在角色名后的括号里：示例格式没有这一项，但语气直接决定表演，
			// 丢了模型会按字面平铺直叙。
			tone := strings.TrimSpace(dl.Tone)
			// 语速只给定性区间；语气里已经写了「语速」就不再重复追加
			if !strings.Contains(tone, "语速") {
				if s := speechRateLabel(float64(dialogueChars([]FrameDialogue{dl})) / float64(secs[i])); s != "" {
					if tone == "" {
						tone = s
					} else {
						tone += "，" + s
					}
				}
			}
			if tone != "" {
				fmt.Fprintf(&b, "台词：@%s（%s）：<d>[Chinese] %s</d>\n", role, tone, strings.TrimSpace(dl.Line))
			} else {
				fmt.Fprintf(&b, "台词：@%s：<d>[Chinese] %s</d>\n", role, strings.TrimSpace(dl.Line))
			}
		}

		b.WriteString("音效：" + fallbackText(strings.TrimSpace(sh.SFX), "无") + "\n")
		fmt.Fprintf(&b, "时长：%ds\n", secs[i])
	}

	// ---- 执行要点 ----
	// 参考站/示例里没有这块，但这几条都是踩过坑的硬约束
	// （字幕、动作被提前做完、没说话的人也在动嘴、自己加 BGM）。
	b.WriteString("\n【执行要点】\n")
	// 人数约束：不写的话，过肩/双人镜头里模型会多画一个「前景背影」，
	// 剪出来就是凭空多了一个人（实测第 22 段出现过两个学生）
	if n := len(d.Roles); n > 0 {
		fmt.Fprintf(&b, "画面里只出现人物行列出的这 %d 个角色，不要增加路人、背景人物或前景背影；", n)
	}
	b.WriteString("台词只用来配音，**不要生成任何字幕**，画面里不许出现文字、标题、角标、水印、logo；")
	b.WriteString("台词里要求的动作（如「坐」「过来」）说到时才发生，不得提前；")
	b.WriteString("每句台词在所在镜头内念完，念完才切下一个镜头；")
	b.WriteString("只有正在说话的角色动嘴，其他人闭口；")
	b.WriteString("人物位置严格按「站位」行，全程不换座、不绕桌走动；单人镜头里其他人不入画；")
	// 场景元素约束：360 视频模式下地点行不列家具，模型容易按「教室」的刻板印象
	// 自己加黑板/讲台（实测出一段里凭空多了一块黑板，而场景是白板）。
	if d.SceneRef == "video" {
		b.WriteString("场景以参考视频为准：墙面、家具、陈设都照参考视频来，" +
			"**不要添加参考视频里没有的东西**（例如黑板、讲台、投影幕、挂钟）；")
	}
	b.WriteString("不要 BGM，只保留同期声。\n")

	return b.String()
}

// standingLine 这一帧的人物站位：谁在哪一侧、坐着还是站着、身体朝谁
//
// 优先用第一镜的 State（LLM 给的起始状态，最准）；
// 没有就退回 Situation 里描述站位的那半句；再没有就按人数给个兜底。
func standingLine(d FrameDraft) string {
	// 有座位表就完全按表拼 —— 这是最稳的一层。
	// 模型每段都会换个说法描述同一个座位，模型自己读着像换了位置，
	// 干脆不让它写位置：位置查表，动作交给镜头的 action。
	if len(d.SeatMap) > 0 && len(d.Roles) > 0 {
		parts := make([]string, 0, len(d.Roles))
		for _, r := range d.Roles {
			if seat, ok := d.SeatMap[r.Name]; ok && strings.TrimSpace(seat) != "" {
				parts = append(parts, "@"+r.Name+" 坐在"+seat)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "；") + "（全程固定座位，不换座）"
		}
	}

	if shots := normalizeShots(d); len(shots) > 0 {
		if s := strings.TrimSpace(shots[0].State); s != "" {
			return ensureSeatWords(s)
		}
	}
	// Situation 是「时间 + 地点 + 站位」混在一起的一段，取最后一个句号之后的部分
	if s := strings.TrimSpace(d.Situation); s != "" {
		if i := strings.LastIndex(s, "。"); i > 0 && i < len(s)-1 {
			if tail := strings.TrimSpace(s[i+len("。"):]); tail != "" {
				return ensureSeatWords(tail)
			}
		}
	}
	// 兜底：两人场景按「隔桌相对」写死，总比不写强
	if len(d.Roles) == 2 {
		return "@" + d.Roles[0].Name + " 与 @" + d.Roles[1].Name +
			" 隔着桌子相对而坐，身体互相朝向、视线交汇，各自固定在自己的座位"
	}
	return ""
}

// scenePlace 地点行：@场景名 + 空间布局描述
func scenePlace(d FrameDraft) string {
	if len(d.Scenes) == 0 {
		return "沿用前序镜头的场景"
	}
	sc := d.Scenes[0]

	// 360 视频模式：房间的家具、陈设全部由参考视频给出。
	// 这里再罗列一遍「左侧靠墙深棕木书架…中央深棕木纹办公桌…」只会和视频里的
	// 实际排布打架，模型还得判断该信文字还是信视频。只留场景名，加一句
	// 「在参考视频 1 的场景中」把空间指过去。
	if d.SceneRef == "video" {
		// 连场景名都不写。
		//
		// 「教师办公室-白天」这种名字会直接把模型带到刻板印象上 ——
		// 实测它据此脑补出一整块黑板（场景里其实是白板）。
		// 空间信息全部交给参考视频，文字这边只说「按视频来、保持一致」。
		return "在参考视频 1 的场景中，严格保持场景一致性"
	}

	out := "@" + sc.Name
	if a := strings.TrimSpace(sc.SpatialAnchor); a != "" {
		out += " " + a
	}
	return out
}

// sceneTime 时间行
func sceneTime(d FrameDraft) string {
	if len(d.Scenes) > 0 {
		if t := strings.TrimSpace(d.Scenes[0].TimeOfDay); t != "" {
			return t
		}
	}
	return "日 / 内"
}

// peopleWithVoice 人物行：@角色（身份），音色
//
// 音色写在人物行里（示例是「音色参考@音频1」）：我们没有音频资产，
// 用文字音色代替，模型同样能据此选嗓音。
func peopleWithVoice(d FrameDraft) string {
	if len(d.Roles) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(d.Roles))
	// 音色是「音频」和「文字描述」二选一：传了参考音频就用音频，
	// 没传才退回文字描述。编号按角色出现顺序，和出片时传音频的顺序一致。
	audioIdx := 0
	for _, r := range d.Roles {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		s := "@" + name
		if brief := briefIdentity(r.IdentityAnchor); brief != "" {
			s += "（" + brief + "）"
		}
		if strings.TrimSpace(r.VoiceSample) != "" {
			audioIdx++
			s += fmt.Sprintf("，音色参考@音频%d", audioIdx)
		} else {
			tone := strings.TrimSpace(r.VoiceTone)
			if tone == "" {
				tone = SuggestVoiceTone(r.IdentityAnchor, r.Personality)
			}
			if tone = briefTone(tone); tone != "" {
				s += "，" + tone
			}
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "；")
}

// singleShotSubject 景别里点名的角色（「老师中近景」→ 老师），没有就返回空
func singleShotSubject(size string, roles []model.Character) string {
	for _, r := range roles {
		if n := strings.TrimSpace(r.Name); n != "" && strings.Contains(size, n) {
			return n
		}
	}
	return ""
}

// briefTone 音色只取第一段：「中年男中低音：中低音域，沙哑温厚…」→「中年男中低音」
func briefTone(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "：:，,。"); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// sceneSituation 拼【素材与场景状态】：时间、地点、光线，外加人物站位。
//
// 参考站这一段是一整段自然语言，站位写在最后一句 ——
// 模型（尤其带优化节点的那种）对「谁坐在哪一侧」这种大白话最敏感。
func sceneSituation(d FrameDraft) string {
	// Situation 就是为这一段准备的：时间 + 地点 + 光线 + 站位，写好了一次性用
	if s := strings.TrimSpace(d.Situation); s != "" {
		return s
	}
	var b strings.Builder
	if len(d.Scenes) > 0 {
		sc := d.Scenes[0]
		if t := strings.TrimSpace(sc.TimeOfDay); t != "" {
			b.WriteString(t + "，")
		}
		b.WriteString("室内，@" + sc.Name + "。")
		if a := strings.TrimSpace(sc.SpatialAnchor); a != "" {
			b.WriteString(a + "。")
		}
		if l := strings.TrimSpace(sc.LightingPrompt); l != "" {
			b.WriteString(l + "。")
		}
	} else {
		b.WriteString("沿用前序镜头的场景与光线。")
	}
	// 站位取第一镜的起始状态：这一帧开场时人都在哪
	if shots := normalizeShots(d); len(shots) > 0 {
		if st := strings.TrimSpace(shots[0].State); st != "" {
			b.WriteString(st + "。")
		}
	}
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
// promptInlineToneRe 新格式把语气写在台词行括号里：台词：@老师（自然，语速平稳）：…
var promptInlineToneRe = regexp.MustCompile(`(?m)^台词：@?[^（：:\n]+（[^）]+）[：:]`)

func EnsureSpeechRate(vp string) string {
	// 已经是官方时间轴结构（[Shot N] / 上一版【Shot N｜…】）或补过语气行的就跳过。
	// 几个标志都要判：老格式补完 <d> 和语气行之后仍然是老格式，只看一个会重复插入。
	//
	// 新格式（场次/镜号/台词：@角色（语气）：）自带语气括号，也要跳过 ——
	// 只判「语气：」这一行的话，会在下面再插一行重复的「语气：语速平稳」。
	if strings.TrimSpace(vp) == "" ||
		strings.Contains(vp, "[Shot ") || strings.Contains(vp, "【Shot ") ||
		strings.Contains(vp, "语气：") ||
		promptInlineToneRe.MatchString(vp) {
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

// promptQuoteLineRe 参考站格式的台词行：【@女主 台词】（满带欣喜，语调轻快而笃定）：正文
//
// 正文可能是中文引号包裹，也可能是 <d>[Chinese] 正文</d>，两种都收，
// 解析时再统一清洗（见 cleanDialogueBody）。用贪婪 .+ 匹配到行尾 ——
// 台词内部本身就可能带引号（「又怕“就是去工地”」），非贪婪会在内层引号处断开。
var promptQuoteLineRe = regexp.MustCompile(`(?m)【@?([^】\s]+)\s*台词】(?:（([^）]*)）)?\s*[：:]?\s*(.+)\s*$`)

// promptFieldDialogueRe 分镜脚本格式的台词行：台词：@老师（平缓笃定）：<d>[Chinese] 坐。</d>
// 角色名后的括号是语气，不是名字的一部分 —— 之前没单独认这种写法，
// 解析时会把「女主（满带欣喜）」整个当角色名，前端 chips 上就会多出一串语气词。
var promptFieldDialogueRe = regexp.MustCompile(`(?m)^台词：\s*@?([^（：:\n]+?)\s*(?:（([^）]*)）)?\s*[：:]\s*(.+)$`)

// promptTagRe 台词正文里需要剥掉的标记：<d> </d> [Chinese]
var promptTagRe = regexp.MustCompile(`</?d>|\[[A-Za-z]+\]`)

// cleanDialogueBody 去掉台词正文外层的引号和 <d> 标记，只留能念的文字
func cleanDialogueBody(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "“")
	s = strings.TrimSuffix(s, "”")
	s = strings.TrimPrefix(s, `"`)
	s = strings.TrimSuffix(s, `"`)
	s = promptTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// ParsePromptDialogues 从已有描述词里按顺序抽出台词。
//
// 单帧「重新生成」只拿 f.Text（画面脚本，不含台词）喂给 LLM，
// 结果新描述词里一句台词都没有 —— 用户点一次重新生成，台词就全丢了。
func ParsePromptDialogues(videoPrompt string) []FrameDialogue {
	// 分镜脚本格式优先：台词：@老师（语气）：<d>[Chinese] 正文</d>
	if ms := promptFieldDialogueRe.FindAllStringSubmatch(videoPrompt, -1); len(ms) > 0 {
		out := make([]FrameDialogue, 0, len(ms))
		for _, m := range ms {
			role := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[1]), "@"))
			line := cleanDialogueBody(m[3])
			if role == "" || line == "" || line == "无" || line == "N/A" {
				continue
			}
			out = append(out, FrameDialogue{Role: role, Tone: strings.TrimSpace(m[2]), Line: line})
		}
		if len(out) > 0 {
			return out
		}
	}

	// 参考站格式：【@女主 台词】（语气）：正文（引号或 <d> 标签都收）
	if ms := promptQuoteLineRe.FindAllStringSubmatch(videoPrompt, -1); len(ms) > 0 {
		out := make([]FrameDialogue, 0, len(ms))
		for _, m := range ms {
			role := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[1]), "@"))
			line := cleanDialogueBody(m[3])
			if role == "" || line == "" || line == "无" || line == "N/A" {
				continue
			}
			out = append(out, FrameDialogue{Role: role, Tone: strings.TrimSpace(m[2]), Line: line})
		}
		if len(out) > 0 {
			return out
		}
	}

	// 旧 H3 结构：(S1) @老师 <d>[中文] 台词</d>
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

	// 六段式提示词自带 subject_definitions（含 OUTFIT LOCKED / POSITION LOCKED
	// 这些锁定清单），这里再加一遍就会出现两套定义，模型不知道听谁的 ——
	// 实测出过「<Picture 1> 是学生」和「<Subject 1> is 老师, from <Picture 1>」
	// 同时出现在开头，人物直接对调。
	if !strings.Contains(videoPrompt, "subject_definitions:") {
		var b strings.Builder
		b.WriteString("subject_definitions:\n")
		for _, d := range defs {
			b.WriteString(d + "\n")
		}
		b.WriteString("\n")
		out = b.String() + out
	}

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
			// 用 partially_copy 而不是 weak_reference：weak_reference 语义上就是
			// 「这张图只当背景，构图别管」，正是人物位置漂移的源头。
			lines = append(lines, tag+"（"+n+"）：partially_copy — 空间布局、家具位置、光线方向、"+
				"以及人物在画面中的左右位置与朝向全部锁定；只有景别远近与机位角度可以调整。")
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
		// 这里必须锁「人物在画面中的左右位置」，不能只说「参考空间结构」。
		// 之前写的是「构图、景别和机位不照搬」—— 模型据此把这张图当纯背景，
		// 每段自己重新摆人，于是同一场戏里两个人一会儿并排、一会儿左右互换、
		// 甚至多出一个背影（实测第 8/13/18/22 段全乱）。
		return tag + " 是「" + name + "」：空间结构、家具布局、光线方向，" +
			"**以及每个人在画面中的左右位置、坐着还是站着、身体朝向**，全部以这张图为准；" +
			"只允许改变景别远近和机位角度，不允许把人挪到另一侧、换座位或改变朝向。"
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

var (
	// 两种时间轴写法都要认：
	//
	//	新格式（参考站风格）：|镜头1| 0-5秒| 双人中景|
	//	旧格式（Ref2VA）    ：[Shot 1] 0.00–3.00s，双人中景
	//
	// 库里新旧数据混杂，只认一种会让另一批数据「改时长」时静默失效 ——
	// 改完发现描述词没变，很容易误判成接口没生效。
	//
	// 捕获组：1=整个头部 2=Shot 编号 3=镜头编号 4=起始秒 5=结束秒
	shotTimelineRe = regexp.MustCompile(`(\[Shot\s+(\d+)\]|\|镜头(\d+)\|)\s*(\d+(?:\.\d+)?)\s*[–—~～\-]\s*(\d+(?:\.\d+)?)\s*(?:s|秒)`)
	// summary 里的秒数：单镜头 3 秒 / 3 个镜头共 13 秒（新格式没有这两行，匹配不到即不改）
	summaryOneShotRe = regexp.MustCompile(`单镜头\s*\d+\s*秒`)
	summaryMultiRe   = regexp.MustCompile(`共\s*\d+\s*秒`)
)

// RescalePromptDuration 把描述词里的时间轴整体缩放到 total 秒。
//
// 为什么需要：Duration 字段（分镜卡片上显示的秒数）和描述词里的
// [Shot N] 起止秒是**两个独立存储的值**。PUT /storyboard/:id 允许只改
// Duration 而不动描述词，于是出现「卡片显示 13 秒、描述词写 0.00–3.00s」。
// 出片时传给时长节点的是 Duration，模型会把 3 秒的动作硬拉成 13 秒，
// 后 10 秒基本是静止或重复 —— 这是最容易被当成「模型不动」的假象。
//
// 多镜头按原比例分配，累计取整保证最后一段正好落在 total，
// 每段至少 1 秒（总时长小于镜头数时不再压缩）。
func RescalePromptDuration(prompt string, total int) string {
	if total <= 0 || strings.TrimSpace(prompt) == "" {
		return prompt
	}
	matches := shotTimelineRe.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) == 0 {
		return prompt
	}

	type seg struct {
		from, to int     // 在 prompt 里的字节区间
		dur      float64 // 原时长
		bracket  bool    // true = [Shot N] 旧格式；false = |镜头N| 新格式
		no       string  // 镜头编号原文，替换时要保留
	}
	segs := make([]seg, 0, len(matches))
	sum := 0.0
	for _, m := range matches {
		s, err1 := strconv.ParseFloat(prompt[m[8]:m[9]], 64)
		e, err2 := strconv.ParseFloat(prompt[m[10]:m[11]], 64)
		if err1 != nil || err2 != nil {
			return prompt // 解析不了就原样返回，不做破坏性改写
		}
		if e <= s {
			e = s + 1
		}
		sg := seg{from: m[0], to: m[1], dur: e - s}
		if m[4] >= 0 { // 旧格式：[Shot N]
			sg.bracket = true
			sg.no = prompt[m[4]:m[5]]
		} else { // 新格式：|镜头N|
			sg.no = prompt[m[6]:m[7]]
		}
		segs = append(segs, sg)
		sum += e - s
	}
	if sum <= 0 {
		return prompt
	}

	var b strings.Builder
	cursor, last := 0, 0
	for i, sg := range segs {
		d := int(math.Round(sg.dur / sum * float64(total)))
		if d < 1 {
			d = 1
		}
		end := cursor + d
		if i == len(segs)-1 {
			end = total // 最后一段收口，避免累计误差
		}
		if end <= cursor {
			end = cursor + 1
		}
		b.WriteString(prompt[last:sg.from])
		// 保持原来的写法，不把用户的格式换掉
		if sg.bracket {
			fmt.Fprintf(&b, "[Shot %s] %.2f–%.2fs", sg.no, float64(cursor), float64(end))
		} else {
			fmt.Fprintf(&b, "|镜头%s| %d-%d秒|", sg.no, cursor, end)
		}
		last = sg.to
		cursor = end
	}
	b.WriteString(prompt[last:])

	out := b.String()
	out = summaryOneShotRe.ReplaceAllString(out, fmt.Sprintf("单镜头 %d 秒", total))
	out = summaryMultiRe.ReplaceAllString(out, fmt.Sprintf("共 %d 秒", total))
	return out
}

// ensureSeatWords 站位缺方位时补一句兜底。
//
// 实测 LLM 有时只写「@学生 坐在对面」，不说哪一侧 —— 模型收到这种描述
// 就会自己摆，结果同一场戏里座位来回换。这里只在明显缺方位时才补，
// 已经写了「靠文件柜一侧」这类的不动。
func ensureSeatWords(s string) string {
	// 和 storyboard_stream 的 hasSeatWords 同口径：位置 + 家具都要有才算写清楚
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
	if hasPos && hasFurniture {
		return s
	}
	// 最后的兜底：重试也失败时才走到这。只能给笼统约束 ——
	// 具体哪一侧要靠 LLM 写，硬编反而会写错。
	return s + "（两人分坐桌子两侧、隔桌相对，全程不换座、不绕桌走动）"
}
