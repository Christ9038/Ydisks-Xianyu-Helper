package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"xianyu-go/internal/xianyu"
)

// startRegServer 启动一个本地 WS 服务，模拟 /reg 握手并保持连接打开。
// 返回服务实例与一个收集到所有客户端消息的 channel。
// startRegServer 封装开始RegServer业务协调。
func startRegServer(t *testing.T) (*httptest.Server, chan map[string]any) {
	t.Helper()
	// got 用于本次流程后续判断的got
	got := make(chan map[string]any, 8)
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		for {
			// data、err 用于本次流程后续判断的data、err
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			// m 用于本次流程后续判断的m
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				select {
				case got <- m:
				default:
				}
				if m["lwp"] == "/reg" {
					// headers 用于本次流程后续判断的headers
					headers, _ := m["headers"].(map[string]any)
					// response 用于本次流程后续判断的响应
					response := map[string]any{
						"code": 200,
						"headers": map[string]any{
							"mid":     headers["mid"],
							"reg-uid": "123@goofish",
						},
					}
					// raw 用于本次流程后续判断的原始
					raw, _ := json.Marshal(response)
					if // err 用于本次流程后续判断的err
					err := c.Write(ctx, websocket.MessageText, raw); err != nil {
						return
					}
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// dialLocal 直接对本地 httptest WS 服务进行 websocket.Dial，返回包装好的 *Conn。
// 与生产 Dial 的差异仅在于不写死 WSURL、不带握手头，但 register/sendJSON 等方法逻辑一致。
// dialLocal 封装dialLocal业务协调。
func dialLocal(t *testing.T, srv *httptest.Server, cfg Config) *Conn {
	t.Helper()
	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	dialed.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = dialed.CloseNow() })
	return newConn(dialed, cfg, nilLogger())
}

// TestOpenBatchRacesDelayedConnections 验证官网 batchConnectWs 会在首条握手
// 迟滞时启动后续连接，并采用最先成功者。
// TestOpenBatchRacesDelayedConnections 封装TestOpen批次RacesDelayedConnections业务协调。
func TestOpenBatchRacesDelayedConnections(t *testing.T) {
	// originalDelays 用于本次流程后续判断的originalDelays
	originalDelays := batchConnectDelays
	batchConnectDelays = []time.Duration{0, 20 * time.Millisecond, 60 * time.Millisecond}
	t.Cleanup(func() { batchConnectDelays = originalDelays })

	// attempts 用于本次流程后续判断的尝试次数
	var attempts atomic.Int32
	// firstAttempt 用于本次流程后续判断的first尝试次数
	firstAttempt := make(chan struct{})
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 用于本次流程后续判断的尝试次数
		attempt := attempts.Add(1)
		if attempt == 1 {
			close(firstAttempt)
			<-r.Context().Done()
			return
		}
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	// openResult 用于本次流程后续判断的open结果
	type openResult struct {
		conn *Conn
		err  error
	}
	// resultCh 用于本次流程后续判断的结果Ch
	resultCh := make(chan openResult, 1)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		// conn、err 用于本次流程后续判断的conn、err
		conn, err := openBatch(ctx, wsURL(srv), Config{}, nilLogger())
		resultCh <- openResult{conn: conn, err: err}
	}()

	select {
	case <-firstAttempt:
	case <-time.After(time.Second):
		t.Fatal("WebSocket 首次握手未开始")
	}
	select {
	case // result 用于本次流程后续判断的结果
	result := <-resultCh:
		if result.err != nil {
			t.Fatalf("openBatch: %v", result.err)
		}
		t.Cleanup(func() { _ = result.conn.ws.CloseNow() })
	case <-time.After(time.Second):
		t.Fatal("后续竞速连接未在首条握手阻塞时成功")
	}
	if // got 用于本次流程后续判断的got
	got := attempts.Load(); got < 2 {
		t.Fatalf("WebSocket 握手次数=%d，期望至少启动 2 条竞速连接", got)
	}
}

