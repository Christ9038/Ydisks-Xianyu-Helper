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
	"testing"
	"time"

	"github.com/coder/websocket"
)

// startRegServer 启动一个本地 WS 服务，模拟 /reg 握手：读取客户端发来的 /reg 与
// ackDiff 两条消息，验证它们的结构，然后保持连接打开（由调用方决定何时关闭）。
// 返回服务实例与一个收集到所有客户端消息的 channel。
func startRegServer(t *testing.T) (*httptest.Server, chan map[string]any) {
	t.Helper()
	got := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
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
	return srv, got
}

// dialLocal 直接对本地 httptest WS 服务进行 websocket.Dial，返回包装好的 *Conn。
// 与生产 Dial 的差异仅在于不写死 WSURL、不带握手头，但 register/sendJSON 等方法逻辑一致。
func dialLocal(t *testing.T, srv *httptest.Server, cfg Config) *Conn {
	t.Helper()
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	dialed.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = dialed.CloseNow() })
	return &Conn{ws: dialed, cfg: cfg, logger: nilLogger(), recorder: cfg.Recorder}
}

// TestRegister_SendsRegAndAckDiff 直接调用 register 覆盖 /reg 握手 + ackDiff 两条消息。
// 验证：1) /reg 消息含正确 lwp/app-key/token/ua/did；2) ackDiff 消息 lwp 正确且 body 非空。
func TestRegister_SendsRegAndAckDiff(t *testing.T) {
	srv, got := startRegServer(t)
	conn := dialLocal(t, srv, Config{
		CookieStr:   "cookie=1",
		DeviceID:    "device-xyz",
		AccessToken: "token-abc",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := conn.register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}

	var msgs []map[string]any
	timer := time.NewTimer(1500 * time.Millisecond)
	defer timer.Stop()
collect:
	for {
		select {
		case m := <-got:
			msgs = append(msgs, m)
			if len(msgs) >= 2 {
				break collect
			}
		case <-timer.C:
			break collect
		}
	}
	if len(msgs) < 2 {
		t.Fatalf("期望收到 2 条消息，实际 %d: %#v", len(msgs), msgs)
	}

	reg := msgs[0]
	if reg["lwp"] != "/reg" {
		t.Fatalf("首条消息 lwp 应为 /reg，实际 %v", reg["lwp"])
	}
	headers, _ := reg["headers"].(map[string]any)
	if headers["app-key"] != RegAppKey {
		t.Errorf("/reg app-key = %v, 期望 %s", headers["app-key"], RegAppKey)
	}
	if headers["token"] != "token-abc" {
		t.Errorf("/reg token = %v, 期望 token-abc", headers["token"])
	}
	if headers["ua"] != regUA {
		t.Errorf("/reg ua = %v, 期望 %s", headers["ua"], regUA)
	}
	if headers["did"] != "device-xyz" {
		t.Errorf("/reg did = %v, 期望 device-xyz", headers["did"])
	}
	if _, ok := headers["mid"].(string); !ok || headers["mid"] == "" {
		t.Errorf("/reg mid 应为非空字符串, 实际 %v", headers["mid"])
	}

	ackDiff := msgs[1]
	if ackDiff["lwp"] != "/r/SyncStatus/ackDiff" {
		t.Fatalf("第二条消息 lwp 应为 ackDiff, 实际 %v", ackDiff["lwp"])
	}
	body, _ := ackDiff["body"].([]any)
	if len(body) == 0 {
		t.Fatalf("ackDiff body 为空")
	}
	first, _ := body[0].(map[string]any)
	if first["pipeline"] != "sync" || first["channel"] != "sync" {
		t.Errorf("ackDiff body[0] 结构异常: %#v", first)
	}
}

// TestRegister_ContextCancelledDuringWait register 在 1 秒等待期间 ctx 取消应返回 ctx.Err。
func TestRegister_ContextCancelledDuringWait(t *testing.T) {
	srv, _ := startRegServer(t)
	conn := dialLocal(t, srv, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	// 等到 /reg 发出、进入 1 秒等待后再取消。
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err := conn.register(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled, 实际 %v", err)
	}
}

// TestDial_RegisterFailure register 的 ackDiff 发送失败时应返回错误。
// 服务端 accept 后等待 1.5s（超过 register 内 1s 等待）再 CloseNow，
// 使 ackDiff 的 sendJSON 写入已关闭连接而失败。
func TestDial_RegisterFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Logf("dial 直接失败: %v", err)
		return
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)
	conn := &Conn{ws: dialed, cfg: Config{}, logger: nilLogger()}
	err = conn.register(dialCtx)
	if err == nil {
		t.Fatal("register 应在连接关闭时返回错误")
	}
}

