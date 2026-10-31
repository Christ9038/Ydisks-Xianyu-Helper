package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	accountapp "xianyu-go/internal/application/account"
	chatapp "xianyu-go/internal/application/chat"
)

// chatGapPort 在通用聊天桩上扩展键集分页游标、事件订阅和按账号归属的判断。
type chatGapPort struct {
	// chatHandlerCoveragePort 提供其余聊天用例的默认桩实现。
	*chatHandlerCoveragePort
	// nextCursor 是 ListSessionPage 返回的下一页游标。
	nextCursor *chatapp.SessionCursor
	// events 是 Subscribe 返回的事件通道，由测试作为发送方关闭。
	events chan chatapp.Event
	// subscribeErr 是 Subscribe 返回的错误。
	subscribeErr error
	// ownedAccount 是唯一被判定为属于当前用户的账号。
	ownedAccount string
}

// ListSessionPage 返回带预设游标的会话页。
func (p *chatGapPort) ListSessionPage(context.Context, int64, string, *chatapp.SessionCursor, int) (chatapp.SessionPage, error) {
	return chatapp.SessionPage{NextCursor: p.nextCursor}, nil
}

// ItemCatalogAvailable 声明商品查询能力未装配，用于覆盖 503 分支。
func (p *chatGapPort) ItemCatalogAvailable() bool { return false }

// Subscribe 返回预设事件通道或错误。
func (p *chatGapPort) Subscribe(context.Context, int64) (<-chan chatapp.Event, func(), error) {
	return p.events, func() {}, p.subscribeErr
}

// OwnsAccount 只认可预设账号。
func (p *chatGapPort) OwnsAccount(_ context.Context, _ int64, accountID string) (bool, error) {
	return accountID == p.ownedAccount, nil
}

// chatGapAuth 包装认证端口，让 ValidateSession 在第 failFrom 次调用起返回预设错误。
type chatGapAuth struct {
	// AuthenticationPort 提供其余认证能力。
	AuthenticationPort
	// calls 统计 ValidateSession 被调用的次数。
	calls atomic.Int32
	// failFrom 是开始失败的调用序号（从 1 开始）；0 表示永不失败。
	failFrom int32
	// err 是失败时返回的错误。
	err error
}

// ValidateSession 在达到失败序号后返回预设错误。
func (a *chatGapAuth) ValidateSession(ctx context.Context, sessionID string, userID int64) error {
	// n 是本次调用的序号。
	n := a.calls.Add(1)
	if a.failFrom > 0 && n >= a.failFrom {
		return a.err
	}
	return a.AuthenticationPort.ValidateSession(ctx, sessionID, userID)
}

// TestChatSessionListGapBranches 覆盖会话列表的游标非法、刷新失败和游标编码失败分支。
func TestChatSessionListGapBranches(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// base 是通用聊天桩。
	base := &chatHandlerCoveragePort{ownsAccountResult: true, sendingAvailable: true, imageUploadAvailable: true}
	// port 在通用桩上扩展分页游标。
	port := &chatGapPort{chatHandlerCoveragePort: base, ownedAccount: "acc1"}
	srv.applications.chat = port
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)

	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/sessions?account_id=acc1&stored_cursor=!!!", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad cursor status=%d", rec.Code)
	}
	base.refreshConversationsErr = errors.New("platform down")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/sessions?account_id=acc1&refresh=1", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("refresh failure status=%d", rec.Code)
	}
	base.refreshConversationsErr = nil
	base.refreshIdentitiesErr = errors.New("identity down")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/sessions?account_id=acc1&refresh=1", ""); rec.Code != http.StatusOK {
		t.Errorf("identity failure status=%d", rec.Code)
	}
	port.nextCursor = &chatapp.SessionCursor{ChatID: " "}
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/sessions?account_id=acc1", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("bad next cursor status=%d", rec.Code)
	}
	// 历史刷新出现未分类错误时仍应回退本地消息。
	base.refreshHistoryErr = errors.New("history down")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/messages?account_id=acc1&chat_id=c1", ""); rec.Code != http.StatusOK {
		t.Errorf("history fallback status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/messages?account_id=other&chat_id=c1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("foreign messages status=%d", rec.Code)
	}
	// 商品查询参数非法与服务未启用。
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/items?account_id=acc1&chat_id=c1&role=peer&page=0", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad item page status=%d", rec.Code)
	}
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/items?account_id=acc1&chat_id=c1&role=peer", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("item catalog unsupported status=%d", rec.Code)
	}
}

