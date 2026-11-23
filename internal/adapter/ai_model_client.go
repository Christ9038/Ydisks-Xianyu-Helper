package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	settingsapp "xianyu-go/internal/application/settings"
	"xianyu-go/internal/llmclient"
	"xianyu-go/internal/netguard"
)

// MaxAIModelsResponseBytes 限制远端模型目录响应大小，避免管理员配置的端点耗尽内存。
const MaxAIModelsResponseBytes = 4 << 20

// AIConnectionTestMaxTokens 限制测试对话最多生成的 token，既保证推理模型有机会给出可见文本，也避免诊断请求消耗过多额度。
const AIConnectionTestMaxTokens = 64

// AIModelClient 通过管理员明确配置的端点读取 AI 模型目录。
type AIModelClient struct {
	// newHTTPClient 允许测试替换端点客户端，同时生产默认使用受信任端点策略。
	newHTTPClient func(baseURL string) (*http.Client, error)
	// newTestHTTPClient 允许测试连接使用更长的超时，适应慢推理模型。
	newTestHTTPClient func(baseURL string) (*http.Client, error)
}

// NewAIModelClient 构造 AI 模型目录适配器。
func NewAIModelClient() *AIModelClient {
	// client 保存生产环境使用的模型目录客户端。
	client := &AIModelClient{
		newHTTPClient: func(baseURL string) (*http.Client, error) {
			return netguard.ConfiguredEndpointHTTPClient(baseURL, 20*time.Second)
		},
		newTestHTTPClient: func(baseURL string) (*http.Client, error) {
			return netguard.ConfiguredEndpointHTTPClient(baseURL, 50*time.Second)
		},
	}
	return client
}

// Fetch 请求模型目录并只返回模型名称，不返回 API 密钥或原始响应内容。
// protocol 为 anthropic 时读取 Anthropic 模型目录，其余按 OpenAI 兼容协议处理。
func (c *AIModelClient) Fetch(ctx context.Context, baseURL, apiKey, protocol string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("AI API 地址为空")
	}
	if c == nil || c.newHTTPClient == nil {
		return nil, fmt.Errorf("AI 模型客户端未初始化")
	}
	if protocol == llmclient.ProtocolAnthropic {
		return c.fetchAnthropic(ctx, baseURL, apiKey)
	}
	// req 是带有请求上下文的模型目录请求；API 密钥只存在于请求头。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	// client 是经过地址校验的出站客户端。
	client, err := c.newHTTPClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("AI API 地址无效: %w", err)
	}
	// response 是远端模型目录 HTTP 响应。
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	defer response.Body.Close()
	// raw 是限制大小后读取的响应内容。
	raw, err := ReadAIModelsBody(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("读取模型失败: HTTP %d %s", response.StatusCode, truncateAIModelBody(string(raw), 180))
	}
	// models、err 保存解析后的模型名称及解析错误。
	models, err := ParseAIModels(raw)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

