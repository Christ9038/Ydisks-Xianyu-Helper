// Package agentreply 定义账号外部 Agent 回复配置与连接测试用例及其消费者侧 Port。
// 本包不依赖 HTTP、数据库模型或具体基础设施实现；真实出站调用由适配器实现 Tester。
package agentreply

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// 以下常量是 Agent 配置允许的取值范围，范围同时受数据库 CHECK 约束保护。
const (
	// DefaultRequestTimeoutSeconds 是单次 Agent 请求的默认超时秒数。
	DefaultRequestTimeoutSeconds = 15
	// MinRequestTimeoutSeconds 与 MaxRequestTimeoutSeconds 是单次请求超时的允许区间。
	MinRequestTimeoutSeconds = 5
	MaxRequestTimeoutSeconds = 30
	// DefaultMaxSessionSeconds 是单轮对话（含轮询）的默认总时长秒数。
	DefaultMaxSessionSeconds = 120
	// MinMaxSessionSeconds 与 MaxMaxSessionSeconds 是单轮总时长的允许区间。
	MinMaxSessionSeconds = 10
	MaxMaxSessionSeconds = 300
	// MaxEndpointURLRunes 是 Agent 地址的最大字符数。
	MaxEndpointURLRunes = 2048
	// MaxTokenRunes 是访问令牌的最大字符数。
	MaxTokenRunes = 512
)

// 以下常量是访问令牌写入命令，与系统敏感设置的三态保持一致。
const (
	// TokenRetain 表示保留已保存令牌；空命令按此处理。
	TokenRetain = "retain"
	// TokenReplace 表示用请求中的令牌替换已保存令牌。
	TokenReplace = "replace"
	// TokenClear 表示清空已保存令牌。
	TokenClear = "clear"
)

// 以下常量是连接测试结果的状态。
const (
	// TestStatusOK 表示 Agent 返回 2xx 且响应符合协议。
	TestStatusOK = "ok"
	// TestStatusHTTPError 表示 Agent 返回非 2xx。
	TestStatusHTTPError = "http_error"
	// TestStatusInvalidResponse 表示 Agent 返回 2xx 但响应不符合协议。
	TestStatusInvalidResponse = "invalid_response"
	// TestStatusUnreachable 表示请求未能到达 Agent（网络、超时或出站策略拒绝）。
	TestStatusUnreachable = "unreachable"
)

// ErrInvalidUser 表示调用方没有提供有效的本地用户身份。
var ErrInvalidUser = errors.New("外部 Agent 回复用户身份无效")

// ErrInvalidCookieID 表示调用方没有提供有效的账号标识。
var ErrInvalidCookieID = errors.New("外部 Agent 回复账号标识无效")

// ErrAccountNotFound 表示目标账号不存在。
var ErrAccountNotFound = errors.New("外部 Agent 回复账号不存在")

// ErrForbidden 表示目标账号存在但不属于当前用户。
var ErrForbidden = errors.New("无权操作该账号的 Agent 回复")

// ErrConfigNotFound 表示账号还没有保存过 Agent 配置，调用方按默认关闭值处理。
var ErrConfigNotFound = errors.New("外部 Agent 回复配置不存在")

// ErrInvalidInput 表示配置不满足业务约束；具体原因通过 InvalidInputError 的 Reason 提供。
var ErrInvalidInput = errors.New("外部 Agent 回复配置参数无效")

// ErrUnavailable 表示连接测试能力未装配。
var ErrUnavailable = errors.New("外部 Agent 连接测试不可用")

// InvalidInputError 携带可直接展示给用户的参数错误原因，并匹配 ErrInvalidInput。
type InvalidInputError struct {
	// Reason 是不含敏感信息的中文原因。
	Reason string
}

// Error 返回参数错误原因。
func (e *InvalidInputError) Error() string { return e.Reason }

// Is 让 errors.Is(err, ErrInvalidInput) 对所有参数错误成立。
func (e *InvalidInputError) Is(target error) bool { return target == ErrInvalidInput }

// Settings 是账号 Agent 回复配置的应用层摘要，不包含访问令牌。
type Settings struct {
	// CookieID 是配置所属账号。
	CookieID string
	// Enabled 表示是否启用 Agent 回复。
	Enabled bool
	// EndpointURL 是外部 Agent 的 http(s) 地址。
	EndpointURL string
	// TokenConfigured 表示已保存访问令牌；令牌明文永远不经过应用服务返回。
	TokenConfigured bool
	// RequestTimeoutSeconds 是单次请求超时秒数。
	RequestTimeoutSeconds int
	// MaxSessionSeconds 是单轮对话总时长上限秒数。
	MaxSessionSeconds int
}

// Draft 是保存配置时调用方提交的内容。
type Draft struct {
	// Enabled 表示是否启用。
	Enabled bool
	// EndpointURL 是 Agent 地址；启用时必填。
	EndpointURL string
	// RequestTimeoutSeconds 与 MaxSessionSeconds 为 0 时使用默认值。
	RequestTimeoutSeconds int
	MaxSessionSeconds     int
	// TokenAction 是令牌写入命令，取 Token* 常量。
	TokenAction string
	// Token 是 TokenAction 为 replace 时提交的明文令牌，不得记录日志。
	Token string
}

