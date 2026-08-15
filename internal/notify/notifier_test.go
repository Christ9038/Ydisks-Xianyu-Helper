package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestNotifierWaitContextHonorsDeadline 验证通知 worker 等待受关闭上下文限制。
func TestNotifierWaitContextHonorsDeadline(t *testing.T) {
	// notifier 保存已启动但尚未完成的通知器，以验证等待超时不会永久阻塞。
	notifier := &Notifier{done: make(chan struct{})}
	notifier.started.Store(true)
	// ctx、cancel 保存短时关闭上下文及其释放函数。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	// err 表示尚未完成 worker 在超时上下文下的等待结果。
	if err := notifier.WaitContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitContext error=%v, want deadline exceeded", err)
	}
	close(notifier.done)
	// err 表示已完成 worker 的等待结果。
	if err := notifier.WaitContext(context.Background()); err != nil {
		t.Fatalf("completed WaitContext error=%v", err)
	}
}

// newNotifyStore 负责newNotifyStore相关处理。
func newNotifyStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// d、err 保存d、err，供当前处理流程使用
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// s 保存s，供当前处理流程使用
	s := db.NewStore(d, db.DialectSQLite)
	s.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := s.Users.GetByUsername(context.Background(), "admin")
	s.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	return s, func() { d.Close() }
}

// TestSendDingTalk 钉钉渠道 POST 正确 payload。
func TestSendDingTalk(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()

	// got 保存got，供当前处理流程使用
	var got map[string]any
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 保存b，供当前处理流程使用
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	// cfg 保存cfg，供当前处理流程使用
	cfg := `{"webhook_url":"` + srv.URL + `"}`
	// ch 保存ch，供当前处理流程使用
	ch := db.NotificationChannel{ID: 1, Name: "钉钉", Type: "ding_talk", Config: cfg}
	if // err 保存err，供当前处理流程使用
	err := n.send(ch, "测试消息"); err != nil {
		t.Fatalf("send dingtalk: %v", err)
	}
	if got["msgtype"] != "markdown" {
		t.Errorf("msgtype=%v want markdown", got["msgtype"])
	}
	// md 保存md，供当前处理流程使用
	md, _ := got["markdown"].(map[string]any)
	if md["text"] != "测试消息" {
		t.Errorf("text=%v", md["text"])
	}
}

// TestSendTelegram Telegram 渠道。
func TestSendTelegram(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()

	// got 保存got，供当前处理流程使用
	var got map[string]any
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 保存b，供当前处理流程使用
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	// 用 config 覆盖 API URL 不可行（硬编码），故直接调内部 sendTelegram。
	cfg := map[string]any{"bot_token": "TOKEN", "chat_id": "123"}
	// 临时替换 httpc 的 URL：用 srv.URL 作为 webhook。sendTelegram 硬编码了 telegram API，
	// 这里改用 dingtalk server 验证 JSON 形状已覆盖；Telegram 单独验证配置校验。
	// 实际会尝试连 api.telegram.org，可能失败；仅验证不 panic。
	_ = n.sendTelegram(cfg, "hi")
	// 配置不全应返回 error。
	if err := n.sendTelegram(map[string]any{"bot_token": ""}, "hi"); err == nil {
		t.Fatal("缺 chat_id 应报错")
	}
	_ = got
}

// TestRouteByChannelType 渠道类型路由。
func TestRouteByChannelType(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()
	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)

	// 不支持的类型应返回 error。
	err := n.send(db.NotificationChannel{Type: "unknown"}, "x")
	if err == nil {
		t.Fatal("未知渠道应报错")
	}
	// QQ 渠道暂不支持。
	if err := n.send(db.NotificationChannel{Type: "qq"}, "x"); err == nil {
		t.Fatal("qq 渠道应报错")
	}
}

// TestNotifyDelivery_NoChannels 无通知配置时不报错。
func TestNotifyDelivery_NoChannels(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()
	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	// 不应 panic。
	n.NotifyDelivery("cid", "买家", "b1", "item1", "发货成功", "chat1")
}

