package service

import (
	"regexp"
	"strings"
)

// GenOptions 创作端资产页工具栏的生成配置
// 对标竞品（即刻AI）角色场景页顶部那排配置项：模型 / 分辨率 / 角色样式 / 场景样式
//
// 关键点：识别阶段存下来的 imagePrompt 已经带了一段「构图收尾约束」（三视图 / 空镜 / 白底），
// 用户在页面上换样式之后，必须把旧的那段拆掉再拼新的，否则会出现
// 「正面无头全身 + 同一角色的三视图」这种自相矛盾的提示词。
type GenOptions struct {
	// CharStyle 角色样式（四种，画幅由样式固定，与竞品一致）
	//   asset   人物资产图 16:9 —— 正面无头全身 / 背面全身 / 正面胸像
	//   quad    人物四视图 3:4 —— 正侧脸特写 / 正面服装 / 背面全身
	//   concept 人物概念图 16:9 —— 整体设计 + 服饰细节
	//   closeup 特写三视图 16:9 —— 三个角度面部特写
	CharStyle string `json:"charStyle"`
	// SceneStyle 场景样式：single 单一场景 / multi 多角度 / wide 广角全景
	SceneStyle string `json:"sceneStyle"`
	// Resolution 分辨率：1K / 2K
	Resolution string `json:"resolution"`
}

const (
	// 人物资产图：正面那一格故意不要头，方便后续把服装贴到别的镜头上
	CharStyleAsset   = "asset"
	CharStyleQuad    = "quad"
	CharStyleConcept = "concept"
	CharStyleCloseup = "closeup"

	SceneStyleSingle  = "single"
	SceneStyleMulti   = "multi"
	SceneStyleConcept = "concept"
	SceneStyleWide    = "wide"

	Res1K = "1K"
	Res2K = "2K"
)

// Normalize 兜底非法/空值，保证下游分支稳定
func (o GenOptions) Normalize() GenOptions {
	switch o.CharStyle {
	case CharStyleAsset, CharStyleQuad, CharStyleConcept, CharStyleCloseup:
	default:
		o.CharStyle = CharStyleAsset
	}
	switch o.SceneStyle {
	case SceneStyleSingle, SceneStyleMulti, SceneStyleConcept, SceneStyleWide:
	default:
		o.SceneStyle = SceneStyleSingle
	}
	switch o.Resolution {
	case Res1K, Res2K:
	default:
		o.Resolution = Res1K
	}
	return o
}

// 各类型提示词尾部可能残留的旧构图约束（老数据在识别阶段就拼进去了），换样式时整段剥掉
var (
	// 我们历史上拼过的几种收尾
	charTailRe = regexp.MustCompile(`[，,；;](?:同一角色的(?:三视图|设定图)|人物资产图|人物四视图|人物概念图|特写三视图|角色概念设计图|面部特写三视图)[：:].*$`)
	// 兜底：LLM 自己写的三视图 / 四视图收尾没被上面命中时
	charLooseRe = regexp.MustCompile(`[，,；;][^，,；;]{0,6}(?:三视图|四视图|资产图|概念图)[：:]?.*$`)
	sceneTailRe = regexp.MustCompile(`[，,；;](?:空镜头，无人物|同一场景的多角度视图|超广角全景构图|场景概念图|场景多角度视图).*$`)
	itemTailRe  = regexp.MustCompile(`[，,；;]白色背景，单件居中.*$`)
)

// 视图 / 构图 / 画幅 / 画质 类分段的特征词
// 只收不会出现在正常描述里的词，避免把「白色背景墙」这种正当内容误切
var framingMarkerRe = regexp.MustCompile(
	`三视图|四视图|资产图|概念图|设计图|设定图|空镜头|空镜|无人物|空无一人|` +
		`纯净无字|无任何文字|无水印|单件居中|平视广角|中央区域留白|等距并排|` +
		`画面横向分为|画面分为|多角度视图|竖版|横版|` +
		`9:16|9：16|16:9|16：9|4:3|4：3|3:4|3：4|1:1|1：1|` +
		`8K|超清|高细节|超高清`,
)

