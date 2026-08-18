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

// automationRoundTripperFunc 用于本次流程后续判断的自动化RoundTripperFunc
type automationRoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip 封装RoundTrip业务协调。
func (f automationRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// testSenderProvider 用于本次流程后续判断的testSenderProvider
type testSenderProvider struct{ sender *testSender }

// Sender 封装Sender业务协调。
func (p testSenderProvider) Sender(string) (MessageSender, bool) { return p.sender, true }

// testSender 用于本次流程后续判断的testSender
type testSender struct {
	texts          []string
	cookieUpdates  []string
	onCookieUpdate func(string)
	events         *[]string
	err            error
	failAfter      int
}

// SendText 封装Send文本业务协调。
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

// TestPartialAutomationRunIsQuarantined 封装TestPartial自动化运行IsQuarantined业务协调。
func TestPartialAutomationRunIsQuarantined(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "partial", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendText, MessageTemplate: "first", Enabled: true, SortOrder: 1},
			{ActionType: "unknown", Enabled: true, SortOrder: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "partial-order", ChatID: "chat", BuyerID: "buyer"}); err == nil {
		t.Fatal("partial execution should return an error")
	}
	// status 用于本次流程后续判断的状态
	var status string
	// sent 用于本次流程后续判断的sent
	var sent int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status,sent_count FROM automation_runs WHERE order_id='partial-order'`).Scan(&status, &sent); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" || sent != 1 || len(sender.texts) != 1 {
		t.Fatalf("status=%s sent=%d texts=%v", status, sent, sender.texts)
	}
}

// TestFinishRunFailureQuarantinesSuccessfulExternalAction 验证外部消息已发送但运行结果落库失败时，系统会转入人工核对而不是允许重复重放。
func TestFinishRunFailureQuarantinesSuccessfulExternalAction(t *testing.T) {
	// store 是当前测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// admin 是创建自动化规则所需的管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// ruleErr 表示创建本次测试消息动作规则时的数据库错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "finish-failure", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "gift", Enabled: true}},
	}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// triggerErr 表示故意阻止 success 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_automation_success
		BEFORE UPDATE OF status ON automation_runs
		WHEN NEW.status='success'
		BEGIN SELECT RAISE(ABORT, 'forced finish failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sender 记录已经交给在线发送器的消息，验证外部副作用只发生一次。
	sender := &testSender{}
	// center 是待验证运行结果补偿逻辑的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// runErr 保存动作已执行但结果收口失败后的人工核对错误。
	runErr := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "finish-failure-order", ChatID: "chat", BuyerID: "buyer"})
	if !errors.Is(runErr, errAutomationNeedsReview) {
		t.Fatalf("运行结果落库失败应转人工核对，err=%v", runErr)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "gift" {
		t.Fatalf("外部消息应只发送一次，got %v", sender.texts)
	}
	// status、message 保存补偿后的运行状态和人工核对原因。
	var status, message string
	// queryErr 表示读取补偿后运行状态时的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,error_message FROM automation_runs WHERE order_id=?`, "finish-failure-order").Scan(&status, &message); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" || !strings.Contains(message, "自动化运行结果保存失败") {
		t.Fatalf("运行未进入人工核对状态: status=%q message=%q", status, message)
	}
}

// TestFinishAndQuarantineFailureIsReturned 验证运行结果和人工核对状态均无法落库时，两个错误都会返回给上层而不会被日志吞掉。
func TestFinishAndQuarantineFailureIsReturned(t *testing.T) {
	// store 是当前测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// admin 是创建自动化规则所需的管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// ruleErr 表示创建本次测试消息动作规则时的数据库错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "finish-and-quarantine-failure", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "gift", Enabled: true}},
	}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// triggerErr 表示故意阻止 success 和 needs_review 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_automation_result_states
		BEFORE UPDATE OF status ON automation_runs
		WHEN NEW.status IN ('success','needs_review')
		BEGIN SELECT RAISE(ABORT, 'forced result-state failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sender 记录已经交给在线发送器的消息，验证外部副作用只发生一次。
	sender := &testSender{}
	// center 是待验证双重落库失败错误传播逻辑的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// runErr 保存结果收口和补偿收口均失败后的组合错误。
	runErr := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "finish-and-quarantine-failure-order", ChatID: "chat", BuyerID: "buyer"})
	if !errors.Is(runErr, errAutomationNeedsReview) || !strings.Contains(runErr.Error(), "保存人工核对状态失败") {
		t.Fatalf("双重落库失败应返回完整人工核对错误，err=%v", runErr)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("外部消息应只发送一次，got %v", sender.texts)
	}
}

// TestMessageDefinitelyNotSentIsRetried 封装Test消息DefinitelyNotSentIsRetried业务协调。
func TestMessageDefinitelyNotSentIsRetried(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "retry-before-send", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "gift", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{err: fmt.Errorf("%w: websocket reconnecting", ErrMessageNotSent)}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 用于本次流程后续判断的任务
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "retry-order", ChatID: "chat", BuyerID: "buyer"}
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, task); err == nil {
		t.Fatal("首次发送应返回连接未就绪错误")
	}
	// status 用于本次流程后续判断的状态
	var status string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id=?`, task.OrderID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("确定未发送应进入可重试 failed，got %q", status)
	}
	sender.err = nil
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE order_id=?`, task.OrderID); err != nil {
		t.Fatal(err)
	}
	NewScheduler(center).runRecoveryTasks(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "gift" {
		t.Fatalf("连接恢复后应安全重试，got %v", sender.texts)
	}
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id=?`, task.OrderID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "success" {
		t.Fatalf("重试后 status=%q want success", status)
	}
}

// TestAbortRunActionFailureQuarantinesRun 封装TestAbort运行动作FailureQuarantines运行业务协调。
func TestAbortRunActionFailureQuarantinesRun(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "abort-failure", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "gift", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `CREATE TRIGGER fail_abort_run_action
		BEFORE UPDATE OF action_started ON automation_runs
		WHEN OLD.action_started = 1 AND NEW.action_started = 0
		BEGIN SELECT RAISE(ABORT, 'forced abort failure'); END`); err != nil {
		t.Fatal(err)
	}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: &testSender{err: fmt.Errorf("%w: websocket unavailable", ErrMessageNotSent)}}, nil)
	// task 用于本次流程后续判断的任务
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "abort-failure-order", ChatID: "chat", BuyerID: "buyer"}
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, task); err == nil || !errors.Is(err, errAutomationNeedsReview) {
		t.Fatalf("回滚检查点失败后应要求人工核对，err=%v", err)
	}
	// status 用于本次流程后续判断的状态
	var status string
	// actionStarted 用于本次流程后续判断的动作Started
	var actionStarted int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status,action_started FROM automation_runs WHERE order_id=?`, task.OrderID).Scan(&status, &actionStarted); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" || actionStarted != 1 {
		t.Fatalf("status=%q action_started=%d want needs_review/1", status, actionStarted)
	}
}

