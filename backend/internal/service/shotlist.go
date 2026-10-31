package service

import (
	"regexp"
	"strings"
)

// 分镜稿（镜1｜标题 + 正文）的抽取逻辑
// 这类稿子没有「场景：」「人物：」声明，地点藏在镜头标题里，
// 角色往往只以职业出现（医生/患者），并且明确要求不露正脸。

// shotScenes 从镜头标题抽场景
func (s *IdentifyService) shotScenes(script string, sp StyleProfile) []SceneDraft {
	blocks := splitShotBlocks(script)
	seen := map[string]bool{}
	out := make([]SceneDraft, 0, len(blocks))

	for _, b := range blocks {
		hay := b.title + " " + b.body

		place := findPlace(hay)
		if place == "" {
			continue
		}
		// 时间优先看标题，标题没有再看正文；扫正文前先剔除「深夜食堂」这类片名
		tod := findTime(b.title)
		if tod == "" {
			tod = findTime(strings.ReplaceAll(b.body, "深夜食堂", ""))
		}
		key := place
		if seen[key] {
			continue
		}
		seen[key] = true

		name := place
		if tod != "" && !strings.Contains(place, tod) {
			name = tod + place
		}

		d := SceneDraft{
			Name:           name,
			SpatialAnchor:  shotSpatial(b.body),
			LightingPrompt: shotLighting(hay, tod),
			TimeOfDay:      tod,
		}
		// 分镜稿正文本身就是完整画面描述，直接用正文（构图收尾在提交生成时按场景样式追加）
		ip := d.SpatialAnchor
		if ip == "" {
			ip = name
		}
		d.ImagePrompt = trimPunct(ip)
		out = append(out, d)
	}

	return out
}

// shotRoles 从分镜稿里抽角色：只认明确的职业/身份词，避免把镜头标题当人名
func (s *IdentifyService) shotRoles(script string, sp StyleProfile) []RoleDraft {
	roleWords := []struct {
		word string
		desc string
		cost string
	}{
		{"医生", "医生，30岁出头，温和耐心，说话时习惯微微低头", "白色长袖白大褂，内搭浅蓝色衬衫与深色西裤"},
		{"护士", "护士，25岁左右，利落亲和，浅粉色护士服", "浅粉色护士服配白色护士鞋"},
		{"医护", "医护人员，30岁左右，安静专注", "白色长袖白大褂，内搭浅色上衣"},
		{"患者", "患者，40岁上下，衣着朴素，神情略显疲惫", "深灰色针织开衫与深色长裤，肩背帆布包"},
		{"病人", "病人，45岁上下，衣着朴素，神情略显疲惫", "深灰色外套与深色长裤"},
		{"家属", "患者家属，38岁上下，神情关切", "米色针织衫与深色长裤"},
		{"前台", "前台工作人员，26岁左右，制服整洁", "浅灰色制服与工牌"},
	}

	var out []RoleDraft
	seen := map[string]bool{}
	// 同义词：优先保留前面的具体词
	skipIfPresent := map[string]string{"医护": "医生", "病人": "患者"}
	for _, rw := range roleWords {
		if !strings.Contains(script, rw.word) || seen[rw.word] {
			continue
		}
		if keep, ok := skipIfPresent[rw.word]; ok && strings.Contains(script, keep) {
			continue
		}
		seen[rw.word] = true
		d := RoleDraft{
			Name:           rw.word,
			IdentityAnchor: rw.desc,
			CostumePrompt:  rw.cost,
			AppearanceCues: "",
			Personality:    "自然克制",
		}
		d.ImagePrompt = RuleCharacterPrompt(d, sp)
		out = append(out, d)
	}
	return out
}

