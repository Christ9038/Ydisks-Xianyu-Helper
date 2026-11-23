package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// anthropicReplyBody 是 Anthropic 协议的固定成功响应。
const anthropicReplyBody = `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"稍后发货"}],"usage":{"input_tokens":1,"output_tokens":1}}`

// newModelAgentFixture 创建使用 openai/anthropic 协议的 Agent 回复夹具；serverURL 是本地模型端点。
func newModelAgentFixture(t *testing.T, serverURL, protocol, model, prompt string, orderContext bool) (*AgentReplier, *recordingDelivery, *db.Store) {
	t.Helper()
	// store 是临时测试库；cleanup 关闭并清理它。
	store, cleanup := newReplyStore(t)
	t.Cleanup(cleanup)
	// err 是写入 Agent 回复配置（协议、模型、提示词）时的错误，应为空。
	if err := store.AgentReply.Upsert(context.Background(), "cid", db.AgentReplyWrite{Enabled: true, EndpointURL: serverURL,
		RequestTimeoutSeconds: 5, MaxSessionSeconds: 30, TokenAction: db.AgentTokenReplace, Token: "model-key",
		Protocol: protocol, Model: model, SystemPrompt: prompt, OrderContextEnabled: orderContext}); err != nil {
		t.Fatal(err)
	}
	// delivery 是记录发出回复的替身投递器。
	delivery := &recordingDelivery{}
	return NewAgentReplier("cid", store, delivery, nil), delivery, store
}

// TestAgentReplyViaOpenAIModel 验证 OpenAI 协议模式单次请求、使用自定义系统提示词、只发一条文字且不泄露会话标识原文。
func TestAgentReplyViaOpenAIModel(t *testing.T) {
	// server 是本地 OpenAI 端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, 0, openAIReplyBody)
	// replier 是被测 Agent 回复器；delivery 记录实际发出的回复。
	replier, delivery, _ := newModelAgentFixture(t, server.URL, db.AgentProtocolOpenAI, "gpt-x", "只回答发货问题", false)
	// result 是回复结果；err 是回复错误，应为空。
	result, err := replier.Reply(context.Background(), chatMsg("什么时候发货", "item", "chat-secret"))
	if err != nil || result == nil || !result.Skip || delivery.count() != 1 || delivery.messages[0].Text != "图片里写着 HELLO" {
		t.Fatalf("result=%+v err=%v sent=%v", result, err, delivery.messages)
	}
	// got 是唯一一次请求：自定义提示词作为 system，会话标识被哈希而不是原样发送。
	got := bodies()
	if len(got) != 1 || !strings.Contains(got[0], "只回答发货问题") || !strings.Contains(got[0], `"model":"gpt-x"`) || strings.Contains(got[0], "chat-secret") ||
		!strings.Contains(got[0], `"user":"`+opaqueChatID("chat-secret")+`"`) {
		t.Fatalf("请求正文: %v", got)
	}
}

// TestAgentReplyViaAnthropicModelWithImage 验证 Anthropic 协议模式使用默认模型与内置提示词并携带买家图片。
func TestAgentReplyViaAnthropicModelWithImage(t *testing.T) {
	// server 是本地 Anthropic 端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, 0, anthropicReplyBody)
	// 提示词为空：使用内置客服提示词（含承诺边界）；模型为空：使用 Anthropic 默认模型。
	replier, delivery, _ := newModelAgentFixture(t, server.URL, db.AgentProtocolAnthropic, "", "", false)
	// result 是回复结果；err 是回复错误，应为空。
	result, err := replier.Reply(context.Background(), imageMessage())
	if err != nil || result == nil || !result.Skip || delivery.count() != 1 || delivery.messages[0].Text != "稍后发货" {
		t.Fatalf("result=%+v err=%v sent=%v", result, err, delivery.messages)
	}
	// got 是已记录的请求正文，应只有一次且包含内置提示词边界与默认模型。
	if got := bodies(); len(got) != 1 || !strings.Contains(got[0], "不可覆盖的承诺边界") || !strings.Contains(got[0], "claude-opus-5-5") ||
		!strings.Contains(got[0], "https://img.example.com/buyer.png") {
		t.Fatalf("请求正文: %v", got)
	}
}

// TestAgentReplyModelImageFallsBackToText 验证端点拒绝图片时降级为纯文字重试并仍发出回复。
func TestAgentReplyModelImageFallsBackToText(t *testing.T) {
	// server 是首次返回 400 的本地端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, http.StatusBadRequest, openAIReplyBody)
	// replier 是被测 Agent 回复器；delivery 记录实际发出的回复。
	replier, delivery, _ := newModelAgentFixture(t, server.URL, db.AgentProtocolOpenAI, "gpt-x", "", false)
	// result 是降级后的回复结果；err 是回复错误，应为空。
	if result, err := replier.Reply(context.Background(), imageMessage()); err != nil || result == nil || delivery.count() != 1 {
		t.Fatalf("降级后应成功: %+v %v", result, err)
	}
	// got 是已记录的两次请求正文，第二次不含图片。
	if got := bodies(); len(got) != 2 || strings.Contains(got[1], "image_url") {
		t.Fatalf("应降级重试一次: %v", got)
	}
}

