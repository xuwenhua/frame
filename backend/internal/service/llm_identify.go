package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"jikeai-clone/pkg/aiclient"
)

// LLMIdentifyService 用 LLM 从剧本抽取资产
type LLMIdentifyService struct {
	client *aiclient.LLMClient
	// 单次调用允许的最大输出 token（从模型配置读，避免写死导致 JSON 被截断）
	maxTokens int
	// 为 nil 时自动降级到规则引擎
	fallback *IdentifyService
}

func NewLLMIdentifyService(client *aiclient.LLMClient, maxTokens int) *LLMIdentifyService {
	if maxTokens < 2048 {
		maxTokens = 8192
	}
	if maxTokens > 16384 {
		maxTokens = 16384
	}
	return &LLMIdentifyService{
		client:    client,
		maxTokens: maxTokens,
		fallback:  NewIdentifyService(),
	}
}

const identifySystemPrompt = `你是资深影视美术指导，任务是产出一份可直接交给 AI 出图模型（如 Qwen Image / GPT Image）的「资产设定」。

核心要求：image_prompt 只描述「这个资产长什么样」——不看剧本、不看其它字段，只读这一段就能画出准确形象。

通用规则：
1. 只输出 JSON，不要任何解释文字、不要 markdown 代码块
2. 全部用中文，具体、可视化、可拍摄；禁止「时尚」「高级感」这类空词
3. 剧本没写的细节，按人物身份与场景档次合理推定（这是职责，不是编造）
4. 一条 image_prompt 用逗号或句号连成一段（不要换行），控制在 100~220 字
5. **必须遵守剧本自己写明的风格**：如果剧本写了「日式电视剧」「水彩插画」「深夜食堂质感」「赛博朋克」等，image_prompt 里必须原样体现这个风格，禁止改成写实人像
6. **必须遵守剧本的镜头限制**：如果剧本写了「无面部」「无脸」「避正脸」「背影」「手部特写」，角色 image_prompt 不得描述五官与表情，只描述服装、体态、手部
7. 分镜稿（镜1｜标题 这种）：场景按「地点+时段」拆，例如「傍晚院区外景」「夜晚三楼走廊」「夜晚诊室门口」，不要把镜头标题本身当角色名或场景名
8. 角色只登记真正出镜的人（医生、患者、家属…），「行人剪影」「背影」这类不单独建角色
9. 道具要列全：凡是镜头特写里出现、或被角色拿起/放下的物件都要单独列出（听诊器、病历本、水杯、台灯这类都算），不要漏

【严禁写入视图与画质要求】image_prompt 里绝不能出现下列内容，它们由系统按用户在界面上选的视图样式统一追加：
- 视图/分格：三视图、四视图、资产图、概念图、设计图、设定图、多角度、单人或双人、正面全身、侧面全身、背面全身、胸像、特写、并排、分格
- 背景与构图：白色背景、白底、空镜头、无人物、空无一人、画面横向分为、画面分为几格、平视广角、中央区域留白、地面清晰可见
- 画幅与画质：竖版、横版、9:16、16:9、4:3、3:4、1:1、8K、超清、高细节、纯净无字画面、无任何文字与水印
写这类内容会让出图自相矛盾（例如用户选的是四视图，提示词里却写着三视图），是严重错误。

【严禁写入姿态与动作】资产图是拿去对位所有镜头的中性参考，角色必须能看出完整身形。image_prompt 里绝不能写：坐着、坐姿、坐在、站着、站姿、走路、行走、奔跑、倚靠、半躺、蹲、弯腰、前倾、后仰、抱膝、翘腿，以及「坐着时…」这类姿态从句。只写身体结构与穿着，不写他此刻在做什么动作。

【角色 image_prompt】按这个顺序写全（写完即止，不要加任何收尾句）：
性别，年龄，气质总评。身形（高矮胖瘦、肩线、体态，只写静态身形，不写动作）。脸型五官（脸型、眉、眼、鼻、唇、下颌、肤色/妆感）。发型（长度、质感、分缝、发尾）。服装（上衣颜色+款式+材质，下装，鞋，配饰/饰品）。

【场景 image_prompt】按这个顺序写全（写完即止，不要加任何收尾句）：
场景类型，档次定位（如 L2普通平民档次）。墙面色调与地面材质。左侧陈设。右侧陈设。中央区域。靠墙固定陈设。空间感（整洁生活化／杂乱／空旷）。时段。光线（光源方向、色温、漫反射或硬光、灯具开或关）。

【物品 image_prompt】按这个顺序写全（写完即止，不要加任何收尾句）：
一件什么，材质（表面质感+内部材质），颜色，形体（形状+比例+结构），细节（开合方式、纹理、标签位置、表面是否有品牌文字）。

输出 JSON 结构：
{
  "characters": [
    {
      "name": "角色名（用剧本里的名字）",
      "identity_anchor": "身份锚定：性别、年龄、职业/关系，30字内",
      "appearance_cues": "脸型五官与发型，60~90字",
      "costume_prompt": "服装：上衣/下装/鞋/配饰，颜色款式材质，40~70字",
      "personality": "性格特征：从剧本行为推断，15字内",
      "voice_tone": "角色音色：音色定位 + 音域 + 音色质感 + 语速，如「松弛年轻男音：中音域，清朗温厚，语速稍快」，30字内",
      "image_prompt": "按上面的顺序写全的角色描述，不含任何视图/画幅要求"
    }
  ],
  "scenes": [
    {
      "name": "场景名（地点+时间，如「公司会议室-白天」）",
      "spatial_anchor": "空间布局：左侧/右侧/中央/靠墙陈设，60~90字",
      "lighting_prompt": "光线：光源、方向、色温、灯具状态，30~50字",
      "time_of_day": "白天/夜晚/黄昏",
      "image_prompt": "按上面的顺序写全的场景描述，不含任何视图/画幅要求"
    }
  ],
  "items": [
    {
      "name": "道具名",
      "category": "护肤品/数码/食品/服饰/家居",
      "prompt": "外观：材质、颜色、形体、细节，60~90字",
      "image_prompt": "按上面的顺序写全的物品描述，不含任何视图/画幅要求"
    }
  ]
}`

