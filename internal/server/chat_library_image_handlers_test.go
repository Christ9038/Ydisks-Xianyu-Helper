package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// libraryImagePort 记录图片库图片发送收到的输入，并返回预设错误，用于覆盖 HTTP 错误映射。
type libraryImagePort struct {
	// contractChatPort 提供无关聊天能力的稳定测试实现。
	contractChatPort
	// sendErr 是图片库图片发送要返回的应用层错误。
	sendErr error
	// got 保存最近一次收到的发送输入。
	got chatapp.LibraryImageInput
	// called 记录发送入口是否被调用。
	called bool
}

// SendLibraryImage 记录输入并返回预设错误。
func (port *libraryImagePort) SendLibraryImage(_ context.Context, input chatapp.LibraryImageInput) (*chatapp.Message, error) {
	port.called, port.got = true, input
	if port.sendErr != nil {
		return &chatapp.Message{AccountID: input.Session.AccountID, ChatID: input.Session.ChatID, MessageKey: "k", Direction: "outgoing", MessageType: "image", Status: "failed"}, port.sendErr
	}
	return &chatapp.Message{AccountID: input.Session.AccountID, ChatID: input.Session.ChatID, MessageKey: "k", Direction: "outgoing", MessageType: "image", Content: "https://img.example/x.png", Status: "sent"}, nil
}

// TestSendChatLibraryImageMapsErrorsAndForwardsInput 验证图片库图片发送的参数校验、归属校验和错误映射。
func TestSendChatLibraryImageMapsErrorsAndForwardsInput(t *testing.T) {
	// item 是当前待请求的场景，包含请求体与期望状态码。
	for _, item := range []struct {
		// name 是子用例名称。
		name string
		// body 是请求 JSON。
		body string
		// sendErr 是应用层返回的错误。
		sendErr error
		// wantStatus 是期望 HTTP 状态。
		wantStatus int
		// wantCalled 表示是否应进入应用层发送入口。
		wantCalled bool
	}{
		{name: "success", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1","library_image_id":7}`, wantStatus: http.StatusCreated, wantCalled: true},
		{name: "missing image", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1"}`, wantStatus: http.StatusBadRequest},
		{name: "missing chat", body: `{"account_id":"acc1","peer_user_id":"b1","library_image_id":7}`, wantStatus: http.StatusBadRequest},
		{name: "bad json", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "offline", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1","library_image_id":7}`, sendErr: chatapp.ErrOffline, wantStatus: http.StatusConflict, wantCalled: true},
		{name: "unavailable", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1","library_image_id":7}`, sendErr: chatapp.ErrUnavailable, wantStatus: http.StatusServiceUnavailable, wantCalled: true},
		{name: "image unreadable", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1","library_image_id":7}`, sendErr: chatapp.ErrSend, wantStatus: http.StatusBadGateway, wantCalled: true},
		{name: "uncertain", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1","library_image_id":7}`, sendErr: chatapp.ErrSendUncertain, wantStatus: http.StatusBadGateway, wantCalled: true},
		{name: "invalid input", body: `{"account_id":"acc1","chat_id":"c1","peer_user_id":"b1","library_image_id":7}`, sendErr: chatapp.ErrSendInvalidInput, wantStatus: http.StatusBadRequest, wantCalled: true},
	} {
		// t 管理单个错误映射场景。
		t.Run(item.name, func(t *testing.T) {
			// port 记录发送输入并返回预设错误。
			port := &libraryImagePort{sendErr: item.sendErr}
			// handler、cookie 是带登录态的测试服务；acc1 属于该登录用户。
			handler, cookie := newChatLibraryImageTestServer(t, port)
			// request 是发往被测路由的 HTTP 请求。
			request := httptest.NewRequest(http.MethodPost, "/api/v1/chat/library-images", strings.NewReader(item.body))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			// recorder 记录被测路由写出的响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != item.wantStatus || port.called != item.wantCalled {
				t.Fatalf("status=%d called=%v body=%s", recorder.Code, port.called, recorder.Body.String())
			}
			if item.wantCalled && (port.got.ImageID != 7 || port.got.Session.AccountID != "acc1" || port.got.Session.PeerUserID != "b1") {
				t.Fatalf("应用层收到的输入不正确: %+v", port.got)
			}
		})
	}
}

// newChatLibraryImageTestServer 构造注入指定聊天端口的真实 Router 并完成管理员登录，资源在测试结束时释放。
func newChatLibraryImageTestServer(t *testing.T, port ChatPort) (http.Handler, *http.Cookie) {
	t.Helper()
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	t.Cleanup(cleanup)
	srv.applications.chat = port
	// handler 是注入可控聊天端口后的真实路由。
	handler := srv.Router()
	return handler, loginHelper(t, handler)
}
