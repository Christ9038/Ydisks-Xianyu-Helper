package automation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

type testSenderProvider struct{ sender *testSender }

func (p testSenderProvider) Sender(string) (MessageSender, bool) { return p.sender, true }

type testSender struct {
	texts  []string
	events *[]string
}

func (s *testSender) SendText(_ context.Context, _, _, text string) error {
	s.texts = append(s.texts, text)
	if s.events != nil {
		*s.events = append(*s.events, "send:"+text)
	}
	return nil
}
func (s *testSender) SendImage(context.Context, string, string, string, int64) error { return nil }
func (s *testSender) UpdateCookie(string)                                            {}

type testFetcher struct{ detail *OrderDetail }

func (f testFetcher) FetchOrderDetail(context.Context, string, string, string, string, string) (*OrderDetail, error) {
	return f.detail, nil
}

func newAutomationTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(database, db.DialectSQLite)
	if _, err := store.Users.Create(context.Background(), "admin", "admin@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	if err := store.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	return store, func() { _ = database.Close() }
}

func TestCenterOrderPaidFetchesOrderDetailMatchesSpecAndQuantity(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()

	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-1','会员',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,data_content,enabled,user_id) VALUES
		(11,'30天库存','data','A1'||char(10)||'A2'||char(10)||'A3'||char(10)||'A4',1,?),
		(12,'90天库存','data','B1'||char(10)||'B2'||char(10)||'B3'||char(10)||'B4',1,?)`, admin.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 11, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"30天"}`, Enabled: true, SortOrder: 1},
			{ActionType: ActionSendCard, CardID: 12, DeliveryCount: 2, ConfigJSON: `{"spec_name":"套餐","spec_value":"90天"}`, Enabled: true, SortOrder: 2},
		},
	})
	if err != nil || ruleID == 0 {
		t.Fatalf("create automation rule: id=%d err=%v", ruleID, err)
	}

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		SpecName: "套餐", SpecValue: "90天", Quantity: "2", Amount: "19.8", OrderStatus: "pending_ship",
	}})

	err = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m1"},
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	if got, want := len(sender.texts), 4; got != want {
		t.Fatalf("发送条数=%d want %d texts=%v", got, want, sender.texts)
	}
	for i, want := range []string{"B1", "B2", "B3", "B4"} {
		if sender.texts[i] != want {
			t.Fatalf("texts[%d]=%q want %q", i, sender.texts[i], want)
		}
	}
	order, err := store.Orders.Get(ctx, "order-1")
	if err != nil {
		t.Fatal(err)
	}
	if order.SpecName != "套餐" || order.SpecValue != "90天" || order.Quantity != "2" {
		t.Fatalf("订单详情未写入: %+v", order)
	}
}

func TestCenterOrderPaidSendsAllCardActionsForSameSpec(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-bundle','组合商品',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES
		(41,'主卡库存','text','MAIN-CARD',1,?),
		(42,'附赠卡库存','text','GIFT-CARD',1,?)`, admin.ID, admin.ID)
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-bundle", Name: "组合商品自动发货", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 41, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"组合版"}`, Enabled: true, SortOrder: 1},
			{ActionType: ActionSendCard, CardID: 42, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"组合版"}`, Enabled: true, SortOrder: 2},
		},
	})
	if err != nil {
		t.Fatalf("create automation rule: %v", err)
	}

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		SpecName: "套餐", SpecValue: "组合版", Quantity: "1", Amount: "29.9",
	}})

	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-bundle", OrderID: "order-bundle", ItemID: "item-bundle", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m-bundle"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	want := []string{"MAIN-CARD", "GIFT-CARD"}
	if len(sender.texts) != len(want) {
		t.Fatalf("发送内容=%v want %v", sender.texts, want)
	}
	for i := range want {
		if sender.texts[i] != want[i] {
			t.Fatalf("发送内容=%v want %v", sender.texts, want)
		}
	}
}

func TestCenterOrderPaidDoesNotConfirmWhenNoCardSpecMatches(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-1','会员',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (21,'30天库存','text','A',1,?)`, admin.ID)
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 1},
			{ActionType: ActionSendCard, CardID: 21, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"30天"}`, Enabled: true, SortOrder: 2},
		},
	})
	if err != nil {
		t.Fatalf("create automation rule: %v", err)
	}

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{SpecName: "套餐", SpecValue: "90天", Quantity: "1"}})

	err = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-no-match", ItemID: "item-1", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m2"},
	})
	if err != nil {
		t.Fatalf("HandleTask should swallow rule execution errors: %v", err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("规格不匹配时不应发送卡密: %v", sender.texts)
	}
	order, err := store.Orders.Get(ctx, "order-no-match")
	if err != nil {
		t.Fatal(err)
	}
	if order.SystemShipped {
		t.Fatal("规格不匹配时不应确认发货")
	}
}

func TestCenterOrderPaidSendsCardBeforeConfirmShipment(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','item-1','会员')`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (31,'默认库存','text','CARD-1',1,?)`, admin.ID)
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			// 历史数据或前端排序可能把确认发货放在前面；自动化中心必须强制先发卡。
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 1},
			{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1, Enabled: true, SortOrder: 2},
		},
	})
	if err != nil {
		t.Fatalf("create automation rule: %v", err)
	}

	events := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events = append(events, "confirm")
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	sender := &testSender{events: &events}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.mtop = &mtop.Client{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}})

	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-seq", ItemID: "item-1", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m3"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	want := []string{"send:CARD-1", "confirm"}
	if len(events) != len(want) {
		t.Fatalf("events=%v want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events=%v want %v", events, want)
		}
	}
}

// recordingNotifier 记录所有 NotifyDelivery 调用，用于断言 automation.Center 接线。
type recordingNotifier struct {
	mu   sync.Mutex
	calls []struct {
		accountID, buyerID, itemID, message, chatID string
	}
}

func (r *recordingNotifier) NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, struct {
		accountID, buyerID, itemID, message, chatID string
	}{accountID, buyerID, itemID, message, chatID})
}

func (r *recordingNotifier) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = c.message
	}
	return out
}

// TestCenterNotifiesOnDeliverySuccess 验证规则执行成功（实际发出卡券）时触发成功通知。
func TestCenterNotifiesOnDeliverySuccess(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-n','N',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (61,'卡','text','CARD',1,?)`, admin.ID)
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-n", Name: "通知测试", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 61, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"标准"}`, Enabled: true, SortOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	sender := &testSender{}
	notifier := &recordingNotifier{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		SpecName: "套餐", SpecValue: "标准", Quantity: "1", Amount: "9.9", OrderStatus: "pending_ship",
	}})
	center.SetNotifier(notifier)

	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-n", OrderID: "order-n", ItemID: "item-n", BuyerID: "buyer-n", Raw: map[string]any{"mid": "m"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	msgs := notifier.messages()
	if len(msgs) != 1 {
		t.Fatalf("应发 1 条成功通知，got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "成功") || !strings.Contains(msgs[0], "order-n") {
		t.Fatalf("通知文案异常: %q", msgs[0])
	}
}

