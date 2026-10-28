package automation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"xianyu-go/internal/db"
)

// continueUncertainActionEnv 控制「消息动作结果不确定时是否放行后续幂等状态动作」：默认开启，显式设为 "0" 时恢复整链熔断。
// 保留开关是为了在行为出乎预期时能快速止血，而不必回滚二进制。
const continueUncertainActionEnv = "XIANYU_CONTINUE_AFTER_UNCERTAIN"

// uncertainActionState 保存「消息动作结果不确定但已放行后续幂等状态动作」的待收口信息。
// 这类运行仍必须收口为人工核对，只是不再把与买家无关的状态动作一并牺牲。
type uncertainActionState struct {
	// reason 是写入人工核对记录的原因文本。
	reason string
	// proof 是需要加密落库的发货凭证，供人工核对远端发送结果。
	proof *db.AutomationDeliveryProof
	// sent 是截至放行时已确认完成的动作数量。
	sent int
	// err 是原始的不确定动作错误，用于最终错误链。
	err error
}

// continueAfterUncertainActionEnabled 返回当前进程是否允许在消息动作结果不确定时继续执行幂等状态动作。
func continueAfterUncertainActionEnabled() bool {
	return strings.TrimSpace(os.Getenv(continueUncertainActionEnv)) != "0"
}

// canContinueAfterUncertainAction 判断剩余动作是否全部为「不触碰买家、且平台侧幂等」的状态动作。
// 只有这类动作才允许在无法确认前序消息是否送达时继续执行：它们不会再次向买家发送内容，
// 重复调用也只会收到平台的「已发货」幂等响应。
func canContinueAfterUncertainAction(remaining []db.AutomationAction) bool {
	if len(remaining) == 0 {
		return false
	}
	// action 是剩余动作中的一个，需要逐个确认它是否为平台侧幂等的发货状态动作。
	for _, action := range remaining {
		if action.ActionType != ActionConfirmShipment {
			return false
		}
	}
	return true
}

// pendingUncertaintyReasonKey 与 pendingUncertaintyErrorKey 是任务快照里记录「待人工核对」的键。
// 剩余动作可能带延迟：延迟任务被持久化后本次调用的内存状态会丢失，只能靠快照把人工核对收口带过延迟。
const (
	pendingUncertaintyReasonKey = "automation_pending_review_reason"
	pendingUncertaintyErrorKey  = "automation_pending_review_error"
)

// restorePendingUncertaintyForRun 从任务快照恢复待人工核对状态，并把已确认数量对齐到当前运行。
// 没有标记时返回 nil，表示本次运行为全新事件而不是延迟重放。
func restorePendingUncertaintyForRun(task Task, sent int) *uncertainActionState {
	// restored 保存从任务快照恢复出的待收口状态；非空时已确认的发送数量取当前运行的计数。
	restored := restorePendingUncertainty(task)
	if restored == nil {
		return nil
	}
	restored.sent = sent
	return restored
}

// markPendingUncertainty 把「消息动作结果不确定，仍需人工核对」写入任务快照，
// 使带延迟的收尾动作被持久化后，恢复时仍能收口为人工核对而不是被记为成功。
// 必须传入任务指针：Raw 为空时需要就地创建映射，按值传参会把新映射丢在副本里。
func markPendingUncertainty(task *Task, reason string, actionErr error) {
	if task == nil {
		return
	}
	if task.Raw == nil {
		task.Raw = map[string]any{}
	}
	task.Raw[pendingUncertaintyReasonKey] = reason
	if actionErr != nil {
		task.Raw[pendingUncertaintyErrorKey] = actionErr.Error()
	}
}

// restorePendingUncertainty 从任务快照恢复待人工核对状态；没有标记时返回 nil。
// 凭证不在此处恢复：放行时已通过 AdvanceRunAction 落库，恢复路径会从运行记录重新装载。
func restorePendingUncertainty(task Task) *uncertainActionState {
	if task.Raw == nil {
		return nil
	}
	// reason 保存放行时写入的人工核对原因；为空说明本次调用没有待收口状态。
	reason, _ := task.Raw[pendingUncertaintyReasonKey].(string)
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	// errText 保存原始不确定错误文本；恢复时用它还原错误链，人工核对页面才能看到原因。
	errText, _ := task.Raw[pendingUncertaintyErrorKey].(string)
	return &uncertainActionState{reason: reason, err: errors.New(errText)}
}

