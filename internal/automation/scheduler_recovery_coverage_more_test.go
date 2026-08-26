package automation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// seedRecoverableSchedulerRun 创建一条未进入外部动作、可由恢复调度器领取的运行记录。
func seedRecoverableSchedulerRun(t *testing.T, store *db.Store, ctx context.Context, suffix string, raw any) int64 {
	// admin、adminErr 保存恢复规则所属管理员及查询错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// ruleID、ruleErr 保存恢复运行引用的规则及创建错误。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "recovery-" + suffix, TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "recovery", Enabled: true}},
	})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// rawJSON、marshalErr 保存恢复任务快照及序列化错误。
	rawJSON, marshalErr := json.Marshal(raw)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	// runID、started、startErr 保存初始运行记录及抢占结果。
	runID, started, startErr := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "cid", OrderID: "recovery-" + suffix, TriggerType: TriggerBuyerReviewed,
		TriggerKey: "recovery:" + suffix, RawEventJSON: string(rawJSON), LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if startErr != nil || !started {
		t.Fatalf("创建恢复运行失败: started=%v err=%v", started, startErr)
	}
	// updateErr 保存清除租约和设置立即恢复时间的错误。
	if _, updateErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0,next_retry_at=0 WHERE id=?`, runID); updateErr != nil {
		t.Fatal(updateErr)
	}
	return runID
}

// TestSchedulerRecoveryCoversInvalidSnapshotAndAccountPostpone 验证恢复调度器对非法快照和账号阻断的收口分支。
func TestSchedulerRecoveryCoversInvalidSnapshotAndAccountPostpone(t *testing.T) {
	// store、cleanup 保存恢复调度器测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是恢复运行数据库操作共用的上下文。
	ctx := context.Background()
	// invalidRunID 保存非法 JSON 快照对应的运行主键。
	invalidRunID := seedRecoverableSchedulerRun(t, store, ctx, "invalid", "not-an-object")
	// invalidErr 保存把快照直接替换为非法 JSON 的更新错误。
	if _, invalidErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET raw_event_json='{"broken' WHERE id=?`, invalidRunID); invalidErr != nil {
		t.Fatal(invalidErr)
	}
	// invalidScheduler 负责把非法快照隔离到人工核对状态。
	invalidScheduler := &Scheduler{center: New(store, nil, nil)}
	// invalidResultErr 保存非法快照隔离结果的错误。
	if invalidResultErr := invalidScheduler.runRecoveryTasks(ctx); invalidResultErr != nil {
		t.Fatalf("非法快照隔离不应失败: %v", invalidResultErr)
	}
	// invalidRun、invalidGetErr 保存隔离后的运行记录。
	invalidRun, invalidGetErr := store.Automation.GetRun(ctx, invalidRunID)
	if invalidGetErr != nil || invalidRun.Status != "needs_review" {
		t.Fatalf("非法快照状态=%+v err=%v", invalidRun, invalidGetErr)
	}
	// blockedRunID 保存账号被暂停时应延期的恢复运行主键。
	blockedRunID := seedRecoverableSchedulerRun(t, store, ctx, "blocked", Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "blocked"})
	// pauseErr 保存暂停账号的状态写入错误。
	_, pauseErr := store.Cookies.SetPause(ctx, "cid", 60)
	if pauseErr != nil {
		t.Fatal(pauseErr)
	}
	// blockedScheduler 负责验证账号状态门禁会将任务移到恢复队列尾部。
	blockedScheduler := &Scheduler{center: New(store, nil, nil)}
	// blockedResultErr 保存账号阻断延期结果的错误。
	if blockedResultErr := blockedScheduler.runRecoveryTasks(ctx); blockedResultErr != nil {
		t.Fatalf("账号阻断延期不应失败: %v", blockedResultErr)
	}
	// blockedRun、blockedGetErr 保存延期后的运行记录。
	blockedRun, blockedGetErr := store.Automation.GetRun(ctx, blockedRunID)
	if blockedGetErr != nil || blockedRun.LeaseExpiresAt <= time.Now().UTC().Unix() {
		t.Fatalf("账号阻断延期状态=%+v err=%v", blockedRun, blockedGetErr)
	}
	// resetErr 保存把延期运行再次置为到期以验证下一次延期写入失败的错误。
	if _, resetErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, blockedRunID); resetErr != nil {
		t.Fatal(resetErr)
	}
	// triggerErr 保存阻止延期写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_scheduler_postpone
		BEFORE UPDATE OF lease_expires_at ON automation_runs
		BEGIN SELECT RAISE(ABORT, 'forced scheduler postpone failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// retryErr 保存延期落库失败后的统一错误。
	retryErr := blockedScheduler.runRecoveryTasks(ctx)
	if retryErr == nil || !strings.Contains(retryErr.Error(), "forced scheduler postpone failure") {
		t.Fatalf("延期写入失败未返回错误: %v", retryErr)
	}
}
