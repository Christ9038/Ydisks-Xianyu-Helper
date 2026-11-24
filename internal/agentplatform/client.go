// Package agentplatform 实现对 Dify 与 FastGPT 这两类工作流 Agent 平台的单次对话调用。
//
// 它只负责协议：拼接地址、构造请求、解析响应并归类错误；不读写数据库、不决定回复规则，也不构造 HTTP 客户端
// （调用方传入已按出站策略限制的 *http.Client）。会话记忆由平台自己维护：Dify 通过 conversation_id，
// FastGPT 通过 chatId；本包只透传调用方给定的会话标识。访问令牌只进入 Authorization 头，不会出现在错误或日志里。
package agentplatform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 以下常量是协议名称，与 db.AgentProtocol* 取值一致；本包不依赖 db，避免基础设施层反向依赖。
const (
	// ProtocolDify 表示 Dify 云端或自建应用的聊天消息接口（/v1/chat-messages）。
	ProtocolDify = "dify"
	// ProtocolFastGPT 表示 FastGPT 的对话接口（/api/v1/chat/completions）。
	ProtocolFastGPT = "fastgpt"
)

const (
	// maxResponseBytes 是单次响应（含流式正文）允许读取的最大字节数，防止异常服务拖垮内存。
	maxResponseBytes = 1 << 20
	// maxErrorBodyBytes 是读取非 2xx 响应体用于提取错误码的上限。
	maxErrorBodyBytes = 64 << 10
	// fastGPTBusyRetries 是 FastGPT 返回“该会话正在生成”（409）时最多重试的次数。
	fastGPTBusyRetries = 2
	// fastGPTBusyWait 是 FastGPT 会话仍在生成时每次重试前的等待时长，随重试次数线性增加。
	fastGPTBusyWait = 2 * time.Second
)

// ErrSessionGone 表示调用方给出的平台会话标识已失效（Dify 会话不存在或已结束），调用方应丢弃它并用空标识重试一次。
var ErrSessionGone = errors.New("平台会话已失效")

// ErrEmptyReply 表示平台调用成功但没有任何可发送的文字。
var ErrEmptyReply = errors.New("平台返回了空回复")

// StatusError 表示平台返回了非 2xx 状态；Reason 是平台给出的机器可读错误码（可为空），不含响应正文和令牌。
type StatusError struct {
	// Code 是 HTTP 状态码；流式连接中途出现的平台错误事件用其 status 字段，缺失时为 502。
	Code int
	// Reason 是平台错误码，例如 Dify 的 not_found、FastGPT 的 chatIsGenerating。
	Reason string
}

// Error 返回不含敏感信息的错误说明。
func (e *StatusError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("平台返回 HTTP %d", e.Code)
	}
	return fmt.Sprintf("平台返回 HTTP %d（%s）", e.Code, e.Reason)
}

// StatusCode 返回错误链中的 HTTP 状态码；不是平台状态错误时返回 0，表示请求没有到达服务端。
func StatusCode(err error) int {
	// statusErr 保存错误链中的平台状态错误。
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code
	}
	return 0
}

// Endpoint 描述一次调用的目标平台。
type Endpoint struct {
	// BaseURL 是用户配置的平台地址：可以是域名根、带 /v1（Dify）或 /api（FastGPT）的基础路径，也可以是完整接口地址。
	BaseURL string
	// Token 是应用 API Key，敏感值，只进入 Authorization 头。
	Token string
	// HTTPClient 是受出站策略限制的客户端，其 Timeout 约束整次调用（含流式读取）。
	HTTPClient *http.Client
}

// Request 是一次对话请求。
type Request struct {
	// Text 是买家本轮输入（合并批次时是合并后的文字）。
	Text string
	// ImageURLs 是随请求发送的买家图片地址；平台拒绝图片时自动降级为纯文字重试一次。
	ImageURLs []string
	// User 是稳定且不暴露平台会话号的终端用户标识，Dify 要求同一会话始终使用同一个值。
	User string
	// SessionID 是平台会话标识：Dify 的 conversation_id（空表示新会话）、FastGPT 的 chatId。
	SessionID string
}

// Result 是一次对话的结果。
type Result struct {
	// Text 是平台返回的回复文字，已去除首尾空白，不会为空。
	Text string
	// SessionID 是平台会话标识，调用方应保存以便下一轮继续同一会话；FastGPT 原样返回请求中的 chatId。
	SessionID string
	// ImageDropped 表示平台拒绝了图片，本次结果来自纯文字重试。
	ImageDropped bool
}

// sleep 等待 d 或 ctx 结束；是包级变量以便测试替换，避免真实等待。
var sleep = func(ctx context.Context, d time.Duration) error {
	// timer 是等待计时器，函数返回前停止以免泄漏。
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Call 按协议名调用对应平台；协议未知时返回错误。
func Call(ctx context.Context, protocol string, endpoint Endpoint, request Request) (Result, error) {
	switch protocol {
	case ProtocolDify:
		return Dify(ctx, endpoint, request)
	case ProtocolFastGPT:
		return FastGPT(ctx, endpoint, request)
	default:
		return Result{}, fmt.Errorf("不支持的平台协议: %q", protocol)
	}
}

// pathRule 描述一条地址补全规则：路径以 ends 结尾时追加 add 得到接口路径。
type pathRule struct {
	// ends 是用户填写路径的结尾；add 是需要追加的路径段。
	ends, add string
}

// resolveURL 把用户配置的地址补全为接口地址：路径已以 full 结尾的完整接口地址原样使用；
// 否则按 rules 顺序匹配第一条以 ends 结尾的规则并追加 add；都不匹配（域名根或任意路径）时追加 fallback。
// 查询参数与片段被丢弃，只接受 http/https。
func resolveURL(raw, full, fallback string, rules ...pathRule) (string, error) {
	// parsed、err 保存地址解析结果及格式错误。
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("平台地址无效")
	}
	// path 是去掉尾部斜杠的路径；add 是需要追加的路径段。
	path, add := strings.TrimRight(parsed.Path, "/"), fallback
	if strings.HasSuffix(path, full) {
		add = ""
	} else {
		// rule 是当前尝试的地址补全规则。
		for _, rule := range rules {
			if strings.HasSuffix(path, rule.ends) {
				add = rule.add
				break
			}
		}
	}
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = path+add, "", "", ""
	return parsed.String(), nil
}

// readLimited 读取至多 limit 字节；超出时多读一字节用来判断并返回截断错误。
func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	// data、err 保存读取结果及读取错误。
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("平台响应过大")
	}
	return data, nil
}
