package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

type automationRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f automationRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type testSenderProvider struct{ sender *testSender }

func (p testSenderProvider) Sender(string) (MessageSender, bool) { return p.sender, true }

type testSender struct {
	texts          []string
	cookieUpdates  []string
	onCookieUpdate func(string)
	events         *[]string
	err            error
	failAfter      int
}

func (s *testSender) SendText(_ context.Context, _, _, text string) error {
	if s.err != nil && (s.failAfter == 0 || len(s.texts) >= s.failAfter) {
		return s.err
	}
	s.texts = append(s.texts, text)
	if s.events != nil {
		*s.events = append(*s.events, "send:"+text)
	}
	return nil
}

func TestPartialAutomationRunIsQuarantined(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "partial", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendText, MessageTemplate: "first", Enabled: true, SortOrder: 1},
			{ActionType: "unknown", Enabled: true, SortOrder: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "partial-order", ChatID: "chat", BuyerID: "buyer"}); err == nil {
		t.Fatal("partial execution should return an error")
	}
	var status string
	var sent int
	if err := store.DB.QueryRowContext(ctx, `SELECT status,sent_count FROM automation_runs WHERE order_id='partial-order'`).Scan(&status, &sent); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" || sent != 1 || len(sender.texts) != 1 {
		t.Fatalf("status=%s sent=%d texts=%v", status, sent, sender.texts)
	}
}
func (s *testSender) SendImage(context.Context, string, string, string, int64) error { return nil }
func (s *testSender) UpdateCookie(cookieStr string) {
	s.cookieUpdates = append(s.cookieUpdates, cookieStr)
	if s.onCookieUpdate != nil {
		s.onCookieUpdate(cookieStr)
	}
}

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

func TestActionDelayUsesCardDefaultUnlessOverridden(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "delayed", Type: "text", TextContent: "x", Enabled: true, DelaySeconds: 15, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	center := New(store, nil, nil)

	got, err := center.actionDelaySeconds(ctx, db.AutomationAction{
		ActionType: ActionSendCard, CardID: cardID, DelaySeconds: 0, ConfigJSON: `{}`,
	})
	if err != nil || got != 15 {
		t.Fatalf("default delay=%d err=%v want 15", got, err)
	}
	got, err = center.actionDelaySeconds(ctx, db.AutomationAction{
		ActionType: ActionSendCard, CardID: cardID, DelaySeconds: 0, ConfigJSON: `{"delay_override":true}`,
	})
	if err != nil || got != 0 {
		t.Fatalf("override delay=%d err=%v want 0", got, err)
	}
}

