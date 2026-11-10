package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	agentreplyapp "xianyu-go/internal/application/agentreply"
)

// agentReplyTokenRequest 是访问令牌写入命令的请求 DTO；令牌只能通过它写入，响应中永不返回。
type agentReplyTokenRequest struct {
	// Action 是写入命令：retain、replace 或 clear；省略时按 retain 处理。
	Action string `json:"action"`
	// Value 是 action 为 replace 时的新令牌明文，不会被记录到日志或错误信息。
	Value string `json:"value"`
}

// agentReplySettingsRequest 是保存账号 Agent 回复配置的具名请求 DTO。
type agentReplySettingsRequest struct {
	// Enabled 表示是否启用 Agent 回复。
	Enabled bool `json:"enabled"`
	// EndpointURL 是外部 Agent 的 http(s) 地址。
	EndpointURL string `json:"endpoint_url"`
	// RequestTimeoutSeconds 是单次请求超时秒数，省略或 0 使用默认值。
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`
	// MaxSessionSeconds 是单轮对话总时长上限秒数，省略或 0 使用默认值。
	MaxSessionSeconds int `json:"max_session_seconds"`
	// Token 是访问令牌写入命令，省略表示保留已保存令牌。
	Token agentReplyTokenRequest `json:"token"`
}

// agentReplyTestRequest 是连接测试的具名请求 DTO，使用表单草稿而不要求先保存。
type agentReplyTestRequest struct {
	// EndpointURL 是待测试的 Agent 地址。
	EndpointURL string `json:"endpoint_url"`
	// RequestTimeoutSeconds 是单次请求超时秒数，省略或 0 使用默认值。
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`
	// Token 是访问令牌命令；retain 时使用账号已保存令牌。
	Token agentReplyTokenRequest `json:"token"`
}

// agentReplySettingsResponse 是账号 Agent 回复配置的具名响应 DTO，不包含访问令牌。
type agentReplySettingsResponse struct {
	// CookieID 是配置所属账号。
	CookieID string `json:"cookie_id"`
	// Enabled 表示是否启用 Agent 回复。
	Enabled bool `json:"enabled"`
	// EndpointURL 是外部 Agent 地址。
	EndpointURL string `json:"endpoint_url"`
	// TokenConfigured 表示已保存访问令牌；令牌本身永不返回。
	TokenConfigured bool `json:"token_configured"`
	// RequestTimeoutSeconds 是单次请求超时秒数。
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`
	// MaxSessionSeconds 是单轮对话总时长上限秒数。
	MaxSessionSeconds int `json:"max_session_seconds"`
}

// agentReplyTestResponse 是连接测试的具名响应 DTO，只含诊断概要，不含 Agent 响应正文。
type agentReplyTestResponse struct {
	// Status 取 ok、http_error、invalid_response 或 unreachable。
	Status string `json:"status"`
	// StatusCode 是 Agent 的 HTTP 状态码；未到达时为 0。
	StatusCode int `json:"status_code"`
	// DurationMS 是请求耗时毫秒数。
	DurationMS int64 `json:"duration_ms"`
	// Handled、Working 与 ReplyCount 是协议响应概要，仅 status 为 ok 时有意义。
	Handled    bool `json:"handled"`
	Working    bool `json:"working"`
	ReplyCount int  `json:"reply_count"`
}

// agentReplyApplication 返回 Agent 回复应用服务。
func (s *Server) agentReplyApplication() AgentReplyPort {
	return s.applicationServiceSet().agentReplies
}

// getAgentReply 读取账号的 Agent 回复配置；没有保存过时返回默认关闭值。
func (s *Server) getAgentReply(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// settings、err 保存应用服务返回的配置摘要及读取错误。
	settings, err := s.agentReplyApplication().Get(r.Context(), userID, chi.URLParam(r, "cookie_id"))
	if err != nil {
		writeAgentReplyError(w, r, err, "查询失败")
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, newAgentReplySettingsResponse(settings))
}

// setAgentReply 保存账号的 Agent 回复配置并返回保存后的摘要。
func (s *Server) setAgentReply(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// req 是更新请求体。
	var req agentReplySettingsRequest
	// decodeErr 保存请求体 JSON 解析错误；错误信息不回显请求内容。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "请求格式错误")
		return
	}
	// settings、err 保存保存后的配置摘要及写入错误。
	settings, err := s.agentReplyApplication().Update(r.Context(), userID, chi.URLParam(r, "cookie_id"), agentreplyapp.Draft{
		Enabled: req.Enabled, EndpointURL: req.EndpointURL, RequestTimeoutSeconds: req.RequestTimeoutSeconds,
		MaxSessionSeconds: req.MaxSessionSeconds, TokenAction: req.Token.Action, Token: req.Token.Value,
	})
	if err != nil {
		writeAgentReplyError(w, r, err, "保存失败")
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, newAgentReplySettingsResponse(settings))
}

// testAgentReply 使用表单草稿向 Agent 发送一次 ping 并返回诊断概要。
func (s *Server) testAgentReply(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// req 是测试请求体。
	var req agentReplyTestRequest
	// decodeErr 保存请求体 JSON 解析错误。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "请求格式错误")
		return
	}
	// result、err 保存诊断结果及调用错误；网络类失败已编码在 result.Status 中。
	result, err := s.agentReplyApplication().Test(r.Context(), userID, chi.URLParam(r, "cookie_id"), agentreplyapp.TestInput{
		EndpointURL: req.EndpointURL, RequestTimeoutSeconds: req.RequestTimeoutSeconds, TokenAction: req.Token.Action, Token: req.Token.Value,
	})
	if err != nil {
		writeAgentReplyError(w, r, err, "测试失败")
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, agentReplyTestResponse{
		Status: result.Status, StatusCode: result.StatusCode, DurationMS: result.DurationMillis,
		Handled: result.Handled, Working: result.Working, ReplyCount: result.ReplyCount,
	})
}

// newAgentReplySettingsResponse 把应用层摘要转换为响应 DTO。
func newAgentReplySettingsResponse(settings agentreplyapp.Settings) agentReplySettingsResponse {
	return agentReplySettingsResponse{
		CookieID: settings.CookieID, Enabled: settings.Enabled, EndpointURL: settings.EndpointURL, TokenConfigured: settings.TokenConfigured,
		RequestTimeoutSeconds: settings.RequestTimeoutSeconds, MaxSessionSeconds: settings.MaxSessionSeconds,
	}
}

// writeAgentReplyError 将应用错误映射为统一错误信封；错误信息不包含令牌或地址。
func writeAgentReplyError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	// invalid 保存可展示的参数错误。
	var invalid *agentreplyapp.InvalidInputError
	switch {
	case errors.As(err, &invalid):
		writeErrRequest(w, r, http.StatusBadRequest, invalid.Reason)
	case errors.Is(err, agentreplyapp.ErrInvalidCookieID):
		writeErrRequest(w, r, http.StatusBadRequest, "账号标识无效")
	case errors.Is(err, agentreplyapp.ErrAccountNotFound):
		writeErrRequest(w, r, http.StatusNotFound, "账号不存在")
	case errors.Is(err, agentreplyapp.ErrForbidden):
		writeErrRequest(w, r, http.StatusForbidden, "无权限操作该账号")
	case errors.Is(err, agentreplyapp.ErrUnavailable):
		writeErrRequest(w, r, http.StatusServiceUnavailable, "Agent 连接测试暂不可用")
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		writeErrRequest(w, r, http.StatusRequestTimeout, "请求已取消")
	default:
		writeErrRequest(w, r, http.StatusInternalServerError, fallback)
	}
}
