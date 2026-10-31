package service

import (
	"strings"
	"testing"
)

const realVideoPrompt = `[出镜角色-物品-场景]：
人物：@生涯规划老师，@高中生
场景：@公司会议室-白天
物品：@大学专业介绍手册，@矿泉水瓶

【素材与场景状态】
白天，室内，@公司会议室-白天。左侧整面落地玻璃窗挂浅灰百叶帘。

｜镜头1｜ 0-10秒｜双人中景｜
@生涯规划老师 拿起 @大学专业介绍手册 转向 @高中生。
【@生涯规划老师 台词】（平和笃定）：“你看这本。”

【视觉风格】
真人现代风格，竖屏 9:16，浅景深，自然光，纯净无字画面

全局执行规则：
视频禁止生成字幕、禁止添加水印；台词必须逐字完整说出。
单镜时长 3-5 秒，动作连贯。`

// @资产 引用必须保留：参考站就是靠 @名 + 参考图层层对应的
func TestSanitizeKeepsAtMentions(t *testing.T) {
	got := SanitizeVideoPrompt(realVideoPrompt, "16:9", 10)
	if !strings.Contains(got, "@生涯规划老师") || !strings.Contains(got, "@大学专业介绍手册") {
		t.Errorf("把 @资产 引用删掉了:\n%s", got)
	}
	if !strings.Contains(got, "[出镜角色-物品-场景]") {
		t.Errorf("把出镜清单块删掉了:\n%s", got)
	}
}

// 视觉风格里的画幅必须跟随实际提交的比例
func TestSanitizeAlignsAspect(t *testing.T) {
	cases := map[string]string{
		"16:9":             "横屏 16:9",
		"9:16":             "竖屏 9:16",
		"1:1":              "方形 1:1",
		"21:9":             "超宽屏 21:9",
		"16:9 (Widescreen)": "横屏 16:9",
	}
	for aspect, want := range cases {
		got := SanitizeVideoPrompt(realVideoPrompt, aspect, 10)
		// 视觉风格可能是「标题行 + 内容行」，只看标题行会误判，取这一段整体
		idx := strings.Index(got, "视觉风格")
		if idx < 0 {
			t.Fatalf("找不到视觉风格段")
		}
		seg := got[idx:]
		if n := strings.Index(seg, "\n\n"); n > 0 {
			seg = seg[:n]
		}
		if !strings.Contains(seg, want) {
			t.Errorf("aspect=%s 期望视觉风格段含 %q，实际 %q", aspect, want, seg)
		}
		// 旧的画幅不能残留
		for _, stale := range []string{"竖屏 9:16", "横屏 16:9"} {
			if stale != want && strings.Contains(seg, stale) {
				t.Errorf("aspect=%s 残留旧画幅 %q：%q", aspect, stale, seg)
			}
		}
	}
}

// 单镜时长要跟工具栏选的时长一致
func TestSanitizeAlignsDuration(t *testing.T) {
	got := SanitizeVideoPrompt(realVideoPrompt, "16:9", 10)
	if !strings.Contains(got, "单镜时长 10 秒") {
		t.Errorf("单镜时长没对齐:\n%s", got)
	}
	if strings.Contains(got, "3-5 秒") {
		t.Errorf("旧的 3-5 秒约束还在")
	}
	if k := SanitizeVideoPrompt(realVideoPrompt, "16:9", 0); !strings.Contains(k, "3-5 秒") {
		t.Errorf("未指定时长时不该改动规则")
	}
}

// 幂等 + 空串安全
func TestSanitizeIdempotent(t *testing.T) {
	once := SanitizeVideoPrompt(realVideoPrompt, "16:9", 10)
	if twice := SanitizeVideoPrompt(once, "16:9", 10); once != twice {
		t.Errorf("清洗不幂等")
	}
	if got := SanitizeVideoPrompt("", "16:9", 10); got != "" {
		t.Errorf("空串应返回空串，实际 %q", got)
	}
}
