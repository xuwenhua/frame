package service

import (
	"regexp"
	"strings"
)

// IdentifyService 从剧本里抽取角色 / 场景 / 道具
// 规则抽取，不依赖外部 LLM；接入 LLM 时替换同名方法即可。
type IdentifyService struct{}

func NewIdentifyService() *IdentifyService {
	return &IdentifyService{}
}

// RoleDraft 角色草稿
type RoleDraft struct {
	Name           string
	IdentityAnchor string
	CostumePrompt  string
	Personality    string
	AppearanceCues string
	ImagePrompt    string // 可直接出图的长提示词
	VoiceTone      string // 角色音色描述（参与后续配音）
}

// SceneDraft 场景草稿
type SceneDraft struct {
	Name           string
	SpatialAnchor  string
	LightingPrompt string
	TimeOfDay      string
	ImagePrompt    string
}

// ItemDraft 道具草稿
type ItemDraft struct {
	Name        string
	Category    string
	Prompt      string
	ImagePrompt string
}

// Identify 一并抽取三类资产
// 会先扫描剧本的视觉风格（画风 / 画幅 / 是否允许正脸 / 是否分镜稿），
// 再按对应策略抽取，避免把分镜稿的「镜6｜定版」当成角色名。
func (s *IdentifyService) Identify(script string) ([]RoleDraft, []SceneDraft, []ItemDraft) {
	sp := DetectStyle(script)

	roles := s.Roles(script, sp)
	scenes := s.Scenes(script, sp)
	items := s.Items(script, sp)

	if sp.ShotList {
		// 分镜稿：场景以镜头标题里的地点为准，角色只认明确的职业/身份词
		scenes = s.shotScenes(script, sp)
		roles = s.shotRoles(script, sp)
		if its := s.shotItems(script, sp); len(its) > 0 {
			items = its
		}
	}

	return roles, scenes, items
}

// Roles 抽取角色
func (s *IdentifyService) Roles(script string, sp StyleProfile) []RoleDraft {
	var out []RoleDraft
	seen := map[string]bool{}

	// 1. 「人物：A、B」这类声明的角色，优先级最高
	decl := regexp.MustCompile(`(?m)^\s*(?:人物|出场人物|角色)\s*[:：]\s*(.+)$`)
	for _, m := range decl.FindAllStringSubmatch(script, -1) {
		for _, name := range splitNames(m[1]) {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, newRoleDraft(name, script))
		}
	}

	// 2. 从对话行「林小满：」补充
	dialog := regexp.MustCompile(`(?m)^\s*([^\s:：，。、｜|]{2,6})\s*[:：]`)
	for _, m := range dialog.FindAllStringSubmatch(script, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] || isMetaWord(name) || strings.Contains(name, "镜") || strings.Contains(name, "图生") {
			continue
		}
		seen[name] = true
		out = append(out, newRoleDraft(name, script))
	}

	for i := range out {
		out[i].ImagePrompt = RuleCharacterPrompt(out[i], sp)
	}
	return out
}

func newRoleDraft(name, script string) RoleDraft {
	d := RoleDraft{Name: name}

	// 找剧本里关于这个人的外貌/身份线索
	cues := findAppearanceCues(script, name)
	if cues != "" {
		d.AppearanceCues = cues
	} else {
		d.AppearanceCues = "保持全片一致"
	}

	// 身份锚定：优先用描写句，其次用角色名占位
	sentence := findSentenceAbout(script, name)
	if sentence != "" {
		d.IdentityAnchor = shorten(sentence, 30)
	} else {
		d.IdentityAnchor = name
	}

	// 性格：从常见形容词里找
	d.Personality = guessPersonality(script, name)
	d.CostumePrompt = guessCostume(script)

	return d
}

// findAppearanceCues 从剧本抽外貌线索（发型、五官、体型、衣着）
func findAppearanceCues(script, name string) string {
	sentences := regexp.MustCompile(`[。！？!?\n]`).Split(script, -1)
	cueWord := regexp.MustCompile(`(长发|短发|卷发|马尾|眼镜|圆脸|瓜子脸|高鼻梁|身材|穿着|白色|黑色|深蓝|西装|衬衫|裙|身高|瘦|胖|年轻|岁)`)
	var cues []string
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if !strings.Contains(s, name) {
			continue
		}
		for _, w := range cueWord.FindAllString(s, -1) {
			dup := false
			for _, c := range cues {
				if c == w {
					dup = true
					break
				}
			}
			if !dup {
				cues = append(cues, w)
			}
		}
	}
	if len(cues) > 4 {
		cues = cues[:4]
	}
	return strings.Join(cues, "，")
}

