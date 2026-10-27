package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestUnknownWebSocketPaidEventWithoutLocalSellerOrderIsDeferredBeforeFacts 验证未知角色付款事件在本地订单尚未同步时会延期核验，不写错订单或创建运行。
func TestUnknownWebSocketPaidEventWithoutLocalSellerOrderIsDeferredBeforeFacts(t *testing.T) {
	// store、cleanup 提供隔离数据库及测试结束后的连接释放责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制当前测试的订单事实和自动化处理生命周期。
	ctx := context.Background()
	// sender 记录任何不应发生的买家消息发送副作用。
	sender := &testSender{}
	// center 使用真实事实记录与运行协调器，确保门禁位于所有持久化副作用之前。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 模拟平台没有提供 role 的付款系统卡片；没有本地订单时不能凭文案猜测卖家身份。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		OrderID: "buyer-side-order", ItemID: "other-seller-item", BuyerID: "buyer", ChatID: "buyer-chat"}
	// handleErr 保存未知角色事件被安全忽略时的处理错误。
	if handleErr := center.HandleTask(ctx, task); handleErr != nil {
		t.Fatalf("未知角色无本地卖家订单不应返回错误: %v", handleErr)
	}
	// order、orderErr 验证买家侧事件在事实写入前被拦截。
	order, orderErr := store.Orders.Get(ctx, task.OrderID)
	if !errors.Is(orderErr, db.ErrNotFound) || order != nil {
		t.Fatalf("未知角色买家订单不应落入当前账号: order=%+v err=%v", order, orderErr)
	}
	// runCount、pendingCount 验证未知角色事件既没有创建运行，也没有写入延期队列。
	var runCount, pendingCount int
	// queryErr 保存运行表计数查询错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs`).Scan(&runCount); queryErr != nil {
		t.Fatal(queryErr)
	}
	// queryErr 保存延期任务表计数查询错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks`).Scan(&pendingCount); queryErr != nil {
		t.Fatal(queryErr)
	}
	if runCount != 0 || pendingCount != 1 || len(sender.texts) != 0 {
		t.Fatalf("未知角色事件产生了自动化副作用: runs=%d pending=%d messages=%v", runCount, pendingCount, sender.texts)
	}
}

// TestUnknownWebSocketPaidEventDeferredThenResumesAfterLocalFacts 验证首条未知角色事件不会漏发：订单和商品同步后，延期任务可安全恢复并只执行一次。
func TestUnknownWebSocketPaidEventDeferredThenResumesAfterLocalFacts(t *testing.T) {
	// store、cleanup 提供隔离数据库及测试结束后的连接释放责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// 本用例只验证发货与恢复行为，关闭账号自动确认发货，避免规则缺少确认动作时的隐式确认步骤访问平台；隐式确认由专门用例覆盖。
	disableAutoConsign(t, store)
	// ctx 限制延期任务、订单同步事实和恢复执行的生命周期。
	ctx := context.Background()
	// admin、adminErr 提供卡密组和付款规则的管理用户归属。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试管理员失败: %v", adminErr)
	}
	// cardID、cardErr 保存恢复时唯一允许发送的测试卡密。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "延迟核验卡", Type: "text", TextContent: "DEFERRED-CARD", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleErr 保存本账号商品付款规则。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", ItemID: "deferred-item", Name: "延期核验规则", TriggerType: TriggerOrderPaid, Enabled: true, Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true}}}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 记录恢复任务实际发出的卡密内容。
	sender := &testSender{}
	// center 先接收没有本地事实的未知角色事件。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 模拟订单同步尚未完成时抵达的旧协议付款事件。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "deferred-order", ItemID: "deferred-item", BuyerID: "deferred-buyer", ChatID: "deferred-chat"}
	// handleErr 保存首条未知角色事件写入延期队列的处理错误。
	if handleErr := center.HandleTask(ctx, task); handleErr != nil {
		t.Fatalf("首条未知角色事件延期失败: %v", handleErr)
	}
	// orderErr 保存 WebSocket 之后补齐的本地订单事实。
	if orderErr := store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{CookieID: "cid", ItemID: task.ItemID, BuyerID: task.BuyerID, ChatID: task.ChatID, OrderStatus: "paid", Quantity: "1", Amount: "1.00"}); orderErr != nil {
		t.Fatal(orderErr)
	}
	// itemErr 保存同步完成的本地商品卖家证据。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: task.ItemID, ItemTitle: "延期商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// dueErr 将延期任务置为立即可领取，模拟同步完成后的下一次调度。
	if _, dueErr := store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`); dueErr != nil {
		t.Fatal(dueErr)
	}
	// scheduler 通过统一延期恢复链路重新执行门禁和规则，不直接调用外部动作。
	scheduler := NewScheduler(center)
	// runErr 保存同步事实到齐后重放延期任务的处理错误。
	if runErr := scheduler.runDeferredTasks(ctx); runErr != nil {
		t.Fatalf("延期未知角色事件恢复失败: %v", runErr)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "DEFERRED-CARD" {
		t.Fatalf("延期未知角色事件未在事实到齐后发货: %v", sender.texts)
	}
	// remaining、remainingErr 验证成功恢复后延期任务被消费，避免重复发卡。
	var remaining int
	// remainingErr 保存恢复后延期任务计数查询错误。
	if remainingErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks`).Scan(&remaining); remainingErr != nil {
		t.Fatal(remainingErr)
	}
	if remaining != 0 {
		t.Fatalf("成功恢复后仍残留延期任务: %d", remaining)
	}
}

