package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
)

// automationRunCompensationTimeout 限制动作取消后的状态补偿写入时间，避免安全收口无限等待数据库。
const automationRunCompensationTimeout = 5 * time.Second

// newAutomationRunCompensationContext 基于调用方的值域创建不受取消影响的短时运行收口上下文。
// 运行状态、人工核对和通知等安全收口必须在外部动作预算结束后继续完成，避免留下可重复执行的 running 记录。
func newAutomationRunCompensationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), automationRunCompensationTimeout)
}

// automationRunCoordinator 负责自动化运行的创建、续租、动作检查点和结果收口；它不决定具体业务动作，只协调动作执行过程中的持久化状态和三态结果。
type automationRunCoordinator struct {
	// store 提供自动化运行、延迟任务和人工核对状态的持久化能力。
	store *db.Store
	// planner 根据事件和规则生成动作计划，保证协调器不直接解释规则细节。
	planner actionPlanner
	// logger 记录运行状态收口和检查点异常。
	logger *slog.Logger
	// audit 记录运行创建、动作结果和终态的审计事件；零值为空操作。
	audit auditRecorder
	// prepareTask 在创建或恢复运行前补全订单事实和凭证上下文。
	prepareTask func(context.Context, Task) (Task, error)
	// actionDelaySeconds 计算当前动作的有效延迟时间。
	actionDelaySeconds func(context.Context, db.AutomationAction) (int, error)
	// accountAutomationAllowed 检查账号是否仍允许执行外部自动化动作。
	accountAutomationAllowed func(context.Context, string) (bool, error)
	// accountSenderReady 判断账号是否已具备可立即发送消息的闲鱼 WebSocket 连接。
	accountSenderReady func(string) bool
	// deferTask 持久化等待延迟后继续执行的任务。
	deferTask func(context.Context, Task, int64) error
	// executeAction 执行一个已经通过账号门禁的具体外部动作，并返回当前运行内的短暂发货凭证。
	executeAction func(context.Context, Task, db.AutomationAction, shipmentDeliveryProof) (actionExecutionResult, error)
	// hasNotifier 判断当前是否注入了结果通知器。
	hasNotifier func() bool
	// notifyResult 将运行结果转换为用户可见的、按运行终态幂等的通知。
	notifyResult func(context.Context, Task, int64, string, int, string)
	// orderDetailAvailable 判断是否配置了订单详情查询器；为 nil 或返回 false 时金额缺失无从补全，不据此拦截发货。
	orderDetailAvailable func() bool
}

