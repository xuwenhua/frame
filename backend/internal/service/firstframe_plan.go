package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/aiclient"
)

// FirstFrameGroup 一组共用同一张首帧图的段
type FirstFrameGroup struct {
	Scene       string `json:"scene"`
	Description string `json:"description"`
	Frames      []int  `json:"frames"`
}

const firstFramePlanSystemPrompt = `你是影视制片。下面是一集的分镜，每段标注了场景和人物位置。

任务：**每段分镜各出一张首帧图**，不做场景归并。

这张图会被视频模型当成第一帧：构图、人物位置、发型服装都从它来。
每段单独一张，好处是某段画错了只重出那一段，不会牵动其他段。

这张图同时承担两件事：定空间（房间布局、家具、光线）+ 定构图（谁在画面哪一侧）。
所以描述要写到「照着画就能画出这一段开头那一格」的程度。

**最重要的一条 —— 人物朝向**：
这张图会被视频模型当成空间基准。如果图里两个人都是「正对镜头」的合影姿态，
后面每一段生成的视频里两人都会各看各的、互不对视（实测踩过：第二段起老师一直不正对学生）。
所以描述里必须写清「谁面向谁」：两个人是隔着桌子**相对而坐、身体互相朝向**，
不是并排站着一起看镜头。

规则：
1. **每一段分镜单独成一组**（第 1 段一组、第 2 段一组…），不合并。
   这样每段都能出一张贴合自己构图的首帧，画错了可以单独重出，
   不会因为共用一张图把整场的构图一起锁死。
2. 每组的描述只写「空间 + 人物 + 光线 + 朝向」，写法参考：
   「全景：咨询间内一张浅木色长桌横在画面中部，老师坐在桌子一侧的布面椅上、身体朝向对面，
   学生坐在桌子另一侧、身体朝老师，两人隔桌相对、视线交汇；右侧有窗，自然光斜入」
3. 描述里**绝对不要写景别和运镜**（双人中景、正面机位、中近景、过肩、推镜 —— 
   这些词会让出图模型把画面理解成「正面合影」，人物就全朝镜头了）
4. 人物位置要写清三件事：谁在桌子哪一侧、谁站着谁坐着、**身体朝向谁**
5. 两人同处一个空间时，必须写明「两人相对」「互相面向」，不许只写左右
6. 组数等于这一集的分镜段数，每组的 frames 数组只放自己那一段的段号

只输出 JSON，不要解释、不要 markdown 代码块：
{"groups":[{"scene":"场景名","description":"全景空间描述","frames":[1,2,3]}]}`

// PlanAsset 首帧规划时一起发给模型的资产参考图
type PlanAsset struct {
	Name    string // 老师 / 教师办公室-白天
	Kind    string // character / scene
	DataURL string // data:image/jpeg;base64,...（InlineImage 产出）
}

// planVisionHint 附在系统提示词后面：这一轮模型能看到资产图
const planVisionHint = `

【这一轮会附上资产参考图】
先看图再写描述。图是事实，文字只是参考：
1. 场景图里的家具布局是基准 —— 桌子在房间中间还是靠墙、椅子有几把、窗户在哪一侧，
   描述都按图写。分镜文字里没提到的家具，不要凭空加。
2. 人物图决定长相和服装，描述里**只写位置和朝向**（谁在哪一侧、坐着还是站着、
   身体朝谁），不要再复述外貌 —— 复述错了反而让出图模型纠结。
3. 分镜文字和图对不上时（比如文字写「坐在桌前」但图里桌前没有椅子），
   按图的空间结构安排人物，把动作改成可行的版本。`

