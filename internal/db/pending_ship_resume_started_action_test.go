package db

import (
	"context"
	"testing"
	"time"
)

// TestPendingShipResumeExcludesStartedActionRuns 验证动作已开始（外部结果不确定）的人工核对运行不进入续跑候选（#113）。
// 这类运行 ReopenRunForRecovery 必然拒绝；纳入候选会让调度器每轮重复尝试并输出“重开失败”告警。
// 动作已清除占用的人工核对运行仍是候选，且能被真正重开。
func TestPendingShipResumeExcludesStartedActionRuns(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	// ruleID 是含发卡与确认发货的启用规则。
	ruleID := seedCatchupRule(t, s, userID, cookieID, "item-1", "动作占用过滤规则", true,
		[]AutomationActionInput{{ActionType: "send_card", MessageTemplate: "card", Enabled: true}, {ActionType: "confirm_shipment", Enabled: true}})
	// started、cleared 分别是“确认发货结果不确定”和“平台明确拒绝后已清除占用”的两笔订单。
	started, cleared := "o-started", "o-cleared"
	// runIDs 保存两笔订单各自的运行主键。
	runIDs := map[string]int64{}
	// orderID、actionStarted 是订单号及其运行的动作占用标记。
	for orderID, actionStarted := range map[string]int{started: 1, cleared: 0} {
		seedCatchupOrder(t, s, orderID, cookieID, "item-1", "chat-"+orderID, "pending_ship")
		// runID 保存本订单运行夹具的主键。
		runID := seedCatchupRun(t, s, ruleID, cookieID, orderID)
		// updateErr 保存把运行改写为人工核对形态的错误。
		if _, updateErr := s.DB.ExecContext(ctx, `UPDATE automation_runs SET status='needs_review', attempt_count=1, action_cursor=1, action_started=?, next_retry_at=0, lease_expires_at=0 WHERE id=?`, actionStarted, runID); updateErr != nil {
			t.Fatal(updateErr)
		}
		runIDs[orderID] = runID
	}

	// resumes、resumeErr 保存续跑扫描结果。
	resumes, resumeErr := s.Automation.PendingShipResumableRunsAfter(ctx, "", 5, 200)
	if resumeErr != nil {
		t.Fatal(resumeErr)
	}
	if len(resumes) != 1 || resumes[0].Order.OrderID != cleared || resumes[0].RunID != runIDs[cleared] {
		t.Fatalf("续跑候选只应包含已清除动作占用的运行: %+v", resumes)
	}

	// reopened、reopenErr 验证候选与重开条件一致：候选运行可以真正重开。
	reopened, reopenErr := s.Automation.ReopenRunForRecovery(ctx, runIDs[cleared], 1, time.Now().Add(time.Minute).Unix())
	if reopenErr != nil || !reopened {
		t.Fatalf("候选运行应可重开: reopened=%v err=%v", reopened, reopenErr)
	}
	// startedReopened、startedErr 验证被排除的运行确实无法重开，排除不会丢失任何可恢复运行。
	startedReopened, startedErr := s.Automation.ReopenRunForRecovery(ctx, runIDs[started], 1, time.Now().Add(time.Minute).Unix())
	if startedErr != nil || startedReopened {
		t.Fatalf("动作已开始的运行不得被重开: reopened=%v err=%v", startedReopened, startedErr)
	}
}