// TestCardDefinitelyNotSentIsRetriedAndDataInventoryRestored 封装Test卡密DefinitelyNotSentIsRetriedAnd数据InventoryRestored业务协调。
func TestCardDefinitelyNotSentIsRetriedAndDataInventoryRestored(t *testing.T) {
	// tc 表示当前遍历过程中的tc
	for _, tc := range []struct {
		name       string
		cardType   string
		cardColumn string
	}{
		{name: "text", cardType: "text", cardColumn: "text_content"},
		{name: "data", cardType: "data", cardColumn: "data_content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// store、cleanup 用于本次流程后续判断的store、cleanup
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// admin 用于本次流程后续判断的admin
			admin, _ := store.Users.GetByUsername(ctx, "admin")
			// query 用于本次流程后续判断的查询
			query := fmt.Sprintf(`INSERT INTO cards (name,type,%s,enabled,user_id) VALUES (?,?,?,?,?)`, tc.cardColumn)
			// res、err 用于本次流程后续判断的res、err
			res, err := store.DB.ExecContext(ctx, query, "gift", tc.cardType, "GIFT-CODE", 1, admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			// cardID 用于本次流程后续判断的卡密ID
			cardID, _ := res.LastInsertId()
			if // err 用于本次流程后续判断的err
			_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
				UserID: admin.ID, CookieID: "cid", ItemID: "item-card", Name: "card-retry-" + tc.name,
				TriggerType: TriggerBuyerReviewed, Enabled: true,
				Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true}},
			}); err != nil {
				t.Fatal(err)
			}
			// sender 用于本次流程后续判断的sender
			sender := &testSender{err: fmt.Errorf("%w: websocket reconnecting", ErrMessageNotSent)}
			// center 用于本次流程后续判断的center
			center := New(store, testSenderProvider{sender: sender}, nil)
			// task 用于本次流程后续判断的任务
			task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "card-order-" + tc.name,
				ItemID: "item-card", ChatID: "chat", BuyerID: "buyer"}
			if // err 用于本次流程后续判断的err
			err := center.HandleTask(ctx, task); err == nil {
				t.Fatal("首次卡密发送应返回连接未就绪错误")
			}
			// status 用于本次流程后续判断的状态
			var status string
			if // err 用于本次流程后续判断的err
			err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id=?`, task.OrderID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "failed" {
				t.Fatalf("确定未发送的卡密应进入可重试 failed，got %q", status)
			}
			if tc.cardType == "data" {
				// inventory 用于本次流程后续判断的inventory
				var inventory string
				if // err 用于本次流程后续判断的err
				err := store.DB.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&inventory); err != nil || inventory != "GIFT-CODE" {
					t.Fatalf("未发送 Data 卡密必须恢复库存: inventory=%q err=%v", inventory, err)
				}
			}
			sender.err = nil
			if // err 用于本次流程后续判断的err
			_, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE order_id=?`, task.OrderID); err != nil {
				t.Fatal(err)
			}
			NewScheduler(center).runRecoveryTasks(ctx)
			if len(sender.texts) != 1 || sender.texts[0] != "GIFT-CODE" {
				t.Fatalf("连接恢复后应发送一次卡密，got %v", sender.texts)
			}
		})
	}
}

// TestRuleMatchingUsesStoredOrderItemWhenEventOmitsItemID 封装Test规则MatchingUsesStored订单商品WhenEventOmits商品ID业务协调。
func TestRuleMatchingUsesStoredOrderItemWhenEventOmitsItemID(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "known-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "known-item", BuyerID: "buyer", ChatID: "chat",
	}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "known-item", Name: "item-specific-review",
		TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review-gift", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{
		AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "known-order",
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "review-gift" {
		t.Fatalf("应使用本地订单 item_id 匹配商品规则，got %v", sender.texts)
	}
}

// TestImageCardMissingSenderIsDefinitelyNotSent 封装Test图片卡密MissingSenderIsDefinitelyNotSent业务协调。
func TestImageCardMissingSenderIsDefinitelyNotSent(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 用于本次流程后续判断的center
	center := New(store, nil, nil)
	// err 用于本次流程后续判断的err
	err := center.sendImage(context.Background(), Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}, "https://example.com/gift.png", 1)
	if !errors.Is(err, ErrMessageNotSent) {
		t.Fatalf("图片发送器缺失应标记为明确未发送，got %v", err)
	}
}

// SendImage 封装Send图片业务协调。
func (s *testSender) SendImage(context.Context, string, string, string, int64) error { return nil }

// UpdateCookie 更新登录凭证。
func (s *testSender) UpdateCookie(cookieStr string) {
	s.cookieUpdates = append(s.cookieUpdates, cookieStr)
	if s.onCookieUpdate != nil {
		s.onCookieUpdate(cookieStr)
	}
}

// testFetcher 用于本次流程后续判断的testFetcher
type testFetcher struct {
	detail *OrderDetail
	err    error
	calls  *int
}

// FetchOrderDetail 封装Fetch订单Detail业务协调。
func (f testFetcher) FetchOrderDetail(context.Context, string, string, string, string, string) (*OrderDetail, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.detail, f.err
}