// TestUnknownWebSocketOrderCreatedUsesLocalItemOwnership 验证未知角色的拍下事件只有在本账号历史商品存在时才记录卖家订单。
func TestUnknownWebSocketOrderCreatedUsesLocalItemOwnership(t *testing.T) {
	// t 负责隔离本测试数据库、事件事实和延期队列。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制本测试内的商品归属与事件记录生命周期。
	ctx := context.Background()
	// itemErr 保存本账号卖家商品事实写入错误。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "created-item", ItemTitle: "拍下商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// center 使用真实事实记录器验证未知角色门禁先于订单落库执行。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// task 模拟缺少 role 但商品属于当前账号的卖家拍下事件。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "created-order", ItemID: "created-item", BuyerID: "created-buyer", ChatID: "created-chat"}
	// handleErr 保存本地商品归属通过后的订单记录结果。
	if handleErr := center.HandleTask(ctx, task); handleErr != nil {
		t.Fatalf("本账号商品拍下事件应记录: %v", handleErr)
	}
	// order、orderErr 验证未知角色通过本地商品证据后才写入当前账号订单。
	order, orderErr := store.Orders.Get(ctx, task.OrderID)
	if orderErr != nil || order == nil || order.CookieID != "cid" {
		t.Fatalf("卖家拍下订单事实未记录: order=%+v err=%v", order, orderErr)
	}
	// buyerTask 模拟买家购买其他卖家商品且当前账号没有该商品历史记录的事件。
	buyerTask := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "foreign-created-order", ItemID: "foreign-item", BuyerID: "seller-user", ChatID: "foreign-chat"}
	// buyerErr 保存买家侧事件进入延期等待归属事实的结果。
	if buyerErr := center.HandleTask(ctx, buyerTask); buyerErr != nil {
		t.Fatalf("买家侧未知角色拍下事件不应返回错误: %v", buyerErr)
	}
	// foreignOrder、foreignErr 验证买家侧拍下事件不会先写入卖家订单表。
	foreignOrder, foreignErr := store.Orders.Get(ctx, buyerTask.OrderID)
	if !errors.Is(foreignErr, db.ErrNotFound) || foreignOrder != nil {
		t.Fatalf("买家侧拍下事件错误写入订单: order=%+v err=%v", foreignOrder, foreignErr)
	}
}

// TestUnknownWebSocketPaidEventWithLocalSellerFactsContinuesAutomation 验证旧协议缺少 role 时，本账号待发货订单和本地商品事实仍能恢复正常发货。
func TestUnknownWebSocketPaidEventWithLocalSellerFactsContinuesAutomation(t *testing.T) {
	// store、cleanup 提供隔离数据库及测试结束后的连接释放责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// 本用例只验证发货与恢复行为，关闭账号自动确认发货，避免规则缺少确认动作时的隐式确认步骤访问平台；隐式确认由专门用例覆盖。
	disableAutoConsign(t, store)
	// ctx 限制当前测试的商品、订单和自动化动作生命周期。
	ctx := context.Background()
	// admin、adminErr 提供卡密组和付款规则的管理用户归属。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试管理员失败: %v", adminErr)
	}
	// itemErr 保存本账号商品归属事实的写入结果；未知角色核验只依赖商品是否存在，不依赖详情内容。
	itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "seller-item", ItemTitle: "卖家商品"})
	if itemErr != nil {
		t.Fatal(itemErr)
	}
	// orderErr 保存完整待发货订单事实，供角色核验和规则规格准备共同使用。
	orderErr := store.Orders.Upsert(ctx, "seller-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "seller-item", BuyerID: "real-buyer", ChatID: "seller-chat",
		OrderStatus: "pending_ship", Quantity: "1", Amount: "9.90",
	})
	if orderErr != nil {
		t.Fatal(orderErr)
	}
	// cardID、cardErr 保存本地一次性卡密组及创建错误。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "未知角色回归卡", Type: "text", TextContent: "SAFE-CARD", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleErr 保存商品级付款发货规则，动作只有在未知角色通过本地卖家核验后才可领取卡密。
	_, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "seller-item", Name: "未知角色回归规则",
		TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true}},
	})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 记录未知角色旧协议恢复后的唯一发货内容。
	sender := &testSender{}
	// center 使用真实商品归属门禁和自动化运行协调器。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 模拟缺少 role 但订单、商品、买家、会话均与本账号待发货事实一致的付款事件。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		OrderID: "seller-order", ItemID: "seller-item", BuyerID: "real-buyer", ChatID: "seller-chat"}
	// handleErr 保存通过本地事实核验后执行自动发货的结果。
	if handleErr := center.HandleTask(ctx, task); handleErr != nil {
		t.Fatalf("未知角色但有完整卖家事实时应继续发货: %v", handleErr)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "SAFE-CARD" {
		t.Fatalf("未知角色卖家订单未按规则发货: %v", sender.texts)
	}
}