// Identify 调用 LLM 识别三类资产
func (s *LLMIdentifyService) Identify(ctx context.Context, script string) ([]RoleDraft, []SceneDraft, []ItemDraft, error) {
	if s.client == nil {
		return s.fallbackWith(script, nil)
	}

	sp := DetectStyle(script)

	// 把识别到的风格/画幅/限制显式告诉模型，避免它自由发挥成写实人像
	var hints []string
	if sp.Desc != "" {
		hints = append(hints, "本剧视觉风格："+sp.Desc+"（所有 image_prompt 必须体现这个风格）")
	}
	if sp.Vertical {
		hints = append(hints, "本剧为竖版 9:16")
	}
	if sp.NoFace {
		hints = append(hints, "本剧要求不露正脸：角色 image_prompt 不得描述五官与表情，改为侧身/背影/手部，并在结尾注明「均不露正脸」")
	}
	if sp.ShotList {
		hints = append(hints, "这是一份分镜稿（镜1｜标题），场景按「地点-时段」命名，不要把镜头标题当角色名")
	}
	userContent := "请分析以下剧本，抽取所有角色、场景、道具：\n\n" + script
	if len(hints) > 0 {
		userContent = "【已识别的制作约束】\n- " + strings.Join(hints, "\n- ") + "\n\n" + userContent
	}

	messages := []aiclient.ChatMessage{
		{Role: "system", Content: identifySystemPrompt},
		{Role: "user", Content: userContent},
	}

	res, err := s.client.Chat(ctx, messages, 0.3, s.maxTokens)
	if err != nil {
		return s.fallbackWith(script, fmt.Errorf("LLM 调用失败（%v），已降级规则引擎", err))
	}

	jsonStr := extractJSON(res.Content)
	if jsonStr == "" {
		return s.fallbackWith(script, fmt.Errorf("LLM 未返回 JSON，已降级规则引擎"))
	}

	var parsed identifyPayload
	parseErr := json.Unmarshal([]byte(jsonStr), &parsed)
	if parseErr != nil {
		// 1) 输出被截断（max_tokens 不够）时先尝试补齐括号再解析
		if repaired := repairJSON(jsonStr); repaired != "" {
			if e := json.Unmarshal([]byte(repaired), &parsed); e == nil {
				parseErr = nil
			}
		}
	}
	if parseErr != nil {
		// 2) 还是没有效 JSON，就压缩要求重试一次
		retryMessages := []aiclient.ChatMessage{
			{Role: "system", Content: identifySystemPrompt},
			{Role: "user", Content: userContent + "\n\n【重要】上一次输出不是完整合法的 JSON。这次请务必输出完整 JSON：角色≤5、场景≤8、道具≤8，每条 image_prompt ≤180 字，字段一个都不能少，结尾不要有多余文字。"},
		}
		if res2, err2 := s.client.Chat(ctx, retryMessages, 0.2, s.maxTokens); err2 == nil {
			if js2 := extractJSON(res2.Content); js2 != "" {
				if e := json.Unmarshal([]byte(js2), &parsed); e == nil {
					parseErr = nil
				}
			}
		}
	}
	if parseErr != nil {
		return s.fallbackWith(script, fmt.Errorf("LLM 输出无法解析为 JSON（%v），已降级规则引擎", parseErr))
	}

	roles := make([]RoleDraft, 0, len(parsed.Characters))
	for _, c := range parsed.Characters {
		if c.Name == "" {
			continue
		}
		d := RoleDraft{
			Name:           c.Name,
			IdentityAnchor: c.IdentityAnchor,
			CostumePrompt:  c.CostumePrompt,
			Personality:    c.Personality,
			AppearanceCues: c.AppearanceCues,
			ImagePrompt:    EnsureDetailedPrompt(c.ImagePrompt, "character", sp),
			VoiceTone:      strings.TrimSpace(c.VoiceTone),
		}
		// LLM 漏写音色时按性别年龄兜一个，用户可在编辑弹窗里改
		if d.VoiceTone == "" {
			d.VoiceTone = SuggestVoiceTone(c.IdentityAnchor, c.Personality)
		}
		if d.ImagePrompt == "" {
			d.ImagePrompt = BuildCharacterPrompt(d, sp)
		}
		roles = append(roles, d)
	}

	scenes := make([]SceneDraft, 0, len(parsed.Scenes))
	for _, sc := range parsed.Scenes {
		if sc.Name == "" {
			continue
		}
		sd := SceneDraft{
			Name:           sc.Name,
			SpatialAnchor:  sc.SpatialAnchor,
			LightingPrompt: sc.LightingPrompt,
			TimeOfDay:      sc.TimeOfDay,
			ImagePrompt:    EnsureDetailedPrompt(sc.ImagePrompt, "scene", sp),
		}
		if sd.ImagePrompt == "" {
			sd.ImagePrompt = BuildScenePrompt(sd, sp)
		}
		scenes = append(scenes, sd)
	}

	items := make([]ItemDraft, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		if it.Name == "" {
			continue
		}
		id := ItemDraft{
			Name:        it.Name,
			Category:    it.Category,
			Prompt:      it.Prompt,
			ImagePrompt: EnsureDetailedPrompt(it.ImagePrompt, "item", sp),
		}
		if id.ImagePrompt == "" {
			id.ImagePrompt = BuildItemPrompt(id, sp)
		}
		items = append(items, id)
	}

	if len(roles) == 0 && len(scenes) == 0 && len(items) == 0 {
		return s.fallbackWith(script, fmt.Errorf("LLM 未识别到任何资产，已降级规则引擎"))
	}

	return roles, scenes, items, nil
}

