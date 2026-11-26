package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordedRequest 保存测试端点收到的一次请求，用于断言协议格式。
type recordedRequest struct {
	// Path 是请求路径；Header 是请求头；Body 是解码后的 JSON 正文。
	Path   string
	Header http.Header
	Body   map[string]any
}

// newTestServer 启动本地模型端点：handler 按调用序号（从 1 开始）返回状态码与响应正文，所有请求按顺序记录。
func newTestServer(t *testing.T, handler func(call int) (int, string)) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	// records 保存收到的请求；calls 是并发安全的调用计数。
	records := &[]recordedRequest{}
	// calls 统计本地模型端点收到的请求次数，用于断言重试次数。
	var calls atomic.Int32
	// server 是记录请求并按回调返回状态与正文的本地模型端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// raw 是请求正文；body 是解码后的 JSON。
		raw, _ := io.ReadAll(r.Body)
		// body 是当前请求解码后的 JSON 正文，解码失败时保持为空映射。
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		*records = append(*records, recordedRequest{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		// status、payload 是本次调用要返回的状态码与正文。
		status, payload := handler(int(calls.Add(1)))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(server.Close)
	return server, records
}

// openAIOK 是 OpenAI 协议的成功响应。
const openAIOK = `{"choices":[{"message":{"role":"assistant","content":" 你好 "}}]}`

// anthropicOK 是 Anthropic 协议的成功响应，包含 thinking 块与两个文字块。
const anthropicOK = `{"id":"msg_1","type":"message","role":"assistant","model":"m","stop_reason":"end_turn",
"content":[{"type":"thinking","thinking":"x","signature":"s"},{"type":"text","text":"你"},{"type":"text","text":"好 "}],
"usage":{"input_tokens":1,"output_tokens":1}}`

// TestOpenAIPlainText 验证 OpenAI 协议纯文字请求的路径、鉴权与响应解析。
func TestOpenAIPlainText(t *testing.T) {
	// server 是本地 OpenAI 端点；records 返回已记录的请求。
	server, records := newTestServer(t, func(int) (int, string) { return 200, openAIOK })
	// result 是调用结果；err 是调用错误，应为空。
	result, err := Complete(context.Background(), Endpoint{Protocol: "", BaseURL: server.URL + "/v1/", APIKey: "k", Model: "m", HTTPClient: server.Client()},
		Request{System: "sys", History: []Turn{{Role: "user", Text: "a"}, {Role: "assistant", Text: "b"}}, UserText: "hi", User: "u1", MaxTokens: 5})
	if err != nil || result.Text != "你好" || result.ImageDropped {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// got 是唯一一次请求。
	got := (*records)[0]
	if got.Path != "/v1/chat/completions" || got.Header.Get("Authorization") != "Bearer k" {
		t.Fatalf("path=%s auth=%s", got.Path, got.Header.Get("Authorization"))
	}
	// messages 依次是 system、历史两条与本轮输入，本轮输入为纯字符串。
	messages := got.Body["messages"].([]any)
	if len(messages) != 4 || messages[3].(map[string]any)["content"] != "hi" || got.Body["user"] != "u1" || got.Body["max_tokens"] != float64(5) {
		t.Fatalf("body=%v", got.Body)
	}
}

// TestOpenAIImagePart 验证带图片时最后一条用户消息使用 image_url 片段。
func TestOpenAIImagePart(t *testing.T) {
	// server 是本地 OpenAI 端点；records 返回已记录的请求。
	server, records := newTestServer(t, func(int) (int, string) { return 200, openAIOK })
	// err 是调用错误，应为空。
	_, err := Complete(context.Background(), Endpoint{BaseURL: server.URL, Model: "m", HTTPClient: server.Client()},
		Request{UserText: ImagePrompt, ImageURLs: []string{"https://img.example/a.png"}})
	if err != nil {
		t.Fatal(err)
	}
	// parts 是最后一条 user 消息的多模态片段：文字在前、图片在后。
	messages := (*records)[0].Body["messages"].([]any)
	// parts 是最后一条消息的多段内容（文字片段与图片片段）。
	parts := messages[len(messages)-1].(map[string]any)["content"].([]any)
	if len(parts) != 2 || parts[0].(map[string]any)["text"] != ImagePrompt ||
		parts[1].(map[string]any)["image_url"].(map[string]any)["url"] != "https://img.example/a.png" {
		t.Fatalf("parts=%v", parts)
	}
}

// TestOpenAIEmptyChoices 验证响应没有 choices 时返回空文本而不是报错。
func TestOpenAIEmptyChoices(t *testing.T) {
	// server 是返回空 choices 的本地端点。
	server, _ := newTestServer(t, func(int) (int, string) { return 200, `{"choices":[]}` })
	// result 是调用结果；err 是调用错误，应为空。
	result, err := Complete(context.Background(), Endpoint{BaseURL: server.URL, HTTPClient: server.Client()}, Request{UserText: "hi"})
	if err != nil || result.Text != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// TestImageDowngradeRetriesOnce 验证图片被 400/415/422 拒绝时去掉图片并只重试一次。
func TestImageDowngradeRetriesOnce(t *testing.T) {
	// status 是模拟上游拒绝图片时返回的状态码。
	for _, status := range []int{400, 415, 422} {
		// protocol 是被测协议，两种协议行为一致。
		for _, protocol := range []string{ProtocolOpenAI, ProtocolAnthropic} {
			// server 是首次请求返回 status、之后成功的本地端点；records 返回已记录的请求。
			server, records := newTestServer(t, func(call int) (int, string) {
				if call == 1 {
					return status, `{"error":{"message":"image not supported","type":"invalid_request_error"},"type":"error"}`
				}
				if protocol == ProtocolAnthropic {
					return 200, anthropicOK
				}
				return 200, openAIOK
			})
			// result 是降级后的调用结果；err 是调用错误，应为空。
			result, err := Complete(context.Background(), Endpoint{Protocol: protocol, BaseURL: server.URL, Model: "m", HTTPClient: server.Client()},
				Request{UserText: ImagePrompt, ImageURLs: []string{"https://img.example/a.png"}})
			if err != nil || !result.ImageDropped || result.Status != status || result.Text == "" {
				t.Fatalf("%s/%d result=%+v err=%v", protocol, status, result, err)
			}
			if len(*records) != 2 || strings.Contains(mustJSON(t, (*records)[1].Body), "img.example") {
				t.Fatalf("%s/%d 重试请求仍含图片: %d", protocol, status, len(*records))
			}
		}
	}
}

// TestImageDowngradeRetryFailureReturnsError 验证降级重试仍失败时返回错误，且总共只请求两次。
func TestImageDowngradeRetryFailureReturnsError(t *testing.T) {
	// server 是始终返回 400 的本地端点；records 返回已记录的请求。
	server, records := newTestServer(t, func(int) (int, string) { return 400, `{"error":{"message":"bad"}}` })
	// err 是重试后仍存在的调用错误。
	_, err := Complete(context.Background(), Endpoint{BaseURL: server.URL, HTTPClient: server.Client()}, Request{UserText: "x", ImageURLs: []string{"https://i/a.png"}})
	if err == nil || len(*records) != 2 {
		t.Fatalf("err=%v calls=%d", err, len(*records))
	}
}

// TestNoRetryForOtherFailures 验证 401/429/5xx 即使带图片也不重试。
func TestNoRetryForOtherFailures(t *testing.T) {
	// status 是模拟上游返回的非图片类错误状态码。
	for _, status := range []int{401, 429, 500, 503} {
		// protocol 是被测协议。
		for _, protocol := range []string{ProtocolOpenAI, ProtocolAnthropic} {
			// server 是始终返回 status 的本地端点；records 返回已记录的请求。
			server, records := newTestServer(t, func(int) (int, string) { return status, `{"error":{"message":"x"}}` })
			// err 是调用错误，应不为空。
			_, err := Complete(context.Background(), Endpoint{Protocol: protocol, BaseURL: server.URL, HTTPClient: server.Client()},
				Request{UserText: "x", ImageURLs: []string{"https://i/a.png"}})
			if err == nil || len(*records) != 1 {
				t.Fatalf("%s/%d err=%v calls=%d", protocol, status, err, len(*records))
			}
		}
	}
}

// TestPlainTextBadRequestIsNotRetried 验证没有图片时的 400 不触发降级重试。
func TestPlainTextBadRequestIsNotRetried(t *testing.T) {
	// server 是始终返回 400 的本地端点；records 返回已记录的请求。
	server, records := newTestServer(t, func(int) (int, string) { return 400, `{"error":{"message":"x"}}` })
	// err 是调用错误，应不为空。
	if _, err := Complete(context.Background(), Endpoint{BaseURL: server.URL, HTTPClient: server.Client()}, Request{UserText: "x"}); err == nil || len(*records) != 1 {
		t.Fatalf("err=%v calls=%d", err, len(*records))
	}
}

// TestAnthropicRequestShape 验证 Anthropic 请求的路径、鉴权头、系统提示词、图片源和不带 temperature。
func TestAnthropicRequestShape(t *testing.T) {
	// server 是本地 Anthropic 端点；records 返回已记录的请求。
	server, records := newTestServer(t, func(int) (int, string) { return 200, anthropicOK })
	// result 是调用结果；err 是调用错误，应为空。
	result, err := Complete(context.Background(), Endpoint{Protocol: "Anthropic", BaseURL: server.URL + "/v1", APIKey: "secret", HTTPClient: server.Client()},
		Request{System: "sys", History: []Turn{{Role: "assistant", Text: "开头的助手消息应被丢弃"}, {Role: "user", Text: "a"}, {Role: "assistant", Text: "b"}, {Role: "user", Text: " "}},
			UserText: ImagePrompt, ImageURLs: []string{"https://img.example/a.png"}, User: "abc"})
	if err != nil || result.Text != "你好" || result.Refused {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// got 是唯一一次请求：路径不应重复 /v1，密钥走 x-api-key 头。
	got := (*records)[0]
	if got.Path != "/v1/messages" || got.Header.Get("X-Api-Key") != "secret" || got.Header.Get("Anthropic-Version") == "" {
		t.Fatalf("path=%s headers=%v", got.Path, got.Header)
	}
	if got.Body["model"] != DefaultAnthropicModel || got.Body["max_tokens"] != float64(anthropicDefaultMaxTokens) {
		t.Fatalf("body=%v", got.Body)
	}
	// has 表示请求体是否带了 temperature（新模型会拒绝该参数，必须没有）。
	if _, has := got.Body["temperature"]; has {
		t.Fatal("不得发送 temperature")
	}
	if got.Body["metadata"].(map[string]any)["user_id"] != "abc" {
		t.Fatalf("metadata=%v", got.Body["metadata"])
	}
	// system 是文字块数组；messages 第一条是 user，最后一条先图片后文字。
	if system := got.Body["system"].([]any); system[0].(map[string]any)["text"] != "sys" {
		t.Fatalf("system=%v", got.Body["system"])
	}
	// messages 是请求体中的消息列表。
	messages := got.Body["messages"].([]any)
	if len(messages) != 3 || messages[0].(map[string]any)["role"] != "user" || messages[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("messages=%v", messages)
	}
	// content 是最后一条消息的内容块。
	content := messages[2].(map[string]any)["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["type"] != "image" ||
		content[0].(map[string]any)["source"].(map[string]any)["url"] != "https://img.example/a.png" ||
		content[1].(map[string]any)["text"] != ImagePrompt {
		t.Fatalf("content=%v", content)
	}
}

// TestAnthropicRefusalAndEmpty 验证拒答与空文本两种响应的返回形态。
func TestAnthropicRefusalAndEmpty(t *testing.T) {
	// server 是依次返回拒答、空文本的本地端点。
	server, _ := newTestServer(t, func(call int) (int, string) {
		if call == 1 {
			return 200, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"refusal","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`
		}
		return 200, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"x","signature":"s"}],"usage":{"input_tokens":1,"output_tokens":1}}`
	})
	// endpoint 是指向本地端点的 Anthropic 配置。
	endpoint := Endpoint{Protocol: ProtocolAnthropic, BaseURL: server.URL, Model: "claude-x", HTTPClient: server.Client()}
	// result 是第一次调用的结果；err 是调用错误，应为空。
	if result, err := Complete(context.Background(), endpoint, Request{UserText: "x", MaxTokens: 16}); err != nil || !result.Refused {
		t.Fatalf("refusal result=%+v err=%v", result, err)
	}
	// result 是第二次调用的结果；err 是调用错误，应为空。
	if result, err := Complete(context.Background(), endpoint, Request{UserText: "x"}); err != nil || result.Text != "" || result.Refused {
		t.Fatalf("empty result=%+v err=%v", result, err)
	}
}

// TestConfigurationErrors 验证未知协议与空地址返回对应的配置错误。
func TestConfigurationErrors(t *testing.T) {
	// err 是未知协议触发的配置错误。
	if _, err := Complete(context.Background(), Endpoint{Protocol: "gemini", BaseURL: "http://x"}, Request{}); err != ErrUnknownProtocol {
		t.Fatalf("err=%v", err)
	}
	// err 是空地址触发的配置错误。
	if _, err := Complete(context.Background(), Endpoint{BaseURL: "  /"}, Request{}); err != ErrEmptyBaseURL {
		t.Fatalf("err=%v", err)
	}
	// 未注入 HTTP 客户端时走 netguard：回环地址属于受限地址，不得发出请求。
	if _, err := Complete(context.Background(), Endpoint{BaseURL: "http://127.0.0.1:1"}, Request{UserText: "x"}); err == nil {
		t.Fatal("受限地址应被拒绝")
	}
}

// TestNormalizeProtocol 验证协议名的大小写与空白归一，未知值报错。
func TestNormalizeProtocol(t *testing.T) {
	// input 是待归一的协议名；want 是期望的归一结果。
	for input, want := range map[string]string{"": ProtocolOpenAI, " OpenAI ": ProtocolOpenAI, "anthropic": ProtocolAnthropic} {
		// got 是归一后的协议；err 是归一错误，应为空。
		if got, err := NormalizeProtocol(input); err != nil || got != want {
			t.Fatalf("%q -> %q %v", input, got, err)
		}
	}
	// err 是未知协议触发的错误，应不为空。
	if _, err := NormalizeProtocol("x"); err == nil {
		t.Fatal("未知协议应报错")
	}
}

// TestAnthropicBaseURL 验证 Anthropic 地址会去掉末尾的 /v1，避免 SDK 重复拼接。
func TestAnthropicBaseURL(t *testing.T) {
	// input 是用户填写的地址；want 是传给 SDK 的地址。
	for input, want := range map[string]string{"https://a.com": "https://a.com", "https://a.com/v1": "https://a.com", "https://a.com/v1/": "https://a.com", "https://a.com/x": "https://a.com/x"} {
		// got 是实际归一后的地址。
		if got := anthropicBaseURL(input); got != want {
			t.Fatalf("%q -> %q", input, got)
		}
	}
}

// mustJSON 把请求正文编码回字符串，便于检查其中是否仍含图片地址。
func mustJSON(t *testing.T, value any) string {
	t.Helper()
	// raw 是序列化后的 JSON 字节；err 是序列化错误。
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestMultipleImagesKeepOrder 验证一次请求携带多张图片时两种协议都按时间顺序为每张图片生成独立片段，文字片段位置不变。
func TestMultipleImagesKeepOrder(t *testing.T) {
	// urls 是按时间顺序排列的买家图片地址。
	urls := []string{"https://img.example/1.png", "https://img.example/2.png", "https://img.example/3.png"}
	// protocol 是被测协议。
	for _, protocol := range []string{ProtocolOpenAI, ProtocolAnthropic} {
		// body 是该协议的成功响应。
		body := openAIOK
		if protocol == ProtocolAnthropic {
			body = anthropicOK
		}
		// server、records 是本地端点及其记录的请求。
		server, records := newTestServer(t, func(int) (int, string) { return 200, body })
		// err 是调用错误，应为空。
		_, err := Complete(context.Background(), Endpoint{Protocol: protocol, BaseURL: server.URL, Model: "m", HTTPClient: server.Client()},
			Request{UserText: "看图", ImageURLs: urls})
		if err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		// messages 是请求体中的消息列表；last 是最后一条 user 消息的内容片段。
		messages := (*records)[0].Body["messages"].([]any)
		// last 是请求体中最后一条用户消息的 content 数组。
		last := messages[len(messages)-1].(map[string]any)["content"].([]any)
		if len(last) != 4 {
			t.Fatalf("%s: 应为 1 个文字片段 + 3 个图片片段: %v", protocol, last)
		}
		// got 是按出现顺序提取到的图片地址。
		var got []string
		// part 是 content 数组中的一个文字或图片块。
		for _, part := range last {
			// item 是当前片段；两种协议的图片字段结构不同。
			item := part.(map[string]any)
			if protocol == ProtocolOpenAI && item["type"] == "image_url" {
				got = append(got, item["image_url"].(map[string]any)["url"].(string))
			}
			if protocol == ProtocolAnthropic && item["type"] == "image" {
				got = append(got, item["source"].(map[string]any)["url"].(string))
			}
		}
		if strings.Join(got, ",") != strings.Join(urls, ",") {
			t.Fatalf("%s: 图片顺序错误: %v", protocol, got)
		}
	}
}

// TestNewHTTPClientUsesUnifiedRequestTimeout 验证模型客户端整体超时为 RequestTimeout，且非法地址被拒绝。
func TestNewHTTPClientUsesUnifiedRequestTimeout(t *testing.T) {
	// client、err 是按合法地址构造的模型请求客户端及构造错误。
	client, err := NewHTTPClient("https://api.example.com/v1")
	if err != nil || client == nil {
		t.Fatalf("合法地址应构造成功: client=%v err=%v", client, err)
	}
	if client.Timeout != RequestTimeout || RequestTimeout != 60*time.Second {
		t.Fatalf("整体超时应为 60 秒: got=%v const=%v", client.Timeout, RequestTimeout)
	}
	// badErr 是非法地址的构造错误，必须非空。
	if _, badErr := NewHTTPClient("not a url"); badErr == nil {
		t.Fatal("非法地址必须返回错误")
	}
}
