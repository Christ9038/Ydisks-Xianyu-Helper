package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// scriptedOutboxRepository 为 outbox worker 测试提供可控的领取、确认和隔离结果。
type scriptedOutboxRepository struct {
	// channel 保存 worker 发送时返回的渠道配置。
	channel *db.NotificationChannel
	// pending 保存下一次 ClaimOutbox 应返回的消息；领取后立即清空以模拟持久化状态转移。
	pending []db.NotificationOutboxMessage
	// completeErr 保存本地完成确认故障，用于验证外部成功后的不确定隔离。
	completeErr error
	// uncertainResult 保存隔离调用是否成功。
	uncertainResult bool
	// uncertainErr 保存隔离状态写入故障。
	uncertainErr error
	// uncertainCalls 记录隔离调用次数。
	uncertainCalls int
	// retryCalls 记录发送失败重试调用次数。
	retryCalls int
	// retryResult 保存重试状态更新的布尔结果。
	retryResult bool
	// retryErr 保存重试状态更新故障。
	retryErr error
	// retryLastError 保存最近一次准备持久化的安全错误文本。
	retryLastError string
	// retryPermanent 保存最近一次重试是否达到永久失败上限。
	retryPermanent bool
	// retryNextAttemptAt 保存最近一次计算出的重试时间戳。
	retryNextAttemptAt int64
	// claimErr 保存领取 outbox 时的预置故障。
	claimErr error
	// channelErr 保存查询通知渠道时的预置故障。
	channelErr error
	// completeResult 保存完成确认的布尔结果。
	completeResult bool
}

// AccountChannels 返回测试不使用的账号渠道列表。
func (r *scriptedOutboxRepository) AccountChannels(context.Context, string) ([]db.NotificationChannel, error) {
	return nil, nil
}

// EnqueueOutbox 返回测试不使用的入队结果。
func (r *scriptedOutboxRepository) EnqueueOutbox(context.Context, []db.NotificationOutboxInput) error {
	return nil
}

// ClaimOutbox 返回一次可控的 outbox 消息并模拟领取后不再重复领取。
func (r *scriptedOutboxRepository) ClaimOutbox(context.Context, string, time.Time, int) ([]db.NotificationOutboxMessage, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	// messages 保存本次领取的消息，并在返回前清空替身队列以模拟一次性领取。
	messages := r.pending
	r.pending = nil
	return messages, nil
}

// GetChannel 返回 worker 发送所需的测试渠道。
func (r *scriptedOutboxRepository) GetChannel(context.Context, int64) (*db.NotificationChannel, error) {
	return r.channel, r.channelErr
}

// CompleteOutbox 返回预置的本地确认故障，模拟发送成功后的落库失败。
func (r *scriptedOutboxRepository) CompleteOutbox(context.Context, int64, string) (bool, error) {
	return r.completeResult, r.completeErr
}

// MarkOutboxUncertain 记录不确定隔离调用并返回预置结果。
func (r *scriptedOutboxRepository) MarkOutboxUncertain(context.Context, int64, string, string) (bool, error) {
	r.uncertainCalls++
	return r.uncertainResult, r.uncertainErr
}

// RetryOutbox 记录发送失败重试调用，便于断言发送成功不会进入重试。
func (r *scriptedOutboxRepository) RetryOutbox(_ context.Context, _ int64, _ string, lastError string, nextAttemptAt int64, permanent bool) (bool, error) {
	r.retryCalls++
	r.retryLastError = lastError
	r.retryNextAttemptAt = nextAttemptAt
	r.retryPermanent = permanent
	return r.retryResult, r.retryErr
}

// GetSetting 返回测试不使用的系统设置值。
func (r *scriptedOutboxRepository) GetSetting(context.Context, string) (string, error) {
	return "", nil
}

// nilLogger 返回一个丢弃所有输出的 logger，用于不需要日志噪声的测试。
func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newNotifyStoreBare 提供一个独立 store，方便各测试用例自由构造数据。
// 预置一个 admin 用户和一个 cookie_id="cid" 的 cookie 记录以满足外键约束。
// newNotifyStoreBare 封装newNotifyStoreBare业务协调。
func newNotifyStoreBare(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// s 用于本次流程后续判断的s
	s := db.NewStore(d, db.DialectSQLite)
	s.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := s.Users.GetByUsername(context.Background(), "admin")
	s.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	return s, func() { _ = d.Close() }
}

