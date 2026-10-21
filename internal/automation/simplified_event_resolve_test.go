package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// seedSimplifiedEventFixture 写入卖家商品和一笔指定状态的订单，供简化系统事件（只含会话标识）测试复用。
func seedSimplifiedEventFixture(t *testing.T, store *db.Store, orderStatus string) {
	t.Helper()
	// ctx 限制夹具写入的生命周期。
	ctx := context.Background()
	// itemErr 保存本账号卖家商品归属事实的写入错误。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "simple-item", ItemTitle: "简化事件商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// orderErr 保存订单事实写入错误；会话、买家、商品与简化事件一致。
	if orderErr := store.Orders.Upsert(ctx, "simple-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "simple-item", BuyerID: "simple-buyer", ChatID: "simple-chat",
		OrderStatus: orderStatus, Quantity: "1", Amount: "9.90",
	}); orderErr != nil {
		t.Fatal(orderErr)
	}
}

// countPendingAndRuns 返回延期任务数和自动化运行数，用于断言简化事件没有产生副作用。
func countPendingAndRuns(t *testing.T, store *db.Store) (pending, runs int) {
	t.Helper()
	// ctx 限制计数查询的生命周期。
	ctx := context.Background()
	// queryErr 保存延期任务计数查询错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks`).Scan(&pending); queryErr != nil {
		t.Fatal(queryErr)
	}
	// queryErr 保存运行计数查询错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs`).Scan(&runs); queryErr != nil {
		t.Fatal(queryErr)
	}
	return pending, runs
}

// simplifiedPaidTask 构造带会话、买家和商品证据但不含订单号与角色的简化付款系统事件。
func simplifiedPaidTask() Task {
	return Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, ChatID: "simple-chat", BuyerID: "simple-buyer", ItemID: "simple-item"}
}

// chatOnlyPaidTask 构造只含账号和会话标识的付款事件，无法证明订单身份。
func chatOnlyPaidTask() Task {
	return Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, ChatID: "simple-chat"}
}

// TestPaidEventWithoutOrderIDNeverGuessesOrder 验证缺订单号的付款事件不按会话猜测订单：
// 即使带买家和商品，同会话存在待发货或已发货订单时也不发卡、不创建运行，而是延期等待订单同步。
func TestPaidEventWithoutOrderIDNeverGuessesOrder(t *testing.T) {
	// tasks 覆盖只有会话标识，以及带买家和商品但仍无订单号的两种付款事件。
	tasks := map[string]Task{"仅会话": chatOnlyPaidTask(), "会话买家商品": simplifiedPaidTask()}
	// status 依次覆盖同会话旧订单仍待发货与已发货两种情形。
	for _, status := range []string{"pending_ship", "shipped"} {
		// name、task 是当前事件形态及其任务。
		for name, task := range tasks {
			t.Run(status+"/"+name, func(t *testing.T) {
				// store、cleanup 提供隔离数据库及关闭责任。
				store, cleanup := newAutomationTestStore(t)
				defer cleanup()
				seedSimplifiedEventFixture(t, store, status)
				// sender 记录任何不应发生的买家消息副作用。
				sender := &testSender{}
				// center 使用真实事实记录和运行协调器。
				center := New(store, testSenderProvider{sender: sender}, nil)
				// handleErr 保存事件处理结果，期望延期而非报错。
				if handleErr := center.HandleTask(context.Background(), task); handleErr != nil {
					t.Fatalf("缺订单号事件丢弃不应返回错误: %v", handleErr)
				}
				// pending、runs 是处理后的延期任务数与运行数。
				pending, runs := countPendingAndRuns(t, store)
				if pending != 0 || runs != 0 || len(sender.texts) != 0 {
					t.Fatalf("缺订单号事件应被丢弃且不关联订单: pending=%d runs=%d messages=%v", pending, runs, sender.texts)
				}
			})
		}
	}
}

