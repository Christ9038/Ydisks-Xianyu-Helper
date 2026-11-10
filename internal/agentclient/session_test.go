package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestSessionReusesConnectionAcrossTurnRequests 验证同一 Session 的 message 与多次 poll 请求复用一条连接，
// 非 2xx 响应被丢弃正文后连接仍可复用，Close 后再请求会重新拨号。
func TestSessionReusesConnectionAcrossTurnRequests(t *testing.T) {
	// newConns 统计服务端观察到的新建 TCP 连接数。
	newConns := &atomic.Int32{}
	// calls 统计收到的请求数，用于让第 3 次请求返回 503。
	calls := &atomic.Int32{}
	// auths 记录每次请求的 Authorization 头，确认令牌在复用连接上仍随每个请求发送。
	auths := make(chan string, 8)
	// server 是本地测试 Agent：第 3 次请求返回带正文的 503，其余返回 working 响应。
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths <- r.Header.Get("Authorization")
		// decoded 保存请求体，确认协议版本由 Session 填充。
		var decoded Request
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		if decoded.Version != ProtocolVersion {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy, retry later"))
			return
		}
		_, _ = w.Write([]byte(`{"working":true}`))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	// session、err 保存本轮会话及创建错误。
	session, err := NewSession(Endpoint{URL: server.URL, Token: "tok", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	// sequence 是本轮第几次请求。
	for sequence := 1; sequence <= 4; sequence++ {
		// response、sendErr 保存本次请求结果。
		response, sendErr := session.Send(context.Background(), Request{Event: EventPoll, TurnID: "t1", Sequence: sequence})
		if sequence == 3 {
			// statusErr 保存非 2xx 错误，用于确认状态码而不暴露正文。
			var statusErr *StatusError
			if !errors.As(sendErr, &statusErr) || statusErr.Code != http.StatusServiceUnavailable {
				t.Fatalf("第 3 次应返回 503 状态错误，得到 %v", sendErr)
			}
			continue
		}
		if sendErr != nil || !response.Working {
			t.Fatalf("第 %d 次请求失败: response=%+v err=%v", sequence, response, sendErr)
		}
	}
	// got 是本轮 4 次请求期间服务端观察到的新建连接数。
	if got := newConns.Load(); got != 1 {
		t.Fatalf("一轮内 4 次请求（含一次 503）应复用一条连接，实际新建 %d 条", got)
	}
	// i 遍历已记录的请求头。
	for i := 0; i < 4; i++ {
		// auth 是第 i 次请求携带的 Authorization 头。
		if auth := <-auths; auth != "Bearer tok" {
			t.Fatalf("第 %d 次请求缺少令牌头: %q", i+1, auth)
		}
	}
	session.Close()
	session.Close()
	// err 保存 Close 之后再次请求的错误，期望成功并重新拨号。
	if _, err := session.Send(context.Background(), Request{Event: EventPoll}); err != nil {
		t.Fatal(err)
	}
	// got 是 Close 后再次请求时累计的新建连接数。
	if got := newConns.Load(); got != 2 {
		t.Fatalf("Close 后应重新拨号，实际新建 %d 条", got)
	}
}

// TestNewSessionRejectsInvalidURL 验证非法地址不创建会话，nil 会话 Close 安全。
func TestNewSessionRejectsInvalidURL(t *testing.T) {
	// rawURL 是逐个校验的非法地址。
	for _, rawURL := range []string{"", "ftp://agent.example.com", "https://user:pass@agent.example.com"} {
		// session、err 保存创建结果。
		session, err := NewSession(Endpoint{URL: rawURL, Timeout: time.Second})
		if err == nil || session != nil {
			t.Fatalf("非法地址 %q 应被拒绝: session=%v err=%v", rawURL, session, err)
		}
	}
	// nilSession 验证 nil 接收者 Close 不会崩溃。
	var nilSession *Session
	nilSession.Close()
}