// TestOpenBatchFirstFailureWins mirrors Promise.race: a fast failed handshake
// rejects the whole batch even when a later candidate could have connected.
// TestOpenBatchFirstFailureWins 封装TestOpen批次FirstFailureWins业务协调。
func TestOpenBatchFirstFailureWins(t *testing.T) {
	// originalDelays 用于本次流程后续判断的originalDelays
	originalDelays := batchConnectDelays
	batchConnectDelays = []time.Duration{0, 100 * time.Millisecond}
	t.Cleanup(func() { batchConnectDelays = originalDelays })

	// attempts 用于本次流程后续判断的尝试次数
	var attempts atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "handshake rejected", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// conn、err 用于本次流程后续判断的conn、err
	conn, err := openBatch(ctx, wsURL(srv), Config{}, nilLogger())
	if err == nil || conn != nil {
		t.Fatalf("openBatch conn=%v err=%v，期望首个握手失败直接结束", conn, err)
	}
	time.Sleep(150 * time.Millisecond)
	if // got 用于本次流程后续判断的got
	got := attempts.Load(); got != 2 {
		t.Fatalf("官网延迟任务应继续启动，实际握手 %d 次，期望 2", got)
	}
}

// TestRegisterSendsOnlyOfficialReg 验证注册只发送 /reg，不再伪造 ackDiff。
func TestRegisterSendsOnlyOfficialReg(t *testing.T) {
	// rawUA 用于本次流程后续判断的原始UA
	rawUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/138.0.7204.92 Safari/537.36"
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: rawUA})
	// srv、got 用于本次流程后续判断的srv、got
	srv, got := startRegServer(t)
	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{
		CookieStr:   "cookie=1",
		DeviceID:    "device-xyz",
		AccessToken: "token%2Fabc%2Braw",
	})

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	err := conn.register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}

	// msgs 用于本次流程后续判断的msgs
	var msgs []map[string]any
	// timer 用于本次流程后续判断的定时器
	timer := time.NewTimer(1500 * time.Millisecond)
	defer timer.Stop()
collect:
	for {
		select {
		case // m 用于本次流程后续判断的m
		m := <-got:
			msgs = append(msgs, m)
			if len(msgs) >= 1 {
				break collect
			}
		case <-timer.C:
			break collect
		}
	}
	if len(msgs) != 1 {
		t.Fatalf("期望只收到 /reg，实际 %d: %#v", len(msgs), msgs)
	}

	// reg 用于本次流程后续判断的reg
	reg := msgs[0]
	if reg["lwp"] != "/reg" {
		t.Fatalf("首条消息 lwp 应为 /reg，实际 %v", reg["lwp"])
	}
	// headers 用于本次流程后续判断的headers
	headers, _ := reg["headers"].(map[string]any)
	if headers["app-key"] != RegAppKey {
		t.Errorf("/reg app-key = %v, 期望 %s", headers["app-key"], RegAppKey)
	}
	if headers["token"] != "token/abc+raw" {
		t.Errorf("/reg token = %v, 期望 decodeURIComponent 后的 token/abc+raw", headers["token"])
	}
	if headers["ua"] != OfficialRegistrationUA(rawUA) {
		t.Errorf("/reg ua = %v, 期望官方复合 UA", headers["ua"])
	}
	if headers["did"] != "device-xyz" {
		t.Errorf("/reg did = %v, 期望 device-xyz", headers["did"])
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := headers["mid"].(string); !ok || headers["mid"] == "" {
		t.Errorf("/reg mid 应为非空字符串, 实际 %v", headers["mid"])
	}

}

// TestRegisterRejectsDecodeURIComponentInvalidUTF8 封装TestRegisterRejectsDecodeURIComponentInvalidUTF8业务协调。
func TestRegisterRejectsDecodeURIComponentInvalidUTF8(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv, _ := startRegServer(t)
	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{DeviceID: "did", AccessToken: "%FF"})
	// err 用于本次流程后续判断的err
	err := conn.register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "非法 UTF-8") {
		t.Fatalf("register error=%v", err)
	}
}

// TestRegister_ContextCancelledDuringWait register 等不到响应时应服从 ctx 取消。
func TestRegister_ContextCancelledDuringWait(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_, _, _ = c.Read(r.Context())
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	// err 用于本次流程后续判断的err
	err := conn.register(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled, 实际 %v", err)
	}
}