// ReadAIModelsBody 读取并限制远端模型目录响应。
func ReadAIModelsBody(reader io.Reader) ([]byte, error) {
	// raw 是限制大小后读取的响应内容。
	raw, err := io.ReadAll(io.LimitReader(reader, MaxAIModelsResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxAIModelsResponseBytes {
		return nil, fmt.Errorf("模型列表响应超过 %d MiB", MaxAIModelsResponseBytes>>20)
	}
	return raw, nil
}

// ParseAIModels 从兼容 OpenAI 的多种响应形状提取去重模型名称。
func ParseAIModels(raw []byte) ([]string, error) {
	// payload 是远端模型目录的通用 JSON 载荷。
	var payload any
	// err 表示远端模型目录 JSON 解析错误。
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	// seen 保存已返回的模型名称，避免重复项污染下拉列表。
	seen := make(map[string]bool)
	// result 保存按远端出现顺序排列的模型名称。
	var result []string
	// addModel 将非空模型名称加入结果并去重。
	addModel := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		result = append(result, value)
	}
	// walk 递归解析 data、models、对象和字符串等兼容响应结构。
	var walk func(any)
	walk = func(value any) {
		// typed 是当前 JSON 节点的具体类型。
		switch typed := value.(type) {
		case []any:
			// item 是模型目录数组中的当前元素。
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			// id、ok 保存优先使用的模型标识及其类型判断结果。
			if id, ok := typed["id"].(string); ok && id != "" {
				addModel(id)
			} else if // name、ok 保存模型名称回退值及其类型判断结果。
			name, ok := typed["name"].(string); ok && name != "" {
				addModel(name)
			}
		case string:
			addModel(typed)
		}
	}
	// root、ok 保存顶层对象及对象类型判断结果。
	if root, ok := payload.(map[string]any); ok {
		// data、ok 保存兼容 OpenAI 的 data 字段及存在性判断结果。
		if data, ok := root["data"]; ok {
			walk(data)
		} else if // models、ok 保存兼容服务的 models 字段及存在性判断结果。
		models, ok := root["models"]; ok {
			walk(models)
		}
	} else {
		walk(payload)
	}
	return result, nil
}

// aiConnectionTestRequest 是发往 OpenAI 兼容 chat completion 端点的最小请求载荷。
type aiConnectionTestRequest struct {
	// Model 是管理员本次要验证的模型名称。
	Model string `json:"model"`
	// Messages 只包含一条短文本，避免测试消耗实际业务对话上下文。
	Messages []aiConnectionTestMessage `json:"messages"`
	// MaxTokens 约束诊断请求的最大生成额度。
	MaxTokens int `json:"max_tokens"`
}

// aiConnectionTestMessage 是 OpenAI 兼容消息数组中的单条用户消息。
type aiConnectionTestMessage struct {
	// Role 固定为 user，要求模型生成一次正常助手回复。
	Role string `json:"role"`
	// Content 是不包含账号或商品信息的固定测试文本。
	Content string `json:"content"`
}

// aiChatCompletionResponse 是连接测试需要读取的最小 OpenAI 兼容响应形状。
type aiChatCompletionResponse struct {
	// Choices 保存服务端给出的候选完成结果。
	Choices []aiChatCompletionChoice `json:"choices"`
}

// aiChatCompletionChoice 是候选完成结果，兼容 chat message 和旧式 text 响应。
type aiChatCompletionChoice struct {
	// Message 保存 chat completion 的结构化回复。
	Message aiChatCompletionMessage `json:"message"`
	// Text 保存少数兼容服务仍使用的扁平回复文本。
	Text string `json:"text"`
}

// aiChatCompletionMessage 保存可显示正文与推理正文的兼容 JSON 字段。
type aiChatCompletionMessage struct {
	// Content 是模型面对用户的正文，可能是字符串或内容块数组。
	Content json.RawMessage `json:"content"`
	// ReasoningContent 是部分推理模型在 token 用尽前仅返回的推理字段，不能直接展示给用户。
	ReasoningContent json.RawMessage `json:"reasoning_content"`
}

// TestConnection 发送一次最小 chat completion 请求，验证 API 地址、密钥和模型的组合是否可用。
// API 密钥只存在于请求头中，不会被记录或返回。
// protocol 为 anthropic 时使用 Anthropic Messages 协议，其余按 OpenAI 兼容协议处理。
func (c *AIModelClient) TestConnection(ctx context.Context, baseURL, apiKey, model, protocol string) (settingsapp.AIConnectionTestResult, error) {
	// baseURL、model 分别规范化为无末尾斜杠的端点和无首尾空白的模型名称。
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return settingsapp.AIConnectionTestResult{}, fmt.Errorf("AI API 地址为空")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return settingsapp.AIConnectionTestResult{}, fmt.Errorf("未选择模型，请先填写模型名称或点击「读取模型」")
	}
	if c == nil || c.newHTTPClient == nil && c.newTestHTTPClient == nil {
		return settingsapp.AIConnectionTestResult{}, fmt.Errorf("AI 模型客户端未初始化")
	}
	// client 是使用管理员配置端点且具备 50 秒超时的受限 HTTP 客户端，适应推理模型的首 token 延迟。
	var client *http.Client
	// clientErr 保存按管理员端点构造受限 HTTP 客户端时发生的地址策略或配置错误。
	var clientErr error
	if c.newTestHTTPClient != nil {
		client, clientErr = c.newTestHTTPClient(baseURL)
	} else {
		// 退化到默认客户端，保证旧测试不需要额外装配。
		client, clientErr = c.newHTTPClient(baseURL)
	}
	if clientErr != nil {
		return settingsapp.AIConnectionTestResult{}, fmt.Errorf("AI API 地址无效: %w", clientErr)
	}
	if protocol == llmclient.ProtocolAnthropic {
		return c.testAnthropic(ctx, client, baseURL, apiKey, model)
	}
	// payload 是不含账号、商品和 API 密钥的固定诊断请求。
	payload := aiConnectionTestRequest{
		Model: model,
		Messages: []aiConnectionTestMessage{{
			Role: "user", Content: "你好",
		}},
		MaxTokens: AIConnectionTestMaxTokens,
	}
	// body、marshalErr 分别保存 JSON 编码后的请求正文及编码失败原因。
	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return settingsapp.AIConnectionTestResult{}, fmt.Errorf("构造测试请求失败: %w", marshalErr)
	}
	// request、requestErr 分别保存绑定调用方取消信号的 HTTP 请求及其创建错误。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if requestErr != nil {
		return settingsapp.AIConnectionTestResult{}, requestErr
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	// start 只用于计算本次远端诊断往返的毫秒耗时。
	start := time.Now()
	// response、requestErr 分别保存远端响应及连接、取消或超时错误。
	response, requestErr := client.Do(request)
	// latency 是从发出请求到获得响应或错误的实际耗时（毫秒）。
	latency := time.Since(start).Milliseconds()
	if requestErr != nil {
		if errors.Is(requestErr, context.DeadlineExceeded) {
			return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("连接超时，请稍后重试")
		}
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("连接失败: %w", requestErr)
	}
	defer response.Body.Close()

	if response.StatusCode == 401 || response.StatusCode == 403 {
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("认证失败：API Key 无效或权限不足 (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode == 404 {
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("模型不存在或地址错误 (HTTP 404)")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("额度或请求频率受限 (HTTP 429)")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("AI 服务请求失败 (HTTP %d)", response.StatusCode)
	}

	// raw、readErr 分别保存受大小限制的成功响应正文及读取错误。
	raw, readErr := ReadAIModelsBody(response.Body)
	if readErr != nil {
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, fmt.Errorf("读取 AI 服务响应失败: %w", readErr)
	}
	// reply、replyErr 分别保存可安全显示的回复摘要及响应协议错误。
	reply, replyErr := extractChatReply(raw)
	if replyErr != nil {
		return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency}, replyErr
	}
	return settingsapp.AIConnectionTestResult{Model: model, LatencyMS: latency, Reply: truncateAIModelBody(reply, 100)}, nil
}