// fallbackWith 降级到规则引擎，同时带回错误信息
func (s *LLMIdentifyService) fallbackWith(script string, err error) ([]RoleDraft, []SceneDraft, []ItemDraft, error) {
	r, sc, it := s.fallback.Identify(script)
	return r, sc, it, err
}

// extractJSON 从可能带 markdown 包裹的文本里提取 JSON
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// 去掉 ```json ... ``` 包裹
	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		s = strings.TrimSpace(rest)
	}

	// 找第一个 { 和最后一个 }
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// identifyPayload LLM 返回结构
type identifyPayload struct {
	Characters []struct {
		Name           string `json:"name"`
		IdentityAnchor string `json:"identity_anchor"`
		CostumePrompt  string `json:"costume_prompt"`
		Personality    string `json:"personality"`
		AppearanceCues string `json:"appearance_cues"`
		ImagePrompt    string `json:"image_prompt"`
		VoiceTone      string `json:"voice_tone"`
	} `json:"characters"`
	Scenes []struct {
		Name           string `json:"name"`
		SpatialAnchor  string `json:"spatial_anchor"`
		LightingPrompt string `json:"lighting_prompt"`
		TimeOfDay      string `json:"time_of_day"`
		ImagePrompt    string `json:"image_prompt"`
	} `json:"scenes"`
	Items []struct {
		Name        string `json:"name"`
		Category    string `json:"category"`
		Prompt      string `json:"prompt"`
		ImagePrompt string `json:"image_prompt"`
	} `json:"items"`
}

// repairJSON 修补被截断的 JSON：闭合未结束的字符串与括号
func repairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '{' {
		return ""
	}

	inString := false
	escaped := false
	var stack []rune

	for _, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch r {
		case '{', '[':
			stack = append(stack, r)
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// 没截断说明原 JSON 另有问题，交给 retry 处理
	if len(stack) == 0 && !inString {
		return ""
	}

	out := s
	if inString {
		out += `"`
	}
	// 去掉尾部半截键值（形如 ,"key": 或 ,"key"）
	out = strings.TrimRight(out, " \t\r\n,")
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			out += "}"
		} else {
			out += "]"
		}
	}
	return out
}
