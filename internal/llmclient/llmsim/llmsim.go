// Package llmsim 提供进程内的 LLM 网关模拟器，仅供测试使用。
//
// 它同时模拟 OpenAI 兼容的 /chat/completions 与 Anthropic 的 /v1/messages 两种协议：被测代码使用真实的
// llmclient（含 SDK 序列化、鉴权头、错误分类），只有服务端是本包的 httptest 服务。模拟器按脚本依次返回
// 成功文本、HTTP 错误、拒绝回答、空内容或延迟响应，并完整记录每个请求，供测试断言“发给 AI 的内容”
// （系统提示词、用户输入、模型名、密钥头）与调用次数。
//
// 模拟器不访问外部网络，全部密钥和文本都是测试数据；生产代码不得导入本包。
package llmsim

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Step 描述网关对下一次请求的响应。零值表示返回空文本的成功响应。
type Step struct {
	// Status 是 HTTP 状态码；非 0 且不是 200 时返回该状态与一段错误正文，忽略其他字段。
	Status int
	// Text 是成功响应中的模型文本。
	Text string
	// Refuse 为真时返回“拒绝回答”：Anthropic 用 stop_reason=refusal，OpenAI 返回空内容。
	Refuse bool
	// Delay 是响应前的等待时间；调用方取消上下文时立即结束等待，用于模拟超时。
	Delay time.Duration
}

// Message 是请求中的一条对话消息。
type Message struct {
	// Role 是 system、user 或 assistant。
	Role string
	// Text 是该消息的纯文字内容。
	Text string
}

// Request 是网关记录下来的一次请求。
type Request struct {
	// Protocol 是 openai 或 anthropic，由请求路径决定。
	Protocol string
	// Path 是请求路径。
	Path string
	// Authorization 是 Authorization 头原文（OpenAI 用 Bearer）。
	Authorization string
	// APIKey 是 x-api-key 头原文（Anthropic 使用）。
	APIKey string
	// Model 是请求声明的模型名。
	Model string
	// System 是系统提示词。
	System string
	// Messages 是除系统提示词外的对话消息。
	Messages []Message
	// MaxTokens 是请求声明的输出上限，未声明为 0。
	MaxTokens int
	// Body 是请求体原文，供需要检查是否含某段文本的断言使用。
	Body string
}

// UserText 返回最后一条 user 消息的文字，没有时为空串。
func (r Request) UserText() string {
	// index 是从后向前遍历消息时的位置。
	for index := len(r.Messages) - 1; index >= 0; index-- {
		if r.Messages[index].Role == "user" {
			return r.Messages[index].Text
		}
	}
	return ""
}

// Gateway 是进程内的 LLM 网关。并发安全：mu 保护 script、fallback 与 requests。
type Gateway struct {
	// server 是承载两种协议路径的 httptest 服务。
	server *httptest.Server
	// mu 保护下面三个字段。
	mu sync.Mutex
	// script 是待消费的响应脚本，按入队顺序使用。
	script []Step
	// fallback 是脚本耗尽后的响应。
	fallback Step
	// requests 按到达顺序保存全部请求。
	requests []Request
}

// New 创建网关并在用例结束时关闭。默认响应为空文本的成功响应，测试应通过 Enqueue 或 SetFallback 指定内容。
func New(t testing.TB) *Gateway {
	t.Helper()
	// gateway 是待返回的网关。
	gateway := &Gateway{}
	gateway.server = httptest.NewServer(http.HandlerFunc(gateway.serve))
	t.Cleanup(gateway.server.Close)
	return gateway
}

// URL 返回网关根地址，可直接作为 OpenAI 的 BaseURL；Anthropic 协议会自行拼接 /v1/messages。
func (g *Gateway) URL() string { return g.server.URL }

// Enqueue 追加若干一次性响应步骤。
func (g *Gateway) Enqueue(steps ...Step) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.script = append(g.script, steps...)
}

// SetFallback 设置脚本耗尽后的响应。
func (g *Gateway) SetFallback(step Step) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fallback = step
}

// Requests 返回已记录请求的副本。
func (g *Gateway) Requests() []Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Request(nil), g.requests...)
}