// executeRule 创建或恢复一次自动化运行，并统一处理运行成功、失败、延期和人工核对结果；resultErr 返回动作执行或结果收口错误。
func (r automationRunCoordinator) executeRule(ctx context.Context, task Task, rule db.AutomationRule) (resultErr error) {
	// preparedTask 是补全订单事实和凭证上下文后的任务；run 是本次独占或恢复的运行状态。
	preparedTask, run, skipped, prepareErr := r.prepareRuleRun(ctx, task, rule)
	if prepareErr != nil {
		return prepareErr
	}
	if skipped {
		return nil
	}
	if run == nil {
		return errors.New("自动化运行记录缺失，已停止执行外部动作")
	}
	task = preparedTask
	// 把运行关联标识挂到 ctx：动作执行中的 MTOP 请求、发卡、通知等下游日志无需逐处传参即可按 run_id 串联。
	ctx = logging.With(ctx,
		slog.Int64("run_id", run.ID),
		slog.String("account", task.AccountID),
		slog.String("order_id", task.OrderID),
		slog.String("trigger", string(task.TriggerType)),
		slog.Int64("rule_id", rule.ID),
	)
	// runStarted 是本次运行开始执行动作的时刻，用于输出运行耗时。
	runStarted := time.Now()
	r.audit.record(ctx, task, auditEvent{eventType: auditEventRunStarted, status: db.AuditStatusRunning, ruleID: rule.ID, runID: run.ID, summary: fmt.Sprintf("开始执行，共 %d 个动作", len(task.ActionPlan))})
	// status 是运行完成时写入数据库的结果状态。
	status := "success"
	// errMsg 是运行完成时记录的可重试或人工核对原因。
	errMsg := ""
	// sent 是截至当前检查点已经确认成功的动作数量。
	sent := run.SentCount
	// finish 表示函数返回时是否应执行正常运行收口。
	finish := true
	defer func() {
		if !finish {
			return
		}
		// finishCtx 限制运行收口不能被原始请求取消。
		finishCtx, cancel := newAutomationRunCompensationContext(ctx)
		// cancel 在结果通知已使用 finishCtx 持久化入队后释放收口上下文，避免取消导致通知静默丢失。
		defer cancel()
		// finishErr 保存运行结果写入失败，避免覆盖原始动作错误。
		if finishErr := r.store.Automation.FinishRun(finishCtx, run.ID, run.AttemptCount, status, sent, errMsg); finishErr != nil {
			// reason 说明结果落库失败后禁止自动重放的原因，并用于人工核对记录。
			reason := "自动化运行结果保存失败，已停止自动重放，请人工核对: " + finishErr.Error()
			// status 和 errMsg 让统一通知明确告知结果未知，避免误报成功或失败可重试。
			status, errMsg = "needs_review", reason
			// quarantineErr 保存将外部动作结果转为人工核对状态时的错误。
			quarantineErr := r.store.Automation.QuarantineRunResult(finishCtx, run.ID, run.AttemptCount, sent, reason)
			if quarantineErr != nil {
				r.logger.Error("保存自动化执行结果和人工核对状态均失败", "run_id", run.ID, "finish_err", finishErr, "quarantine_err", quarantineErr)
				resultErr = errors.Join(resultErr, errAutomationNeedsReview, fmt.Errorf("保存自动化执行结果失败: %w", finishErr), fmt.Errorf("保存人工核对状态失败: %w", quarantineErr))
			} else {
				resultErr = errors.Join(resultErr, errAutomationNeedsReview, fmt.Errorf("保存自动化执行结果失败: %w", finishErr))
			}
		}
		if r.hasNotifier() {
			r.notifyResult(finishCtx, task, run.ID, status, sent, errMsg)
		}
		r.audit.runFinished(ctx, task, rule.ID, run.ID, status, sent, errMsg)
		// 运行终态摘要：成功为 Info，失败为 Warn，转人工核对为 Error；run_id 等关联字段来自 ctx。
		switch status {
		case "success":
			r.logger.InfoContext(ctx, "自动化规则执行成功", "status", status, "sent_count", sent, "duration", time.Since(runStarted).Round(time.Millisecond))
		case "needs_review":
			r.logger.ErrorContext(ctx, "自动化规则转人工核对", "status", status, "sent_count", sent, "duration", time.Since(runStarted).Round(time.Millisecond), "reason", errMsg)
		default:
			r.logger.WarnContext(ctx, "自动化规则执行失败", "status", status, "sent_count", sent, "duration", time.Since(runStarted).Round(time.Millisecond), "reason", errMsg)
		}
	}()
	// actions 是当前规则生成的完整动作计划。
	actions := task.ActionPlan
	if task.TriggerType == TriggerOrderPaid && !r.planner.hasMatchingSendCard(task, actions) {
		status, errMsg = "failed", "未匹配到订单规格对应的卡密动作"
		return errors.New(errMsg)
	}
	// deferred 表示动作已写入延迟队列；actionErr 表示动作执行或检查点失败。
	var deferred bool
	// actionErr 表示动作执行或检查点持久化失败，成功时为 nil。
	var actionErr error
	sent, deferred, actionErr = r.executeRunActions(ctx, task, rule.ID, run, actions, false)
	if deferred {
		finish = false
		return errAutomationDeferred
	}
	if errors.Is(actionErr, errAutomationNeedsReview) {
		finish = false
		if r.hasNotifier() {
			// notifyCtx 保证人工核对通知在动作预算取消后仍能进入持久化通知链路。
			notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
			r.notifyResult(notifyCtx, task, run.ID, "needs_review", sent, actionErr.Error())
			notifyCancel()
		}
		r.logger.ErrorContext(ctx, "自动化规则转人工核对", "status", "needs_review", "sent_count", sent, "duration", time.Since(runStarted).Round(time.Millisecond), "reason", actionErr.Error())
		r.audit.runFinished(ctx, task, rule.ID, run.ID, db.AuditStatusNeedsReview, sent, actionErr.Error())
		return actionErr
	}
	if actionErr != nil {
		if sent > 0 && !errors.Is(actionErr, ErrMessageNotSent) && !errors.Is(actionErr, errActionNotPerformed) {
			// reason 说明部分动作成功后为何必须人工核对。
			reason := "运行已完成部分动作，后续动作失败，已禁止从头自动重放: " + actionErr.Error()
			// quarantineCtx 保证部分成功运行的人工核对状态不被已取消的动作上下文阻断。
			quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
			// quarantineErr 保存部分成功运行的人工核对状态。
			quarantineErr := r.store.Automation.QuarantineRunResult(quarantineCtx, run.ID, run.AttemptCount, sent, reason)
			quarantineCancel()
			if quarantineErr != nil {
				finish = false
				r.logger.Error("保存自动化人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return errors.Join(errAutomationNeedsReview, errAutomationQuarantine, actionErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				// notifyCtx 保证部分成功运行的人工核对通知不被动作预算取消阻断。
				notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
				r.notifyResult(notifyCtx, task, run.ID, "needs_review", sent, reason)
				notifyCancel()
			}
			r.logger.ErrorContext(ctx, "自动化规则转人工核对", "status", "needs_review", "sent_count", sent, "duration", time.Since(runStarted).Round(time.Millisecond), "reason", reason)
			r.audit.runFinished(ctx, task, rule.ID, run.ID, db.AuditStatusNeedsReview, sent, reason)
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, actionErr)
		}
		status, errMsg = "failed", actionErr.Error()
		if errors.Is(actionErr, ErrMessageNotSent) || errors.Is(actionErr, errActionNotPerformed) {
			errMsg = db.SafeRetryErrorPrefix + errMsg
		}
		return actionErr
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		// incrementErr 保存求评价消息成功后的提醒次数。
		if incrementErr := r.store.Automation.IncrementReviewRequest(ctx, task.OrderID); incrementErr != nil {
			// reason 记录外部求评价消息已发送而本地计数未落库的原因，用于隔离运行并通知人工核对。
			reason := "求评价消息已发送，但保存提醒次数失败，已停止自动重放: " + incrementErr.Error()
			// quarantineCtx 保证求评价运行的人工核对状态在请求取消后仍能落库。
			quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
			// quarantineErr 保存提醒次数写入失败的人工核对状态。
			quarantineErr := r.store.Automation.QuarantineRunResult(quarantineCtx, run.ID, run.AttemptCount, sent, reason)
			quarantineCancel()
			if quarantineErr != nil {
				finish = false
				r.logger.Error("保存求评价人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return errors.Join(errAutomationNeedsReview, errAutomationQuarantine, incrementErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				// notifyCtx 保证求评价人工核对通知在请求取消后仍能进入通知链路。
				notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
				r.notifyResult(notifyCtx, task, run.ID, "needs_review", sent, reason)
				notifyCancel()
			}
			r.logger.ErrorContext(ctx, "自动化规则转人工核对", "status", "needs_review", "sent_count", sent, "duration", time.Since(runStarted).Round(time.Millisecond), "reason", reason)
			r.audit.runFinished(ctx, task, rule.ID, run.ID, db.AuditStatusNeedsReview, sent, reason)
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, incrementErr)
		}
	}
	return nil
}

