package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSendSuccess 验证请求头、请求体与规范化响应：缺省 handled 视为 true，空回复项被丢弃。
func TestSendSuccess(t *testing.T) {
	// captured 保存测试服务收到的请求体，用于断言协议字段。
	var captured Request
	// auth 保存测试服务收到的 Authorization 头。
	var auth string
	// server 是本地测试 Agent，记录请求头与请求体后返回预设协议响应。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`{"working":true,"poll_after_ms":1500,"replies":[{"text":" 你好 "},{"text":"","image_url":""},{"image_url":"https://example.com/a.png"}]}`))
	}))
	defer server.Close()
	// response、err 保存协议调用结果。
	response, err := Send(context.Background(), Endpoint{URL: server.URL, Token: "tok", Timeout: time.Second},
		Request{Event: EventMessage, TurnID: "t1", Sequence: 1, ChatID: "c1", Message: &Message{Type: MessageTypeText, Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tok" || captured.Version != ProtocolVersion || captured.Event != EventMessage || captured.Message.Text != "hi" {
		t.Fatalf("请求不符合协议: auth=%q req=%+v", auth, captured)
	}
	if !response.Handled || !response.Working || response.PollAfter != 1500*time.Millisecond {
		t.Fatalf("响应规范化错误: %+v", response)
	}
	if len(response.Replies) != 2 || response.Replies[0].Text != "你好" || response.Replies[1].ImageURL != "https://example.com/a.png" {
		t.Fatalf("回复过滤错误: %+v", response.Replies)
	}
}

// TestSendHandledFalseAndNoToken 验证显式 handled=false 被保留，且无令牌时不发送 Authorization。
func TestSendHandledFalseAndNoToken(t *testing.T) {
	// hasAuth 记录测试服务是否收到 Authorization 头。
	var hasAuth bool
	// server 是本地测试 Agent，只返回显式 handled=false 的响应并记录是否收到 Authorization 头。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"handled":false}`))
	}))
	defer server.Close()
	// response、err 保存协议调用的规范化响应及错误。
	response, err := Send(context.Background(), Endpoint{URL: server.URL, Timeout: time.Second}, Request{Event: EventPing})
	if err != nil || response.Handled || hasAuth {
		t.Fatalf("handled=%v hasAuth=%v err=%v", response.Handled, hasAuth, err)
	}
}

// TestSendErrorsDoNotLeakSecrets 验证非 2xx、非法 JSON、超大响应和网络错误的类型，且错误文本不含令牌与正文。
func TestSendErrorsDoNotLeakSecrets(t *testing.T) {
	// cases 覆盖每类远端失败；handler 决定测试服务的响应。
	cases := []struct {
		name    string
		handler http.HandlerFunc
		check   func(error) bool
	}{
		{"状态码", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret-body", http.StatusBadGateway) }, func(err error) bool {
			// status 用于从错误链中取出非 2xx 状态码。
			var status *StatusError
			return errors.As(err, &status) && status.Code == http.StatusBadGateway
		}},
		{"非法JSON", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("secret-body")) }, func(err error) bool { return errors.Is(err, ErrInvalidResponse) }},
		{"超大响应", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"replies":[{"text":"` + strings.Repeat("a", MaxResponseBytes) + `"}]}`))
		}, func(err error) bool { return errors.Is(err, ErrInvalidResponse) }},
	}
	// item 表示当前失败场景。
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// server 是本场景对应的本地测试 Agent，响应行为由 item.handler 决定。
			server := httptest.NewServer(item.handler)
			defer server.Close()
			// err 保存调用错误，用于断言错误类型且不含令牌与响应正文。
			_, err := Send(context.Background(), Endpoint{URL: server.URL, Token: "super-token", Timeout: time.Second}, Request{Event: EventPing})
			if err == nil || !item.check(err) || strings.Contains(err.Error(), "super-token") || strings.Contains(err.Error(), "secret-body") {
				t.Fatalf("错误不符合预期或泄漏敏感内容: %v", err)
			}
		})
	}
	// 连接被拒绝时只返回通用错误，不含地址。
	closed := httptest.NewServer(http.NotFoundHandler())
	// address 是已关闭测试服务的地址，用来构造必然连接失败的场景。
	address := closed.URL
	closed.Close()
	// err 保存连接被拒绝时的错误，断言只返回通用文案。
	_, err := Send(context.Background(), Endpoint{URL: address, Token: "super-token", Timeout: time.Second}, Request{Event: EventPing})
	if err == nil || strings.Contains(err.Error(), address) || strings.Contains(err.Error(), "super-token") {
		t.Fatalf("网络错误泄漏地址或令牌: %v", err)
	}
}

// TestSendContextCancellation 验证调用方取消 ctx 时返回 ctx 错误而不是通用失败。
func TestSendContextCancellation(t *testing.T) {
	// release 用于在断言结束后放行阻塞中的测试服务。
	release := make(chan struct{})
	// server 是会一直阻塞到测试结束的本地 Agent，用来验证取消语义。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	// ctx、cancel 保存被主动取消的请求上下文。
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	// err 保存被取消请求的错误，期望是 context.Canceled。
	_, err := Send(ctx, Endpoint{URL: server.URL, Timeout: 5 * time.Second}, Request{Event: EventPing})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled，得到 %v", err)
	}
}

// TestValidateURL 验证地址格式校验的各个分支。
func TestValidateURL(t *testing.T) {
	// valid 是合法地址；invalid 是各类应被拒绝的地址。
	for _, valid := range []string{"http://127.0.0.1:8080/agent", " https://agent.example.com/v1 "} {
		// err 保存合法地址的校验错误，期望为空。
		if err := ValidateURL(valid); err != nil {
			t.Fatalf("%q 应合法: %v", valid, err)
		}
	}
	// invalid 表示当前待验证的非法地址。
	for _, invalid := range []string{"", "agent.example.com", "ftp://example.com", "https://", "https://user:pass@example.com", "http://[::1"} {
		// err 保存非法地址的校验错误，期望非空。
		if err := ValidateURL(invalid); err == nil {
			t.Fatalf("%q 应被拒绝", invalid)
		}
	}
	// err 保存 Send 对非法地址的拒绝错误，期望非空。
	if _, err := Send(context.Background(), Endpoint{URL: "ftp://x"}, Request{}); err == nil {
		t.Fatal("Send 应拒绝非法地址")
	}
}