func TestActionDelayRejectsDisabledCardAndEmptyTextCard(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	disabledID, err := store.Cards.Create(ctx, &db.CardFull{Name: "disabled", Type: "text", TextContent: "x", Enabled: false, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	center := New(store, nil, nil)
	if _, err := center.actionDelaySeconds(ctx, db.AutomationAction{ActionType: ActionSendCard, CardID: disabledID}); err == nil {
		t.Fatal("disabled card must not be executed")
	}
	if _, _, err := center.cardContent(ctx, &db.CardFull{ID: 99, Type: "text", Enabled: true}); err == nil {
		t.Fatal("empty text card must not produce a successful send")
	}
}

func TestSendDataCardKeepsConsumedInventoryWhenDeliveryResultIsUncertain(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "data", Type: "data", DataContent: "secret-1\nsecret-2", Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := &testSender{err: errors.New("temporary send failure")}
	center := New(store, testSenderProvider{sender: sender}, nil)
	_, err = center.sendCard(ctx, Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}, db.AutomationAction{
		ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`,
	})
	if err == nil {
		t.Fatal("send failure must be returned")
	}
	reserved, err := store.Cards.ConsumeBatchData(ctx, cardID)
	if err != nil || reserved != "secret-2" {
		t.Fatalf("uncertain send must not expose the same secret again: got=%q err=%v", reserved, err)
	}
}

func TestCenterSkipsPausedAccountTasks(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "paused", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "must-not-send", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cookies.SetPause(ctx, "cid", 10); err != nil {
		t.Fatal(err)
	}
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "paused-order", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("paused account sent messages: %v", sender.texts)
	}
	if _, err := store.Orders.Get(ctx, "paused-order"); err != nil {
		t.Fatalf("paused event facts must be persisted, got %v", err)
	}
	if _, err := center.ManualFullDelivery(ctx, &db.Order{OrderID: "manual", CookieID: "cid"}); err == nil {
		t.Fatal("manual full delivery must reject a paused account")
	}
	if _, err := store.Cookies.SetPause(ctx, "cid", 0); err != nil {
		t.Fatal(err)
	}
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "must-not-send" {
		t.Fatalf("deferred event was not replayed exactly once: %v", sender.texts)
	}
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	if len(sender.texts) != 1 {
		t.Fatalf("deferred event replay was not idempotent: %v", sender.texts)
	}
	if err := store.Cookies.SetStatus(ctx, "cid", false); err != nil {
		t.Fatal(err)
	}
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "disabled-order", ChatID: "chat"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("disabled account sent messages: %v", sender.texts)
	}
	if _, err := center.ManualFullDelivery(ctx, &db.Order{OrderID: "disabled-manual", CookieID: "cid"}); err == nil {
		t.Fatal("manual full delivery must reject a disabled account")
	}
}

func TestDelayedAutomationIsPersistedAndReplayedWithoutSleeping(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "delayed", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "delayed-message", DelaySeconds: 30, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	start := time.Now()
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "delay-order", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second || len(sender.texts) != 0 {
		t.Fatalf("delayed task blocked or sent immediately: elapsed=%s texts=%v", time.Since(start), sender.texts)
	}
	var pending int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks WHERE status='pending'`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`)
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "delayed-message" {
		t.Fatalf("replayed texts=%v", sender.texts)
	}
}

func TestMultipleActionDelaysPreserveSequentialSemantics(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "sequential", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendText, MessageTemplate: "first", DelaySeconds: 0, Enabled: true, SortOrder: 1},
			{ActionType: ActionSendText, MessageTemplate: "second", DelaySeconds: 30, Enabled: true, SortOrder: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "seq-order", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "first" {
		t.Fatalf("first action was not immediate: %v", sender.texts)
	}
	var cursor int
	if err := store.DB.QueryRowContext(ctx, `SELECT action_cursor FROM automation_runs WHERE order_id='seq-order'`).Scan(&cursor); err != nil || cursor != 1 {
		t.Fatalf("cursor=%d err=%v", cursor, err)
	}
	if err := store.Automation.Update(ctx, admin.ID, ruleID, db.AutomationRuleInput{
		CookieID: "cid", Name: "changed", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendText, MessageTemplate: "inserted", Enabled: true, SortOrder: 1},
			{ActionType: ActionSendText, MessageTemplate: "changed-second", Enabled: true, SortOrder: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`)
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	if len(sender.texts) != 2 || sender.texts[1] != "second" {
		t.Fatalf("second action was not replayed: %v", sender.texts)
	}
}

func TestExpiredRunDuringExternalActionIsQuarantined(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	ruleID, _ := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "crash", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "must-not-repeat", Enabled: true}}})
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "crash-order", ChatID: "chat", BuyerID: "buyer"}
	raw, _ := json.Marshal(task)
	runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "cid", OrderID: "crash-order",
		TriggerType: TriggerBuyerReviewed, TriggerKey: buildTriggerKey(task), RawEventJSON: string(raw), LeaseExpiresAt: time.Now().Add(time.Minute).Unix()})
	if err != nil || !started {
		t.Fatalf("start=%v err=%v", started, err)
	}
	if ok, err := store.Automation.StartRunAction(ctx, runID, 1, 0, time.Now().Add(-time.Minute).Unix()); err != nil || !ok {
		t.Fatalf("start action=%v err=%v", ok, err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, runID)
	sender := &testSender{}
	(&Scheduler{center: New(store, testSenderProvider{sender: sender}, nil)}).runRecoveryTasks(ctx)
	run, _ := store.Automation.GetRun(ctx, runID)
	if run.Status != "needs_review" || len(sender.texts) != 0 {
		t.Fatalf("run=%+v texts=%v", run, sender.texts)
	}
}

