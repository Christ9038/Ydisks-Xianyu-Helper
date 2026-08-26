package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestAutomationDeliveryProofBoundaryBranches 覆盖发货凭证读取、清除、无凭证推进、租约丢失和数据库取消分支。
func TestAutomationDeliveryProofBoundaryBranches(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "")
	// store、cleanup 保存不启用数据密钥的本地测试数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本次数据库操作共用的上下文。
	ctx := context.Background()
	// userID、cookieID 保存创建自动化规则所需的本地账号归属。
	userID, cookieID := seedAccount(t, store)
	// ruleID、createErr 保存测试规则创建结果。
	ruleID, createErr := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, "proof-boundary-item", "paid", true, 1))
	if createErr != nil {
		t.Fatal(createErr)
	}
	// runID、started、startErr 保存第一条自动化运行的创建结果。
	runID, started, startErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, ItemID: "proof-boundary-item", TriggerType: "paid", TriggerKey: "proof-boundary"})
	if startErr != nil || !started {
		t.Fatalf("创建自动化运行失败 id=%d started=%v err=%v", runID, started, startErr)
	}
	// missingErr 保存读取不存在运行时的稳定错误。
	if _, missingErr := store.Automation.GetRun(ctx, runID+99999); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("不存在运行错误=%v", missingErr)
	}
	// writeErr 保存写入非法明文凭证的数据库错误。
	if _, writeErr := store.DB.ExecContext(ctx, "UPDATE automation_runs SET delivery_proof=? WHERE id=?", "not-json", runID); writeErr != nil {
		t.Fatal(writeErr)
	}
	// invalidErr 保存凭证 JSON 无法解析时的错误。
	if _, invalidErr := store.Automation.GetRun(ctx, runID); invalidErr == nil || !strings.Contains(invalidErr.Error(), "解析自动化发货凭证失败") {
		t.Fatalf("非法凭证错误=%v", invalidErr)
	}
	// encryptedWriteErr 保存写入伪造密文的数据库错误。
	if _, encryptedWriteErr := store.DB.ExecContext(ctx, "UPDATE automation_runs SET delivery_proof=? WHERE id=?", "enc:v1:invalid", runID); encryptedWriteErr != nil {
		t.Fatal(encryptedWriteErr)
	}
	// encryptedErr 保存缺少数据密钥时的解密错误。
	if _, encryptedErr := store.Automation.GetRun(ctx, runID); encryptedErr == nil || !strings.Contains(encryptedErr.Error(), "XIANYU_DATA_KEY") {
		t.Fatalf("伪造密文错误=%v", encryptedErr)
	}
	// clearWriteErr 保存清空非法凭证字段的数据库错误。
	if _, clearWriteErr := store.DB.ExecContext(ctx, "UPDATE automation_runs SET delivery_proof='' WHERE id=?", runID); clearWriteErr != nil {
		t.Fatal(clearWriteErr)
	}
	// run、readErr 保存清空凭证后的运行检查点。
	run, readErr := store.Automation.GetRun(ctx, runID)
	if readErr != nil || run.DeliveryProof.TradeText != "" {
		t.Fatalf("空凭证读取异常 run=%+v err=%v", run, readErr)
	}
	// startedAction、actionErr 保存第一条运行的动作占用结果。
	startedAction, actionErr := store.Automation.StartRunAction(ctx, runID, run.AttemptCount, 0, time.Now().Add(time.Minute).Unix())
	if actionErr != nil || !startedAction {
		t.Fatalf("动作占用失败 started=%v err=%v", startedAction, actionErr)
	}
	// clearErr 保存不携带新凭证而清除旧凭证的检查点推进错误。
	clearErr := store.Automation.AdvanceRunAction(ctx, AutomationRunActionAdvance{RunID: runID, Attempt: run.AttemptCount, Cursor: 0, SentDelta: 1, ClearDeliveryProof: true})
	if clearErr != nil {
		t.Fatalf("清除凭证推进失败: %v", clearErr)
	}
	// secondRunID、secondStarted、secondStartErr 保存第二条自动化运行的创建结果。
	secondRunID, secondStarted, secondStartErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, ItemID: "proof-boundary-item", TriggerType: "paid", TriggerKey: "proof-boundary-second"})
	if secondStartErr != nil || !secondStarted {
		t.Fatalf("创建第二条运行失败 id=%d started=%v err=%v", secondRunID, secondStarted, secondStartErr)
	}
	// secondRun、secondReadErr 保存第二条运行的当前检查点。
	secondRun, secondReadErr := store.Automation.GetRun(ctx, secondRunID)
	if secondReadErr != nil {
		t.Fatal(secondReadErr)
	}
	// secondActionStarted、secondActionErr 保存第二条运行的动作占用结果。
	secondActionStarted, secondActionErr := store.Automation.StartRunAction(ctx, secondRunID, secondRun.AttemptCount, 0, time.Now().Add(time.Minute).Unix())
	if secondActionErr != nil || !secondActionStarted {
		t.Fatalf("第二条动作占用失败 started=%v err=%v", secondActionStarted, secondActionErr)
	}
	// noProofErr 保存不携带凭证且不要求清除时的检查点推进结果。
	noProofErr := store.Automation.AdvanceRunAction(ctx, AutomationRunActionAdvance{RunID: secondRunID, Attempt: secondRun.AttemptCount, Cursor: 0, SentDelta: 0})
	if noProofErr != nil {
		t.Fatalf("无凭证推进失败: %v", noProofErr)
	}
	// staleErr 保存重复使用旧动作游标时的租约丢失错误。
	staleErr := store.Automation.AdvanceRunAction(ctx, AutomationRunActionAdvance{RunID: runID, Attempt: run.AttemptCount, Cursor: 0})
	if !errors.Is(staleErr, ErrAutomationRunLeaseLost) {
		t.Fatalf("旧游标错误=%v", staleErr)
	}
	// canceledCtx、cancel 生成已取消的数据库上下文。
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	// canceledReadErr 保存取消上下文读取运行时的数据库错误。
	if _, canceledReadErr := store.Automation.GetRun(canceledCtx, secondRunID); canceledReadErr == nil {
		t.Fatal("取消上下文读取应返回数据库错误")
	}
	// canceledAdvanceErr 保存取消上下文推进检查点时的数据库错误。
	if canceledAdvanceErr := store.Automation.AdvanceRunAction(canceledCtx, AutomationRunActionAdvance{RunID: secondRunID, Attempt: secondRun.AttemptCount, Cursor: 1}); canceledAdvanceErr == nil {
		t.Fatal("取消上下文推进应返回数据库错误")
	}
}