// guessPersonality 按「该角色出场的那几句」推断性格，避免所有角色串味
func guessPersonality(script, name string) string {
	// 先切句，只留提到这个角色的句子
	sentences := regexp.MustCompile(`[。！？!?\n]`).Split(script, -1)
	var mine []string
	for _, s := range sentences {
		if strings.Contains(s, name) {
			mine = append(mine, s)
		}
	}
	scope := strings.Join(mine, "。")
	if scope == "" {
		scope = script
	}

	traits := []struct {
		word string
		desc string
	}{
		{"冷峻", "冷峻克制"}, {"冷漠", "冷漠疏离"}, {"温柔", "温和"},
		{"微笑", "温和"}, {"倔", "倔强有韧性"}, {"没有低头", "倔强不服软"},
		{"深吸一口气", "紧张但镇定"}, {"自信", "自信笃定"}, {"紧张", "紧张克制"},
		{"愤怒", "压抑怒火"}, {"松动", "防线上松动"}, {"沉默", "内敛寡言"},
		{"看向她", "审视感"},
	}
	var found []string
	for _, t := range traits {
		if !strings.Contains(scope, t.word) {
			continue
		}
		dup := false
		for _, f := range found {
			if f == t.desc {
				dup = true
				break
			}
		}
		if !dup {
			found = append(found, t.desc)
		}
	}
	if len(found) == 0 {
		return "自然克制"
	}
	if len(found) > 2 {
		found = found[:2]
	}
	return strings.Join(found, "，")
}

// guessCostume 从剧本里抽服装描述
func guessCostume(script string) string {
	costumeWord := regexp.MustCompile(`(白色衬衫|黑西装|白衬衫|职业装|连衣裙|西装|衬衫|西装裤|高跟鞋|风衣|毛衣|卫衣)`)
	seen := map[string]bool{}
	var out []string
	for _, w := range costumeWord.FindAllString(script, -1) {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return ""
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return strings.Join(out, "，")
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Scenes 抽取场景
func (s *IdentifyService) Scenes(script string, sp StyleProfile) []SceneDraft {
	var out []SceneDraft
	seen := map[string]bool{}

	decl := regexp.MustCompile(`(?m)^\s*(?:场景|地点|环境)\s*[:：]\s*(.+)$`)
	for _, m := range decl.FindAllStringSubmatch(script, -1) {
		raw := strings.TrimSpace(m[1])
		if raw == "" {
			continue
		}
		// 拆出地名与时间段
		name, tod := splitSceneName(raw)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, SceneDraft{
			Name:           name,
			SpatialAnchor:  spatialHint(name),
			LightingPrompt: lightingHint(tod),
			TimeOfDay:      tod,
		})
	}

	// 地名兜底：从正文里识别常见场所
	if len(out) == 0 {
		places := []string{"会议室", "办公室", "客厅", "卧室", "厨房", "餐厅", "咖啡厅", "餐厅", "街道", "电梯", "车内", "医院", "教室", "商场"}
		for _, p := range places {
			if strings.Contains(script, p) && !seen[p] {
				seen[p] = true
				out = append(out, SceneDraft{
					Name:           p,
					SpatialAnchor:  spatialHint(p),
					LightingPrompt: lightingHint(""),
					TimeOfDay:      "白天",
				})
			}
		}
	}
	for i := range out {
		out[i].ImagePrompt = RuleScenePrompt(out[i], sp)
	}
	return out
}

// Items 抽取道具
func (s *IdentifyService) Items(script string, sp StyleProfile) []ItemDraft {
	var out []ItemDraft
	seen := map[string]bool{}

	// 1. 「道具：面霜」显式声明
	decl := regexp.MustCompile(`(?m)^\s*(?:道具|物品|关键道具)\s*[:：]\s*(.+)$`)
	for _, m := range decl.FindAllStringSubmatch(script, -1) {
		for _, name := range splitNames(m[1]) {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, ItemDraft{Name: name, Category: guessCategory(name), Prompt: itemPrompt(name)})
		}
	}

	// 2. 从「拿出X」「握着X」这类动作里抓（排除方位词结尾）
	verb := regexp.MustCompile(`(?:拿出|掏出|取出|握着|捧着|举起|递给|拧开)([\p{Han}A-Za-z0-9]{2,6})`)
	for _, m := range verb.FindAllStringSubmatch(script, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] || isStopWord(name) || endsWithPosition(name) {
			continue
		}
		seen[name] = true
		out = append(out, ItemDraft{Name: name, Category: guessCategory(name), Prompt: itemPrompt(name)})
	}
	for i := range out {
		out[i].ImagePrompt = RuleItemPrompt(out[i], sp)
	}
	return out
}