// tryContinueAfterUncertain 尝试在消息动作结果不确定时放行后续幂等状态动作。
// 参数 task 是本次运行的任务快照（放行时会把待核对原因写入其中，指针语义）；
// ctx 是调用方取消边界；run 提供运行 ID 与代次号；ruleID 仅用于日志定位；
// cursor 是不确定动作在冻结计划中的下标；actions 是完整冻结计划；
// actionResult 是本动作已明确产生的外部结果；sentPlusN 是截至本动作已确认的动作数量；
// reason 是人工核对原因文本；proofInput 是需要加密保留的凭证；
// deliveryProof 是已成功投递凭证的汇总；actionErr 是原始的不确定错误。
// 返回的待收口状态非空表示已放行，调用方必须继续执行剩余动作并在循环结束时收口为人工核对；
// 返回的凭证是放行后应继续携带的汇总。返回错误表示检查点推进失败，调用方必须立刻把该错误
// 作为运行结果返回：此时函数已按原有熔断语义写好人工核对记录，不留中间态。
func (r automationRunCoordinator) tryContinueAfterUncertain(
	task *Task,
	ctx context.Context,
	run *db.AutomationRun,
	ruleID int64,
	cursor int,
	actions []db.AutomationAction,
	actionResult actionExecutionResult,
	sentPlusN int,
	reason string,
	proofInput *db.AutomationDeliveryProof,
	deliveryProof shipmentDeliveryProof,
	actionErr error,
) (*uncertainActionState, shipmentDeliveryProof, error) {
	if !continueAfterUncertainActionEnabled() || !canContinueAfterUncertainAction(actions[cursor+1:]) {
		return nil, deliveryProof, nil
	}
	// 调用方上下文已取消（例如扫描预算耗尽）时不能再放行后续状态动作：检查点推进与确认发货都会因取消失败，
	// 运行会停留在 running。此时直接沿用整链熔断，由调用方用独立补偿上下文收口为人工核对。
	if ctx.Err() != nil {
		return nil, deliveryProof, nil
	}
	// 放行前必须排除「确定未执行」这一类失败：发送前就被拒绝、或本地补取与库存恢复失败时，
	// 买家并没有收到任何内容（例如库存在恢复失败后仍处于占用状态），此时把订单改判为已发货
	// 只会掩盖本地不一致，必须维持整链熔断。判据与 executeRule 对确定未执行的处理保持一致。
	if errors.Is(actionErr, ErrMessageNotSent) || errors.Is(actionErr, errActionNotPerformed) {
		return nil, deliveryProof, nil
	}
	r.logger.Warn("消息动作结果不确定，放行后续幂等状态动作", "run_id", run.ID, "rule_id", ruleID, "cursor", cursor, "remaining_actions", len(actions)-cursor-1, "err", actionErr)
	// advance 推进游标并保留已产生的发货凭证，但本次动作不计入已确认数量。
	advance := db.AutomationRunActionAdvance{RunID: run.ID, Attempt: run.AttemptCount, Cursor: cursor, SentDelta: actionResult.sent}
	if proofInput != nil {
		advance.DeliveryProof = proofInput
	}
	// advanceErr 保存推进动作检查点的写入错误；非空时退回原有熔断语义。
	if advanceErr := r.store.Automation.AdvanceRunAction(ctx, advance); advanceErr != nil {
		// 游标推进失败时退回原有熔断语义，避免留下无法解释的中间态。
		r.logger.Error("放行幂等状态动作时推进检查点失败", "run_id", run.ID, "cursor", cursor, "err", advanceErr)
		// quarantineErr 保存写入人工核对结果的错误；非空时随其他错误一并上报。
		if quarantineErr := r.store.Automation.QuarantineRunResultWithProof(ctx, run.ID, run.AttemptCount, sentPlusN, reason, proofInput); quarantineErr != nil {
			r.logger.Error("保存不确定动作人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
			return nil, deliveryProof, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, actionErr, quarantineErr)
		}
		return nil, deliveryProof, fmt.Errorf("%w: %v", errAutomationNeedsReview, actionErr)
	}
	// 放行成功后补齐本动作已产生的凭证，供后续状态动作作为发货凭证提交。
	deliveryProof = mergeShipmentDeliveryProof(deliveryProof, actionResult.proof)
	deliveryProof = mergeShipmentDeliveryProof(deliveryProof, actionResult.reviewProof)
	// 待核对原因必须同时写入任务快照：收尾动作带延迟时本次调用会先返回，恢复只能凭快照继续收口。
	markPendingUncertainty(task, reason, actionErr)
	return &uncertainActionState{reason: reason, proof: proofInput, sent: sentPlusN, err: actionErr}, deliveryProof, nil
}
