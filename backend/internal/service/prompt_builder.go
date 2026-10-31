package service

import (
	"fmt"
	"regexp"
	"strings"
)

// 参考竞品（即刻AI）的资产图提示词写法：
//   - 角色：性别年龄气质 → 身形肩线肤色 → 脸型眉眼鼻唇下颌 → 发型 → 上衣下装鞋配饰
//   - 场景：场景类型档次 → 墙地材质 → 左侧/右侧/中央/靠墙陈设 → 空间感与地面 → 构图留白 → 空镜无人物 → 时段光照与灯具状态
//   - 物品：一件什么 → 材质 → 颜色 → 形体结构 → 表面细节（无品牌文字）→ 白底
// 目标是「一个提示词就能独立出图」，不依赖其它上下文。


// BuildCharacterPrompt 拼角色出图提示词
// 注意：这里只产出「通用」的人物描述，不含任何视图/构图收尾。
// 三视图还是四视图取决于用户在工具栏选的样式，由 ApplyGenOptions 在提交生成时追加，
// 这样资产卡里展示、用户编辑的都是同一份通用提示词。
func BuildCharacterPrompt(d RoleDraft, sp StyleProfile) string {
	parts := make([]string, 0, 6)

	if s := strings.TrimSpace(d.IdentityAnchor); s != "" {
		parts = append(parts, trimPunct(s))
	}
	if s := strings.TrimSpace(d.AppearanceCues); s != "" && s != "保持全片一致" {
		parts = append(parts, trimPunct(s))
	}
	if s := strings.TrimSpace(d.CostumePrompt); s != "" {
		if !strings.HasPrefix(s, "穿") {
			s = "穿" + s
		}
		parts = append(parts, trimPunct(s))
	}

	body := strings.Join(parts, "。")
	if body == "" {
		body = d.Name
	}
	return body
}

// BuildScenePrompt 拼场景出图提示词（同样只给通用描述，构图收尾在提交时追加）
func BuildScenePrompt(d SceneDraft, sp StyleProfile) string {
	parts := make([]string, 0, 4)
	if s := strings.TrimSpace(d.SpatialAnchor); s != "" {
		parts = append(parts, trimPunct(s))
	} else {
		parts = append(parts, trimPunct(d.Name))
	}
	if s := strings.TrimSpace(d.TimeOfDay); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(d.LightingPrompt); s != "" {
		parts = append(parts, trimPunct(s))
	}
	return strings.Join(parts, "。")
}

// BuildItemPrompt 拼物品出图提示词（同上，不含白底等构图收尾）
func BuildItemPrompt(d ItemDraft, sp StyleProfile) string {
	body := strings.TrimSpace(d.Prompt)
	if body == "" {
		body = "一件" + d.Name
	}
	return trimPunct(body)
}

// EnsureDetailedPrompt 已有 image_prompt 时补上「剧本画风」这一层（LLM 常写成 8K 超清了事）
// 只补画风，不补构图：构图属于用户可切换的视图样式，交给生成阶段
func EnsureDetailedPrompt(prompt, kind string, sp StyleProfile) string {
	p := trimPunct(strings.TrimSpace(prompt))
	if p == "" {
		return ""
	}

	// 角色的瞬时姿态（坐着时…／倚靠…）不该进资产描述：
	// 资产图要拿去对位所有镜头，坐姿是没法用的。入库即清洗，编辑弹窗里也是干净文本。
	if kind == "character" {
		p = stripPoseClauses(p)
	}

	// LLM 常常漏掉剧本指定的画风，这里兜一层
	if sp.Desc != "" && !styleReflected(p, sp.Desc) {
		p = strings.TrimRight(p, "。，, ") + "，" + sp.TailStyle()
	}
	return p
}

// styleReflected 判断提示词里是否已经体现了剧本风格
func styleReflected(prompt, desc string) bool {
	for _, seg := range strings.FieldsFunc(desc, func(r rune) bool { return r == '，' || r == ',' }) {
		seg = strings.TrimSpace(seg)
		if len([]rune(seg)) < 4 {
			continue
		}
		// 取每个风格片段的前 6 个字作为特征词
		key := string([]rune(seg)[:6])
		if strings.Contains(prompt, key) {
			return true
		}
	}
	return false
}

// ==================== 规则引擎兜底：按角色/场景原型补足细节 ====================