// prepareRuleRun 先补全任务事实和动作计划，再恢复既有运行或原子创建新的幂等运行；skipped 为 true 表示重复事件或已失效恢复任务无需执行。
func (r automationRunCoordinator) prepareRuleRun(ctx context.Context, task Task, rule db.AutomationRule) (preparedTask Task, run *db.AutomationRun, skipped bool, err error) {
	// AllowAllItems 固化当前规则的显式全商品授权；恢复历史运行时仍以当前有效规则授权为准，不从订单规格推断通配范围。
	task.AllowAllItems = ruleAllowsAllItems(rule, task.TriggerType)
	if len(task.ActionPlan) == 0 && task.TriggerType != TriggerOrderPaid {
		task.ActionPlan = r.planner.plan(task, rule.Actions)
	}
	// preparedTask 是订单事实和凭证上下文补全后的任务；prepareErr 表示准备阶段的可见失败。
	preparedTask, prepareErr := r.prepareTask(ctx, task)
	if prepareErr != nil {
		return Task{}, nil, false, &preparationError{err: prepareErr}
	}
	// blockedReason 是付款发货前对订单最新状态的核对结果；非空表示订单已发货、完成、取消或退款，不得再发货。
	blockedReason := ""
	if preparedTask.TriggerType == TriggerOrderPaid && preparedTask.OrderID != "" {
		// order、orderErr 保存准备阶段写入事实后的最新订单；读取失败按前置失败延期重试，绝不失败放行。
		order, orderErr := r.store.Orders.Get(ctx, preparedTask.OrderID)
		if orderErr != nil && !errors.Is(orderErr, db.ErrNotFound) {
			return Task{}, nil, false, &preparationError{err: fmt.Errorf("核对付款发货订单状态: %w", orderErr)}
		}
		if orderErr == nil {
			blockedReason = paidDeliveryBlockedReason(order)
		}
	}
	// amountBlocked 表示拦截来自成交金额门禁：服务预约单等 ¥0 订单仍是待发货状态，必须与订单状态拦截区分处理。
	amountBlocked := false
	if blockedReason == "" && preparedTask.TriggerType == TriggerOrderPaid && preparedTask.OrderID != "" {
		// 金额取准备阶段合并本地订单与平台详情后的事实；详情补全后仍缺失或不为正数时一律不发货。
		blockedReason = paidAmountBlockedReason(preparedTask.Amount)
		if blockedReason == reasonOrderAmountMissing && (r.orderDetailAvailable == nil || !r.orderDetailAvailable()) {
			// 没有详情查询器时无法补全金额，保持原有行为；有查询器却补全后仍无金额，则按无法证明为正数处理。
			blockedReason = ""
		}
		amountBlocked = blockedReason != ""
	}
	if blockedReason != "" && taskAutomationRunID(preparedTask) == 0 {
		// 迟到的付款事件、延期重放或零金额订单只记录，不创建运行也不触发任何外部动作。
		r.logger.Info("订单不满足付款自动发货条件，跳过", "amount_blocked", amountBlocked, "account", preparedTask.AccountID, "order_id", preparedTask.OrderID, "reason", blockedReason, "source", preparedTask.Source)
		r.audit.skipped(ctx, preparedTask, "订单不满足付款自动发货条件："+blockedReason)
		return preparedTask, nil, true, nil
	}
	// triggerKey 是用于幂等创建运行的稳定事件键，缺失时安全跳过外部动作。
	triggerKey := buildTriggerKey(preparedTask)
	if triggerKey == "" {
		return preparedTask, nil, true, nil
	}
	if len(preparedTask.ActionPlan) == 0 {
		preparedTask.ActionPlan = r.planner.plan(preparedTask, rule.Actions)
	}
	// resumeID 是延迟任务或恢复任务携带的既有运行 ID。
	if resumeID := taskAutomationRunID(preparedTask); resumeID > 0 {
		// existingRun 是由延迟或恢复任务请求继续执行的运行记录。
		existingRun, getErr := r.store.Automation.GetRun(ctx, resumeID)
		if getErr != nil {
			return Task{}, nil, false, getErr
		}
		if existingRun.Status != "running" || existingRun.RuleID != rule.ID {
			return preparedTask, nil, true, nil
		}
		if amountBlocked && existingRun.SentCount == 0 && existingRun.ActionCursor == 0 && !existingRun.ActionStarted {
			// 零金额订单仍是待发货状态，条件取消不会生效；尚无外部副作用的运行直接以取消终态收口，且不发送人工通知。
			cancelReason := "订单金额不是正数（" + blockedReason + "），不自动发货"
			// finishErr 保存零进度运行收口为取消状态的错误；失败时延期重试，不放行外部动作。
			if finishErr := r.store.Automation.FinishRun(ctx, existingRun.ID, existingRun.AttemptCount, "canceled", 0, cancelReason); finishErr != nil {
				return Task{}, nil, false, &preparationError{err: fmt.Errorf("取消零金额付款运行: %w", finishErr)}
			}
			r.logger.Info("订单金额不是正数，取消尚未执行动作的付款运行", "run_id", existingRun.ID, "account", preparedTask.AccountID, "order_id", preparedTask.OrderID, "reason", blockedReason)
			r.audit.runFinished(ctx, preparedTask, rule.ID, existingRun.ID, db.AuditStatusCanceled, 0, cancelReason)
			return preparedTask, nil, true, nil
		}
		if blockedReason != "" && existingRun.SentCount == 0 && existingRun.ActionCursor == 0 && !existingRun.ActionStarted {
			// 尚未产生任何外部副作用的运行可以安全取消；已有进度的运行保留发货凭证，继续交给原恢复与人工核对链路。
			cancelReason := "订单状态为 " + blockedReason + "，不再需要自动发货"
			// cancelErr 保存条件取消尚未执行动作的付款运行时的错误。
			if _, cancelErr := r.store.Automation.CancelObsoletePaidRecoveryRun(ctx, *existingRun, cancelReason); cancelErr != nil {
				return Task{}, nil, false, fmt.Errorf("取消已失效付款运行: %w", cancelErr)
			}
			r.logger.Info("订单已不再待发货，取消尚未执行动作的付款运行", "run_id", existingRun.ID, "account", preparedTask.AccountID, "order_id", preparedTask.OrderID, "reason", blockedReason)
			r.audit.runFinished(ctx, preparedTask, rule.ID, existingRun.ID, db.AuditStatusCanceled, 0, cancelReason)
			return preparedTask, nil, true, nil
		}
		if blockedReason == "order_shipped" || blockedReason == "order_completed" || blockedReason == "system_shipped" {
			// 订单已发货、完成或系统已发货时，旧运行不再需要执行任何外部动作，包括再次调用确认发货接口；
			// 条件取消运行（仅在订单确实不再待发货时生效），不发送人工通知。
			cancelReason := "订单状态为 " + blockedReason + "，不再需要自动发货"
			// cancelErr 保存条件取消已有进度的付款运行时的错误。
			if _, cancelErr := r.store.Automation.CancelObsoletePaidRecoveryRun(ctx, *existingRun, cancelReason); cancelErr != nil {
				return Task{}, nil, false, fmt.Errorf("取消已失效付款运行: %w", cancelErr)
			}
			r.logger.Info("订单已发货或完成，取消已有进度的付款运行", "run_id", existingRun.ID, "account", preparedTask.AccountID, "order_id", preparedTask.OrderID, "reason", blockedReason)
			r.audit.runFinished(ctx, preparedTask, rule.ID, existingRun.ID, db.AuditStatusCanceled, existingRun.SentCount, cancelReason)
			return preparedTask, nil, true, nil
		}
		if blockedReason != "" {
			// 已有发货进度的运行遇到订单取消、退款或部分发货时，剩余的发卡、模板与确认发货都不应再执行；
			// 已发出的内容需要人工核对，因此停止自动重放并发送独立的人工处理通知。
			reason := "订单状态为 " + blockedReason + "，付款发货已有进度，已停止自动重放，请人工核对"
			if amountBlocked {
				reason = "订单金额不是正数（" + blockedReason + "），付款发货已有进度，已停止自动重放，请人工核对"
			}
			// quarantineCtx 保证人工核对状态在请求取消后仍能落库。
			quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
			// quarantineErr 保存人工核对状态写入错误；失败时延期重试，不放行外部动作。
			quarantineErr := r.store.Automation.QuarantineRunResult(quarantineCtx, existingRun.ID, existingRun.AttemptCount, existingRun.SentCount, reason)
			quarantineCancel()
			if quarantineErr != nil {
				return Task{}, nil, false, &preparationError{err: fmt.Errorf("订单已结束且付款运行已有进度，保存人工核对状态失败: %w", quarantineErr)}
			}
			r.logger.Warn("订单已结束但付款运行已有进度，转人工核对", "run_id", existingRun.ID, "account", preparedTask.AccountID, "order_id", preparedTask.OrderID, "reason", blockedReason)
			r.audit.runFinished(ctx, preparedTask, rule.ID, existingRun.ID, db.AuditStatusNeedsReview, existingRun.SentCount, reason)
			if r.hasNotifier() {
				// notifyCtx 保证人工核对通知在请求取消后仍能进入通知链路。
				notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
				r.notifyResult(notifyCtx, preparedTask, existingRun.ID, "needs_review", existingRun.SentCount, reason)
				notifyCancel()
			}
			return preparedTask, nil, true, nil
		}
		// 恢复快照不能借用其他账号或订单的运行检查点；外部动作前还会按数据库中的运行归属再次防御。
		if existingRun.CookieID != preparedTask.AccountID || existingRun.OrderID != preparedTask.OrderID {
			return Task{}, nil, false, db.ErrForbidden
		}
		return preparedTask, existingRun, false, nil
	}
	// retryTask 是去除敏感 Cookie 后写入运行快照的任务副本。
	retryTask := preparedTask
	retryTask.CookieStr = ""
	// rawJSON 是用于恢复运行的无敏感任务快照；当前 Task 字段均可 JSON 编码，因此保持原有忽略编码错误行为。
	rawJSON, _ := json.Marshal(retryTask)
	// newRun 是传给原子创建操作的运行初值，租约限制异常 worker 不能永久占用事件。
	newRun := db.AutomationRun{
		RuleID: rule.ID, CookieID: preparedTask.AccountID, ItemID: preparedTask.ItemID, OrderID: preparedTask.OrderID,
		BuyerID: preparedTask.BuyerID, ChatID: preparedTask.ChatID, TriggerType: preparedTask.TriggerType,
		TriggerKey: triggerKey, RawEventJSON: string(rawJSON),
		LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute).Unix(),
	}
	// runID 是新建运行的数据库主键；started 表示当前事件是否抢到唯一执行权。
	runID, started, startErr := r.store.Automation.TryStartRun(ctx, newRun)
	if startErr != nil {
		return Task{}, nil, false, startErr
	}
	if !started {
		// 同一规则的同一事件已由其他投递处理或已完成，不能把空运行记录交给执行阶段。
		r.audit.skipped(ctx, preparedTask, "同一规则的同一事件已处理，重复事件已忽略")
		return preparedTask, nil, true, nil
	}
	// createdRun 是重新读取的完整运行状态，包含动作游标和累计发送数。
	createdRun, getErr := r.store.Automation.GetRun(ctx, runID)
	if getErr != nil {
		return Task{}, nil, false, getErr
	}
	return preparedTask, createdRun, false, nil
}