// TestChatImageAndReadGapBranches 覆盖图片缺失/为空与已读上报取消、消息标识筛选分支。
func TestChatImageAndReadGapBranches(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是聊天桩，已读标识解析为固定 PNM。
	port := &chatHandlerCoveragePort{ownsAccountResult: true, sendingAvailable: true, imageUploadAvailable: true, resolvedReadID: "m1.PNM"}
	srv.applications.chat = port
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)

	// emptyRequest 上传了空图片。
	emptyRequest := newChatImageCoverageRequest(t, cookie, "", "", true)
	// emptyRecorder 保存响应。
	emptyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(emptyRecorder, emptyRequest)
	if emptyRecorder.Code != http.StatusBadRequest {
		t.Errorf("empty image status=%d", emptyRecorder.Code)
	}
	// noFile 只有文本字段而没有 image 文件。
	noFile := httptest.NewRequest(http.MethodPost, "/api/v1/chat/images", strings.NewReader("--b\r\nContent-Disposition: form-data; name=\"account_id\"\r\n\r\nacc1\r\n--b\r\nContent-Disposition: form-data; name=\"chat_id\"\r\n\r\nc1\r\n--b\r\nContent-Disposition: form-data; name=\"peer_user_id\"\r\n\r\nb1\r\n--b--\r\n"))
	noFile.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	noFile.AddCookie(cookie)
	// noFileRecorder 保存响应。
	noFileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(noFileRecorder, noFile)
	if noFileRecorder.Code != http.StatusBadRequest {
		t.Errorf("missing image status=%d body=%s", noFileRecorder.Code, noFileRecorder.Body.String())
	}

	port.reportReadErr = context.Canceled
	// ids 含非字符串、空白、重复与可解析的消息标识，只应保留一条。
	body := `{"account_id":"acc1","chat_id":"c1","message_ids":[{"messageId":5},{"messageId":" "},{"messageId":"m1"},{"messageId":"m1"}]}`
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "POST", "/api/v1/chat/read", body); rec.Code != http.StatusOK {
		t.Errorf("read status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 解析不到 PNM 的消息会被忽略。
	port.resolvedReadID = "legacy"
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "POST", "/api/v1/chat/read", body); rec.Code != http.StatusOK {
		t.Errorf("read unresolved status=%d", rec.Code)
	}
}

// TestWriteChatItemErrorMapsEveryApplicationError 覆盖商品卡片错误到 HTTP 契约的完整映射。
func TestWriteChatItemErrorMapsEveryApplicationError(t *testing.T) {
	// srv 是零值 Server，恢复器缺失时 recoverExpiredSession 安全返回 false。
	srv := &Server{}
	// cases 描述每个应用错误、操作类型与期望状态码。
	cases := []struct {
		// err 是注入的应用错误。
		err error
		// operation 是 list 或 send。
		operation string
		// want 是期望状态码。
		want int
	}{
		{chatapp.ErrChatItemInvalid, "send", 400}, {chatapp.ErrSendInvalidInput, "send", 400},
		{chatapp.ErrChatItemForbidden, "send", 403}, {chatapp.ErrChatSessionNotFound, "send", 404},
		{chatapp.ErrOffline, "send", 409}, {chatapp.ErrSendUncertain, "send", 502},
		{chatapp.ErrUnavailable, "send", 503}, {chatapp.ErrChatItemUnavailable, "list", 503},
		{chatapp.ErrSessionUnavailable, "list", 503}, {chatapp.ErrStatusSave, "send", 500},
		{chatapp.ErrChatItemCreate, "send", 500}, {chatapp.ErrSend, "send", 502},
		{errors.New("x"), "send", 502}, {errors.New("x"), "list", 502},
	}
	// tc 表示当前错误映射场景。
	for _, tc := range cases {
		// recorder 保存响应。
		recorder := httptest.NewRecorder()
		srv.writeChatItemError(recorder, httptest.NewRequest("GET", "/", nil), "acc1", tc.operation, tc.err, &chatapp.Message{})
		if recorder.Code != tc.want {
			t.Errorf("%v/%s => %d want %d", tc.err, tc.operation, recorder.Code, tc.want)
		}
	}
}

