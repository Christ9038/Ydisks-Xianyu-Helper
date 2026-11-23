// Package llmclient 是对话类大模型的共用出站客户端，支持 OpenAI 兼容与 Anthropic 两种协议，并支持买家图片输入。
//
// 本包只负责构造请求、解析响应和“端点不支持图片时降级为纯文字”的一次重试，
// 不决定何时回复、不读写任何业务数据，也不记录日志；调用方（AI 回复、Agent 回复、测试连接）自行处理结果。
// 它访问的是用户自己配置的模型端点，不属于 MTOP 请求，不受 MTOP 节流器约束。
package llmclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/netguard"
)

// 以下常量是 Endpoint.Protocol 允许的取值。
const (
	// ProtocolOpenAI 表示 OpenAI 兼容的 chat/completions 协议，也是历史配置的默认协议。
	ProtocolOpenAI = "openai"
	// ProtocolAnthropic 表示 Anthropic Messages 协议。
	ProtocolAnthropic = "anthropic"
)

// 以下常量是调用参数的默认值与上限。
const (
	// DefaultAnthropicModel 是 Anthropic 协议未指定模型时使用的模型标识。
	DefaultAnthropicModel = "claude-opus-5-5"
	// anthropicDefaultMaxTokens 是 Anthropic 协议未指定输出上限时的 max_tokens；该协议要求必须提供。
	anthropicDefaultMaxTokens = 16000
	// openAITemperature 是 OpenAI 协议下沿用的采样温度；Anthropic 新模型不接受采样参数，因此该协议不发送。
	openAITemperature = 0.7
	// defaultTimeout 是调用方没有提供 HTTP 客户端时构造受限客户端使用的整体超时。
	defaultTimeout = 30 * time.Second
	// ImagePrompt 是买家发送图片且图片被一并发给模型时使用的文字部分，避免把占位符“[图片]”当作正文。
	ImagePrompt = "买家发送了一张图片，请结合图片内容回复。"
)

// 以下错误供调用方区分配置问题与上游问题。
var (
	// ErrUnknownProtocol 表示 Endpoint.Protocol 不是受支持的取值。
	ErrUnknownProtocol = errors.New("不支持的模型协议")
	// ErrEmptyBaseURL 表示没有配置模型服务地址。
	ErrEmptyBaseURL = errors.New("AI API 地址为空")
)

// Endpoint 描述一个模型端点；APIKey 只用于请求头，不得写入日志或错误信息。
type Endpoint struct {
	// Protocol 是 ProtocolOpenAI 或 ProtocolAnthropic；空值按 OpenAI 处理，兼容历史配置。
	Protocol string
	// BaseURL 是服务地址：OpenAI 协议包含 /v1 等路径前缀，Anthropic 协议为站点根地址（末尾的 /v1 会被忽略）。
	BaseURL string
	// APIKey 是访问密钥。
	APIKey string
	// Model 是模型标识；Anthropic 协议为空时使用 DefaultAnthropicModel。
	Model string
	// HTTPClient 是已通过出站策略校验的客户端；为空时按 BaseURL 构造 netguard 受限客户端，测试可注入本地客户端。
	HTTPClient *http.Client
}

// Turn 是一条历史对话；Role 取 "user" 或 "assistant"，其他值按 user 处理。
type Turn struct {
	// Role 是说话方。
	Role string
	// Text 是纯文字内容；历史对话不携带图片。
	Text string
}

// Request 是一次对话请求。
type Request struct {
	// System 是系统提示词，可为空。
	System string
	// History 是按时间顺序排列的历史对话，不含本轮买家输入。
	History []Turn
	// UserText 是本轮买家输入的文字；ImageURLs 非空时调用方应把 ImagePrompt 并入文字。
	UserText string
	// ImageURLs 是本轮买家图片的公网地址（按时间顺序，调用方负责数量上限），为空表示纯文字。它们只随本次请求发送，不会被本包保存。
	ImageURLs []string
	// MaxTokens 是输出上限；0 表示 OpenAI 协议不限制、Anthropic 协议使用默认值。
	MaxTokens int
	// User 是不含个人信息的不透明会话标识，用于上游关联同一会话；为空则不发送。
	User string
}