// TestReviewAutomationsDoNotRequireOrderDetail 封装TestReviewAutomationsDoNotRequire订单Detail业务协调。
func TestReviewAutomationsDoNotRequireOrderDetail(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// trigger 表示当前遍历过程中的trigger
	for _, trigger := range []string{TriggerBuyerReviewed, TriggerReviewMissingTimeout} {
		if // err 用于本次流程后续判断的err
		_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
			UserID: admin.ID, CookieID: "cid", Name: trigger, TriggerType: trigger, Enabled: true,
			Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: trigger, Enabled: true}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// calls 用于本次流程后续判断的calls
	calls := 0
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{err: errors.New("must not fetch"), calls: &calls},
	})
	// i、trigger 表示当前遍历过程中的i、trigger
	for i, trigger := range []string{TriggerBuyerReviewed, TriggerReviewMissingTimeout} {
		// task 用于本次流程后续判断的任务
		task := Task{Source: "ws", AccountID: "cid", TriggerType: trigger, OrderID: fmt.Sprintf("review-no-detail-%d", i), ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"attempt": 1}}
		if // err 用于本次流程后续判断的err
		err := center.HandleTask(ctx, task); err != nil {
			t.Fatalf("%s should not fetch order detail: %v", trigger, err)
		}
	}
	if calls != 0 || len(sender.texts) != 2 {
		t.Fatalf("order detail calls=%d texts=%v", calls, sender.texts)
	}
}

// TestOrderPaidPreparationFailureIsPersistedAndRecovered 封装Test订单PaidPreparationFailureIsPersistedAndRecovered业务协调。
func TestOrderPaidPreparationFailureIsPersistedAndRecovered(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','pending-item','商品',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (91,'库存','text','RECOVERED-CARD',1,?)`, admin.ID)
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "pending-item", Name: "付款恢复", TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: 91, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"恢复版"}`, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{err: errors.New("temporary order API failure")},
	})
	// task 用于本次流程后续判断的任务
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "pending-order", ItemID: "pending-item", ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"message_id": "paid-1"}}
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, task); err != nil {
		t.Fatalf("preparation failure should be durably deferred: %v", err)
	}
	// pending、runs 用于本次流程后续判断的pending、runs
	var pending, runs int
	_ = store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks WHERE cookie_id='cid' AND trigger_type='order_paid'`).Scan(&pending)
	_ = store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE order_id='pending-order'`).Scan(&runs)
	if pending != 1 || runs != 0 {
		t.Fatalf("pending=%d runs=%d", pending, runs)
	}
	// recoveredCenter 模拟进程重启后以可用详情查询器重新装配自动化中心。
	recoveredCenter := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{SpecName: "套餐", SpecValue: "恢复版", Quantity: "1", Amount: "9.9"}},
	})
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`)
	NewScheduler(recoveredCenter).runDeferredTasks(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "RECOVERED-CARD" {
		t.Fatalf("recovered sends=%v", sender.texts)
	}
	_ = store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks WHERE cookie_id='cid'`).Scan(&pending)
	// status 用于本次流程后续判断的状态
	var status string
	_ = store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id='pending-order'`).Scan(&status)
	if pending != 0 || status != "success" {
		t.Fatalf("pending=%d status=%q", pending, status)
	}
}

// newAutomationTestStore 封装new自动化TestStore业务协调。
func newAutomationTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// database、err 用于本次流程后续判断的database、err
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, db.DialectSQLite)
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(context.Background(), "admin", "admin@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	return store, func() { _ = database.Close() }
}

