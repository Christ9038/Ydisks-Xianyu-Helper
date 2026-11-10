// Package agentclient 实现助手与用户自建外部 Agent 之间的 HTTP 连接协议（v1）。
//
// 本包只负责协议报文、出站请求和响应校验，不决定何时回复、也不写入任何业务数据；
// 调用方（引擎、测试连接适配器）负责轮询节奏、上限与消息投递。
package agentclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xianyu-go/internal/netguard"
)

// ProtocolVersion 是请求体 version 字段的当前协议版本。
const ProtocolVersion = 1

// MaxResponseBytes 是单个 Agent 响应允许读取的最大字节数，避免远端响应耗尽服务端内存。
const MaxResponseBytes = 256 << 10

// 以下常量是请求体 event 字段允许的事件类型。
const (
	// EventMessage 表示首次把买家消息交给 Agent。
	EventMessage = "message"
	// EventPoll 表示 Agent 上一次返回 working=true 后的继续查询，不携带新消息。
	EventPoll = "poll"
	// EventPing 表示测试连接，不携带任何买家数据。
	EventPing = "ping"
)

// 以下常量是消息 type 字段允许的取值。
const (
	// MessageTypeText 表示买家发送的是文字。
	MessageTypeText = "text"
	// MessageTypeImage 表示买家发送的是图片，image_url 携带公网图片地址。
	MessageTypeImage = "image"
)

// Buyer 描述当前会话的买家，只包含平台公开的用户标识与昵称。
type Buyer struct {
	// UserID 是买家平台用户标识。
	UserID string `json:"user_id"`
	// Name 是买家昵称，可能为空。
	Name string `json:"name"`
}

// Message 描述本次转发给 Agent 的买家消息。
type Message struct {
	// ID 是平台消息标识，可能为空。
	ID string `json:"id"`
	// Type 是消息类型，取 MessageTypeText 或 MessageTypeImage。
	Type string `json:"type"`
	// Text 是文字消息正文；图片消息为平台占位文字。
	Text string `json:"text"`
	// ImageURL 是图片消息的公网地址，文字消息为空。
	ImageURL string `json:"image_url,omitempty"`
}

// Request 是发给 Agent 的一次 POST JSON 请求体，不包含账号 Cookie、Token 等凭证。
type Request struct {
	// Version 是协议版本，固定为 ProtocolVersion。
	Version int `json:"version"`
	// Event 是事件类型，取 EventMessage、EventPoll 或 EventPing。
	Event string `json:"event"`
	// TurnID 是同一轮对话内保持不变的随机标识，供 Agent 关联轮询。
	TurnID string `json:"turn_id"`
	// Sequence 是本轮内从 1 开始递增的请求序号。
	Sequence int `json:"sequence"`
	// AccountID 是助手内部账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是平台会话标识，Agent 可据此维护自己的会话记忆。
	ChatID string `json:"chat_id"`
	// ItemID 是会话关联的商品标识，可能为空。
	ItemID string `json:"item_id"`
	// Buyer 是买家信息；ping 事件为空。
	Buyer *Buyer `json:"buyer,omitempty"`
	// Message 是买家消息；仅 message 事件携带。
	Message *Message `json:"message,omitempty"`
}

// Reply 是 Agent 要求发给买家的一条回复，文字与图片至少有一项非空。
type Reply struct {
	// Text 是文字回复。
	Text string `json:"text"`
	// ImageURL 是图片回复的公网地址，由聊天应用下载并上传。
	ImageURL string `json:"image_url"`
}

// Response 是 Agent 返回的规范化响应。
type Response struct {
	// Handled 表示 Agent 是否接手本条消息；协议缺省为 true。
	Handled bool
	// Working 表示 Agent 仍在处理，调用方应在 PollAfter 后发送 poll 事件。
	Working bool
	// Replies 是本次响应携带的待发送回复，已去除完全为空的项。
	Replies []Reply
	// PollAfter 是 Agent 建议的下次查询间隔；零值表示由调用方使用默认间隔。
	PollAfter time.Duration
}