// Result 是一次对话的结果。
type Result struct {
	// Text 是模型回复的文字，已去除首尾空白；可能为空。
	Text string
	// Refused 表示模型因安全策略拒绝回答（Anthropic stop_reason=refusal），调用方应当作未回复处理。
	Refused bool
	// ImageDropped 表示端点拒绝了图片输入，本次回复是去掉图片后的纯文字重试结果。
	ImageDropped bool
	// Status 在 ImageDropped 为 true 时是端点对带图片请求返回的 HTTP 状态码，供调用方记录日志。
	Status int
}

// NormalizeProtocol 把空值归一为 OpenAI；不是受支持取值时返回 ErrUnknownProtocol。
func NormalizeProtocol(protocol string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", ProtocolOpenAI:
		return ProtocolOpenAI, nil
	case ProtocolAnthropic:
		return ProtocolAnthropic, nil
	default:
		return "", ErrUnknownProtocol
	}
}

// Complete 发送一次对话请求。带图片的请求被端点以 400/415/422 拒绝时，去掉图片重试一次；
// 网络错误、超时、401、429 与 5xx 不重试，原样返回错误，避免放大对用户端点的请求。
func Complete(ctx context.Context, endpoint Endpoint, request Request) (Result, error) {
	// protocol 是归一化后的协议。
	protocol, err := NormalizeProtocol(endpoint.Protocol)
	if err != nil {
		return Result{}, err
	}
	// baseURL 是去掉首尾空白与末尾斜杠的服务地址。
	baseURL := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	if baseURL == "" {
		return Result{}, ErrEmptyBaseURL
	}
	endpoint.Protocol, endpoint.BaseURL = protocol, baseURL
	if endpoint.HTTPClient == nil {
		// client 是按用户配置地址构造的受限客户端；地址不合法时不发出请求。
		client, clientErr := netguard.ConfiguredEndpointHTTPClient(baseURL, defaultTimeout)
		if clientErr != nil {
			return Result{}, fmt.Errorf("AI API 地址无效: %w", clientErr)
		}
		endpoint.HTTPClient = client
	}
	// result、callErr 保存第一次调用的结果与错误。
	result, callErr := dispatch(ctx, endpoint, request)
	if callErr == nil || len(request.ImageURLs) == 0 {
		return result, callErr
	}
	// status 是上游状态码；只有“请求格式或内容类型被拒绝”才说明端点不支持图片。
	status := upstreamStatus(callErr)
	if status != http.StatusBadRequest && status != http.StatusUnsupportedMediaType && status != http.StatusUnprocessableEntity {
		return result, callErr
	}
	request.ImageURLs = nil
	// retried、retryErr 保存去掉图片后的重试结果与错误。
	retried, retryErr := dispatch(ctx, endpoint, request)
	if retryErr != nil {
		return retried, retryErr
	}
	retried.ImageDropped, retried.Status = true, status
	return retried, nil
}

// dispatch 按协议分发一次请求。
func dispatch(ctx context.Context, endpoint Endpoint, request Request) (Result, error) {
	if endpoint.Protocol == ProtocolAnthropic {
		return completeAnthropic(ctx, endpoint, request)
	}
	return completeOpenAI(ctx, endpoint, request)
}

// StatusCode 返回 Complete 错误携带的上游 HTTP 状态码；不是上游 HTTP 错误（网络、超时、地址无效）时返回 0。
func StatusCode(err error) int { return upstreamStatus(err) }