// TestInput 是连接测试请求，使用表单草稿而不要求先保存。
type TestInput struct {
	// EndpointURL 是待测试的 Agent 地址。
	EndpointURL string
	// TokenAction 与 Token 的含义同 Draft；retain 时使用账号已保存的令牌。
	TokenAction string
	Token       string
	// RequestTimeoutSeconds 为 0 时使用默认值。
	RequestTimeoutSeconds int
}

// TestResult 是连接测试的非敏感诊断结果，不含响应正文和令牌。
type TestResult struct {
	// Status 取 TestStatus* 常量。
	Status string
	// StatusCode 是 Agent 的 HTTP 状态码；未到达时为 0。
	StatusCode int
	// DurationMillis 是请求耗时毫秒数。
	DurationMillis int64
	// Handled、Working 与 ReplyCount 是协议响应的概要，仅 Status 为 ok 时有意义。
	Handled    bool
	Working    bool
	ReplyCount int
}

// AccountOwnership 是账号所有权 Port 返回的非敏感结果。
type AccountOwnership struct {
	// OwnerID 是账号所属本地用户标识；账号不存在时 Port 应返回 ErrAccountNotFound。
	OwnerID int64
}

// Repository 定义 Agent 配置用例所需的最小持久化能力。
type Repository interface {
	// CheckOwnership 查询账号所有者，不读取或解密任何凭证字段。
	CheckOwnership(ctx context.Context, userID int64, cookieID string) (AccountOwnership, error)
	// Get 读取配置摘要；不存在返回 ErrConfigNotFound。
	Get(ctx context.Context, cookieID string) (Settings, error)
	// Save 保存配置并按 TokenAction 处理令牌。
	Save(ctx context.Context, cookieID string, draft Draft) error
	// StoredToken 读取已保存的访问令牌明文，仅供连接测试使用；没有配置返回空串。
	StoredToken(ctx context.Context, cookieID string) (string, error)
}

// Endpoint 描述一次连接测试的出站目标，Token 是敏感值。
type Endpoint struct {
	// URL 是 Agent 地址。
	URL string
	// Token 是可选访问令牌。
	Token string
	// Timeout 是单次请求超时。
	Timeout time.Duration
}

// Tester 定义发送一次 ping 事件的最小出站能力，由适配器基于协议客户端实现。
type Tester interface {
	// Ping 向 endpoint 发送 ping 并返回诊断结果；网络类失败也应编码为 TestResult 而不是 error。
	Ping(ctx context.Context, endpoint Endpoint) (TestResult, error)
}

// Service 编排 Agent 回复配置的归属校验、输入校验、读写与连接测试。
type Service struct {
	// repository 保存调用方注入的持久化 Port。
	repository Repository
	// tester 保存连接测试 Port；为空时测试返回 ErrUnavailable。
	tester Tester
}

// NewService 创建 Agent 回复应用服务。
func NewService(repository Repository, tester Tester) *Service {
	return &Service{repository: repository, tester: tester}
}

// Get 返回账号的 Agent 配置；没有保存过时返回默认关闭值。
func (s *Service) Get(ctx context.Context, userID int64, cookieID string) (Settings, error) {
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return Settings{}, err
	}
	// settings、err 保存读取结果；不存在时返回默认值而不是错误。
	settings, err := s.repository.Get(ctx, cookieID)
	if errors.Is(err, ErrConfigNotFound) {
		return defaultSettings(cookieID), nil
	}
	return settings, err
}

// Update 校验归属与输入后保存配置，并返回保存后的摘要。
func (s *Service) Update(ctx context.Context, userID int64, cookieID string, draft Draft) (Settings, error) {
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return Settings{}, err
	}
	// normalized、err 保存规整后的草稿及校验错误。
	normalized, err := normalizeDraft(draft)
	if err != nil {
		return Settings{}, err
	}
	// err 表示配置持久化错误，原样返回给调用方。
	if err := s.repository.Save(ctx, cookieID, normalized); err != nil {
		return Settings{}, err
	}
	return s.Get(ctx, userID, cookieID)
}

