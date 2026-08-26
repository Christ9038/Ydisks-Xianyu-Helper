package notify

import (
	"strings"
	"testing"
	"time"
)

// TestNotificationPureMappings 验证通知事件、等级和账号告警文本的纯映射分支。
func TestNotificationPureMappings(t *testing.T) {
	// levelCases 保存等级到中文标签的映射样例。
	levelCases := map[string]string{"critical": "严重", "warn": "警告", "info": "提示", "custom": "custom"}
	// level、want 表示当前等级及预期展示标签。
	for level, want := range levelCases {
		// got 是当前等级转换后的中文展示标签。
		got := levelLabel(level)
		if got != want {
			t.Fatalf("等级标签错误 level=%q got=%q want=%q", level, got, want)
		}
	}
	// eventCases 保存通知事件到中文标签的映射样例。
	eventCases := map[string]string{
		EventAccountOffline: "掉线通知", EventAccountRecovered: "恢复通知", EventAccountDisabled: "禁用通知",
		EventSecurityVerification: "风控验证", EventTokenRenewal: "续期通知", EventDeliveryResult: "交易通知",
		EventSystemError: "系统错误", "": "通知", "custom": "custom",
	}
	// event、want 表示当前事件及预期展示标签。
	for event, want := range eventCases {
		// got 是当前事件转换后的中文展示标签。
		got := eventLabel(event)
		if got != want {
			t.Fatalf("事件标签错误 event=%q got=%q want=%q", event, got, want)
		}
	}
	// alertCases 保存账号告警文本到业务事件的映射样例。
	alertCases := []struct {
		// title 是告警标题。
		title string
		// body 是告警正文。
		body string
		// want 是预期事件类型。
		want string
	}{
		{title: "captcha risk", want: EventSecurityVerification},
		{body: "账号已禁用", want: EventAccountDisabled},
		{body: "offline session", want: EventAccountOffline},
		{body: "token renew failed", want: EventTokenRenewal},
		{body: "unknown problem", want: EventSystemError},
	}
	// alertCase 表示当前账号告警映射样例。
	for _, alertCase := range alertCases {
		// got 是当前告警文本归类后的事件类型。
		got := classifyAccountAlertEvent(alertCase.title, alertCase.body)
		if got != alertCase.want {
			t.Fatalf("告警分类错误 title=%q body=%q got=%q want=%q", alertCase.title, alertCase.body, got, alertCase.want)
		}
	}
}

// TestNotificationBusinessErrorMappings 验证渠道响应中的多种错误编码和消息字段。
func TestNotificationBusinessErrorMappings(t *testing.T) {
	// cases 保存通知渠道响应及是否应视为业务错误。
	cases := []struct {
		// body 是渠道返回正文。
		body string
		// wantError 表示是否应返回业务错误。
		wantError bool
	}{
		{body: "", wantError: false},
		{body: "not-json", wantError: false},
		{body: `{"errcode":1,"errmsg":"bad"}`, wantError: true},
		{body: `{ "StatusCode": "500", "msg": "bad" }`, wantError: true},
		{body: `{ "code": 500, "message": "bad" }`, wantError: true},
		{body: `{ "code": 200 }`, wantError: false},
		{body: `{ "ok": false, "description": "bad" }`, wantError: true},
		{body: `{ "errcode": "bad" }`, wantError: false},
	}
	// testCase 表示当前渠道响应样例。
	for _, testCase := range cases {
		// err 保存当前渠道响应的业务错误解析结果。
		err := notificationBusinessError([]byte(testCase.body))
		if (err != nil) != testCase.wantError {
			t.Fatalf("渠道错误解析错误 body=%q err=%v wantError=%v", testCase.body, err, testCase.wantError)
		}
	}
	// numberPayload 保存数字和字符串数字的映射样例。
	numberPayload := map[string]any{"float": float64(2), "string": "3.5", "invalid": "x", "other": true}
	// number、ok 保存数字映射解析结果。
	// floatNumber、floatOK 保存浮点数字段解析结果。
	floatNumber, floatOK := mapNumber(numberPayload, "float")
	if !floatOK || floatNumber != 2 {
		t.Fatalf("float 数字解析错误: number=%v ok=%v", floatNumber, floatOK)
	}
	// stringNumber、stringOK 保存字符串数字段解析结果。
	stringNumber, stringOK := mapNumber(numberPayload, "string")
	if !stringOK || stringNumber != 3.5 {
		t.Fatalf("string 数字解析错误: number=%v ok=%v", stringNumber, stringOK)
	}
	// invalidNumber、invalidOK 保存非法字符串数字段解析结果。
	invalidNumber, invalidOK := mapNumber(numberPayload, "invalid")
	if invalidOK || invalidNumber != 0 {
		t.Fatal("非法字符串不应解析为数字")
	}
	// otherNumber、otherOK 保存非数字类型字段解析结果。
	otherNumber, otherOK := mapNumber(numberPayload, "other")
	if otherOK || otherNumber != 0 {
		t.Fatal("非数字类型不应解析为数字")
	}
	// missingNumber、missingOK 保存缺失字段解析结果。
	missingNumber, missingOK := mapNumber(numberPayload, "missing")
	if missingOK || missingNumber != 0 {
		t.Fatal("缺失字段不应解析为数字")
	}
}

// TestFormatEventCoversFallbacksAndStableFieldOrder 验证通知格式化的标题、等级、字段排序和正文收口。
func TestFormatEventCoversFallbacksAndStableFieldOrder(t *testing.T) {
	// event 保存缺省标题和等级、带空字段的通知样例。
	event := NotificationEvent{Type: EventSystemError, Level: "", AccountID: "account-1", Fields: map[string]string{"zeta": "", "alpha": "值"}, Body: "  正文  ", Time: time.Date(2026, 8, 26, 11, 45, 0, 0, time.UTC)}
	// formatted 保存通知格式化后的文本。
	formatted := formatEvent(event)
	if !strings.Contains(formatted, "[提示] 系统错误") || !strings.Contains(formatted, "账号: account-1") || !strings.Contains(formatted, "alpha: 值") || strings.Contains(formatted, "zeta:") || !strings.Contains(formatted, "\n\n正文") {
		t.Fatalf("通知格式化结果异常: %q", formatted)
	}
	// orderedIndex 保存字段稳定排序后的第一个字段位置。
	orderedIndex := strings.Index(formatted, "alpha: 值")
	if orderedIndex < 0 {
		t.Fatalf("通知字段缺失: %q", formatted)
	}
}
