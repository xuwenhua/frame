package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BuildRef2VAPrompt 按「短剧 H3 六段式」组装描述词。
//
// 和旧的「场次/地点/人物/镜号」格式相比，这套结构是 H3 官方 Ref2VA 写法：
//
//	subject_definitions  每个 Subject 绑定一张参考图 + 锁定清单
//	summary              整体说明 + 人物锁定重申
//	retention_analysis   每张参考图保留了什么
//	detailed_description 带绝对时间码的分镜（[Shot N] At MM:SS.mmm）
//	overall_soundscape   环境音
//	non_diegetic_music   无 BGM 时统一 N/A
//
// 图位编号规则：角色 → 场景 → 道具，和 frameRefSlots 的推送顺序一致。
// 这里写死 <Picture N> 而不是 @名字 —— 六段式里 Subject 必须先绑定到具体图位，
// 模型才知道「这个人是哪张图」。
func BuildRef2VAPrompt(d FrameDraft, duration int) string {
	shots := normalizeShots(d)
	if duration <= 0 {
		duration = EstimateFrameDuration(d)
	}
	secs, _ := planShotSeconds(shots)

	// 每镜的起止时间码
	type span struct{ start, end float64 }
	spans := make([]span, len(shots))
	cursor := 0.0
	for i := range shots {
		spans[i] = span{start: cursor, end: cursor + float64(secs[i])}
		cursor += float64(secs[i])
	}

	// 图位编号：必须和 frameRefSlots 的推送顺序完全一致，
	// 否则整份提示词引用错位（实测出过：首帧图占了 Picture 1，
	// 提示词却说 Picture 1 是老师 —— 模型把首帧当成老师，人物全乱）。
	//
	// frameRefSlots 的顺序是：首帧图（若有）→ 角色 → 场景 → 道具。
	// 所以有首帧时，编号从 2 开始。
	picIdx := map[string]int{}
	next := 1
	hasFirstFrame := strings.TrimSpace(d.FirstFrameImage) != ""
	if hasFirstFrame {
		next = 2 // Picture 1 留给首帧图
	}
	for _, r := range d.Roles {
		if _, ok := picIdx[r.Name]; !ok && strings.TrimSpace(r.Name) != "" {
			picIdx[r.Name] = next
			next++
		}
	}
	for _, sc := range d.Scenes {
		if _, ok := picIdx[sc.Name]; !ok && strings.TrimSpace(sc.Name) != "" {
			picIdx[sc.Name] = next
			next++
		}
	}
	for _, it := range d.Items {
		if _, ok := picIdx[it.Name]; !ok && strings.TrimSpace(it.Name) != "" {
			picIdx[it.Name] = next
			next++
		}
	}

	var b strings.Builder

	// ---- 1. subject_definitions ----
	b.WriteString("subject_definitions:\n")
	if hasFirstFrame {
		b.WriteString("<Subject 0> is the opening frame of this shot, from <Picture 1>. ")
		b.WriteString("This is the exact first frame of the video: camera angle, framing, ")
		b.WriteString("everyone's position and pose, furniture layout and lighting ")
		b.WriteString("must all match it exactly at t=0, then the action develops from there.\n")
	}
	for i, r := range d.Roles {
		n, ok := picIdx[r.Name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "<Subject %d> is 「%s」, from <Picture %d>. ", i+1, r.Name, n)
		if brief := briefIdentity(r.IdentityAnchor); brief != "" {
			fmt.Fprintf(&b, "Identity: %s. ", brief)
		}
		if cue := strings.TrimSpace(r.AppearanceCues); cue != "" {
			fmt.Fprintf(&b, "HAIR & FACE LOCKED as: %s. ", cue)
		}
		if cos := strings.TrimSpace(r.CostumePrompt); cos != "" {
			fmt.Fprintf(&b, "OUTFIT LOCKED as: %s. ", cos)
		}
		if seat, ok := d.SeatMap[r.Name]; ok && strings.TrimSpace(seat) != "" {
			fmt.Fprintf(&b, "POSITION LOCKED: seated at %s, and never leaves this seat.", seat)
		}
		b.WriteString("\n")
	}
	for _, sc := range d.Scenes {
		n, ok := picIdx[sc.Name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "<Subject %d> is the environment 「%s」, from <Picture %d>. Preserve: %s.\n",
			next, sc.Name, n, strings.TrimSpace(sc.SpatialAnchor))
	}

	// ---- 2. summary ----
	b.WriteString("\nsummary:\n")
	fmt.Fprintf(&b, "[reference generation] A realistic Chinese short-drama scene, approximately %d seconds. ", duration)
	for _, r := range d.Roles {
		if cos := strings.TrimSpace(r.CostumePrompt); cos != "" {
			fmt.Fprintf(&b, "%s OUTFIT LOCKED: %s. ", r.Name, firstClause(cos))
		}
	}
	if sit := strings.TrimSpace(d.Situation); sit != "" {
		b.WriteString(sit)
	}
	b.WriteString("\n")

	// ---- 3. retention_analysis ----
	b.WriteString("\nretention_analysis:\n")
	for i, r := range d.Roles {
		if _, ok := picIdx[r.Name]; !ok {
			continue
		}
		var keep []string
		if c := strings.TrimSpace(r.CostumePrompt); c != "" {
			keep = append(keep, "outfit")
		}
		if c := strings.TrimSpace(r.AppearanceCues); c != "" {
			keep = append(keep, "hair and facial features")
		}
		desc := "identity"
		if len(keep) > 0 {
			desc = strings.Join(keep, ", ")
		}
		fmt.Fprintf(&b, "<Subject %d>: fully_preserved - %s retained throughout.\n", i+1, desc)
	}
	for _, sc := range d.Scenes {
		if _, ok := picIdx[sc.Name]; !ok {
			continue
		}
		fmt.Fprintf(&b, "<Subject %d>: fully_preserved - room layout, furniture and lighting retained.\n", next)
	}

	// ---- 4. detailed_description ----
	b.WriteString("\ndetailed_description:\n")
	b.WriteString("Realistic live-action Chinese drama, bright natural daylight, natural skin texture, no stylization.\n")
	for i, sh := range shots {
		sp := spans[i]
		fmt.Fprintf(&b, "[Shot %d] At %s, ", i+1, timecode(sp.start))
		if g := strings.TrimSpace(sh.ShotSize); g != "" {
			fmt.Fprintf(&b, "%s. ", g)
		}
		if mv := strings.TrimSpace(sh.Camera); mv != "" {
			fmt.Fprintf(&b, "Camera: %s. ", mv)
		}
		if st := strings.TrimSpace(sh.State); st != "" {
			fmt.Fprintf(&b, "%s. ", st)
		}
		if act := strings.TrimSpace(sh.Action); act != "" {
			fmt.Fprintf(&b, "%s. ", act)
		}
		for _, dlg := range sh.Dialogues {
			line := strings.TrimSpace(dlg.Line)
			if line == "" {
				continue
			}
			speaker := strings.TrimSpace(dlg.Role)
			if speaker != "" {
				fmt.Fprintf(&b, "%s says: ", speaker)
			}
			fmt.Fprintf(&b, "<d>[Chinese] %s</d> ", line)
		}
		fmt.Fprintf(&b, "Duration about %.1fs.\n", sp.end-sp.start)
	}

	// ---- 5. overall_soundscape ----
	b.WriteString("\noverall_soundscape:\n")
	if sfx := strings.TrimSpace(d.SFX); sfx != "" {
		b.WriteString(sfx + "\n")
	} else {
		b.WriteString("Quiet office room tone, faint outdoor traffic, subtle clothing rustle.\n")
	}

	// ---- 6. non_diegetic_music ----
	b.WriteString("\nnon_diegetic_music:\nN/A\n")

	return b.String()
}

