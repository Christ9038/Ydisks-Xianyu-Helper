package notify

import (
	"bufio"
	"context"
	"encoding/json"
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
type redirectTransport struct{ target string }

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.target
	req.Host = t.target
	return http.DefaultTransport.RoundTrip(req)
}

// notifierWithRedirectClient 构造一个把所有 HTTP 请求重定向到 target 的 Notifier。
func notifierWithRedirectClient(target string) *Notifier {
	n := New("cid", nil, nil)
	n.httpc = &http.Client{Timeout: 10 * time.Second, Transport: &redirectTransport{target: target}}
	return n
}

// readJSONBody 读取并反序列化请求 body。
func readJSONBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(b))
	}
	return m
}

// ---------------- 钉钉 ----------------

// TestSendDingTalk_WithSecret 带签名时 URL 携带 timestamp/sign。
// 注：生产代码用 "&" 拼接签名参数，需 webhook 已带查询串；此处补一个占位参数。
func TestSendDingTalk_WithSecret(t *testing.T) {
	var gotURL string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := New("cid", nil, nil)
	// webhook 已带占位查询参数，使 "&timestamp=..." 拼接合法。
	cfg := map[string]any{"webhook_url": srv.URL + "?token=x", "secret": "SECxxxx"}
	if err := n.sendDingTalk(cfg, "内容"); err != nil {
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
	n := New("cid", nil, nil)
	if err := n.sendDingTalk(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

// TestSendDingTalk_HTTPError 服务端 4xx/5xx 应返回错误。
func TestSendDingTalk_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendDingTalk(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("4xx 应报错")
	}
}

// TestSendDingTalk_NetworkError 连接失败应返回错误。
func TestSendDingTalk_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // 立即关闭，制造连接失败
	n := New("cid", nil, nil)
	if err := n.sendDingTalk(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("网络错误应报错")
	}
}

// TestSendDingTalk_OldConfigFormat 旧格式 webhook 直接是 URL。
func TestSendDingTalk_OldConfigFormat(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	// config 字段存放旧 URL。
	if err := n.sendDingTalk(map[string]any{"config": srv.URL}, "旧格式"); err != nil {
		t.Fatalf("sendDingTalk 旧格式: %v", err)
	}
	md, _ := got["markdown"].(map[string]any)
	if md["text"] != "旧格式" {
		t.Errorf("旧格式正文: %v", md["text"])
	}
}

// ---------------- 飞书 ----------------

// TestSendFeishu 成功 + 带签名 + 空 webhook + HTTP 错误。
func TestSendFeishu(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendFeishu(map[string]any{"webhook_url": srv.URL, "secret": "SEC"}, "飞书消息"); err != nil {
		t.Fatalf("sendFeishu: %v", err)
	}
	if got["msg_type"] != "text" {
		t.Errorf("msg_type=%v", got["msg_type"])
	}
	if _, ok := got["sign"]; !ok {
		t.Errorf("带 secret 应有 sign 字段: %v", got)
	}
	content, _ := got["content"].(map[string]any)
	if content["text"] != "飞书消息" {
		t.Errorf("text=%v", content["text"])
	}
}

func TestSendFeishu_EmptyWebhook(t *testing.T) {
	n := New("cid", nil, nil)
	if err := n.sendFeishu(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

func TestSendFeishu_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendFeishu(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("5xx 应报错")
	}
}

// ---------------- Bark ----------------

// TestSendBark 成功 + 可选字段 + 空 device_key + HTTP 错误。
func TestSendBark(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/push" {
			t.Errorf("path=%s want /push", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	cfg := map[string]any{
		"server_url": srv.URL,
		"device_key": "KEY",
		"title":      "标题",
		"icon":       "https://img/x.png",
		"url":        "https://click",
		"sound":      "bell",
		"group":      "g1",
	}
	if err := n.sendBark(cfg, "正文"); err != nil {
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
func TestSendBark_DefaultServerURL(t *testing.T) {
	var hitPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	if err := n.sendBark(map[string]any{"device_key": "K"}, "x"); err != nil {
		t.Fatalf("sendBark 默认 server: %v", err)
	}
	if hitPath != "/push" {
		t.Errorf("默认 server 应请求 /push, 实际 %s", hitPath)
	}
}

func TestSendBark_EmptyDeviceKey(t *testing.T) {
	n := New("cid", nil, nil)
	if err := n.sendBark(map[string]any{"server_url": "https://api.day.app"}, "x"); err == nil {
		t.Fatal("缺 device_key 应报错")
	}
}

func TestSendBark_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendBark(map[string]any{"server_url": srv.URL, "device_key": "K"}, "x"); err == nil {
		t.Fatal("5xx 应报错")
	}
}

// ---------------- 企业微信 ----------------

func TestSendWeChat(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendWeChat(map[string]any{"webhook_url": srv.URL}, "微信消息"); err != nil {
		t.Fatalf("sendWeChat: %v", err)
	}
	if got["msgtype"] != "text" {
		t.Errorf("msgtype=%v", got["msgtype"])
	}
	text, _ := got["text"].(map[string]any)
	if text["content"] != "微信消息" {
		t.Errorf("content=%v", text["content"])
	}
}

func TestSendWeChat_EmptyWebhook(t *testing.T) {
	n := New("cid", nil, nil)
	if err := n.sendWeChat(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

func TestSendWeChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendWeChat(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("4xx 应报错")
	}
}

// ---------------- Telegram ----------------

// TestSendTelegram_Success 通过 redirect transport 拦截 telegram API。
func TestSendTelegram_Success(t *testing.T) {
	var got map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	if err := n.sendTelegram(map[string]any{"bot_token": "TOK", "chat_id": "123"}, "TG消息"); err != nil {
		t.Fatalf("sendTelegram: %v", err)
	}
	wantPath := "/botTOK/sendMessage"
	if gotPath != wantPath {
		t.Errorf("path=%s want %s", gotPath, wantPath)
	}
	if got["chat_id"] != "123" || got["text"] != "TG消息" {
		t.Errorf("telegram payload: %v", got)
	}
	if _, ok := got["parse_mode"]; ok {
		t.Errorf("动态文本不应启用未转义的 HTML 模式: %v", got)
	}
}

func TestPostJSONRejectsBusinessErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"signature invalid"}`))
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendDingTalk(map[string]any{"webhook_url": srv.URL}, "x"); err == nil || !strings.Contains(err.Error(), "signature invalid") {
		t.Fatalf("business error=%v", err)
	}
}

func TestSendTelegram_MissingConfig(t *testing.T) {
	n := New("cid", nil, nil)
	if err := n.sendTelegram(map[string]any{"bot_token": ""}, "x"); err == nil {
		t.Fatal("缺 bot_token 应报错")
	}
	if err := n.sendTelegram(map[string]any{"bot_token": "T", "chat_id": ""}, "x"); err == nil {
		t.Fatal("缺 chat_id 应报错")
	}
}

func TestSendTelegram_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	if err := n.sendTelegram(map[string]any{"bot_token": "T", "chat_id": "1"}, "x"); err == nil {
		t.Fatal("4xx 应报错")
	}
}

// ---------------- Webhook ----------------

// TestSendWebhook_CustomHeadersAndMethod 自定义 headers + GET 方法。
func TestSendWebhook_CustomHeadersAndMethod(t *testing.T) {
	var gotMethod string
	var gotAuth string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	cfg := map[string]any{
		"webhook_url": srv.URL,
		"http_method": "get",
		"headers":     `{"Authorization":"Bearer abc","X-Custom":"v"}`,
	}
	if err := n.sendWebhook(cfg, "消息"); err != nil {
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

func TestSendWebhook_EmptyWebhook(t *testing.T) {
	n := New("cid", nil, nil)
	if err := n.sendWebhook(map[string]any{}, "x"); err == nil {
		t.Fatal("缺 webhook_url 应报错")
	}
}

func TestSendWebhook_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendWebhook(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("5xx 应报错")
	}
}

// TestSendWebhook_InvalidHeaders headers 非法 JSON 时静默忽略。
func TestSendWebhook_InvalidHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	cfg := map[string]any{"webhook_url": srv.URL, "headers": "{bad json"}
	if err := n.sendWebhook(cfg, "x"); err != nil {
		t.Fatalf("非法 headers 不应报错: %v", err)
	}
}

// ---------------- 邮件 ----------------

// fakeSMTPServer 启动一个最小化 SMTP 服务器，响应 EHLO/MAIL/RCPT/DATA/QUIT。
// 返回监听地址与接收到的 DATA 正文 builder。
func fakeSMTPServer(t *testing.T) (string, *strings.Builder) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var received strings.Builder
	go func() {
		for {
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

func handleSMTP(conn net.Conn, received *strings.Builder) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s)) }
	write("220 mock smtp\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
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
	addr, received := fakeSMTPServer(t)
	host, port, _ := strings.Cut(addr, ":")
	n := New("cid", nil, nil)
	cfg := map[string]any{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "u@e.com",
		"smtp_password": "pw",
		"smtp_use_tls":  false,
		"to_email":      "to@e.com",
	}
	if err := n.sendEmail(cfg, "邮件正文"); err != nil {
		t.Fatalf("sendEmail: %v", err)
	}
	body := received.String()
	if !strings.Contains(body, "Subject:") || !strings.Contains(body, "邮件正文") {
		t.Errorf("邮件内容异常: %s", body)
	}
}

// TestSendEmail_DefaultFrom 缺省 smtp_from 时回退为 smtp_user。
func TestSendEmail_DefaultFrom(t *testing.T) {
	addr, received := fakeSMTPServer(t)
	host, port, _ := strings.Cut(addr, ":")
	n := New("cid", nil, nil)
	cfg := map[string]any{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "from@e.com",
		"smtp_password": "pw",
		"smtp_use_tls":  false,
		"to_email":      "to@e.com",
	}
	if err := n.sendEmail(cfg, "x"); err != nil {
		t.Fatalf("sendEmail: %v", err)
	}
	if !strings.Contains(received.String(), "From: from@e.com") {
		t.Errorf("缺省 from 应回退为 user: %s", received.String())
	}
}

func TestSendEmail_SeparatesDisplayNameAndEnvelopeAddress(t *testing.T) {
	addr, received := fakeSMTPServer(t)
	host, port, _ := strings.Cut(addr, ":")
	n := New("cid", nil, nil)
	cfg := map[string]any{
		"smtp_server": host, "smtp_port": port, "smtp_user": "login@e.com", "smtp_password": "pw",
		"smtp_use_tls":   false,
		"smtp_from_name": "闲鱼自动回复系统", "smtp_from_address": "sender@e.com", "to_email": "to@e.com",
	}
	if err := n.sendEmail(cfg, "x"); err != nil {
		t.Fatal(err)
	}
	body := received.String()
	if !strings.Contains(body, "sender@e.com") || !strings.Contains(body, "From: =?utf-8?") {
		t.Fatalf("smtp envelope/header mismatch: %s", body)
	}
}

func TestSendEmail_UsesSystemSMTPFallback(t *testing.T) {
	addr, received := fakeSMTPServer(t)
	host, port, _ := strings.Cut(addr, ":")
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	ctx := context.Background()
	for key, value := range map[string]string{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "system@e.com",
		"smtp_password": "pw",
		"smtp_use_tls":  "false",
	} {
		if err := store.Settings.Set(ctx, key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	n := New("cid", store, nil)
	if err := n.sendEmail(map[string]any{"to_email": "to@e.com"}, "系统SMTP正文"); err != nil {
		t.Fatalf("sendEmail with system fallback: %v", err)
	}
	body := received.String()
	if !strings.Contains(body, "From: system@e.com") || !strings.Contains(body, "系统SMTP正文") {
		t.Fatalf("system SMTP fallback body mismatch: %s", body)
	}
}

func TestSMTPConfigValueUsesExclusiveExplicitModes(t *testing.T) {
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Settings.Set(ctx, "smtp_server", "system.example.com"); err != nil {
		t.Fatal(err)
	}
	n := New("cid", store, nil)

	if got := n.smtpConfigValue(ctx, map[string]any{
		"use_custom_smtp": false,
		"smtp_server":     "stale-channel.example.com",
	}, "smtp_server", ""); got != "system.example.com" {
		t.Fatalf("system mode mixed in channel override: %q", got)
	}
	if got := n.smtpConfigValue(ctx, map[string]any{
		"use_custom_smtp": true,
	}, "smtp_server", ""); got != "" {
		t.Fatalf("custom mode inherited system value: %q", got)
	}
	if got := n.smtpConfigValue(ctx, map[string]any{
		"smtp_server": "legacy-channel.example.com",
	}, "smtp_server", ""); got != "legacy-channel.example.com" {
		t.Fatalf("legacy channel override compatibility lost: %q", got)
	}
}

// TestSendEmail_IncompleteConfig 配置不完整应报错。
func TestSendEmail_IncompleteConfig(t *testing.T) {
	n := New("cid", nil, nil)
	cases := []map[string]any{
		{"smtp_user": "u", "to_email": "t@e.com"},   // 缺 server
		{"smtp_server": "s", "to_email": "t@e.com"}, // 缺 user
		{"smtp_server": "s", "smtp_user": "u"},      // 缺 to
	}
	for _, cfg := range cases {
		if err := n.sendEmail(cfg, "x"); err == nil {
			t.Fatalf("配置不完整应报错: %v", cfg)
		}
	}
}

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
	host, port, _ := strings.Cut(ln.Addr().String(), ":")
	_ = ln.Close()
	n := New("cid", nil, nil)
	cfg := map[string]any{
		"smtp_server":   host,
		"smtp_port":     port,
		"smtp_user":     "u",
		"smtp_password": "p",
		"to_email":      "t@e.com",
	}
	if err := n.sendEmail(cfg, "x"); err == nil {
		t.Fatal("连接失败应报错")
	}
}

// TestPostJSON_NetworkError postJSON 网络错误路径。
func TestPostJSON_NetworkError(t *testing.T) {
	n := New("cid", nil, nil)
	// 非法 URL（无 host）。
	if err := n.postJSON("http://", map[string]any{"a": 1}); err == nil {
		t.Fatal("非法 URL 应报错")
	}
}

// TestPostJSON_HTTPError 覆盖状态码 >=300 分支。
func TestPostJSON_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.postJSON(srv.URL, map[string]any{"a": 1}); err == nil {
		t.Fatal("401 应报错")
	}
}

// TestPostJSON_MarshalError 传入无法序列化的值触发 json.Marshal 错误。
func TestPostJSON_MarshalError(t *testing.T) {
	n := New("cid", nil, nil)
	// chan 无法被 json.Marshal 序列化。
	bad := map[string]any{"ch": make(chan int)}
	if err := n.postJSON("http://127.0.0.1:1", bad); err == nil {
		t.Fatal("无法序列化的 payload 应报错")
	}
}

// TestPostJSON_BodyReadError 服务端声明 Content-Length 但提前断开，触发 io.Copy 错误。
func TestPostJSON_BodyReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 声明 100 字节但只写 5 字节后立即关闭底层连接。
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.postJSON(srv.URL, map[string]any{"a": 1}); err == nil {
		t.Fatal("body 读取错误应报错")
	}
}

// TestSendWebhook_BodyReadError webhook 渠道触发 io.Copy 错误。
func TestSendWebhook_BodyReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("x"))
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	if err := n.sendWebhook(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("body 读取错误应报错")
	}
}

// TestSendWebhook_NetworkError 服务端不可达触发 httpc.Do 错误。
func TestSendWebhook_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // 关闭使连接失败
	n := New("cid", nil, nil)
	if err := n.sendWebhook(map[string]any{"webhook_url": srv.URL}, "x"); err == nil {
		t.Fatal("网络错误应报错")
	}
}

// TestSendWebhook_NewRequestError URL 含空格导致 http.NewRequest 失败。
func TestSendWebhook_NewRequestError(t *testing.T) {
	n := New("cid", nil, nil)
	cfg := map[string]any{"webhook_url": "http://in valid host/path"}
	if err := n.sendWebhook(cfg, "x"); err == nil {
		t.Fatal("非法 URL 应触发 NewRequest 错误")
	}
}

// TestSend_RouteAllTypes 通过 send 入口覆盖各渠道类型分发分支。
// HTTP 渠道用 redirect client 拦截到本地 httptest server；email 走 SMTP 必然失败，
// 但能覆盖 send 的 email case。
func TestSend_RouteAllTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := `{"webhook_url":"` + srv.URL + `","server_url":"` + srv.URL + `","device_key":"K","bot_token":"T","chat_id":"1"}`
	n := notifierWithRedirectClient(srv.Listener.Addr().String())
	types := []string{"feishu", "lark", "bark", "webhook", "wechat", "telegram", "ding_talk"}
	for _, typ := range types {
		ch := dbNotificationChannel(typ, cfg)
		if err := n.send(ch, "分发测试"); err != nil {
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
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = readJSONBody(t, b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New("cid", nil, nil)
	ch := dbNotificationChannel("dingtalk", `{"webhook_url":"`+srv.URL+`"}`)
	if err := n.send(ch, "别名"); err != nil {
		t.Fatalf("send(dingtalk): %v", err)
	}
	md, _ := got["markdown"].(map[string]any)
	if md["text"] != "别名" {
		t.Errorf("别名路由正文: %v", md["text"])
	}
}