// TestSendACK_RepliesWithMidSid 服务端发带 headers(mid/sid) 的消息，ReceiveLoop 回 ACK，
// 服务端读到的 ACK 应含相同 mid/sid 且 code=200。
func TestSendACK_RepliesWithMidSid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 发一条带 mid/sid 的非同步消息（无 syncPushPackage），客户端应回 ACK 但不调 onMessage。
		frame := map[string]any{
			"headers": map[string]any{
				"mid":     "server-mid-1",
				"sid":     "server-sid-1",
				"app-key": "ak",
				"ua":      "ua-1",
				"dt":      "j",
			},
		}
		raw, _ := json.Marshal(frame)
		if err := c.Write(r.Context(), websocket.MessageText, raw); err != nil {
			return
		}
		// 读 ACK。
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var ack map[string]any
		if err := json.Unmarshal(data, &ack); err != nil {
			return
		}
		// 把 ACK 内容包成同步推送帧回写，让客户端解码后回调 onMessage。
		ackBytes, _ := json.Marshal(map[string]any{"ack": ack})
		b64 := base64.StdEncoding.EncodeToString(ackBytes)
		echo, _ := json.Marshal(map[string]any{
			"headers": map[string]any{"mid": "echo"},
			"body":    map[string]any{"syncPushPackage": map[string]any{"data": []any{map[string]any{"data": b64}}}},
		})
		_ = c.Write(r.Context(), websocket.MessageText, echo)
		_, _, _ = c.Read(ctx) // 读掉这条 echo 的 ACK 后关闭
	}))
	t.Cleanup(srv.Close)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	conn := &Conn{ws: dialed, logger: nilLogger()}
	var ackSeen map[string]any
	var onMessageCount int
	loopDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(decrypted map[string]any) {
			onMessageCount++
			if ack, ok := decrypted["ack"].(map[string]any); ok {
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

// TestSendACK_NoHeaders 消息无 headers 时 ACK 用 fallback mid（GenerateMid 非空）且 sid 为空。
func TestSendACK_NoHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// 发一条无 headers 的非同步消息。
		raw, _ := json.Marshal(map[string]any{"lwp": "/!"})
		_ = c.Write(r.Context(), websocket.MessageText, raw)
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var ack map[string]any
		if json.Unmarshal(data, &ack) != nil {
			return
		}
		// 回写 echo 带 ack（包成同步推送帧）。
		ackBytes, _ := json.Marshal(map[string]any{"ack": ack})
		b64 := base64.StdEncoding.EncodeToString(ackBytes)
		echo, _ := json.Marshal(map[string]any{
			"headers": map[string]any{"mid": "echo"},
			"body":    map[string]any{"syncPushPackage": map[string]any{"data": []any{map[string]any{"data": b64}}}},
		})
		_ = c.Write(r.Context(), websocket.MessageText, echo)
		_, _, _ = c.Read(ctx)
	}))
	t.Cleanup(srv.Close)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	conn := &Conn{ws: dialed, logger: nilLogger()}
	var ackSeen map[string]any
	loopDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		loopDone <- conn.ReceiveLoop(ctx, func(d map[string]any) {
			if ack, ok := d["ack"].(map[string]any); ok {
				ackSeen = ack
			}
		})
	}()
	<-loopDone
	if ackSeen == nil {
		t.Fatal("未收到 ack")
	}
	ackHeaders, _ := ackSeen["headers"].(map[string]any)
	mid, _ := ackHeaders["mid"].(string)
	if mid == "" {
		t.Errorf("无 headers 时 ACK mid 应用 fallback (GenerateMid), 实际空")
	}
	if ackHeaders["sid"] != "" {
		t.Errorf("无 headers 时 ACK sid 应为空, 实际 %v", ackHeaders["sid"])
	}
}