// TestActionDelayUsesCardDefaultUnlessOverridden 封装Test动作延迟Uses卡密DefaultUnlessOverridden业务协调。
func TestActionDelayUsesCardDefaultUnlessOverridden(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// cardID、err 用于本次流程后续判断的卡密ID、err
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "delayed", Type: "text", TextContent: "x", Enabled: true, DelaySeconds: 15, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// center 用于本次流程后续判断的center
	center := New(store, nil, nil)

	// got、err 用于本次流程后续判断的got、err
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

// TestActionDelayRejectsDisabledCardAndEmptyTextCard 封装Test动作延迟RejectsDisabled卡密AndEmpty文本卡密业务协调。
func TestActionDelayRejectsDisabledCardAndEmptyTextCard(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// disabledID、err 用于本次流程后续判断的disabledID、err
	disabledID, err := store.Cards.Create(ctx, &db.CardFull{Name: "disabled", Type: "text", TextContent: "x", Enabled: false, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	// center 用于本次流程后续判断的center
	center := New(store, nil, nil)
	if // err 用于本次流程后续判断的err
	_, err := center.actionDelaySeconds(ctx, db.AutomationAction{ActionType: ActionSendCard, CardID: disabledID}); err == nil {
		t.Fatal("disabled card must not be executed")
	}
	if // err 用于本次流程后续判断的err
	_, _, err := center.cardContent(ctx, &db.CardFull{ID: 99, Type: "text", Enabled: true}); err == nil {
		t.Fatal("empty text card must not produce a successful send")
	}
}

// TestSendDataCardKeepsConsumedInventoryWhenDeliveryResultIsUncertain 封装TestSend数据卡密KeepsConsumedInventoryWhen发货结果IsUncertain业务协调。
func TestSendDataCardKeepsConsumedInventoryWhenDeliveryResultIsUncertain(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// cardID、err 用于本次流程后续判断的卡密ID、err
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "data", Type: "data", DataContent: "secret-1\nsecret-2", Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{err: errors.New("temporary send failure")}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	_, err = center.sendCard(ctx, Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}, db.AutomationAction{
		ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`,
	})
	if err == nil {
		t.Fatal("send failure must be returned")
	}
	// reserved、err 用于本次流程后续判断的reserved、err
	reserved, err := store.Cards.ConsumeBatchData(ctx, cardID)
	if err != nil || reserved != "secret-2" {
		t.Fatalf("uncertain send must not expose the same secret again: got=%q err=%v", reserved, err)
	}
}

// TestCenterSkipsPausedAccountTasks 封装TestCenterSkipsPaused账号任务列表业务协调。
func TestCenterSkipsPausedAccountTasks(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "paused", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "must-not-send", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Cookies.SetPause(ctx, "cid", 10); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "paused-order", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("paused account sent messages: %v", sender.texts)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Orders.Get(ctx, "paused-order"); err != nil {
		t.Fatalf("paused event facts must be persisted, got %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := center.ManualFullDelivery(ctx, &db.Order{OrderID: "manual", CookieID: "cid"}); err == nil {
		t.Fatal("manual full delivery must reject a paused account")
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Cookies.SetPause(ctx, "cid", 0); err != nil {
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
	if // err 用于本次流程后续判断的err
	err := store.Cookies.SetStatus(ctx, "cid", false); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "disabled-order", ChatID: "chat"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("disabled account sent messages: %v", sender.texts)
	}
	if // err 用于本次流程后续判断的err
	_, err := center.ManualFullDelivery(ctx, &db.Order{OrderID: "disabled-manual", CookieID: "cid"}); err == nil {
		t.Fatal("manual full delivery must reject a disabled account")
	}
}

// TestDelayedAutomationIsPersistedAndReplayedWithoutSleeping 封装TestDelayed自动化IsPersistedAndReplayedWithoutSleeping业务协调。
func TestDelayedAutomationIsPersistedAndReplayedWithoutSleeping(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "delayed", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "delayed-message", DelaySeconds: 30, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	// start 用于本次流程后续判断的开始
	start := time.Now()
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "delay-order", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second || len(sender.texts) != 0 {
		t.Fatalf("delayed task blocked or sent immediately: elapsed=%s texts=%v", time.Since(start), sender.texts)
	}
	// pending 用于本次流程后续判断的pending
	var pending int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks WHERE status='pending'`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`)
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "delayed-message" {
		t.Fatalf("replayed texts=%v", sender.texts)
	}
}

// TestMultipleActionDelaysPreserveSequentialSemantics 封装TestMultiple动作DelaysPreserveSequentialSemantics业务协调。
func TestMultipleActionDelaysPreserveSequentialSemantics(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、err 用于本次流程后续判断的规则ID、err
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
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "seq-order", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "first" {
		t.Fatalf("first action was not immediate: %v", sender.texts)
	}
	// cursor 用于本次流程后续判断的游标
	var cursor int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT action_cursor FROM automation_runs WHERE order_id='seq-order'`).Scan(&cursor); err != nil || cursor != 1 {
		t.Fatalf("cursor=%d err=%v", cursor, err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Automation.Update(ctx, admin.ID, ruleID, db.AutomationRuleInput{
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

// TestExpiredRunDuringExternalActionIsQuarantined 封装TestExpired运行DuringExternal动作IsQuarantined业务协调。
func TestExpiredRunDuringExternalActionIsQuarantined(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID 用于本次流程后续判断的规则ID
	ruleID, _ := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "crash", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "must-not-repeat", Enabled: true}}})
	// task 用于本次流程后续判断的任务
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "crash-order", ChatID: "chat", BuyerID: "buyer"}
	// raw 用于本次流程后续判断的原始
	raw, _ := json.Marshal(task)
	// runID、started、err 用于本次流程后续判断的运行ID、started、err
	runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "cid", OrderID: "crash-order",
		TriggerType: TriggerBuyerReviewed, TriggerKey: buildTriggerKey(task), RawEventJSON: string(raw), LeaseExpiresAt: time.Now().Add(time.Minute).Unix()})
	if err != nil || !started {
		t.Fatalf("start=%v err=%v", started, err)
	}
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Automation.StartRunAction(ctx, runID, 1, 0, time.Now().Add(-time.Minute).Unix()); err != nil || !ok {
		t.Fatalf("start action=%v err=%v", ok, err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, runID)
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	(&Scheduler{center: New(store, testSenderProvider{sender: sender}, nil)}).runRecoveryTasks(ctx)
	// run 用于本次流程后续判断的运行
	run, _ := store.Automation.GetRun(ctx, runID)
	if run.Status != "needs_review" || len(sender.texts) != 0 {
		t.Fatalf("run=%+v texts=%v", run, sender.texts)
	}
}

// TestInvalidDeferredTaskMovesToDeadLetter 封装TestInvalidDeferred任务MovesToDeadLetter业务协调。
func TestInvalidDeferredTaskMovesToDeadLetter(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO automation_pending_tasks
		(task_key,cookie_id,trigger_type,task_json,due_at,status,attempt_count,lease_expires_at,error_message)
		VALUES ('cid:bad','cid',?,'{"broken',0,'pending',0,0,'')`, TriggerBuyerReviewed); err != nil {
		t.Fatal(err)
	}
	// scheduler 用于本次流程后续判断的scheduler
	scheduler := &Scheduler{center: New(store, testSenderProvider{sender: &testSender{}}, nil)}
	for // i 用于本次流程后续判断的i
	i := 0; i < 5; i++ {
		_, _ = store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`)
		scheduler.runDeferredTasks(ctx)
	}
	// status 用于本次流程后续判断的状态
	var status string
	// attempts 用于本次流程后续判断的尝试次数
	var attempts int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status,attempt_count FROM automation_pending_tasks WHERE task_key='cid:bad'`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" || attempts != 5 {
		t.Fatalf("status=%s attempts=%d", status, attempts)
	}
}

// TestUnknownExternalAutomationRunCannotBeRetried 封装TestUnknownExternal自动化运行CannotBeRetried业务协调。
func TestUnknownExternalAutomationRunCannotBeRetried(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "retry", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "retry-message", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{err: errors.New("temporary")}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "retry-order", ChatID: "chat", BuyerID: "buyer"}); err == nil {
		t.Fatal("first execution should fail")
	}
	// runID 用于本次流程后续判断的运行ID
	var runID int64
	// initialStatus 用于本次流程后续判断的initial状态
	var initialStatus string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT id,status FROM automation_runs WHERE order_id='retry-order'`).Scan(&runID, &initialStatus); err != nil {
		t.Fatal(err)
	}
	if initialStatus != "needs_review" {
		t.Fatalf("ambiguous failure status=%s", initialStatus)
	}
	if // err 用于本次流程后续判断的err
	err := store.Automation.ResolveRunIssue(ctx, admin.ID, runID, "retry"); err == nil {
		t.Fatal("unknown external send result must reject retry")
	}
	// status 用于本次流程后续判断的状态
	var status string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id='retry-order'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" {
		t.Fatalf("status=%s want needs_review", status)
	}
}

