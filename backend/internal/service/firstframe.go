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

	b.WriteString("参考图：")
	refs := make([]string, 0, len(roles)+len(scenes)+len(items))
	for i, r := range roles {
		refs = append(refs, fmt.Sprintf("{{ref %d}}=%s", i+1, r.Name))
	}
	for i, sc := range scenes {
		refs = append(refs, fmt.Sprintf("{{ref %d}}=%s", len(roles)+i+1, sc.Name))
	}
	itemBase := len(roles) + len(scenes)
	for i, it := range items {
		refs = append(refs, fmt.Sprintf("{{ref %d}}=%s", itemBase+i+1, it.Name))
	}
	b.WriteString(strings.Join(refs, "、") + "\n\n")

	if sit := stripCameraWords(FrameScene(vp)); sit != "" {
		b.WriteString("画面内容：" + sit + "\n\n")
	}
	// 用第一个镜头的景别/运镜定构图；多镜头段的后续镜头由视频自己推进
	if shots := ParseFrameShots(vp); len(shots) > 0 {
		s := shots[0]
		if sz := strings.TrimSpace(s.Size); sz != "" {
			b.WriteString("景别：" + sz + "\n")
		}
		if d := strings.TrimSpace(s.Desc); d != "" {
			b.WriteString("人物动作：" + d + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("要求：\n")
	b.WriteString("1. **环境布局严格按场景图**：家具的位置、数量、朝向都不许改，")
	b.WriteString("不许增减家具，不许更换墙面与地面材质\n")
	b.WriteString("2. 人物长相/发型/服装对角色图，道具外观对道具图\n")
	b.WriteString("3. 站着还是坐着、身体朝向谁，一律按上面文字执行，不要照抄参考图里的人像站姿\n")
	b.WriteString("4. 真实电影质感、自然光；不要插画/CG；画面里不要有文字、水印\n")

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

	b.WriteString("把参考图合成一张这个场景的**全景空间参考图**：室内效果图视角，")
	b.WriteString("从斜上方俯看整个房间，能看清家具布局、每个人坐在或站在哪里、身体朝哪边。\n\n")

	// 参考图只标「谁是谁」，不逐个复述外观。
	// 模型看图比读文字准，写一大段「深蓝色保温杯、外壳为哑光烤漆不锈钢、
	// 高约 22 厘米…」反而会和图里的实际样子打架，还把提示词撑到两千多字，
	// 模型抓不住重点。
	b.WriteString("参考图：")
	refs := make([]string, 0, len(roles)+len(scenes)+len(items))
	for i, r := range roles {
		refs = append(refs, fmt.Sprintf("{{ref %d}}=%s", i+1, r.Name))
	}
	for i, sc := range scenes {
		refs = append(refs, fmt.Sprintf("{{ref %d}}=%s", len(roles)+i+1, sc.Name))
	}
	itemBase := len(roles) + len(scenes)
	for i, it := range items {
		refs = append(refs, fmt.Sprintf("{{ref %d}}=%s", itemBase+i+1, it.Name))
	}
	b.WriteString(strings.Join(refs, "、") + "\n\n")

	if d := stripCameraWords(description); d != "" {
		b.WriteString("空间与人物：" + d + "\n\n")
	}
	// 这里**故意不再拼分镜里的「地点」行**（FrameScene(vp)）。
	//
	// 那张「地点」行是这一集第 1 段开头的瞬间站位，里面常带着场景里不该固化的东西：
	// 「黑色双肩包还背在肩上」「刚进门」「双肩包放在脚边」。
	// 而这张图是 28 段共用的全景空间基准，一旦把「学生站着背书包」固化进去，
	// 后面每一段都会带着这个姿势 —— 描述里明明写着两人相对而坐，出图却是站着的。
	//
	// 人物位置以规划描述为准就够了：那是模型看过资产图之后写的，
	// 而分镜的站位是「某一段某一秒」的状态，不属于空间基准。

	b.WriteString("要求：\n")
	n := 1
	// 环境布局单独成条：和「人物一致」挤在一句里时，模型容易只顾人物、忽略空间。
	// 这张图的核心价值就是把房间结构钉死，后面几十段视频共用它。
	fmt.Fprintf(&b, "%d. **环境布局严格按场景图**：桌子、椅子、窗户、柜子、书架的位置、数量、朝向都不许改，", n)
	b.WriteString("不许增减家具，不许更换墙面与地面材质；只改变观看角度，不改变空间结构\n")
	n++
	fmt.Fprintf(&b, "%d. 人物长相/发型/服装对角色图，道具外观对道具图\n", n)
	n++
	// 踩过的坑：角色图是白底站姿人像，不强调的话模型会照抄站姿，文字写「坐」也不听
	fmt.Fprintf(&b, "%d. 站着还是坐着、身体朝向谁，一律按上面文字执行，不要照抄参考图里的人像站姿\n", n)
	n++
	if len(roles) >= 2 {
		fmt.Fprintf(&b, "%d. 两人隔着桌子**相对而坐**、身体互相朝向、视线落在对方脸上；", n)
		b.WriteString("不要画成并肩正对镜头的合影\n")
		n++
	}
	fmt.Fprintf(&b, "%d. 真实照片质感、自然光；不要插画/CG；画面里不要有文字、水印\n", n)

	return b.String()
}