// wireResponse 是响应 JSON 的原始形态，Handled 用指针区分“缺省”和“显式 false”。
type wireResponse struct {
	Handled     *bool   `json:"handled"`
	Working     bool    `json:"working"`
	Replies     []Reply `json:"replies"`
	PollAfterMS int     `json:"poll_after_ms"`
}

// Endpoint 描述一次出站调用的目标，Token 是敏感值，不得写入日志或错误。
type Endpoint struct {
	// URL 是用户配置的 Agent 地址，只允许 http 或 https。
	URL string
	// Token 是可选的 Bearer 令牌，为空时不发送 Authorization 头。
	Token string
	// Timeout 是单次 HTTP 请求的总超时。
	Timeout time.Duration
}

// StatusError 表示 Agent 返回了非 2xx 状态码；它不携带响应正文，避免泄漏第三方内容。
type StatusError struct {
	// Code 是 HTTP 状态码。
	Code int
}

// Error 返回不含响应正文的状态码错误。
func (e *StatusError) Error() string { return fmt.Sprintf("Agent 返回 HTTP %d", e.Code) }

// ErrInvalidResponse 表示 Agent 响应不是协议要求的 JSON 结构或超过大小上限。
var ErrInvalidResponse = errors.New("外部 Agent 响应格式无效")

// ValidateURL 检查用户配置的 Agent 地址格式：必须是带主机的 http(s) 绝对地址，且不得内嵌账号密码。
func ValidateURL(rawURL string) error {
	// parsed、err 保存地址解析结果及格式错误。
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return errors.New("外部 Agent 地址无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("外部 Agent 地址只支持 http 或 https")
	}
	if parsed.User != nil {
		return errors.New("外部 Agent 地址不能包含账号密码，请使用访问令牌")
	}
	return nil
}

// Send 向 endpoint 发送一次协议请求并返回规范化响应；ctx 取消会立即中止请求。
// 它使用不复用连接的客户端，适合测试连接这类一次性调用；同一轮多次请求应使用 Session。
// 错误文本不包含令牌、地址路径或响应正文。
func Send(ctx context.Context, endpoint Endpoint, request Request) (Response, error) {
	// err 表示 Agent 地址的格式校验错误；地址不合法时不会发起任何请求。
	if err := ValidateURL(endpoint.URL); err != nil {
		return Response{}, err
	}
	// client、err 保存带出站策略、不复用连接的 HTTP 客户端。
	client, err := netguard.ConfiguredEndpointHTTPClient(endpoint.URL, endpoint.Timeout)
	if err != nil {
		return Response{}, err
	}
	return send(ctx, client, endpoint, request)
}

// Session 是一轮 Agent 对话内复用同一 HTTP 客户端和连接的发送器，避免每次轮询都重新建立 TCP/TLS 连接。
//
// 生命周期：由 NewSession 创建，调用方在本轮结束时必须调用 Close 释放空闲连接；Session 只供顺序调用，
// 不保证并发安全。新连接仍按出站公网策略校验；公网开关在两次请求之间变化时，下一次请求前会丢弃复用连接。
type Session struct {
	// endpoint 是本轮固定的出站目标，令牌随它传递但不会写入日志或错误。
	endpoint Endpoint
	// client 是本轮复用的策略客户端，持有至多 1 条空闲连接。
	client *http.Client
}

// NewSession 校验地址并创建一轮对话使用的会话；地址不合法时返回错误且不发起任何请求。
func NewSession(endpoint Endpoint) (*Session, error) {
	// err 表示 Agent 地址的格式校验错误。
	if err := ValidateURL(endpoint.URL); err != nil {
		return nil, err
	}
	// client、err 保存允许本轮复用连接的策略客户端。
	client, err := netguard.ConfiguredEndpointSessionHTTPClient(endpoint.URL, endpoint.Timeout)
	if err != nil {
		return nil, err
	}
	return &Session{endpoint: endpoint, client: client}, nil
}