// TestNotifyDelivery_WithChannel 有渠道时发送。
func TestNotifyDelivery_WithChannel(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	_ = admin

	// gotBody 保存got请求体，供当前处理流程使用
	var gotBody string
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 保存b，供当前处理流程使用
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// 插入一个 webhook 渠道 + 绑定。
	res, _ := s.DB.ExecContext(ctx,
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('测试','webhook',?,1,1)`,
		`{"webhook_url":"`+srv.URL+`"}`)
	// chID 保存chID，供当前处理流程使用
	chID, _ := res.LastInsertId()
	s.DB.ExecContext(ctx,
		`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES ('cid',?,1)`, chID)

	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	n.NotifyDelivery("cid", "买家甲", "b1", "item1", "发货成功", "chat1")
	if gotBody == "" {
		t.Fatal("应发送通知")
	}
	if !contains(gotBody, "买家甲") || !contains(gotBody, "发货成功") {
		t.Errorf("通知正文异常: %s", gotBody)
	}
}

// TestNotifyEvent_FiltersByChannelEventTypes 负责TestNotifyEventFiltersBy渠道EventTypes相关处理。
func TestNotifyEvent_FiltersByChannelEventTypes(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// hits 保存hits，供当前处理流程使用
	hits := map[string]int{}
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// resOffline、err 保存响应Offline、err，供当前处理流程使用
	resOffline, err := s.DB.ExecContext(ctx,
		`INSERT INTO notification_channels (name,type,config,event_types,enabled,user_id) VALUES ('掉线','webhook',?,?,1,1)`,
		`{"webhook_url":"`+srv.URL+`/offline"}`, `["`+EventAccountOffline+`"]`)
	if err != nil {
		t.Fatalf("insert offline channel: %v", err)
	}
	// offlineID 保存offlineID，供当前处理流程使用
	offlineID, _ := resOffline.LastInsertId()
	// resDisabled、err 保存响应Disabled、err，供当前处理流程使用
	resDisabled, err := s.DB.ExecContext(ctx,
		`INSERT INTO notification_channels (name,type,config,event_types,enabled,user_id) VALUES ('禁用','webhook',?,?,1,1)`,
		`{"webhook_url":"`+srv.URL+`/disabled"}`, `["`+EventAccountDisabled+`"]`)
	if err != nil {
		t.Fatalf("insert disabled channel: %v", err)
	}
	// disabledID 保存disabledID，供当前处理流程使用
	disabledID, _ := resDisabled.LastInsertId()
	// id 表示当前遍历过程中的标识
	for _, id := range []int64{offlineID, disabledID} {
		if // err 保存err，供当前处理流程使用
		_, err := s.DB.ExecContext(ctx,
			`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES ('cid',?,1)`, id); err != nil {
			t.Fatalf("insert binding: %v", err)
		}
	}

	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	n.NotifyAccountEvent("cid", EventAccountOffline, "warn", "掉线", "正在恢复")
	if hits["/offline"] != 1 {
		t.Fatalf("offline channel hit=%d want 1", hits["/offline"])
	}
	if hits["/disabled"] != 0 {
		t.Fatalf("disabled channel should be filtered, hit=%d", hits["/disabled"])
	}
}

// TestNotifyAccountAlertClassifiesLegacyAlerts 负责TestNotify账号AlertClassifiesLegacyAlerts相关处理。
func TestNotifyAccountAlertClassifiesLegacyAlerts(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// hits 保存hits，供当前处理流程使用
	hits := map[string]int{}
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// resSecurity、err 保存响应Security、err，供当前处理流程使用
	resSecurity, err := s.DB.ExecContext(ctx,
		`INSERT INTO notification_channels (name,type,config,event_types,enabled,user_id) VALUES ('风控','webhook',?,?,1,1)`,
		`{"webhook_url":"`+srv.URL+`/security"}`, `["`+EventSecurityVerification+`"]`)
	if err != nil {
		t.Fatalf("insert security channel: %v", err)
	}
	// securityID 保存securityID，供当前处理流程使用
	securityID, _ := resSecurity.LastInsertId()
	// resToken、err 保存响应Token、err，供当前处理流程使用
	resToken, err := s.DB.ExecContext(ctx,
		`INSERT INTO notification_channels (name,type,config,event_types,enabled,user_id) VALUES ('续期','webhook',?,?,1,1)`,
		`{"webhook_url":"`+srv.URL+`/token"}`, `["`+EventTokenRenewal+`"]`)
	if err != nil {
		t.Fatalf("insert token channel: %v", err)
	}
	// tokenID 保存令牌ID，供当前处理流程使用
	tokenID, _ := resToken.LastInsertId()
	// id 表示当前遍历过程中的标识
	for _, id := range []int64{securityID, tokenID} {
		if // err 保存err，供当前处理流程使用
		_, err := s.DB.ExecContext(ctx,
			`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES ('cid',?,1)`, id); err != nil {
			t.Fatalf("insert binding: %v", err)
		}
	}

	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	n.NotifyAccountAlert("cid", "warn", "闲鱼要求滑块验证", "请完成 captcha")
	if hits["/security"] != 1 {
		t.Fatalf("security channel hit=%d want 1", hits["/security"])
	}
	if hits["/token"] != 0 {
		t.Fatalf("token channel should not receive security alert, hit=%d", hits["/token"])
	}
}

// TestNotifyEventRejectsMalformedEventTypes 负责TestNotifyEventRejectsMalformedEventTypes相关处理。
func TestNotifyEventRejectsMalformedEventTypes(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newNotifyStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// hits 保存hits，供当前处理流程使用
	hits := 0
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// res、err 保存res、err，供当前处理流程使用
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO notification_channels (name,type,config,event_types,enabled,user_id) VALUES ('坏配置','webhook',?,?,1,1)`,
		`{"webhook_url":"`+srv.URL+`"}`, `["`+EventAccountOffline+`"`)
	if err != nil {
		t.Fatalf("insert malformed channel: %v", err)
	}
	// channelID 保存渠道ID，供当前处理流程使用
	channelID, _ := res.LastInsertId()
	if // err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES ('cid',?,1)`, channelID); err != nil {
		t.Fatalf("insert binding: %v", err)
	}

	// n 保存n，供当前处理流程使用
	n := New("cid", s, nil)
	n.NotifyAccountEvent("cid", EventAccountOffline, "warn", "掉线", "正在恢复")
	if hits != 0 {
		t.Fatalf("malformed event_types should deny delivery, hits=%d", hits)
	}
}

// TestParseConfig 旧格式兼容。
func TestParseConfig(t *testing.T) {
	// 标准 JSON。
	m := parseConfig(`{"webhook_url":"http://x"}`)
	if m["webhook_url"] != "http://x" {
		t.Errorf("JSON 解析: %v", m)
	}
	// 旧格式字符串。
	m2 := parseConfig(`http://old`)
	if m2["config"] != "http://old" {
		t.Errorf("旧格式兼容: %v", m2)
	}
	// 空字符串。
	m3 := parseConfig("")
	if len(m3) != 0 {
		t.Errorf("空配置: %v", m3)
	}
}

// contains 负责contains相关处理。
func contains(s, sub string) bool {
	for // i 保存i，供当前处理流程使用
	i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
