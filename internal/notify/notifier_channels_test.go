package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// redirectTransport 将所有请求重定向到指定 httptest.Server，保留原 path/query。
// 用于测试 URL 硬编码（telegram）或 https-only 的渠道。
// redirectTransport 用于本次流程后续判断的redirectTransport
type redirectTransport struct{ target string }

// RoundTrip 封装RoundTrip业务协调。
func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.target
	req.Host = t.target
	return http.DefaultTransport.RoundTrip(req)
}

// failingNotificationTransport 为通知敏感 URL 测试稳定返回网络错误，不访问真实外部服务。
type failingNotificationTransport struct{}

// RoundTrip 返回不含请求 URL 的底层错误，由 http.Client 包装完整 URL 后验证生产脱敏边界。
// req 仅用于满足 RoundTripper 接口，测试不读取或发送其中的敏感地址。
func (failingNotificationTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("simulated connection failure")
}

// notifierWithRedirectClient 构造一个把所有 HTTP 请求重定向到 target 的 Notifier。
func notifierWithRedirectClient(target string) *Notifier {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	n.httpc = &http.Client{Timeout: 10 * time.Second, Transport: &redirectTransport{target: target}}
	return n
}

// readJSONBody 读取并反序列化请求 body。
func readJSONBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	// m 用于本次流程后续判断的m
	var m map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(b))
	}
	return m
}

// ---------------- 钉钉 ----------------

// TestSendDingTalk_WithSecret 带签名时 URL 携带 timestamp/sign。
// 注：生产代码用 "&" 拼接签名参数，需 webhook 已带查询串；此处补一个占位参数。
// TestSendDingTalk_WithSecret 封装TestSendDingTalkWithSecret业务协调。
func TestSendDingTalk_WithSecret(t *testing.T) {
	// gotURL 用于本次流程后续判断的gotURL
	var gotURL string
	// got 用于本次流程后续判断的got
	var got map[string]any
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// webhook 已带占位查询参数，使 "&timestamp=..." 拼接合法。
	cfg := map[string]any{"webhook_url": srv.URL + "?token=x", "secret": "SECxxxx"}
	if // err 用于本次流程后续判断的err
	err := n.sendDingTalk(cfg, "内容"); err != nil {
		t.Fatalf("sendDingTalk: %v", err)
	}
	if !strings.Contains(gotURL, "timestamp=") || !strings.Contains(gotURL, "sign=") {
		t.Errorf("带 secret 时 URL 应包含签名: %s", gotURL)
	}
	if got["msgtype"] != "markdown" {
		t.Errorf("msgtype=%v", got["msgtype"])
	}
}

// TestSendDingTalk_EmptyWebhook webhook 缺失应报错。
func TestSendDingTalk_EmptyWebhook(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendDingTalk(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

// TestSendDingTalk_HTTPError 服务端 4xx/5xx 应返回错误。
func TestSendDingTalk_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendDingTalk(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("4xx 应报错")
	}
}

// TestSendDingTalk_NetworkError 连接失败应返回错误。
func TestSendDingTalk_NetworkError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // 立即关闭，制造连接失败
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendDingTalk(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("网络错误应报错")
	}
}

// TestSendDingTalk_OldConfigFormat 旧格式 webhook 直接是 URL。
func TestSendDingTalk_OldConfigFormat(t *testing.T) {
	// got 用于本次流程后续判断的got
	var got map[string]any
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// config 字段存放旧 URL。
	if err := n.sendDingTalk(map[string]any{"config": srv.URL}, "旧格式"); err != nil {
		t.Fatalf("sendDingTalk 旧格式: %v", err)
	}
	// md 用于本次流程后续判断的md
	md, _ := got["markdown"].(map[string]any)
	if md["text"] != "旧格式" {
		t.Errorf("旧格式正文: %v", md["text"])
	}
}

// ---------------- 飞书 ----------------