// RuleCharacterPrompt 在没有 LLM 时，根据角色名与剧本线索合成一段可出图的描述
func RuleCharacterPrompt(d RoleDraft, sp StyleProfile) string {
	name := d.Name
	text := d.IdentityAnchor + " " + d.AppearanceCues + " " + d.CostumePrompt

	gender := detectGender(name, text)

	age := "26岁"
	switch {
	case regexp.MustCompile(`(总|经理|老板|董事|总监)`).MatchString(name):
		age = "38岁"
	case regexp.MustCompile(`(奶奶|爷爷|老人|婆婆|外婆)`).MatchString(name):
		age = "65岁"
	case regexp.MustCompile(`(学生|实习|新人)`).MatchString(name):
		age = "23岁"
	}

	temper := "气质干净利落"
	switch {
	case regexp.MustCompile(`(总|经理|老板|董事|总监)`).MatchString(name):
		temper = "气场沉稳，神情克制"
	case regexp.MustCompile(`(奶奶|爷爷|婆婆|外婆)`).MatchString(name):
		temper = "面容慈和，带岁月痕迹"
	}

	// 五官与发型按性别给一套自然、可复现的描述
	var face, hair string
	if gender == "女性" {
		face = "柔和的鹅蛋脸，下颌线流畅，弧形细眉配清亮杏眼，鼻梁纤巧顺直，唇峰清晰，淡雅裸妆，皮肤质感自然通透"
		hair = "深棕色锁骨发，三七侧分，发根轻盈蓬松，发尾自然外弯"
	} else {
		face = "轮廓清晰的长方脸，平直浓眉配带弧度的修长眼型，鼻梁挺直，唇形清晰，下颌转折利落，肤色自然偏暖"
		hair = "黑色短发，顶部保留蓬松碎层，两侧修剪清爽"
	}

	costume := strings.TrimSpace(d.CostumePrompt)
	if costume == "" {
		switch {
		case regexp.MustCompile(`(总|经理|老板|董事|总监)`).MatchString(name):
			costume = "深藏青色羊毛混纺西装外套，平驳领、单排两粒扣，内搭白色棉质衬衫与深灰斜纹领带，下穿同色西裤，脚穿黑色系带牛津皮鞋，佩戴银色表带腕表"
		case gender == "女性":
			costume = "燕麦色细针织开衫，内搭浅杏色圆领棉质上衣，下穿雾蓝色高腰直筒休闲裤和米白色软底鞋"
		default:
			costume = "鼠尾草绿色宽松圆领棉质卫衣，下穿深灰色直筒长裤和浅灰色软底鞋"
		}
	}
	if !strings.HasPrefix(costume, "穿") {
		costume = "穿" + costume
	}

	// 身份锚定若是剧情句（含动作动词），改用「性别+年龄+气质」，避免把剧情写进出图提示词
	identity := strings.TrimSpace(d.IdentityAnchor)
	if identity == "" || identity == name || isNarrativeLine(identity) {
		identity = fmt.Sprintf("%s，%s，%s", gender, age, temper)
	}

	// 不露正脸的设定图：不写五官与发型，只描述体态、手部与服装
	if sp.NoFace {
		body := strings.Join([]string{
			trimPunct(identity),
			"体态自然，双手干净修长、指甲修剪整齐，画面中看不到面部",
			trimPunct(costume),
		}, "。")
		return body
	}

	body := strings.Join([]string{
		trimPunct(identity),
		"身形匀称，肩颈舒展，肤色自然",
		face,
		hair,
		trimPunct(costume),
	}, "。")

	return body
}

// detectGender 从角色名与描写里判断性别
func detectGender(name, text string) string {
	if regexp.MustCompile(`(女主|女配|妈妈|母亲|姐|妹|奶|婆|姨|姑|嫂|夫人|小姐|她)`).MatchString(name) {
		return "女性"
	}
	if regexp.MustCompile(`(男主|男配|爸爸|父亲|哥|弟|爷|叔|伯|先生|总裁|总$|经理|老板|董事)`).MatchString(name) {
		return "男性"
	}
	// 名字看不出来后看描写里的代词
	if strings.Contains(text, "他") && !strings.Contains(text, "她") {
		return "男性"
	}
	return "女性"
}

// isNarrativeLine 判断一句是不是剧情叙述（含动作/对话动词）
func isNarrativeLine(s string) bool {
	return regexp.MustCompile(`(看向|看了|说|问|走|推|拿|放|站|坐|抬|低头|转身|笑|喊|回答|盯着|掏出|握着)`).MatchString(s)
}

