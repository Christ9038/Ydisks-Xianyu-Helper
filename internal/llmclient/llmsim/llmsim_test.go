package llmsim_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"xianyu-go/internal/llmclient"
	"xianyu-go/internal/llmclient/llmsim"
)

// complete 通过真实 llmclient 向网关发起一次请求。
func complete(ctx context.Context, gateway *llmsim.Gateway, protocol string) (llmclient.Result, error) {
	return llmclient.Complete(ctx, llmclient.Endpoint{Protocol: protocol, BaseURL: gateway.URL(), APIKey: "sk-sim", Model: "model-x", HTTPClient: http.DefaultClient},
		llmclient.Request{System: "系统提示", UserText: "商品：《手机壳》", MaxTokens: 77})
}

// TestGatewayScriptsAndRecordsBothProtocols 验证两种协议的成功响应、脚本顺序、兜底响应以及请求记录。
func TestGatewayScriptsAndRecordsBothProtocols(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	for /* protocol 是被测协议。 */ _, protocol := range []string{llmclient.ProtocolOpenAI, llmclient.ProtocolAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			// gateway 是本协议独立的网关；脚本先放两条，兜底为 fallback 文案。
			gateway := llmsim.New(t)
			gateway.Enqueue(llmsim.Step{Text: "第一条"}, llmsim.Step{Text: "第二条"})
			gateway.SetFallback(llmsim.Step{Text: "兜底"})
			for /* want 是依次期望的响应文本。 */ _, want := range []string{"第一条", "第二条", "兜底"} {
				result, err := complete(ctx, gateway, protocol)
				if err != nil || result.Text != want {
					t.Fatalf("期望 %q，得到 %q err=%v", want, result.Text, err)
				}
			}
			// requests 是网关记录的请求。
			requests := gateway.Requests()
			if gateway.Calls() != 3 || len(requests) != 3 {
				t.Fatalf("应记录 3 次请求: %d", gateway.Calls())
			}
			// first 是第一次请求的记录。
			first := requests[0]
			if first.Protocol != protocol || first.Model != "model-x" || first.System != "系统提示" || first.UserText() != "商品：《手机壳》" || first.MaxTokens != 77 {
				t.Fatalf("请求内容记录不完整: %+v", first)
			}
			if protocol == llmclient.ProtocolOpenAI && first.Authorization != "Bearer sk-sim" {
				t.Fatalf("OpenAI 应携带 Bearer 密钥: %q", first.Authorization)
			}
			if protocol == llmclient.ProtocolAnthropic && first.APIKey != "sk-sim" {
				t.Fatalf("Anthropic 应携带 x-api-key: %q", first.APIKey)
			}
		})
	}
}

// TestGatewayFailureModes 验证 HTTP 错误、拒绝回答、延迟超时和未知路径。
func TestGatewayFailureModes(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// gateway 是脚本化的网关。
	gateway := llmsim.New(t)
	gateway.Enqueue(llmsim.Step{Status: http.StatusTooManyRequests}, llmsim.Step{Refuse: true}, llmsim.Step{Delay: time.Minute, Text: "太慢"})

	// err 是 OpenAI 协议收到限流响应时的调用错误。
	if _, err := complete(ctx, gateway, llmclient.ProtocolOpenAI); llmclient.StatusCode(err) != http.StatusTooManyRequests {
		t.Fatalf("OpenAI 应得到 429: %v", err)
	}
	// result 是拒绝回答时的响应；err 是调用错误，应为空。
	if result, err := complete(ctx, gateway, llmclient.ProtocolAnthropic); err != nil || !result.Refused {
		t.Fatalf("Anthropic 拒绝回答应标记 Refused: %+v %v", result, err)
	}
	// timeoutCtx 在响应延迟期间到期，网关应立即放弃等待。
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	// err 是延迟响应超过调用方超时的调用错误，应非空。
	if _, err := complete(timeoutCtx, gateway, llmclient.ProtocolOpenAI); err == nil {
		t.Fatal("延迟响应超过调用方超时应报错")
	}
	// response 是访问未知路径的结果。
	response, err := http.Get(gateway.URL() + "/unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("未知路径应 404: %d", response.StatusCode)
	}
}