// TestSendFeishu 成功 + 带签名 + 空 webhook + HTTP 错误。
func TestSendFeishu(t *testing.T) {
	// got 用于本次流程后续判断的got
	var got map[string]any
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendFeishu(map[string]any{"webhook_url": srv.URL, "secret": "SEC"}, "飞书消息"); err != nil {
		t.Fatalf("sendFeishu: %v", err)
	}
	if got["msg_type"] != "text" {
		t.Errorf("msg_type=%v", got["msg_type"])
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := got["sign"]; !ok {
		t.Errorf("带 secret 应有 sign 字段: %v", got)
	}
	// content 用于本次流程后续判断的内容
	content, _ := got["content"].(map[string]any)
	if content["text"] != "飞书消息" {
		t.Errorf("text=%v", content["text"])
	}
}

// TestSendFeishu_EmptyWebhook 封装TestSendFeishuEmptyWebhook业务协调。
func TestSendFeishu_EmptyWebhook(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendFeishu(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

// TestSendFeishu_HTTPError 封装TestSendFeishuHTTP错误业务协调。
func TestSendFeishu_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendFeishu(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("5xx 应报错")
	}
}

// ---------------- Bark ----------------

// TestSendBark 成功 + 可选字段 + 空 device_key + HTTP 错误。
func TestSendBark(t *testing.T) {
	// got 用于本次流程后续判断的got
	var got map[string]any
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/push" {
			t.Errorf("path=%s want /push", r.URL.Path)
		}
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{
		"server_url": srv.URL,
		"device_key": "KEY",
		"title":      "标题",
		"icon":       "https://img/x.png",
		"url":        "https://click",
		"sound":      "bell",
		"group":      "g1",
	}
	if // err 用于本次流程后续判断的err
	err := n.sendBark(cfg, "正文"); err != nil {
		t.Fatalf("sendBark: %v", err)
	}
	if got["device_key"] != "KEY" || got["title"] != "标题" || got["body"] != "正文" {
		t.Errorf("bark payload: %v", got)
	}
	if got["icon"] != "https://img/x.png" || got["url"] != "https://click" {
		t.Errorf("可选字段缺失: %v", got)
	}
	if got["sound"] != "bell" || got["group"] != "g1" {
		t.Errorf("sound/group: %v", got)
	}
}

// TestSendBark_DefaultServerURL 缺省 server_url 时使用默认 https://api.day.app。
// 用 redirect client 拦截默认 host 的请求。
// TestSendBark_DefaultServerURL 封装TestSendBarkDefaultServerURL业务协调。
func TestSendBark_DefaultServerURL(t *testing.T) {
	// hitPath 用于本次流程后续判断的hit路径
	var hitPath string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	if // err 用于本次流程后续判断的err
	err := n.sendBark(map[string]any{"device_key": "K"}, "x"); err != nil {
		t.Fatalf("sendBark 默认 server: %v", err)
	}
	if hitPath != "/push" {
		t.Errorf("默认 server 应请求 /push, 实际 %s", hitPath)
	}
}

// TestSendBark_EmptyDeviceKey 封装TestSendBarkEmptyDeviceKey业务协调。
func TestSendBark_EmptyDeviceKey(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendBark(map[string]any{"server_url": "https://api.day.app"}, "x"); err == nil {
		t.Fatal("缺 device_key 应报错")
	}
}

// TestSendBark_HTTPError 封装TestSendBarkHTTP错误业务协调。
func TestSendBark_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendBark(map[string]any{"server_url": srv.URL, "device_key": "K"}, "x"); err == nil {
		t.Fatal("5xx 应报错")
	}
}

// ---------------- 企业微信 ----------------

// TestSendWeChat 封装TestSendWe聊天业务协调。
func TestSendWeChat(t *testing.T) {
	// got 用于本次流程后续判断的got
	var got map[string]any
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWeChat(map[string]any{"webhook_url": srv.URL}, "微信消息"); err != nil {
		t.Fatalf("sendWeChat: %v", err)
	}
	if got["msgtype"] != "text" {
		t.Errorf("msgtype=%v", got["msgtype"])
	}
	// text 用于本次流程后续判断的文本
	text, _ := got["text"].(map[string]any)
	if text["content"] != "微信消息" {
		t.Errorf("content=%v", text["content"])
	}
}

