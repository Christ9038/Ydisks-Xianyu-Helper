package automation

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/db"
)

// handleBargainPending 处理砍价“待刀成”WS 阶段：仅按独立账号开关调用免拼，绝不匹配发卡或确认发货规则。
func (c *Center) handleBargainPending(ctx context.Context, task Task) (bool, error) {
	if task.Source != "ws" {
		c.logger.Warn("拒绝非 WebSocket 的免拼阶段任务", "source", task.Source, "account", task.AccountID, "order_id", task.OrderID)
		return false, nil
	}
	// autoBargain、settingsErr 保存独立自动免拼开关和读取错误。
	autoBargain, settingsErr := c.store.Cookies.GetAutoBargain(ctx, task.AccountID)
	if settingsErr != nil {
		return false, fmt.Errorf("读取自动免拼设置: %w", settingsErr)
	}
	if !autoBargain {
		c.logger.Info("账号未启用自动免拼，跳过待刀成阶段", "account", task.AccountID, "order_id", task.OrderID)
		c.audit.skipped(ctx, task, "账号未启用自动免拼，不处理待刀成阶段")
		return false, nil
	}
	if task.OrderID == "" {
		return false, fmt.Errorf("免拼阶段缺少订单ID")
	}
	// claimed、claimErr 保存本次 WS 是否取得免拼阶段唯一执行权。
	claimed, claimErr := c.store.Automation.ClaimBargainFreeShipping(ctx, task.OrderID, task.AccountID)
	if claimErr != nil {
		return false, fmt.Errorf("领取免拼阶段执行权: %w", claimErr)
	}
	if !claimed {
		c.logger.Info("免拼阶段已有其他任务处理，跳过重复事件", "account", task.AccountID, "order_id", task.OrderID)
		c.audit.skipped(ctx, task, "免拼阶段已有其他任务处理，重复事件已忽略")
		return false, nil
	}
	c.logger.Info("开始执行自动免拼", "account", task.AccountID, "order_id", task.OrderID, "trigger", task.TriggerType)
	// actionErr 保存独立免拼接口的执行结果。
	actionErr := c.actions.freeShipBargain(ctx, task)
	// status 保存可重试的明确失败、需要人工核对的未知结果或成功终态。
	status := "succeeded"
	if actionErr != nil {
		// uncertain 用于识别可能已被平台执行、因而不能自动再次提交的免拼结果。
		var uncertain *uncertainActionError
		if errors.As(actionErr, &uncertain) {
			status = "needs_review"
		} else {
			status = "failed"
		}
		c.logger.Warn("自动免拼失败，已保存阶段状态", "account", task.AccountID, "order_id", task.OrderID, "status", status, "err", actionErr)
	} else {
		c.logger.Info("自动免拼成功，等待成功小刀消息后发卡", "account", task.AccountID, "order_id", task.OrderID)
	}
	c.recordBargainResult(ctx, task, status, actionErr)
	// finishErr 保存免拼阶段终态写入错误，避免远端成功后丢失兜底资格。
	if finishErr := c.store.Automation.FinishBargainFreeShipping(ctx, task.OrderID, task.AccountID, status); finishErr != nil {
		// reviewReason 说明阶段收口失败后为何不能再次自动免拼。
		reviewReason := "免拼请求已经执行，但本地阶段状态保存失败，禁止自动重试，请核对平台订单状态：" + finishErr.Error()
		if actionErr != nil {
			reviewReason = "免拼请求结果和本地阶段状态均无法确认，禁止自动重试，请核对平台订单状态：" + errors.Join(actionErr, finishErr).Error()
		}
		// notifyCtx 保证原始请求取消后，人工处理通知仍有独立的短时入队预算。
		notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
		c.notifications.notifyManualIntervention(notifyCtx, task, "二人小刀免拼", reviewReason, bargainManualInterventionKey(task))
		notifyCancel()
		if actionErr != nil {
			return false, errors.Join(actionErr, fmt.Errorf("收口免拼阶段: %w", finishErr))
		}
		return false, uncertainAction(fmt.Errorf("闲鱼已免拼，但本地阶段保存失败: %w", finishErr))
	}
	if status == "needs_review" {
		// notifyCtx 保证免拼结果不确定时的人工处理通知不受平台调用上下文取消影响。
		notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
		c.notifications.notifyManualIntervention(notifyCtx, task, "二人小刀免拼", actionErr.Error(), bargainManualInterventionKey(task))
		notifyCancel()
	}
	return false, actionErr
}

// bargainManualInterventionKey 返回同一账号、订单、免拼阶段共享的通知幂等键，重复 WS 不会制造重复告警。
func bargainManualInterventionKey(task Task) string {
	return fmt.Sprintf("manual-intervention:bargain-free-shipping:%s:%s", task.AccountID, task.OrderID)
}

// recordBargainResult 把免拼阶段的执行结果追加到审计表；stageStatus 取阶段表状态 succeeded、failed 或 needs_review。
func (c *Center) recordBargainResult(ctx context.Context, task Task, stageStatus string, actionErr error) {
	// auditStatus 把阶段表的 succeeded 映射为审计统一的 success。
	auditStatus := stageStatus
	if stageStatus == "succeeded" {
		auditStatus = db.AuditStatusSuccess
	}
	// reason 只在失败或待人工核对时携带错误文本。
	reason := ""
	if actionErr != nil {
		reason = actionErr.Error()
	}
	c.audit.record(ctx, task, auditEvent{eventType: auditEventBargainFreeShip, status: auditStatus, summary: "二人小刀自动免拼", reason: reason})
}