// TestRegisterResponseWinsImmediateClose 封装TestRegister响应WinsImmediateClose业务协调。
func TestRegisterResponseWinsImmediateClose(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		// raw、err 用于本次流程后续判断的raw、err
		_, raw, err := c.Read(r.Context())
		if err != nil {
			return
		}
		// request 用于本次流程后续判断的请求
		var request map[string]any
		if json.Unmarshal(raw, &request) != nil {
			return
		}
		// headers 用于本次流程后续判断的headers
		headers, _ := request["headers"].(map[string]any)
		// response 用于本次流程后续判断的响应
		response, _ := json.Marshal(map[string]any{
			"code": 200, "headers": map[string]any{"mid": headers["mid"], "reg-uid": "123@goofish"},
		})
		_ = c.Write(r.Context(), websocket.MessageText, response)
	}))
	t.Cleanup(srv.Close)
	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{DeviceID: "did", AccessToken: "token"})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	err := conn.register(ctx); err != nil {
		t.Fatalf("服务端响应后立即断链不应覆盖成功响应: %v", err)
	}
}

// TestRequestTimeoutIncludesSendPhase 验证请求超时从等待发送权开始计算，
// 不会因另一条半开连接写入占用发送权而永久阻塞。
// TestRequestTimeoutIncludesSendPhase 封装Test请求TimeoutIncludesSendPhase业务协调。
func TestRequestTimeoutIncludesSendPhase(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})

	conn.sendGate <- struct{}{}
	// started 用于本次流程后续判断的started
	started := time.Now()
	// err 用于本次流程后续判断的err
	_, err := conn.request(context.Background(), "/!", map[string]any{}, nil, 60*time.Millisecond)
	// elapsed 用于本次流程后续判断的elapsed
	elapsed := time.Since(started)
	<-conn.sendGate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request error=%v，期望 context.DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("发送阶段未及时超时，耗时 %v", elapsed)
	}
	conn.pendingMu.Lock()
	// pending 用于本次流程后续判断的pending
	pending := len(conn.pending)
	conn.pendingMu.Unlock()
	if pending != 0 {
		t.Fatalf("请求超时后仍残留 %d 个 pending", pending)
	}
}

// TestRegisterRejectsInvalidToken 封装TestRegisterRejectsInvalid令牌业务协调。
func TestRegisterRejectsInvalidToken(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		// data、err 用于本次流程后续判断的data、err
		_, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		// reg 用于本次流程后续判断的reg
		var reg map[string]any
		_ = json.Unmarshal(data, &reg)
		// headers 用于本次流程后续判断的headers
		headers, _ := reg["headers"].(map[string]any)
		// response 用于本次流程后续判断的响应
		response, _ := json.Marshal(map[string]any{
			"code":    401,
			"headers": map[string]any{"mid": headers["mid"]},
			"body":    map[string]any{"reason": "invalid token"},
		})
		_ = c.Write(r.Context(), websocket.MessageText, response)
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{AccessToken: "rejected"})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// err 用于本次流程后续判断的err
	err := conn.register(ctx)
	if !IsInvalidTokenError(err) {
		t.Fatalf("register error=%v, want invalid token", err)
	}
}

// TestRegisterBuffersFrameBeforeResponse 封装TestRegisterBuffersFrameBefore响应业务协调。
func TestRegisterBuffersFrameBeforeResponse(t *testing.T) {
	// push 用于本次流程后续判断的push
	push := map[string]any{"lwp": "/push/test", "headers": map[string]any{"mid": "push-1"}}
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		// data、err 用于本次流程后续判断的data、err
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		// reg 用于本次流程后续判断的reg
		var reg map[string]any
		_ = json.Unmarshal(data, &reg)
		// headers 用于本次流程后续判断的headers
		headers, _ := reg["headers"].(map[string]any)
		// pushRaw 用于本次流程后续判断的push原始
		pushRaw, _ := json.Marshal(push)
		_ = c.Write(ctx, websocket.MessageText, pushRaw)
		// response 用于本次流程后续判断的响应
		response, _ := json.Marshal(map[string]any{"code": 200, "headers": map[string]any{"mid": headers["mid"]}})
		_ = c.Write(ctx, websocket.MessageText, response)
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	err := conn.register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	// got 用于本次流程后续判断的got
	var got map[string]any
	select {
	case // frame 用于本次流程后续判断的frame
	frame := <-conn.pushes:
		got = frame.parsed
	case <-ctx.Done():
		t.Fatalf("read buffered frame: %v", ctx.Err())
	}
	if got["lwp"] != push["lwp"] {
		t.Fatalf("buffered frame=%v want=%v", got, push)
	}
}