// TestSendWeChat_EmptyWebhook 封装TestSendWe聊天EmptyWebhook业务协调。
func TestSendWeChat_EmptyWebhook(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWeChat(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

// TestSendWeChat_HTTPError 封装TestSendWe聊天HTTP错误业务协调。
func TestSendWeChat_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWeChat(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("4xx 应报错")
	}
}

// ---------------- Telegram ----------------

// TestSendTelegram_Success 通过 redirect transport 拦截 telegram API。
func TestSendTelegram_Success(t *testing.T) {
	// got 用于本次流程后续判断的got
	var got map[string]any
	// gotPath 用于本次流程后续判断的got路径
	var gotPath string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	if // err 用于本次流程后续判断的err
	err := n.sendTelegram(map[string]any{"bot_token": "TOK", "chat_id": "123"}, "TG消息"); err != nil {
		t.Fatalf("sendTelegram: %v", err)
	}
	// wantPath 用于本次流程后续判断的want路径
	wantPath := "/botTOK/sendMessage"
	if gotPath != wantPath {
		t.Errorf("path=%s want %s", gotPath, wantPath)
	}
	if got["chat_id"] != "123" || got["text"] != "TG消息" {
		t.Errorf("telegram payload: %v", got)
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := got["parse_mode"]; ok {
		t.Errorf("动态文本不应启用未转义的 HTML 模式: %v", got)
	}
}

// TestPostJSONRejectsBusinessErrors 封装TestPostJSONRejectsBusiness错误列表业务协调。
func TestPostJSONRejectsBusinessErrors(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"signature invalid"}`))
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendDingTalk(map[string]any{"webhook_url": srv.URL}, "x"); err == nil || !strings.Contains(err.Error(), "signature invalid") {
		t.Fatalf("business error=%v", err)
	}
}

// TestSendTelegram_MissingConfig 封装TestSendTelegramMissing配置业务协调。
func TestSendTelegram_MissingConfig(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendTelegram(map[string]any{"bot_token": ""}, "x"); err == nil {
		t.Fatal("缺 bot_token 应报错")
	}
	if // err 用于本次流程后续判断的err
	err := n.sendTelegram(map[string]any{"bot_token": "T", "chat_id": ""}, "x"); err == nil {
		t.Fatal("缺 chat_id 应报错")
	}
}

// TestSendTelegram_HTTPError 封装TestSendTelegramHTTP错误业务协调。
func TestSendTelegram_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	if // err 用于本次流程后续判断的err
	err := n.sendTelegram(map[string]any{"bot_token": "T", "chat_id": "1"}, "x"); err == nil {
		t.Fatal("4xx 应报错")
	}
}

// ---------------- Webhook ----------------

// TestSendWebhook_CustomHeadersAndMethod 自定义 headers + GET 方法。
func TestSendWebhook_CustomHeadersAndMethod(t *testing.T) {
	// gotMethod 用于本次流程后续判断的got方法
	var gotMethod string
	// gotAuth 用于本次流程后续判断的gotAuth
	var gotAuth string
	// gotBody 用于本次流程后续判断的got请求体
	var gotBody string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{
		"webhook_url": srv.URL,
		"http_method": "get",
		"headers":     `{"Authorization":"Bearer abc","X-Custom":"v"}`,
	}
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(cfg, "消息"); err != nil {
		t.Fatalf("sendWebhook: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method=%s want GET", gotMethod)
	}
	if gotAuth != "Bearer abc" {
		t.Errorf("Authorization=%s", gotAuth)
	}
	if !strings.Contains(gotBody, "消息") || !strings.Contains(gotBody, "source") {
		t.Errorf("body 异常: %s", gotBody)
	}
}

// TestSendWebhook_EmptyWebhook 封装TestSendWebhookEmptyWebhook业务协调。
func TestSendWebhook_EmptyWebhook(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

// TestSendWebhook_HTTPError 封装TestSendWebhookHTTP错误业务协调。
func TestSendWebhook_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("5xx 应报错")
	}
}

// TestSendWebhook_InvalidHeaders headers 非法 JSON 时静默忽略。
func TestSendWebhook_InvalidHeaders(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{"webhook_url": srv.URL, "headers": "{bad json"}
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(cfg, "x"); err != nil {
		t.Fatalf("非法 headers 不应报错: %v", err)
	}
}

// ---------------- 邮件 ----------------

// fakeSMTPServer 启动一个最小化 SMTP 服务器，响应 EHLO/MAIL/RCPT/DATA/QUIT。
// 返回监听地址与接收到的 DATA 正文 builder。
// fakeSMTPServer 封装fakeSMTPServer业务协调。
func fakeSMTPServer(t *testing.T) (string, *strings.Builder) {
	t.Helper()
	// ln、err 用于本次流程后续判断的ln、err
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// received 用于本次流程后续判断的received
	var received strings.Builder
	go func() {
		for {
			// conn、err 用于本次流程后续判断的conn、err
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSMTP(conn, &received)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), &received
}

// handleSMTP 封装handleSMTP业务协调。
func handleSMTP(conn net.Conn, received *strings.Builder) {
	defer conn.Close()
	// r 用于本次流程后续判断的r
	r := bufio.NewReader(conn)
	// write 用于本次流程后续判断的write
	write := func(s string) { _, _ = conn.Write([]byte(s)) }
	write("220 mock smtp\r\n")
	for {
		// line、err 用于本次流程后续判断的line、err
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		// upper 用于本次流程后续判断的upper
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			write("250-mock\r\n250-AUTH PLAIN LOGIN\r\n250 OK\r\n")
		case strings.HasPrefix(upper, "AUTH"):
			write("235 2.7.0 Authentication successful\r\n")
		case strings.HasPrefix(upper, "MAIL FROM"):
			write("250 OK\r\n")
		case strings.HasPrefix(upper, "RCPT TO"):
			write("250 OK\r\n")
		case strings.HasPrefix(upper, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>\r\n")
			for {
				// l、err 用于本次流程后续判断的l、err
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(l) == "." {
					break
				}
				received.WriteString(l)
			}
			write("250 OK\r\n")
		case strings.HasPrefix(upper, "QUIT"):
			write("221 Bye\r\n")
			return
		case upper == "":
			continue
		default:
			write("250 OK\r\n")
		}
	}
}

// TestSendEmail_Success 通过 mock SMTP 服务器验证邮件发送。
func TestSendEmail_Success(t *testing.T) {
	// addr、received 用于本次流程后续判断的addr、received
	addr, received := fakeSMTPServer(t)
	// host、port 用于本次流程后续判断的host、port
	host, port, _ := strings.Cut(addr, ":")
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "u@e.com",
		"smtp_password": "pw",
		"smtp_use_tls":  false,
		"to_email":      "to@e.com",
	}
	if // err 用于本次流程后续判断的err
	err := n.sendEmail(cfg, "邮件正文"); err != nil {
		t.Fatalf("sendEmail: %v", err)
	}
	// body 用于本次流程后续判断的请求体
	body := received.String()
	if !strings.Contains(body, "Subject:") || !strings.Contains(body, "邮件正文") {
		t.Errorf("邮件内容异常: %s", body)
	}
}

// TestSendEmail_DefaultFrom 缺省 smtp_from 时回退为 smtp_user。
func TestSendEmail_DefaultFrom(t *testing.T) {
	// addr、received 用于本次流程后续判断的addr、received
	addr, received := fakeSMTPServer(t)
	// host、port 用于本次流程后续判断的host、port
	host, port, _ := strings.Cut(addr, ":")
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "from@e.com",
		"smtp_password": "pw",
		"smtp_use_tls":  false,
		"to_email":      "to@e.com",
	}
	if // err 用于本次流程后续判断的err
	err := n.sendEmail(cfg, "x"); err != nil {
		t.Fatalf("sendEmail: %v", err)
	}
	if !strings.Contains(received.String(), "From: from@e.com") {
		t.Errorf("缺省 from 应回退为 user: %s", received.String())
	}
}

// TestSendEmail_SeparatesDisplayNameAndEnvelopeAddress 封装TestSend邮箱SeparatesDisplay名称AndEnvelopeAddress业务协调。
func TestSendEmail_SeparatesDisplayNameAndEnvelopeAddress(t *testing.T) {
	// addr、received 用于本次流程后续判断的addr、received
	addr, received := fakeSMTPServer(t)
	// host、port 用于本次流程后续判断的host、port
	host, port, _ := strings.Cut(addr, ":")
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{
		"smtp_server": host, "smtp_port": port, "smtp_user": "login@e.com", "smtp_password": "pw",
		"smtp_use_tls":   false,
		"smtp_from_name": "闲鱼自动回复系统", "smtp_from_address": "sender@e.com", "to_email": "to@e.com",
	}
	if // err 用于本次流程后续判断的err
	err := n.sendEmail(cfg, "x"); err != nil {
		t.Fatal(err)
	}
	// body 用于本次流程后续判断的请求体
	body := received.String()
	if !strings.Contains(body, "sender@e.com") || !strings.Contains(body, "From: =?utf-8?") {
		t.Fatalf("smtp envelope/header mismatch: %s", body)
	}
}

// TestSendEmail_UsesSystemSMTPFallback 封装TestSend邮箱Uses系统SMTPFallback业务协调。
func TestSendEmail_UsesSystemSMTPFallback(t *testing.T) {
	// addr、received 用于本次流程后续判断的addr、received
	addr, received := fakeSMTPServer(t)
	// host、port 用于本次流程后续判断的host、port
	host, port, _ := strings.Cut(addr, ":")
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// key、value 表示当前遍历过程中的key、value
	for key, value := range map[string]string{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "system@e.com",
		"smtp_password": "pw",
		"smtp_use_tls":  "false",
	} {
		if // err 用于本次流程后续判断的err
		err := store.Settings.Set(ctx, key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	// channelID、channelErr 保存数据库渠道及创建错误，用于覆盖测试发送的真实取渠道路径。
	channelID, channelErr := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "系统 SMTP 测试渠道", Type: "email", Config: `{"to_email":"to@e.com"}`, Enabled: true, UserID: 1,
	})
	if channelErr != nil {
		t.Fatalf("create email channel: %v", channelErr)
	}
	// n 用于验证全局通知器按数据库渠道所有者读取系统 SMTP 配置。
	n := New("", store, nil)
	if // err 用于本次流程后续判断的err
	err := n.SendToChannel(channelID, "系统SMTP正文"); err != nil {
		t.Fatalf("SendToChannel with system fallback: %v", err)
	}
	// body 用于本次流程后续判断的请求体
	body := received.String()
	if !strings.Contains(body, "From: system@e.com") || !strings.Contains(body, "系统SMTP正文") {
		t.Fatalf("system SMTP fallback body mismatch: %s", body)
	}
	// auditRecords、auditErr 保存系统 SMTP 密码读取产生的访问审计记录及查询错误。
	auditRecords, auditErr := store.SecurityAudit.ListByUser(ctx, 1, 10)
	if auditErr != nil || len(auditRecords) != 1 {
		t.Fatalf("系统 SMTP 密码审计记录异常: records=%+v err=%v", auditRecords, auditErr)
	}
	if auditRecords[0].Action != "settings.use" || auditRecords[0].Resource != "notifications" || len(auditRecords[0].Keys) != 1 || auditRecords[0].Keys[0] != "smtp_password" {
		t.Fatalf("系统 SMTP 密码审计上下文异常: %+v", auditRecords[0])
	}
}

// TestSMTPConfigValueUsesExclusiveExplicitModes 封装TestSMTP配置值UsesExclusiveExplicitModes业务协调。
func TestSMTPConfigValueUsesExclusiveExplicitModes(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.Settings.Set(ctx, "smtp_server", "system.example.com"); err != nil {
		t.Fatal(err)
	}
	// n 用于本次流程后续判断的n
	n := New("cid", store, nil)

	if // got 用于本次流程后续判断的got
	got := n.smtpConfigValue(ctx, map[string]any{
		"use_custom_smtp": false,
		"smtp_server":     "stale-channel.example.com",
	}, "smtp_server", ""); got != "system.example.com" {
		t.Fatalf("system mode mixed in channel override: %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := n.smtpConfigValue(ctx, map[string]any{
		"use_custom_smtp": true,
	}, "smtp_server", ""); got != "" {
		t.Fatalf("custom mode inherited system value: %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := n.smtpConfigValue(ctx, map[string]any{
		"smtp_server": "legacy-channel.example.com",
	}, "smtp_server", ""); got != "legacy-channel.example.com" {
		t.Fatalf("legacy channel override compatibility lost: %q", got)
	}
}

// TestSendEmail_IncompleteConfig 配置不完整应报错。
func TestSendEmail_IncompleteConfig(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cases 用于本次流程后续判断的cases
	cases := []map[string]any{
		{"smtp_user": "u", "to_email": "t@e.com"},   // 缺 server
		{"smtp_server": "s", "to_email": "t@e.com"}, // 缺 user
		{"smtp_server": "s", "smtp_user": "u"},      // 缺 to
	}
	// cfg 表示当前遍历过程中的cfg
	for _, cfg := range cases {
		if // err 用于本次流程后续判断的err
		err := n.sendEmail(cfg, "x"); err == nil {
			t.Fatalf("配置不完整应报错: %v", cfg)
		}
	}
}

// TestParseSMTPTransportFlags 封装TestParseSMTPTransportFlags业务协调。
func TestParseSMTPTransportFlags(t *testing.T) {
	if !parseConfigBool("true", false) || !parseConfigBool("1", false) || parseConfigBool("off", true) {
		t.Fatal("SMTP boolean settings were not parsed consistently")
	}
	if !parseConfigBool("invalid", true) || parseConfigBool("invalid", false) {
		t.Fatal("SMTP boolean fallback was ignored")
	}
}

// TestSendEmail_ConnectionFailed 连接失败应返回错误。
func TestSendEmail_ConnectionFailed(t *testing.T) {
	// 创建并立即关闭一个监听器，复用其地址制造连接失败。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// host、port 用于本次流程后续判断的host、port
	host, port, _ := strings.Cut(ln.Addr().String(), ":")
	_ = ln.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "u",
		"smtp_password": "p",
		"to_email":      "t@e.com",
	}
	if // err 用于本次流程后续判断的err
	err := n.sendEmail(cfg, "x"); err == nil {
		t.Fatal("连接失败应报错")
	}
}

// TestPostJSON_NetworkError postJSON 网络错误路径。
func TestPostJSON_NetworkError(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// 非法 URL（无 host）。
	if err := n.postJSON("http://", map[string]any{"a": 1}); err == nil {
		t.Fatal("非法 URL 应报错")
	}
}

// TestSendTelegramNetworkErrorRedactsTokenPath 验证 Telegram 网络失败不会把路径中的 Bot Token 返回给调用方。
func TestSendTelegramNetworkErrorRedactsTokenPath(t *testing.T) {
	if notificationRequestError(nil) != nil {
		t.Fatal("空通知请求错误应保持为空")
	}
	// token 是用于证明路径脱敏的模拟 Telegram Bot Token。
	const token = "123456:REVIEW_SECRET"
	// notifier 使用固定失败 transport，确保测试不会访问 Telegram。
	notifier := New("cid", nil, nil)
	notifier.httpc = &http.Client{Transport: failingNotificationTransport{}}
	// sendErr 保存经过通知网络边界清理后的错误。
	sendErr := notifier.sendTelegram(map[string]any{"bot_token": token, "chat_id": "review-chat"}, "测试")
	if sendErr == nil {
		t.Fatal("模拟 Telegram 网络失败应返回错误")
	}
	// diagnostic 保存可进入日志、数据库或上层处理的最终错误文本。
	diagnostic := sendErr.Error()
	if strings.Contains(diagnostic, token) || strings.Contains(diagnostic, "sendMessage") || strings.Contains(diagnostic, "review-chat") {
		t.Fatalf("Telegram 错误泄露敏感路径或请求数据: %s", diagnostic)
	}
	if !strings.Contains(diagnostic, "https://api.telegram.org/<redacted>") || !strings.Contains(diagnostic, "simulated connection failure") {
		t.Fatalf("Telegram 错误缺少安全诊断上下文: %s", diagnostic)
	}
}

// TestPostJSON_HTTPError 覆盖状态码 >=300 分支。
func TestPostJSON_HTTPError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.postJSON(srv.URL, map[string]any{"a": 1}); err == nil {
		t.Fatal("401 应报错")
	}
}

// TestPostJSON_MarshalError 传入无法序列化的值触发 json.Marshal 错误。
func TestPostJSON_MarshalError(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// chan 无法被 json.Marshal 序列化。
	bad := map[string]any{"ch": make(chan int)}
	if // err 用于本次流程后续判断的err
	err := n.postJSON("http://127.0.0.1:1", bad); err == nil {
		t.Fatal("无法序列化的 payload 应报错")
	}
}

// TestPostJSON_BodyReadError 服务端声明 Content-Length 但提前断开，触发 io.Copy 错误。
func TestPostJSON_BodyReadError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 声明 100 字节但只写 5 字节后立即关闭底层连接。
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
		if // hj、ok 用于本次流程后续判断的hj、ok
		hj, ok := w.(http.Hijacker); ok {
			// conn 用于本次流程后续判断的conn
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.postJSON(srv.URL, map[string]any{"a": 1}); err == nil {
		t.Fatal("body 读取错误应报错")
	}
}

// TestSendWebhook_BodyReadError webhook 渠道触发 io.Copy 错误。
func TestSendWebhook_BodyReadError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("x"))
		if // hj、ok 用于本次流程后续判断的hj、ok
		hj, ok := w.(http.Hijacker); ok {
			// conn 用于本次流程后续判断的conn
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("body 读取错误应报错")
	}
}

// TestSendWebhook_NetworkError 服务端不可达触发 httpc.Do 错误。
func TestSendWebhook_NetworkError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // 关闭使连接失败
	n := New("cid", nil, nil)
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("网络错误应报错")
	}
}

// TestSendWebhook_NewRequestError URL 含空格导致 http.NewRequest 失败。
func TestSendWebhook_NewRequestError(t *testing.T) {
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// cfg 用于本次流程后续判断的cfg
	cfg := map[string]any{"webhook_url": "http://in valid host/path"}
	if // err 用于本次流程后续判断的err
	err := n.sendWebhook(cfg, "x"); err == nil {
		t.Fatal("非法 URL 应触发 NewRequest 错误")
	}
}

// TestSend_RouteAllTypes 通过 send 入口覆盖各渠道类型分发分支。
// HTTP 渠道用 redirect client 拦截到本地 httptest server；email 走 SMTP 必然失败，
// 但能覆盖 send 的 email case。
// TestSend_RouteAllTypes 封装TestSendRouteAllTypes业务协调。
func TestSend_RouteAllTypes(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// cfg 用于本次流程后续判断的cfg
	cfg := `{"webhook_url":"` + srv.URL + `","server_url":"` + srv.URL + `","device_key":"K","bot_token":"T","chat_id":"1"}`
	// n 用于本次流程后续判断的n
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	// types 用于本次流程后续判断的types
	types := []string{"feishu", "lark", "bark", "webhook", "wechat", "telegram", "ding_talk"}
	// typ 表示当前遍历过程中的typ
	for _, typ := range types {
		// ch 用于本次流程后续判断的ch
		ch := dbNotificationChannel(typ, cfg)
		if // err 用于本次流程后续判断的err
		err := n.send(ch, "分发测试"); err != nil {
			t.Errorf("send(%s): %v", typ, err)
		}
	}

	// email 走 SMTP，配置不可达应返回 error（覆盖 send 的 email case）。
	if err := n.send(dbNotificationChannel("email", `{"smtp_server":"127.0.0.1","smtp_port":"1","smtp_user":"u","to_email":"t@e.com"}`), "x"); err == nil {
		t.Error("email 不可达应报错")
	}
}

// dbNotificationChannel 构造一个最小渠道行，避免直接依赖 db 包构造。
func dbNotificationChannel(typ, cfg string) db.NotificationChannel {
	return db.NotificationChannel{ID: 1, Name: "ch", Type: typ, Config: cfg}
}

// TestSend_DingTalkAlias 钉钉别名 dingtalk 也能路由。
func TestSend_DingTalkAlias(t *testing.T) {
	// got 用于本次流程后续判断的got
	var got map[string]any
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// b 用于本次流程后续判断的b
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// n 用于本次流程后续判断的n
	n := New("cid", nil, nil)
	// ch 用于本次流程后续判断的ch
	ch := dbNotificationChannel("dingtalk", `{"webhook_url":"`+srv.URL+`"}`)
	if // err 用于本次流程后续判断的err
	err := n.send(ch, "别名"); err != nil {
		t.Fatalf("send(dingtalk): %v", err)
	}
	// md 用于本次流程后续判断的md
	md, _ := got["markdown"].(map[string]any)
	if md["text"] != "别名" {
		t.Errorf("别名路由正文: %v", md["text"])
	}
}