// PlanFirstFrames 让 LLM 把一集的分镜归并成若干首帧图分组。
//
// 为什么要归并：16 段每段单独合成一张首帧图，模型每次对这个房间的理解都不同，
// 空间越画越漂；而按「场景 + 机位」归并后，同机位的所有段共用一张图，
// 空间和人物位置就锁死了，图的数量也从「段数」降到「机位数」。
//
// assets 是这一集的角色图与场景图（已转成 data URL）。
// 传了图，描述才会贴合真实的空间布局；不传也能跑，但描述是「凭空想象」的。
func PlanFirstFrames(
	ctx context.Context,
	client *aiclient.LLMClient,
	frames []model.Storyboard,
	assets []PlanAsset,
) ([]FirstFrameGroup, error) {
	if client == nil {
		return nil, fmt.Errorf("没有可用的文本模型")
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("这一集还没有分镜")
	}

	var b strings.Builder
	b.WriteString("【这一集的分镜】\n")
	for i := range frames {
		f := &frames[i]
		f.FillRefs()

		scene := "未知场景"
		if len(f.Scenes) > 0 {
			scene = f.Scenes[0].Name
		}
		sit := FrameScene(f.VideoPrompt)
		if r := []rune(sit); len(r) > 80 {
			sit = string(r[:80])
		}

		// 景别不喂给规划模型：之前把「镜头：双人中景/老师」原样塞进去，
		// LLM 会照抄成「正面机位」，出图模型就画成两个人一起看镜头的合影，
		// 后面所有段的朝向全废（实测第二段起老师一直不正对学生）。
		people := make([]string, 0, len(f.Roles))
		for _, r := range f.Roles {
			people = append(people, r.Name)
		}
		fmt.Fprintf(&b, "第%d段 | 场景：%s | 位置：%s | 出场：%s\n",
			f.FrameIndex, scene, fallbackText(sit, "（未写）"), fallbackText(strings.Join(people, "、"), "（未写）"))
	}

	sysPrompt := firstFramePlanSystemPrompt
	userParts := make([]aiclient.ContentPart, 0, len(assets)*2+2)
	userParts = append(userParts, aiclient.TextPart(b.String()))

	if len(assets) > 0 {
		sysPrompt += planVisionHint
		userParts = append(userParts, aiclient.TextPart("\n【资产参考图】先看图，再按图写描述："))
		for _, a := range assets {
			if strings.TrimSpace(a.DataURL) == "" {
				continue
			}
			kind := "人物"
			if a.Kind == "scene" {
				kind = "场景"
			}
			// 图前先标明这张是谁，否则模型对着一串图分不清哪张是场景
			userParts = append(userParts, aiclient.TextPart(fmt.Sprintf("「%s」（%s）：", a.Name, kind)))
			userParts = append(userParts, aiclient.ImagePart(a.DataURL))
		}
	}

	res, err := client.Chat(ctx, []aiclient.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userParts},
	}, 0.3, 8000)
	if err != nil {
		return nil, fmt.Errorf("首帧规划失败: %w", err)
	}

	jsonStr := extractJSON(res.Content)
	if jsonStr == "" {
		return nil, fmt.Errorf("首帧规划未返回有效 JSON")
	}
	var parsed struct {
		Groups []FirstFrameGroup `json:"groups"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("首帧规划 JSON 解析失败: %w", err)
	}

	// 校验：段号必须真实存在，且每段只能属于一组（漏掉的段补到最接近的一组）
	valid := make(map[int]bool, len(frames))
	for _, f := range frames {
		valid[f.FrameIndex] = true
	}
	seen := make(map[int]bool, len(frames))
	clean := make([]FirstFrameGroup, 0, len(parsed.Groups))
	for _, g := range parsed.Groups {
		fs := make([]int, 0, len(g.Frames))
		for _, idx := range g.Frames {
			if valid[idx] && !seen[idx] {
				fs = append(fs, idx)
				seen[idx] = true
			}
		}
		if len(fs) == 0 {
			continue
		}
		g.Frames = fs
		if strings.TrimSpace(g.Description) == "" {
			g.Description = "沿用场景默认机位"
		}
		clean = append(clean, g)
	}

	// 漏掉的段（LLM 可能漏写）按场景补进同场景那组，找不到才单开一组
	for _, f := range frames {
		if seen[f.FrameIndex] {
			continue
		}
		scene := "未知场景"
		if len(f.Scenes) > 0 {
			scene = f.Scenes[0].Name
		}
		placed := false
		for i := range clean {
			if clean[i].Scene == scene {
				clean[i].Frames = append(clean[i].Frames, f.FrameIndex)
				placed = true
				break
			}
		}
		if !placed {
			clean = append(clean, FirstFrameGroup{
				Scene:       scene,
				Description: "沿用场景默认机位",
				Frames:      []int{f.FrameIndex},
			})
		}
	}

	if len(clean) == 0 {
		return nil, fmt.Errorf("首帧规划没有产出有效分组")
	}
	return clean, nil
}