// TestDial_RegisterFailure register 的 ackDiff 发送失败时应返回错误。
// 服务端 accept 后等待 1.5s（超过 register 内 1s 等待）再 CloseNow，
// 使 ackDiff 的 sendJSON 写入已关闭连接而失败。
// TestDial_RegisterFailure 封装TestDialRegisterFailure业务协调。
func TestDial_RegisterFailure(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// 在客户端 1s 等待期间关闭连接，使 ackDiff 的 sendJSON 写入失败。
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = c.CloseNow()
		}()
	}))
	t.Cleanup(srv.Close)

	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Logf("dial 直接失败: %v", err)
		return
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)
	// conn 用于本次流程后续判断的conn
	conn := newConn(dialed, Config{}, nilLogger())
	err = conn.register(dialCtx)
	if err == nil {
		t.Fatal("register 应在连接关闭时返回错误")
	}
}

// TestSendACK_RepliesWithMidSid 服务端发带 headers(mid/sid) 的消息，ReceiveLoop 回 ACK，
// 服务端读到的 ACK 应含相同 mid/sid 且 code=200。
// TestSendACK_RepliesWithMidSid 封装TestSendACK回复列表WithMidSid业务协调。
func TestSendACK_RepliesWithMidSid(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 发一条带 mid/sid 的非同步消息（无 syncPushPackage），客户端应回 ACK 但不调 onMessage。
		frame := map[string]any{
			"lwp": "/push/test",
			"headers": map[string]any{
				"mid":     "server-mid-1",
				"sid":     "server-sid-1",
				"app-key": "ak",
				"ua":      "ua-1",
				"dt":      "j",
			},
		}
		// raw 用于本次流程后续判断的原始
		raw, _ := json.Marshal(frame)
		if // err 用于本次流程后续判断的err
		err := c.Write(r.Context(), websocket.MessageText, raw); err != nil {
			return
		}
		// 读 ACK。
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		// data、err 用于本次流程后续判断的data、err
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		// ack 用于本次流程后续判断的ack
		var ack map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(data, &ack); err != nil {
			return
		}
		// 把 ACK 内容包成同步推送帧回写，让客户端解码后回调 onMessage。
		ackBytes, _ := json.Marshal(map[string]any{"ack": ack})
		// b64 用于本次流程后续判断的b64
		b64 := base64.StdEncoding.EncodeToString(ackBytes)
		// echo 用于本次流程后续判断的echo
		echo, _ := json.Marshal(map[string]any{
			"lwp":     "/s/sync",
			"headers": map[string]any{"mid": "echo"},
			"body":    map[string]any{"syncPushPackage": map[string]any{"data": []any{map[string]any{"data": b64}}}},
		})
		_ = c.Write(r.Context(), websocket.MessageText, echo)
		_, _, _ = c.Read(ctx) // 读掉这条 echo 的 ACK 后关闭
	}))
	t.Cleanup(srv.Close)

	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	// conn 用于本次流程后续判断的conn
	conn := newConn(dialed, Config{}, nilLogger())
	// ackSeen 用于本次流程后续判断的ackSeen
	var ackSeen map[string]any
	// onMessageCount 用于本次流程后续判断的on消息数量
	var onMessageCount int
	// loopDone 用于本次流程后续判断的loopDone
	loopDone := make(chan error, 1)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(decrypted map[string]any) {
			onMessageCount++
			if // ack、ok 用于本次流程后续判断的ack、ok
			ack, ok := decrypted["ack"].(map[string]any); ok {
				ackSeen = ack
			}
		})
	}()
	<-loopDone

	// 第二条消息是 echo（解码后含 ack），所以 onMessage 被调用一次。
	if onMessageCount != 1 {
		t.Fatalf("onMessage 调用次数 = %d, 期望 1", onMessageCount)
	}
	if ackSeen == nil {
		t.Fatal("未收到 echo 的 ack")
	}
	if ackSeen["code"] != float64(200) {
		t.Errorf("ACK code = %v, 期望 200", ackSeen["code"])
	}
	// ackHeaders 用于本次流程后续判断的ackHeaders
	ackHeaders, _ := ackSeen["headers"].(map[string]any)
	if ackHeaders["mid"] != "server-mid-1" {
		t.Errorf("ACK mid = %v, 期望 server-mid-1", ackHeaders["mid"])
	}
	if ackHeaders["sid"] != "server-sid-1" {
		t.Errorf("ACK sid = %v, 期望 server-sid-1", ackHeaders["sid"])
	}
	if ackHeaders["app-key"] != "ak" {
		t.Errorf("ACK 应回传 app-key, 实际 %v", ackHeaders["app-key"])
	}
}