// splitSegments 按中英文标点切成小段，保留内容
func splitSegments(p string) []string {
	raw := strings.FieldsFunc(p, func(r rune) bool {
		switch r {
		case '，', ',', '；', ';', '。', '\n', '、':
			return true
		}
		return false
	})
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// segSeparators 分段用的分隔符（用于回退到最近的分隔位置，保留原文标点）
const segSeparators = "，,；;。\n、"

// StripFraming 剥掉提示词尾部残留的构图约束，还原成「通用提示」
// 用途：
//  1. 老数据在识别阶段就把「三视图 / 空镜 / 白色背景」写进了 image_prompt，读出来展示时先剥掉
//  2. 提交生成前也要剥一次，避免和用户选的视图样式打架
//
// 两段式：先按已知格式整段正则匹配（快、准），再按特征词兜底
// （LLM 会自己换标点、自己改措辞，正则不可能穷举，只能按「构图特征词」从该段起截断）。
// 截断时回退到最近的分隔符，保留原文标点，不重排用户的标点。
func StripFraming(prompt, kind string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return ""
	}

	switch kind {
	case "character":
		p = charTailRe.ReplaceAllString(p, "")
		p = charLooseRe.ReplaceAllString(p, "")
	case "scene":
		p = sceneTailRe.ReplaceAllString(p, "")
	case "item":
		p = itemTailRe.ReplaceAllString(p, "")
	}

	// 兜底：视图要求总是写在尾部，找到第一个构图特征词后，切到它前面最近的分隔符
	if loc := framingMarkerRe.FindStringIndex(p); loc != nil {
		head := p[:loc[0]]
		if idx := strings.LastIndexAny(head, segSeparators); idx > 0 {
			p = head[:idx]
		}
		// 特征词出现在第一段（前面没有分隔符）：说明整条都是构图描述，原样返回交给上游兜底
	}

	return trimPunct(p)
}

// ApplyGenOptions 把工具栏选择「翻译」成提示词
// 存库的提示词只描述资产本身（通用提示），三视图 / 四视图 / 广角这些**视图要求**在这里才拼上去，
// 与竞品一致：用户改提示词只改内容，不改构图；换样式不需要重新识别。
func ApplyGenOptions(prompt, kind string, sp StyleProfile, o GenOptions) string {
	p := trimPunct(strings.TrimSpace(prompt))
	if p == "" {
		return ""
	}
	o = o.Normalize()

	// 老数据的提示词尾部可能还带着当年写死的构图，先剥干净
	p = StripFraming(p, kind)
	// 角色描述里的瞬时姿态（坐着时…／倚靠…）会和「必须站立」的约束打架，先去掉
	if kind == "character" {
		p = stripPoseClauses(p)
	}
	p = trimPunct(p)
	if p == "" {
		return ""
	}

	framing := o.framing(kind, sp)
	if framing == "" {
		return p
	}

	// 2K 只在提示词层面加细节要求（RH 工作流没有单独的尺寸节点）
	if o.Resolution == Res2K && !strings.Contains(p, "高分辨率细节") {
		p += "，高分辨率细节，纹理与边缘清晰锐利"
	}

	// 无字声明 + 画风收尾；画风已在通用提示里体现时不重复追加
	suffix := "，纯净无字画面，无任何文字与水印"
	if kind == "item" {
		suffix = "，纯净无字画面，无任何文字与标识"
	}
	if sp.Desc != "" && !styleReflected(p, sp.Desc) {
		suffix += "，" + sp.TailStyle()
	} else if sp.Desc == "" {
		suffix += "，" + sp.TailStyle()
	}

	return p + "，" + framing + suffix
}

// framing 当前样式对应的「视图要求」（构图框架，不含无字声明与画风）
func (o GenOptions) framing(kind string, sp StyleProfile) string {
	switch kind {
	case "character":
		return o.charFraming(sp)
	case "scene":
		return o.sceneFraming()
	case "item":
		return "白色背景，单件居中"
	}
	return ""
}

