package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestAutomationNotificationsCoverOptionalAndTerminalBranches 验证自动化通知器的可选依赖、成功空跑、失败和人工核对通知。
func TestAutomationNotificationsCoverOptionalAndTerminalBranches(t *testing.T) {
	// ctx 是通知测试共用的上下文。
	ctx := context.Background()
	// nilNotifier 验证未装配通知器时所有入口都安全跳过。
	nilNotifier := deliveryNotifier{current: func() Notifier { return nil }}
	nilNotifier.notifyResult(ctx, Task{TriggerType: TriggerBuyerReviewed}, 1, "success", 1, "")
	nilNotifier.notifyRunNeedsReview(ctx, db.AutomationRun{}, "no notifier")
	// notifier 保存记录通知调用的本地替身。
	notifier := &recordingNotifier{}
	// delivery 保存固定通知器的通知协调器。
	delivery := deliveryNotifier{current: func() Notifier { return notifier }}
	// task 保存成功通知展示所需的订单事实。
	task := Task{AccountID: "account", BuyerID: "buyer", ItemID: "item", ChatID: "chat", OrderID: "order", TriggerType: TriggerOrderPaid}
	delivery.notifyResult(ctx, task, 2, "success", 0, "empty")
	if len(notifier.messages()) != 0 {
		t.Fatal("成功但没有发送内容时不应通知")
	}
	delivery.notifyResult(ctx, task, 2, "success", 1, "")
	delivery.notifyResult(ctx, Task{TriggerType: "unknown_trigger", OrderID: "order"}, 3, "failed", 0, "failed")
	delivery.notifyRunNeedsReview(ctx, db.AutomationRun{ID: 4, CookieID: "account", BuyerID: "buyer", ItemID: "item", ChatID: "chat", OrderID: "order", TriggerType: TriggerBuyerReviewed}, "uncertain")
	if len(notifier.messages()) != 3 {
		t.Fatalf("终态通知数量=%d want 3", len(notifier.messages()))
	}
	// nilCenter 验证 Center 兼容通知入口的空接收者保护。
	var nilCenter *Center
	nilCenter.notifyRunNeedsReview(ctx, db.AutomationRun{}, "nil center")
	// store、cleanup 保存唤醒凭证阻断任务测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 保存具备自动化存储的通知中心。
	center := New(store, nil, nil)
	center.wakeCredentialBlockedAutomation(ctx, "")
	// closeErr 保存数据库关闭错误，随后用于覆盖唤醒写入失败日志路径。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	center.wakeCredentialBlockedAutomation(ctx, "account")
	// emptyCenter 验证缺少存储的中心不会尝试执行数据库 I/O。
	emptyCenter := &Center{}
	emptyCenter.wakeCredentialBlockedAutomation(ctx, "account")
}