// TestReceiveLoop_NoHeadersIsIgnored 官网只把同时包含 lwp/headers 的帧当 Push。
func TestReceiveLoop_NoHeadersIsIgnored(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 发一条无 headers 的非同步消息。
		raw, _ := json.Marshal(map[string]any{"lwp": "/!"})
		_ = c.Write(r.Context(), websocket.MessageText, raw)
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, _, _ = c.Read(ctx)
	}))
	t.Cleanup(srv.Close)

	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	// conn 用于本次流程后续判断的conn
	conn := newConn(dialed, Config{}, nilLogger())
	// called 用于本次流程后续判断的called
	var called bool
	// loopDone 用于本次流程后续判断的loopDone
	loopDone := make(chan error, 1)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(map[string]any) { called = true })
	}()
	<-loopDone
	if called {
		t.Fatal("无 headers 的帧不应进入 Push 回调")
	}
}

// TestReceiveLoop_OfficialControlPushesCloseSession 封装TestReceiveLoopOfficialControlPushesClose会话业务协调。
func TestReceiveLoop_OfficialControlPushesCloseSession(t *testing.T) {
	// tests 用于本次流程后续判断的tests
	tests := []struct {
		name       string
		lwp        string
		matchesErr func(error) bool
	}{
		{name: "kickout", lwp: "/push/kickout", matchesErr: IsAuthenticationError},
		{name: "session remove", lwp: "/s/session/remove", matchesErr: IsConnectLimitError},
	}
	// tt 表示当前遍历过程中的tt
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// srv 用于本次流程后续判断的srv
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// c、err 用于本次流程后续判断的c、err
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				// frame 用于本次流程后续判断的frame
				frame, _ := json.Marshal(map[string]any{
					"lwp": tt.lwp, "headers": map[string]any{"mid": "control-1", "sid": "s1"},
				})
				_ = c.Write(r.Context(), websocket.MessageText, frame)
				// 官网控制 handler 先发起 close，随后 LWP 的 ACK 尝试通常因
				// readyState=CLOSING 失败；紧接着断链也不能吞掉控制事件。
			}))
			defer srv.Close()

			// dialCtx、cancel 用于本次流程后续判断的dialCtx、cancel
			dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			// dialed、err 用于本次流程后续判断的dialed、err
			dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dialed.CloseNow()
			// conn 用于本次流程后续判断的conn
			conn := newConn(dialed, Config{}, nilLogger())
			err = conn.ReceiveLoop(dialCtx, nil)
			if !tt.matchesErr(err) {
				t.Fatalf("ReceiveLoop err=%v", err)
			}
		})
	}
}