// TestUnknownWebSocketPaidEventWithMismatchedLocalOrderIsRejected 验证订单、商品、买家或会话任一事实不一致时，未知角色事件不会借用本账号订单发货。
func TestUnknownWebSocketPaidEventWithMismatchedLocalOrderIsRejected(t *testing.T) {
	// mismatchField、task 保存当前待验证的不一致字段及其事件副本。
	for _, mismatchField := range []string{"item", "buyer", "chat"} {
		t.Run(mismatchField, func(t *testing.T) {
			// store、cleanup 提供本轮隔离数据库及资源释放责任。
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 限制当前子测试的同步数据库操作生命周期。
			ctx := context.Background()
			// itemErr 保存卖家商品事实写入结果。
			itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "owned-item", ItemTitle: "本账号商品"})
			if itemErr != nil {
				t.Fatal(itemErr)
			}
			// orderErr 保存与本账号商品、买家和会话完全一致的待发货订单。
			orderErr := store.Orders.Upsert(ctx, "owned-order", db.OrderUpsertOpts{
				CookieID: "cid", ItemID: "owned-item", BuyerID: "owned-buyer", ChatID: "owned-chat",
				OrderStatus: "pending_ship", Quantity: "1", Amount: "1.00",
			})
			if orderErr != nil {
				t.Fatal(orderErr)
			}
			// task 是只修改一个身份事实的未知角色付款事件。
			task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
				OrderID: "owned-order", ItemID: "owned-item", BuyerID: "owned-buyer", ChatID: "owned-chat"}
			switch mismatchField {
			case "item":
				task.ItemID = "other-item"
			case "buyer":
				task.BuyerID = "other-buyer"
			case "chat":
				task.ChatID = "other-chat"
			}
			// center 不配置规则也不应影响身份门禁先于事实写入执行的断言。
			center := New(store, testSenderProvider{sender: &testSender{}}, nil)
			// handleErr 保存身份不一致事件的安全忽略结果。
			if handleErr := center.HandleTask(ctx, task); handleErr != nil {
				t.Fatalf("身份不一致事件不应返回错误: %v", handleErr)
			}
			// runCount 验证身份不一致事件没有创建任何自动化运行。
			var runCount int
			// queryErr 保存身份不一致事件运行计数查询错误。
			if queryErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs`).Scan(&runCount); queryErr != nil {
				t.Fatal(queryErr)
			}
			if runCount != 0 {
				t.Fatalf("身份不一致事件不应创建运行: %d", runCount)
			}
		})
	}
}

// TestUnknownWebSocketRecoveryRequiresSellerEvidence 验证历史未知角色付款运行在恢复前仍需重新核对本地商品归属。
func TestUnknownWebSocketRecoveryRequiresSellerEvidence(t *testing.T) {
	// store、cleanup 提供恢复路径所需的隔离数据库及资源释放责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// 本用例只验证发货与恢复行为，关闭账号自动确认发货，避免规则缺少确认动作时的隐式确认步骤访问平台；隐式确认由专门用例覆盖。
	disableAutoConsign(t, store)
	// ctx 限制规则、订单和恢复扫描的同步生命周期。
	ctx := context.Background()
	// admin、adminErr 提供恢复规则的管理用户归属。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试管理员失败: %v", adminErr)
	}
	// itemErr 保存本地商品归属事实，证明未知角色历史运行可以安全恢复。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "recovery-item", ItemTitle: "恢复商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// orderErr 保存待发货订单事实，字段必须与历史任务快照一致。
	if orderErr := store.Orders.Upsert(ctx, "recovery-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "recovery-item", BuyerID: "recovery-buyer", ChatID: "recovery-chat",
		OrderStatus: "pending_ship", Quantity: "1", Amount: "2.00",
	}); orderErr != nil {
		t.Fatal(orderErr)
	}
	// cardID、cardErr 保存恢复运行将要发送的卡密组及创建错误。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "恢复角色卡", Type: "text", TextContent: "RECOVERY-CARD", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleID、ruleErr 保存恢复运行对应的付款发货规则。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "recovery-item", Name: "恢复角色规则",
		TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true}},
	})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// task 是持久化时仍没有 role 的历史 WebSocket 任务快照。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "recovery-order",
		ItemID: "recovery-item", BuyerID: "recovery-buyer", ChatID: "recovery-chat", Quantity: "1", OrderStatus: "pending_ship"}
	// rawJSON、marshalErr 保存无敏感凭证的历史任务快照及序列化错误。
	rawJSON, marshalErr := json.Marshal(task)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	// runID、started、startErr 保存待恢复运行的创建结果。
	runID, started, startErr := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "cid", ItemID: task.ItemID, OrderID: task.OrderID, BuyerID: task.BuyerID,
		ChatID: task.ChatID, TriggerType: TriggerOrderPaid, TriggerKey: "unknown-role-recovery",
		RawEventJSON: string(rawJSON), LeaseExpiresAt: time.Now().UTC().Add(time.Minute).Unix(),
	})
	if startErr != nil || !started {
		t.Fatalf("创建未知角色恢复运行失败: started=%v err=%v", started, startErr)
	}
	// expireErr 将运行置为可立即恢复，避免测试等待生产调度周期。
	if _, expireErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0,next_retry_at=0 WHERE id=?`, runID); expireErr != nil {
		t.Fatal(expireErr)
	}
	// sender 记录历史未知角色运行通过重新核验后的发货结果。
	sender := &testSender{}
	// scheduler 使用真实恢复链路，验证角色门禁位于所有外部发货动作之前。
	scheduler := NewScheduler(New(store, testSenderProvider{sender: sender}, nil))
	// recoveryErr 保存历史未知角色运行恢复的处理错误。
	if recoveryErr := scheduler.runRecoveryTasks(ctx); recoveryErr != nil {
		t.Fatalf("未知角色历史运行恢复失败: %v", recoveryErr)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "RECOVERY-CARD" {
		t.Fatalf("未知角色历史卖家运行未恢复发货: %v", sender.texts)
	}
}