// Test 使用表单草稿向 Agent 发送一次 ping；retain 令牌时读取账号已保存令牌。
func (s *Service) Test(ctx context.Context, userID int64, cookieID string, input TestInput) (TestResult, error) {
	// err 表示账号标识无效、不存在或不属于当前用户的错误。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return TestResult{}, err
	}
	if s.tester == nil {
		return TestResult{}, ErrUnavailable
	}
	// draft、err 复用保存时的校验，保证测试与实际运行使用同一套规则。
	draft, err := normalizeDraft(Draft{Enabled: true, EndpointURL: input.EndpointURL, RequestTimeoutSeconds: input.RequestTimeoutSeconds,
		MaxSessionSeconds: DefaultMaxSessionSeconds, TokenAction: input.TokenAction, Token: input.Token})
	if err != nil {
		return TestResult{}, err
	}
	// token 保存本次测试使用的令牌：replace 用草稿，retain 用已保存值，clear 不带令牌。
	token := ""
	switch draft.TokenAction {
	case TokenReplace:
		token = draft.Token
	case TokenRetain:
		if token, err = s.repository.StoredToken(ctx, cookieID); err != nil {
			return TestResult{}, err
		}
	}
	return s.tester.Ping(ctx, Endpoint{URL: draft.EndpointURL, Token: token, Timeout: time.Duration(draft.RequestTimeoutSeconds) * time.Second})
}

// defaultSettings 返回账号尚未配置时的默认关闭配置。
func defaultSettings(cookieID string) Settings {
	return Settings{CookieID: cookieID, RequestTimeoutSeconds: DefaultRequestTimeoutSeconds, MaxSessionSeconds: DefaultMaxSessionSeconds}
}

// normalizeDraft 补默认值、规整空白并校验取值范围；启用时必须有合法地址。
func normalizeDraft(draft Draft) (Draft, error) {
	draft.EndpointURL = strings.TrimSpace(draft.EndpointURL)
	draft.Token = strings.TrimSpace(draft.Token)
	draft.TokenAction = strings.TrimSpace(draft.TokenAction)
	if draft.RequestTimeoutSeconds == 0 {
		draft.RequestTimeoutSeconds = DefaultRequestTimeoutSeconds
	}
	if draft.MaxSessionSeconds == 0 {
		draft.MaxSessionSeconds = DefaultMaxSessionSeconds
	}
	switch draft.TokenAction {
	case "":
		draft.TokenAction = TokenRetain
	case TokenRetain, TokenReplace, TokenClear:
	default:
		return Draft{}, &InvalidInputError{Reason: "访问令牌命令无效"}
	}
	switch {
	case draft.RequestTimeoutSeconds < MinRequestTimeoutSeconds || draft.RequestTimeoutSeconds > MaxRequestTimeoutSeconds:
		return Draft{}, &InvalidInputError{Reason: "单次请求超时需在 5 到 30 秒之间"}
	case draft.MaxSessionSeconds < MinMaxSessionSeconds || draft.MaxSessionSeconds > MaxMaxSessionSeconds:
		return Draft{}, &InvalidInputError{Reason: "单轮对话总时长需在 10 到 300 秒之间"}
	case draft.MaxSessionSeconds < draft.RequestTimeoutSeconds:
		return Draft{}, &InvalidInputError{Reason: "单轮对话总时长不能小于单次请求超时"}
	case utf8.RuneCountInString(draft.EndpointURL) > MaxEndpointURLRunes:
		return Draft{}, &InvalidInputError{Reason: "Agent 地址过长"}
	case utf8.RuneCountInString(draft.Token) > MaxTokenRunes:
		return Draft{}, &InvalidInputError{Reason: "访问令牌过长"}
	case draft.TokenAction == TokenReplace && draft.Token == "":
		return Draft{}, &InvalidInputError{Reason: "替换访问令牌时令牌不能为空"}
	case draft.Enabled && draft.EndpointURL == "":
		return Draft{}, &InvalidInputError{Reason: "启用 Agent 回复前请填写 Agent 地址"}
	}
	if draft.EndpointURL != "" {
		// reason 是地址格式问题的中文说明；空串表示地址合法。
		if reason := endpointProblem(draft.EndpointURL); reason != "" {
			return Draft{}, &InvalidInputError{Reason: reason}
		}
	}
	return draft, nil
}

// endpointProblem 返回地址格式问题；空串表示地址合法。
func endpointProblem(rawURL string) string {
	// parsed、err 保存地址解析结果及格式错误。
	parsed, err := url.Parse(rawURL)
	switch {
	case err != nil || parsed.Hostname() == "":
		return "Agent 地址无效"
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return "Agent 地址只支持 http 或 https"
	case parsed.User != nil:
		return "Agent 地址不能包含账号密码，请使用访问令牌"
	}
	return ""
}

// validateUser 检查应用服务依赖和用户身份，避免无效请求进入 Port。
func (s *Service) validateUser(userID int64) error {
	if s == nil || s.repository == nil {
		return errors.New("外部 Agent 回复 repository 未初始化")
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	return nil
}

// ensureOwned 统一执行账号标识校验和非敏感所有权判断。
func (s *Service) ensureOwned(ctx context.Context, userID int64, cookieID string) error {
	// err 表示服务依赖未装配或用户身份无效的错误。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if cookieID == "" {
		return ErrInvalidCookieID
	}
	// ownership 保存 Port 返回的账号所有者信息，不包含 Cookie 或登录秘密。
	ownership, err := s.repository.CheckOwnership(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if ownership.OwnerID != userID {
		return ErrForbidden
	}
	return nil
}
