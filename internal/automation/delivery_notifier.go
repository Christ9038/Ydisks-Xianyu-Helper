package automation

import (
	"context"
	"fmt"

	"xianyu-go/internal/db"
)

// deliveryNotifier 将自动化运行状态转换为可选的多渠道发货结果通知，并通过回调读取构造期固定的通知器。
type deliveryNotifier struct {
	// current 返回构造期固定的通知器实例；为空时不发送外部通知。
	current func() Notifier
}

// notifyResult 根据规则执行结果发送通知，成功且实际发出内容时才发送成功通知。
// runID 与 status 会传给持久化 outbox，防止恢复扫描对同一运行重复排队。
func (n deliveryNotifier) notifyResult(ctx context.Context, task Task, runID int64, status string, sent int, errMsg string) {
	// notifier 是当前可选的外部通知器。
	notifier := n.current()
	if notifier == nil {
		return
	}
	// triggerName 是面向用户展示的自动化触发类型名称。
	triggerName := map[string]string{
		TriggerOrderPaid:            "付款发货",
		TriggerBuyerReviewed:        "评价赠品",
		TriggerReviewMissingTimeout: "求评价",
	}[task.TriggerType]
	if triggerName == "" {
		triggerName = task.TriggerType
	}
	if status == "success" {
		if sent <= 0 {
			return
		}
		// message 是成功通知正文。
		message := fmt.Sprintf("✅ %s成功（订单 %s，已发送 %d 条）", triggerName, task.OrderID, sent)
		notifier.NotifyAutomationRun(ctx, runID, task.AccountID, task.BuyerID, task.ItemID, status, message, task.ChatID)
		return
	}
	// message 是失败或人工核对通知正文。
	message := fmt.Sprintf("🚨 %s失败（订单 %s）：%s", triggerName, task.OrderID, errMsg)
	notifier.NotifyAutomationRun(ctx, runID, task.AccountID, task.BuyerID, task.ItemID, status, message, task.ChatID)
}

// notifyRunNeedsReview 通知运行需要人工核对，并复用统一结果通知格式。
func (n deliveryNotifier) notifyRunNeedsReview(ctx context.Context, run db.AutomationRun, reason string) {
	if n.current() == nil {
		return
	}
	// task 是从运行快照构造出的最小通知上下文。
	task := Task{
		AccountID:   run.CookieID,
		BuyerID:     run.BuyerID,
		ItemID:      run.ItemID,
		ChatID:      run.ChatID,
		OrderID:     run.OrderID,
		TriggerType: run.TriggerType,
	}
	n.notifyResult(ctx, task, run.ID, "needs_review", run.SentCount, "需要人工核对："+reason)
}