// chatGapWebSocketServer 启动带有桩依赖的真实 HTTP 服务，返回 ws 地址、请求头与清理函数。
func chatGapWebSocketServer(t *testing.T, port *chatGapPort, authPort *chatGapAuth) (string, http.Header, func()) {
	t.Helper()
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	authPort.AuthenticationPort = srv.applications.authentication
	srv.applications.chat = port
	srv.applications.authentication = authPort
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)
	// httpServer 是真实监听的测试服务。
	httpServer := httptest.NewServer(handler)
	// header 携带会话 Cookie。
	header := make(http.Header)
	header.Set("Cookie", cookie.String())
	return "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/v1/chat/ws", header, func() {
		httpServer.Close()
		cleanup()
	}
}

// TestChatWebSocketEventFilteringAndSessionRevocation 覆盖实时事件的账号筛选、通道关闭与会话失效关闭码。
func TestChatWebSocketEventFilteringAndSessionRevocation(t *testing.T) {
	// ctxTimeout 限制每个子场景的总时长。
	const ctxTimeout = 5 * time.Second

	t.Run("filters and closes", func(t *testing.T) {
		// events 是由测试关闭的事件通道。
		events := make(chan chatapp.Event, 8)
		// port 只承认 acc1。
		port := &chatGapPort{chatHandlerCoveragePort: &chatHandlerCoveragePort{}, events: events, ownedAccount: "acc1"}
		// header、stop、url 分别保存本步骤的返回值：header为携带会话 Cookie 的 WebSocket 握手请求头；stop为释放测试服务与监听端口的清理函数；url为测试服务的 WebSocket 连接地址
		url, header, stop := chatGapWebSocketServer(t, port, &chatGapAuth{})
		defer stop()
		// ctx 控制客户端读写。
		ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
		defer cancel()
		// conn 是客户端连接。
		conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		// ready 保存就绪帧。
		var ready map[string]any
		if err = wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
			t.Fatalf("ready=%v err=%v", ready, err)
		}
		// 依次发送：账号冲突、未授权账号、仅会话事件、正常消息。
		events <- chatapp.Event{Type: "x", Message: &chatapp.Message{AccountID: "acc1"}, Session: &chatapp.Session{AccountID: "acc9"}}
		events <- chatapp.Event{Type: "x", Message: &chatapp.Message{AccountID: "acc2"}}
		events <- chatapp.Event{Type: "session.updated", Session: &chatapp.Session{AccountID: "acc1", ChatID: "c1"}}
		events <- chatapp.Event{Type: "message.created", Message: &chatapp.Message{AccountID: "acc1", ChatID: "c1"}}
		// first、second 依次保存应当到达的两个事件。
		var first, second chatapp.Event
		if err = wsjson.Read(ctx, conn, &first); err != nil || first.Type != "session.updated" {
			t.Fatalf("first=%+v err=%v", first, err)
		}
		if err = wsjson.Read(ctx, conn, &second); err != nil || second.Type != "message.created" {
			t.Fatalf("second=%+v err=%v", second, err)
		}
		close(events)
		if err = wsjson.Read(ctx, conn, &second); err == nil {
			t.Error("connection should end after events channel closes")
		}
	})

	t.Run("heartbeat ping gets pong", func(t *testing.T) {
		// port 不产生任何事件，连接保持空闲。
		port := &chatGapPort{chatHandlerCoveragePort: &chatHandlerCoveragePort{}, events: make(chan chatapp.Event), ownedAccount: "acc1"}
		// header、stop、url 分别是握手请求头、清理函数和测试服务地址。
		url, header, stop := chatGapWebSocketServer(t, port, &chatGapAuth{})
		defer stop()
		// ctx 控制客户端读写。
		ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
		defer cancel()
		// conn 是客户端连接。
		conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		// frame 保存服务端帧。
		var frame map[string]any
		if err = wsjson.Read(ctx, conn, &frame); err != nil || frame["type"] != "ready" {
			t.Fatalf("ready=%v err=%v", frame, err)
		}
		// 非心跳帧（非 JSON、其他类型）被忽略，只有 ping 得到 pong。
		for _, payload := range []string{"not-json", `{"type":"noop"}`, `{"type":"ping"}`} {
			if err = conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
				t.Fatal(err)
			}
		}
		if err = wsjson.Read(ctx, conn, &frame); err != nil || frame["type"] != "pong" {
			t.Fatalf("pong=%v err=%v", frame, err)
		}
	})

	// failure 描述会话校验在第几次调用开始失败及期望关闭码。
	failures := []struct {
		// name 是子场景名称。
		name string
		// failFrom 是开始失败的校验序号。
		failFrom int32
		// err 是失败错误。
		err error
		// wantClose 是期望的 WebSocket 关闭码。
		wantClose websocket.StatusCode
		// sendEvent 表示是否要先发送一个事件触发写前校验。
		sendEvent bool
	}{
		{"revoked before ready", 2, accountapp.ErrSessionInvalid, websocket.StatusPolicyViolation, false},
		{"infra error before ready", 2, errors.New("db down"), websocket.StatusInternalError, false},
		{"revoked before event", 3, accountapp.ErrSessionInvalid, websocket.StatusPolicyViolation, true},
		{"infra error before event", 3, errors.New("db down"), websocket.StatusInternalError, true},
	}
	// tc 表示当前会话失效场景。
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			// events 是事件通道。
			events := make(chan chatapp.Event, 1)
			// port 只承认 acc1。
			port := &chatGapPort{chatHandlerCoveragePort: &chatHandlerCoveragePort{}, events: events, ownedAccount: "acc1"}
			// header、stop、url 分别保存本步骤的返回值：header为携带会话 Cookie 的 WebSocket 握手请求头；stop为释放测试服务与监听端口的清理函数；url为测试服务的 WebSocket 连接地址
			url, header, stop := chatGapWebSocketServer(t, port, &chatGapAuth{failFrom: tc.failFrom, err: tc.err})
			defer stop()
			// ctx 控制客户端读写。
			ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
			defer cancel()
			// conn 是客户端连接。
			conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
			if tc.sendEvent {
				// ready 保存就绪帧。
				var ready map[string]any
				if err = wsjson.Read(ctx, conn, &ready); err != nil {
					t.Fatal(err)
				}
				events <- chatapp.Event{Type: "message.created", Message: &chatapp.Message{AccountID: "acc1"}}
			}
			// frame 读取到连接被服务端关闭为止。
			var frame map[string]any
			err = wsjson.Read(ctx, conn, &frame)
			if websocket.CloseStatus(err) != tc.wantClose {
				t.Errorf("close status=%v err=%v want %v", websocket.CloseStatus(err), err, tc.wantClose)
			}
		})
	}

	t.Run("initial validation and subscribe errors", func(t *testing.T) {
		// 首次校验失败时返回 401。
		srv, _, cleanup := newTestServerWithChat(t)
		defer cleanup()
		// authPort 在第一次校验即失败。
		authPort := &chatGapAuth{AuthenticationPort: srv.applications.authentication, failFrom: 1, err: accountapp.ErrSessionInvalid}
		srv.applications.authentication = authPort
		srv.applications.chat = &chatGapPort{chatHandlerCoveragePort: &chatHandlerCoveragePort{}, ownedAccount: "acc1"}
		// handler 是真实路由。
		handler := srv.Router()
		// cookie 是管理员会话。
		cookie := loginHelper(t, handler)
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/ws", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("revoked status=%d", rec.Code)
		}
		authPort.failFrom = 0
		srv.applications.chat = &chatGapPort{chatHandlerCoveragePort: &chatHandlerCoveragePort{}, subscribeErr: errors.New("subscribe failed")}
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/ws", ""); rec.Code != http.StatusInternalServerError {
			t.Errorf("subscribe error status=%d", rec.Code)
		}
		// 非 WebSocket 升级请求在订阅成功后 Accept 失败并直接返回。
		srv.applications.chat = &chatGapPort{chatHandlerCoveragePort: &chatHandlerCoveragePort{}, events: make(chan chatapp.Event)}
		serveChatCoverageRequest(handler, cookie, "GET", "/api/v1/chat/ws", "")
	})
}
