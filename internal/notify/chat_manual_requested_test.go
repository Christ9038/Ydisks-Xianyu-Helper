package notify

import (
	"strings"
	"testing"
	"time"
)

// TestChatManualRequestedEventFormattingAndSubscription 验证“人工客服”通知首行带类别标签与具体标题，且只被显式订阅或全订阅渠道接收，不受旧版统一交易开关放行。
func TestChatManualRequestedEventFormattingAndSubscription(t *testing.T) {
	// full 是格式化后的通知全文。
	full := formatEvent(NotificationEvent{AccountID: "acc1", Type: EventChatManualRequested, Level: "warn", Title: "买家请求人工客服", Body: "小明 请求人工客服", Time: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)})
	if !strings.HasPrefix(full, "【人工客服】 买家请求人工客服") || !strings.Contains(full, "小明 请求人工客服") {
		t.Fatalf("通知格式错误: %s", full)
	}
	// cases 是订阅配置到预期放行结果的映射。
	cases := map[string]bool{
		"":                                      true,
		`["chat_manual_requested"]`:             true,
		`["delivery_result"]`:                   false,
		`["manual_intervention_required"]`:      false,
		"account_offline,chat_manual_requested": true,
	}
	// raw、want 是订阅配置与预期结果。
	for raw, want := range cases {
		// allowed、err 是订阅判断结果。
		allowed, err := eventAllowed(raw, EventChatManualRequested)
		if err != nil || allowed != want {
			t.Fatalf("订阅 %q: allowed=%v want=%v err=%v", raw, allowed, want, err)
		}
	}
}
