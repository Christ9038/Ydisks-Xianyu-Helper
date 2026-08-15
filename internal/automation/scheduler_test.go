package automation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// readinessTestSender 保存readinessTestSender，供当前处理流程使用
type readinessTestSender struct {
	*testSender
	ready bool
}

// AutomationReady 负责自动化Ready相关处理。
func (s *readinessTestSender) AutomationReady() bool { return s.ready }

// readinessTestProvider 保存readinessTestProvider，供当前处理流程使用
type readinessTestProvider struct{ sender MessageSender }

// Sender 负责Sender相关处理。
func (p readinessTestProvider) Sender(string) (MessageSender, bool) { return p.sender, true }

// TestParseReviewRuleConfig 默认值 + JSON 覆盖 + 非法输入兜底。
func TestParseReviewRuleConfig(t *testing.T) {
	// 空配置 → 默认 72h / 1 次。
	cfg := parseReviewRuleConfig("")
	if cfg.AfterShippedHours != 72 || cfg.MaxAttempts != 1 {
		t.Fatalf("默认值: %+v", cfg)
	}
	// 合法 JSON 覆盖。
	cfg = parseReviewRuleConfig(`{"after_shipped_hours":48,"max_attempts":3}`)
	if cfg.AfterShippedHours != 48 || cfg.MaxAttempts != 3 {
		t.Fatalf("JSON 覆盖: %+v", cfg)
	}
	// 非法 JSON → 默认。
	cfg = parseReviewRuleConfig("not json")
	if cfg.AfterShippedHours != 72 {
		t.Fatalf("非法 JSON 应兜底默认: %+v", cfg)
	}
	// 0 或负值应被忽略（保留默认）。
	cfg = parseReviewRuleConfig(`{"after_shipped_hours":0,"max_attempts":-1}`)
	if cfg.AfterShippedHours != 72 || cfg.MaxAttempts != 1 {
		t.Fatalf("非正值应忽略: %+v", cfg)
	}
}

// TestIntFromAny float64/int/string 三类来源 + 无效类型返回 0。
func TestIntFromAny(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		in   any
		want int
	}{
		{float64(42), 42},
		{int(7), 7},
		{"15", 15},
		{"  20 ", 20},
		{"abc", 0},
		{nil, 0},
		{true, 0},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		if // got 保存got，供当前处理流程使用
		got := intFromAny(c.in); got != c.want {
			t.Errorf("intFromAny(%v)=%d want %d", c.in, got, c.want)
		}
	}
}

// TestParseDBTime 支持的三种格式 + 无效返回零值。
func TestParseDBTime(t *testing.T) {
	if // t1 保存t1，供当前处理流程使用
	t1 := parseDBTime("2026-01-02 15:04:05"); t1.IsZero() {
		t.Error("datetime 格式应解析成功")
	}
	if // t1 保存t1，供当前处理流程使用
	t1 := parseDBTime("2026-01-02T15:04:05Z"); t1.IsZero() {
		t.Error("RFC3339 格式应解析成功")
	}
	if // t1 保存t1，供当前处理流程使用
	t1 := parseDBTime(""); !t1.IsZero() {
		t.Error("空串应返回零值")
	}
	if // t1 保存t1，供当前处理流程使用
	t1 := parseDBTime("not a time"); !t1.IsZero() {
		t.Error("非法串应返回零值")
	}
}

// TestReviewRequestRuleDue 综合判定：达到时长且未超次数 → due；否则不 due。
func TestReviewRequestRuleDue(t *testing.T) {
	// rule 保存规则，供当前处理流程使用
	rule := db.AutomationRule{ConfigJSON: `{"after_shipped_hours":1,"max_attempts":2}`}

	// 已发货 2 小时、未请求过 → due。
	order := db.Order{
		OrderID:            "o1",
		SystemShipped:      true,
		ShippedAt:          time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05"),
		ReviewRequestCount: 0,
	}
	if !reviewRequestRuleDue(order, rule) {
		t.Error("发货满 2h、未请求应 due")
	}

	// 发货不到 1 小时 → 不 due。
	order.ShippedAt = time.Now().UTC().Add(-30 * time.Minute).Format("2006-01-02 15:04:05")
	if reviewRequestRuleDue(order, rule) {
		t.Error("发货仅 30min 不应 due")
	}

	// 已达最大次数 → 不 due。
	order.ShippedAt = time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	order.ReviewRequestCount = 2
	if reviewRequestRuleDue(order, rule) {
		t.Error("达到 max_attempts 不应 due")
	}

	// 无任何时间字段 → 不 due。
	order2 := db.Order{OrderID: "o2", SystemShipped: true, ReviewRequestCount: 0}
	if reviewRequestRuleDue(order2, rule) {
		t.Error("无时间基点不应 due")
	}

	// 缺 shipped_at 时回退到 updated_at。
	order3 := db.Order{
		OrderID:   "o3",
		UpdatedAt: time.Now().UTC().Add(-3 * time.Hour).Format("2006-01-02 15:04:05"),
	}
	if !reviewRequestRuleDue(order3, rule) {
		t.Error("缺 shipped_at 应回退 updated_at 判定 due")
	}
}

