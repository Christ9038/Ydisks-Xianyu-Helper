package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// readinessTestSender 用于本次流程后续判断的readinessTestSender
type readinessTestSender struct {
	*testSender
	ready bool
}

// AutomationReady 封装自动化Ready业务协调。
func (s *readinessTestSender) AutomationReady() bool { return s.ready }

// readinessTestProvider 用于本次流程后续判断的readinessTestProvider
type readinessTestProvider struct{ sender MessageSender }

// Sender 封装Sender业务协调。
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

// TestSchedulerQuarantineSuccess 验证恢复任务成功隔离时返回 nil，并把运行状态写成 needs_review。
func TestSchedulerQuarantineSuccess(t *testing.T) {
	// t 提供测试失败定位、临时目录和清理注册能力。
	_ = t
	// store 是本测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是恢复任务共用的数据库上下文。
	ctx := context.Background()
	// runID 是已执行外部动作且租约过期的运行主键。
	runID := seedExpiredRecoveryRun(t, store, ctx, "success")
	// scheduler 负责扫描并隔离过期恢复运行。
	scheduler := &Scheduler{center: New(store, testSenderProvider{sender: &testSender{}}, nil)}
	// runErr 保存本轮状态收口结果，成功隔离时不应产生错误。
	runErr := scheduler.runRecoveryTasks(ctx)
	if runErr != nil {
		t.Fatalf("隔离成功不应返回错误: %v", runErr)
	}
	// run 保存隔离后的运行记录，用于确认人工核对状态已落库。
	run, getErr := store.Automation.GetRun(ctx, runID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if run.Status != "needs_review" {
		t.Fatalf("status=%q want needs_review", run.Status)
	}
}

// TestSchedulerQuarantineWriteFailureUsesJoinedNeedsReviewError 验证隔离写失败不会被吞掉，并保留 needs_review 与 quarantine 错误哨兵。
func TestSchedulerQuarantineWriteFailureUsesJoinedNeedsReviewError(t *testing.T) {
	// t 提供测试失败定位、临时目录和清理注册能力。
	_ = t
	// store 是本测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是恢复任务共用的数据库上下文。
	ctx := context.Background()
	// runID 确保数据库中存在一条待隔离的过期运行。
	_ = seedExpiredRecoveryRun(t, store, ctx, "write-failure")
	// triggerErr 表示阻止人工核对状态写入的数据库触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_scheduler_quarantine
		BEFORE UPDATE OF status ON automation_runs
		WHEN NEW.status='needs_review'
		BEGIN SELECT RAISE(ABORT, 'forced scheduler quarantine failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// scheduler 负责扫描并返回隔离写失败。
	scheduler := &Scheduler{center: New(store, testSenderProvider{sender: &testSender{}}, nil)}
	// runErr 保存统一收口错误，必须同时包含人工核对和隔离失败哨兵。
	runErr := scheduler.runRecoveryTasks(ctx)
	if runErr == nil || !errors.Is(runErr, errAutomationNeedsReview) || !errors.Is(runErr, errAutomationQuarantine) {
		t.Fatalf("runErr=%v want joined needs_review/quarantine error", runErr)
	}
	if !strings.Contains(runErr.Error(), "保存自动化恢复运行人工核对状态失败") {
		t.Fatalf("runErr=%v 缺少隔离写失败上下文", runErr)
	}
}

// TestSchedulerDeferredFinishWriteFailureUsesNeedsReviewError 验证非法延迟任务的最终状态写失败会返回人工核对错误。
func TestSchedulerDeferredFinishWriteFailureUsesNeedsReviewError(t *testing.T) {
	// t 提供测试失败定位、临时目录和清理注册能力。
	_ = t
	// store 是本测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是延迟任务共用的数据库上下文。
	ctx := context.Background()
	// insertErr 表示写入故意非法延迟任务时的数据库错误。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO automation_pending_tasks
		(task_key,cookie_id,trigger_type,task_json,due_at,status,attempt_count,lease_expires_at,error_message)
		VALUES ('cid:scheduler-bad','cid',?,'{"broken',0,'pending',0,0,'')`, TriggerBuyerReviewed); insertErr != nil {
		t.Fatal(insertErr)
	}
	// triggerErr 表示阻止非法任务进入重试或死信状态的数据库触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_scheduler_deferred_finish
		BEFORE UPDATE OF status ON automation_pending_tasks
		WHEN NEW.status IN ('pending','dead_letter')
		BEGIN SELECT RAISE(ABORT, 'forced scheduler deferred finish failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// scheduler 负责扫描并返回延迟任务状态写失败。
	scheduler := &Scheduler{center: New(store, testSenderProvider{sender: &testSender{}}, nil)}
	// runErr 保存统一收口错误，避免状态写失败被当作已处理。
	runErr := scheduler.runDeferredTasks(ctx)
	if runErr == nil || !errors.Is(runErr, errAutomationNeedsReview) {
		t.Fatalf("runErr=%v want needs_review error", runErr)
	}
	if !strings.Contains(runErr.Error(), "保存解析失败的暂停事件状态失败") {
		t.Fatalf("runErr=%v 缺少延迟任务状态写失败上下文", runErr)
	}
}

// seedExpiredRecoveryRun 创建一条外部动作已开始且租约已过期的恢复运行，供调度器状态收口测试复用。
func seedExpiredRecoveryRun(t *testing.T, store *db.Store, ctx context.Context, suffix string) int64 {
	// t 提供测试失败定位和临时资源辅助能力。
	_ = t
	// store 是待写入恢复运行的自动化数据库存储。
	_ = store
	// ctx 约束本次恢复运行构造过程中的数据库操作。
	_ = ctx
	// suffix 区分同一测试数据库内的运行规则和触发键，避免测试数据冲突。
	_ = suffix
	t.Helper()
	// admin 是创建自动化规则所需的管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// ruleID 是测试恢复运行引用的自动化规则主键。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "scheduler-" + suffix, TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "must-not-repeat", Enabled: true}},
	})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// task 是恢复运行中保存的无敏感事件快照。
	task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "scheduler-" + suffix, ChatID: "chat", BuyerID: "buyer"}
	// raw 保存可被恢复逻辑解析的任务 JSON。
	raw, marshalErr := json.Marshal(task)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	// runID 是新建自动化运行的数据库主键。
	runID, started, startErr := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "cid", OrderID: task.OrderID, TriggerType: task.TriggerType,
		TriggerKey: "scheduler:" + suffix, RawEventJSON: string(raw), LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if startErr != nil || !started {
		t.Fatalf("start=%v err=%v", started, startErr)
	}
	// actionStarted 表示恢复运行已进入外部动作检查点。
	actionStarted, actionErr := store.Automation.StartRunAction(ctx, runID, 1, 0, time.Now().Add(-time.Minute).Unix())
	if actionErr != nil || !actionStarted {
		t.Fatalf("start action=%v err=%v", actionStarted, actionErr)
	}
	// updateErr 表示把租约置为过期以便调度器选中运行时的数据库错误。
	if _, updateErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, runID); updateErr != nil {
		t.Fatal(updateErr)
	}
	return runID
}

// TestIntFromAny float64/int/string 三类来源 + 无效类型返回 0。
func TestIntFromAny(t *testing.T) {
	// cases 用于本次流程后续判断的cases
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
		if // got 用于本次流程后续判断的got
		got := intFromAny(c.in); got != c.want {
			t.Errorf("intFromAny(%v)=%d want %d", c.in, got, c.want)
		}
	}
}

// TestParseDBTime 支持的三种格式 + 无效返回零值。
func TestParseDBTime(t *testing.T) {
	if // t1 用于本次流程后续判断的t1
	t1 := parseDBTime("2026-01-02 15:04:05"); t1.IsZero() {
		t.Error("datetime 格式应解析成功")
	}
	if // t1 用于本次流程后续判断的t1
	t1 := parseDBTime("2026-01-02T15:04:05Z"); t1.IsZero() {
		t.Error("RFC3339 格式应解析成功")
	}
	if // t1 用于本次流程后续判断的t1
	t1 := parseDBTime(""); !t1.IsZero() {
		t.Error("空串应返回零值")
	}
	if // t1 用于本次流程后续判断的t1
	t1 := parseDBTime("not a time"); !t1.IsZero() {
		t.Error("非法串应返回零值")
	}
}

// TestReviewRequestRuleDue 综合判定：达到时长且未超次数 → due；否则不 due。
func TestReviewRequestRuleDue(t *testing.T) {
	// rule 用于本次流程后续判断的规则
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

// TestReviewRequestRuleDueUsesRepeatIntervalAfterFirstAttempt 封装TestReview请求规则DueUsesRepeatIntervalAfterFirst尝试次数业务协调。
func TestReviewRequestRuleDueUsesRepeatIntervalAfterFirstAttempt(t *testing.T) {
	// rule 用于本次流程后续判断的规则
	rule := db.AutomationRule{ConfigJSON: `{"first_delay_hours":1,"repeat_interval_hours":24,"max_attempts":3}`}
	// order 用于本次流程后续判断的订单
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

// TestParseDBTimeAcceptsPostgresTimestampText 封装TestParseDB时间AcceptsPostgresTimestamp文本业务协调。
func TestParseDBTimeAcceptsPostgresTimestampText(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := parseDBTime("2026-07-27 03:36:29.123456+00")
	if got.IsZero() {
		t.Fatal("Postgres CURRENT_TIMESTAMP 文本不应解析为零值")
	}
	// want 用于本次流程后续判断的want
	want := time.Date(2026, 7, 27, 3, 36, 29, 123456000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseDBTime=%s want %s", got, want)
	}
}

// TestFirstNonEmpty 返回首个非空串。
func TestFirstNonEmpty(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty=%q want x", got)
	}
	if // got 用于本次流程后续判断的got
	got := firstNonEmpty(); got != "" {
		t.Errorf("无参应返回空，got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := firstNonEmpty("a"); got != "a" {
		t.Errorf("单参=%q want a", got)
	}
}

// TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts 端到端验证调度扫描：
// 首次扫描命中到期订单 → 执行规则 → 发送文本 + 计数 +1；
// 二次扫描因达到 max_attempts 跳过，不再发送。
// TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts 封装TestSchedulerScanExecutesDueThenSkipsOnMax尝试次数业务协调。
func TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts(t *testing.T) {
	// database、err 用于本次流程后续判断的database、err
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, db.DialectSQLite)
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
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
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id, cookie_id, item_id, buyer_id, chat_id, system_shipped, shipped_at, review_request_count)
		VALUES ('o-sched', 'cid', 'item-1', 'buyer-1', 'chat-1', 1, ?, 0)`, shipped); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := New(store, testSenderProvider{sender: sender}, nil)
	// sched 用于本次流程后续判断的sched
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

// TestSchedulerWaitsForWebSocketBeforeCreatingRun 封装TestSchedulerWaitsForWebSocketBeforeCreating运行业务协调。
func TestSchedulerWaitsForWebSocketBeforeCreatingRun(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-ready", Name: "wait-ws",
		TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions:    []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// shipped 用于本次流程后续判断的shipped
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,cookie_id,item_id,buyer_id,chat_id,system_shipped,shipped_at)
		VALUES ('wait-ws-order','cid','item-ready','buyer','chat',1,?)`, shipped); err != nil {
		t.Fatal(err)
	}
	// sender 用于本次流程后续判断的sender
	sender := &readinessTestSender{testSender: &testSender{}, ready: false}
	// scheduler 用于本次流程后续判断的scheduler
	scheduler := NewScheduler(New(store, readinessTestProvider{sender: sender}, nil))
	scheduler.scan(ctx)
	// count 用于本次流程后续判断的数量
	var count int
	if // err 用于本次流程后续判断的err
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

// TestSchedulerScansMoreThanOneReviewPage 封装TestSchedulerScansMoreThanOneReview页码业务协调。
func TestSchedulerScansMoreThanOneReviewPage(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "review-all", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions:    []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// shipped 用于本次流程后续判断的shipped
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	for // i 用于本次流程后续判断的i
	i := 0; i < 205; i++ {
		if // err 用于本次流程后续判断的err
		_, err := store.DB.ExecContext(ctx, `INSERT INTO orders
			(order_id,cookie_id,buyer_id,chat_id,system_shipped,shipped_at,review_request_count,updated_at)
			VALUES (?,?,?,?,1,?,0,?)`, fmt.Sprintf("review-%03d", i), "cid", "buyer", fmt.Sprintf("chat-%03d", i), shipped, shipped); err != nil {
			t.Fatal(err)
		}
	}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	NewScheduler(New(store, testSenderProvider{sender: sender}, nil)).scan(ctx)
	if len(sender.texts) != 205 {
		t.Fatalf("sent=%d want 205", len(sender.texts))
	}
}

// TestRecoveryNeedsSenderUsesNextActionType 封装TestRecoveryNeedsSenderUsesNext动作类型业务协调。
func TestRecoveryNeedsSenderUsesNextActionType(t *testing.T) {
	// rule 用于本次流程后续判断的规则
	rule := db.AutomationRule{Actions: []db.AutomationAction{
		{ActionType: ActionConfirmShipment, Enabled: true},
		{ActionType: ActionSendText, Enabled: true},
	}}
	// task 用于本次流程后续判断的任务
	task := Task{TriggerType: TriggerBuyerReviewed}
	if recoveryNeedsSender(task, rule, 0) {
		t.Fatal("确认发货动作不应等待 WebSocket")
	}
	if !recoveryNeedsSender(task, rule, 1) {
		t.Fatal("发送文本动作必须等待 WebSocket")
	}
}

// TestAutomationSchedulerWaitsForShutdown 封装Test自动化SchedulerWaitsForShutdown业务协调。
func TestAutomationSchedulerWaitsForShutdown(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// scheduler 用于本次流程后续判断的scheduler
	scheduler := NewScheduler(New(store, testSenderProvider{sender: &testSender{}}, nil))
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
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

// TestAutomationSchedulerRunRejectsNilContext 验证 nil Context 不会启动不可取消的调度 goroutine。
func TestAutomationSchedulerRunRejectsNilContext(t *testing.T) {
	// scheduler 保存具备最小存储占位的调度器测试替身，确保 nil 检查先于业务扫描。
	scheduler := &Scheduler{center: &Center{store: &db.Store{}}, done: make(chan struct{})}
	scheduler.Run(nil)
	select {
	case <-scheduler.done:
		t.Fatal("nil Context 不应启动并完成调度器 worker")
	default:
	}
}
