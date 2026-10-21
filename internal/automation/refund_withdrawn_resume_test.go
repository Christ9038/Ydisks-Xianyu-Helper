package automation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestRefundWithdrawnResumeKeepsDeliveryProofForConfirmShipment 验证端到端恢复链：
// 已发卡并保存凭证 → 订单退款中取消运行 → 退款撤回回到待发货 → 续跑只确认发货，且带着原卡密凭证、不重发内容。
func TestRefundWithdrawnResumeKeepsDeliveryProofForConfirmShipment(t *testing.T) {
	// store、cleanup 保存测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// admin 保存规则所属管理员账号。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	catchupAccount(t, store, admin.ID, "refund-acc")
	// confirmErr 保存开启账号自动发货与「自动确认发货」开关的结果；关闭时续跑不会调用确认发货接口。
	if _, confirmErr := store.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=1,auto_consign=1 WHERE id='refund-acc'`); confirmErr != nil {
		t.Fatal(confirmErr)
	}
	// ruleID 是含发卡与确认发货的付款发货规则。
	ruleID := catchupRule(t, store, admin.ID, "refund-acc", "item-refund",
		[]db.AutomationActionInput{catchupCardAction(catchupCard(t, store, admin.ID)), {ActionType: ActionConfirmShipment, Enabled: true}})
	catchupOrder(t, store, "refund-acc", "o-refund", "item-refund", "chat-refund")
	// rawTask 是运行冻结快照，续跑从中恢复计划。
	rawTask := `{"Source":"scheduler","AccountID":"refund-acc","TriggerType":"order_paid","ChatID":"chat-refund","OrderID":"o-refund","ItemID":"item-refund","BuyerID":"buyer-o-refund","Text":"待发货运行未完成，按检查点续跑",` +
		`"ActionPlan":[{"ActionType":"send_card","Enabled":true,"ConfigJSON":"{\"spec_name\":\"颜色\",\"spec_value\":\"黑\"}"},{"ActionType":"confirm_shipment","Enabled":true}]}`
	// runID 是已发卡、尚未确认发货的付款运行。
	runID := catchupRun(t, store, ruleID, "refund-acc", "o-refund", rawTask)
	// run 是用于保存凭证的运行快照。
	run, getErr := store.Automation.GetRun(ctx, runID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	// proof 是卡密已成功发送给买家的凭证。
	proof := db.AutomationDeliveryProof{TradeText: "SAVED-CARD", Messages: []db.AutomationDeliveryMessage{{Kind: "text", Content: "SAVED-CARD"}}, ExpectedUnits: 1, PreparedUnits: 1}
	// proofErr 保存发货凭证与失败状态的落库结果。
	if proofErr := store.Automation.QuarantineRunResultWithProof(ctx, runID, run.AttemptCount, 1, "确认发货结果不确定", &proof); proofErr != nil {
		t.Fatal(proofErr)
	}
	setRunState(t, store, runID, "failed", 1, 1)

	// 订单进入退款中，调度器取消运行；凭证必须保留。
	if refundErr := store.Orders.Upsert(ctx, "o-refund", db.OrderUpsertOpts{CookieID: "refund-acc", OrderStatus: "refunding", Quantity: "1", Amount: "9.90", SpecName: "颜色", SpecValue: "黑", PlatformSnapshot: true}); refundErr != nil {
		t.Fatal(refundErr)
	}
	// stale 是取消用的最新运行快照。
	stale, staleErr := store.Automation.GetRun(ctx, runID)
	if staleErr != nil {
		t.Fatal(staleErr)
	}
	// canceled、cancelErr 保存条件取消结果。
	canceled, cancelErr := store.Automation.CancelObsoletePaidRecoveryRun(ctx, *stale, "订单退款中")
	if cancelErr != nil || !canceled {
		t.Fatalf("取消失败: canceled=%v err=%v", canceled, cancelErr)
	}
	// 买家撤回退款，平台快照使订单回到待发货。
	if withdrawErr := store.Orders.Upsert(ctx, "o-refund", db.OrderUpsertOpts{CookieID: "refund-acc", OrderStatus: "pending_ship", Quantity: "1", Amount: "9.90", SpecName: "颜色", SpecValue: "黑", PlatformSnapshot: true}); withdrawErr != nil {
		t.Fatal(withdrawErr)
	}

	// sender 记录是否错误地再次发送卡密。
	sender := &testSender{}
	// platform 记录确认发货收到的凭证。
	platform := &fakeMTop{consignOk: true}
	// scheduler 使用真实存储与平台替身驱动续跑扫描。
	scheduler := &Scheduler{center: NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: platform}),
		pendingShipCooldown: map[string]time.Time{}, pendingShipSettleWindow: -1}
	scheduler.scanPendingShipResumes(ctx)

	if platform.consignCalls != 1 || platform.consignTradeTextIn != "SAVED-CARD" {
		t.Fatalf("确认发货应带原卡密凭证: calls=%d tradeText=%q", platform.consignCalls, platform.consignTradeTextIn)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("续跑不得重发卡密: %v", sender.texts)
	}
}

// TestRefundWithdrawnResumeWithoutProofGoesToManualReview 验证旧版本遗留的「已取消、游标已推进但凭证已清空」运行：
// 退款撤回后不得提交空凭证确认发货，而是转入人工核对且之后不再被自动续跑扫描反复选中。
func TestRefundWithdrawnResumeWithoutProofGoesToManualReview(t *testing.T) {
	// store、cleanup 保存测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// admin 保存规则所属管理员账号。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	catchupAccount(t, store, admin.ID, "legacy-acc")
	// confirmErr 保存开启账号自动发货与自动确认发货开关的结果。
	if _, confirmErr := store.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=1,auto_consign=1 WHERE id='legacy-acc'`); confirmErr != nil {
		t.Fatal(confirmErr)
	}
	// ruleID 是含发卡与确认发货的付款发货规则。
	ruleID := catchupRule(t, store, admin.ID, "legacy-acc", "item-legacy",
		[]db.AutomationActionInput{catchupCardAction(catchupCard(t, store, admin.ID)), {ActionType: ActionConfirmShipment, Enabled: true}})
	catchupOrder(t, store, "legacy-acc", "o-legacy", "item-legacy", "chat-legacy")
	// rawTask 是运行冻结快照。
	rawTask := `{"Source":"scheduler","AccountID":"legacy-acc","TriggerType":"order_paid","ChatID":"chat-legacy","OrderID":"o-legacy","ItemID":"item-legacy","BuyerID":"buyer-o-legacy",` +
		`"ActionPlan":[{"ActionType":"send_card","Enabled":true,"ConfigJSON":"{\"spec_name\":\"颜色\",\"spec_value\":\"黑\"}"},{"ActionType":"confirm_shipment","Enabled":true}]}`
	// runID 是模拟 07453c1 取消后的历史记录：带可续跑标记、游标已推进、凭证为空。
	runID := catchupRun(t, store, ruleID, "legacy-acc", "o-legacy", rawTask)
	// seedErr 保存历史遗留状态的写入结果。
	if _, seedErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status='canceled',error_message=?,action_cursor=1,sent_count=1,action_started=0,delivery_proof='',attempt_count=1 WHERE id=?`, db.ObsoleteRunReasonMarker+"订单退款中", runID); seedErr != nil {
		t.Fatal(seedErr)
	}
	// notifier 记录人工处理通知是否入队。
	notifier := &recordingNotifier{}
	// sender 记录是否错误地重发内容。
	sender := &testSender{}
	// platform 记录是否错误地提交确认发货。
	platform := &fakeMTop{consignOk: true}
	// scheduler 使用真实存储与平台替身驱动续跑扫描。
	scheduler := &Scheduler{center: NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: platform, Notifier: notifier}),
		pendingShipCooldown: map[string]time.Time{}, pendingShipSettleWindow: -1}
	// 连续扫描两轮：第一轮隔离，第二轮不得再次选中或提交。
	scheduler.scanPendingShipResumes(ctx)
	scheduler.scanPendingShipResumes(ctx)

	// run、runErr 保存扫描后的运行记录。
	run, runErr := store.Automation.GetRun(ctx, runID)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if run.Status != "needs_review" || run.AttemptCount != 1 {
		t.Fatalf("缺凭证运行应转人工核对且不重开: status=%s attempt=%d", run.Status, run.AttemptCount)
	}
	// 通知只在隔离成功的那一轮发送一次，且不依赖运行回读。
	if len(notifier.calls) != 1 || notifier.calls[0].runID != runID || notifier.calls[0].status != "needs_review" {
		t.Fatalf("缺凭证运行应恰好发送一次人工处理通知: %+v", notifier.calls)
	}
	if platform.consignCalls != 0 || len(sender.texts) != 0 {
		t.Fatalf("不得用空凭证确认发货或重发内容: confirms=%d texts=%v", platform.consignCalls, sender.texts)
	}
}

// preparingNotifier 是支持事务内入队的通知器替身：prepareErr 非空时模拟渠道查询失败，否则返回写向 channelID 的一条 outbox 记录。
type preparingNotifier struct {
	// channelID 是真实存在的通知渠道主键，满足 outbox 外键。
	channelID int64
	// prepareErr 是渠道查询失败时返回的错误。
	prepareErr error
	// legacyCalls 记录被回退路径调用的次数，事务内入队时必须为零。
	legacyCalls int
}

// NotifyAutomationRun 满足 Notifier 接口；事务内入队路径不应调用它。
func (p *preparingNotifier) NotifyAutomationRun(context.Context, int64, string, string, string, string, string, string) {
	p.legacyCalls++
}

// PrepareAutomationRunOutbox 返回待与状态更新同事务写入的 outbox 记录。
func (p *preparingNotifier) PrepareAutomationRunOutbox(_ context.Context, _ string, runID int64, _, _, _, status, message, _ string) ([]db.NotificationOutboxInput, error) {
	if p.prepareErr != nil {
		return nil, p.prepareErr
	}
	return []db.NotificationOutboxInput{{ChannelID: p.channelID, EventType: "manual_intervention_required", Body: message, IdempotencyKey: fmt.Sprintf("automation-run:%d:%s", runID, status)}}, nil
}

// seedMissingProofRun 写入一条退款撤回后缺凭证的历史已取消运行和账号夹具，返回存储、运行主键与通知渠道主键。
func seedMissingProofRun(t *testing.T, accountID string) (*db.Store, int64, int64, func()) {
	t.Helper()
	// store、cleanup 保存测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	// ctx 保存夹具共用的数据库上下文。
	ctx := context.Background()
	// admin 保存规则所属管理员账号。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	catchupAccount(t, store, admin.ID, accountID)
	// ruleID 是含发卡与确认发货的付款发货规则。
	ruleID := catchupRule(t, store, admin.ID, accountID, "item-"+accountID,
		[]db.AutomationActionInput{catchupCardAction(catchupCard(t, store, admin.ID)), {ActionType: ActionConfirmShipment, Enabled: true}})
	catchupOrder(t, store, accountID, "o-"+accountID, "item-"+accountID, "chat-"+accountID)
	// rawTask 是运行冻结快照。
	rawTask := `{"Source":"scheduler","AccountID":"` + accountID + `","TriggerType":"order_paid","ChatID":"chat-` + accountID + `","OrderID":"o-` + accountID + `","ItemID":"item-` + accountID + `","BuyerID":"buyer-o-` + accountID + `",` +
		`"ActionPlan":[{"ActionType":"send_card","Enabled":true,"ConfigJSON":"{\"spec_name\":\"颜色\",\"spec_value\":\"黑\"}"},{"ActionType":"confirm_shipment","Enabled":true}]}`
	// runID 是缺凭证的历史已取消运行。
	runID := catchupRun(t, store, ruleID, accountID, "o-"+accountID, rawTask)
	// seedErr 保存历史状态写入结果。
	if _, seedErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status='canceled',error_message=?,action_cursor=1,sent_count=1,action_started=0,delivery_proof='',attempt_count=1 WHERE id=?`, db.ObsoleteRunReasonMarker+"订单退款中", runID); seedErr != nil {
		t.Fatal(seedErr)
	}
	// channelResult、channelErr 保存满足 outbox 外键的通知渠道。
	channelResult, channelErr := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('测试','webhook','{}',1,?)`, admin.ID)
	if channelErr != nil {
		t.Fatal(channelErr)
	}
	// channelID 是新建渠道主键。
	channelID, _ := channelResult.LastInsertId()
	return store, runID, channelID, cleanup
}

// TestMissingProofQuarantineAndNotificationAreAtomic 验证人工处理通知与隔离同事务：
// 渠道查询失败或事务内入队失败时运行保持已取消且没有任何通知；成功时状态与 outbox 同时生效且不走非事务回退。
func TestMissingProofQuarantineAndNotificationAreAtomic(t *testing.T) {
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// outboxCount 统计指定运行的 outbox 记录数。
	outboxCount := func(store *db.Store, runID int64) int {
		// count 保存匹配幂等键的记录数。
		var count int
		if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox WHERE idempotency_key=?`, fmt.Sprintf("automation-run:%d:needs_review", runID)).Scan(&count); err != nil { // err 保存计数查询错误。
			t.Fatal(err)
		}
		return count
	}
	// newScheduler 构造使用给定通知器的调度器。
	newScheduler := func(store *db.Store, notifier Notifier) *Scheduler {
		return &Scheduler{center: NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{MTop: &fakeMTop{consignOk: true}, Notifier: notifier}),
			pendingShipCooldown: map[string]time.Time{}, pendingShipSettleWindow: -1}
	}
	// statusOf 读取运行当前状态。
	statusOf := func(store *db.Store, runID int64) string {
		// run、err 保存最新运行记录。
		run, err := store.Automation.GetRun(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		return run.Status
	}

	t.Run("渠道查询失败保持原状并可重试", func(t *testing.T) {
		// store、runID、channelID、cleanup 保存夹具存储、缺凭证运行、通知渠道及释放函数。
		store, runID, channelID, cleanup := seedMissingProofRun(t, "atomic-a")
		defer cleanup()
		// notifier 先模拟渠道查询失败，再恢复。
		notifier := &preparingNotifier{channelID: channelID, prepareErr: context.DeadlineExceeded}
		// scheduler 使用该通知器驱动续跑扫描。
		scheduler := newScheduler(store, notifier)
		scheduler.scanPendingShipResumes(ctx)
		if statusOf(store, runID) != "canceled" || outboxCount(store, runID) != 0 {
			t.Fatalf("渠道查询失败时应保持已取消且无通知")
		}
		notifier.prepareErr = nil
		scheduler.scanPendingShipResumes(ctx)
		if statusOf(store, runID) != "needs_review" || outboxCount(store, runID) != 1 || notifier.legacyCalls != 0 {
			t.Fatalf("恢复后应同时转人工核对并写入一条通知: status=%s outbox=%d legacy=%d", statusOf(store, runID), outboxCount(store, runID), notifier.legacyCalls)
		}
	})

	t.Run("事务内入队失败回滚状态", func(t *testing.T) {
		// store、runID、channelID、cleanup 保存夹具存储、缺凭证运行、通知渠道及释放函数。
		store, runID, channelID, cleanup := seedMissingProofRun(t, "atomic-b")
		defer cleanup()
		// 指向不存在渠道的记录会触发外键失败，模拟「状态已更新但通知写入失败」。
		notifier := &preparingNotifier{channelID: channelID + 999}
		newScheduler(store, notifier).scanPendingShipResumes(ctx)
		if statusOf(store, runID) != "canceled" || outboxCount(store, runID) != 0 {
			t.Fatalf("通知写入失败时状态更新必须回滚: status=%s", statusOf(store, runID))
		}
	})

	t.Run("并发状态变化零行更新不发通知", func(t *testing.T) {
		// store、runID、channelID、cleanup 保存夹具存储、缺凭证运行、通知渠道及释放函数。
		store, runID, channelID, cleanup := seedMissingProofRun(t, "atomic-c")
		defer cleanup()
		// run、runErr 保存扫描时的运行快照，随后让运行状态被其他流程改变。
		if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET attempt_count=2 WHERE id=?`, runID); err != nil { // err 保存模拟并发代次变化的结果。
			t.Fatal(err)
		}
		// quarantined、err 保存使用过期代次的条件隔离结果。
		quarantined, err := store.Automation.QuarantineCanceledRunMissingProof(ctx, runID, 1, "原因", []db.NotificationOutboxInput{{ChannelID: channelID, EventType: "x", Body: "b", IdempotencyKey: fmt.Sprintf("automation-run:%d:needs_review", runID)}})
		if err != nil || quarantined {
			t.Fatalf("过期代次不应隔离: quarantined=%v err=%v", quarantined, err)
		}
		if outboxCount(store, runID) != 0 {
			t.Fatal("零行更新不得留下通知")
		}
	})
}
