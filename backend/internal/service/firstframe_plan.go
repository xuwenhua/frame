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

任务：按「场景」归并。同一个场景的所有段共用一张**全景空间参考图**。

关键认知：这张图不是视频的第一帧，它只负责告诉模型「这个空间长什么样、人长什么样、光从哪来」。
镜头景别、机位方向、运镜（正打 / 反打 / 过肩 / 推镜）全部由视频模型按每段的描述自己完成。

**最重要的一条 —— 人物朝向**：
这张图会被视频模型当成空间基准。如果图里两个人都是「正对镜头」的合影姿态，
后面每一段生成的视频里两人都会各看各的、互不对视（实测踩过：第二段起老师一直不正对学生）。
所以描述里必须写清「谁面向谁」：两个人是隔着桌子**相对而坐、身体互相朝向**，
不是并排站着一起看镜头。

规则：
1. 一个场景一组，同一场景绝不拆成两组
2. 每组的描述只写「空间 + 人物 + 光线 + 朝向」，写法参考：
   「全景：咨询间内一张浅木色长桌横在画面中部，老师坐在桌子一侧的布面椅上、身体朝向对面，
   学生坐在桌子另一侧、身体朝老师，两人隔桌相对、视线交汇；右侧有窗，自然光斜入」
3. 描述里**绝对不要写景别和运镜**（双人中景、正面机位、中近景、过肩、推镜 —— 
   这些词会让出图模型把画面理解成「正面合影」，人物就全朝镜头了）
4. 人物位置要写清三件事：谁在桌子哪一侧、谁站着谁坐着、**身体朝向谁**
5. 两人同处一个空间时，必须写明「两人相对」「互相面向」，不许只写左右
6. 组数等于这一集出现的场景数

只输出 JSON，不要解释、不要 markdown 代码块：
{"groups":[{"scene":"场景名","description":"全景空间描述","frames":[1,2,3]}]}`

// PlanFirstFrames 让 LLM 把一集的分镜归并成若干首帧图分组。
//
// 为什么要归并：16 段每段单独合成一张首帧图，模型每次对这个房间的理解都不同，
// 空间越画越漂；而按「场景 + 机位」归并后，同机位的所有段共用一张图，
// 空间和人物位置就锁死了，图的数量也从「段数」降到「机位数」。
func PlanFirstFrames(ctx context.Context, client *aiclient.LLMClient, frames []model.Storyboard) ([]FirstFrameGroup, error) {
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

	res, err := client.Chat(ctx, []aiclient.ChatMessage{
		{Role: "system", Content: firstFramePlanSystemPrompt},
		{Role: "user", Content: b.String()},
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