// upstreamStatus 提取两种 SDK 错误携带的 HTTP 状态码；不是上游 HTTP 错误时返回 0。
func upstreamStatus(err error) int {
	// openAIErr、requestErr、anthropicErr 分别是三种可携带状态码的错误形状。
	var openAIErr *openai.APIError
	// requestErr 承接 OpenAI SDK 的网络层错误（未收到 HTTP 响应）。
	var requestErr *openai.RequestError
	// anthropicErr 承接 Anthropic SDK 的 HTTP 错误，携带上游状态码。
	var anthropicErr *anthropic.Error
	switch {
	case errors.As(err, &openAIErr):
		return openAIErr.HTTPStatusCode
	case errors.As(err, &requestErr):
		return requestErr.HTTPStatusCode
	case errors.As(err, &anthropicErr):
		return anthropicErr.StatusCode
	}
	return 0
}

// completeOpenAI 通过 OpenAI 兼容协议发送请求；有图片时最后一条 user 消息使用多模态内容。
func completeOpenAI(ctx context.Context, endpoint Endpoint, request Request) (Result, error) {
	// cfg 是 OpenAI 兼容客户端配置，地址与 HTTP 客户端均来自用户配置。
	cfg := openai.DefaultConfig(endpoint.APIKey)
	cfg.BaseURL, cfg.HTTPClient = endpoint.BaseURL, endpoint.HTTPClient
	// messages 依次是系统提示词、历史对话与本轮买家输入。
	var messages []openai.ChatCompletionMessage
	if strings.TrimSpace(request.System) != "" {
		messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: request.System})
	}
	// turn 是当前遍历的历史对话。
	for _, turn := range request.History {
		// role 把非 assistant 的角色统一视为 user。
		role := openai.ChatMessageRoleUser
		if turn.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: turn.Text})
	}
	// current 是本轮买家输入；图片作为独立 image_url 片段，文字部分不含占位符。
	current := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: request.UserText}
	if len(request.ImageURLs) > 0 {
		// parts 先放文字，再按时间顺序放每张图片。
		parts := []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeText, Text: request.UserText}}
		// imageURL 表示当前遍历的买家图片地址。
		for _, imageURL := range request.ImageURLs {
			parts = append(parts, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeImageURL, ImageURL: &openai.ChatMessageImageURL{URL: imageURL, Detail: openai.ImageURLDetailAuto}})
		}
		current = openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, MultiContent: parts}
	}
	messages = append(messages, current)
	// response、err 保存模型响应及调用错误。
	response, err := openai.NewClientWithConfig(cfg).CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: endpoint.Model, Messages: messages, Temperature: openAITemperature,
		MaxTokens: request.MaxTokens, User: request.User,
	})
	if err != nil {
		return Result{}, fmt.Errorf("AI 调用失败: %w", err)
	}
	if len(response.Choices) == 0 {
		return Result{}, nil
	}
	return Result{Text: strings.TrimSpace(response.Choices[0].Message.Content)}, nil
}

// anthropicBaseURL 去掉末尾的 /v1：SDK 会自己拼接 /v1/messages，用户常把 OpenAI 风格的地址原样填进来。
func anthropicBaseURL(baseURL string) string {
	return strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
}

// newAnthropicClient 构造 Anthropic 客户端：关闭 SDK 自带重试，保证对用户端点的请求次数完全由本包控制。
func newAnthropicClient(endpoint Endpoint) anthropic.Client {
	return anthropic.NewClient(
		option.WithAPIKey(endpoint.APIKey),
		// 同时发送 x-api-key：SDK 默认只发 Bearer，而多数 Anthropic 兼容网关只认 x-api-key。
		option.WithHeader("x-api-key", endpoint.APIKey),
		option.WithBaseURL(anthropicBaseURL(endpoint.BaseURL)),
		option.WithHTTPClient(endpoint.HTTPClient),
		option.WithMaxRetries(0),
	)
}

// maxListedModels 是读取 Anthropic 模型目录时最多返回的模型数，防止异常端点无限分页。
const maxListedModels = 500