// TestHeartbeatLoop_SendFailure 服务端立刻关闭连接，HeartbeatLoop 的 sendJSON 应失败；
// 连续失败达 maxFailures(3) 次后应返回错误。
func TestHeartbeatLoop_SendFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// 立刻关闭，使客户端发送心跳时写失败。
		_ = c.Close(websocket.StatusInternalError, "bye")
	}))
	t.Cleanup(srv.Close)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	conn := &Conn{ws: dialed, logger: nilLogger()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- conn.HeartbeatLoop(ctx, 20*time.Millisecond)
	}()
	select {
	case err := <-loopDone:
		if err == nil {
			t.Fatal("HeartbeatLoop 应在连续失败后返回错误")
		}
		if !strings.Contains(err.Error(), "心跳连续失败") {
			t.Fatalf("期望心跳连续失败错误, 实际 %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("HeartbeatLoop 未在超时内退出")
	}
}

// TestSendText_ServerReceives SendText 应在服务端收到 sendByReceiverScope 消息，且
// content.data 是 base64 编码的 contentType=1 文本内容。
func TestSendText_ServerReceives(t *testing.T) {
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
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

	conn := dialLocal(t, srv, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.SendText(ctx, "100", "conv-1", "200", "你好"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	select {
	case m := <-got:
		if m["lwp"] != "/r/MessageSend/sendByReceiverScope" {
			t.Errorf("lwp = %v", m["lwp"])
		}
		body, _ := m["body"].([]any)
		if len(body) != 2 {
			t.Fatalf("body len = %d, 期望 2", len(body))
		}
		first, _ := body[0].(map[string]any)
		if first["cid"] != "conv-1@goofish" {
			t.Errorf("cid = %v, 期望 conv-1@goofish", first["cid"])
		}
		content, _ := first["content"].(map[string]any)
		if content["contentType"] != float64(101) {
			t.Errorf("contentType = %v, 期望 101", content["contentType"])
		}
		custom, _ := content["custom"].(map[string]any)
		data, _ := custom["data"].(string)
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatalf("custom.data 非 base64: %v", err)
		}
		var inner map[string]any
		if err := json.Unmarshal(decoded, &inner); err != nil {
			t.Fatalf("解码后非 JSON: %v", err)
		}
		if inner["contentType"] != float64(1) {
			t.Errorf("内层 contentType = %v, 期望 1", inner["contentType"])
		}
		text, _ := inner["text"].(map[string]any)
		if text["text"] != "你好" {
			t.Errorf("text = %v, 期望 你好", text["text"])
		}
		// 第二个 body 元素含 actualReceivers。
		second, _ := body[1].(map[string]any)
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
func TestSendImage_ServerReceivesAndDefaults(t *testing.T) {
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
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

	conn := dialLocal(t, srv, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// width/height 传 0/负数，触发默认值分支。
	if err := conn.SendImage(ctx, "100", "conv-1", "200", "https://cdn/img.png", 0, -1); err != nil {
		t.Fatalf("SendImage: %v", err)
	}

	select {
	case m := <-got:
		body, _ := m["body"].([]any)
		first, _ := body[0].(map[string]any)
		content, _ := first["content"].(map[string]any)
		custom, _ := content["custom"].(map[string]any)
		data, _ := custom["data"].(string)
		decoded, _ := base64.StdEncoding.DecodeString(data)
		var inner map[string]any
		if err := json.Unmarshal(decoded, &inner); err != nil {
			t.Fatalf("解码后非 JSON: %v", err)
		}
		if inner["contentType"] != float64(2) {
			t.Errorf("内层 contentType = %v, 期望 2", inner["contentType"])
		}
		image, _ := inner["image"].(map[string]any)
		pics, _ := image["pics"].([]any)
		if len(pics) != 1 {
			t.Fatalf("pics len = %d, 期望 1", len(pics))
		}
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
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
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

	conn := dialLocal(t, srv, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.SendImage(ctx, "100", "conv-1", "200", "u", 1024, 768); err != nil {
		t.Fatalf("SendImage: %v", err)
	}
	select {
	case m := <-got:
		body, _ := m["body"].([]any)
		first, _ := body[0].(map[string]any)
		content, _ := first["content"].(map[string]any)
		custom, _ := content["custom"].(map[string]any)
		data, _ := custom["data"].(string)
		decoded, _ := base64.StdEncoding.DecodeString(data)
		var inner map[string]any
		_ = json.Unmarshal(decoded, &inner)
		image, _ := inner["image"].(map[string]any)
		pics, _ := image["pics"].([]any)
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
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		for {
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

	conn := dialLocal(t, srv, Config{})
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

// TestSetAccessToken_AppliedToSubsequentSend SetAccessToken 后 register/SendText 发送的消息
// 应携带新 token（通过 register 验证，因为 SendText 不直接带 token；这里用 register 覆盖）。
func TestSetAccessToken_AppliedToSubsequentSend(t *testing.T) {
	srv, got := startRegServer(t)
	conn := dialLocal(t, srv, Config{AccessToken: "old-token"})

	// 在线更新 token。
	conn.SetAccessToken("new-token-999")

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := conn.register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}

	select {
	case m := <-got:
		if m["lwp"] != "/reg" {
			t.Fatalf("期望首条为 /reg, 实际 %v", m["lwp"])
		}
		headers, _ := m["headers"].(map[string]any)
		if headers["token"] != "new-token-999" {
			t.Errorf("/reg token = %v, 期望 new-token-999 (在线更新后)", headers["token"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到 /reg")
	}
}

// TestSetRecorder_RecordsOutgoingAndIncoming SetRecorder 后 outgoing/incoming 都应触发回调。
func TestSetRecorder_RecordsOutgoingAndIncoming(t *testing.T) {
	payload := `{"event":"paid"}`
	srv := startWSEchoServer(t, payload)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.CloseNow()
	dialed.SetReadLimit(8 << 20)

	var mu sync.Mutex
	var records []struct {
		dir, parsed, status string
	}
	rec := func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
		mu.Lock()
		defer mu.Unlock()
		records = append(records, struct {
			dir, parsed, status string
		}{direction, parsedJSON, parseStatus})
	}
	conn := &Conn{ws: dialed, logger: nilLogger()}
	// 通过 SetRecorder 方法设置（覆盖该方法），而非直接赋值字段。
	conn.SetRecorder(rec)
	if conn.recorder == nil {
		t.Fatal("SetRecorder 未设置 recorder")
	}

	loopDone := make(chan error, 1)
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
	hasOut := false
	hasIn := false
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	dialed, _, err := websocket.Dial(dialCtx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	dialed.SetReadLimit(8 << 20)
	conn := &Conn{ws: dialed, logger: nilLogger()}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close 后 Read 应失败。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := dialed.Read(ctx); err == nil {
		t.Fatal("Close 后 Read 应返回错误")
	}
}

// TestSendJSON_MarshalError sendJSON 传入不可序列化值（如 channel）应返回 marshal 错误。
func TestSendJSON_MarshalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
	}))
	t.Cleanup(srv.Close)

	conn := dialLocal(t, srv, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// channel 不可被 json.Marshal。
	err := conn.sendJSON(ctx, map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("sendJSON 应在 marshal 失败时返回错误")
	}
}
