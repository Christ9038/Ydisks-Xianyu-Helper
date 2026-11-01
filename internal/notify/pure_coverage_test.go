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
		EventAutomationOrderCreated: "拍下改价", EventAutomationOrderPaid: "付款发货", EventAutomationBuyerReviewed: "评价赠品",
		EventAutomationReviewMissingTimeout: "求评价", EventManualDeliveryResult: "手动发货结果",
		EventManualInterventionRequired: "需要人工处理",
		EventSystemError:                "系统错误", "": "通知", "custom": "custom",
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

// TestEventAllowedWithFallbacksPreservesAutomationSubscriptions 验证独立人工处理类别兼容原自动化类别和旧交易总开关。
func TestEventAllowedWithFallbacksPreservesAutomationSubscriptions(t *testing.T) {
	if /* got 保存免拼阶段映射到的兼容自动化通知类别。 */ got := automationEventType("bargain_pending"); got != EventAutomationOrderPaid {
		t.Fatalf("免拼阶段人工处理来源类别=%q want %q", got, EventAutomationOrderPaid)
	}
	// cases 保存订阅配置、兼容来源和预期放行结果。
	cases := []struct {
		// name 是当前兼容订阅场景名称。
		name string
		// raw 是渠道持久化的事件订阅 JSON。
		raw string
		// fallbacks 是人工处理事件的来源类别。
		fallbacks []string
		// want 是预期放行结果。
		want bool
	}{
		{name: "独立人工处理", raw: `["manual_intervention_required"]`, want: true},
		{name: "原付款发货", raw: `["automation_order_paid"]`, fallbacks: []string{EventAutomationOrderPaid}, want: true},
		{name: "旧交易总开关", raw: `["delivery_result"]`, want: true},
		{name: "无关事件", raw: `["account_offline"]`, fallbacks: []string{EventAutomationOrderPaid}, want: false},
	}
	// testCase 表示当前兼容订阅场景。
	for _, testCase := range cases {
		// allowed、allowErr 保存当前订阅配置的放行结果和解析错误。
		allowed, allowErr := eventAllowedWithFallbacks(testCase.raw, EventManualInterventionRequired, testCase.fallbacks)
		if allowErr != nil || allowed != testCase.want {
			t.Fatalf("%s allowed=%v want=%v err=%v", testCase.name, allowed, testCase.want, allowErr)
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
	if !strings.HasPrefix(formatted, "【系统错误】\n") || !strings.Contains(formatted, "级别: 提示") || !strings.Contains(formatted, "账号: account-1") || !strings.Contains(formatted, "alpha: 值") || strings.Contains(formatted, "zeta:") || !strings.Contains(formatted, "\n\n正文") {
		t.Fatalf("通知格式化结果异常: %q", formatted)
	}
	// orderedIndex 保存字段稳定排序后的第一个字段位置。
	orderedIndex := strings.Index(formatted, "alpha: 值")
	if orderedIndex < 0 {
		t.Fatalf("通知字段缺失: %q", formatted)
	}
}

// TestFormatEventHeadlineCarriesTypeAndOutcome 验证首行直接给出类型与成败，且泛称标题不再占据首行。
func TestFormatEventHeadlineCarriesTypeAndOutcome(t *testing.T) {
	// cases 覆盖付款发货成败、手动发货推断、人工处理和带具体标题的掉线告警。
	cases := []struct {
		// name 是用例名称；event 是输入事件；want 是期望首行。
		name  string
		event NotificationEvent
		want  string
	}{
		{"paid-ok", NotificationEvent{Type: EventAutomationOrderPaid, Title: "自动化运行通知", Outcome: statusOutcome("success")}, "【付款发货】成功"},
		{"paid-fail", NotificationEvent{Type: EventAutomationOrderPaid, Title: "自动化运行通知", Outcome: statusOutcome("failed")}, "【付款发货】失败"},
		{"manual-fail", NotificationEvent{Type: EventManualDeliveryResult, Title: "自动发货通知", Outcome: messageOutcome("手动确认发货异常: x")}, "【手动发货结果】失败"},
		{"manual-ok", NotificationEvent{Type: EventManualDeliveryResult, Outcome: messageOutcome("手动完整发货成功")}, "【手动发货结果】成功"},
		{"review", NotificationEvent{Type: EventManualInterventionRequired, Title: "自动化需要人工处理", Outcome: statusOutcome("needs_review")}, "【需要人工处理】"},
		{"offline", NotificationEvent{Type: EventAccountOffline, Title: "账号掉线"}, "【掉线通知】 账号掉线"},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		// got 是格式化结果的首行。
		if got, _, _ := strings.Cut(formatEvent(tc.event), "\n"); got != tc.want {
			t.Fatalf("%s: 首行=%q want=%q", tc.name, got, tc.want)
		}
	}
	if messageHeadline("  \n", "兜底") != "兜底" || messageHeadline("【A】\n正文", "x") != "【A】" {
		t.Fatal("渠道标题提取异常")
	}
}
