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

// TestChatHistoryAndAccountTaskSettingsEndpoints 负责Test聊天HistoryAnd账号任务设置Endpoints相关处理。
func TestChatHistoryAndAccountTaskSettingsEndpoints(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// handler 保存handler，供当前处理流程使用
	handler := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, handler)

	// err 保存err，供当前处理流程使用
	_, _, err := store.Chats.SaveMessage(context.Background(), db.ChatSession{CookieID: "acc1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家甲"},
		db.ChatMessage{MessageKey: "platform-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家甲", MessageType: "text", Content: "你好", Status: "received", SentAt: 1000}, true)
	if err != nil {
		t.Fatal(err)
	}

	// request 保存请求，供当前处理流程使用
	request := httptest.NewRequest(http.MethodGet, "/api/chat/sessions?account_id=acc1", nil)
	request.AddCookie(cookie)
	// recorder 保存recorder，供当前处理流程使用
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
	// settings、err 保存settings、err，供当前处理流程使用
	settings, err := store.AccountTasks.Get(context.Background(), "acc1")
	if err != nil || !settings.AutoRateEnabled || !settings.AutoPolishEnabled || settings.PolishTime != "04:30" {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}

// TestChatWebSocketStreamsOnlyAuthenticatedAccountEvents 负责Test聊天WebSocketStreamsOnlyAuthenticated账号Events相关处理。
func TestChatWebSocketStreamsOnlyAuthenticatedAccountEvents(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// service 保存service，供当前处理流程使用
	service := srv.chat
	// handler 保存handler，供当前处理流程使用
	handler := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, handler)
	// httpServer 保存httpServer，供当前处理流程使用
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	// header 保存header，供当前处理流程使用
	header := make(http.Header)
	header.Set("Cookie", cookie.String())
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// conn、err 保存conn、err，供当前处理流程使用
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/chat/ws", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	// ready 保存ready，供当前处理流程使用
	var ready map[string]any
	if // err 保存err，供当前处理流程使用
	err := wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if // err 保存err，供当前处理流程使用
	_, _, err := service.RecordIncoming(ctx, chat.Incoming{AccountID: "acc1", ChatID: "chat-live", BuyerID: "buyer",
		BuyerName: "实时买家", Text: "实时消息", Raw: map[string]any{"messageId": "live-1"}}); err != nil {
		t.Fatal(err)
	}
	// event 保存event，供当前处理流程使用
	var event chat.Event
	if // err 保存err，供当前处理流程使用
	err := wsjson.Read(ctx, conn, &event); err != nil || event.Type != "message.created" || event.Message.Content != "实时消息" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

// TestChatAndTaskEndpointsEnforceOwnershipAndValidation 负责Test聊天And任务EndpointsEnforceOwnershipAndValidation相关处理。
func TestChatAndTaskEndpointsEnforceOwnershipAndValidation(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// handler 保存handler，供当前处理流程使用
	handler := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, handler)

	// cases 保存cases，供当前处理流程使用
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
		// request 保存请求，供当前处理流程使用
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 保存recorder，供当前处理流程使用
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