// TestFailedDeferredTaskRemainsPending 封装Test失败Deferred任务RemainsPending业务协调。
func TestFailedDeferredTaskRemainsPending(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "deferred-fail", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "x", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// task 用于本次流程后续判断的任务
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "deferred-fail", ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"delays_elapsed": true}}
	// raw 用于本次流程后续判断的原始
	raw, _ := json.Marshal(task)
	if // err 用于本次流程后续判断的err
	err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{TaskKey: "cid:buyer_reviewed:deferred-fail", CookieID: "cid", TriggerType: TriggerBuyerReviewed, TaskJSON: string(raw), DueAt: 0}); err != nil {
		t.Fatal(err)
	}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: &testSender{err: errors.New("temporary")}}, nil)
	(&Scheduler{center: center}).runDeferredTasks(ctx)
	// status 用于本次流程后续判断的状态
	var status string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_pending_tasks WHERE task_key='cid:buyer_reviewed:deferred-fail'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status=%s", status)
	}
}

// TestCenterOrderPaidFetchesOrderDetailMatchesSpecAndQuantity 封装TestCenter订单PaidFetches订单DetailMatchesSpecAndQuantity业务协调。
func TestCenterOrderPaidFetchesOrderDetailMatchesSpecAndQuantity(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()

	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-1','会员',1)`); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,data_content,enabled,user_id) VALUES
		(11,'30天库存','data','A1'||char(10)||'A2'||char(10)||'A3'||char(10)||'A4',1,?),
		(12,'90天库存','data','B1'||char(10)||'B2'||char(10)||'B3'||char(10)||'B4',1,?)`, admin.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	// ruleID、err 用于本次流程后续判断的规则ID、err
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

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{
			SpecName: "套餐", SpecValue: "90天", Quantity: "2", Amount: "19.8", OrderStatus: "pending_ship",
		}},
	})

	err = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m1"},
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	if // got、want 用于本次流程后续判断的got、want
	got, want := len(sender.texts), 4; got != want {
		t.Fatalf("发送条数=%d want %d texts=%v", got, want, sender.texts)
	}
	// i、want 表示当前遍历过程中的i、want
	for i, want := range []string{"B1", "B2", "B3", "B4"} {
		if sender.texts[i] != want {
			t.Fatalf("texts[%d]=%q want %q", i, sender.texts[i], want)
		}
	}
	// order、err 用于本次流程后续判断的order、err
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

// TestCenterBuyerReviewedFirstEventRecordsReviewTime 封装TestCenter买家ReviewedFirstEventRecordsReview时间业务协调。
func TestCenterBuyerReviewedFirstEventRecordsReviewTime(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	// err 用于本次流程后续判断的err
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

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerBuyerReviewed,
		ChatID: "chat-review", OrderID: "order-review", ItemID: "item-review", BuyerID: "buyer-review",
		Raw: map[string]any{"message_id": "review-1"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "谢谢评价" {
		t.Fatalf("评价赠品发送异常: %v", sender.texts)
	}
	// order、err 用于本次流程后续判断的order、err
	order, err := store.Orders.Get(ctx, "order-review")
	if err != nil {
		t.Fatalf("Get order-review: %v", err)
	}
	if order.BuyerReviewedAt == "" {
		t.Fatalf("首次评价事件创建订单时应记录 buyer_reviewed_at: %+v", order)
	}
	// sysShipped 用于本次流程后续判断的sysShipped
	sysShipped := true
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "order-review", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "item-review", BuyerID: "buyer-review", ChatID: "chat-review", SystemShipped: &sysShipped,
	}); err != nil {
		t.Fatalf("mark shipped: %v", err)
	}
	// due、err 用于本次流程后续判断的due、err
	due, err := store.Automation.DueReviewRequestOrders(ctx, 200)
	if err != nil {
		t.Fatalf("DueReviewRequestOrders: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("已评价订单不应进入求评价扫描: %+v", due)
	}
}

// TestCenterOrderPaidSendsAllCardActionsForSameSpec 封装TestCenter订单PaidSendsAll卡密动作列表ForSameSpec业务协调。
func TestCenterOrderPaidSendsAllCardActionsForSameSpec(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-bundle','组合商品',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES
		(41,'主卡库存','text','MAIN-CARD',1,?),
		(42,'附赠卡库存','text','GIFT-CARD',1,?)`, admin.ID, admin.ID)
	// err 用于本次流程后续判断的err
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

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{
			SpecName: "套餐", SpecValue: "组合版", Quantity: "1", Amount: "29.9",
		}},
	})

	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-bundle", OrderID: "order-bundle", ItemID: "item-bundle", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m-bundle"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	// want 用于本次流程后续判断的want
	want := []string{"MAIN-CARD", "GIFT-CARD"}
	if len(sender.texts) != len(want) {
		t.Fatalf("发送内容=%v want %v", sender.texts, want)
	}
	// i 表示当前遍历过程中的i
	for i := range want {
		if sender.texts[i] != want[i] {
			t.Fatalf("发送内容=%v want %v", sender.texts, want)
		}
	}
}

// TestCenterOrderPaidDoesNotConfirmWhenNoCardSpecMatches 封装TestCenter订单PaidDoesNotConfirmWhenNo卡密SpecMatches业务协调。
func TestCenterOrderPaidDoesNotConfirmWhenNoCardSpecMatches(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-1','会员',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (21,'30天库存','text','A',1,?)`, admin.ID)
	// err 用于本次流程后续判断的err
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

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{SpecName: "套餐", SpecValue: "90天", Quantity: "1"}},
	})

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
	// order、err 用于本次流程后续判断的order、err
	order, err := store.Orders.Get(ctx, "order-no-match")
	if err != nil {
		t.Fatal(err)
	}
	if order.SystemShipped {
		t.Fatal("规格不匹配时不应确认发货")
	}
}

// TestCenterOrderPaidSendsCardBeforeConfirmShipment 封装TestCenter订单PaidSends卡密BeforeConfirmShipment业务协调。
func TestCenterOrderPaidSendsCardBeforeConfirmShipment(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','item-1','会员')`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (31,'默认库存','text','CARD-1',1,?)`, admin.ID)
	// err 用于本次流程后续判断的err
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

	// events 用于本次流程后续判断的events
	events := []string{}
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events = append(events, "confirm")
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	// sender 用于本次流程后续判断的sender
	sender := &testSender{events: &events}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop:               &mtop.ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"},
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}},
	})

	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-seq", ItemID: "item-1", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m3"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	// want 用于本次流程后续判断的want
	want := []string{"send:CARD-1", "confirm"}
	if len(events) != len(want) {
		t.Fatalf("events=%v want %v", events, want)
	}
	// i 表示当前遍历过程中的i
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events=%v want %v", events, want)
		}
	}
}

