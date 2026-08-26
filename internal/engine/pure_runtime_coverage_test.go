package engine

import (
	"testing"
	"time"
)

// TestPureRuntimeHelpers 覆盖运行时消息字段递归提取、时间展示和令牌失败计数规则。
func TestPureRuntimeHelpers(t *testing.T) {
	// nestedMessage 保存包含大小写混合字段和多层嵌套信封的消息样本。
	nestedMessage := map[string]any{"Envelope": map[string]any{"senderId": "  buyer-42  "}}
	// senderID 保存递归提取后的买家标识。
	senderID := extractNestedString(nestedMessage, "sender_id", "senderId")
	if senderID != "buyer-42" {
		t.Fatalf("sender id=%q", senderID)
	}
	// missingValue 保存不存在字段的递归提取结果。
	missingValue := extractNestedString(map[string]any{"nested": map[string]any{"sender_id": "  "}}, "sender_id")
	if missingValue != "" {
		t.Fatalf("blank nested value=%q", missingValue)
	}
	// zeroTimeResult 保存零时间的用户可见展示文本。
	zeroTimeResult := formatTimeOrUnknown(time.Time{})
	if zeroTimeResult != "未知" {
		t.Fatalf("zero time=%q", zeroTimeResult)
	}
	// knownTimeResult 保存固定时间的用户可见展示文本。
	knownTimeResult := formatTimeOrUnknown(time.Date(2024, time.March, 1, 2, 3, 4, 0, time.Local))
	if knownTimeResult != "2024-03-01 02:03:04" {
		t.Fatalf("known time=%q", knownTimeResult)
	}
	// countedStatuses 保存必须计入账号失败计数的令牌刷新状态。
	countedStatuses := []string{tokenRefreshFailedAPI, tokenRefreshFailedNetwork, tokenRefreshFailedTimeout, tokenRefreshFailedSession}
	// status 表示当前待判断的令牌刷新状态。
	for _, status := range countedStatuses {
		if tokenFailureIsNonCounted(status) {
			t.Fatalf("status %q should be counted", status)
		}
	}
	// nonCountedStatuses 保存风控冷却等不应累计为连接失败的状态。
	nonCountedStatuses := []string{tokenRefreshFailedCaptcha, tokenRefreshFailedCaptchaError, tokenRefreshSkippedCooldown}
	// status 表示当前待判断的不计数令牌刷新状态。
	for _, status := range nonCountedStatuses {
		if !tokenFailureIsNonCounted(status) {
			t.Fatalf("status %q should not be counted", status)
		}
	}
}
