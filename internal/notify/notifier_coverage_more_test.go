package notify

import (
	"context"
	"testing"
)

// TestNotifyAutomationRunIgnoresInvalidInputs 验证自动化终态通知对空通知器、无效运行 ID 和空状态保持幂等忽略。
func TestNotifyAutomationRunIgnoresInvalidInputs(t *testing.T) {
	// ctx 是本测试通知入口使用的非取消上下文。
	ctx := context.Background()
	// nilNotifier 验证空接收者不会触发数据库或网络操作。
	var nilNotifier *Notifier
	nilNotifier.NotifyAutomationRun(ctx, 1, "cid", "buyer", "item", "success", "body", "chat")
	// store、cleanup 保存用于验证无效输入不入队的本地通知存储。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// notifier 是未启动 worker 的本地通知器。
	notifier := New("cid", store, nil)
	notifier.NotifyAutomationRun(ctx, 0, "cid", "buyer", "item", "success", "body", "chat")
	notifier.NotifyAutomationRun(ctx, 1, "cid", "buyer", "item", " ", "body", "chat")
	// count 保存无效输入被忽略后的 outbox 记录数。
	var count int
	// countErr 保存读取 outbox 记录数的数据库错误。
	if countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox`).Scan(&count); countErr != nil {
		t.Fatal(countErr)
	}
	if count != 0 {
		t.Fatalf("无效自动化通知不应入队：count=%d", count)
	}
}