// TestConfirmShipmentPersistsAuthoritativeSessionBeforeParseError 封装TestConfirmShipmentPersistsAuthoritative会话BeforeParse错误业务协调。
func TestConfirmShipmentPersistsAuthoritativeSessionBeforeParseError(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// initialValue 用于本次流程后续判断的initial值
	initialValue := "flat_leak=must-not-send; unb=123; _m_h5_tk=flat_old_1"
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "snapshot_old_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "document_only", Value: "doc", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_only", Value: "api", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":"yes"}`, snapshot)
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initialValue, metadata, 1); err != nil {
		t.Fatal(err)
	}

	// requestCookie 用于本次流程后续判断的请求登录凭证
	var requestCookie string
	// client 用于本次流程后续判断的client
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
	// lockReleasedBeforeRuntimeUpdate 用于本次流程后续判断的锁ReleasedBeforeRuntimeUpdate
	lockReleasedBeforeRuntimeUpdate := false
	// sender 用于本次流程后续判断的sender
	sender := &testSender{onCookieUpdate: func(string) {
		// acquired 用于本次流程后续判断的acquired
		acquired := make(chan struct{})
		go func() {
			// unlock 用于本次流程后续判断的unlock
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
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: &mtop.ClientImpl{HTTPClient: client, ConsignURL: mtop.ConsignAPI},
	})
	// err 用于本次流程后续判断的err
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "session-parse-error", ForceConfirmShipment: true,
	})
	// uncertain 用于本次流程后续判断的uncertain
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("远程响应解析失败应进入人工核对: %v", err)
	}
	// want 表示当前遍历过程中的want
	for _, want := range []string{"unb=123", "_m_h5_tk=snapshot_old_1", "api_only=api"} {
		if !strings.Contains(requestCookie, want) {
			t.Fatalf("发货请求 Cookie %q 未使用加锁后重读的权威 Jar，缺少 %q", requestCookie, want)
		}
	}
	// unwanted 表示当前遍历过程中的unwanted
	for _, unwanted := range []string{"flat_leak=", "document_only="} {
		if strings.Contains(requestCookie, unwanted) {
			t.Fatalf("发货请求 Cookie %q 泄漏了错误作用域 %q", requestCookie, unwanted)
		}
	}

	// detail、getErr 用于本次流程后续判断的detail、getErr
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
	// gotSnapshot、ok 用于本次流程后续判断的gotSnapshot、ok
	gotSnapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok {
		t.Fatalf("响应后权威 snapshot 丢失: %s", detail.MetadataJSON)
	}
	// values 用于本次流程后续判断的values
	values := make(map[string]string, len(gotSnapshot))
	// cookie 表示当前遍历过程中的登录凭证
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

// TestConfirmShipmentPropagatesAuthoritativeEmptySession 封装TestConfirmShipmentPropagatesAuthoritativeEmpty会话业务协调。
func TestConfirmShipmentPropagatesAuthoritativeEmptySession(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// initialValue 用于本次流程后续判断的initial值
	initialValue := "unb=123; _m_h5_tk=old_1"
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":true}`, []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "old_1", Domain: ".goofish.com", Path: "/", Secure: true},
	})
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initialValue, metadata, 1); err != nil {
		t.Fatal(err)
	}
	// client 用于本次流程后续判断的client
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
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: &mtop.ClientImpl{HTTPClient: client, ConsignURL: mtop.ConsignAPI},
	})
	// err 用于本次流程后续判断的err
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "authoritative-empty-session", ForceConfirmShipment: true,
	})
	// uncertain 用于本次流程后续判断的uncertain
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("删除凭证后的解析失败应进入人工核对: %v", err)
	}
	// detail、getErr 用于本次流程后续判断的detail、getErr
	detail, getErr := store.Cookies.GetDetails(ctx, "cid")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if detail.Value != "" {
		t.Fatalf("权威空 Jar 未持久化: %q", detail.Value)
	}
	// snapshot、ok 用于本次流程后续判断的snapshot、ok
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok || snapshot == nil || len(snapshot) != 0 {
		t.Fatalf("权威空 snapshot 语义丢失: ok=%v snapshot=%#v metadata=%s", ok, snapshot, detail.MetadataJSON)
	}
	if len(sender.cookieUpdates) != 1 || sender.cookieUpdates[0] != "" {
		t.Fatalf("权威空 Jar 未在锁外通知运行实例: %#v", sender.cookieUpdates)
	}
}

// TestManualFullDeliveryIsImmediateIdempotentAndForcesConfirmation 封装TestManualFull发货IsImmediateIdempotentAndForcesConfirmation业务协调。
func TestManualFullDeliveryIsImmediateIdempotentAndForcesConfirmation(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	_, _ = store.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id='cid'`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','manual-item','会员')`)
	// cardID、err 用于本次流程后续判断的卡密ID、err
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
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// mtopMock 用于本次流程后续判断的mtopMock
	mtopMock := &fakeMTop{consignOk: true}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop:               mtopMock,
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", OrderStatus: "pending_ship"}},
	})
	// order 用于本次流程后续判断的订单
	order := &db.Order{OrderID: "manual-order", CookieID: "cid", ItemID: "manual-item", BuyerID: "buyer", ChatID: "chat"}

	// sent、err 用于本次流程后续判断的sent、err
	sent, err := center.ManualFullDelivery(ctx, order)
	if err != nil || sent != 1 || len(sender.texts) != 1 || mtopMock.consignCalls != 1 {
		t.Fatalf("first manual delivery sent=%d texts=%v consign=%d err=%v", sent, sender.texts, mtopMock.consignCalls, err)
	}
	if // err 用于本次流程后续判断的err
	_, err := center.ManualFullDelivery(ctx, order); err == nil || !strings.Contains(err.Error(), "执行过") {
		t.Fatalf("duplicate manual delivery should be rejected: %v", err)
	}
	if len(sender.texts) != 1 || mtopMock.consignCalls != 1 {
		t.Fatalf("duplicate request caused side effects: texts=%v consign=%d", sender.texts, mtopMock.consignCalls)
	}
}