// executeRunActions 按动作游标执行计划，并在每个外部动作前后保存检查点。
func (r automationRunCoordinator) executeRunActions(ctx context.Context, task Task, ruleID int64, run *db.AutomationRun, actions []db.AutomationAction, skipDelays bool) (int, bool, error) {
	if run.DeliveryProof.RefillPending {
		// reason 说明即使普通恢复队列重新领取运行，也不能绕过未可靠收口的人工补取占用。
		reason := "补取卡密结果未可靠保存，已禁止再次取卡，请人工核对"
		// quarantineCtx 保证补取占用保护在调用方取消后仍能落库。
		quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
		// quarantineErr 保存隔离当前恢复代次的错误，失败时仍不得执行任何外部动作。
		quarantineErr := r.store.Automation.QuarantineRunResult(quarantineCtx, run.ID, run.AttemptCount, run.SentCount, reason)
		quarantineCancel()
		return run.SentCount, false, errors.Join(errAutomationNeedsReview, errors.New(reason), quarantineErr)
	}
	// sent 保存本次运行已经确认完成的动作数量。
	sent := run.SentCount
	// persistDeliveryProof 在订单付款发货和评价赠品链路保存可重放内容；两者都可能消费库存或调用卡密接口，
	// 结果不确定时必须保留原文，避免恢复时再次扣减库存或再次请求外部卡密接口。
	persistDeliveryProof := task.TriggerType == TriggerOrderPaid || task.TriggerType == TriggerBuyerReviewed
	// deliveryProof 保存本次运行已经成功投递的卡密文本和图片，并从数据库检查点恢复。
	deliveryProof := shipmentDeliveryProof{
		tradeText:               run.DeliveryProof.TradeText,
		picList:                 append([]string(nil), run.DeliveryProof.PicList...),
		messages:                append([]db.AutomationDeliveryMessage(nil), run.DeliveryProof.Messages...),
		expectedUnits:           run.DeliveryProof.ExpectedUnits,
		preparedUnits:           run.DeliveryProof.PreparedUnits,
		unknownUnits:            run.DeliveryProof.UnknownUnits,
		refillPending:           run.DeliveryProof.RefillPending,
		skippedTemplateMessages: append([]db.AutomationDeliverySkip(nil), run.DeliveryProof.SkippedTemplateMessages...),
	}
	return r.executeRunActionLoop(ctx, task, ruleID, run, actions, skipDelays, persistDeliveryProof, sent, deliveryProof)
}

