package automation

import (
	"context"
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
