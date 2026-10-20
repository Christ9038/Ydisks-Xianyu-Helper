package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// paidGateFixture 保存付款发货状态门禁测试共用的账号、规则、发送器和中心。
type paidGateFixture struct {
	// store 是隔离的测试数据库。
	store *db.Store
	// center 使用真实事实记录、规则匹配和运行幂等流程。
	center *Center
	// sender 是记录发货内容的本地假发送器，不访问平台。
	sender *testSender
	// ruleID 是账号唯一的付款发货规则。
	ruleID int64
}

// newPaidGateFixture 建立带启用账号、卡密与付款规则的门禁测试环境，返回夹具和资源释放函数。
func newPaidGateFixture(t *testing.T) (paidGateFixture, func()) {
	t.Helper()
	// cleanup、store 分别保存：测试数据库的关闭函数；隔离的测试数据库仓储。
	store, cleanup := newAutomationTestStore(t)
	// ctx 限定夹具创建期间的同步数据库调用。
	ctx := context.Background()
	// admin、adminErr 分别保存：测试管理员账号；读取管理员的错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试管理员失败: %v", adminErr)
	}
	// cardID 是付款动作完整性校验所需的卡密组。
	cardID := catchupCard(t, store, admin.ID)
	catchupAccount(t, store, admin.ID, "gate-acc")
	// sender 是在线的假发送器。
	sender := &testSender{}
	return paidGateFixture{
		store:  store,
		center: New(store, testSenderProvider{sender: sender}, nil),
		sender: sender,
		ruleID: catchupRule(t, store, admin.ID, "gate-acc", "gate-item", []db.AutomationActionInput{catchupCardAction(cardID)}),
	}, cleanup
}

// seedOrder 写入指定状态的订单事实；systemShipped 为真时同时标记系统已发货。
func (f paidGateFixture) seedOrder(t *testing.T, orderID, status string, systemShipped bool) {
	t.Helper()
	// opts 是订单夹具字段，规格与规则动作保持一致。
	opts := db.OrderUpsertOpts{ItemID: "gate-item", BuyerID: "gate-buyer", CookieID: "gate-acc", ChatID: "gate-chat", OrderStatus: status, Quantity: "1", Amount: "9.90", SpecName: "颜色", SpecValue: "黑"}
	if systemShipped {
		opts.SystemShipped = &systemShipped
	}
	// err 保存当前调用返回的错误，非空时测试立即失败。
	if err := f.store.Orders.Upsert(context.Background(), orderID, opts); err != nil {
		t.Fatalf("写入订单夹具失败: %v", err)
	}
}

// paidTask 构造明确卖家角色的 WebSocket 付款任务，模拟迟到的付款卡片。
func paidTask(orderID string) Task {
	return Task{Source: "ws", AccountID: "gate-acc", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, ChatID: "gate-chat", OrderID: orderID, ItemID: "gate-item", BuyerID: "gate-buyer", Text: "买家已付款"}
}