// Calls 返回已收到的请求数量。
func (g *Gateway) Calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.requests)
}

// next 取出下一步响应；脚本耗尽时返回兜底响应。
func (g *Gateway) next() Step {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.script) == 0 {
		return g.fallback
	}
	// step 是脚本中取出的下一步响应。
	step := g.script[0]
	g.script = g.script[1:]
	return step
}

// serve 识别协议、记录请求并按脚本响应。
func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	// body 是请求体原文。
	body, _ := io.ReadAll(r.Body)
	// record 是本次请求的记录。
	record := Request{Path: r.URL.Path, Authorization: r.Header.Get("Authorization"), APIKey: r.Header.Get("x-api-key"), Body: string(body)}
	switch {
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		record.Protocol = "openai"
		parseOpenAI(&record, body)
	case strings.HasSuffix(r.URL.Path, "/messages"):
		record.Protocol = "anthropic"
		parseAnthropic(&record, body)
	default:
		http.NotFound(w, r)
		return
	}
	g.mu.Lock()
	g.requests = append(g.requests, record)
	g.mu.Unlock()

	// step 是本次请求对应的脚本步骤。
	step := g.next()
	if step.Delay > 0 {
		select {
		case <-time.After(step.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if step.Status != 0 && step.Status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.Status)
		_, _ = w.Write([]byte(`{"error":{"message":"simulated upstream failure","type":"simulated_error"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if record.Protocol == "anthropic" {
		writeAnthropic(w, step)
		return
	}
	writeOpenAI(w, step)
}

// parseOpenAI 解析 OpenAI 兼容请求体的模型、消息和输出上限。
func parseOpenAI(record *Request, body []byte) {
	// payload 对应 chat/completions 请求体；content 可能是字符串或多模态数组，这里只取文字。
	var payload struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &payload)
	record.Model, record.MaxTokens = payload.Model, payload.MaxTokens
	for /* message 是请求中的一条消息。 */ _, message := range payload.Messages {
		// text 是该消息的文字内容。
		text := plainText(message.Content)
		if message.Role == "system" {
			record.System = text
			continue
		}
		record.Messages = append(record.Messages, Message{Role: message.Role, Text: text})
	}
}

// parseAnthropic 解析 Anthropic Messages 请求体的模型、系统提示词、消息和输出上限。
func parseAnthropic(record *Request, body []byte) {
	// payload 对应 /v1/messages 请求体；system 与消息 content 可能是字符串或文字块数组。
	var payload struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    json.RawMessage `json:"system"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &payload)
	record.Model, record.MaxTokens, record.System = payload.Model, payload.MaxTokens, plainText(payload.System)
	for /* message 是请求中的一条消息。 */ _, message := range payload.Messages {
		record.Messages = append(record.Messages, Message{Role: message.Role, Text: plainText(message.Content)})
	}
}

// plainText 把字符串或 [{type:text,text:...}] 形式的内容折叠为纯文字。
func plainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// text 是字符串形式的内容。
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	// parts 是文字块数组形式的内容。
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	// joined 按顺序拼接全部文字块。
	var joined strings.Builder
	for /* part 是一个文字块。 */ _, part := range parts {
		joined.WriteString(part.Text)
	}
	return joined.String()
}

// writeOpenAI 写出 OpenAI 兼容的成功响应；拒绝回答时返回空内容。
func writeOpenAI(w http.ResponseWriter, step Step) {
	// content 是返回给调用方的模型文本。
	content := step.Text
	if step.Refuse {
		content = ""
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-sim", "object": "chat.completion", "model": "sim",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}},
	})
}

// writeAnthropic 写出 Anthropic Messages 的成功响应；拒绝回答时 stop_reason 为 refusal。
func writeAnthropic(w http.ResponseWriter, step Step) {
	// stopReason 是响应的停止原因。
	stopReason := "end_turn"
	if step.Refuse {
		stopReason = "refusal"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "msg_sim", "type": "message", "role": "assistant", "model": "sim", "stop_reason": stopReason,
		"content": []any{map[string]any{"type": "text", "text": step.Text}},
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
}