func TestInvalidDeferredTaskMovesToDeadLetter(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_pending_tasks
		(task_key,cookie_id,trigger_type,task_json,due_at,status,attempt_count,lease_expires_at,error_message)
		VALUES ('cid:bad','cid',?,'{"broken',0,'pending',0,0,'')`, TriggerBuyerReviewed); err != nil {
		t.Fatal(err)
	}
	scheduler := &Scheduler{center: New(store, testSenderProvider{sender: &testSender{}}, nil)}
	for i := 0; i < 5; i++ {
		_, _ = store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`)
		scheduler.runDeferredTasks(ctx)
	}
	var status string
	var attempts int
	if err := store.DB.QueryRowContext(ctx, `SELECT status,attempt_count FROM automation_pending_tasks WHERE task_key='cid:bad'`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" || attempts != 5 {
		t.Fatalf("status=%s attempts=%d", status, attempts)
	}
}

func TestUnknownExternalAutomationRunCannotBeRetried(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "retry", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "retry-message", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	sender := &testSender{err: errors.New("temporary")}
	center := New(store, testSenderProvider{sender: sender}, nil)
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "retry-order", ChatID: "chat", BuyerID: "buyer"}); err == nil {
		t.Fatal("first execution should fail")
	}
	var runID int64
	var initialStatus string
	if err := store.DB.QueryRowContext(ctx, `SELECT id,status FROM automation_runs WHERE order_id='retry-order'`).Scan(&runID, &initialStatus); err != nil {
		t.Fatal(err)
	}
	if initialStatus != "needs_review" {
		t.Fatalf("ambiguous failure status=%s", initialStatus)
	}
	if err := store.Automation.ResolveRunIssue(ctx, admin.ID, runID, "retry"); err == nil {
		t.Fatal("unknown external send result must reject retry")
	}
	var status string
	if err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id='retry-order'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" {
		t.Fatalf("status=%s want needs_review", status)
	}
}

func TestFailedDeferredTaskRemainsPending(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "deferred-fail", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "x", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "deferred-fail", ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"delays_elapsed": true}}
	raw, _ := json.Marshal(task)
	if err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{TaskKey: "cid:buyer_reviewed:deferred-fail", CookieID: "cid", TriggerType: TriggerBuyerReviewed, TaskJSON: string(raw), DueAt: 0}); err != nil {
		t.Fatal(err)
	}
	center := New(store, testSenderProvider{sender: &testSender{err: errors.New("temporary")}}, nil)
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	var status string
	if err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_pending_tasks WHERE task_key='cid:buyer_reviewed:deferred-fail'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status=%s", status)
	}
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
	if order.PaidAt == "" {
		t.Fatalf("首次付款事件创建订单时应记录 paid_at: %+v", order)
	}
}

