package service

import (
	"strings"
	"testing"

	"jikeai-clone/internal/model"
)

const guardScript = `**老师：** 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？

**学生：** 盖房子、修路架桥吧，听说毕业常驻工地。`

func frameWith(idx int, lines ...string) model.Storyboard {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("台词：" + l + "\n")
	}
	return model.Storyboard{FrameIndex: idx, VideoPrompt: b.String()}
}

// 剧本对白被完整落地时不报问题
func TestDialogueIssuesClean(t *testing.T) {
	frames := []model.Storyboard{
		frameWith(1, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？</d>"),
		frameWith(2, "@学生：<d>[Chinese] 盖房子、修路架桥吧，听说毕业常驻工地。</d>"),
	}
	if issues := DialogueIssues(frames, guardScript); len(issues) != 0 {
		t.Errorf("不该有问题，实际：%v", issues)
	}
}

// 漏掉一句要报出来
func TestDialogueIssuesMissing(t *testing.T) {
	frames := []model.Storyboard{
		frameWith(1, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？</d>"),
	}
	issues := DialogueIssues(frames, guardScript)
	if len(issues) == 0 || !strings.Contains(strings.Join(issues, "；"), "漏台词") {
		t.Errorf("应报漏台词，实际：%v", issues)
	}
}

// 同一句出现在两段要报出来
func TestDialogueIssuesRepeated(t *testing.T) {
	frames := []model.Storyboard{
		frameWith(1, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？</d>"),
		frameWith(2, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？</d>"),
		frameWith(3, "@学生：<d>[Chinese] 盖房子、修路架桥吧，听说毕业常驻工地。</d>"),
	}
	issues := DialogueIssues(frames, guardScript)
	if len(issues) == 0 || !strings.Contains(strings.Join(issues, "；"), "重复") {
		t.Errorf("应报重复，实际：%v", issues)
	}
}

// 一句台词分到两段、但拼回去完整 —— 属于正常拆分（一镜一句），不该报警
func TestDialogueIssuesSplitAcross(t *testing.T) {
	frames := []model.Storyboard{
		frameWith(1, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕</d>"),
		frameWith(2, "@老师：<d>[Chinese] 就是去工地。先说说你印象里的土木是什么？</d>"),
		frameWith(3, "@学生：<d>[Chinese] 盖房子、修路架桥吧，听说毕业常驻工地。</d>"),
	}
	if issues := DialogueIssues(frames, guardScript); len(issues) != 0 {
		t.Errorf("拼接完整就不该报警，实际：%v", issues)
	}
}

// 拆开后拼不回原句（后半丢了）要报出来
func TestDialogueIssuesSplitAndLost(t *testing.T) {
	frames := []model.Storyboard{
		frameWith(1, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕</d>"),
		frameWith(2, "@老师：<d>[Chinese] 那是一部分。</d>"),
		frameWith(3, "@学生：<d>[Chinese] 盖房子、修路架桥吧，听说毕业常驻工地。</d>"),
	}
	issues := DialogueIssues(frames, guardScript)
	joined := strings.Join(issues, "；")
	if !strings.Contains(joined, "拼不回") && !strings.Contains(joined, "只说了一半") {
		t.Errorf("应报台词残缺，实际：%v", issues)
	}
}

// 长台词装不进一段时按句号拆开是合理的，不该报警
func TestDialogueIssuesSentenceSplitAllowed(t *testing.T) {
	frames := []model.Storyboard{
		frameWith(1, "@老师：<d>[Chinese] 坐。你说对土木有兴趣，又怕“就是去工地”。</d>"),
		frameWith(2, "@老师：<d>[Chinese] 先说说你印象里的土木是什么？</d>"),
		frameWith(3, "@学生：<d>[Chinese] 盖房子、修路架桥吧，听说毕业常驻工地。</d>"),
	}
	if issues := DialogueIssues(frames, guardScript); len(issues) != 0 {
		t.Errorf("按句拆段不该报警，实际：%v", issues)
	}
}

// 剧本没有对白（纯旁白/纯动作）时不该报问题
func TestDialogueIssuesNoScriptDialogue(t *testing.T) {
	frames := []model.Storyboard{frameWith(1, "@老师：<d>[Chinese] 随便说点什么。</d>")}
	if issues := DialogueIssues(frames, "他推开门，走了进去。"); len(issues) != 0 {
		t.Errorf("无对白剧本不该报问题，实际：%v", issues)
	}
}