// TestReviewRequestRuleDueUsesRepeatIntervalAfterFirstAttempt 负责TestReview请求规则DueUsesRepeatIntervalAfterFirst尝试次数相关处理。
func TestReviewRequestRuleDueUsesRepeatIntervalAfterFirstAttempt(t *testing.T) {
	// rule 保存规则，供当前处理流程使用
	rule := db.AutomationRule{ConfigJSON: `{"first_delay_hours":1,"repeat_interval_hours":24,"max_attempts":3}`}
	// order 保存订单，供当前处理流程使用
	order := db.Order{
		OrderID:             "repeat-review",
		SystemShipped:       true,
		ShippedAt:           time.Now().UTC().Add(-72 * time.Hour).Format("2006-01-02 15:04:05"),
		ReviewRequestCount:  1,
		LastReviewRequestAt: time.Now().UTC().Add(-23 * time.Hour).Format("2006-01-02 15:04:05"),
	}
	if reviewRequestRuleDue(order, rule) {
		t.Fatal("repeat request must wait from last_review_request_at, not shipped_at")
	}
	order.LastReviewRequestAt = time.Now().UTC().Add(-25 * time.Hour).Format("2006-01-02 15:04:05")
	if !reviewRequestRuleDue(order, rule) {
		t.Fatal("repeat request should be due after repeat_interval_hours")
	}
}

// TestParseDBTimeAcceptsPostgresTimestampText 负责TestParseDB时间AcceptsPostgresTimestamp文本相关处理。
func TestParseDBTimeAcceptsPostgresTimestampText(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := parseDBTime("2026-07-27 03:36:29.123456+00")
	if got.IsZero() {
		t.Fatal("Postgres CURRENT_TIMESTAMP 文本不应解析为零值")
	}
	// want 保存want，供当前处理流程使用
	want := time.Date(2026, 7, 27, 3, 36, 29, 123456000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseDBTime=%s want %s", got, want)
	}
}

// TestFirstNonEmpty 返回首个非空串。
func TestFirstNonEmpty(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty=%q want x", got)
	}
	if // got 保存got，供当前处理流程使用
	got := firstNonEmpty(); got != "" {
		t.Errorf("无参应返回空，got %q", got)
	}
	if // got 保存got，供当前处理流程使用
	got := firstNonEmpty("a"); got != "a" {
		t.Errorf("单参=%q want a", got)
	}
}

// TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts 端到端验证调度扫描：
// 首次扫描命中到期订单 → 执行规则 → 发送文本 + 计数 +1；
// 二次扫描因达到 max_attempts 跳过，不再发送。
// TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts 负责TestSchedulerScanExecutesDueThenSkipsOnMax尝试次数相关处理。
func TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts(t *testing.T) {
	// database、err 保存database、err，供当前处理流程使用
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 保存store，供当前处理流程使用
	store := db.NewStore(database, db.DialectSQLite)
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk;", admin.ID)

	// 求评价规则：发货满 1 小时即到期，最多 1 次。
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID:      admin.ID,
		CookieID:    "cid",
		ItemID:      "item-1",
		Name:        "求评价",
		TriggerType: TriggerReviewMissingTimeout,
		Enabled:     true,
		Priority:    100,
		ConfigJSON:  `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions: []db.AutomationActionInput{{
			ActionType:      ActionSendText,
			MessageTemplate: "亲，记得来评价哦",
			Enabled:         true,
		}},
	})
	if err != nil || ruleID == 0 {
		t.Fatalf("create rule: %v", err)
	}

	// 已发货、未评价、有 chat_id 的订单。
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id, cookie_id, item_id, buyer_id, chat_id, system_shipped, shipped_at, review_request_count)
		VALUES ('o-sched', 'cid', 'item-1', 'buyer-1', 'chat-1', 1, ?, 0)`, shipped); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	// sender 保存sender，供当前处理流程使用
	sender := &testSender{}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: sender}, nil)
	// sched 保存sched，供当前处理流程使用
	sched := NewScheduler(center)
	// 缩短间隔不影响单次 scan 调用，但避免 Run 阻塞。
	_ = sched

	// 首次扫描：应执行规则，发送一条文本。
	sched.scan(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "亲，记得来评价哦" {
		t.Fatalf("首次扫描应发送一条文本，got %v", sender.texts)
	}
	// 计数应 +1。
	order, _ := store.Orders.Get(ctx, "o-sched")
	if order.ReviewRequestCount != 1 {
		t.Fatalf("ReviewRequestCount=%d want 1", order.ReviewRequestCount)
	}

	// 二次扫描：达到 max_attempts=1，应跳过，不再发送。
	sender.texts = nil
	sched.scan(ctx)
	if len(sender.texts) != 0 {
		t.Fatalf("达到 max_attempts 不应再发送，got %v", sender.texts)
	}
}