// shotItems 分镜稿里常见道具：按名词表扫，命中即建
func (s *IdentifyService) shotItems(script string, sp StyleProfile) []ItemDraft {
	nouns := []struct{ word, cat, desc string }{
		{"听诊器", "家居", "一副医用听诊器，哑光金属耳件与黑色导管，胸件为平整的金属圆盘，表面清洁无刻度文字，随手放置时导管自然弯曲"},
		{"病历本", "家居", "一本医院病历本，米白色纸质封面与内页，封面无任何印刷字迹与标志，边缘略有翻阅痕迹，可平摊打开"},
		{"水杯", "家居", "一只白色陶瓷马克杯，杯壁细腻有细微釉面反光，杯口圆润，杯身无图案无文字，杯内盛着清水并升起一丝热气"},
		{"台灯", "家居", "一盏简约台式台灯，暖白色灯罩配细金属灯杆，灯罩边缘圆润，表面哑光无标识，开灯时洒出暖黄色柔光"},
		{"叫号屏", "数码", "一块壁挂式医院叫号显示屏，深色窄边框，屏幕表面微微反光，屏幕上的内容以柔和光晕呈现、无可辨认的文字"},
		{"电脑", "数码", "一台办公笔记本电脑，银灰色金属机身，屏幕开合自然，表面无品牌标识"},
		{"手机", "数码", "一部直板智能手机，深色玻璃背板与金属边框，屏幕熄灭呈深黑色，无品牌标识"},
		{"面霜", "护肤品", "一罐护肤面霜，细腻乳霜与磨砂玻璃罐材质，乳白色膏体配哑光盖，矮圆柱形广口罐，表面无品牌文字"},
		{"咖啡", "食品", "一杯热咖啡，白色陶瓷杯与浅色液体，杯口有一层细密泡沫，杯身无图案"},
		{"鲜花", "家居", "一束日常鲜花，浅色花瓣与绿色枝叶，用素色纸包裹，无品牌标签"},
		{"雨伞", "服饰", "一把长柄雨伞，深色伞面与木质伞柄，伞骨整齐，表面无图案"},
		{"帆布包", "服饰", "一只米色帆布托特包，粗织帆布材质，包口自然敞开，表面无印花与文字"},
	}

	// 先找出所有命中词，再剔除被更长命中词包含的（病历 ⊂ 病历本）
	hit := make([]string, 0, len(nouns))
	for _, n := range nouns {
		if strings.Contains(script, n.word) {
			hit = append(hit, n.word)
		}
	}
	keep := map[string]bool{}
	for _, w := range hit {
		shadowed := false
		for _, other := range hit {
			if other != w && strings.Contains(other, w) {
				shadowed = true
				break
			}
		}
		if !shadowed {
			keep[w] = true
		}
	}

	var out []ItemDraft
	for _, n := range nouns {
		if !keep[n.word] {
			continue
		}
		d := ItemDraft{Name: n.word, Category: n.cat, Prompt: n.desc}
		d.ImagePrompt = RuleItemPrompt(d, sp)
		out = append(out, d)
	}
	return out
}

// shotBlock 一个镜头的标题与正文
type shotBlock struct {
	title string
	body  string
}

// splitShotBlocks 按「镜N｜标题」切块，正文到下一个镜头标题为止
func splitShotBlocks(script string) []shotBlock {
	locs := shotTitleRe.FindAllStringSubmatchIndex(script, -1)
	if len(locs) == 0 {
		return nil
	}
	out := make([]shotBlock, 0, len(locs))

	for i, loc := range locs {
		// loc: [全匹配起点, 全匹配终点, 捕获组起点, 捕获组终点]
		title := strings.TrimSpace(script[loc[2]:loc[3]])
		bodyStart := loc[1]
		bodyEnd := len(script)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		body := strings.TrimSpace(script[bodyStart:bodyEnd])
		// 去掉「图生视频运动句：」这类制作备注
		if idx := strings.Index(body, "图生视频运动句"); idx > 0 {
			body = strings.TrimSpace(body[:idx])
		}
		out = append(out, shotBlock{title: title, body: body})
	}
	return out
}

// findPlace 从标题/正文里找地点
func findPlace(hay string) string {
	// 优先标题部分（调用方已把标题放前面）
	for _, w := range placeWords {
		if strings.Contains(hay, w) {
			return w
		}
	}
	return ""
}

// findTime 找时段
func findTime(hay string) string {
	for _, t := range timeWords {
		if strings.Contains(hay, t.word) {
			return t.val
		}
	}
	return ""
}

// shotSpatial 取镜头正文的第一句作为空间描述（分镜稿本身就写得很具体）
func shotSpatial(body string) string {
	s := strings.TrimSpace(body)
	if s == "" {
		return ""
	}
	// 按句号切第一句，最长 90 字
	if idx := strings.Index(s, "。"); idx > 0 {
		s = s[:idx]
	}
	s = strings.Trim(s, "，, ")
	return truncateRunes(s, 90)
}

// shotLighting 从正文里拎出光线描述，拎不到就按时段给默认值
func shotLighting(hay, tod string) string {
	for _, seg := range strings.FieldsFunc(hay, func(r rune) bool {
		return r == '，' || r == '。' || r == ',' || r == '；'
	}) {
		seg = strings.TrimSpace(seg)
		if !containsAny(seg, "灯光", "光斑", "暖光", "夜色", "台灯", "光线", "窗") {
			continue
		}
		if len([]rune(seg)) >= 6 {
			return truncateRunes(seg, 60)
		}
	}
	switch tod {
	case "夜晚", "深夜":
		return "夜晚，暖黄灯光与深蓝夜色对比，柔和漫反射，无明显硬阴影"
	case "傍晚":
		return "傍晚，天色由橙红过渡到深蓝，暖黄灯光初亮，柔和低对比"
	}
	return "自然光柔和均匀，漫反射为主，无明显硬阴影"
}

var _ = regexp.MustCompile // 保持 regexp 引用（分镜标题正则定义在 style.go）
