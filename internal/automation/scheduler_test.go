package automation

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

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
	for _, c := range cases {
		if got := intFromAny(c.in); got != c.want {
			t.Errorf("intFromAny(%v)=%d want %d", c.in, got, c.want)
		}
	}
}

// TestParseDBTime 支持的三种格式 + 无效返回零值。
func TestParseDBTime(t *testing.T) {
	if t1 := parseDBTime("2026-01-02 15:04:05"); t1.IsZero() {
		t.Error("datetime 格式应解析成功")
	}
	if t1 := parseDBTime("2026-01-02T15:04:05Z"); t1.IsZero() {
		t.Error("RFC3339 格式应解析成功")
	}
	if t1 := parseDBTime(""); !t1.IsZero() {
		t.Error("空串应返回零值")
	}
	if t1 := parseDBTime("not a time"); !t1.IsZero() {
		t.Error("非法串应返回零值")
	}
}

// TestReviewRequestRuleDue 综合判定：达到时长且未超次数 → due；否则不 due。
func TestReviewRequestRuleDue(t *testing.T) {
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

func TestReviewRequestRuleDueUsesRepeatIntervalAfterFirstAttempt(t *testing.T) {
	rule := db.AutomationRule{ConfigJSON: `{"first_delay_hours":1,"repeat_interval_hours":24,"max_attempts":3}`}
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

func TestParseDBTimeAcceptsPostgresTimestampText(t *testing.T) {
	got := parseDBTime("2026-07-27 03:36:29.123456+00")
	if got.IsZero() {
		t.Fatal("Postgres CURRENT_TIMESTAMP 文本不应解析为零值")
	}
	want := time.Date(2026, 7, 27, 3, 36, 29, 123456000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseDBTime=%s want %s", got, want)
	}
}

// TestFirstNonEmpty 返回首个非空串。
func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty=%q want x", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("无参应返回空，got %q", got)
	}
	if got := firstNonEmpty("a"); got != "a" {
		t.Errorf("单参=%q want a", got)
	}
}

// TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts 端到端验证调度扫描：
// 首次扫描命中到期订单 → 执行规则 → 发送文本 + 计数 +1；
// 二次扫描因达到 max_attempts 跳过，不再发送。
func TestSchedulerScanExecutesDueThenSkipsOnMaxAttempts(t *testing.T) {
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	store := db.NewStore(database, db.DialectSQLite)
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
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
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id, cookie_id, item_id, buyer_id, chat_id, system_shipped, shipped_at, review_request_count)
		VALUES ('o-sched', 'cid', 'item-1', 'buyer-1', 'chat-1', 1, ?, 0)`, shipped); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
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

func TestSchedulerScansMoreThanOneReviewPage(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "review-all", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions:    []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "review", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	for i := 0; i < 205; i++ {
		if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
			(order_id,cookie_id,buyer_id,chat_id,system_shipped,shipped_at,review_request_count,updated_at)
			VALUES (?,?,?,?,1,?,0,?)`, fmt.Sprintf("review-%03d", i), "cid", "buyer", fmt.Sprintf("chat-%03d", i), shipped, shipped); err != nil {
			t.Fatal(err)
		}
	}
	sender := &testSender{}
	NewScheduler(New(store, testSenderProvider{sender: sender}, nil)).scan(ctx)
	if len(sender.texts) != 205 {
		t.Fatalf("sent=%d want 205", len(sender.texts))
	}
}