// executeRunActionLoop 负责按动作游标执行外部动作、保存检查点并收口不确定结果。
func (r automationRunCoordinator) executeRunActionLoop(ctx context.Context, task Task, ruleID int64, run *db.AutomationRun, actions []db.AutomationAction, skipDelays, persistDeliveryProof bool, sent int, deliveryProof shipmentDeliveryProof) (int, bool, error) {
	// pendingUncertainty 保存「消息动作结果不确定但已放行后续幂等状态动作」的待收口信息；非空表示循环结束时必须收口为人工核对。
	// 优先从任务快照恢复：收尾动作带延迟时上一次调用已带着标记返回，内存状态丢失，只能靠快照把收口带过延迟。
	pendingUncertainty := restorePendingUncertaintyForRun(task, sent)
	// cursor 表示当前动作在计划中的位置。
	for cursor := run.ActionCursor; cursor < len(actions); cursor++ {
		// action 是当前待执行的动作定义。
		action := actions[cursor]
		if !skipDelays {
			// delaySeconds 是当前动作生效后的等待秒数。
			delaySeconds, err := r.actionDelaySeconds(ctx, action)
			if err != nil {
				return sent, false, err
			}
			if delaySeconds > 0 && taskDelayCursor(task) != cursor {
				if task.Raw == nil {
					task.Raw = map[string]any{}
				}
				task.Raw["automation_run_id"] = run.ID
				task.Raw["automation_rule_id"] = ruleID
				task.Raw["automation_delay_cursor"] = cursor
				// dueAt 是延迟动作重新进入可执行状态的 UTC 时间点，同时用于续租当前运行。
				dueAt := time.Now().UTC().Add(time.Duration(delaySeconds) * time.Second)
				// leaseErr 保存延期运行续租失败的原因。
				if leaseErr := r.store.Automation.RenewRunLease(ctx, run.ID, run.AttemptCount, dueAt.Add(5*time.Minute).Unix()); leaseErr != nil {
					return sent, false, leaseErr
				}
				// deferErr 保存延迟任务写入失败的原因。
				if deferErr := r.deferTask(ctx, task, dueAt.Unix()); deferErr != nil {
					return sent, false, deferErr
				}
				r.audit.queued(ctx, task, run.ID, ruleID, fmt.Sprintf("动作延迟 %d 秒后执行：%s", delaySeconds, summarizeAction(action, actionExecutionResult{})))
				return sent, true, nil
			}
		}
		// started 表示当前 worker 是否成功占用动作检查点。
		started, err := r.store.Automation.StartRunAction(ctx, run.ID, run.AttemptCount, cursor, time.Now().UTC().Add(5*time.Minute).Unix())
		if err != nil || !started {
			if err == nil {
				err = errors.New("自动化动作已被其他 worker 领取")
			}
			// settledErr、settledSent 分别保存：必要时已收口为人工核对的错误；必要时已收口为人工核对后的已确认数量。
			settledSent, settledErr := r.settlePendingUncertainty(ctx, run, pendingUncertainty, sent, err)
			return settledSent, false, settledErr
		}
		// n 保存外部动作明确成功产生的结果数量。
		actionResult, actionErr := r.executeLeasedAction(ctx, task, action, deliveryProof, run)
		// actionResult 中的模板跳过位置只在当前动作内有意义，此处补全冻结计划下标后再持久化。
		actionResult.proof = tagTemplateDeliverySkips(actionResult.proof, cursor)
		actionResult.reviewProof = tagTemplateDeliverySkips(actionResult.reviewProof, cursor)
		// n 表示本动作已明确完成的外部结果数量。
		n := actionResult.sent
		if actionErr != nil {
			// uncertain 标记外部系统可能已经执行动作但本地无法确认的错误。
			var uncertain *uncertainActionError
			// failedStatus 区分明确失败与结果不确定：后者买家可能已收到内容，需要人工核对。
			failedStatus := db.AuditStatusFailed
			if n > 0 || errors.As(actionErr, &uncertain) {
				failedStatus = db.AuditStatusNeedsReview
			}
			r.audit.record(ctx, task, auditEvent{eventType: auditEventActionFailed, status: failedStatus, ruleID: ruleID, runID: run.ID, actionType: action.ActionType, summary: summarizeAction(action, actionResult), reason: actionErr.Error()})
			if n > 0 || errors.As(actionErr, &uncertain) {
				// reason 说明外部动作结果未知时为何必须隔离运行。
				reason := "外部动作可能已部分或全部执行，已禁止自动重放，请人工核对: " + actionErr.Error()
				// quarantineErr 保存外部动作结果未知的人工核对状态。
				// quarantineProof 合并运行既有、已确认及结果不确定的发货凭证，供人工核对远端状态。
				quarantineProof := mergeShipmentDeliveryProof(deliveryProof, actionResult.proof)
				quarantineProof = mergeShipmentDeliveryProof(quarantineProof, actionResult.reviewProof)
				// proofInput 保存需要加密写入人工核对记录的凭证。
				var proofInput *db.AutomationDeliveryProof
				if persistDeliveryProof && (quarantineProof.refillPending || quarantineProof.tradeText != "" || len(quarantineProof.picList) > 0 || len(quarantineProof.messages) > 0 || len(quarantineProof.skippedTemplateMessages) > 0) {
					proofInput = &db.AutomationDeliveryProof{
						TradeText:               quarantineProof.tradeText,
						PicList:                 append([]string(nil), quarantineProof.picList...),
						Messages:                append([]db.AutomationDeliveryMessage(nil), quarantineProof.messages...),
						ExpectedUnits:           quarantineProof.expectedUnits,
						PreparedUnits:           quarantineProof.preparedUnits,
						UnknownUnits:            quarantineProof.unknownUnits,
						RefillPending:           quarantineProof.refillPending,
						SkippedTemplateMessages: append([]db.AutomationDeliverySkip(nil), quarantineProof.skippedTemplateMessages...),
					}
				}
				// 剩余动作全部是幂等状态动作时放行：消息可能已经送达，不该让与买家无关的状态动作一并作废。
				// continuedState 非空表示本次已放行；该状态同时写入任务快照，供带延迟的收尾动作在恢复时继续收口。
				continuedState, continuedProof, continueErr := r.tryContinueAfterUncertain(&task, ctx, run, ruleID, cursor, actions, actionResult, sent+n, reason, proofInput, deliveryProof, actionErr)
				if continueErr != nil {
					return sent + n, false, continueErr
				}
				if continuedState != nil {
					pendingUncertainty = continuedState
					deliveryProof = continuedProof
					sent += n
					continue
				}
				// quarantineCtx 保证结果不确定时的人工核对状态在扫描预算取消后仍能落库。
				quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
				// quarantineErr 保存人工核对状态写入错误。
				quarantineErr := r.store.Automation.QuarantineRunResultWithProof(quarantineCtx, run.ID, run.AttemptCount, sent+n, reason, proofInput)
				quarantineCancel()
				if quarantineErr != nil {
					r.logger.Error("保存不确定动作人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
					return sent + n, false, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, actionErr, quarantineErr)
				}
				return sent + n, false, fmt.Errorf("%w: %v", errAutomationNeedsReview, actionErr)
			}
			// abortCtx 清理明确未执行动作的占用检查点，即使扫描预算已经取消也不能遗留动作占用。
			abortCtx, abortCancel := newAutomationRunCompensationContext(ctx)
			// abortErr 清理明确未执行动作的占用检查点。
			abortErr := r.store.Automation.AbortRunAction(abortCtx, run.ID, run.AttemptCount, cursor)
			abortCancel()
			if abortErr != nil {
				// reason 记录动作明确未执行但检查点无法释放的原因，用于隔离该运行以阻止自动重放。
				reason := "外部动作明确未执行，但清除动作占用状态失败，已停止自动重放: " + abortErr.Error()
				// quarantineCtx 保证动作检查点异常后的隔离状态仍能在取消后落库。
				quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
				// quarantineErr 保存动作检查点无法清理时的隔离结果。
				quarantineErr := r.store.Automation.QuarantineRun(quarantineCtx, run.ID, run.AttemptCount, reason)
				quarantineCancel()
				if quarantineErr != nil {
					r.logger.Error("隔离动作状态异常的自动化运行失败", "run_id", run.ID, "err", quarantineErr)
				}
				return sent, false, fmt.Errorf("%w: %s", errAutomationNeedsReview, reason)
			}
			// settledErr、settledSent 分别保存：必要时已收口为人工核对的错误；必要时已收口为人工核对后的已确认数量。
			settledSent, settledErr := r.settlePendingUncertainty(ctx, run, pendingUncertainty, sent, actionErr)
			return settledSent, false, settledErr
		}
		// nextProof 是本动作成功后应持久化的完整凭证，避免延迟或重启后丢失已发送内容。
		nextProof := deliveryProof
		// proofChanged 表示本动作产生了需要持久化的新凭证。
		proofChanged := persistDeliveryProof && (actionResult.proof.tradeText != "" || len(actionResult.proof.picList) > 0 || len(actionResult.proof.messages) > 0 || len(actionResult.proof.skippedTemplateMessages) > 0)
		if proofChanged {
			nextProof = mergeShipmentDeliveryProof(deliveryProof, actionResult.proof)
		}
		// advance 描述外部动作完成后的原子检查点更新，凭证和游标必须在同一条 UPDATE 中推进。
		advance := db.AutomationRunActionAdvance{RunID: run.ID, Attempt: run.AttemptCount, Cursor: cursor, SentDelta: n}
		if proofChanged {
			// persistedProof 是数据库仓储使用的导出凭证模型。
			persistedProof := db.AutomationDeliveryProof{
				TradeText:               nextProof.tradeText,
				PicList:                 append([]string(nil), nextProof.picList...),
				Messages:                append([]db.AutomationDeliveryMessage(nil), nextProof.messages...),
				ExpectedUnits:           nextProof.expectedUnits,
				PreparedUnits:           nextProof.preparedUnits,
				UnknownUnits:            nextProof.unknownUnits,
				RefillPending:           nextProof.refillPending,
				SkippedTemplateMessages: append([]db.AutomationDeliverySkip(nil), nextProof.skippedTemplateMessages...),
			}
			advance.DeliveryProof = &persistedProof
		}
		// err 表示外部动作完成后推进检查点的数据库错误；失败时必须隔离运行避免重复执行。
		if err := r.store.Automation.AdvanceRunAction(ctx, advance); err != nil {
			// quarantineCtx 保证检查点写入失败后的隔离状态不被已取消的动作上下文阻断。
			quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
			// quarantineErr 保存检查点失败后的人工核对状态。
			quarantineErr := r.store.Automation.QuarantineRun(quarantineCtx, run.ID, run.AttemptCount, "动作已执行但检查点保存失败，请人工核对，禁止自动重放: "+err.Error())
			quarantineCancel()
			if quarantineErr != nil {
				r.logger.Error("保存检查点异常的人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return sent + n, false, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, err, quarantineErr)
			}
			return sent + n, false, fmt.Errorf("%w: %v", errAutomationNeedsReview, err)
		}
		r.audit.record(ctx, task, auditEvent{eventType: auditEventActionSucceeded, status: db.AuditStatusSuccess, ruleID: ruleID, runID: run.ID, actionType: action.ActionType, summary: summarizeAction(action, actionResult)})
		sent += n
		if proofChanged {
			deliveryProof = nextProof
		}
		if task.Raw != nil {
			delete(task.Raw, "automation_delay_cursor")
		}
	}
	// 放行过幂等状态动作的运行同样必须收口为人工核对：消息动作的送达结果仍未被确认；没有待收口状态时原样返回。
	finalSent, finalErr := r.settlePendingUncertainty(ctx, run, pendingUncertainty, sent, nil)
	return finalSent, false, finalErr
}

// settlePendingUncertainty 把放行后的待收口状态落库为人工核对，并返回应对外报告的数量与错误。
// pending 为 nil 表示没有待收口的不确定结果，原样返回 sent 与 cause；cause 非空时与人工核对错误合并，使调用方既能识别人工核对，也能看到后续动作的失败原因。
// 放行后的幂等状态动作因取消、占用冲突或明确未执行而提前退出时也必须经过这里，否则前序消息动作的不确定结果会被丢弃，运行可能被当作普通失败而重放发卡。
func (r automationRunCoordinator) settlePendingUncertainty(ctx context.Context, run *db.AutomationRun, pending *uncertainActionState, sent int, cause error) (int, error) {
	if pending == nil {
		return sent, cause
	}
	// quarantineCtx 保证调用方已取消时人工核对状态仍能落库。
	quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
	// quarantineErr 保存人工核对状态的写入错误。
	quarantineErr := r.store.Automation.QuarantineRunResultWithProof(quarantineCtx, run.ID, run.AttemptCount, pending.sent, pending.reason, pending.proof)
	quarantineCancel()
	if quarantineErr != nil {
		r.logger.Error("保存不确定动作人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
		return pending.sent, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, pending.err, cause, quarantineErr)
	}
	return pending.sent, errors.Join(fmt.Errorf("%w: %v", errAutomationNeedsReview, pending.err), cause)
}

// executeActionNow 在动作真正触达外部系统前执行账号门禁，并把前序发卡动作的凭证传给当前动作。
func (r automationRunCoordinator) executeActionNow(ctx context.Context, task Task, action db.AutomationAction, proof shipmentDeliveryProof) (actionExecutionResult, error) {
	// allowed 表示账号当前是否允许继续执行自动化动作。
	allowed, err := r.accountAutomationAllowed(ctx, task.AccountID)
	if err != nil {
		return actionExecutionResult{}, err
	}
	if !allowed {
		return actionExecutionResult{}, fmt.Errorf("账号已暂停或停用，取消自动化动作")
	}
	if actionNeedsOnlineSender(action) && r.accountSenderReady != nil && !r.accountSenderReady(task.AccountID) {
		return actionExecutionResult{}, fmt.Errorf("%w: 账号 %s 的 WebSocket 尚未就绪，未执行消息或卡密动作", ErrMessageNotSent, task.AccountID)
	}
	return r.executeAction(ctx, task, action, proof)
}

// mergeShipmentDeliveryProof 合并连续发卡动作的已投递凭证，保持文本和图片的发送顺序。
func mergeShipmentDeliveryProof(current, next shipmentDeliveryProof) shipmentDeliveryProof {
	current.tradeText = appendTradeText(current.tradeText, next.tradeText)
	current.picList = append(current.picList, next.picList...)
	current.messages = append(current.messages, next.messages...)
	if next.expectedUnits > 0 {
		current.expectedUnits += next.expectedUnits
	}
	current.preparedUnits += next.preparedUnits
	current.unknownUnits += next.unknownUnits
	current.refillPending = current.refillPending || next.refillPending
	current.skippedTemplateMessages = append(current.skippedTemplateMessages, next.skippedTemplateMessages...)
	return current
}

// tagTemplateDeliverySkips 将当前模板动作内的跳过消息下标绑定到运行计划动作下标，防止多模板动作之间产生歧义。
func tagTemplateDeliverySkips(proof shipmentDeliveryProof, actionIndex int) shipmentDeliveryProof {
	for index := range proof.skippedTemplateMessages { // index 表示当前模板跳过凭证在动作结果中的位置。
		proof.skippedTemplateMessages[index].ActionIndex = actionIndex
	}
	return proof
}

// actionNeedsOnlineSender 判断动作是否会向买家发送消息；这类动作必须先确认账号 WebSocket 已就绪，避免 API 卡密已领取但无法投递。
func actionNeedsOnlineSender(action db.AutomationAction) bool {
	switch action.ActionType {
	case ActionSendCard, ActionSendTemplate, ActionSendText:
		return true
	default:
		return false
	}
}
