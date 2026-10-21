package notify

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestEnqueueAutomationRunForTriggerReportsEnqueueFailure 验证严格入口把 outbox 写入失败返回给调用方，
// 成功入队、无启用渠道和无效入参都不算失败，且兼容入口仍只记录日志。
func TestEnqueueAutomationRunForTriggerReportsEnqueueFailure(t *testing.T) {
	// repository 提供订阅人工处理类别的渠道，并可注入入队失败。
	repository := &queueRepository{scriptedOutboxRepository: &scriptedOutboxRepository{}, channels: []db.NotificationChannel{{ID: 1, Type: "webhook"}}}
	// notifier 使用该仓储。
	notifier := NewWithRepository("cid", repository, nilLogger())
	// ctx 是不会取消的测试上下文。
	ctx := context.Background()

	repository.enqueueErr = errors.New("outbox down")
	// err 保存各次严格入队的返回错误。
	if err := notifier.EnqueueAutomationRunForTrigger(ctx, "order_paid", 7, "cid", "b", "i", "needs_review", "msg", "chat"); err == nil {
		t.Fatal("outbox 写入失败应返回错误")
	}
	// 兼容入口不得 panic，也不向调用方暴露错误。
	notifier.NotifyAutomationRunForTrigger(ctx, "order_paid", 7, "cid", "b", "i", "needs_review", "msg", "chat")

	repository.enqueueErr = nil
	// okErr 保存成功入队的返回错误。
	if okErr := notifier.EnqueueAutomationRunForTrigger(ctx, "order_paid", 7, "cid", "b", "i", "needs_review", "msg", "chat"); okErr != nil {
		t.Fatalf("成功入队不应返回错误: %v", okErr)
	}
	if len(repository.enqueued) != 1 || repository.enqueued[0].IdempotencyKey != "automation-run:7:needs_review" {
		t.Fatalf("入队内容异常: %+v", repository.enqueued)
	}
	// invalidErr 保存无效入参的返回错误。
	if invalidErr := notifier.EnqueueAutomationRunForTrigger(ctx, "order_paid", 0, "cid", "b", "i", "needs_review", "msg", "chat"); invalidErr != nil {
		t.Fatalf("无效入参按忽略处理: %v", invalidErr)
	}
	// empty 是没有任何启用渠道的账号仓储：无需投递不算失败。
	empty := NewWithRepository("cid", &queueRepository{scriptedOutboxRepository: &scriptedOutboxRepository{}}, nilLogger())
	// emptyErr 保存无渠道账号的返回错误。
	if emptyErr := empty.EnqueueAutomationRunForTrigger(ctx, "order_paid", 8, "cid", "b", "i", "needs_review", "msg", "chat"); emptyErr != nil {
		t.Fatalf("无渠道不应返回错误: %v", emptyErr)
	}
}
