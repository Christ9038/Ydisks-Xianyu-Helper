package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// startWSEchoServer 启动一个本地 WS 服务：升级后发送一条同步推送消息，再读取并忽略 ACK，
// 最后关闭连接。返回服务 URL。用于驱动 ReceiveLoop 的消息分发测试。
func startWSEchoServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 构造同步推送帧：body.syncPushPackage.data[0].data = base64(payload)。
		b64 := base64.StdEncoding.EncodeToString([]byte(payload))
		frame := map[string]any{
			"headers": map[string]any{"mid": "m1", "sid": "s1"},
			"body": map[string]any{"syncPushPackage": map[string]any{
				"data": []any{map[string]any{"data": b64}},
			}},
		}
		raw, _ := json.Marshal(frame)
		if err := c.Write(r.Context(), websocket.MessageText, raw); err != nil {
			return
		}
		// 读掉 ACK 后关闭（触发客户端 Read 返回错误，结束 ReceiveLoop）。
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, _, _ = c.Read(ctx)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsURL 把 httptest 的 http:// URL 转成 ws://。
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// TestReceiveLoop_DecodesSyncPayload 验证 ReceiveLoop 收到同步推送帧后：
// 解码 base64+JSON → 调用 onMessage → 回 ACK（服务端能读到）→ 连接关闭后退出。
func TestReceiveLoop_DecodesSyncPayload(t *testing.T) {
	payload := `{"event":"paid","order_id":"o1"}`
	srv := startWSEchoServer(t, payload)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	conn := newConn(dialed, Config{}, nilLogger())

	var got map[string]any
	loopDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(decrypted map[string]any) {
			got = decrypted
		})
	}()

	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("ReceiveLoop 未在超时内退出")
	}
	if got == nil || got["event"] != "paid" || got["order_id"] != "o1" {
		t.Fatalf("onMessage 未收到解码结果: %#v", got)
	}
}

// TestReceiveLoop_NonJSONSkipped 非 JSON 消息应被跳过，不回调 onMessage，循环继续。
func TestReceiveLoop_NonJSONSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 先发一条非 JSON，再发一条合法同步推送。
		c.Write(r.Context(), websocket.MessageText, []byte("not-json"))
		b64 := base64.StdEncoding.EncodeToString([]byte(`{"ok":true}`))
		frame := map[string]any{
			"headers": map[string]any{"mid": "m2"},
			"body":    map[string]any{"syncPushPackage": map[string]any{"data": []any{map[string]any{"data": b64}}}},
		}
		raw, _ := json.Marshal(frame)
		c.Write(r.Context(), websocket.MessageText, raw)
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, _, _ = c.Read(ctx) // ACK
	}))
	defer srv.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	conn := newConn(dialed, Config{}, nilLogger())
	var got map[string]any
	loopDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(decrypted map[string]any) { got = decrypted })
	}()
	<-loopDone
	if got == nil || got["ok"] != true {
		t.Fatalf("应跳过非 JSON 并处理合法帧: %#v", got)
	}
}

// TestHeartbeatLoop_ContextCancel HeartbeatLoop 应在 ctx 取消时及时退出。
func TestHeartbeatLoop_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 持续读，忽略心跳。
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	conn := newConn(dialed, Config{}, nilLogger())
	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- conn.HeartbeatLoop(ctx, 50*time.Millisecond)
	}()
	// 让心跳发几次再取消。
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-loopDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("HeartbeatLoop 退出 err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HeartbeatLoop 未在取消后退出")
	}
}

// nilLogger 返回一个丢弃所有输出的 slog.Logger，避免测试输出刷屏。
func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
