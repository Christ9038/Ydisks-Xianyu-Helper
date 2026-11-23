package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseAIModelsSupportsOpenAIShapes 验证模型目录解析兼容 data、models 和名称回退结构。
func TestParseAIModelsSupportsOpenAIShapes(t *testing.T) {
	// models、err 保存模型目录解析结果和错误。
	models, err := ParseAIModels([]byte(`{"data":[{"id":"qwen-plus"},{"name":"fallback"},{"id":"qwen-plus"}]}`))
	if err != nil || len(models) != 2 || models[0] != "qwen-plus" || models[1] != "fallback" {
		t.Fatalf("models=%v err=%v", models, err)
	}
}

// TestAIModelClientFetchUsesHeaderAndBounds 验证模型请求只把密钥放入请求头并拒绝空模型列表。
func TestAIModelClientFetchUsesHeaderAndBounds(t *testing.T) {
	// server 是返回固定模型目录的本地测试端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("authorization header was not forwarded")
		}
		_, _ = writer.Write([]byte(`{"models":["qwen-plus"]}`))
	}))
	defer server.Close()
	// client 是替换了受信任端点 HTTP 工厂的测试客户端。
	client := &AIModelClient{newHTTPClient: func(string) (*http.Client, error) { return &http.Client{}, nil }}
	// models、err 保存模型请求结果和错误。
	models, err := client.Fetch(context.Background(), server.URL, "test-secret", "openai")
	if err != nil || len(models) != 1 || models[0] != "qwen-plus" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	// oversizedErr 表示超大模型响应被拒绝的错误。
	_, oversizedErr := ReadAIModelsBody(strings.NewReader(strings.Repeat("x", MaxAIModelsResponseBytes+1)))
	if oversizedErr == nil {
		t.Fatal("oversized model response should fail")
	}
}

// TestAIModelParsingAndErrorFormatting 覆盖模型目录根数组、非法 JSON、空目录和错误正文截断。
func TestAIModelParsingAndErrorFormatting(t *testing.T) {
	// rootModels、rootErr 保存根数组模型目录解析结果。
	rootModels, rootErr := ParseAIModels([]byte(`[" a ",{"name":"b"},42]`))
	if rootErr != nil || len(rootModels) != 2 || rootModels[0] != "a" || rootModels[1] != "b" {
		t.Fatalf("root models=%v err=%v", rootModels, rootErr)
	}
	// invalidModels、invalidErr 保存非法 JSON 的解析结果。
	invalidModels, invalidErr := ParseAIModels([]byte("{"))
	if invalidModels != nil || invalidErr == nil {
		t.Fatalf("invalid models=%v err=%v", invalidModels, invalidErr)
	}
	// emptyModels、emptyErr 保存没有目录字段的兼容响应结果。
	emptyModels, emptyErr := ParseAIModels([]byte(`{"unexpected":[]}`))
	if emptyErr != nil || len(emptyModels) != 0 {
		t.Fatalf("empty models=%v err=%v", emptyModels, emptyErr)
	}
	// short、long 保存错误正文截断前后的文本。
	short := truncateAIModelBody(" short ", 10)
	// long 保存超过上限时截断后的错误正文。
	long := truncateAIModelBody("1234567890", 5)
	if short != "short" || long != "12345" {
		t.Fatalf("body=%q/%q", short, long)
	}
}

// TestAIModelClientTestConnection 验证连接测试使用独立长时客户端、正确的 chat 请求形状和兼容回复解析。
func TestAIModelClientTestConnection(t *testing.T) {
	// server 是断言实际请求路径、认证头和 JSON 载荷的本地 OpenAI 兼容端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/chat/completions" {
			t.Errorf("unexpected request=%s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("authorization header was not forwarded")
		}
		// payload 保存解码后的最小诊断请求，避免通过字符串比较遗漏 JSON 转义问题。
		var payload aiConnectionTestRequest
		// decodeErr 表示本地替身未收到符合约定的 JSON 请求正文。
		if decodeErr := json.NewDecoder(request.Body).Decode(&payload); decodeErr != nil {
			t.Fatalf("decode payload: %v", decodeErr)
		}
		if payload.Model != "reasoning-model" || payload.MaxTokens != AIConnectionTestMaxTokens || len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "你好" {
			t.Fatalf("unexpected payload=%+v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":"连接成功"}]}}]}`))
	}))
	defer server.Close()
	// client 仅装配测试连接客户端，验证 TestConnection 不错误依赖模型目录客户端工厂。
	client := &AIModelClient{newTestHTTPClient: func(string) (*http.Client, error) { return server.Client(), nil }}
	// result、testErr 分别保存连接测试的非敏感结果及失败原因。
	result, testErr := client.TestConnection(context.Background(), server.URL+"/", "test-secret", " reasoning-model ", "openai")
	if testErr != nil {
		t.Fatalf("test connection: %v", testErr)
	}
	if result.Model != "reasoning-model" || result.Reply != "连接成功" || result.LatencyMS < 0 {
		t.Fatalf("unexpected result=%+v", result)
	}
}

// TestAIModelClientTestConnectionClassifiesErrors 验证上游失败不会把远端正文返回给管理员页面，并保留可操作的分类提示。
func TestAIModelClientTestConnectionClassifiesErrors(t *testing.T) {
	// cases 保存不同 HTTP 状态对应的用户可见诊断分类。
	cases := []struct {
		// name 是当前上游失败场景的稳定名称。
		name string
		// status 是本地替身返回的 HTTP 状态码。
		status int
		// want 是结果错误文本必须包含的分类提示。
		want string
	}{
		{name: "authentication", status: http.StatusUnauthorized, want: "认证失败"},
		{name: "not-found", status: http.StatusNotFound, want: "模型不存在"},
		{name: "rate-limited", status: http.StatusTooManyRequests, want: "额度或请求频率受限"},
		{name: "upstream", status: http.StatusBadGateway, want: "AI 服务请求失败"},
	}
	// testCase 是当前正在验证的上游失败分类。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 是返回秘密样式正文的上游替身，用于确认错误响应不会被转发。
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte("upstream-secret-body"))
			}))
			defer server.Close()
			// client 使用本地替身的 HTTP 客户端，避免触发真实外部服务。
			client := &AIModelClient{newTestHTTPClient: func(string) (*http.Client, error) { return server.Client(), nil }}
			// result、testErr 分别保存失败测试返回的诊断信息和错误。
			result, testErr := client.TestConnection(context.Background(), server.URL, "test-secret", "test-model", "openai")
			if testErr == nil || !strings.Contains(testErr.Error(), testCase.want) || strings.Contains(testErr.Error(), "upstream-secret-body") {
				t.Fatalf("result=%+v err=%v", result, testErr)
			}
			if result.Model != "test-model" || result.LatencyMS < 0 {
				t.Fatalf("unexpected result=%+v", result)
			}
		})
	}
}

// TestExtractChatReplyAcceptsReasoningOnlyResponse 验证推理模型在尚未给出可显示正文时仍被识别为有效 chat completion。
func TestExtractChatReplyAcceptsReasoningOnlyResponse(t *testing.T) {
	// reply、replyErr 分别保存仅含 reasoning_content 的兼容响应解析结果及错误。
	reply, replyErr := extractChatReply([]byte(`{"choices":[{"message":{"reasoning_content":"正在推理"}}]}`))
	if replyErr != nil || reply != "模型已返回推理结果" {
		t.Fatalf("reply=%q err=%v", reply, replyErr)
	}
}
