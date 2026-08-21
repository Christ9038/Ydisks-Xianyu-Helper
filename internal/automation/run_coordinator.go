package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"xianyu-go/internal/db"
)

// automationRunCoordinator 负责自动化运行的创建、续租、动作检查点和结果收口；它不决定具体业务动作，只协调动作执行过程中的持久化状态和三态结果。
type automationRunCoordinator struct {
	// store 提供自动化运行、延迟任务和人工核对状态的持久化能力。
	store *db.Store
	// planner 根据事件和规则生成动作计划，保证协调器不直接解释规则细节。
	planner actionPlanner
	// logger 记录运行状态收口和检查点异常。
	logger *slog.Logger
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
	// executeAction 执行一个已经通过账号门禁的具体外部动作。
	executeAction func(context.Context, Task, db.AutomationAction) (int, error)
	// hasNotifier 判断当前是否注入了结果通知器。
	hasNotifier func() bool
	// notifyResult 将运行结果转换为用户可见的、按运行终态幂等的通知。
	notifyResult func(context.Context, Task, int64, string, int, string)
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
		finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		if r.hasNotifier() && !errors.Is(actionErr, errAutomationQuarantine) {
			r.notifyResult(ctx, task, run.ID, "needs_review", sent, actionErr.Error())
		}
		return actionErr
	}
	if actionErr != nil {
		if sent > 0 && !errors.Is(actionErr, ErrMessageNotSent) && !errors.Is(actionErr, errActionNotPerformed) {
			// reason 说明部分动作成功后为何必须人工核对。
			reason := "运行已完成部分动作，后续动作失败，已禁止从头自动重放: " + actionErr.Error()
			// quarantineErr 保存部分成功运行的人工核对状态。
			if quarantineErr := r.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent, reason); quarantineErr != nil {
				finish = false
				r.logger.Error("保存自动化人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return errors.Join(errAutomationNeedsReview, errAutomationQuarantine, actionErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				r.notifyResult(ctx, task, run.ID, "needs_review", sent, reason)
			}
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
			// quarantineErr 保存提醒次数写入失败的人工核对状态。
			if quarantineErr := r.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent, reason); quarantineErr != nil {
				finish = false
				r.logger.Error("保存求评价人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return errors.Join(errAutomationNeedsReview, errAutomationQuarantine, incrementErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				r.notifyResult(ctx, task, run.ID, "needs_review", sent, reason)
			}
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, incrementErr)
		}
	}
	return nil
}

// prepareRuleRun 先补全任务事实和动作计划，再恢复既有运行或原子创建新的幂等运行；skipped 为 true 表示重复事件或已失效恢复任务无需执行。
func (r automationRunCoordinator) prepareRuleRun(ctx context.Context, task Task, rule db.AutomationRule) (preparedTask Task, run *db.AutomationRun, skipped bool, err error) {
	if len(task.ActionPlan) == 0 && task.TriggerType != TriggerOrderPaid {
		task.ActionPlan = r.planner.plan(task, rule.Actions)
	}
	// preparedTask 是订单事实和凭证上下文补全后的任务；prepareErr 表示准备阶段的可见失败。
	preparedTask, prepareErr := r.prepareTask(ctx, task)
	if prepareErr != nil {
		return Task{}, nil, false, &preparationError{err: prepareErr}
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
	// sent 保存本次运行已经确认完成的动作数量。
	sent := run.SentCount
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
				return sent, true, nil
			}
		}
		// started 表示当前 worker 是否成功占用动作检查点。
		started, err := r.store.Automation.StartRunAction(ctx, run.ID, run.AttemptCount, cursor, time.Now().UTC().Add(5*time.Minute).Unix())
		if err != nil || !started {
			if err == nil {
				err = errors.New("自动化动作已被其他 worker 领取")
			}
			return sent, false, err
		}
		// n 保存外部动作明确成功产生的结果数量。
		n, actionErr := r.executeActionNow(ctx, task, action)
		if actionErr != nil {
			// uncertain 标记外部系统可能已经执行动作但本地无法确认的错误。
			var uncertain *uncertainActionError
			if n > 0 || errors.As(actionErr, &uncertain) {
				// reason 说明外部动作结果未知时为何必须隔离运行。
				reason := "外部动作可能已部分或全部执行，已禁止自动重放，请人工核对: " + actionErr.Error()
				// quarantineErr 保存外部动作结果未知的人工核对状态。
				if quarantineErr := r.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent+n, reason); quarantineErr != nil {
					r.logger.Error("保存不确定动作人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
					return sent + n, false, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, actionErr, quarantineErr)
				}
				return sent + n, false, fmt.Errorf("%w: %v", errAutomationNeedsReview, actionErr)
			}
			// abortErr 清理明确未执行动作的占用检查点。
			if abortErr := r.store.Automation.AbortRunAction(ctx, run.ID, run.AttemptCount, cursor); abortErr != nil {
				// reason 记录动作明确未执行但检查点无法释放的原因，用于隔离该运行以阻止自动重放。
				reason := "外部动作明确未执行，但清除动作占用状态失败，已停止自动重放: " + abortErr.Error()
				// quarantineErr 保存动作检查点无法清理时的隔离结果。
				if quarantineErr := r.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, reason); quarantineErr != nil {
					r.logger.Error("隔离动作状态异常的自动化运行失败", "run_id", run.ID, "err", quarantineErr)
				}
				return sent, false, fmt.Errorf("%w: %s", errAutomationNeedsReview, reason)
			}
			return sent, false, actionErr
		}
		// err 表示外部动作完成后推进检查点的数据库错误；失败时必须隔离运行避免重复执行。
		if err := r.store.Automation.AdvanceRunAction(ctx, run.ID, run.AttemptCount, cursor, n); err != nil {
			// quarantineErr 保存检查点失败后的人工核对状态。
			if quarantineErr := r.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, "动作已执行但检查点保存失败，请人工核对，禁止自动重放: "+err.Error()); quarantineErr != nil {
				r.logger.Error("保存检查点异常的人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return sent + n, false, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, err, quarantineErr)
			}
			return sent + n, false, fmt.Errorf("%w: %v", errAutomationNeedsReview, err)
		}
		sent += n
		if task.Raw != nil {
			delete(task.Raw, "automation_delay_cursor")
		}
	}
	return sent, false, nil
}

// executeActionNow 在动作真正触达外部系统前执行账号门禁。
func (r automationRunCoordinator) executeActionNow(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	// allowed 表示账号当前是否允许继续执行自动化动作。
	allowed, err := r.accountAutomationAllowed(ctx, task.AccountID)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, fmt.Errorf("账号已暂停或停用，取消自动化动作")
	}
	if actionNeedsOnlineSender(action) && r.accountSenderReady != nil && !r.accountSenderReady(task.AccountID) {
		return 0, fmt.Errorf("%w: 账号 %s 的 WebSocket 尚未就绪，未执行消息或卡密动作", ErrMessageNotSent, task.AccountID)
	}
	return r.executeAction(ctx, task, action)
}

// actionNeedsOnlineSender 判断动作是否会向买家发送消息；这类动作必须先确认账号 WebSocket 已就绪，避免 API 卡密已领取但无法投递。
func actionNeedsOnlineSender(action db.AutomationAction) bool {
	switch action.ActionType {
	case ActionSendCard, ActionSendText:
		return true
	default:
		return false
	}
}