// TestSchedulerWaitsForWebSocketBeforeCreatingRun 负责TestSchedulerWaitsForWebSocketBeforeCreating运行相关处理。
func TestSchedulerWaitsForWebSocketBeforeCreatingRun(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、err 保存规则ID、err，供当前处理流程使用
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-ready", Name: "wait-ws",
		TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions:    []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// shipped 保存shipped，供当前处理流程使用
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,cookie_id,item_id,buyer_id,chat_id,system_shipped,shipped_at)
		VALUES ('wait-ws-order','cid','item-ready','buyer','chat',1,?)`, shipped); err != nil {
		t.Fatal(err)
	}
	// sender 保存sender，供当前处理流程使用
	sender := &readinessTestSender{testSender: &testSender{}, ready: false}
	// scheduler 保存scheduler，供当前处理流程使用
	scheduler := NewScheduler(New(store, readinessTestProvider{sender: sender}, nil))
	scheduler.scan(ctx)
	// count 保存数量，供当前处理流程使用
	var count int
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=?`, ruleID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("WS 未就绪时不应创建运行记录，got %d", count)
	}
	sender.ready = true
	scheduler.scan(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "review" {
		t.Fatalf("WS 就绪后应发送，got %v", sender.texts)
	}
}

// TestSchedulerScansMoreThanOneReviewPage 负责TestSchedulerScansMoreThanOneReview页码相关处理。
func TestSchedulerScansMoreThanOneReviewPage(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "review-all", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions:    []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// shipped 保存shipped，供当前处理流程使用
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	for // i 保存i，供当前处理流程使用
	i := 0; i < 205; i++ {
		if // err 保存err，供当前处理流程使用
		_, err := store.DB.ExecContext(ctx, `INSERT INTO orders
			(order_id,cookie_id,buyer_id,chat_id,system_shipped,shipped_at,review_request_count,updated_at)
			VALUES (?,?,?,?,1,?,0,?)`, fmt.Sprintf("review-%03d", i), "cid", "buyer", fmt.Sprintf("chat-%03d", i), shipped, shipped); err != nil {
			t.Fatal(err)
		}
	}
	// sender 保存sender，供当前处理流程使用
	sender := &testSender{}
	NewScheduler(New(store, testSenderProvider{sender: sender}, nil)).scan(ctx)
	if len(sender.texts) != 205 {
		t.Fatalf("sent=%d want 205", len(sender.texts))
	}
}

// TestRecoveryNeedsSenderUsesNextActionType 负责TestRecoveryNeedsSenderUsesNext动作类型相关处理。
func TestRecoveryNeedsSenderUsesNextActionType(t *testing.T) {
	// rule 保存规则，供当前处理流程使用
	rule := db.AutomationRule{Actions: []db.AutomationAction{
		{ActionType: ActionConfirmShipment, Enabled: true},
		{ActionType: ActionSendText, Enabled: true},
	}}
	// task 保存任务，供当前处理流程使用
	task := Task{TriggerType: TriggerBuyerReviewed}
	if recoveryNeedsSender(task, rule, 0) {
		t.Fatal("确认发货动作不应等待 WebSocket")
	}
	if !recoveryNeedsSender(task, rule, 1) {
		t.Fatal("发送文本动作必须等待 WebSocket")
	}
}

// TestAutomationSchedulerWaitsForShutdown 负责Test自动化SchedulerWaitsForShutdown相关处理。
func TestAutomationSchedulerWaitsForShutdown(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// scheduler 保存scheduler，供当前处理流程使用
	scheduler := NewScheduler(New(store, testSenderProvider{sender: &testSender{}}, nil))
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	// done 保存done，供当前处理流程使用
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()
	cancel()
	scheduler.Wait()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("自动化调度器关闭后没有退出")
	}
}

// TestAutomationSchedulerWaitContextHonorsDeadline 验证自动化调度器等待受关闭上下文限制。
func TestAutomationSchedulerWaitContextHonorsDeadline(t *testing.T) {
	// scheduler 保存尚未完成的调度器，以验证等待超时不会永久阻塞。
	scheduler := &Scheduler{done: make(chan struct{})}
	// ctx、cancel 保存短时关闭上下文及其释放函数。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	// err 表示尚未完成调度器在超时上下文下的等待结果。
	if err := scheduler.WaitContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitContext error=%v, want deadline exceeded", err)
	}
	close(scheduler.done)
	// err 表示已完成调度器的等待结果。
	if err := scheduler.WaitContext(context.Background()); err != nil {
		t.Fatalf("completed WaitContext error=%v", err)
	}
}
