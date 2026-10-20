package automation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// renameOrdersFetcher 在查询订单详情期间把订单表藏起来，用于制造"详情已取回但事实写入失败"的故障。
type renameOrdersFetcher struct {
	// store 是需要被改写表结构的测试数据库。
	store *db.Store
	// detail 是返回给调用方的订单详情；为空表示平台没有详情。
	detail *OrderDetail
}

// FetchOrderDetail 先隐藏订单表再返回预置详情，模拟详情查询之后的本地写入故障。
func (f renameOrdersFetcher) FetchOrderDetail(ctx context.Context, _, _, _, _, _ string) (*OrderDetail, error) {
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if _, err := f.store.DB.ExecContext(ctx, `ALTER TABLE orders RENAME TO orders_gap_hidden`); err != nil {
		return nil, err
	}
	return f.detail, nil
}

// TestHandleTaskGapBranches 覆盖 handleTask 在输入保护、事实写入、账号状态与规则查询阶段的失败分支。
func TestHandleTaskGapBranches(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// reviewed 是不经过 WebSocket 角色门禁的评价任务，订单号用于事实写入。
	reviewed := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "gap-order", ChatID: "chat", BuyerID: "buyer"}

	t.Run("空中心与空字段直接忽略", func(t *testing.T) {
		// deferred 表示任务是否被持久化延期等待后续重放；err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if deferred, err := (*Center)(nil).handleTask(ctx, reviewed); deferred || err != nil {
			t.Fatalf("deferred=%v err=%v", deferred, err)
		}
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 使用真实存储。
		center := New(store, nil, nil)
		// deferred 表示任务是否被持久化延期等待后续重放；err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if deferred, err := center.handleTask(ctx, Task{AccountID: "cid"}); deferred || err != nil {
			t.Fatalf("缺触发类型: deferred=%v err=%v", deferred, err)
		}
		// deferred 表示任务是否被持久化延期等待后续重放；err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if deferred, err := center.handleTask(ctx, Task{TriggerType: TriggerBuyerReviewed}); deferred || err != nil {
			t.Fatalf("缺账号: deferred=%v err=%v", deferred, err)
		}
	})
	t.Run("订单事实回填失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `ALTER TABLE orders RENAME TO orders_gap_hidden`)
		// paid 是携带订单号的付款任务。
		paid := Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, OrderID: "gap-paid"}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, paid); err == nil {
			t.Fatal("订单回填失败应返回错误")
		}
	})
	t.Run("未知角色核验读取失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `ALTER TABLE item_info RENAME TO item_info_gap_hidden`)
		// created 是缺少角色的 WebSocket 拍下任务。
		created := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderCreated, ItemID: "gap-item"}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, created); err == nil {
			t.Fatal("核验失败应返回错误")
		}
	})
	t.Run("未知角色重放仍缺证据交给退避", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// replayed 是延期重放的未知角色付款任务，本地没有订单。
		replayed := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "gap-none", Raw: map[string]any{"automation_deferred_replay": true}}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, replayed); !errors.Is(err, errSellerRoleEvidencePending) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("未知角色首次事件保存失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `ALTER TABLE automation_pending_tasks RENAME TO automation_pending_gap_hidden`)
		// first 是首次到达的未知角色付款任务。
		first := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "gap-none"}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, first); err == nil {
			t.Fatal("保存延期任务失败应返回错误")
		}
	})
	t.Run("事实写入通用失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `ALTER TABLE orders RENAME TO orders_gap_hidden`)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, reviewed); err == nil {
			t.Fatal("事实写入失败应返回错误")
		}
	})
	t.Run("订单归属其他账号时忽略副本", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// admin 保存新账号归属用户。
		admin, _ := store.Users.GetByUsername(ctx, "admin")
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := store.Cookies.Save(ctx, "other", "unb=9; _m_h5_tk=tk;", admin.ID); err != nil {
			t.Fatal(err)
		}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := store.Orders.Upsert(ctx, "gap-order", db.OrderUpsertOpts{CookieID: "other", ItemID: "i", BuyerID: "b", ChatID: "c"}); err != nil {
			t.Fatal(err)
		}
		// deferred 表示任务是否被持久化延期等待后续重放；err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if deferred, err := New(store, nil, nil).handleTask(ctx, reviewed); deferred || err != nil {
			t.Fatalf("deferred=%v err=%v", deferred, err)
		}
	})
	t.Run("账号暂停状态读取失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `ALTER TABLE cookies RENAME TO cookies_gap_hidden`)
		// noOrder 不携带订单号，避免订单写入先失败。
		noOrder := reviewed
		noOrder.OrderID = ""
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, noOrder); err == nil {
			t.Fatal("暂停状态读取失败应返回错误")
		}
	})
	t.Run("账号启用状态读取失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `DROP TABLE cookie_status`)
		// noOrder 不携带订单号，避免订单写入先失败。
		noOrder := reviewed
		noOrder.OrderID = ""
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, noOrder); err == nil {
			t.Fatal("启用状态读取失败应返回错误")
		}
	})
	t.Run("暂停期间缺少防重键", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := store.Cookies.SetPause(ctx, "cid", 60); err != nil {
			t.Fatal(err)
		}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed}); err == nil {
			t.Fatal("缺少防重键应返回错误")
		}
		// unmarshalable 的原始报文无法序列化为延期任务。
		bad := reviewed
		bad.Raw = map[string]any{"bad": make(chan int)}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, bad); err == nil {
			t.Fatal("无法序列化应返回错误")
		}
	})
	t.Run("订单完成事件只记录事实", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// done 是买家确认收货事件。
		done := Task{AccountID: "cid", TriggerType: TriggerOrderCompleted, OrderID: "gap-done", ItemID: "i", BuyerID: "b", ChatID: "c"}
		// deferred 表示任务是否被持久化延期等待后续重放；err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if deferred, err := New(store, nil, nil).handleTask(ctx, done); !deferred || err != nil {
			t.Fatalf("deferred=%v err=%v", deferred, err)
		}
	})
	t.Run("规则查询失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// admin 保存规则归属用户。
		admin, _ := store.Users.GetByUsername(ctx, "admin")
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "r", TriggerType: TriggerBuyerReviewed, Enabled: true,
			Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "x", Enabled: true}}}); err != nil {
			t.Fatal(err)
		}
		execGapSQL(t, store, `ALTER TABLE automation_rule_actions RENAME TO automation_rule_actions_gap_hidden`)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).handleTask(ctx, reviewed); err == nil {
			t.Fatal("规则查询失败应返回错误")
		}
	})
	t.Run("前置失败且持久化失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// admin 保存规则归属用户。
		admin, _ := store.Users.GetByUsername(ctx, "admin")
		execGapSQL(t, store, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','gap-item','商品',1)`)
		execGapSQL(t, store, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (91,'库存','text','C',1,`+itoa(admin.ID)+`)`)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", ItemID: "gap-item", Name: "r", TriggerType: TriggerOrderPaid, Enabled: true,
			Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: 91, DeliveryCount: 1, ConfigJSON: `{}`, Enabled: true}}}); err != nil {
			t.Fatal(err)
		}
		execGapSQL(t, store, `CREATE TRIGGER gap_abort_defer BEFORE INSERT ON automation_pending_tasks BEGIN SELECT RAISE(ABORT, 'forced defer failure'); END`)
		// center 的订单详情查询必然失败，触发前置失败延期。
		center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{OrderDetailFetcher: testFetcher{err: errors.New("临时失败")}})
		// paid 是完整的卖家付款任务。
		paid := Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, OrderID: "gap-pending", ItemID: "gap-item", ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"message_id": "m1"}}
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleTask(ctx, paid); err == nil || !strings.Contains(err.Error(), "持久化前置失败任务") {
			t.Fatalf("err=%v", err)
		}
	})
}

// TestPrepareTaskGapBranches 覆盖订单详情补全阶段的昵称读取、凭证读取、空详情与事实写入失败。
func TestPrepareTaskGapBranches(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// paid 是需要补全详情的付款任务。
	paid := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "prep-order", ItemID: "i", BuyerID: "b", ChatID: "chat"}
	t.Run("昵称读取失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		execGapSQL(t, store, `ALTER TABLE chat_sessions RENAME TO chat_sessions_gap_hidden`)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := New(store, nil, nil).prepareTask(ctx, paid); err == nil {
			t.Fatal("昵称读取失败应报错")
		}
	})
	t.Run("账号凭证读取失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 的 Cookie 来源返回错误。
		center := NewWithDependencies(store, nil, nil, CenterDependencies{
			OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1"}},
			CookieSource:       func(context.Context, string) (string, error) { return "", errors.New("凭证不可读") },
		})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.prepareTask(ctx, paid); err == nil || !strings.Contains(err.Error(), "凭证不可读") {
			t.Fatalf("err=%v", err)
		}
		// cookie、cookieErr 同时覆盖构造期固定的 Cookie 来源闭包。
		if cookie, cookieErr := center.cookieValue(ctx, "cid"); cookie != "" || cookieErr == nil {
			t.Fatalf("cookie=%q err=%v", cookie, cookieErr)
		}
	})
	t.Run("平台没有返回详情", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 的详情查询返回空详情。
		center := NewWithDependencies(store, nil, nil, CenterDependencies{OrderDetailFetcher: testFetcher{}})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发；got 保存被测函数的实际返回值，供断言比较。
		if got, err := center.prepareTask(ctx, paid); err == nil || !strings.Contains(err.Error(), "购买数量") {
			t.Fatalf("没有可靠数量时应拒绝自动发货: got=%+v err=%v", got, err)
		}
		// withQuantity 已带可靠数量的付款任务，详情为空时仍可继续。
		withQuantity := paid
		withQuantity.Quantity = "2"
		// got、err 保存已有可靠数量任务的准备结果。
		if got, err := center.prepareTask(ctx, withQuantity); err != nil || got.OrderID != "prep-order" || got.Quantity != "2" {
			t.Fatalf("已有可靠数量应继续: got=%+v err=%v", got, err)
		}
	})
	t.Run("详情缺少数量不默认一件", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 的详情查询成功但没有返回购买数量。
		center := NewWithDependencies(store, nil, nil, CenterDependencies{OrderDetailFetcher: testFetcher{detail: &OrderDetail{Amount: "9.9"}}})
		// err 保存缺少数量时的准备阶段错误。
		if _, err := center.prepareTask(ctx, paid); err == nil || !strings.Contains(err.Error(), "购买数量") {
			t.Fatalf("缺少数量应拒绝自动发货: %v", err)
		}
		// invalid 的数量文本无法识别，同样不得退化为一件。
		invalid := paid
		invalid.Quantity = "abc"
		// err 保存无法识别数量时的准备阶段错误。
		if _, err := center.prepareTask(ctx, invalid); err == nil {
			t.Fatal("无法识别的数量应拒绝自动发货")
		}
	})
	t.Run("详情写回失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 的详情查询后会让订单表不可用。
		center := NewWithDependencies(store, nil, nil, CenterDependencies{OrderDetailFetcher: renameOrdersFetcher{store: store, detail: &OrderDetail{Quantity: "2", Amount: "9", SpecName: "a", SpecValue: "b", OrderStatus: "pending_ship"}}})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.prepareTask(ctx, paid); err == nil || !strings.Contains(err.Error(), "保存订单详情事实") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("API卡取回器闭包", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 使用默认依赖，未注入 API 卡取回器。
		center := New(store, nil, nil)
		if center.actions.apiFetcher() != nil {
			t.Fatal("未注入时应为空")
		}
	})
}

// TestRunAccountTaskGapBranches 覆盖手动账号任务入口的未初始化与无日志分支。
func TestRunAccountTaskGapBranches(t *testing.T) {
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if _, err := (*Center)(nil).RunAccountTask(context.Background(), "cid", "auto_rate"); err == nil {
		t.Fatal("空中心应报错")
	}
	// store、cleanup 保存隔离数据库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 清空日志器后覆盖无日志直接返回分支。
	center := New(store, nil, nil)
	center.logger = nil
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if _, err := center.RunAccountTask(context.Background(), "cid", "unknown_task"); err == nil {
		t.Fatal("未知任务类型应报错")
	}
}
