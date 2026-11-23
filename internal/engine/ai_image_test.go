package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"xianyu-go/internal/db"
)

// imageRawFrame 是带公网图片地址的买家图片消息原始帧，与平台真实帧的 image.pics 结构一致。
var imageRawFrame = map[string]any{"1": map[string]any{"10": map[string]any{"image": map[string]any{"pics": []any{map[string]any{"url": "https://img.example.com/buyer.png"}}}}}}

// capturingModelServer 启动记录请求正文的本地模型端点；first 非 0 时第一次请求返回该错误状态码，之后按 body 返回成功响应。
func capturingModelServer(t *testing.T, first int, body string) (*httptest.Server, func() []string) {
	t.Helper()
	// mu 保护 bodies；bodies 按顺序保存每次请求的正文。
	var mu sync.Mutex
	// bodies 按顺序保存本地端点收到的每次请求正文。
	var bodies []string
	// server 是记录请求正文并按需返回首次错误的本地模型端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// raw 是当前请求的原始正文字节。
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		// count 是包含本次请求在内的累计请求数。
		count := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if first != 0 && count == 1 {
			w.WriteHeader(first)
			_, _ = w.Write([]byte(`{"error":{"message":"image_url is not supported"}}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// openAIReplyBody 是 OpenAI 协议的固定成功响应。
const openAIReplyBody = `{"choices":[{"message":{"role":"assistant","content":"图片里写着 HELLO"}}]}`

// setupImageAI 创建启用 AI 的全场景客服账号并把全局模型端点指向 server；settings 是额外的全局设置。
func setupImageAI(t *testing.T, serverURL, mode string, settings map[string]string) (*db.Store, *AIReplierImpl) {
	t.Helper()
	// store 是临时测试库；cleanup 关闭并清理它。
	store, cleanup := newAIStore(t)
	t.Cleanup(cleanup)
	// ctx 是测试用的后台上下文。
	ctx := context.Background()
	// err 是预置账号 AI 设置时的写入错误，应为空。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts, ai_mode) VALUES ('cid', 1, '', ?)`, mode); err != nil {
		t.Fatal(err)
	}
	// key、value 是预置的全局 AI 连接设置键值。
	for key, value := range map[string]string{"ai_api_key": "sk-test", "ai_api_url": serverURL} {
		// err 是全局设置写入错误，应为空。
		if err := store.Settings.Set(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	// key、value 是调用方附加的全局设置键值。
	for key, value := range settings {
		// err 是附加设置写入错误，应为空。
		if err := store.Settings.Set(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	return store, NewAIReplier("cid", store, nil)
}

// imageMessage 构造带图片原始帧的买家消息。
func imageMessage() ChatMessage {
	// message 是带图片原始帧的买家消息，文字部分仍是占位符。
	message := chatMsg("[图片]", "item1", "chat1")
	message.Raw = imageRawFrame
	return message
}

// TestBuyerImageURL 验证只有占位符消息且原始帧含地址时才返回图片地址。
func TestBuyerImageURL(t *testing.T) {
	// got 是从图片消息取到的地址。
	if got := buyerImageURL(imageMessage()); got != "https://img.example.com/buyer.png" {
		t.Fatalf("图片消息应取到地址: %q", got)
	}
	// text 是不带图片的文字消息；noURL 是占位符但原始帧里没有地址的图片消息。
	text := chatMsg("你好", "item1", "chat1")
	text.Raw = imageRawFrame
	// noURL 是占位符消息但原始帧里取不到地址的情形。
	noURL := chatMsg(" [图片] ", "item1", "chat1")
	if buyerImageURL(text) != "" || buyerImageURL(noURL) != "" {
		t.Fatal("文字消息或取不到地址时必须返回空串")
	}
}

// TestAIReplyVisionSendsImagePart 验证开关开启时请求带图片片段，且图片地址不入库。
func TestAIReplyVisionSendsImagePart(t *testing.T) {
	// server 是记录请求的本地 OpenAI 端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, 0, openAIReplyBody)
	// store 是测试库；replier 是被测的 AI 回复器。
	store, replier := setupImageAI(t, server.URL, "full_service", map[string]string{"ai_vision_enabled": "true"})
	// result 是回复结果；err 是回复错误，应为空。
	result, err := replier.Reply(context.Background(), imageMessage())
	if err != nil || result == nil || result.Text != "图片里写着 HELLO" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// got 是唯一一次请求正文：最后一条消息含图片片段，文字部分不是占位符。
	got := bodies()
	if len(got) != 1 || !strings.Contains(got[0], `"image_url"`) || !strings.Contains(got[0], "https://img.example.com/buyer.png") || strings.Contains(got[0], `"text":"[图片]"`) {
		t.Fatalf("请求正文: %v", got)
	}
	// 对话历史在回复确认可发送后才保存：这里模拟提交成功后的持久化。
	if result.Persist == nil {
		t.Fatal("带会话与商品的回复应携带提交后持久化回调")
	}
	// persistErr 是提交后保存对话历史的错误，期望为空。
	if persistErr := result.Persist(context.Background()); persistErr != nil {
		t.Fatal(persistErr)
	}
	// history 中保存的仍是占位符，图片地址不得写入本地库。
	history, histErr := store.AIReply.ConversationHistory(context.Background(), "cid", "chat1", "item1", 10)
	if histErr != nil || len(history) != 2 || history[0].Content != "[图片]" {
		t.Fatalf("history=%+v err=%v", history, histErr)
	}
	// message 是历史里的一条消息，其内容不得包含图片地址。
	for _, message := range history {
		if strings.Contains(message.Content, "img.example.com") {
			t.Fatalf("图片地址不得入库: %+v", message)
		}
	}
}

// TestAIReplyVisionDisabledKeepsPlaceholder 验证开关关闭时仍只发送纯文字占位符。
func TestAIReplyVisionDisabledKeepsPlaceholder(t *testing.T) {
	// server 是记录请求的本地 OpenAI 端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, 0, openAIReplyBody)
	// replier 是未开启图片识别的 AI 回复器。
	_, replier := setupImageAI(t, server.URL, "full_service", nil)
	// err 是回复错误，应为空。
	if _, err := replier.Reply(context.Background(), imageMessage()); err != nil {
		t.Fatal(err)
	}
	// got 是已记录的请求正文列表。
	if got := bodies(); len(got) != 1 || strings.Contains(got[0], "image_url") || !strings.Contains(got[0], `"content":"[图片]"`) {
		t.Fatalf("开关关闭时必须保持纯文字占位符: %v", got)
	}
}

// TestAIReplyVisionFallsBackToPlainText 验证端点拒绝图片后降级为纯文字重试并成功。
func TestAIReplyVisionFallsBackToPlainText(t *testing.T) {
	// server 是首次返回 400 的本地端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, http.StatusBadRequest, openAIReplyBody)
	// replier 是开启图片识别的 AI 回复器。
	_, replier := setupImageAI(t, server.URL, "full_service", map[string]string{"ai_vision_enabled": "1"})
	// result 是降级后的回复结果；err 是回复错误，应为空。
	result, err := replier.Reply(context.Background(), imageMessage())
	if err != nil || result == nil || result.Text == "" {
		t.Fatalf("降级后应成功: result=%+v err=%v", result, err)
	}
	// got 是已记录的两次请求正文。
	if got := bodies(); len(got) != 2 || !strings.Contains(got[0], "image_url") || strings.Contains(got[1], "image_url") {
		t.Fatalf("应先带图再降级重试一次: %v", got)
	}
}

// TestAIReplyVisionServerErrorDoesNotRetry 验证 5xx 错误不触发降级重试。
func TestAIReplyVisionServerErrorDoesNotRetry(t *testing.T) {
	// server 是返回 500 的本地端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, http.StatusInternalServerError, openAIReplyBody)
	// replier 是开启图片识别的 AI 回复器。
	_, replier := setupImageAI(t, server.URL, "full_service", map[string]string{"ai_vision_enabled": "true"})
	// err 是回复错误，应不为空。
	if _, err := replier.Reply(context.Background(), imageMessage()); err == nil || len(bodies()) != 1 {
		t.Fatalf("5xx 不应重试: err=%v bodies=%d", err, len(bodies()))
	}
}

// TestAIReplyImageIgnoredInBargainOnlyMode 验证仅砍价模式不处理图片，也不请求模型。
func TestAIReplyImageIgnoredInBargainOnlyMode(t *testing.T) {
	// server 是本地端点；bodies 返回已记录的请求正文，应为空。
	server, bodies := capturingModelServer(t, 0, openAIReplyBody)
	// replier 是仅砍价模式的 AI 回复器。
	_, replier := setupImageAI(t, server.URL, "bargain_only", map[string]string{"ai_vision_enabled": "true"})
	// result 是回复结果，应为空；err 是回复错误，应为空。
	if result, err := replier.Reply(context.Background(), imageMessage()); err != nil || result != nil || len(bodies()) != 0 {
		t.Fatalf("仅砍价模式不应处理图片: result=%+v err=%v bodies=%d", result, err, len(bodies()))
	}
}

// TestAIReplyAnthropicProtocol 验证 Anthropic 协议端到端：系统提示词在顶层、图片为 url 源块。
func TestAIReplyAnthropicProtocol(t *testing.T) {
	// server 是本地 Anthropic 端点；bodies 返回已记录的请求正文。
	server, bodies := capturingModelServer(t, 0, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"看到了"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	// replier 是 Anthropic 协议并开启图片识别的 AI 回复器。
	_, replier := setupImageAI(t, server.URL, "full_service", map[string]string{"ai_api_protocol": "anthropic", "ai_vision_enabled": "true"})
	// result 是回复结果；err 是回复错误，应为空。
	result, err := replier.Reply(context.Background(), imageMessage())
	if err != nil || result == nil || result.Text != "看到了" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// payload 是 Anthropic 请求正文：系统提示词在顶层，图片为 url 源块。
	var payload map[string]any
	// got 是已记录的请求正文列表。
	if got := bodies(); len(got) != 1 || json.Unmarshal([]byte(got[0]), &payload) != nil || payload["system"] == nil || !strings.Contains(got[0], `"type":"url"`) {
		t.Fatalf("请求正文: %v", got)
	}
}

// TestAIReplyAnthropicRefusalFallsThrough 验证模型拒答时不接手，交给后续回复。
func TestAIReplyAnthropicRefusalFallsThrough(t *testing.T) {
	// server 是返回拒答的本地 Anthropic 端点。
	server, _ := capturingModelServer(t, 0, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"refusal","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
	// replier 是 Anthropic 协议的 AI 回复器。
	_, replier := setupImageAI(t, server.URL, "full_service", map[string]string{"ai_api_protocol": "anthropic"})
	// result 是回复结果，应为空；err 是回复错误，应为空。
	if result, err := replier.Reply(context.Background(), chatMsg("你好", "item1", "chat1")); err != nil || result != nil {
		t.Fatalf("拒绝回答应交给后续回复: result=%+v err=%v", result, err)
	}
}

// TestGlobalAIConfigProtocolAndVision 验证全局配置里协议与图片开关的默认值、Anthropic 默认值和未知协议回退。
func TestGlobalAIConfigProtocolAndVision(t *testing.T) {
	// store 是测试库；cleanup 关闭并清理它。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试用的后台上下文。
	ctx := context.Background()
	// replier 是读取全局配置的 AI 回复器。
	replier := NewAIReplier("cid", store, nil)
	// 默认：openai、关闭图片。
	cfg, err := replier.globalAIConfig(ctx)
	if err != nil || cfg.Protocol != "openai" || cfg.VisionEnabled {
		t.Fatalf("默认值: %+v err=%v", cfg, err)
	}
	// 迁移会预置 OpenAI 兼容地址与模型，这里清空以验证“未配置”时的 Anthropic 默认值。
	for key, value := range map[string]string{"ai_api_protocol": "anthropic", "ai_vision_enabled": " TRUE ", "ai_api_url": "", "ai_base_url": "", "ai_model": ""} {
		// err 是预置全局设置时的写入错误，应为空。
		if err := store.Settings.Set(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	// Anthropic 协议未配置地址和模型时使用 Anthropic 默认值。
	cfg, err = replier.globalAIConfig(ctx)
	if err != nil || cfg.Protocol != "anthropic" || !cfg.VisionEnabled || cfg.BaseURL != defaultAnthropicBaseURL || cfg.Model != "claude-opus-5-5" {
		t.Fatalf("anthropic 默认值: %+v err=%v", cfg, err)
	}
	// 无法识别的协议回退到 openai，而不是让 AI 回复整体失效。
	if err := store.Settings.Set(ctx, "ai_api_protocol", "gemini"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = replier.globalAIConfig(ctx); cfg.Protocol != "openai" || cfg.BaseURL != defaultAIBaseURL {
		t.Fatalf("未知协议应回退: %+v", cfg)
	}
}

// TestIsTruthySetting 验证设置值的真值解析，忽略大小写与空白。
func TestIsTruthySetting(t *testing.T) {
	// value 是设置原值；want 是期望的真值结果。
	for value, want := range map[string]bool{"true": true, " On ": true, "1": true, "YES": true, "": false, "false": false, "0": false} {
		if isTruthySetting(value) != want {
			t.Fatalf("%q 应为 %v", value, want)
		}
	}
}