// TestCenterNotifiesOnDeliveryFailure 验证无匹配规格动作导致失败时触发失败通知。
func TestCenterNotifiesOnDeliveryFailure(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-f','F',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (71,'卡','text','CARD',1,?)`, admin.ID)
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-f", Name: "失败通知测试", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		// 动作规格是"30天"，但订单规格是"90天"，不会匹配 → 失败。
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 71, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"30天"}`, Enabled: true, SortOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	sender := &testSender{}
	notifier := &recordingNotifier{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		SpecName: "套餐", SpecValue: "90天", Quantity: "1", Amount: "9.9", OrderStatus: "pending_ship",
	}})
	center.SetNotifier(notifier)

	// HandleTask 对单条规则失败只记录日志不返回错误，但通知应已发出。
	_ = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-f", OrderID: "order-f", ItemID: "item-f", BuyerID: "buyer-f", Raw: map[string]any{"mid": "m"},
	})

	msgs := notifier.messages()
	if len(msgs) != 1 {
		t.Fatalf("应发 1 条失败通知，got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "失败") || !strings.Contains(msgs[0], "order-f") {
		t.Fatalf("失败通知文案异常: %q", msgs[0])
	}
}

// TestCenterNoNotifyWhenNoMatchingRule 验证无匹配规则时不发通知（空跑不刷屏）。
func TestCenterNoNotifyWhenNoMatchingRule(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()

	notifier := &recordingNotifier{}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetNotifier(notifier)

	_ = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "c", OrderID: "o", ItemID: "none", BuyerID: "b", Raw: map[string]any{"mid": "m"},
	})

	if len(notifier.messages()) != 0 {
		t.Fatalf("无匹配规则不应发通知，got %v", notifier.messages())
	}
}

// TestDueReviewRequestOrdersHandlesNullSpecColumns 验证订单 spec_name/spec_value 为 NULL
// 时（旧库升级数据常见）DueReviewRequestOrders 不报扫描错误。
func TestDueReviewRequestOrdersHandlesNullSpecColumns(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// 插入一条已发货、未评价、spec_name/spec_value 为 NULL 的订单（旧库常见形态）。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id, cookie_id, chat_id, system_shipped, spec_name, spec_value, quantity, amount)
		VALUES ('o-null', 'cid', 'chat-1', 1, NULL, NULL, NULL, NULL)`); err != nil {
		t.Fatal(err)
	}

	orders, err := store.Automation.DueReviewRequestOrders(ctx, 200)
	if err != nil {
		t.Fatalf("DueReviewRequestOrders 扫描 NULL 列失败: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != "o-null" {
		t.Fatalf("应返回 1 条订单，got %+v", orders)
	}
	if orders[0].SpecName != "" || orders[0].SpecValue != "" {
		t.Fatalf("NULL 列应归一为空串，got spec=%q/%q", orders[0].SpecName, orders[0].SpecValue)
	}
}