// RuleScenePrompt 无 LLM 时按场景名推断一个合理的空间描述
func RuleScenePrompt(d SceneDraft, sp StyleProfile) string {
	name := d.Name
	spatial := strings.TrimSpace(d.SpatialAnchor)

	kind := "现代普通室内空间"
	layout := "画面左侧为采光窗与浅色窗帘，右侧摆放主体家具，中央为通行区域"
	furnish := "靠墙设有低矮收纳柜与基础家居陈设"
	tier := "L2普通平民档次"

	switch {
	case regexp.MustCompile(`(会议室|办公室|公司|写字楼)`).MatchString(name):
		kind = "现代普通办公会议室"
		tier = "L2标准商务档次"
		layout = "画面中央为长条会议桌，桌周摆放黑色皮质会议椅，左侧为落地玻璃隔断，右侧墙面设投影幕布"
		furnish = "靠墙设有边柜与饮水设备，桌面摆放少量文件与笔记本"
	case regexp.MustCompile(`(客厅|家|卧室|出租屋|公寓)`).MatchString(name):
		kind = "现代普通家居空间"
		layout = "左侧双扇玻璃窗配浅色布艺窗帘，右侧摆放浅灰色布艺沙发，中央为简约木质茶几"
		furnish = "靠墙设有低矮电视柜与基础家居陈设"
	case regexp.MustCompile(`(餐厅|咖啡|酒吧|奶茶)`).MatchString(name):
		kind = "现代休闲餐饮空间"
		tier = "L2普通市井档次"
		layout = "左侧为吧台与操作区，右侧成排卡座，中央通道宽敞"
		furnish = "墙面设简约置物架与暖光灯带"
	case regexp.MustCompile(`(街|路|广场|公园|小区)`).MatchString(name):
		kind = "城市日常外景"
		tier = "L2普通生活区"
		layout = "前景为人行道与行道树，中景为临街店铺立面，远景为成排住宅楼"
		furnish = "路边可见路牌、路灯与共享单车"
	case regexp.MustCompile(`(医院|病房|诊室)`).MatchString(name):
		kind = "现代医院诊疗空间"
		layout = "左侧为诊疗床与器械车，右侧为医生工位与电脑，中央为通行区域"
		furnish = "墙面为浅色抗菌板，靠墙设储物柜"
	}

	body := kind + "，" + tier
	// spatialHint 多是「长条会议桌，投影幕布」这类短词，信息量不如下面的 layout，短的就跳过避免重复
	if len([]rune(spatial)) > 24 {
		body += "，" + trimPunct(spatial)
	}
	// 注意：构图/留白/地面细节由 sceneSuffix 统一收尾，这里不重复
	body += "，" + layout + "，" + furnish + "，空间整洁生活化，有日常使用痕迹"

	if tod := strings.TrimSpace(d.TimeOfDay); tod != "" {
		body += "。" + tod
	}
	lighting := strings.TrimSpace(d.LightingPrompt)
	if lighting == "" {
		if strings.Contains(name, "夜") {
			lighting = "室内主灯开启，暖色筒灯形成柔和光斑，窗外为夜景霓虹"
		} else {
			lighting = "自然光经窗帘柔和洒入，形成温暖均匀的漫反射与自然柔影，室内灯具关闭"
		}
	}
	body += "。" + trimPunct(lighting)

	return body
}

// RuleItemPrompt 无 LLM 时按品类给物品一段可出图的描述
func RuleItemPrompt(d ItemDraft, sp StyleProfile) string {
	name := d.Name
	cat := d.Category
	if cat == "" {
		cat = guessCategory(name)
	}
	// 已有的 prompt 足够长就直接用，太短（如「面霜，外观清晰」）则重写
	if p := strings.TrimSpace(d.Prompt); len([]rune(p)) >= 40 {
		return BuildItemPrompt(d, sp)
	}

	classifier := "件"
	switch cat {
	case "护肤品":
		classifier = "罐"
	case "数码":
		classifier = "台"
	case "食品":
		classifier = "盒"
	case "家居":
		classifier = "个"
	}

	material := "细腻磨砂材质"
	color := "柔和暖白色"
	shape := "简洁圆柱形，比例修长"
	switch cat {
	case "护肤品":
		material = "细腻乳霜与磨砂玻璃罐材质"
		shape = "矮圆柱形广口罐，搭配完整旋拧式圆盖"
	case "数码":
		material = "哑光金属与玻璃材质"
		shape = "轻薄长方形机身，四角圆润"
	case "食品":
		material = "食品级塑料与哑光纸材质"
		shape = "规整方形包装，边缘挺括"
	case "服饰":
		material = "柔软棉质面料"
		shape = "自然垂坠，版型宽松"
	}

	body := fmt.Sprintf("一%s%s，%s，整体呈%s，%s，包装干净精致、表面无品牌文字与图案", classifier, name, material, color, shape)
	return body
}

// trimPunct 去掉首尾多余标点，避免拼接出现「。。」或「，，」
func trimPunct(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "，。、；：,;: ")
	return s
}