func TestCenterBuyerReviewedFirstEventRecordsReviewTime(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-review", Name: "评价赠品", TriggerType: TriggerBuyerReviewed,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendText, MessageTemplate: "谢谢评价", Enabled: true, SortOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("create review rule: %v", err)
	}

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerBuyerReviewed,
		ChatID: "chat-review", OrderID: "order-review", ItemID: "item-review", BuyerID: "buyer-review",
		Raw: map[string]any{"message_id": "review-1"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "谢谢评价" {
		t.Fatalf("评价赠品发送异常: %v", sender.texts)
	}
	order, err := store.Orders.Get(ctx, "order-review")
	if err != nil {
		t.Fatalf("Get order-review: %v", err)
	}
	if order.BuyerReviewedAt == "" {
		t.Fatalf("首次评价事件创建订单时应记录 buyer_reviewed_at: %+v", order)
	}
	sysShipped := true
	if err := store.Orders.Upsert(ctx, "order-review", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "item-review", BuyerID: "buyer-review", ChatID: "chat-review", SystemShipped: &sysShipped,
	}); err != nil {
		t.Fatalf("mark shipped: %v", err)
	}
	due, err := store.Automation.DueReviewRequestOrders(ctx, 200)
	if err != nil {
		t.Fatalf("DueReviewRequestOrders: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("已评价订单不应进入求评价扫描: %+v", due)
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
	if err == nil || !strings.Contains(err.Error(), "未匹配") {
		t.Fatalf("HandleTask should return rule execution errors: %v", err)
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
	center.SetMTop(&mtop.ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"})
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

func TestConfirmShipmentPersistsAuthoritativeSessionBeforeParseError(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	initialValue := "flat_leak=must-not-send; unb=123; _m_h5_tk=flat_old_1"
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "snapshot_old_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "document_only", Value: "doc", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_only", Value: "api", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
	}
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":"yes"}`, snapshot)
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initialValue, metadata, 1); err != nil {
		t.Fatal(err)
	}

	var requestCookie string
	client := &http.Client{Transport: automationRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requestCookie = req.Header.Get("Cookie")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": []string{
				"_m_h5_tk=snapshot_new_2; Domain=.goofish.com; Path=/; Secure",
				"consign_scope=new; Path=/h5; Secure; HttpOnly",
			}},
			Body:    io.NopCloser(strings.NewReader(`{"ret":`)),
			Request: req,
		}, nil
	})}
	lockReleasedBeforeRuntimeUpdate := false
	sender := &testSender{onCookieUpdate: func(string) {
		acquired := make(chan struct{})
		go func() {
			unlock := store.LockAccountCredentials("cid")
			unlock()
			close(acquired)
		}()
		select {
		case <-acquired:
			lockReleasedBeforeRuntimeUpdate = true
		case <-time.After(500 * time.Millisecond):
		}
	}}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetMTop(&mtop.ClientImpl{HTTPClient: client, ConsignURL: mtop.ConsignAPI})
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "session-parse-error", ForceConfirmShipment: true,
	})
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("远程响应解析失败应进入人工核对: %v", err)
	}
	for _, want := range []string{"unb=123", "_m_h5_tk=snapshot_old_1", "api_only=api"} {
		if !strings.Contains(requestCookie, want) {
			t.Fatalf("发货请求 Cookie %q 未使用加锁后重读的权威 Jar，缺少 %q", requestCookie, want)
		}
	}
	for _, unwanted := range []string{"flat_leak=", "document_only="} {
		if strings.Contains(requestCookie, unwanted) {
			t.Fatalf("发货请求 Cookie %q 泄漏了错误作用域 %q", requestCookie, unwanted)
		}
	}

	detail, getErr := store.Cookies.GetDetails(ctx, "cid")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if !strings.Contains(detail.Value, "_m_h5_tk=snapshot_new_2") || strings.Contains(detail.Value, "flat_leak=") {
		t.Fatalf("正文解析失败后未优先持久化响应 Cookie Jar: %q", detail.Value)
	}
	if !strings.Contains(detail.MetadataJSON, `"preserved":"yes"`) {
		t.Fatalf("持久化 Jar 时丢失原 metadata: %s", detail.MetadataJSON)
	}
	gotSnapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok {
		t.Fatalf("响应后权威 snapshot 丢失: %s", detail.MetadataJSON)
	}
	values := make(map[string]string, len(gotSnapshot))
	for _, cookie := range gotSnapshot {
		values[cookie.Name+"|"+cookie.Domain+"|"+cookie.Path] = cookie.Value
	}
	if values["_m_h5_tk|.goofish.com|/"] != "snapshot_new_2" ||
		values["consign_scope|h5api.m.goofish.com|/h5"] != "new" ||
		values["document_only|www.goofish.com|/im"] != "doc" {
		t.Fatalf("响应后 snapshot 作用域不完整: %+v", gotSnapshot)
	}
	if len(sender.cookieUpdates) != 1 || !strings.Contains(sender.cookieUpdates[0], "_m_h5_tk=snapshot_new_2") {
		t.Fatalf("运行实例未同步已持久化的 Cookie: %+v", sender.cookieUpdates)
	}
	if !lockReleasedBeforeRuntimeUpdate {
		t.Fatal("运行实例 Cookie 必须在账号凭证锁外更新")
	}
}

