package service

import (
	"strings"
	"testing"
)

// 一帧可能含多个镜头（实测第 1 帧就是 3 镜 9 秒），
// 必须按镜头拆出来，首帧图的构图和视频的站位都靠它。
func TestParseFrameShots(t *testing.T) {
	vp := `【素材与场景状态】（这一帧开始时的姿态与位置）
咨询间内，@老师 坐在桌左侧，@学生 站在桌右侧。

｜镜头1｜ 0–1秒｜中景｜固定机位｜
【@老师台词】（沉稳）：“坐。”
【动作】（按台词顺序推进；台词里要求对方做的动作，说到时才发生）
@老师 抬右手向下轻按，示意 @学生 落座
【音效】：椅脚轻响

｜镜头2｜ 1–5秒｜中近景｜缓慢推镜｜
【@老师台词】（平和）：“你说对土木有兴趣。”
【动作】（按台词顺序推进；台词里要求对方做的动作，说到时才发生）
@老师 目光落在 @学生 脸上，双手交叠放在桌面

｜镜头3｜ 5–9秒｜近景｜固定机位｜
【动作】（按台词顺序推进；台词里要求对方做的动作，说到时才发生）
@老师 身体微微前倾，视线稳定看向 @学生`

	shots := ParseFrameShots(vp)
	if len(shots) != 3 {
		t.Fatalf("应拆出 3 个镜头，实际 %d 个", len(shots))
	}
	wantSize := []string{"中景", "中近景", "近景"}
	for i, s := range shots {
		if s.Size != wantSize[i] {
			t.Errorf("镜头%d 景别 = %q，期望 %q", i+1, s.Size, wantSize[i])
		}
		if strings.Contains(s.Desc, "@") {
			t.Errorf("镜头%d 描述里还有 @：%q", i+1, s.Desc)
		}
		if strings.Contains(s.Desc, "按台词顺序推进") {
			t.Errorf("镜头%d 把【动作】的说明行也当成画面了：%q", i+1, s.Desc)
		}
		if strings.Contains(s.Desc, "台词") || strings.Contains(s.Desc, "音效") {
			t.Errorf("镜头%d 混进了台词/音效：%q", i+1, s.Desc)
		}
	}
	if !strings.Contains(shots[0].Desc, "抬右手向下轻按") {
		t.Errorf("镜头1 描述不对：%q", shots[0].Desc)
	}
	if !strings.Contains(shots[2].Desc, "身体微微前倾") {
		t.Errorf("镜头3 描述不对：%q", shots[2].Desc)
	}

	// 没有镜头头的老数据要返回空，让调用方退回整帧描述
	if got := ParseFrameShots("只有一段普通描述，没有镜头头"); len(got) != 0 {
		t.Errorf("无镜头头时应返回空，实际 %d 个", len(got))
	}
}

// 站位描述是首帧图构图的唯一文字来源，必须能取到且去掉 @
func TestFrameScene(t *testing.T) {
	vp := "【素材与场景状态】（这一帧开始时的姿态与位置）\n@老师 坐在画面左侧桌后，@学生 站在右侧。\n\n｜镜头1｜ 0–5秒｜中景｜"
	got := FrameScene(vp)
	if !strings.Contains(got, "坐在画面左侧桌后") || strings.Contains(got, "@") {
		t.Errorf("站位描述不对：%q", got)
	}
	if FrameScene("没有状态块的描述") != "" {
		t.Error("没有状态块时应返回空")
	}
}

func TestStripColorWords(t *testing.T) {
	if got := StripColorWords("深蓝色的笔记本放在白色桌面上"); strings.Contains(got, "蓝色") || strings.Contains(got, "白色") {
		t.Errorf("颜色词没去干净：%q", got)
	}
}

// 新格式把站位并进了「地点：@场景名 站位描述」，首帧图靠它取人物位置
func TestFrameSceneNewFormat(t *testing.T) {
	vp := "场次：1-3\n地点：@咨询间-白天 老师坐在桌子左侧的办公椅上，学生站在右侧。\n时间：日 / 内\n\n镜号：1\n景别：中景/老师\n"
	got := FrameScene(vp)
	if !strings.Contains(got, "坐在桌子左侧") {
		t.Errorf("地点行没取到站位：%q", got)
	}
	if strings.Contains(got, "@") || strings.Contains(got, "咨询间") {
		t.Errorf("应去掉 @ 与场景名，实际 %q", got)
	}
	// 没有站位时不该返回场景名本身
	if got := FrameScene("地点：@咨询间-白天\n时间：日 / 内"); got != "" {
		t.Errorf("只有场景名时应返回空，实际 %q", got)
	}
}
