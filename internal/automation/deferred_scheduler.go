package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"xianyu-go/internal/db"
)

// scanDeferredTasks 领取并重放已到期延迟动作；错误独立记录，避免影响分钟级扫描的调度节奏。
func (s *Scheduler) scanDeferredTasks(ctx context.Context) {
	// deferredErr 保存本轮延迟动作状态收口错误，必须记录以便管理员追踪人工核对任务。
	deferredErr := s.runDeferredTasks(ctx)
	if deferredErr != nil {
		s.center.logger.Error("自动化延迟任务状态收口失败", "err", deferredErr)
	}
}

// runDeferredTasks 封装运行Deferred任务列表业务协调。
func (s *Scheduler) runDeferredTasks(ctx context.Context) error {
	// resultErr 汇总延迟任务最终状态写入失败，避免领取成功后状态异常被静默吞掉。
	var resultErr error
	// tasks、err 用于本次流程后续判断的tasks、err
	tasks, err := s.center.store.Automation.ClaimDueDeferredTasks(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描暂停期间自动化事件失败", "err", err)
		return err
	}
	// blockedAccounts 记录本轮已遇到风控、会话失效或 Token 耗尽的账号；这些账号的剩余任务不再请求平台，
	// 已领取但被跳过的任务保持 running，租约到期后才会被重新领取，形成天然冷却。
	blockedAccounts := map[string]struct{}{}
	// pending 表示当前遍历过程中的pending
	for _, pending := range tasks {
		// blocked 表示该延迟任务所属账号本轮已被平台阻断，其任务保持 running 直到租约到期。
		if _, blocked := blockedAccounts[pending.CookieID]; blocked {
			s.center.logger.Info("账号本轮已触发平台阻断，跳过其余延迟任务", "task_id", pending.ID, "account", pending.CookieID)
			continue
		}
		// task 用于本次流程后续判断的任务
		var task Task
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(pending.TaskJSON), &task); err != nil {
			// failureReason 是写入重试状态和人工处理通知共用的解析失败原因。
			failureReason := "解析任务失败: " + err.Error()
			// finishErr 表示解析失败后写入延迟任务重试或死信状态时的错误。
			finishErr := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, false, failureReason)
			if finishErr != nil {
				s.center.logger.Error("保存解析失败的暂停事件状态失败", "task_id", pending.ID, "err", finishErr)
				s.notifyDeferredTaskNeedsReview(ctx, pending, Task{AccountID: pending.CookieID, TriggerType: pending.TriggerType}, failureReason+"；保存任务状态失败："+finishErr.Error())
				resultErr = errors.Join(
					resultErr,
					errAutomationNeedsReview,
					fmt.Errorf("保存解析失败的暂停事件状态失败: %w", finishErr),
				)
			} else {
				s.center.logger.Warn("暂停期间自动化事件重放失败", "task_id", pending.ID, "account", pending.CookieID, "err", err)
				if pending.ClaimVersion >= 5 {
					s.notifyDeferredTaskNeedsReview(ctx, pending, Task{AccountID: pending.CookieID, TriggerType: pending.TriggerType}, failureReason+"；已达到自动重试上限")
				}
			}
			continue
		}
		if task.TriggerType == TriggerBargainPending {
			// 免拼只能由「待刀成」WebSocket 实时阶段触发，调度器重放不得调用免拼接口：延期期间订单阶段可能已变化。
			// 这里直接结束该延期任务（不再重试），并通知人工核对订单后自行处理。
			// reason 是通知给人工的说明，不含凭证。
			reason := "账号暂停期间收到「待刀成」阶段，免拼只能由实时 WebSocket 阶段触发，已取消自动重放，请核对订单后手动处理"
			// finishErr 是结束延期任务的写入结果；失败时任务会在租约到期后再次进入本分支，通知按任务 ID 幂等。
			finishErr := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, true, "")
			if finishErr != nil {
				reason += "；保存任务状态失败：" + finishErr.Error()
				resultErr = errors.Join(resultErr, errAutomationNeedsReview, fmt.Errorf("保存免拼重放取消状态失败: %w", finishErr))
			}
			s.center.logger.Warn("暂停期间的待刀成事件不再重放，已通知人工处理", "task_id", pending.ID, "account", task.AccountID, "order_id", task.OrderID)
			s.notifyDeferredTaskNeedsReview(ctx, pending, task, reason)
			continue
		}
		if task.Raw == nil {
			task.Raw = map[string]any{}
		}
		task.Raw["automation_deferred_replay"] = true
		// deferredAgain、runErr 用于本次流程后续判断的deferredAgain、runErr
		deferredAgain, runErr := s.center.handleTask(ctx, task)
		if isPlatformBlockErr(runErr) {
			blockedAccounts[pending.CookieID] = struct{}{}
		}
		if deferredAgain {
			// handleTask 已按新的 paused_until 重置同一任务；当前 claim 不再删除。
			s.center.logger.Info("暂停期间自动化事件重放再次延期", "task_id", pending.ID, "account", task.AccountID, "trigger", task.TriggerType)
			continue
		}
		// finishErr 保存暂停事件重放终态的持久化错误；只有它成功后才记录重放成功或失败。
		finishErr := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, runErr == nil, errorString(runErr))
		if finishErr != nil {
			s.center.logger.Warn("保存暂停事件重放结果失败", "task_id", pending.ID, "err", finishErr)
			s.notifyDeferredTaskNeedsReview(ctx, pending, task, "暂停事件重放后无法保存任务状态："+finishErr.Error())
			resultErr = errors.Join(resultErr, errAutomationNeedsReview, runErr, fmt.Errorf("保存暂停事件重放结果失败: %w", finishErr))
			continue
		}
		if runErr == nil {
			s.center.logger.Info("暂停期间自动化事件重放成功", "task_id", pending.ID, "account", task.AccountID, "trigger", task.TriggerType)
		} else {
			s.center.logger.Warn("暂停期间自动化事件重放失败", "task_id", pending.ID, "account", task.AccountID, "trigger", task.TriggerType, "err", runErr)
			if pending.ClaimVersion >= 5 {
				s.notifyDeferredTaskNeedsReview(ctx, pending, task, "暂停事件连续重放失败并已达到自动重试上限："+runErr.Error())
			}
		}
	}
	return resultErr
}

// notifyDeferredTaskNeedsReview 为进入死信或无法安全收口的延期自动化任务发送一次人工处理通知。
func (s *Scheduler) notifyDeferredTaskNeedsReview(ctx context.Context, pending db.DeferredAutomationTask, task Task, reason string) {
	if s == nil || s.center == nil {
		return
	}
	if task.AccountID == "" {
		task.AccountID = pending.CookieID
	}
	if task.TriggerType == "" {
		task.TriggerType = pending.TriggerType
	}
	s.center.audit.record(ctx, task, auditEvent{eventType: auditEventDeadLetter, status: db.AuditStatusNeedsReview, summary: "延期自动化任务转人工处理", reason: reason})
	// notificationKey 是同一延期任务共享的稳定人工处理通知键，重复扫描不会制造重复告警。
	notificationKey := fmt.Sprintf("manual-intervention:deferred-task:%d", pending.ID)
	// notifyCtx 保证任务状态写失败或原始重放预算取消后，告警仍有独立的短时入队预算。
	notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
	s.center.notifications.notifyManualIntervention(notifyCtx, task, "暂停自动化事件重放", reason, notificationKey)
	notifyCancel()
}
