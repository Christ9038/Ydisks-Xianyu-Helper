package automation

import (
	"context"
	"fmt"
	"log/slog"

	"xianyu-go/internal/db"
)

// deliveryNotifier 将自动化运行状态转换为可选的多渠道发货结果通知，并通过回调读取构造期固定的通知器。
type deliveryNotifier struct {
	// current 返回构造期固定的通知器实例；为空时不发送外部通知。
	current func() Notifier
	// logger 记录自动化终态通知是否进入统一通知器；不记录通知正文或渠道凭据。
	logger *slog.Logger
}

// triggerAwareNotifier 定义能够按自动化触发类别发送终态通知的可选能力。
type triggerAwareNotifier interface {
	// NotifyAutomationRunForTrigger 按具体自动化触发类别写入终态通知。
	NotifyAutomationRunForTrigger(ctx context.Context, triggerType string, runID int64, accountID, buyerID, itemID, status, message, chatID string)
}

// strictTriggerNotifier 定义能把通知入队失败返回给调用方的可选能力，供「先确认入队、再推进状态」的路径使用。
type strictTriggerNotifier interface {
	// EnqueueAutomationRunForTrigger 按触发类别写入终态通知；渠道查询或 outbox 写入失败时返回错误，无渠道不算失败。
	EnqueueAutomationRunForTrigger(ctx context.Context, triggerType string, runID int64, accountID, buyerID, itemID, status, message, chatID string) error
}

// manualInterventionNotifier 定义没有自动化运行主键的阶段异常通知能力。
type manualInterventionNotifier interface {
	// NotifyManualIntervention 按稳定业务键发送一条必须人工处理的通知。
	NotifyManualIntervention(ctx context.Context, triggerType, accountID, orderID, itemID, buyerID, action, reason, chatID, idempotencyKey string)
}

// notifyResult 根据规则执行终态发送通知；只要运行进入 success，就通知自动化已完成，避免 sent_count 为零时静默丢失结果。
// runID 与 status 会传给持久化 outbox，防止恢复扫描对同一运行重复排队。
func (n deliveryNotifier) notifyResult(ctx context.Context, task Task, runID int64, status string, sent int, errMsg string) {
	// 兼容入口只记录日志；入队失败由 notifyResultStrict 的调用方决定如何重试。
	_ = n.notifyResultStrict(ctx, task, runID, status, sent, errMsg)
}

// notifyResultStrict 与 notifyResult 相同，但在通知器支持时返回入队错误；不支持的旧实现视为已投递。
func (n deliveryNotifier) notifyResultStrict(ctx context.Context, task Task, runID int64, status string, sent int, errMsg string) error {
	// notifier 是当前可选的外部通知器。
	notifier := n.current()
	if notifier == nil {
		if n.logger != nil {
			n.logger.Warn("自动化终态通知未触发：通知器未注入", "run_id", runID, "account_id", task.AccountID, "status", status)
		}
		return nil
	}
	if n.logger != nil {
		n.logger.Info("自动化终态通知已触发", "run_id", runID, "account_id", task.AccountID, "status", status, "sent_count", sent)
	}
	// triggerName 是面向用户展示的自动化触发类型名称。
	triggerName := map[string]string{
		TriggerOrderCreated:         "拍下改价",
		TriggerOrderPaid:            "付款发货",
		TriggerBuyerReviewed:        "评价赠品",
		TriggerReviewMissingTimeout: "求评价",
	}[task.TriggerType]
	if triggerName == "" {
		triggerName = task.TriggerType
	}
	// notifyForTrigger 让新通知器按四类自动化任务分别过滤；旧替身或兼容实现继续使用统一入口。
	notifyForTrigger := func(notificationMessage string) error {
		// strict、strictOK 表示通知器是否支持返回入队错误的入口。
		if strict, strictOK := notifier.(strictTriggerNotifier); strictOK {
			return strict.EnqueueAutomationRunForTrigger(ctx, task.TriggerType, runID, task.AccountID, task.BuyerID, task.ItemID, status, notificationMessage, task.ChatID)
		}
		// triggerAware 表示通知器是否支持细分事件；ok 表示类型断言是否成功。
		if triggerAware, ok := notifier.(triggerAwareNotifier); ok {
			triggerAware.NotifyAutomationRunForTrigger(ctx, task.TriggerType, runID, task.AccountID, task.BuyerID, task.ItemID, status, notificationMessage, task.ChatID)
			return nil
		}
		notifier.NotifyAutomationRun(ctx, runID, task.AccountID, task.BuyerID, task.ItemID, status, notificationMessage, task.ChatID)
		return nil
	}
	if status == "success" {
		// message 是成功通知正文。
		message := fmt.Sprintf("✅ %s成功（订单 %s，已发送 %d 条）", triggerName, task.OrderID, sent)
		return notifyForTrigger(message)
	}
	// message 是失败或人工核对通知正文。
	message := fmt.Sprintf("🚨 %s失败（订单 %s）：%s", triggerName, task.OrderID, errMsg)
	return notifyForTrigger(message)
}

// notifyRunNeedsReview 通知运行需要人工核对，并复用统一结果通知格式。
func (n deliveryNotifier) notifyRunNeedsReview(ctx context.Context, run db.AutomationRun, reason string) {
	_ = n.notifyRunNeedsReviewStrict(ctx, run, reason)
}

// notifyRunNeedsReviewStrict 与 notifyRunNeedsReview 相同，但返回通知入队失败，供调用方在失败时保留重试机会。
func (n deliveryNotifier) notifyRunNeedsReviewStrict(ctx context.Context, run db.AutomationRun, reason string) error {
	if n.current() == nil {
		return nil
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
	return n.notifyResultStrict(ctx, task, run.ID, "needs_review", run.SentCount, "需要人工核对："+reason)
}

// notifyManualIntervention 通知没有运行主键的自动化阶段已经停止，需要用户人工处理。
func (n deliveryNotifier) notifyManualIntervention(ctx context.Context, task Task, action, reason, idempotencyKey string) {
	// notifier 是当前可选通知器；旧兼容实现不具备独立人工处理入口时只记录中文告警。
	notifier := n.current()
	if notifier == nil {
		if n.logger != nil {
			n.logger.Warn("人工处理通知未触发：通知器未注入", "account", task.AccountID, "order_id", task.OrderID, "action", action)
		}
		return
	}
	// capable 表示通知器是否支持不依赖自动化运行主键的人工处理事件。
	capable, ok := notifier.(manualInterventionNotifier)
	if !ok {
		if n.logger != nil {
			n.logger.Warn("人工处理通知未触发：通知器不支持阶段告警", "account", task.AccountID, "order_id", task.OrderID, "action", action)
		}
		return
	}
	capable.NotifyManualIntervention(ctx, task.TriggerType, task.AccountID, task.OrderID, task.ItemID, task.BuyerID, action, reason, task.ChatID, idempotencyKey)
}