// ListAnthropicModels 读取 Anthropic 协议端点的模型目录（只返回模型标识，不返回密钥或原始响应）。
// endpoint.HTTPClient 为空时按 BaseURL 构造受限客户端；最多读取 maxListedModels 个模型。
func ListAnthropicModels(ctx context.Context, endpoint Endpoint) ([]string, error) {
	// baseURL 是去掉首尾空白与末尾斜杠的服务地址。
	baseURL := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	if baseURL == "" {
		return nil, ErrEmptyBaseURL
	}
	endpoint.BaseURL = baseURL
	if endpoint.HTTPClient == nil {
		// client、err 是按用户配置地址构造的受限客户端。
		client, err := netguard.ConfiguredEndpointHTTPClient(baseURL, defaultTimeout)
		if err != nil {
			return nil, fmt.Errorf("AI API 地址无效: %w", err)
		}
		endpoint.HTTPClient = client
	}
	// client、pager 是 SDK 客户端及自动分页迭代器。
	client := newAnthropicClient(endpoint)
	// pager 是 Anthropic 模型列表的自动分页迭代器，每页一次请求。
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	// models 保存模型标识。
	var models []string
	for pager.Next() && len(models) < maxListedModels {
		models = append(models, pager.Current().ID)
	}
	// err 是分页迭代过程中的错误，出现后整体失败。
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	return models, nil
}

// completeAnthropic 通过 Anthropic Messages 协议发送请求；有图片时使用 URL 图片源块。
func completeAnthropic(ctx context.Context, endpoint Endpoint, request Request) (Result, error) {
	// client 是绑定端点、密钥与受控 HTTP 客户端的 Anthropic SDK 客户端。
	client := newAnthropicClient(endpoint)
	// model 在未配置时使用默认模型。
	model := strings.TrimSpace(endpoint.Model)
	if model == "" {
		model = DefaultAnthropicModel
	}
	// maxTokens 是输出上限；该协议要求必须提供。
	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	// params 是请求参数；不发送 temperature 等采样参数，新模型会拒绝它们。
	params := anthropic.MessageNewParams{
		Model: anthropic.Model(model), MaxTokens: int64(maxTokens), Messages: anthropicMessages(request),
	}
	if strings.TrimSpace(request.System) != "" {
		params.System = []anthropic.TextBlockParam{{Text: request.System}}
	}
	if request.User != "" {
		params.Metadata = anthropic.MetadataParam{UserID: anthropic.String(request.User)}
	}
	// response、err 保存模型响应及调用错误。
	response, err := client.Messages.New(ctx, params)
	if err != nil {
		return Result{}, fmt.Errorf("AI 调用失败: %w", err)
	}
	if response.StopReason == anthropic.StopReasonRefusal {
		return Result{Refused: true}, nil
	}
	// text 只拼接文字块，忽略 thinking 等其他块。
	var text strings.Builder
	// block 是当前遍历的响应内容块。
	for _, block := range response.Content {
		// textBlock 是响应中的文本块，ok 表示该内容块确为文本类型（忽略思考等其他块）。
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(textBlock.Text)
		}
	}
	return Result{Text: strings.TrimSpace(text.String())}, nil
}

// anthropicMessages 把历史与本轮输入转换为 Anthropic 消息：第一条必须是 user，空文字块会被拒绝，因此丢弃开头的 assistant 与空内容。
func anthropicMessages(request Request) []anthropic.MessageParam {
	// messages 是转换后的消息列表。
	var messages []anthropic.MessageParam
	// turn 是当前遍历的历史对话。
	for _, turn := range request.History {
		if strings.TrimSpace(turn.Text) == "" {
			continue
		}
		if turn.Role == "assistant" {
			if len(messages) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(turn.Text)))
			}
			continue
		}
		messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(turn.Text)))
	}
	// blocks 是本轮买家输入的内容块：先图片后文字。
	var blocks []anthropic.ContentBlockParamUnion
	// imageURL 表示当前遍历的买家图片地址。
	for _, imageURL := range request.ImageURLs {
		blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: imageURL}))
	}
	blocks = append(blocks, anthropic.NewTextBlock(request.UserText))
	return append(messages, anthropic.NewUserMessage(blocks...))
}