// TestAgentReplyModelOrderContext 验证开启订单阶段后系统提示词追加买家订单事实。
func TestAgentReplyModelOrderContext(t *testing.T) {
	// server 是本地 OpenAI 端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, 0, openAIReplyBody)
	// replier 是开启订单阶段的 Agent 回复器。
	replier, _, _ := newModelAgentFixture(t, server.URL, db.AgentProtocolOpenAI, "gpt-x", "", true)
	// err 是回复错误，应为空。
	if _, err := replier.Reply(context.Background(), chatMsg("发货了吗", "item", "chat")); err != nil {
		t.Fatal(err)
	}
	// got 是已记录的请求正文，应包含订单事实段落。
	if got := bodies(); len(got) != 1 || !strings.Contains(got[0], "买家订单事实") {
		t.Fatalf("开启订单阶段后提示词应包含事实区: %v", got)
	}
}

// TestAgentReplyModelFallsThrough 验证请求失败、拒答和空文本都返回空结果，交给 AI 与默认回复。
func TestAgentReplyModelFallsThrough(t *testing.T) {
	// 错误、拒绝回答、空文字与 openai 缺模型都应让后续 AI 与默认回复接管，且不发送任何内容。
	for name, tc := range map[string]struct {
		protocol, model, body string
		status                int
		wantErr               bool
	}{
		"上游 500":     {protocol: db.AgentProtocolOpenAI, model: "m", status: 500, wantErr: true},
		"空文字":        {protocol: db.AgentProtocolOpenAI, model: "m", body: `{"choices":[{"message":{"content":"  "}}]}`},
		"无 choices":  {protocol: db.AgentProtocolOpenAI, model: "m", body: `{"choices":[]}`},
		"拒绝回答":       {protocol: db.AgentProtocolAnthropic, body: `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"refusal","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`},
		"openai 缺模型": {protocol: db.AgentProtocolOpenAI, model: "", body: openAIReplyBody},
	} {
		// server 是按用例返回状态码与正文的本地端点。
		server, _ := capturingModelServer(t, tc.status, tc.body)
		if tc.status != 0 {
			// 复用夹具时第一次请求返回错误状态码，之后才返回成功正文；这里让每次请求都失败。
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", tc.status) })
		}
		// replier 是按用例协议与模型构造的 Agent 回复器；delivery 记录实际发出的回复。
		replier, delivery, _ := newModelAgentFixture(t, server.URL, tc.protocol, tc.model, "", false)
		// result 是回复结果；err 是回复错误。
		result, err := replier.Reply(context.Background(), chatMsg("你好", "item", "chat"))
		if result != nil || (err != nil) != tc.wantErr || delivery.count() != 0 {
			t.Fatalf("%s: result=%+v err=%v sent=%d", name, result, err, delivery.count())
		}
	}
}

// TestAgentReplyModelCancelledSuppressesReply 验证上下文取消后不发送任何回复，也不回落到后续回复。
func TestAgentReplyModelCancelledSuppressesReply(t *testing.T) {
	// release 在测试结束时放行被阻塞的端点；server 在收到请求后一直阻塞，模拟模型迟迟不返回。
	release := make(chan struct{})
	// server 收到请求后一直阻塞，模拟模型迟迟不返回，让请求在取消时仍在途。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	// replier 是被测 Agent 回复器；delivery 记录实际发出的回复。
	replier, delivery, _ := newModelAgentFixture(t, server.URL, db.AgentProtocolOpenAI, "gpt-x", "", false)
	// ctx 在请求发出后被取消：被新消息顶替或账号停止时必须抑制回复而不是降级。
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	// result 是回复结果，应为跳过后续回复；err 是回复错误。
	if result, err := replier.Reply(ctx, chatMsg("你好", "item", "chat")); err != nil || result == nil || !result.Skip || delivery.count() != 0 {
		t.Fatalf("取消应抑制回复: %+v %v sent=%d", result, err, delivery.count())
	}
}

// TestAgentReplyModelInvalidEndpointIsRejected 验证被出站策略拒绝的地址（云元数据地址）返回错误且不发送回复。
func TestAgentReplyModelInvalidEndpointIsRejected(t *testing.T) {
	// 受限地址（云元数据地址）被出站策略拒绝：返回错误让后续回复接管，不发出请求。
	replier, delivery, _ := newModelAgentFixture(t, "http://169.254.169.254", db.AgentProtocolOpenAI, "m", "", false)
	// result 是回复结果，应为空；err 是回复错误，应不为空。
	if result, err := replier.Reply(context.Background(), chatMsg("你好", "item", "chat")); result != nil || err == nil || delivery.count() != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// TestOpaqueChatID 验证会话标识哈希稳定、定长，空串保持为空，不同会话互不相同。
func TestOpaqueChatID(t *testing.T) {
	// first、again 是同一会话两次计算的结果，必须相同。
	first, again := opaqueChatID("c1"), opaqueChatID("c1")
	if opaqueChatID("") != "" || len(first) != 16 || first == opaqueChatID("c2") || first != again {
		t.Fatal("会话标识哈希应稳定、定长且区分不同会话")
	}
}