func TestConfirmShipmentPropagatesAuthoritativeEmptySession(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	initialValue := "unb=123; _m_h5_tk=old_1"
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":true}`, []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "old_1", Domain: ".goofish.com", Path: "/", Secure: true},
	})
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initialValue, metadata, 1); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: automationRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": []string{
				"unb=; Max-Age=-1; Domain=.goofish.com; Path=/; Secure",
				"_m_h5_tk=; Max-Age=-1; Domain=.goofish.com; Path=/; Secure",
			}},
			Body:    io.NopCloser(strings.NewReader(`{"ret":`)),
			Request: req,
		}, nil
	})}
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetMTop(&mtop.ClientImpl{HTTPClient: client, ConsignURL: mtop.ConsignAPI})
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "authoritative-empty-session", ForceConfirmShipment: true,
	})
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("删除凭证后的解析失败应进入人工核对: %v", err)
	}
	detail, getErr := store.Cookies.GetDetails(ctx, "cid")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if detail.Value != "" {
		t.Fatalf("权威空 Jar 未持久化: %q", detail.Value)
	}
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok || snapshot == nil || len(snapshot) != 0 {
		t.Fatalf("权威空 snapshot 语义丢失: ok=%v snapshot=%#v metadata=%s", ok, snapshot, detail.MetadataJSON)
	}
	if len(sender.cookieUpdates) != 1 || sender.cookieUpdates[0] != "" {
		t.Fatalf("权威空 Jar 未在锁外通知运行实例: %#v", sender.cookieUpdates)
	}
}

func TestManualFullDeliveryIsImmediateIdempotentAndForcesConfirmation(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	_, _ = store.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id='cid'`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','manual-item','会员')`)
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "manual-card", Type: "text", TextContent: "MANUAL-CARD", Enabled: true, DelaySeconds: 86400, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "manual-item", Name: "manual", TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`, Enabled: true, SortOrder: 1},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := &testSender{}
	mtopMock := &fakeMTop{consignOk: true}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetMTop(mtopMock)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{Quantity: "1", OrderStatus: "pending_ship"}})
	order := &db.Order{OrderID: "manual-order", CookieID: "cid", ItemID: "manual-item", BuyerID: "buyer", ChatID: "chat"}

	sent, err := center.ManualFullDelivery(ctx, order)
	if err != nil || sent != 1 || len(sender.texts) != 1 || mtopMock.consignCalls != 1 {
		t.Fatalf("first manual delivery sent=%d texts=%v consign=%d err=%v", sent, sender.texts, mtopMock.consignCalls, err)
	}
	if _, err := center.ManualFullDelivery(ctx, order); err == nil || !strings.Contains(err.Error(), "执行过") {
		t.Fatalf("duplicate manual delivery should be rejected: %v", err)
	}
	if len(sender.texts) != 1 || mtopMock.consignCalls != 1 {
		t.Fatalf("duplicate request caused side effects: texts=%v consign=%d", sender.texts, mtopMock.consignCalls)
	}
}

// recordingNotifier 记录所有 NotifyDelivery 调用，用于断言 automation.Center 接线。
type recordingNotifier struct {
	mu    sync.Mutex
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
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "review", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}

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

func TestReviewRequestCounterFailureMovesCompletedActionToNeedsReview(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "review-counter", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "请评价", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Orders.Upsert(ctx, "review-counter-order", db.OrderUpsertOpts{CookieID: "cid", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_review_counter
		BEFORE UPDATE OF review_request_count ON orders
		WHEN NEW.review_request_count>OLD.review_request_count
		BEGIN SELECT RAISE(FAIL, 'forced review counter failure'); END`); err != nil {
		t.Fatal(err)
	}
	rule, err := store.Automation.Get(ctx, ruleID)
	if err != nil {
		t.Fatal(err)
	}
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	err = center.executeRule(ctx, Task{
		Source: "scheduler", AccountID: "cid", TriggerType: TriggerReviewMissingTimeout,
		OrderID: "review-counter-order", ChatID: "chat", BuyerID: "buyer",
		Raw: map[string]any{"attempt": 1},
	}, *rule)
	if !errors.Is(err, errAutomationNeedsReview) {
		t.Fatalf("counter failure should require review, got %v", err)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("message action should execute exactly once, got %v", sender.texts)
	}
	order, err := store.Orders.Get(ctx, "review-counter-order")
	if err != nil || order.ReviewRequestCount != 0 {
		t.Fatalf("counter=%d err=%v", order.ReviewRequestCount, err)
	}
	var status, message string
	if err := store.DB.QueryRowContext(ctx, `SELECT status,error_message FROM automation_runs WHERE order_id=?`, "review-counter-order").Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" || !strings.Contains(message, "保存提醒次数失败") {
		t.Fatalf("status=%q message=%q", status, message)
	}
}