// Send 通过本轮复用的连接发送一次协议请求，语义与包级 Send 相同。
func (s *Session) Send(ctx context.Context, request Request) (Response, error) {
	return send(ctx, s.client, s.endpoint, request)
}

// Close 释放本轮持有的空闲连接；重复调用安全。
func (s *Session) Close() {
	if s != nil && s.client != nil {
		s.client.CloseIdleConnections()
	}
}

// nonSuccessDrainBytes 是非 2xx 响应在关闭前最多丢弃读取的字节数，读完小响应体后连接才能被复用。
const nonSuccessDrainBytes = 4 << 10

// send 用给定客户端发送一次协议请求并规范化响应；client 由调用方负责创建和释放。
func send(ctx context.Context, client *http.Client, endpoint Endpoint, request Request) (Response, error) {
	request.Version = ProtocolVersion
	// body、err 保存序列化后的请求体及序列化错误。
	body, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("外部 Agent 请求序列化失败: %w", err)
	}
	// httpRequest 保存带取消上下文的 POST 请求。
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(endpoint.URL), bytes.NewReader(body))
	if err != nil {
		return Response{}, errors.New("外部 Agent 地址无效")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	// token 是去除空白后的访问令牌；为空时不发送 Authorization 头。
	if token := strings.TrimSpace(endpoint.Token); token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	// httpResponse、err 保存远端响应；传输层错误不透出底层文本，避免 URL 或令牌进入日志。
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		// ctxErr 是调用方上下文的取消或超时原因，优先于通用失败返回，便于上层区分取消与网络故障。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
		return Response{}, errors.New("外部 Agent 请求失败")
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode > 299 {
		// 丢弃少量响应正文以便复用连接；正文内容不读取、不返回，避免泄漏第三方内容。
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResponse.Body, nonSuccessDrainBytes))
		return Response{}, &StatusError{Code: httpResponse.StatusCode}
	}
	return decodeResponse(httpResponse.Body)
}

// decodeResponse 读取受限大小的响应体并规范化；超限或 JSON 无效均返回 ErrInvalidResponse。
func decodeResponse(reader io.Reader) (Response, error) {
	// raw、err 保存读取到的响应字节，多读一个字节用来识别超限。
	raw, err := io.ReadAll(io.LimitReader(reader, MaxResponseBytes+1))
	if err != nil || len(raw) > MaxResponseBytes {
		return Response{}, ErrInvalidResponse
	}
	// wire 保存原始响应字段。
	var wire wireResponse
	// err 表示响应 JSON 解析错误；统一映射为 ErrInvalidResponse，不暴露响应正文。
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Response{}, ErrInvalidResponse
	}
	// result 保存规范化响应；handled 缺省视为接手。
	result := Response{Handled: wire.Handled == nil || *wire.Handled, Working: wire.Working}
	if wire.PollAfterMS > 0 {
		result.PollAfter = time.Duration(wire.PollAfterMS) * time.Millisecond
	}
	// reply 表示当前遍历的回复项；全空的项没有可发送内容，直接丢弃。
	for _, reply := range wire.Replies {
		reply.Text = strings.TrimSpace(reply.Text)
		reply.ImageURL = strings.TrimSpace(reply.ImageURL)
		if reply.Text == "" && reply.ImageURL == "" {
			continue
		}
		result.Replies = append(result.Replies, reply)
	}
	return result, nil
}

// NewTurnID 生成一轮对话的随机标识；随机源失败时退回时间戳，不影响协议正确性。
func NewTurnID() string {
	// raw 保存 8 字节随机数。
	raw := make([]byte, 8)
	// err 表示系统随机源读取错误；失败时退回时间戳标识，不影响协议正确性。
	if _, err := rand.Read(raw); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(raw)
}
