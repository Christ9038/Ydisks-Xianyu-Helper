package notify

import (
	"context"
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

// nilLogger 返回一个丢弃所有输出的 logger，用于不需要日志噪声的测试。
func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newNotifyStoreBare 提供一个独立 store，方便各测试用例自由构造数据。
// 预置一个 admin 用户和一个 cookie_id="cid" 的 cookie 记录以满足外键约束。
func newNotifyStoreBare(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d, db.DialectSQLite)
	s.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(context.Background(), "admin")
	s.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	return s, func() { _ = d.Close() }
}

// addWebhookChannel 插入一个 webhook 渠道并绑定到 cookieID，返回渠道 ID。
func addWebhookChannel(t *testing.T, s *db.Store, cookieID, name, webhookURL string) int64 {
	t.Helper()
	res, err := s.DB.ExecContext(context.Background(),
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,1)`,
		name, "webhook", `{"webhook_url":"`+webhookURL+`"}`)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := s.DB.ExecContext(context.Background(),
		`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES (?,?,1)`,
		cookieID, id); err != nil {
		t.Fatalf("insert binding: %v", err)
	}
	return id
}

// TestNotifyAccountAlert_NoStore store 为 nil 时安全返回。
func TestNotifyAccountAlert_NoStore(t *testing.T) {
	n := &Notifier{logger: nilLogger()}
	// 不应 panic。
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNotifyAccountAlert_NoChannels 无绑定渠道时不报错。
func TestNotifyAccountAlert_NoChannels(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	n := New("cid", s, nil)
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNotifyAccountAlert_WithChannel 告警通知发送并包含标题与正文。
func TestNotifyAccountAlert_WithChannel(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "告警渠道", srv.URL)
	n := New("cid", s, nil)
	n.NotifyAccountAlert("cid", "critical", "Token失效", "请重新登录")

	if gotBody == "" {
		t.Fatal("应发送告警通知")
	}
	if !strings.Contains(gotBody, "严重") || !strings.Contains(gotBody, "Token失效") || !strings.Contains(gotBody, "请重新登录") {
		t.Errorf("告警正文缺少关键字: %s", gotBody)
	}
}

func TestNotifyEventUsesPersistentOutboxWhenStarted(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	addWebhookChannel(t, s, "cid", "outbox", srv.URL)
	n := New("cid", s, nilLogger())
	// 标记异步模式但不启动循环，精确验证调用返回时尚未发生外部网络请求。
	n.started.Store(true)
	n.NotifyAccountAlert("cid", "warn", "持久化通知", "正文")
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("business call performed synchronous network I/O: %d", got)
	}
	var queued int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE status='pending'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	n.drainOutbox(context.Background())
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("outbox delivery calls=%d", got)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("remaining=%d err=%v", queued, err)
	}
}

// TestNotifyAccountAlert_LevelLabels 覆盖 levelLabel 各分支。
func TestNotifyAccountAlert_LevelLabels(t *testing.T) {
	cases := map[string]string{
		"critical": "严重",
		"warn":     "警告",
		"info":     "提示",
		"unknown":  "unknown",
	}
	for level, want := range cases {
		if got := levelLabel(level); got != want {
			t.Errorf("levelLabel(%q)=%q want %q", level, got, want)
		}
	}
}

// TestSendToChannel 直接发送到指定渠道。
func TestSendToChannel(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	chID := addWebhookChannel(t, s, "cid", "直发渠道", srv.URL)
	n := New("cid", s, nil)
	if err := n.SendToChannel(chID, "直发测试"); err != nil {
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
	if err := n.SendToChannel(1, "x"); err == nil {
		t.Fatal("store 为 nil 应报错")
	}

	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	n2 := New("cid", s, nil)
	// 不存在的渠道 ID。
	if err := n2.SendToChannel(99999, "x"); err == nil {
		t.Fatal("渠道不存在应报错")
	}
}

// TestNotifyDelivery_MultiChannel 某渠道失败不影响其他渠道。
func TestNotifyDelivery_MultiChannel(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

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

	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家", "b1", "item1", "成功", "chat1")
	if got := atomic.LoadInt32(&okCount); got != 1 {
		t.Errorf("成功渠道应收到 1 次，实际 %d", got)
	}
}

// TestNotifyDelivery_TemplateVars 通知正文应包含买家名、商品ID、结果等变量。
func TestNotifyDelivery_TemplateVars(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "模板渠道", srv.URL)
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "张三", "BID123", "ITEM456", "发货成功", "CHAT789")

	for _, want := range []string{"张三", "BID123", "ITEM456", "发货成功", "CHAT789"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("正文缺少 %q: %s", want, gotBody)
		}
	}
}

// TestNotifyDelivery_EmptyChatID chatID 为空时回退为“未知”。
func TestNotifyDelivery_EmptyChatID(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "渠道", srv.URL)
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家", "b1", "item1", "结果", "")
	if !strings.Contains(gotBody, "未知") {
		t.Errorf("空 chatID 应回退为“未知”: %s", gotBody)
	}
}

// TestParseConfig_InvalidJSON 非法 JSON 走旧格式兼容分支。
func TestParseConfig_InvalidJSON(t *testing.T) {
	m := parseConfig("{not json")
	if m["config"] != "{not json" {
		t.Errorf("非法 JSON 应放入 config: %v", m)
	}
}

// TestStrOr 覆盖 string / 非 string / 缺失三个分支。
func TestStrOr(t *testing.T) {
	m := map[string]any{
		"s": "abc",
		"n": 42,
		"b": true,
	}
	if got := strOr(m, "s", "x"); got != "abc" {
		t.Errorf("strOr(s)=%q", got)
	}
	if got := strOr(m, "n", "x"); got != "42" {
		t.Errorf("strOr(n)=%q", got)
	}
	if got := strOr(m, "b", "x"); got != "true" {
		t.Errorf("strOr(b)=%q", got)
	}
	if got := strOr(m, "missing", "def"); got != "def" {
		t.Errorf("strOr(missing)=%q", got)
	}
}

// TestFallback fallback 空串与非空串。
func TestFallback(t *testing.T) {
	if got := fallback("", "默认"); got != "默认" {
		t.Errorf("fallback('')=%q", got)
	}
	if got := fallback("值", "默认"); got != "值" {
		t.Errorf("fallback('值')=%q", got)
	}
}

// TestNotifyAccountAlert_DBError 查询出错时不 panic（err != nil 分支）。
func TestNotifyAccountAlert_DBError(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	d := s.DB
	cleanup() // 提前关闭 DB，使查询返回错误
	_ = d
	n := New("cid", s, nil)
	// store 非 nil 但 DB 已关闭，查询会报错；不应 panic。
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNotifyDelivery_DBError 查询出错时不 panic（err != nil 分支）。
func TestNotifyDelivery_DBError(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	cleanup() // 提前关闭 DB
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家", "b1", "item1", "结果", "chat1")
}

// TestSendToChannel_DBError 查询渠道出错时返回包装错误。
func TestSendToChannel_DBError(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	cleanup() // 提前关闭 DB
	n := New("cid", s, nil)
	if err := n.SendToChannel(1, "x"); err == nil {
		t.Fatal("DB 查询出错应返回 error")
	}
}

// TestNotifyAccountAlert_SendError 某渠道发送失败时记录错误但不中断。
func TestNotifyAccountAlert_SendError(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "失败渠道", srv.URL)
	n := New("cid", s, nil)
	// 渠道返回 5xx，send 报错 → 走 logger.Error 分支，但不应 panic。
	n.NotifyAccountAlert("cid", "warn", "标题", "正文")
}

// TestNew_DefaultLogger logger 为 nil 时使用默认 logger。
func TestNew_DefaultLogger(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()
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