// newManualDeliveryFixture 创建可重复使用的手动完整发货测试夹具，并返回清理函数。
func newManualDeliveryFixture(t *testing.T, orderID string) (context.Context, *db.Store, *Center, *testSender, *fakeMTop, *db.Order, func()) {
	t.Helper()
	// store、cleanup 保存测试数据库及其关闭函数。
	store, cleanup := newAutomationTestStore(t)
	// ctx 保存夹具共用的数据库上下文。
	ctx := context.Background()
	// admin 保存创建卡密和规则所需的管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// updateResult 保存关闭自动确认后执行 SQL 的结果。
	if _, updateResultErr := store.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id='cid'`); updateResultErr != nil {
		t.Fatal(updateResultErr)
	}
	// itemResult 保存测试商品占位记录的写入结果。
	if _, itemResultErr := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','manual-item','会员')`); itemResultErr != nil {
		t.Fatal(itemResultErr)
	}
	// cardID 保存手动发货使用的卡密组标识。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{
		Name: "manual-card", Type: "text", TextContent: "MANUAL-CARD", Enabled: true, DelaySeconds: 86400, UserID: admin.ID,
	})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleInputErr 保存自动发货规则创建错误。
	if _, ruleInputErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "manual-item", Name: "manual-finish-failure", TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`, Enabled: true, SortOrder: 1},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		},
	}); ruleInputErr != nil {
		t.Fatal(ruleInputErr)
	}
	// sender 记录手动发货发送的卡密消息。
	sender := &testSender{}
	// mtopMock 记录远端确认发货调用。
	mtopMock := &fakeMTop{consignOk: true}
	// center 是待验证的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop:               mtopMock,
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", OrderStatus: "pending_ship"}},
	})
	// order 保存夹具使用的订单输入。
	order := &db.Order{OrderID: orderID, CookieID: "cid", ItemID: "manual-item", BuyerID: "buyer", ChatID: "chat"}
	return ctx, store, center, sender, mtopMock, order, cleanup
}

// TestManualFullDeliveryFinishFailureQuarantinesExternalSuccess 验证手动发货外部动作完成但 FinishRun 失败时会隔离运行，避免重复发货。
func TestManualFullDeliveryFinishFailureQuarantinesExternalSuccess(t *testing.T) {
	// ctx、store、center、sender、mtopMock、order、cleanup 保存手动发货测试夹具。
	ctx, store, center, sender, mtopMock, order, cleanup := newManualDeliveryFixture(t, "manual-finish-failure-order")
	defer cleanup()
	// triggerErr 表示故意阻止 success 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_manual_finish_success
		BEFORE UPDATE OF status ON automation_runs
		WHEN NEW.status='success'
		BEGIN SELECT RAISE(ABORT, 'forced manual finish failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sent、runErr 保存外部发货数量和结果收口错误。
	sent, runErr := center.ManualFullDelivery(ctx, order)
	if !errors.Is(runErr, errAutomationNeedsReview) {
		t.Fatalf("FinishRun 失败应转人工核对: sent=%d err=%v", sent, runErr)
	}
	if sent != 1 || len(sender.texts) != 1 || mtopMock.consignCalls != 1 {
		t.Fatalf("外部动作应只执行一次: sent=%d texts=%v consign=%d", sent, sender.texts, mtopMock.consignCalls)
	}
	// status、message 保存手动发货隔离后的状态和原因。
	var status, message string
	// queryErr 保存读取手动发货运行终态的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,error_message FROM automation_runs WHERE order_id=?`, order.OrderID).Scan(&status, &message); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" || !strings.Contains(message, "完整发货外部动作可能已执行") {
		t.Fatalf("手动发货未进入人工核对: status=%q message=%q", status, message)
	}
}

// TestManualFullDeliveryQuarantineFailureIsReturned 验证手动发货结果和人工核对状态均无法落库时会返回双重错误。
func TestManualFullDeliveryQuarantineFailureIsReturned(t *testing.T) {
	// ctx、store、center、sender、mtopMock、order、cleanup 保存手动发货测试夹具。
	ctx, store, center, sender, mtopMock, order, cleanup := newManualDeliveryFixture(t, "manual-quarantine-failure-order")
	defer cleanup()
	// triggerErr 表示故意阻止 success 与 needs_review 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_manual_result_states
		BEFORE UPDATE OF status ON automation_runs
		WHEN NEW.status IN ('success','needs_review')
		BEGIN SELECT RAISE(ABORT, 'forced manual result-state failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// runErr 保存结果收口和人工核对收口均失败后的组合错误。
	_, runErr := center.ManualFullDelivery(ctx, order)
	if !errors.Is(runErr, errAutomationNeedsReview) || !strings.Contains(runErr.Error(), "保存完整发货人工核对状态") {
		t.Fatalf("双重落库失败应保留完整错误: %v", runErr)
	}
	if len(sender.texts) != 1 || mtopMock.consignCalls != 1 {
		t.Fatalf("双重落库失败不应重复外部动作: texts=%v consign=%d", sender.texts, mtopMock.consignCalls)
	}
}

// TestPrepareTaskUpsertFailureStopsBeforeExternalAction 验证自动化准备阶段订单事实写入失败时不会继续执行外部动作。
func TestPrepareTaskUpsertFailureStopsBeforeExternalAction(t *testing.T) {
	// ctx、store、center、sender、cleanup 保存准备阶段测试夹具。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存测试数据库上下文。
	ctx := context.Background()
	// triggerErr 表示故意阻止准备阶段订单占位写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_prepare_order_insert
		BEFORE INSERT ON orders
		BEGIN SELECT RAISE(ABORT, 'forced preparation order failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sender 记录准备阶段不应触发的外部消息。
	sender := &testSender{}
	// center 是只用于执行规则准备阶段的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 保存待准备的付款事件。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "prepare-failure-order", ChatID: "chat", BuyerID: "buyer"}
	// rule 保存会尝试发送消息的规则，验证准备失败会在动作前返回。
	rule := db.AutomationRule{ID: 1, CookieID: "cid", TriggerType: TriggerOrderPaid, Enabled: true, Actions: []db.AutomationAction{{ActionType: ActionSendText, MessageTemplate: "must-not-send", Enabled: true}}}
	// runErr 保存准备阶段订单事实写入失败后的错误。
	runErr := center.executeRule(ctx, task, rule)
	if runErr == nil || !strings.Contains(runErr.Error(), "保存自动化准备阶段订单事实") {
		t.Fatalf("准备阶段写入失败应返回明确错误: %v", runErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("准备阶段失败不应发送外部消息: %v", sender.texts)
	}
}

// recordingNotifier 记录所有按自动化运行幂等的通知调用，用于断言 automation.Center 接线。
type recordingNotifier struct {
	mu    sync.Mutex
	calls []struct {
		runID                                               int64
		contextCanceled                                     bool
		accountID, buyerID, itemID, status, message, chatID string
	}
}

// NotifyAutomationRun 记录自动化运行终态通知；测试替身只记录参数，不模拟持久化 outbox。
func (r *recordingNotifier) NotifyAutomationRun(ctx context.Context, runID int64, accountID, buyerID, itemID, status, message, chatID string) {
	// contextCanceled 记录入队上下文是否已被取消；正常运行收口必须在取消前持久化通知。
	contextCanceled := ctx.Err() != nil
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, struct {
		runID                                               int64
		contextCanceled                                     bool
		accountID, buyerID, itemID, status, message, chatID string
	}{runID, contextCanceled, accountID, buyerID, itemID, status, message, chatID})
}

// messages 封装消息列表业务协调。
func (r *recordingNotifier) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	// out 用于本次流程后续判断的out
	out := make([]string, len(r.calls))
	// i、c 表示当前遍历过程中的i、c
	for i, c := range r.calls {
		out[i] = c.message
	}
	return out
}

// hasCanceledContext 返回是否存在使用已取消上下文的自动化终态通知调用。
func (r *recordingNotifier) hasCanceledContext() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// call 表示当前遍历到的终态通知调用记录。
	for _, call := range r.calls {
		if call.contextCanceled {
			return true
		}
	}
	return false
}