// TestHeartbeatLoop_SendFailure 服务端关闭后心跳立即结束，不做三次写失败重试。
func TestHeartbeatLoop_SendFailure(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// 立刻关闭，使客户端发送心跳时写失败。
		_ = c.Close(websocket.StatusInternalError, "bye")
	}))
	t.Cleanup(srv.Close)

	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	// conn 用于本次流程后续判断的conn
	conn := newConn(dialed, Config{}, nilLogger())
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// loopDone 用于本次流程后续判断的loopDone
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- conn.HeartbeatLoop(ctx, 20*time.Millisecond)
	}()
	select {
	case // err 用于本次流程后续判断的err
	err := <-loopDone:
		if err == nil {
			t.Fatal("HeartbeatLoop 应在连续失败后返回错误")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("HeartbeatLoop 未在超时内退出")
	}
}

// TestSendText_ServerReceives SendText 应在服务端收到 sendByReceiverScope 消息，且
// content.data 是 base64 编码的 contentType=1 文本内容。
// TestSendText_ServerReceives 封装TestSend文本ServerReceives业务协调。
func TestSendText_ServerReceives(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := make(chan map[string]any, 4)
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			// data、err 用于本次流程后续判断的data、err
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			// m 用于本次流程后续判断的m
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				select {
				case got <- m:
				default:
				}
			}
		}
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	err := conn.SendText(ctx, "100", "conv-1", "200", "你好"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	select {
	case // m 用于本次流程后续判断的m
	m := <-got:
		if m["lwp"] != "/r/MessageSend/sendByReceiverScope" {
			t.Errorf("lwp = %v", m["lwp"])
		}
		// body 用于本次流程后续判断的请求体
		body, _ := m["body"].([]any)
		if len(body) != 2 {
			t.Fatalf("body len = %d, 期望 2", len(body))
		}
		// first 用于本次流程后续判断的first
		first, _ := body[0].(map[string]any)
		if first["cid"] != "conv-1@goofish" {
			t.Errorf("cid = %v, 期望 conv-1@goofish", first["cid"])
		}
		// content 用于本次流程后续判断的内容
		content, _ := first["content"].(map[string]any)
		if content["contentType"] != float64(101) {
			t.Errorf("contentType = %v, 期望 101", content["contentType"])
		}
		// custom 用于本次流程后续判断的custom
		custom, _ := content["custom"].(map[string]any)
		// data 用于本次流程后续判断的数据
		data, _ := custom["data"].(string)
		// decoded、err 用于本次流程后续判断的decoded、err
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatalf("custom.data 非 base64: %v", err)
		}
		// inner 用于本次流程后续判断的inner
		var inner map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(decoded, &inner); err != nil {
			t.Fatalf("解码后非 JSON: %v", err)
		}
		if inner["contentType"] != float64(1) {
			t.Errorf("内层 contentType = %v, 期望 1", inner["contentType"])
		}
		// text 用于本次流程后续判断的文本
		text, _ := inner["text"].(map[string]any)
		if text["text"] != "你好" {
			t.Errorf("text = %v, 期望 你好", text["text"])
		}
		// 第二个 body 元素含 actualReceivers。
		second, _ := body[1].(map[string]any)
		// receivers 用于本次流程后续判断的receivers
		receivers, _ := second["actualReceivers"].([]any)
		if len(receivers) != 2 {
			t.Fatalf("actualReceivers len = %d, 期望 2", len(receivers))
		}
		if receivers[0] != "200@goofish" || receivers[1] != "100@goofish" {
			t.Errorf("actualReceivers = %#v", receivers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到消息")
	}
}

// TestSendImage_ServerReceivesAndDefaults SendImage 应在服务端收到 contentType=2 图片内容，
// 且 width/height <= 0 时使用默认值 800/600。
// TestSendImage_ServerReceivesAndDefaults 封装TestSend图片ServerReceivesAndDefaults业务协调。
func TestSendImage_ServerReceivesAndDefaults(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := make(chan map[string]any, 4)
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			// data、err 用于本次流程后续判断的data、err
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			// m 用于本次流程后续判断的m
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				select {
				case got <- m:
				default:
				}
			}
		}
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// width/height 传 0/负数，触发默认值分支。
	if err := conn.SendImage(ctx, "100", "conv-1", "200", "https://cdn/img.png", 0, -1); err != nil {
		t.Fatalf("SendImage: %v", err)
	}

	select {
	case // m 用于本次流程后续判断的m
	m := <-got:
		// body 用于本次流程后续判断的请求体
		body, _ := m["body"].([]any)
		// first 用于本次流程后续判断的first
		first, _ := body[0].(map[string]any)
		// content 用于本次流程后续判断的内容
		content, _ := first["content"].(map[string]any)
		// custom 用于本次流程后续判断的custom
		custom, _ := content["custom"].(map[string]any)
		// data 用于本次流程后续判断的数据
		data, _ := custom["data"].(string)
		// decoded 用于本次流程后续判断的decoded
		decoded, _ := base64.StdEncoding.DecodeString(data)
		// inner 用于本次流程后续判断的inner
		var inner map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(decoded, &inner); err != nil {
			t.Fatalf("解码后非 JSON: %v", err)
		}
		if inner["contentType"] != float64(2) {
			t.Errorf("内层 contentType = %v, 期望 2", inner["contentType"])
		}
		// image 用于本次流程后续判断的图片
		image, _ := inner["image"].(map[string]any)
		// pics 用于本次流程后续判断的pics
		pics, _ := image["pics"].([]any)
		if len(pics) != 1 {
			t.Fatalf("pics len = %d, 期望 1", len(pics))
		}
		// pic 用于本次流程后续判断的pic
		pic, _ := pics[0].(map[string]any)
		if pic["width"] != float64(800) {
			t.Errorf("width = %v, 期望 800 (默认)", pic["width"])
		}
		if pic["height"] != float64(600) {
			t.Errorf("height = %v, 期望 600 (默认)", pic["height"])
		}
		if pic["url"] != "https://cdn/img.png" {
			t.Errorf("url = %v", pic["url"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到图片消息")
	}
}

// TestSendImage_ExplicitDimensions 传入正数 width/height 时应原样使用。
func TestSendImage_ExplicitDimensions(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := make(chan map[string]any, 4)
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			// data、err 用于本次流程后续判断的data、err
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			// m 用于本次流程后续判断的m
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				select {
				case got <- m:
				default:
				}
			}
		}
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	err := conn.SendImage(ctx, "100", "conv-1", "200", "u", 1024, 768); err != nil {
		t.Fatalf("SendImage: %v", err)
	}
	select {
	case // m 用于本次流程后续判断的m
	m := <-got:
		// body 用于本次流程后续判断的请求体
		body, _ := m["body"].([]any)
		// first 用于本次流程后续判断的first
		first, _ := body[0].(map[string]any)
		// content 用于本次流程后续判断的内容
		content, _ := first["content"].(map[string]any)
		// custom 用于本次流程后续判断的custom
		custom, _ := content["custom"].(map[string]any)
		// data 用于本次流程后续判断的数据
		data, _ := custom["data"].(string)
		// decoded 用于本次流程后续判断的decoded
		decoded, _ := base64.StdEncoding.DecodeString(data)
		// inner 用于本次流程后续判断的inner
		var inner map[string]any
		_ = json.Unmarshal(decoded, &inner)
		// image 用于本次流程后续判断的图片
		image, _ := inner["image"].(map[string]any)
		// pics 用于本次流程后续判断的pics
		pics, _ := image["pics"].([]any)
		// pic 用于本次流程后续判断的pic
		pic, _ := pics[0].(map[string]any)
		if pic["width"] != float64(1024) || pic["height"] != float64(768) {
			t.Errorf("width/height = %v/%v, 期望 1024/768", pic["width"], pic["height"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到图片消息")
	}
}

// TestSendChatContent_MissingParams 缺少必要参数时应返回错误，不发消息。
func TestSendChatContent_MissingParams(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := make(chan map[string]any, 4)
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		for {
			// err 用于本次流程后续判断的err
			_, _, err := c.Read(ctx)
			if err != nil {
				return
			}
			select {
			case got <- map[string]any{"unexpected": true}:
			default:
			}
		}
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// myID 空。
	if err := conn.SendText(ctx, "", "cid", "to", "hi"); err == nil {
		t.Fatal("myID 空时应返回错误")
	}
	// cid 空（stripGoofish 后）。
	if err := conn.SendText(ctx, "100", "@goofish", "to", "hi"); err == nil {
		t.Fatal("cid 空时应返回错误")
	}
	// toID 空。
	if err := conn.SendText(ctx, "100", "cid", "", "hi"); err == nil {
		t.Fatal("toID 空时应返回错误")
	}
	select {
	case <-got:
		t.Fatal("参数缺失时不应发送任何消息")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestSetRecorder_RecordsOutgoingAndIncoming SetRecorder 后 outgoing/incoming 都应触发回调。
func TestSetRecorder_RecordsOutgoingAndIncoming(t *testing.T) {
	// payload 用于本次流程后续判断的请求载荷
	payload := `{"event":"paid"}`
	// srv 用于本次流程后续判断的srv
	srv := startWSEchoServer(t, payload)

	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	// mu 用于本次流程后续判断的mu
	var mu sync.Mutex
	// records 用于本次流程后续判断的records
	var records []struct {
		dir, parsed, status string
	}
	// rec 用于本次流程后续判断的rec
	rec := func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
		mu.Lock()
		defer mu.Unlock()
		records = append(records, struct {
			dir, parsed, status string
		}{direction, parsedJSON, parseStatus})
	}
	// conn 用于本次流程后续判断的conn
	conn := newConn(dialed, Config{}, nilLogger())
	// 通过 SetRecorder 方法设置（覆盖该方法），而非直接赋值字段。
	conn.SetRecorder(rec)
	if conn.recorder == nil {
		t.Fatal("SetRecorder 未设置 recorder")
	}

	// loopDone 用于本次流程后续判断的loopDone
	loopDone := make(chan error, 1)
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(map[string]any) {})
	}()

	// 主动发一条心跳（outgoing）触发 recorder("out",...)。
	hbCtx, hbCancel := context.WithTimeout(context.Background(), time.Second)
	defer hbCancel()
	_ = conn.sendJSON(hbCtx, map[string]any{"lwp": "/!"})

	<-loopDone

	mu.Lock()
	defer mu.Unlock()
	// hasOut 用于本次流程后续判断的hasOut
	hasOut := false
	// hasIn 用于本次流程后续判断的hasIn
	hasIn := false
	// r 表示当前遍历过程中的r
	for _, r := range records {
		if r.dir == "out" && r.status == "json" {
			hasOut = true
		}
		if r.dir == "in" && (r.status == "json" || r.status == "decrypted") {
			hasIn = true
		}
	}
	if !hasOut {
		t.Errorf("recorder 未记录 outgoing: %#v", records)
	}
	if !hasIn {
		t.Errorf("recorder 未记录 incoming: %#v", records)
	}
}

// TestClose_TerminatesConnection Close 后底层连接应关闭，后续 Read 返回错误。
func TestClose_TerminatesConnection(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// ctx、cancel 用于本次流程后续判断的ctx、cancel
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			if // err 用于本次流程后续判断的err
			_, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	// dialCtx、dialCancel 用于本次流程后续判断的dialCtx、dial取消
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	// dialed、err 用于本次流程后续判断的dialed、err
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	dialed.SetReadLimit(8 << 20)
	// conn 用于本次流程后续判断的conn
	conn := newConn(dialed, Config{}, nilLogger())

	if // err 用于本次流程后续判断的err
	err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close 后 Read 应失败。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if // err 用于本次流程后续判断的err
	_, _, err := dialed.Read(ctx); err == nil {
		t.Fatal("Close 后 Read 应返回错误")
	}
}

// TestSendJSON_MarshalError sendJSON 传入不可序列化值（如 channel）应返回 marshal 错误。
func TestSendJSON_MarshalError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// c、err 用于本次流程后续判断的c、err
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
	}))
	t.Cleanup(srv.Close)

	// conn 用于本次流程后续判断的conn
	conn := dialLocal(t, srv, Config{})
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// channel 不可被 json.Marshal。
	err := conn.sendJSON(ctx, map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("sendJSON 应在 marshal 失败时返回错误")
	}
}