// timecode 秒 → MM:SS.mmm
func timecode(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	m := int(sec) / 60
	s := sec - float64(m*60)
	return fmt.Sprintf("%02d:%06.3f", m, s)
}

// firstClause 取第一句，用作 summary 里的简短锁定提示
func firstClause(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"。", "；", ";", "\n"} {
		if i := strings.Index(s, sep); i > 0 {
			return s[:i]
		}
	}
	if len(s) > 60 {
		return s[:60]
	}
	return s
}

// ShiftPictureRefs 把提示词里的 <Picture N> 全部 +offset。
//
// 用在「首帧图后补」的场景：分镜生成时还没有首帧，提示词按
// Picture 1=老师、2=学生 编号；等首帧出好了再出片，图位变成
// 1=首帧、2=老师、3=学生 —— 不整体后移一位，模型就把首帧当成了老师。
//
// 从大到小替换，避免 1→2 之后又被 2→3 连锁改掉。
func ShiftPictureRefs(prompt string, offset int) string {
	if offset <= 0 || prompt == "" {
		return prompt
	}
	// 先找出最大编号
	re := regexp.MustCompile(`<Picture (\d+)>`)
	matches := re.FindAllStringSubmatch(prompt, -1)
	maxN := 0
	for _, m := range matches {
		if n, err := strconv.Atoi(m[1]); err == nil && n > maxN {
			maxN = n
		}
	}
	for i := maxN; i >= 1; i-- {
		prompt = strings.ReplaceAll(prompt,
			fmt.Sprintf("<Picture %d>", i),
			fmt.Sprintf("<Picture %d>", i+offset))
	}
	return prompt
}

// PrependFirstFrameSubject 在 subject_definitions 开头补一条首帧声明。
//
// 用「定位 subject_definitions: 再插入」会被前导空白/换行差异搞挂（实测插不进去），
// 所以直接拼在整份提示词最前面 —— 它在语义上就是 subject_definitions 的第一条。
func PrependFirstFrameSubject(prompt string) string {
	if !strings.Contains(prompt, "subject_definitions:") {
		return prompt
	}
	if strings.Contains(prompt, "<Subject 0>") {
		return prompt // 已经加过
	}
	decl := "<Subject 0> is the opening frame of this shot, from <Picture 1>. " +
		"This is the exact first frame of the video: camera angle, framing, " +
		"everyone's position and pose, furniture layout and lighting " +
		"must all match it exactly at t=0, then the action develops from there.\n"
	// 插在 "subject_definitions:" 这一行之后
	idx := strings.Index(prompt, "subject_definitions:")
	if idx < 0 {
		return decl + prompt
	}
	lineEnd := strings.Index(prompt[idx:], "\n")
	if lineEnd < 0 {
		return prompt + "\n" + decl
	}
	cut := idx + lineEnd + 1
	return prompt[:cut] + decl + prompt[cut:]
}

// DropEnvironmentSubject 删掉 subject_definitions 里的 environment 那一行。
//
// 有首帧时场景图不再传给模型（首帧本身就是定稿画面），提示词里还留着
// "<Subject N> is the environment, from <Picture M>" 会指向一个不存在的图位。
func DropEnvironmentSubject(prompt string) string {
	lines := strings.Split(prompt, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.Contains(ln, "is the environment") && strings.Contains(ln, "<Picture") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}