// extractChatReply 从 OpenAI 兼容的 chat completion 响应中提取模型回复文本。
func extractChatReply(raw []byte) (string, error) {
	// payload、decodeErr 分别保存解析后的兼容响应及 JSON 格式错误。
	var payload aiChatCompletionResponse
	// decodeErr 表示成功响应不是可解析的 OpenAI 兼容 JSON。
	if decodeErr := json.Unmarshal(raw, &payload); decodeErr != nil {
		return "", fmt.Errorf("AI 服务返回了无法解析的响应")
	}
	if len(payload.Choices) == 0 {
		return "", fmt.Errorf("请求成功但未返回有效回复内容")
	}
	// choice 是服务端返回的首个候选结果，测试不需要消耗其余候选内容。
	choice := payload.Choices[0]
	// content 是首个候选中可安全展示的用户可见正文。
	content := extractAIMessageText(choice.Message.Content)
	if content != "" {
		return content, nil
	}
	if strings.TrimSpace(choice.Text) != "" {
		return strings.TrimSpace(choice.Text), nil
	}
	if extractAIMessageText(choice.Message.ReasoningContent) != "" {
		return "模型已返回推理结果", nil
	}
	return "模型已成功响应（未返回可显示文本）", nil
}

// extractAIMessageText 从字符串或内容块数组中提取可显示文本，未知响应形状按空文本处理。
func extractAIMessageText(raw json.RawMessage) string {
	// text 保存字符串形状 content 的去空白结果。
	var text string
	// unmarshalErr 表示 content 不是纯字符串，需要继续按内容块数组解析。
	if unmarshalErr := json.Unmarshal(raw, &text); unmarshalErr == nil {
		return strings.TrimSpace(text)
	}
	// blocks 保存内容块数组形状，兼容多模态服务中的 text 字段。
	var blocks []struct {
		// Text 是当前内容块可展示的文本字段。
		Text string `json:"text"`
	}
	// unmarshalErr 表示 content 既不是字符串也不是兼容的内容块数组。
	if unmarshalErr := json.Unmarshal(raw, &blocks); unmarshalErr != nil {
		return ""
	}
	// texts 保存所有非空文本块，按原顺序拼接避免丢失分段内容。
	texts := make([]string, 0, len(blocks))
	// block 是当前正在提取文本的内容块。
	for _, block := range blocks {
		// text 是当前内容块去除首尾空白后的可展示文本。
		if text := strings.TrimSpace(block.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// truncateAIModelBody 截取文本，避免把超大远端正文写入日志或 HTTP 错误。
// 按 rune 截断，防止切出半个多字节字符。
func truncateAIModelBody(value string, limit int) string {
	value = strings.TrimSpace(value)
	// runes 是按 Unicode 字符边界截断的文本序列，避免破坏中文等多字节字符。
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// fetchAnthropic 通过 Anthropic Models API 读取模型目录，只返回模型标识。
func (c *AIModelClient) fetchAnthropic(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	// client 是经过地址校验的出站客户端。
	client, err := c.newHTTPClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("AI API 地址无效: %w", err)
	}
	// models、err 保存模型标识及读取错误；错误只含状态码，不含远端正文。
	models, err := llmclient.ListAnthropicModels(ctx, llmclient.Endpoint{BaseURL: baseURL, APIKey: strings.TrimSpace(apiKey), HTTPClient: client})
	if err != nil {
		// status 是上游返回的 HTTP 状态码，非 0 时按状态码分类错误文案。
		if status := llmclient.StatusCode(err); status != 0 {
			return nil, fmt.Errorf("读取模型失败: HTTP %d", status)
		}
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

// testAnthropic 通过 Anthropic Messages 协议发送一次最小对话请求，并复用 OpenAI 路径的错误分类文案。
func (c *AIModelClient) testAnthropic(ctx context.Context, client *http.Client, baseURL, apiKey, model string) (settingsapp.AIConnectionTestResult, error) {
	// start、latency 用于计算本次远端诊断往返的毫秒耗时。
	start := time.Now()
	// completion、err 保存模型结果及调用错误。
	completion, err := llmclient.Complete(ctx, llmclient.Endpoint{Protocol: llmclient.ProtocolAnthropic, BaseURL: baseURL, APIKey: strings.TrimSpace(apiKey), Model: model, HTTPClient: client},
		llmclient.Request{UserText: "你好", MaxTokens: AIConnectionTestMaxTokens})
	// result 是不含回复前的诊断结果骨架。
	result := settingsapp.AIConnectionTestResult{Model: model, LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		// status 是上游返回的 HTTP 状态码，0 表示请求未到达服务端。
		switch status := llmclient.StatusCode(err); {
		case status == 401 || status == 403:
			return result, fmt.Errorf("认证失败：API Key 无效或权限不足 (HTTP %d)", status)
		case status == http.StatusNotFound:
			return result, fmt.Errorf("模型不存在或地址错误 (HTTP 404)")
		case status == http.StatusTooManyRequests:
			return result, fmt.Errorf("额度或请求频率受限 (HTTP 429)")
		case status != 0:
			return result, fmt.Errorf("AI 服务请求失败 (HTTP %d)", status)
		case errors.Is(err, context.DeadlineExceeded):
			return result, fmt.Errorf("连接超时，请稍后重试")
		}
		return result, fmt.Errorf("连接失败: %w", err)
	}
	if completion.Refused {
		return result, fmt.Errorf("模型拒绝了测试请求，请换一个模型重试")
	}
	if completion.Text == "" {
		return result, fmt.Errorf("AI 服务返回了空回复，请检查模型名称")
	}
	result.Reply = truncateAIModelBody(completion.Text, 100)
	return result, nil
}

var _ interface {
	Fetch(context.Context, string, string, string) ([]string, error)
	TestConnection(context.Context, string, string, string, string) (settingsapp.AIConnectionTestResult, error)
} = (*AIModelClient)(nil)
