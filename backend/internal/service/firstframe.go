package service

import (
	"fmt"
	"strings"

	"jikeai-clone/internal/model"
)

// BuildFirstFramePrompt 拼「首帧图」的图生图提示词。
//
// 为什么要有首帧图：视频生成时首帧位放的是角色参考图（白底人像），
// 模型得自己从文字重建整个房间 —— 每次重建的空间都不一样，座位就漂了。
// 先用「角色图 + 场景图 + 构图描述」合成一张真实画面，视频只负责让它动，
// 空间关系在图上已经定死，不会漂。
//
// refs 顺序必须与调用方传的参考图顺序一致（角色在前、场景在后），
// Qwen 图编辑用 {{ref N}} 指代第 N 张。
func BuildFirstFramePrompt(vp string, roles []model.Character, scenes []model.Scene, items []model.Item) string {
	var b strings.Builder

	b.WriteString("把参考图合成一张真实照片级的电影画面，这张图会作为视频的第一帧。\n\n")

	b.WriteString("参考图对应关系：\n")
	for i, r := range roles {
		fmt.Fprintf(&b, "{{ref %d}} 是「%s」，长相、发型、服装必须与这张图完全一致\n", i+1, r.Name)
	}
	for i, sc := range scenes {
		fmt.Fprintf(&b, "{{ref %d}} 是「%s」的环境（空间布局、家具、光线以这张图为准）\n",
			len(roles)+i+1, sc.Name)
	}
	itemBase := len(roles) + len(scenes)
	for i, it := range items {
		fmt.Fprintf(&b, "{{ref %d}} 是道具「%s」的外观参考（颜色、材质、形状必须与这张图一致）\n",
			itemBase+i+1, it.Name)
	}

	if sit := stripCameraWords(FrameScene(vp)); sit != "" {
		b.WriteString("\n【画面内容】这一帧开始时的人物位置与姿态：\n")
		b.WriteString(sit + "\n")
	}

	// 用第一个镜头的景别/运镜定构图；多镜头段的后续镜头由视频自己推进
	if shots := ParseFrameShots(vp); len(shots) > 0 {
		s := shots[0]
		if sz := strings.TrimSpace(s.Size); sz != "" {
			b.WriteString("\n【景别】" + sz + "\n")
		}
		if d := strings.TrimSpace(s.Desc); d != "" {
			b.WriteString("【人物动作】" + d + "\n")
		}
	}

	b.WriteString("\n【硬性要求】\n")
	b.WriteString("1. 真实电影质感、自然光、写实人物 —— 不要插画、线稿、漫画、3D 渲染风\n")
	b.WriteString("2. 空间关系严格按【画面内容】：谁在画面哪一侧、桌子在谁和谁之间，不能改\n")
	b.WriteString("3. 人物数量就是上面列的这几个，不要增加路人或背景人物\n")
	b.WriteString("4. 画面里不要出现任何文字、字幕、水印、台标\n")
	b.WriteString("5. 人物外观以角色参考图为准，不要因为场景光线改脸型、发型、服装款式\n")
	if len(items) > 0 {
		b.WriteString("6. 道具只能是这些：")
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, it.Name)
		}
		b.WriteString(strings.Join(names, "、") + "；颜色材质形状必须与对应参考图一致，不要凭空添加或替换\n")
	}

	return b.String()
}

// cameraWords 镜头语言词。全景空间参考图里混进这些词，出图模型会把它们当构图指令：
// 「双人中景、正面机位」会被理解成「两个人一起正对镜头的合影」，
// 于是两人各看各的、互不对视 —— 视频模型再照着这张图走，后面每段朝向全废。
// 规划 LLM 经常把分镜里的景别抄进 description，所以出图前必须洗掉。
//
// 只收复合词和纯景别词：「镜头」「机位」「全景」这种单词不收 ——
// 「不要正对镜头」是我们要保留的约束，「全景空间参考图」也要留着。
var cameraWords = []string{
	"过肩中近景", "双人中近景", "双人中景", "双人全景", "过肩镜头", "过肩",
	"正面机位", "反打机位", "固定机位", "固定镜头", "缓慢推镜", "手持跟拍", "手持镜头",
	"大特写", "中近景", "中景", "近景", "特写", "反打", "正打",
	"推镜", "拉镜", "摇镜", "移镜", "手持",
}

// stripCameraWords 洗掉描述里的镜头语言，只留「空间 + 人物 + 光线 + 朝向」。
func stripCameraWords(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, w := range cameraWords {
		s = strings.ReplaceAll(s, w, "")
	}
	s = strings.Join(strings.Fields(s), " ")
	// 删词后会留下「、：」「，：」这类空标点，压成单个冒号
	for {
		old := s
		for _, p := range []string{"、：", "，：", "：、", "：，", "、、", "，，", "、 ", "， ", " ："} {
			s = strings.ReplaceAll(s, p, "：")
		}
		if s == old {
			break
		}
	}
	return strings.TrimSpace(strings.TrimLeft(s, "、，：;；, "))
}