// addWebhookChannel 插入一个 webhook 渠道并绑定到 cookieID，返回渠道 ID。
func addWebhookChannel(t *testing.T, s *db.Store, cookieID, name, webhookURL string) int64 {
	t.Helper()
	// res、err 用于本次流程后续判断的res、err
	res, err := s.DB.ExecContext(context.Background(),
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,1)`,
		name, "webhook", `{"webhook_url":"`+webhookURL+`"}`)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	// id 用于本次流程后续判断的标识
	id, _ := res.LastInsertId()
	if // err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(context.Background(),
		`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES (?,?,1)`,
		cookieID, id); err != nil {
		t.Fatalf("insert binding: %v", err)
	}
	return id
}

// TestNotifyAccountAlert_NoStore store 为 nil 时安全返回。
func TestNotifyAccountAlert_NoStore(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := &Notifier{logger: nilLogger()}
	// 不应 panic。
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNotifyAccountAlert_NoChannels 无绑定渠道时不报错。
func TestNotifyAccountAlert_NoChannels(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNotifyAccountAlert_WithChannel 告警通知发送并包含标题与正文。
func TestNotifyAccountAlert_WithChannel(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	// gotBody 用于本次流程后续判断的got请求体
	var gotBody string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "告警渠道", srv.URL)
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	n.NotifyAccountAlert("cid", "critical", "Token失效", "请重新登录")

	if gotBody == "" {
		t.Fatal("应发送告警通知")
	}
	if !strings.Contains(gotBody, "严重") || !strings.Contains(gotBody, "Token失效") || !strings.Contains(gotBody, "请重新登录") {
		t.Errorf("告警正文缺少关键字: %s", gotBody)
	}
}

// TestNotifyEventUsesPersistentOutboxWhenStarted 封装TestNotifyEventUsesPersistentOutboxWhenStarted业务协调。
func TestNotifyEventUsesPersistentOutboxWhenStarted(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// calls 用于本次流程后续判断的calls
	var calls int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	addWebhookChannel(t, s, "cid", "outbox", srv.URL)
	// n 用于本次流程后续判断的n
	n := New("cid", s, nilLogger())
	// 标记异步模式但不启动循环，精确验证调用返回时尚未发生外部网络请求。
	n.started.Store(true)
	n.NotifyAccountAlert("cid", "warn", "持久化通知", "正文")
	if // got 用于本次流程后续判断的got
	got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("business call performed synchronous network I/O: %d", got)
	}
	// queued 用于本次流程后续判断的queued
	var queued int
	if // err 用于本次流程后续判断的err
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE status='pending'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	n.drainOutbox(context.Background())
	if // got 用于本次流程后续判断的got
	got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("outbox delivery calls=%d", got)
	}
	if // err 用于本次流程后续判断的err
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("remaining=%d err=%v", queued, err)
	}
}

// TestDrainOutbox_SendSuccessCompletionFailureQuarantines 验证外部发送成功但本地确认失败时进入不确定隔离且不重发。
func TestDrainOutbox_SendSuccessCompletionFailureQuarantines(t *testing.T) {
	// calls 保存测试 HTTP 服务实际收到的请求次数。
	var calls int32
	// server 保存接收测试通知的本地 HTTP 服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	// repository 保存可控的 outbox 状态转换替身。
	repository := &scriptedOutboxRepository{
		channel:         &db.NotificationChannel{ID: 7, Type: "webhook", Config: `{"webhook_url":"` + server.URL + `"}`},
		pending:         []db.NotificationOutboxMessage{{ID: 11, ChannelID: 7, EventType: EventSystemError, Body: "正文", AttemptCount: 1}},
		completeErr:     errors.New("确认落库失败"),
		uncertainResult: true,
	}
	// notifier 保存使用替身 repository 的通知器。
	notifier := NewWithRepository("cid", repository, nilLogger())
	notifier.started.Store(true)
	notifier.drainOutbox(context.Background())
	notifier.drainOutbox(context.Background())
	if // got 保存外部服务收到的请求次数。
	got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("send count=%d want 1", got)
	}
	if repository.uncertainCalls != 1 {
		t.Fatalf("uncertain calls=%d want 1", repository.uncertainCalls)
	}
	if repository.retryCalls != 0 {
		t.Fatalf("retry calls=%d want 0", repository.retryCalls)
	}
}

// TestNotifyAccountAlert_LevelLabels 覆盖 levelLabel 各分支。
func TestNotifyAccountAlert_LevelLabels(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"critical": "严重",
		"warn":     "警告",
		"info":     "提示",
		"unknown":  "unknown",
	}
	// level、want 表示当前遍历过程中的level、want
	for level, want := range cases {
		if // got 用于本次流程后续判断的got
		got := levelLabel(level); got != want {
			t.Errorf("levelLabel(%q)=%q want %q", level, got, want)
		}
	}
}

// TestSendToChannel 直接发送到指定渠道。
func TestSendToChannel(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	// gotBody 用于本次流程后续判断的got请求体
	var gotBody string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// chID 用于本次流程后续判断的chID
	chID := addWebhookChannel(t, s, "cid", "直发渠道", srv.URL)
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	if // err 用于本次流程后续判断的err
	err := n.SendToChannel(chID, "直发测试"); err != nil {
		t.Fatalf("SendToChannel: %v", err)
	}
	if !strings.Contains(gotBody, "直发测试") {
		t.Errorf("正文缺失: %s", gotBody)
	}
}

// TestSendToChannel_Errors store 未初始化 / 渠道不存在。
func TestSendToChannel_Errors(t *testing.T) {
	// store 为 nil。
	n := &Notifier{logger: nilLogger()}
	if // err 用于本次流程后续判断的err
	err := n.SendToChannel(1, "x"); err == nil {
		t.Fatal("store 为 nil 应报错")
	}

	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// n2 用于本次流程后续判断的n2
	n2 := New("cid", s, nil)
	// 不存在的渠道 ID。
	if err := n2.SendToChannel(99999, "x"); err == nil {
		t.Fatal("渠道不存在应报错")
	}
}

// TestNotifyDelivery_MultiChannel 某渠道失败不影响其他渠道。
func TestNotifyDelivery_MultiChannel(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	// okCount 用于本次流程后续判断的ok数量
	var okCount int32
	// 成功渠道。
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		atomic.AddInt32(&okCount, 1)
		w.WriteHeader(200)
	}))
	defer okSrv.Close()
	// 失败渠道（始终 500）。
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(500)
	}))
	defer failSrv.Close()

	addWebhookChannel(t, s, "cid", "成功渠道", okSrv.URL)
	addWebhookChannel(t, s, "cid", "失败渠道", failSrv.URL)

	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家", "b1", "item1", "成功", "chat1")
	if // got 用于本次流程后续判断的got
	got := atomic.LoadInt32(&okCount); got != 1 {
		t.Errorf("成功渠道应收到 1 次，实际 %d", got)
	}
}

// TestNotifyDelivery_TemplateVars 通知正文应包含买家名、商品ID、结果等变量。
func TestNotifyDelivery_TemplateVars(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	// gotBody 用于本次流程后续判断的got请求体
	var gotBody string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "模板渠道", srv.URL)
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "张三", "BID123", "ITEM456", "发货成功", "CHAT789")

	// want 表示当前遍历过程中的want
	for _, want := range []string{"张三", "BID123", "ITEM456", "发货成功", "CHAT789"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("正文缺少 %q: %s", want, gotBody)
		}
	}
}

// TestNotifyDelivery_EmptyChatID chatID 为空时回退为“未知”。
func TestNotifyDelivery_EmptyChatID(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	// gotBody 用于本次流程后续判断的got请求体
	var gotBody string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "渠道", srv.URL)
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家", "b1", "item1", "结果", "")
	if !strings.Contains(gotBody, "未知") {
		t.Errorf("空 chatID 应回退为“未知”: %s", gotBody)
	}
}

// TestNotifyAutomationRunQueuesEachTerminalStateOnce 验证自动化运行通知即使 worker 尚未启动也写入
// 持久化 outbox，同一运行同一终态重复恢复只保留一次；不同终态保留各自独立的人工核对记录。
func TestNotifyAutomationRunQueuesEachTerminalStateOnce(t *testing.T) {
	// store、cleanup 保存本测试的 SQLite 通知存储与关闭责任。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// channelID 保存绑定给自动化账号的 webhook 渠道主键。
	channelID := addWebhookChannel(t, store, "cid", "自动化幂等渠道", "http://127.0.0.1:1")
	// notifier 保存未启动 worker 的真实 outbox 通知器；自动化终态仍不得降级为同步网络发送。
	notifier := New("cid", store, nil)
	// notifyCtx 约束自动化结果入队操作，不携带网络或账号凭证。
	notifyCtx := context.Background()
	notifier.NotifyAutomationRun(notifyCtx, 73, "cid", "buyer", "item", "success", "付款发货成功", "chat")
	notifier.NotifyAutomationRun(notifyCtx, 73, "cid", "buyer", "item", "success", "付款发货成功", "chat")
	notifier.NotifyAutomationRun(notifyCtx, 73, "cid", "buyer", "item", "needs_review", "付款发货需要人工核对", "chat")
	// rows 保存该渠道上同一自动化运行的 outbox 终态键和记录数量。
	rows, queryErr := store.DB.QueryContext(notifyCtx, `SELECT idempotency_key FROM notification_outbox WHERE channel_id=? ORDER BY idempotency_key`, channelID)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	defer rows.Close()
	// keys 保存已持久化的自动化运行终态键，用于断言重复 success 未产生第二条记录。
	keys := make([]string, 0, 2)
	for rows.Next() {
		// key 保存当前 outbox 行绑定的稳定自动化通知键。
		var key string
		// scanErr 保存读取当前 outbox 幂等键时的数据库错误。
		if scanErr := rows.Scan(&key); scanErr != nil {
			t.Fatal(scanErr)
		}
		keys = append(keys, key)
	}
	// rowsErr 保存遍历 outbox 查询结果结束时的数据库错误。
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatal(rowsErr)
	}
	if len(keys) != 2 || keys[0] != "automation-run:73:needs_review" || keys[1] != "automation-run:73:success" {
		t.Fatalf("outbox keys=%v", keys)
	}
}

// TestParseConfig_InvalidJSON 非法 JSON 走旧格式兼容分支。
func TestParseConfig_InvalidJSON(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := parseConfig("{not json")
	if m["config"] != "{not json" {
		t.Errorf("非法 JSON 应放入 config: %v", m)
	}
}

// TestStrOr 覆盖 string / 非 string / 缺失三个分支。
func TestStrOr(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := map[string]any{
		"s": "abc",
		"n": 42,
		"b": true,
	}
	if // got 用于本次流程后续判断的got
	got := strOr(m, "s", "x"); got != "abc" {
		t.Errorf("strOr(s)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := strOr(m, "n", "x"); got != "42" {
		t.Errorf("strOr(n)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := strOr(m, "b", "x"); got != "true" {
		t.Errorf("strOr(b)=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := strOr(m, "missing", "def"); got != "def" {
		t.Errorf("strOr(missing)=%q", got)
	}
}

// TestFallback fallback 空串与非空串。
func TestFallback(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := fallback("", "默认"); got != "默认" {
		t.Errorf("fallback('')=%q", got)
	}
	if // got 用于本次流程后续判断的got
	got := fallback("值", "默认"); got != "值" {
		t.Errorf("fallback('值')=%q", got)
	}
}

// TestNotifyAccountAlert_DBError 查询出错时不 panic（err != nil 分支）。
func TestNotifyAccountAlert_DBError(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	// d 用于本次流程后续判断的d
	d := s.DB
	cleanup() // 提前关闭 DB，使查询返回错误
	_ = d
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	// store 非 nil 但 DB 已关闭，查询会报错；不应 panic。
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNotifyDelivery_DBError 查询出错时不 panic（err != nil 分支）。
func TestNotifyDelivery_DBError(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	cleanup() // 提前关闭 DB
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家", "b1", "item1", "结果", "chat1")
}

// TestSendToChannel_DBError 查询渠道出错时返回包装错误。
func TestSendToChannel_DBError(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	cleanup() // 提前关闭 DB
	n := New("cid", s, nil)
	if // err 用于本次流程后续判断的err
	err := n.SendToChannel(1, "x"); err == nil {
		t.Fatal("DB 查询出错应返回 error")
	}
}

// TestNotifyAccountAlert_SendError 某渠道发送失败时记录错误但不中断。
func TestNotifyAccountAlert_SendError(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "失败渠道", srv.URL)
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	// 渠道返回 5xx，send 报错 → 走 logger.Error 分支，但不应 panic。
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNew_DefaultLogger logger 为 nil 时使用默认 logger。
func TestNew_DefaultLogger(t *testing.T) {
	// s、cleanup 用于本次流程后续判断的s、cleanup
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// n 用于本次流程后续判断的n
	n := New("cid", s, nil)
	if n == nil || n.logger == nil || n.httpc == nil {
		t.Fatal("New 未正确初始化字段")
	}
	if n.cookieID != "cid" {
		t.Errorf("cookieID=%q", n.cookieID)
	}
	if n.httpc.Timeout != 10*time.Second {
		t.Errorf("httpc timeout=%v", n.httpc.Timeout)
	}
}

// TestNotifierWorkerLifecycle 覆盖通知 worker 的启动、重复启动、运行循环和兼容等待入口。
func TestNotifierWorkerLifecycle(t *testing.T) {
	// repository 提供空 outbox，使 worker 能在取消后安全退出。
	repository := &scriptedOutboxRepository{}
	// notifier 保存本次生命周期测试使用的通知器。
	notifier := NewWithRepository("cid", repository, nilLogger())
	// ctx、cancel 为 worker 提供可控的退出边界。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	notifier.Start(ctx)
	notifier.Start(ctx)
	notifier.Wait()
	// err 表示 worker 已完成后的带上下文等待结果。
	if err := notifier.WaitContext(context.Background()); err != nil {
		t.Fatalf("WaitContext: %v", err)
	}
	// nilNotifier 覆盖兼容等待入口的空接收者分支。
	var nilNotifier *Notifier
	nilNotifier.Wait()
	// err 表示空接收者等待应直接返回的结果。
	if err := nilNotifier.WaitContext(context.Background()); err != nil {
		t.Fatalf("nil WaitContext: %v", err)
	}
}

// TestNotifierOutboxRetryBranches 覆盖 outbox 领取、渠道查询、发送失败和重试状态更新分支。
func TestNotifierOutboxRetryBranches(t *testing.T) {
	// repository 保存一个不存在渠道的消息，用于覆盖无效渠道清理路径。
	repository := &scriptedOutboxRepository{
		pending:        []db.NotificationOutboxMessage{{ID: 1, ChannelID: 2, AttemptCount: 1}},
		completeResult: true,
	}
	// notifier 保存使用替身 repository 的通知器。
	notifier := NewWithRepository("cid", repository, nilLogger())
	notifier.drainOutbox(context.Background())

	// repository.pending 保存渠道查询失败的消息，随后验证失败会进入重试。
	repository.pending = []db.NotificationOutboxMessage{{ID: 2, ChannelID: 3, AttemptCount: 1}}
	repository.channelErr = errors.New("渠道查询失败")
	repository.retryResult = true
	notifier.drainOutbox(context.Background())
	if repository.retryCalls != 1 || repository.retryPermanent {
		t.Fatalf("retry calls=%d permanent=%v", repository.retryCalls, repository.retryPermanent)
	}

	// repository.pending 保存发送失败消息，使用不可用 webhook 覆盖发送错误重试。
	repository.channelErr = nil
	// webhookSecret 是故意放在 URL 路径中的模拟渠道秘密，必须在 outbox 持久化前被移除。
	const webhookSecret = "review-webhook-secret"
	repository.channel = &db.NotificationChannel{ID: 4, Type: "webhook", Config: `{"webhook_url":"http://127.0.0.1:1/` + webhookSecret + `"}`}
	repository.pending = []db.NotificationOutboxMessage{{ID: 4, ChannelID: 4, Body: "body", AttemptCount: 10}}
	repository.retryErr = errors.New("重试状态写入失败")
	notifier.drainOutbox(context.Background())
	if repository.retryCalls != 2 || !repository.retryPermanent {
		t.Fatalf("permanent retry calls=%d permanent=%v", repository.retryCalls, repository.retryPermanent)
	}
	if strings.Contains(repository.retryLastError, webhookSecret) || !strings.Contains(repository.retryLastError, "http://127.0.0.1:1/<redacted>") {
		t.Fatalf("outbox 重试错误未安全持久化: %s", repository.retryLastError)
	}

	// repository.pending 保存领取故障，覆盖 ClaimOutbox 错误返回路径。
	repository.pending = nil
	repository.retryErr = nil
	repository.claimErr = errors.New("领取失败")
	notifier.drainOutbox(context.Background())
}

// TestNotifierRepositoryOutboxDelegates 覆盖通知 repository 对不确定隔离和重试方法的委托。
func TestNotifierRepositoryOutboxDelegates(t *testing.T) {
	// store、cleanup 提供真实 SQLite repository，确保委托方法经过数据库实现。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// repository 保存通知 store 的窄适配器。
	repository := newStoreRepository("cid", store)
	// uncertain、uncertainErr 保存不存在消息的隔离结果。
	uncertain, uncertainErr := repository.MarkOutboxUncertain(context.Background(), 999, "worker", "失败")
	if uncertainErr != nil || uncertain {
		t.Fatalf("uncertain=%v err=%v", uncertain, uncertainErr)
	}
	// retried、retryErr 保存不存在消息的重试结果。
	retried, retryErr := repository.RetryOutbox(context.Background(), 999, "worker", "失败", time.Now().Unix(), false)
	if retryErr != nil || retried {
		t.Fatalf("retried=%v err=%v", retried, retryErr)
	}
}
