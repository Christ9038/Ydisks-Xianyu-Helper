package adapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentreplyapp "xianyu-go/internal/application/agentreply"
)

// TestAgentReplyTesterPlatformProtocols 验证 dify/fastgpt 协议的连接测试：成功、HTTP 错误、空回复、未能到达与调用方取消。
func TestAgentReplyTesterPlatformProtocols(t *testing.T) {
	// tester 是被测连接测试适配器。
	tester := NewAgentReplyTester()
	// run 向给定状态码与响应体的本地平台发起一次连接测试，返回诊断结果与错误。
	run := func(protocol string, status int, body string) (agentreplyapp.TestResult, error) {
		// server 是本地测试服务端，按用例预设响应并记录请求。
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// raw 读取并丢弃请求正文，确保客户端完整发送。
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		defer server.Close()
		return tester.Ping(context.Background(), agentreplyapp.Endpoint{URL: server.URL, Token: "k", Timeout: 5 * time.Second, Protocol: protocol})
	}
	// difyOK、fastOK 是两种协议的成功响应体。
	const difyOK = "data: {\"event\":\"message\",\"answer\":\"pong\",\"conversation_id\":\"c\"}\n\n"
	// fastOK 是 FastGPT 的成功响应体。
	const fastOK = `{"choices":[{"message":{"content":"pong"}}]}`
	// tc 是当前表驱动用例。
	for _, tc := range []struct {
		name, protocol, body string
		status               int
		want                 string
		wantCode             int
	}{
		{"dify 成功", agentreplyapp.ProtocolDify, difyOK, 200, agentreplyapp.TestStatusOK, 0},
		{"fastgpt 成功", agentreplyapp.ProtocolFastGPT, fastOK, 200, agentreplyapp.TestStatusOK, 0},
		{"dify 401", agentreplyapp.ProtocolDify, `{"code":"unauthorized"}`, 401, agentreplyapp.TestStatusHTTPError, 401},
		{"fastgpt 500", agentreplyapp.ProtocolFastGPT, `boom`, 500, agentreplyapp.TestStatusHTTPError, 500},
		{"dify 空回复", agentreplyapp.ProtocolDify, "data: {\"event\":\"message_end\"}\n\n", 200, agentreplyapp.TestStatusInvalidResponse, 0},
		{"fastgpt 空回复", agentreplyapp.ProtocolFastGPT, `{"choices":[]}`, 200, agentreplyapp.TestStatusInvalidResponse, 0},
	} {
		// result、err 是该场景的诊断结果。
		result, err := run(tc.protocol, tc.status, tc.body)
		if err != nil || result.Status != tc.want || result.StatusCode != tc.wantCode || (tc.want == agentreplyapp.TestStatusOK && (!result.Handled || result.ReplyCount != 1)) {
			t.Errorf("%s: %+v err=%v", tc.name, result, err)
		}
	}
	// 出站策略拒绝的地址与关闭的服务端都按未能到达处理。
	if result, err := tester.Ping(context.Background(), agentreplyapp.Endpoint{URL: "ftp://example.com", Timeout: time.Second, Protocol: agentreplyapp.ProtocolDify}); err != nil || result.Status != agentreplyapp.TestStatusUnreachable {
		t.Errorf("非法地址: %+v err=%v", result, err)
	}
	// dead 是已关闭的服务端地址。
	dead := httptest.NewServer(http.NotFoundHandler())
	// deadURL 是已关闭服务端的地址，用于模拟无法连接。
	deadURL := dead.URL
	dead.Close()
	// err 是该步骤的错误，期望为空或为被测的特定错误；result 是被测调用的返回结果。
	if result, err := tester.Ping(context.Background(), agentreplyapp.Endpoint{URL: deadURL, Timeout: time.Second, Protocol: agentreplyapp.ProtocolFastGPT}); err != nil || result.Status != agentreplyapp.TestStatusUnreachable {
		t.Errorf("关闭的服务端: %+v err=%v", result, err)
	}
	// 调用方已取消：返回取消错误而不是诊断结果。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// slow 是会阻塞到连接断开的服务端，保证请求确实是因取消而失败。
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := tester.Ping(canceled, agentreplyapp.Endpoint{URL: slow.URL, Timeout: 5 * time.Second, Protocol: agentreplyapp.ProtocolDify}); err == nil {
		t.Error("取消应返回错误")
	}
}