// charFraming 角色图取景
// 四种样式的取景方式按竞品「人物资产图」那套来：
//   - 人物资产图：正面无头全身 + 背面全身 + 正面胸像（正面那一格不画头，方便换装复用）
//   - 人物四视图：正侧脸特写 + 正面服装 + 背面全身
//   - 人物概念图：整体设计 + 服饰与配饰细节放大
//   - 特写三视图：三个角度的面部特写
// 角色描述里偶发的「瞬时姿态」
//
// 资产参考图要的是中性站姿。描述词里一旦写了「坐姿微微前倾」「站着时…」，
// 模型会直接画成坐姿 —— 而资产图后续要拿去对位所有镜头，坐姿是没法用的。
//
// 实现按「分段」处理而不是单条正则：真实数据里的写法太散
// （「坐姿微微前倾、下巴内收」「站姿习惯性前倾」「坐着时上身微微前倾」），
// 统一成分段后，凡命中小段的整段丢弃，剩下的主体信息不受影响。
var poseMarkers = []string{
	"坐姿", "站姿", "坐着", "坐在", "站着", "站立时", "瘫坐",
	"走路", "行走", "奔跑", "倚靠", "半躺", "蹲", "弯腰",
	"前倾", "后仰", "抱膝", "翘腿", "盘腿", "侧躺",
}