// TestUnknownWebSocketOrderCreatedForNonLocalItemIsSkipped 验证无角色拍下事件的商品不在本账号商品库时静默跳过：
// 不写订单事实、不进入延期队列、不创建运行；同一用户作为买家拍下他人商品即属此类。
func TestUnknownWebSocketOrderCreatedForNonLocalItemIsSkipped(t *testing.T) {
	// store、cleanup 提供隔离数据库及连接释放责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制当前测试的自动化处理生命周期。
	ctx := context.Background()
	// task 模拟账号作为买家拍下他人商品；该商品不在本账号商品库。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "buyer-created", ItemID: "someone-else-item", BuyerID: "self", ChatID: "buyer-chat"}
	// handleErr 保存拍下事件被静默跳过时的处理错误，必须为空。
	if handleErr := New(store, nil, nil).HandleTask(ctx, task); handleErr != nil {
		t.Fatalf("商品不在本地的拍下事件应被静默跳过: %v", handleErr)
	}
	// order、orderErr 验证订单没有被记入本账号。
	if order, orderErr := store.Orders.Get(ctx, task.OrderID); !errors.Is(orderErr, db.ErrNotFound) || order != nil {
		t.Fatalf("订单不应落入当前账号: order=%+v err=%v", order, orderErr)
	}
	// runCount、pendingCount 验证没有运行记录和延期任务。
	var runCount, pendingCount int
	// queryErr 保存运行表与延期任务表计数查询的错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM automation_runs),(SELECT COUNT(*) FROM automation_pending_tasks)`).Scan(&runCount, &pendingCount); queryErr != nil {
		t.Fatal(queryErr)
	}
	if runCount != 0 || pendingCount != 0 {
		t.Fatalf("拍下事件产生副作用: runs=%d pending=%d", runCount, pendingCount)
	}
}