// TestSimplifiedPaidEventStillDeliversPendingShipOrder 验证待发货订单只在付款事件携带订单号时发卡，缺订单号的事件不会抢先发货。
func TestSimplifiedPaidEventStillDeliversPendingShipOrder(t *testing.T) {
	// store、cleanup 提供隔离数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// 本用例只验证发货与恢复行为，关闭账号自动确认发货，避免规则缺少确认动作时的隐式确认步骤访问平台；隐式确认由专门用例覆盖。
	disableAutoConsign(t, store)
	// ctx 限制本测试的规则创建与事件处理生命周期。
	ctx := context.Background()
	seedSimplifiedEventFixture(t, store, "pending_ship")
	// admin、adminErr 提供卡密组和规则的管理用户归属。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试管理员失败: %v", adminErr)
	}
	// cardID、cardErr 保存本地一次性卡密组及创建错误。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "简化事件卡", Type: "text", TextContent: "SIMPLE-CARD", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleErr 保存商品级付款发卡规则创建错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", ItemID: "simple-item", Name: "简化事件规则", TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true}}}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 记录实际发出的卡密内容。
	sender := &testSender{}
	// center 使用真实事实记录和运行协调器。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// guessErr 保存缺订单号事件的处理结果，期望延期且不发卡。
	if guessErr := center.HandleTask(ctx, simplifiedPaidTask()); guessErr != nil {
		t.Fatalf("缺订单号事件应延期: %v", guessErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("缺订单号事件不应发卡: %v", sender.texts)
	}
	// withOrder 是携带订单号的同一笔付款事件。
	withOrder := simplifiedPaidTask()
	withOrder.OrderID = "simple-order"
	// handleErr 保存带订单号事件的处理结果，期望为 nil。
	if handleErr := center.HandleTask(ctx, withOrder); handleErr != nil {
		t.Fatalf("带订单号的待发货事件应正常处理: %v", handleErr)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "SIMPLE-CARD" {
		t.Fatalf("带订单号事件未按待发货订单发卡: %v", sender.texts)
	}
}

// TestSimplifiedPaidEventWithoutRecentOrderIsDropped 验证没有任何近期订单时，缺订单号的简化事件被丢弃而不进入延期队列（重放无法补全订单号，订单同步后的兜底由待发货扫描负责）。
func TestSimplifiedPaidEventWithoutRecentOrderIsDropped(t *testing.T) {
	// store、cleanup 提供隔离数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 使用真实事实记录和运行协调器。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// handleErr 保存简化事件处理结果，期望为 nil。
	if handleErr := center.HandleTask(context.Background(), simplifiedPaidTask()); handleErr != nil {
		t.Fatalf("无订单的简化事件丢弃不应返回错误: %v", handleErr)
	}
	// pending、runs 是延期任务数与运行数。
	pending, runs := countPendingAndRuns(t, store)
	if pending != 0 || runs != 0 {
		t.Fatalf("无订单时缺订单号事件应被丢弃: pending=%d runs=%d", pending, runs)
	}
}

// TestSimplifiedPaidEventOutsideWindowIsDropped 验证订单更新时间超出回溯窗口时不会被误关联，缺订单号事件同样被丢弃。
func TestSimplifiedPaidEventOutsideWindowIsDropped(t *testing.T) {
	// store、cleanup 提供隔离数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制本测试的数据库写入生命周期。
	ctx := context.Background()
	seedSimplifiedEventFixture(t, store, "shipped")
	// execErr 保存把订单更新时间改到窗口外的写入错误。
	if _, execErr := store.DB.ExecContext(ctx, `UPDATE orders SET updated_at='2000-01-01 00:00:00' WHERE order_id=?`, "simple-order"); execErr != nil {
		t.Fatal(execErr)
	}
	// center 使用真实事实记录和运行协调器。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// handleErr 保存简化事件处理结果，期望为 nil。
	if handleErr := center.HandleTask(ctx, simplifiedPaidTask()); handleErr != nil {
		t.Fatalf("窗口外简化事件丢弃不应返回错误: %v", handleErr)
	}
	// pending、runs 是延期任务数与运行数。
	pending, runs := countPendingAndRuns(t, store)
	if pending != 0 || runs != 0 {
		t.Fatalf("窗口外订单不应被关联: pending=%d runs=%d", pending, runs)
	}
}

// TestSimplifiedOrderCreatedResolvesToExistingOrderKey 验证简化拍下事件只在订单号和商品都缺失时按近期订单补齐，
// 补齐后的幂等键与完整事件相同，因此不会产生第二笔订单或延期任务。
func TestSimplifiedOrderCreatedResolvesToExistingOrderKey(t *testing.T) {
	// store、cleanup 提供隔离数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制本测试的数据库访问生命周期。
	ctx := context.Background()
	seedSimplifiedEventFixture(t, store, "shipped")
	// center 使用真实事实记录和运行协调器。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// simplified 是只含会话标识的拍下事件。
	simplified := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderCreated, ChatID: "simple-chat"}
	// resolved、resolveErr 是按会话补齐后的任务及错误。
	resolved, resolveErr := center.resolvePaidTaskOrder(ctx, simplified)
	if resolveErr != nil || resolved.OrderID != "simple-order" || resolved.ItemID != "simple-item" {
		t.Fatalf("简化拍下事件应补齐已有订单: task=%+v err=%v", resolved, resolveErr)
	}
	// got、want 分别是补齐后任务的实际幂等键和与完整事件一致的期望键。
	if got, want := buildTriggerKey(resolved), "order_created:simple-order"; got != want {
		t.Fatalf("补齐后幂等键=%q，期望 %q", got, want)
	}
	// withItem 已带商品，说明不是只含会话标识的简化事件，不应被改写。
	withItem := simplified
	withItem.ItemID = "other-item"
	// unchanged、unchangedErr 是已有商品标识的拍下事件处理结果，必须原样返回。
	unchanged, unchangedErr := center.resolvePaidTaskOrder(ctx, withItem)
	if unchangedErr != nil || unchanged.OrderID != "" {
		t.Fatalf("已带商品的拍下事件不应按会话改写: task=%+v err=%v", unchanged, unchangedErr)
	}
	// handleErr 保存简化事件处理结果，期望为 nil。
	if handleErr := center.HandleTask(ctx, simplified); handleErr != nil {
		t.Fatalf("简化拍下事件不应返回错误: %v", handleErr)
	}
	// pending 是处理后的延期任务数。
	pending, _ := countPendingAndRuns(t, store)
	if pending != 0 {
		t.Fatalf("简化拍下事件不应进入延期队列: pending=%d", pending)
	}
}

// TestDeferredReplayWithoutOrderIDDoesNotRetry 验证历史遗留的缺订单号延期任务重放时直接收口，不再要求重试，也就不会耗尽次数后误发人工处理通知。
func TestDeferredReplayWithoutOrderIDDoesNotRetry(t *testing.T) {
	// store、cleanup 提供隔离数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 使用真实事实记录和运行协调器。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// task 是历史延期队列中缺订单号的卖家付款事件，带延期重放标记。
	task := chatOnlyPaidTask()
	task.OrderRole = OrderRoleSeller
	task.Raw = map[string]any{"automation_deferred_replay": true}
	// deferredAgain、replayErr 分别表示是否再次延期以及重放错误，期望均为否。
	deferredAgain, replayErr := center.handleTask(context.Background(), task)
	if deferredAgain || replayErr != nil {
		t.Fatalf("缺订单号的延期重放应直接收口: deferredAgain=%v err=%v", deferredAgain, replayErr)
	}
}
