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

func TestChatHistoryAndAccountTaskSettingsEndpoints(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	service := chat.New(store)
	srv.SetChatService(service)
	handler := srv.Router()
	cookie := loginHelper(t, handler)

	_, _, err := store.Chats.SaveMessage(context.Background(), db.ChatSession{CookieID: "acc1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家甲"},
		db.ChatMessage{MessageKey: "platform-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家甲", MessageType: "text", Content: "你好", Status: "received", SentAt: 1000}, true)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/chat/sessions?account_id=acc1", nil)
	request.AddCookie(cookie)
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
	settings, err := store.AccountTasks.Get(context.Background(), "acc1")
	if err != nil || !settings.AutoRateEnabled || !settings.AutoPolishEnabled || settings.PolishTime != "04:30" {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}

func TestChatWebSocketStreamsOnlyAuthenticatedAccountEvents(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	service := chat.New(store)
	srv.SetChatService(service)
	handler := srv.Router()
	cookie := loginHelper(t, handler)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	header := make(http.Header)
	header.Set("Cookie", cookie.String())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/chat/ws", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	var ready map[string]any
	if err := wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if _, _, err := service.RecordIncoming(ctx, chat.Incoming{AccountID: "acc1", ChatID: "chat-live", BuyerID: "buyer",
		BuyerName: "实时买家", Text: "实时消息", Raw: map[string]any{"messageId": "live-1"}}); err != nil {
		t.Fatal(err)
	}
	var event chat.Event
	if err := wsjson.Read(ctx, conn, &event); err != nil || event.Type != "message.created" || event.Message.Content != "实时消息" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestChatAndTaskEndpointsEnforceOwnershipAndValidation(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.SetChatService(chat.New(srv.Store))
	handler := srv.Router()
	cookie := loginHelper(t, handler)

	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/chat/sessions?account_id=missing", "", http.StatusForbidden},
		{http.MethodGet, "/api/chat/messages?account_id=acc1", "", http.StatusBadRequest},
		{http.MethodPut, "/api/account-tasks/acc1", `{"auto_rate_enabled":true,"rate_content":"","auto_polish_enabled":false,"polish_time":"03:00"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/account-tasks/acc1", `{"auto_rate_enabled":false,"rate_content":"x","auto_polish_enabled":true,"polish_time":"25:99"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("%s %s status=%d want=%d body=%s", tc.method, tc.path, recorder.Code, tc.want, recorder.Body.String())
		}
	}
}

func TestFindChatPlatformMessageID(t *testing.T) {
	raw := map[string]any{
		"1": map[string]any{
			"2": "64725235816@goofish",
			"3": "4263141580162.PNM",
			"10": map[string]any{
				"extJson": `{"messageId":"f87f8f6dabca4eff940863ef72a393f7"}`,
			},
		},
	}
	if got := findChatPlatformMessageID(raw, "64725235816", "f87f8f6dabca4eff940863ef72a393f7"); got != "4263141580162.PNM" {
		t.Fatalf("platform message id=%q", got)
	}
	if got := findChatPlatformMessageID(raw, "other-chat", "f87f8f6dabca4eff940863ef72a393f7"); got != "" {
		t.Fatalf("跨会话错误匹配: %q", got)
	}
}

func TestResolveChatReadMessageIDsMigratesLegacyID(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	raw := `{"1":{"2":"64725235816@goofish","3":"4263141580162.PNM","10":{"extJson":"{\"messageId\":\"f87f8f6dabca4eff940863ef72a393f7\"}"}}}`
	if err := store.WSMessages.Add(context.Background(), db.WSMessage{CookieID: "acc1", Direction: "in", ParsedJSON: raw, ParseStatus: "decrypted"}); err != nil {
		t.Fatal(err)
	}
	got := srv.resolveChatReadMessageIDs(context.Background(), "acc1", "64725235816", []map[string]any{{"messageId": "f87f8f6dabca4eff940863ef72a393f7"}})
	if len(got) != 1 || got[0]["messageId"] != "4263141580162.PNM" {
		t.Fatalf("resolved=%+v", got)
	}
}