// endsWithPosition 过滤「桌上」「面前」这类方位词误匹配
func endsWithPosition(s string) bool {
	suffixes := []string{"桌上", "面前", "手里", "身边", "身上", "地上", "门口", "里头", "上面", "下面", "里面", "外面", "旁边"}
	for _, sfx := range suffixes {
		if strings.HasSuffix(s, sfx) || s == sfx {
			return true
		}
	}
	return false
}

// ---------- 辅助 ----------

func splitNames(s string) []string {
	s = strings.NewReplacer("、", ",", "，", ",", "和", ",", "与", ",", " ", ",").Replace(s)
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len([]rune(p)) >= 1 && len([]rune(p)) <= 8 && !isMetaWord(p) {
			out = append(out, p)
		}
	}
	return out
}

func isMetaWord(s string) bool {
	meta := []string{"场景", "人物", "时间", "地点", "道具", "物品", "环境", "角色", "旁白", "字幕"}
	for _, m := range meta {
		if s == m {
			return true
		}
	}
	return false
}

func isStopWord(s string) bool {
	stops := []string{"文件", "手机", "盖子", "门", "眼睛", "头", "手", "口气", "话", "嘴"}
	for _, st := range stops {
		if s == st {
			return true
		}
	}
	return false
}

// splitSceneName 把「公司会议室 - 白天」拆成 ("公司会议室", "白天")
func splitSceneName(raw string) (string, string) {
	tod := ""
	tods := []string{"清晨", "白天", "上午", "中午", "下午", "黄昏", "傍晚", "夜晚", "深夜", "凌晨"}
	for _, t := range tods {
		if strings.Contains(raw, t) {
			tod = t
			break
		}
	}
	name := raw
	for _, sep := range []string{" - ", "-", "—", "－", "_"} {
		if idx := strings.Index(name, sep); idx > 0 {
			name = strings.TrimSpace(name[:idx])
			break
		}
	}
	if tod != "" && strings.HasSuffix(name, tod) {
		name = strings.TrimSpace(strings.TrimSuffix(name, tod))
	}
	if tod == "" {
		tod = "白天"
	}
	return name, tod
}

// findSentenceAbout 找最能代表该角色的一句，要求「角色名出现在句首或前半句」
func findSentenceAbout(script, name string) string {
	sentences := regexp.MustCompile(`[。！？!?\n]`).Split(script, -1)
	best := ""
	bestScore := -1

	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "场景") || strings.HasPrefix(s, "人物") ||
			strings.HasPrefix(s, "道具") || strings.HasPrefix(s, "地点") {
			continue
		}
		idx := strings.Index(s, name)
		if idx < 0 {
			continue
		}

		// 位置越靠前越可能是主语；长度适中加分
		score := 0
		runes := []rune(s)
		pos := len([]rune(s[:idx]))
		if pos == 0 {
			score += 10
		} else if pos <= 3 {
			score += 6
		} else {
			score += 1
		}
		if len(runes) >= 8 && len(runes) <= 40 {
			score += 4
		}
		if score > bestScore {
			bestScore = score
			best = s
		}
	}
	_ = name
	return best
}

func spatialHint(name string) string {
	hints := map[string]string{
		"会议室": "长条会议桌，投影幕布，落地玻璃墙",
		"办公室": "办公桌，文件柜，百叶窗",
		"客厅":  "沙发，茶几，电视机",
		"卧室":  "双人床，床头柜，衣柜",
		"厨房":  "橱柜，灶台，冰箱",
		"咖啡厅": "木质吧台，落地窗，暖光吊灯",
		"电梯":  "金属轿厢，镜面墙，楼层按钮",
		"街道":  "人行道，路边店铺，来往车辆",
		"医院":  "白色走廊，护士站，病房门",
		"车内":  "驾驶座，方向盘，挡风玻璃",
	}
	for k, v := range hints {
		if strings.Contains(name, k) {
			return v
		}
	}
	return name + "内部空间"
}

func lightingHint(tod string) string {
	switch tod {
	case "清晨":
		return "浅金色晨光，柔和平射"
	case "夜晚", "深夜":
		return "室内暖光，局部打光，暗部保留细节"
	case "黄昏", "傍晚":
		return "橙金色夕照，长投影"
	case "下午":
		return "侧逆光，窗光斜切"
	default:
		return "自然光从窗户射入，柔和均匀"
	}
}

func guessCategory(name string) string {
	cats := map[string]string{
		"面霜": "护肤品", "口红": "化妆品", "香水": "香水",
		"手机": "数码", "电脑": "数码", "文件": "办公",
		"咖啡": "饮品", "水杯": "日用品",
	}
	for k, v := range cats {
		if strings.Contains(name, k) {
			return v
		}
	}
	return "通用道具"
}

func itemPrompt(name string) string {
	return name + "，外观清晰，质感真实，细节可辨"
}