// runCount 返回订单的付款运行数量。
func (f paidGateFixture) runCount(t *testing.T, orderID string) int {
	t.Helper()
	// count 保存付款运行数量。
	var count int
	// err 保存当前调用返回的错误，非空时测试立即失败。
	if err := f.store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM automation_runs WHERE order_id=? AND trigger_type='order_paid'`, orderID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestPaidDeliveryBlockedReason 验证门禁只拦截已越过待发货阶段的订单，对未知状态与缺失订单放行。
func TestPaidDeliveryBlockedReason(t *testing.T) {
	// cases 覆盖拦截、放行与空订单边界。
	cases := []struct {
		// name 是用例名称。
		name string
		// order 是输入订单。
		order *db.Order
		// want 是期望的拦截原因。
		want string
	}{
		{name: "缺失订单放行", order: nil},
		{name: "未知状态放行", order: &db.Order{OrderStatus: ""}},
		{name: "处理中放行", order: &db.Order{OrderStatus: "processing"}},
		{name: "待发货放行", order: &db.Order{OrderStatus: "pending_ship"}},
		{name: "数字待发货放行", order: &db.Order{OrderStatus: "2"}},
		{name: "已发货拦截", order: &db.Order{OrderStatus: "shipped"}, want: "order_shipped"},
		{name: "数字已发货拦截", order: &db.Order{OrderStatus: "3"}, want: "order_shipped"},
		{name: "已完成拦截", order: &db.Order{OrderStatus: "completed"}, want: "order_completed"},
		{name: "已取消拦截", order: &db.Order{OrderStatus: "cancelled"}, want: "order_cancelled"},
		{name: "退款中拦截", order: &db.Order{OrderStatus: "refunding"}, want: "order_refunding"},
		{name: "部分发货拦截", order: &db.Order{OrderStatus: "partial_success"}, want: "order_partial_success"},
		{name: "部分发货待收口拦截", order: &db.Order{OrderStatus: "partial_pending_finalize"}, want: "order_partial_pending_finalize"},
		{name: "系统已发货拦截", order: &db.Order{OrderStatus: "pending_ship", SystemShipped: true}, want: "system_shipped"},
	}
	// testCase 是当前待验证的边界。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 保存被测函数的实际返回值，用于与期望值比较。
			if got := paidDeliveryBlockedReason(testCase.order); got != testCase.want {
				t.Fatalf("paidDeliveryBlockedReason=%q want %q", got, testCase.want)
			}
		})
	}
}

// TestPaidEventSkipsEndedOrdersAndDeliversUnknownStatus 验证迟到的卖家付款事件不会对已结束订单发卡，未知状态订单仍正常发货。
func TestPaidEventSkipsEndedOrdersAndDeliversUnknownStatus(t *testing.T) {
	// blocked 列出必须拦截的订单状态与系统已发货标记。
	blocked := []struct {
		// status 是订单状态。
		status string
		// systemShipped 表示是否系统已发货。
		systemShipped bool
	}{{"shipped", false}, {"completed", false}, {"cancelled", false}, {"refunding", false}, {"partial_success", false}, {"partial_pending_finalize", false}, {"pending_ship", true}}
	// item 是当前被拦截的订单夹具。
	for index, item := range blocked {
		// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
		fixture, cleanup := newPaidGateFixture(t)
		// orderID 是当前订单号。
		orderID := "gate-blocked-" + string(rune('a'+index))
		fixture.seedOrder(t, orderID, item.status, item.systemShipped)
		// err 保存当前调用返回的错误，非空时测试立即失败。
		if err := fixture.center.HandleTask(context.Background(), paidTask(orderID)); err != nil {
			t.Fatalf("迟到付款事件应被安全忽略: %v", err)
		}
		if len(fixture.sender.texts) != 0 || fixture.runCount(t, orderID) != 0 {
			t.Fatalf("状态 %s/system=%v 不应发卡或创建运行: texts=%v runs=%d", item.status, item.systemShipped, fixture.sender.texts, fixture.runCount(t, orderID))
		}
		cleanup()
	}

	// 状态未知的订单是付款事件的正常入口，必须继续发货。
	fixture, cleanup := newPaidGateFixture(t)
	defer cleanup()
	fixture.seedOrder(t, "gate-unknown", "unknown", false)
	// err 保存当前调用返回的错误，非空时测试立即失败。
	if err := fixture.center.HandleTask(context.Background(), paidTask("gate-unknown")); err != nil {
		t.Fatal(err)
	}
	if len(fixture.sender.texts) == 0 || fixture.runCount(t, "gate-unknown") != 1 {
		t.Fatalf("未知状态订单应正常发货: texts=%v runs=%d", fixture.sender.texts, fixture.runCount(t, "gate-unknown"))
	}
}

// TestDeferredReplayOfPaidEventSkipsCancelledOrder 验证延期任务重放时订单已取消则不再发卡。
func TestDeferredReplayOfPaidEventSkipsCancelledOrder(t *testing.T) {
	// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
	fixture, cleanup := newPaidGateFixture(t)
	defer cleanup()
	fixture.seedOrder(t, "gate-replay", "cancelled", false)
	// task 模拟从延期队列取出的重放任务，带有延期重放标记。
	task := paidTask("gate-replay")
	task.Raw = map[string]any{"automation_deferred_replay": true}
	// err 保存当前调用返回的错误，非空时测试立即失败。
	if err := fixture.center.HandleTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if len(fixture.sender.texts) != 0 || fixture.runCount(t, "gate-replay") != 0 {
		t.Fatalf("延期重放不应对已取消订单发卡: texts=%v", fixture.sender.texts)
	}
}

// TestResumedPaidRunWithoutProgressIsCanceledWhenOrderEnded 验证零进度的恢复运行在订单已结束时被取消，已有进度的运行不被取消。
func TestResumedPaidRunWithoutProgressIsCanceledWhenOrderEnded(t *testing.T) {
	// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
	fixture, cleanup := newPaidGateFixture(t)
	defer cleanup()
	// ctx 限定本测试内的同步调用。
	ctx := context.Background()
	fixture.seedOrder(t, "gate-resume", "cancelled", false)
	// runID 是零进度的运行中记录。
	runID := catchupRun(t, fixture.store, fixture.ruleID, "gate-acc", "gate-resume", "{}")
	// task 携带运行 ID，模拟延期或恢复链路继续执行既有运行。
	task := paidTask("gate-resume")
	task.Raw = map[string]any{"automation_run_id": runID}
	// err 保存当前调用返回的错误，非空时测试立即失败。
	if err := fixture.center.HandleTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	// run、getErr 是处理后的运行状态。
	run, getErr := fixture.store.Automation.GetRun(ctx, runID)
	if getErr != nil || run.Status != "canceled" {
		t.Fatalf("零进度恢复运行应被取消: run=%+v err=%v", run, getErr)
	}
	if len(fixture.sender.texts) != 0 {
		t.Fatalf("取消的运行不应发送内容: %v", fixture.sender.texts)
	}

	// 已有动作进度的运行保留凭证与现状，交给原恢复链路处理。
	fixture.seedOrder(t, "gate-resume-progress", "cancelled", false)
	// progressRunID 是已推进动作游标的运行。
	progressRunID := catchupRun(t, fixture.store, fixture.ruleID, "gate-acc", "gate-resume-progress", "{}")
	// err 保存当前调用返回的错误，非空时测试立即失败。
	if _, err := fixture.store.DB.ExecContext(ctx, `UPDATE automation_runs SET action_cursor=1 WHERE id=?`, progressRunID); err != nil {
		t.Fatal(err)
	}
	// progressTask 继续该运行。
	progressTask := paidTask("gate-resume-progress")
	progressTask.Raw = map[string]any{"automation_run_id": progressRunID}
	_ = fixture.center.HandleTask(ctx, progressTask)
	// progressRun 是处理后的运行状态，不得被门禁取消。
	progressRun, progressErr := fixture.store.Automation.GetRun(ctx, progressRunID)
	if progressErr != nil || progressRun.Status == "canceled" {
		t.Fatalf("已有进度的运行不应被门禁取消: run=%+v err=%v", progressRun, progressErr)
	}
}

// TestResumedPaidRunWithProgressStopsWhenOrderCancelledOrRefunding 验证发卡后订单被取消或退款时，延迟恢复的运行转人工核对且不再执行任何外部动作。
func TestResumedPaidRunWithProgressStopsWhenOrderCancelledOrRefunding(t *testing.T) {
	// status 依次覆盖取消与退款两种不可继续确认发货的订单终态。
	for _, status := range []string{"cancelled", "refunding"} {
		t.Run(status, func(t *testing.T) {
			// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
			fixture, cleanup := newPaidGateFixture(t)
			defer cleanup()
			// ctx 限定本测试内的同步调用。
			ctx := context.Background()
			// orderID 是当前状态对应的订单号。
			orderID := "gate-progress-" + status
			fixture.seedOrder(t, orderID, status, false)
			// runID 是已完成发卡动作、等待延迟后续动作的运行。
			runID := catchupRun(t, fixture.store, fixture.ruleID, "gate-acc", orderID, "{}")
			// err 保存当前调用返回的错误，非空时测试立即失败。
			if _, err := fixture.store.DB.ExecContext(ctx, `UPDATE automation_runs SET action_cursor=1,sent_count=1 WHERE id=?`, runID); err != nil {
				t.Fatal(err)
			}
			// task 携带运行 ID，模拟延迟任务到期继续执行。
			task := paidTask(orderID)
			task.Raw = map[string]any{"automation_run_id": runID}
			// handleErr 保存延迟任务处理结果，订单已结束时应为空表示安全收口。
			if handleErr := fixture.center.HandleTask(ctx, task); handleErr != nil {
				t.Fatalf("订单已结束的延迟任务应被安全收口: %v", handleErr)
			}
			// run、getErr 是处理后的运行状态。
			run, getErr := fixture.store.Automation.GetRun(ctx, runID)
			if getErr != nil || run.Status != "needs_review" || run.SentCount != 1 {
				t.Fatalf("已有进度的运行应转人工核对并保留发送计数: run=%+v err=%v", run, getErr)
			}
			if len(fixture.sender.texts) != 0 {
				t.Fatalf("人工核对的运行不应继续发送内容: %v", fixture.sender.texts)
			}
		})
	}
}

// TestResumedPaidRunWithProgressWhenOrderPastPendingShip 验证订单已越过待发货阶段时：已发货类订单取消旧运行，部分发货转人工核对，且都不再发送内容。
func TestResumedPaidRunWithProgressWhenOrderPastPendingShip(t *testing.T) {
	// cases 覆盖已发货、已完成、系统已发货与两种部分发货状态，cursor 覆盖剩余动作为空与仍有动作。
	cases := []struct {
		// name 是用例名称。
		name string
		// status 是订单状态。
		status string
		// systemShipped 表示订单是否被系统标记为已发货。
		systemShipped bool
		// cursor 是运行已完成的动作游标。
		cursor int
		// want 是运行期望的最终状态。
		want string
	}{
		{"已发货剩余发卡", "shipped", false, 0, "canceled"},
		{"已发货无剩余动作", "shipped", false, 1, "canceled"},
		{"已完成", "completed", false, 0, "canceled"},
		{"系统已发货", "pending_ship", true, 1, "canceled"},
		{"部分发货", "partial_success", false, 0, "needs_review"},
		{"部分发货待收口", "partial_pending_finalize", false, 1, "needs_review"},
	}
	// testCase 是当前待验证的订单与运行进度组合。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
			fixture, cleanup := newPaidGateFixture(t)
			defer cleanup()
			// ctx 限定本测试内的同步调用。
			ctx := context.Background()
			// orderID 是当前用例对应的订单号。
			orderID := "gate-past-pending"
			fixture.seedOrder(t, orderID, testCase.status, testCase.systemShipped)
			// runID 是已有发送计数的运行。
			runID := catchupRun(t, fixture.store, fixture.ruleID, "gate-acc", orderID, "{}")
			// execErr 保存构造运行进度的数据库错误。
			if _, execErr := fixture.store.DB.ExecContext(ctx, `UPDATE automation_runs SET action_cursor=?,sent_count=1 WHERE id=?`, testCase.cursor, runID); execErr != nil {
				t.Fatal(execErr)
			}
			// task 携带运行 ID，模拟恢复链路继续执行。
			task := paidTask(orderID)
			task.Raw = map[string]any{"automation_run_id": runID}
			// handleErr 保存恢复任务处理结果。
			if handleErr := fixture.center.HandleTask(ctx, task); handleErr != nil {
				t.Fatalf("应安全收口: %v", handleErr)
			}
			// run、getErr 是处理后的运行状态。
			run, getErr := fixture.store.Automation.GetRun(ctx, runID)
			if getErr != nil || run.Status != testCase.want {
				t.Fatalf("运行状态应为 %s: run=%+v err=%v", testCase.want, run, getErr)
			}
			if len(fixture.sender.texts) != 0 {
				t.Fatalf("不应再次发送内容: %v", fixture.sender.texts)
			}
		})
	}
}

// TestPaidEventWithoutOrderIDNeverDeliversEvenAsExplicitSeller 验证缺订单号的付款事件即使明确标为卖家、带更新键并匹配规则，也不发卡、不创建运行。
func TestPaidEventWithoutOrderIDNeverDeliversEvenAsExplicitSeller(t *testing.T) {
	// sources 覆盖 WebSocket 与非 WebSocket 来源，缺订单号的防线不依赖来源。
	for _, source := range []string{"ws", "api"} {
		t.Run(source, func(t *testing.T) {
			// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
			fixture, cleanup := newPaidGateFixture(t)
			defer cleanup()
			// task 是明确卖家角色、带更新键与买家信息但没有订单号的付款事件。
			task := paidTask("")
			task.Source = source
			task.UpdateKey = "gate-update-key"
			// handleErr 保存事件处理结果，缺订单号只能延期或忽略，不应报错。
			if handleErr := fixture.center.HandleTask(context.Background(), task); handleErr != nil {
				t.Fatalf("缺订单号事件不应返回错误: %v", handleErr)
			}
			// runs 保存付款运行总数。
			var runs int
			// queryErr 保存运行计数查询错误。
			if queryErr := fixture.store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM automation_runs`).Scan(&runs); queryErr != nil {
				t.Fatal(queryErr)
			}
			if runs != 0 || len(fixture.sender.texts) != 0 {
				t.Fatalf("缺订单号不得发卡: runs=%d texts=%v", runs, fixture.sender.texts)
			}
		})
	}
}