// BuildGroupFirstFramePrompt 拼「场景全景参考图」的提示词。
//
// 关键：这不是视频的第一帧，是空间参考图。
// H3 的节点只有 ref_images（参考图），没有 first_frame 输入 ——
// 镜头景别、机位方向、运镜都由模型按每段描述自己完成，
// 这张图只负责「这个空间长什么样、人长什么样、光从哪来、人朝哪边」。
// 所以必须生成全景（能看见整个房间），而不是某个中近景构图。
func BuildGroupFirstFramePrompt(description, vp string, roles []model.Character, scenes []model.Scene, items []model.Item) string {
	var b strings.Builder

	b.WriteString("把参考图合成一张这个场景的**全景空间参考图**。\n")
	b.WriteString("这不是电影画面，是一张定空间用的图：像室内设计效果图，从斜上方约 45° 俯看整个房间，")
	b.WriteString("能同时看清房间的布局、家具摆放、每个人站在或坐在哪里、身体朝哪一边。\n\n")

	b.WriteString("参考图对应关系：\n")
	for i, r := range roles {
		fmt.Fprintf(&b, "{{ref %d}} 是「%s」，长相、发型、服装必须与这张图完全一致\n", i+1, r.Name)
	}
	for i, sc := range scenes {
		fmt.Fprintf(&b, "{{ref %d}} 是「%s」的环境（空间布局、家具、光线以这张图为准）\n",
			len(roles)+i+1, sc.Name)
	}
	itemBase := len(roles) + len(scenes)
	for i, it := range items {
		fmt.Fprintf(&b, "{{ref %d}} 是道具「%s」的外观参考（颜色、材质、形状必须与这张图一致）\n",
			itemBase+i+1, it.Name)
	}

	if d := stripCameraWords(description); d != "" {
		b.WriteString("\n【空间与人物】" + d + "\n")
	}
	if sit := stripCameraWords(FrameScene(vp)); sit != "" {
		b.WriteString("\n【人物位置补充】下面这段是分镜里最新的站位，和上面的空间描述冲突时以它为准：\n")
		b.WriteString(sit + "\n")
	}

	// 朝向锁定：这是这张图唯一的「非画面」使命，也是踩坑最多的地方。
	// 图里两人一起正对镜头 → 视频里两人永远不看对方（实测第二段起老师不正对学生）。
	if len(roles) >= 2 {
		b.WriteString("\n【朝向 —— 这张图最重要的一条】\n")
		b.WriteString(fmt.Sprintf("画面里有 %d 个人，必须让他们的**身体互相朝向**：\n", len(roles)))
		b.WriteString("- 隔着桌子时：一人坐桌子这一侧、另一人坐对侧，椅子分别在桌子两条对边上，")
		b.WriteString("不是一人坐桌头一人站桌侧\n")
		b.WriteString("- 不隔桌子时：两人站或坐在彼此面前，上半身转向对方，不要并肩一起看镜头\n")
		b.WriteString("- 观察视角是斜侧方，两个人分别呈现「四分之三侧面」和「侧面」，能明显看出他们在对视\n")
		b.WriteString("- **禁止**把两个人画成并排站、肩并肩、一起正对镜头的合影姿势；")
		b.WriteString("禁止两个人都用正脸看镜头\n")
		b.WriteString("- 视线要落在对方脸上，不要看镜头、不要看别处\n")
	}

	// 道具必须逐个点名：只给图不点名时，模型会按自己的理解画成别的颜色/形状
	// （实测把深灰保温杯画成白色、牛皮纸笔记本画成蓝色）
	if len(items) > 0 {
		b.WriteString("\n【道具清单】画面里出现的道具只能是下面这几个，外观逐个对照参考图：\n")
		for i, it := range items {
			desc := strings.TrimSpace(it.ImagePrompt)
			if desc == "" {
				desc = strings.TrimSpace(it.Prompt)
			}
			if desc != "" {
				fmt.Fprintf(&b, "- %s（对应 {{ref %d}}）：%s\n", it.Name, itemBase+i+1, desc)
			} else {
				fmt.Fprintf(&b, "- %s（对应 {{ref %d}}）\n", it.Name, itemBase+i+1)
			}
		}
		b.WriteString("道具的颜色、材质、形状必须与对应参考图完全一致，不要换成别的颜色或款式；" +
			"不需要的道具不要凭空添加，缺了某个道具也不要拿相似的东西替代。\n")
	}

	b.WriteString("\n【硬性要求】\n")
	b.WriteString("1. 全景视角：画面能同时看到整个房间的主要布局（桌子、椅子、窗户、门），不要拍成特写或中近景\n")
	b.WriteString("2. 真实照片质感、自然光、写实人物 —— 不要插画、线稿、漫画、3D 渲染风\n")
	b.WriteString("3. 空间关系严格按上面描述：谁在桌子哪一侧、谁站着谁坐着，不能改\n")
	b.WriteString("4. 人物数量就是上面列的这几个，不要增加路人或背景人物\n")
	b.WriteString("5. 画面里不要出现任何文字、字幕、水印、台标\n")
	b.WriteString("6. 人物外观以角色参考图为准，不要因为场景光线改脸型、发型、服装款式\n")
	if len(roles) >= 2 {
		b.WriteString("7. 人物的身体朝向必须按【朝向】执行 —— 两人相对而立/相对而坐、互相看得见对方的脸，")
		b.WriteString("不要画成两个人都正面朝向镜头\n")
	}

	return b.String()
}