// isPoseSegment 该段是否只讲姿态
func isPoseSegment(s string) bool {
	for _, m := range poseMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// 去掉描述词里的姿态片段，避免和「必须站立」的构图约束打架
//
// 注意：必须保留原始标点。早先的实现是「切段 → 用逗号重新拼接」，
// 结果把原文的句号也改成了逗号，下游「通用提示原样保留」的断言都挂了。
func stripPoseClauses(p string) string {
	runes := []rune(p)
	var b strings.Builder
	start := 0

	for i := 0; i <= len(runes); i++ {
		isSep := i == len(runes)
		if !isSep {
			switch runes[i] {
			case '，', ',', '；', ';', '、', '。', '\n':
				isSep = true
			}
		}
		if !isSep {
			continue
		}
		seg := string(runes[start:i])
		i2 := i
		start = i + 1

		if strings.TrimSpace(seg) == "" {
			continue
		}
		// 整段都是姿态才丢；带服装/五官等实质信息的段保留
		if isPoseSegment(seg) && !hasSubstantiveInfo(seg) {
			continue
		}
		b.WriteString(seg)
		if i2 < len(runes) {
			b.WriteRune(runes[i2])
		}
	}

	out := trimPunct(b.String())
	if out == "" {
		// 全被删空说明原文本来就是纯姿态描述，保留原样交上游处理
		return trimPunct(p)
	}
	return out
}

// hasSubstantiveInfo 判断这段是否还承载服装/颜色等必须保留的信息
func hasSubstantiveInfo(s string) bool {
	for _, k := range []string{"穿", "衬衫", "外套", "长裤", "短裤", "鞋", "裙", "帽", "包", "眼镜", "发", "眉", "眼", "鼻", "唇", "脸", "肤色"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func (o GenOptions) charFraming(sp StyleProfile) string {
	// 剧本声明不露正脸时不画脸，但取景方式仍跟着样式走
	if sp.NoFace {
		switch o.CharStyle {
		case CharStyleQuad:
			return "人物四视图：画面分为四格，上排为正面全身（头部裁切、不出现面部）与侧面全身（不露正脸），下排为背面全身与手部细节，同一角色，四格均为自然站姿、双脚着地、全身直立，服装与发型完全一致，白色背景"
		case CharStyleConcept:
			return "角色概念设计图：以该角色侧身或背影的站立全身像为主体，自然站姿、双脚着地、全身直立，四周辅以服装纹样、配饰与面料的细节放大示意，整体呈设定集排版感，不出现面部，白色背景"
		case CharStyleCloseup:
			return "局部特写三视图：同一角色的手部、服装细节与配饰各取一格并排呈现，比例一致，不出现面部，白色背景"
		default: // 人物资产图：正面那格本来就不画头，天然满足不露正脸
			return "人物资产图：画面横向分为三部分，左侧为正面全身但头部被裁切、完全不出现头与面部，中间为背面全身（可见后脑发型与背部服装），右侧为手部与服装细节特写（不出现面部），三者为同一角色，均为自然站姿、双脚着地、全身直立，白色背景"
		}
	}

	switch o.CharStyle {
	case CharStyleQuad:
		return "人物四视图：画面分为四格，上排左侧为正面脸部特写、右侧为侧面脸部特写，下排左侧为正面全身（完整呈现服装版型与配饰）、右侧为背面全身，四格为同一角色、比例一致、服装与发型完全一致，全身的两格均为自然站姿、双脚着地、全身直立，白色背景"
	case CharStyleConcept:
		return "角色概念设计图：以该角色正面站立全身像为主体居中，自然站姿、双脚着地、全身直立，四周辅以服装纹样、配饰、鞋履与面料的细节放大示意，整体呈角色设定集的排版感，白色背景"
	case CharStyleCloseup:
		return "面部特写三视图：同一角色的面部以三个角度并排呈现，从左到右依次为正脸、四分之三侧面与正侧面，肩部以上入画，重点表现五官、妆容与表情细节，白色背景"
	default: // CharStyleAsset 人物资产图
		return "人物资产图：画面横向分为三部分，左侧为正面全身但头部被裁切、完全不出现头与面部，中间为背面全身（可见后脑发型与背部服装），右侧为正面胸像（头肩至胸，清晰呈现面部五官与妆容），三者为同一角色、比例协调、服装与发型完全一致，全身部分必须为自然站姿、双脚着地、全身直立，不要坐姿、不要倚靠、不要弯腰，白色背景"
	}
}

// sceneFraming 场景图取景
func (o GenOptions) sceneFraming() string {
	switch o.SceneStyle {
	case SceneStyleMulti:
		return "同一场景的多角度视图：左半部分为平视主视角，右半部分为该场景的俯视图与两处局部细节特写，三者光线、时段与材质保持一致，空镜头，无人物，画面中空无一人"
	case SceneStyleWide:
		return "超广角全景构图，完整展示空间的纵深与四周陈设，地面清晰可见，空镜头，无人物，画面中空无一人"
	case SceneStyleConcept:
		return "场景概念图：以该场景的平视主视角为主体居中，四周辅以材质、陈设、灯具与局部光源的细节放大示意，整体呈场景设定集的排版感，空镜头，无人物，画面中空无一人"
	default: // SceneStyleSingle
		return "空镜头，无人物，画面中空无一人，地面清晰可见，平视广角构图，中央区域留白"
	}
}

// AspectFor 资产图画幅
// 角色四种样式的画幅与竞品一致，由样式直接固定：
// 人物资产图 16:9 / 人物四视图 3:4 / 人物概念图 16:9 / 特写三视图 16:9
// —— 资产图是参考图不是镜头，竖版剧本也不改它的画幅
func (o GenOptions) AspectFor(assetType string, sp StyleProfile) string {
	o = o.Normalize()
	switch assetType {
	case "character":
		switch o.CharStyle {
		case CharStyleQuad:
			return "3:4"
		default: // asset / concept / closeup
			return "16:9"
		}
	case "scene":
		if o.SceneStyle == SceneStyleMulti || o.SceneStyle == SceneStyleWide || o.SceneStyle == SceneStyleConcept {
			return "16:9"
		}
		if sp.Vertical {
			return "9:16"
		}
		return "4:3"
	case "item":
		return "1:1"
	}
	return ""
}

// GenOptionBriefs 给前端渲染工具栏选项用的元数据（避免前后端各写一份中文）
func GenOptionBriefs() map[string]interface{} {
	return map[string]interface{}{
		"charStyle": []map[string]string{
			{"value": CharStyleAsset, "label": "人物资产图", "ratio": "16:9", "hint": "正面无头全身、背面全身与正面胸像"},
			{"value": CharStyleQuad, "label": "人物四视图", "ratio": "3:4", "hint": "正侧脸特写、正面服装与背面全身设定"},
			{"value": CharStyleConcept, "label": "人物概念图", "ratio": "16:9", "hint": "适合展现角色整体设计与服饰细节"},
			{"value": CharStyleCloseup, "label": "特写三视图", "ratio": "16:9", "hint": "适合突出面部表情和细节特写"},
		},
		"sceneStyle": []map[string]string{
			{"value": SceneStyleSingle, "label": "单一场景视图", "hint": "一个平视主视角"},
			{"value": SceneStyleMulti, "label": "多角度视图", "hint": "主视角 + 俯视 + 局部细节"},
			{"value": SceneStyleConcept, "label": "场景概念图", "hint": "适合展现场景整体氛围与陈设细节"},
			{"value": SceneStyleWide, "label": "广角全景", "hint": "超广角展示空间纵深"},
		},
		"resolution": []map[string]string{
			{"value": Res1K, "label": "1K", "hint": "出图更快，够用"},
			{"value": Res2K, "label": "2K", "hint": "提示词追加高精度细节"},
		},
	}
}