// TestCenterNotifiesOnDeliverySuccess 验证规则执行成功（实际发出卡券）时触发成功通知。
func TestCenterNotifiesOnDeliverySuccess(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-n','N',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (61,'卡','text','CARD',1,?)`, admin.ID)
	// err 用于本次流程后续判断的err
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

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// notifier 用于本次流程后续判断的notifier
	notifier := &recordingNotifier{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{
			SpecName: "套餐", SpecValue: "标准", Quantity: "1", Amount: "9.9", OrderStatus: "pending_ship",
		}},
		Notifier: notifier,
	})

	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-n", OrderID: "order-n", ItemID: "item-n", BuyerID: "buyer-n", Raw: map[string]any{"mid": "m"},
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	// msgs 用于本次流程后续判断的msgs
	msgs := notifier.messages()
	if len(msgs) != 1 {
		t.Fatalf("应发 1 条成功通知，got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "成功") || !strings.Contains(msgs[0], "order-n") {
		t.Fatalf("通知文案异常: %q", msgs[0])
	}
	if notifier.hasCanceledContext() {
		t.Fatal("运行收口通知不应使用已取消上下文，否则 outbox 无法持久化")
	}
}

// TestCenterNotifiesOnDeliveryFailure 验证无匹配规格动作导致失败时触发失败通知。
func TestCenterNotifiesOnDeliveryFailure(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-f','F',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (71,'卡','text','CARD',1,?)`, admin.ID)
	// err 用于本次流程后续判断的err
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

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// notifier 用于本次流程后续判断的notifier
	notifier := &recordingNotifier{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{
			SpecName: "套餐", SpecValue: "90天", Quantity: "1", Amount: "9.9", OrderStatus: "pending_ship",
		}},
		Notifier: notifier,
	})

	// HandleTask 对单条规则失败只记录日志不返回错误，但通知应已发出。
	_ = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-f", OrderID: "order-f", ItemID: "item-f", BuyerID: "buyer-f", Raw: map[string]any{"mid": "m"},
	})

	// msgs 用于本次流程后续判断的msgs
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
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()

	// notifier 用于本次流程后续判断的notifier
	notifier := &recordingNotifier{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{Notifier: notifier})

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
// TestDueReviewRequestOrdersHandlesNullSpecColumns 封装TestDueReview请求订单列表HandlesNullSpecColumns业务协调。
func TestDueReviewRequestOrdersHandlesNullSpecColumns(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
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

	// orders、err 用于本次流程后续判断的orders、err
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

// TestReviewRequestCounterFailureMovesCompletedActionToNeedsReview 封装TestReview请求CounterFailureMovesCompleted动作ToNeedsReview业务协调。
func TestReviewRequestCounterFailureMovesCompletedActionToNeedsReview(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "review-counter", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "请评价", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "review-counter-order", db.OrderUpsertOpts{CookieID: "cid", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_review_counter
		BEFORE UPDATE OF review_request_count ON orders
		WHEN NEW.review_request_count>OLD.review_request_count
		BEGIN SELECT RAISE(FAIL, 'forced review counter failure'); END`); err != nil {
		t.Fatal(err)
	}
	// rule、err 用于本次流程后续判断的rule、err
	rule, err := store.Automation.Get(ctx, ruleID)
	if err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
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
	// order、err 用于本次流程后续判断的order、err
	order, err := store.Orders.Get(ctx, "review-counter-order")
	if err != nil || order.ReviewRequestCount != 0 {
		t.Fatalf("counter=%d err=%v", order.ReviewRequestCount, err)
	}
	// status、message 用于本次流程后续判断的status、message
	var status, message string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status,error_message FROM automation_runs WHERE order_id=?`, "review-counter-order").Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != "needs_review" || !strings.Contains(message, "保存提醒次数失败") {
		t.Fatalf("status=%q message=%q", status, message)
	}
}

// TestCookieValueFallbackUsesSingleValueQuery 验证订单详情补全的 Cookie 回退不会读取登录密码等完整账号字段。
func TestCookieValueFallbackUsesSingleValueQuery(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "cookie-value-fallback-key")
	// store 是当前测试使用的 SQLite repository 聚合器。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"fallback-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// center 是待验证 Cookie 回退读取逻辑的自动化中心。
	center := New(store, nil, nil)
	// value 是单值查询返回的 Cookie 明文。
	value, valueErr := center.cookieValue(ctx, "cid")
	if valueErr != nil || value != "unb=123; _m_h5_tk=tk_1;" {
		t.Fatalf("cookieValue value=%q err=%v", value, valueErr)
	}
}
