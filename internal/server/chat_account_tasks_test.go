package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
)

// TestChatHistoryAndAccountTaskSettingsEndpoints 封装Test聊天HistoryAnd账号任务设置Endpoints业务协调。
func TestChatHistoryAndAccountTaskSettingsEndpoints(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// handler 用于本次流程后续判断的handler
	handler := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, handler)

	// err 用于本次流程后续判断的err
	_, _, err := store.Chats.SaveMessage(context.Background(), db.ChatSession{CookieID: "acc1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家甲"},
		db.ChatMessage{MessageKey: "platform-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家甲", MessageType: "text", Content: "你好", Status: "received", SentAt: 1000}, true)
	if err != nil {
		t.Fatal(err)
	}

	// request 用于本次流程后续判断的请求
	request := httptest.NewRequest(http.MethodGet, "/api/chat/sessions?account_id=acc1", nil)
	request.AddCookie(cookie)
	// recorder 用于本次流程后续判断的recorder
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "买家甲") {
		t.Fatalf("sessions status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/chat/messages?account_id=acc1&chat_id=chat-1", nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "你好") {
		t.Fatalf("messages status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/account-tasks/acc1", strings.NewReader(`{
		"auto_rate_enabled":true,"rate_content":"交易愉快","auto_polish_enabled":true,"polish_time":"04:30"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "交易愉快") {
		t.Fatalf("task settings status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := store.AccountTasks.Get(context.Background(), "acc1")
	if err != nil || !settings.AutoRateEnabled || !settings.AutoPolishEnabled || settings.PolishTime != "04:30" {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}

// canceledReadReportPort 在本地已读保存后取消浏览器请求，用于验证平台上报使用独立的短时 Context。
type canceledReadReportPort struct {
	// contractChatPort 提供无关聊天能力的稳定测试实现。
	contractChatPort
	// cancel 在本地已读状态写入完成后模拟浏览器中止当前 HTTP 请求。
	cancel context.CancelFunc
	// reportContextCanceled 记录平台上报调用当刻是否已被取消，不能继承浏览器请求的取消状态。
	reportContextCanceled bool
	// reportContextHasDeadline 记录平台上报是否具有有界远端等待截止时间。
	reportContextHasDeadline bool
}

// MarkRead 模拟本地未读状态已成功落库后浏览器立即中断连接。
func (port *canceledReadReportPort) MarkRead(context.Context, int64, string, string) error {
	port.cancel()
	return nil
}

// ReportPlatformRead 记录平台上报使用的 Context，不进行真实外部调用。
func (port *canceledReadReportPort) ReportPlatformRead(ctx context.Context, _ string, _ string, _ []map[string]any) error {
	port.reportContextCanceled = ctx.Err() != nil
	_, port.reportContextHasDeadline = ctx.Deadline()
	return nil
}

// TestMarkChatReadKeepsPlatformReportAliveAfterClientCancellation 验证本地已读成功后，浏览器取消不会中断有界的平台已读回执。
func TestMarkChatReadKeepsPlatformReportAliveAfterClientCancellation(t *testing.T) {
	// srv、cleanup 分别是聊天路由测试服务器和测试资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// requestCtx 和 cancel 模拟浏览器请求生命周期；测试端口在本地已读完成后触发取消。
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// port 保存会记录平台回执 Context 的聊天应用替身。
	port := &canceledReadReportPort{cancel: cancel}
	srv.applications.chat = port
	// handler 是替换聊天端口后的真实 HTTP Router。
	handler := srv.Router()
	// cookie 是通过真实认证流程取得的管理员会话。
	cookie := loginHelper(t, handler)
	// request 是带有一条有效平台消息标识的已读请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat/read", strings.NewReader(`{"account_id":"acc1","chat_id":"chat-read","message_ids":[{"messageId":"message.PNM"}]}`)).WithContext(requestCtx)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	// recorder 保存真实 handler 响应，已取消的浏览器请求不应使本地状态返回失败。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("已读请求状态=%d，响应=%s", recorder.Code, recorder.Body.String())
	}
	if port.reportContextCanceled {
		t.Fatal("平台已读上报错误继承了浏览器取消")
	}
	if !port.reportContextHasDeadline {
		t.Fatal("平台已读上报缺少独立超时限制")
	}
}

// TestChatWebSocketStreamsOnlyAuthenticatedAccountEvents 封装Test聊天WebSocketStreamsOnlyAuthenticated账号Events业务协调。
func TestChatWebSocketStreamsOnlyAuthenticatedAccountEvents(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// service 用于本次流程后续判断的service
	service := testChatDomain(srv)
	// handler 用于本次流程后续判断的handler
	handler := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, handler)
	// httpServer 用于本次流程后续判断的httpServer
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	// header 用于本次流程后续判断的header
	header := make(http.Header)
	header.Set("Cookie", cookie.String())
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// conn、err 用于本次流程后续判断的conn、err
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/chat/ws", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	// ready 用于本次流程后续判断的ready
	var ready map[string]any
	if // err 用于本次流程后续判断的err
	err := wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if // err 用于本次流程后续判断的err
	_, _, err := service.RecordIncoming(ctx, chat.Incoming{AccountID: "acc1", ChatID: "chat-live", BuyerID: "buyer",
		BuyerName: "实时买家", Text: "实时消息", Raw: map[string]any{"messageId": "live-1"}}); err != nil {
		t.Fatal(err)
	}
	// event 用于本次流程后续判断的event
	var event chat.Event
	if // err 用于本次流程后续判断的err
	err := wsjson.Read(ctx, conn, &event); err != nil || event.Type != "message.created" || event.Message.Content != "实时消息" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

// TestChatAndTaskEndpointsEnforceOwnershipAndValidation 封装Test聊天And任务EndpointsEnforceOwnershipAndValidation业务协调。
func TestChatAndTaskEndpointsEnforceOwnershipAndValidation(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// handler 用于本次流程后续判断的handler
	handler := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, handler)

	// cases 用于本次流程后续判断的cases
	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/chat/sessions?account_id=missing", "", http.StatusForbidden},
		{http.MethodGet, "/api/chat/messages?account_id=acc1", "", http.StatusBadRequest},
		{http.MethodPut, "/api/account-tasks/acc1", `{"auto_rate_enabled":true,"rate_content":"","auto_polish_enabled":false,"polish_time":"03:00"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/account-tasks/acc1", `{"auto_rate_enabled":false,"rate_content":"x","auto_polish_enabled":true,"polish_time":"25:99"}`, http.StatusBadRequest},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		// request 用于本次流程后续判断的请求
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 用于本次流程后续判断的recorder
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("%s %s status=%d want=%d body=%s", tc.method, tc.path, recorder.Code, tc.want, recorder.Body.String())
		}
	}
}

// TestFindChatPlatformMessageID 验证历史推送关联 ID 只在同一会话内映射为 PNM 已读 ID。
func TestFindChatPlatformMessageID(t *testing.T) {
	// raw 模拟持久化的解密 WS 消息，包含推送关联 ID 与平台 PNM ID。
	raw := map[string]any{
		"1": map[string]any{
			"2": "64725235816@goofish",
			"3": "4263141580162.PNM",
			"10": map[string]any{
				"extJson": `{"messageId":"f87f8f6dabca4eff940863ef72a393f7"}`,
			},
		},
	}
	// got 保存同会话匹配后可向平台上报的 PNM ID。
	if got := findChatPlatformMessageID(raw, "64725235816", "f87f8f6dabca4eff940863ef72a393f7"); got != "4263141580162.PNM" {
		t.Fatalf("platform message id=%q", got)
	}
	// got 保存跨会话查询结果，必须为空以禁止错误标记其他会话已读。
	if got := findChatPlatformMessageID(raw, "other-chat", "f87f8f6dabca4eff940863ef72a393f7"); got != "" {
		t.Fatalf("跨会话错误匹配: %q", got)
	}
}

// TestResolveChatReadMessageIDsMigratesLegacyID 验证历史关联 ID 会转换为平台接受的 PNM ID。
func TestResolveChatReadMessageIDsMigratesLegacyID(t *testing.T) {
	// srv 提供待测解析路径；store 写入历史 WS 消息；cleanup 释放测试服务器和数据库。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// raw 是保存到数据库的解密信封 JSON，含旧关联 ID 及其对应的 PNM ID。
	raw := `{"1":{"2":"64725235816@goofish","3":"4263141580162.PNM","10":{"extJson":"{\"messageId\":\"f87f8f6dabca4eff940863ef72a393f7\"}"}}}`
	// err 保存写入历史 WS 消息失败的原因，写入失败会使后续解析夹具无效。
	if err := store.WSMessages.Add(context.Background(), db.WSMessage{CookieID: "acc1", Direction: "in", ParsedJSON: raw, ParseStatus: "decrypted"}); err != nil {
		t.Fatal(err)
	}
	// got 是兼容解析后的上报参数，旧关联 ID 必须被替换为 PNM ID。
	got := srv.resolveChatReadMessageIDs(context.Background(), "acc1", "64725235816", []map[string]any{{"messageId": "f87f8f6dabca4eff940863ef72a393f7"}})
	if len(got) != 1 || got[0]["messageId"] != "4263141580162.PNM" {
		t.Fatalf("resolved=%+v", got)
	}
}
