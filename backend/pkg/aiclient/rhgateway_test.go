package aiclient

import (
	"encoding/json"
	"testing"
)

// 网关的 TaskStatus 字段类型不稳定：
// errorCode 有时是 200 有时是 ""，failedReason 有时是 "" 有时是 {}，
// consumeCoins 有时是 "2" 有时是 2。
//
// 早期用强类型（int / string）声明，任何一次变异都会让整包 JSON 解析失败。
// 而解析失败在轮询里被 continue 吞掉，任务就永远停在 QUEUED 直到超时 ——
// 这是线上真实发生过的事故，用这些用例守住。
func TestTaskStatusTolerantDecode(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantStat string
		wantErrs string // 期望的 failedReason（空串表示无）
		wantURL  string
	}{
		{
			name:     "正常成功响应",
			raw:      `{"taskId":"t1","status":"SUCCESS","errorCode":"","errorMessage":"","failedReason":{},"results":[{"url":"a.png","nodeId":"33","outputType":"png"}],"usage":{"consumeCoins":"2","taskCostTime":"30"}}`,
			wantStat: "SUCCESS",
			wantURL:  "a.png",
		},
		{
			name:     "errorCode 是数字",
			raw:      `{"taskId":"t2","status":"RUNNING","errorCode":200,"failedReason":"","usage":{"consumeCoins":2,"taskCostTime":12}}`,
			wantStat: "RUNNING",
		},
		{
			name:     "failedReason 是字符串",
			raw:      `{"taskId":"t3","status":"FAILED","errorCode":"-6","failedReason":"显存不足"}`,
			wantStat: "FAILED",
			wantErrs: "显存不足",
		},
		{
			name:     "failedReason 是非空对象",
			raw:      `{"taskId":"t4","status":"FAILED","failedReason":{"node":"12","reason":"超时"}}`,
			wantStat: "FAILED",
		},
		{
			name:     "errorCode 是 null",
			raw:      `{"taskId":"t5","status":"QUEUED","errorCode":null}`,
			wantStat: "QUEUED",
		},
		{
			name:     "usage 缺失",
			raw:      `{"taskId":"t6","status":"SUCCESS","results":[{"url":"b.mp4","nodeId":"9","outputType":"mp4"}]}`,
			wantStat: "SUCCESS",
			wantURL:  "b.mp4",
		},
	}

	for _, c := range cases {
		var st TaskStatus
		if err := json.Unmarshal([]byte(c.raw), &st); err != nil {
			t.Fatalf("%s: 解析不应失败，实际 %v", c.name, err)
		}
		if st.Status != c.wantStat {
			t.Errorf("%s: status 期望 %s，实际 %s", c.name, c.wantStat, st.Status)
		}
		if c.wantErrs != "" && string(st.FailedReason) != c.wantErrs {
			t.Errorf("%s: failedReason 期望 %q，实际 %q", c.name, c.wantErrs, st.FailedReason)
		}
		if c.wantURL != "" {
			if len(st.Results) == 0 || st.Results[0].URL != c.wantURL {
				t.Errorf("%s: results 期望首个 %q，实际 %+v", c.name, c.wantURL, st.Results)
			}
		}
	}
}

// 空对象 failedReason 应被归一成空串，避免拼进错误文案里出现 "{}"
func TestFailedReasonEmptyObject(t *testing.T) {
	var st TaskStatus
	if err := json.Unmarshal([]byte(`{"status":"FAILED","failedReason":{}}`), &st); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if string(st.FailedReason) != "" {
		t.Errorf("空对象应归一为空串，实际 %q", st.FailedReason)
	}
}
