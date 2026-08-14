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
	// deferTask 持久化等待延迟后继续执行的任务。
	deferTask func(context.Context, Task, int64) error
	// executeAction 执行一个已经通过账号门禁的具体外部动作。
	executeAction func(context.Context, Task, db.AutomationAction) (int, error)
	// hasNotifier 判断当前是否注入了结果通知器。
	hasNotifier func() bool
	// notifyResult 将运行结果转换为用户可见的通知。
	notifyResult func(Task, string, int, string)
}

// executeRule 创建或恢复一次自动化运行，并统一处理运行成功、失败、延期和人工核对结果。
func (r automationRunCoordinator) executeRule(ctx context.Context, task Task, rule db.AutomationRule) error {
	// err 保存任务准备、运行创建或动作执行阶段的错误。
	var err error
	if len(task.ActionPlan) == 0 && task.TriggerType != TriggerOrderPaid {
		task.ActionPlan = r.planner.plan(task, rule.Actions)
	}
	// preparedTask 保存补全订单事实和凭证上下文后的任务。
	preparedTask, prepareErr := r.prepareTask(ctx, task)
	if prepareErr != nil {
		return &preparationError{err: prepareErr}
	}
	task = preparedTask
	// triggerKey 是用于幂等创建运行的稳定事件键。
	triggerKey := buildTriggerKey(task)
	if triggerKey == "" {
		return nil
	}
	if len(task.ActionPlan) == 0 {
		task.ActionPlan = r.planner.plan(task, rule.Actions)
	}
	// retryTask 是去除敏感 Cookie 后写入运行快照的任务副本。
	retryTask := task
	retryTask.CookieStr = ""
	// rawJSON 是用于恢复运行的无敏感任务快照。
	rawJSON, _ := json.Marshal(retryTask)
	// run 保存当前自动化运行及其动作检查点。
	var run *db.AutomationRun
	// resumeID 是延迟任务或恢复任务携带的既有运行 ID。
	if resumeID := taskAutomationRunID(task); resumeID > 0 {
		run, err = r.store.Automation.GetRun(ctx, resumeID)
		if err != nil {
			return err
		}
		if run.Status != "running" || run.RuleID != rule.ID {
			return nil
		}
	} else {
		// runID 是新建运行的数据库主键。
		var runID int64
		// started 表示当前事件是否成功抢到幂等运行的执行权。
		var started bool
		runID, started, err = r.store.Automation.TryStartRun(ctx, db.AutomationRun{
			RuleID: rule.ID, CookieID: task.AccountID, ItemID: task.ItemID, OrderID: task.OrderID,
			BuyerID: task.BuyerID, ChatID: task.ChatID, TriggerType: task.TriggerType,
			TriggerKey: triggerKey, RawEventJSON: string(rawJSON),
			LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute).Unix(),
		})
		if err != nil || !started {
			return err
		}
		// loadedRun 是创建后重新读取的完整运行状态。
		loadedRun, getErr := r.store.Automation.GetRun(ctx, runID)
		if getErr != nil {
			return getErr
		}
		run = loadedRun
	}
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
		// finishErr 保存运行结果写入失败，避免覆盖原始动作错误。
		if finishErr := r.store.Automation.FinishRun(finishCtx, run.ID, run.AttemptCount, status, sent, errMsg); finishErr != nil {
			r.logger.Error("保存自动化执行结果失败", "run_id", run.ID, "err", finishErr)
		}
		cancel()
		if r.hasNotifier() {
			r.notifyResult(task, status, sent, errMsg)
		}
	}()
	// actions 是当前规则生成的完整动作计划。
	actions := task.ActionPlan
	if task.TriggerType == TriggerOrderPaid && !r.planner.hasMatchingSendCard(task, actions) {
		status, errMsg = "failed", "未匹配到订单规格对应的卡密动作"
		return errors.New(errMsg)
	}
	// deferred 表示动作已保存到延迟队列，当前运行不能被标记为完成。
	var deferred bool
	sent, deferred, err = r.executeRunActions(ctx, task, rule.ID, run, actions, false)
	if deferred {
		finish = false
		return errAutomationDeferred
	}
	if errors.Is(err, errAutomationNeedsReview) {
		finish = false
		if r.hasNotifier() && !errors.Is(err, errAutomationQuarantine) {
			r.notifyResult(task, "needs_review", sent, err.Error())
		}
		return err
	}
	if err != nil {
		if sent > 0 && !errors.Is(err, ErrMessageNotSent) && !errors.Is(err, errActionNotPerformed) {
			// reason 说明部分动作成功后为何必须人工核对。
			reason := "运行已完成部分动作，后续动作失败，已禁止从头自动重放: " + err.Error()
			// quarantineErr 保存部分成功运行的人工核对状态。
			if quarantineErr := r.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent, reason); quarantineErr != nil {
				finish = false
				r.logger.Error("保存自动化人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return errors.Join(errAutomationNeedsReview, errAutomationQuarantine, err, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				r.notifyResult(task, "needs_review", sent, reason)
			}
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, err)
		}
		status, errMsg = "failed", err.Error()
		if errors.Is(err, ErrMessageNotSent) || errors.Is(err, errActionNotPerformed) {
			errMsg = db.SafeRetryErrorPrefix + errMsg
		}
		return err
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		// incrementErr 保存求评价消息成功后的提醒次数。
		if incrementErr := r.store.Automation.IncrementReviewRequest(ctx, task.OrderID); incrementErr != nil {
			reason := "求评价消息已发送，但保存提醒次数失败，已停止自动重放: " + incrementErr.Error()
			// quarantineErr 保存提醒次数写入失败的人工核对状态。
			if quarantineErr := r.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent, reason); quarantineErr != nil {
				finish = false
				r.logger.Error("保存求评价人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return errors.Join(errAutomationNeedsReview, errAutomationQuarantine, incrementErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				r.notifyResult(task, "needs_review", sent, reason)
			}
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, incrementErr)
		}
	}
	return nil
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
				// dueAt 是动作重新进入可执行状态的时间点。
				// dueAt 是动作重新进入可执行状态的时间点。
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
				reason := "外部动作明确未执行，但清除动作占用状态失败，已停止自动重放: " + abortErr.Error()
				// quarantineErr 保存动作检查点无法清理时的隔离结果。
				if quarantineErr := r.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, reason); quarantineErr != nil {
					r.logger.Error("隔离动作状态异常的自动化运行失败", "run_id", run.ID, "err", quarantineErr)
				}
				return sent, false, fmt.Errorf("%w: %s", errAutomationNeedsReview, reason)
			}
			return sent, false, actionErr
		}
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
	return r.executeAction(ctx, task, action)
}
