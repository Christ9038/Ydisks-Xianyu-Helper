package ws

import (
	"errors"
	"fmt"
	"strings"
)

// RegErrorKind 保存Reg错误类型，供当前处理流程使用
type RegErrorKind string

// RegErrorInvalidToken 保存Reg错误Invalid令牌，供当前处理流程使用
const (
	RegErrorInvalidToken   RegErrorKind = "invalid_token"
	RegErrorConnectLimit   RegErrorKind = "connect_limit"
	RegErrorAuthentication RegErrorKind = "authentication"
)

// RegError describes a server-side /reg rejection after the WebSocket itself
// has opened successfully.
// RegError 保存Reg错误，供当前处理流程使用
type RegError struct {
	Kind   RegErrorKind
	Code   int
	Reason string
}

// Error 负责错误相关处理。
func (e *RegError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("WS /reg 被拒绝: kind=%s code=%d reason=%s", e.Kind, e.Code, e.Reason)
}

// IsInvalidTokenError 负责IsInvalid令牌错误相关处理。
func IsInvalidTokenError(err error) bool {
	// regErr 保存regErr，供当前处理流程使用
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorInvalidToken
}

// IsConnectLimitError 负责IsConnect上限错误相关处理。
func IsConnectLimitError(err error) bool {
	// regErr 保存regErr，供当前处理流程使用
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorConnectLimit
}

// IsAuthenticationError 负责IsAuthentication错误相关处理。
func IsAuthenticationError(err error) bool {
	// regErr 保存regErr，供当前处理流程使用
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorAuthentication
}

// newRegError 负责newReg错误相关处理。
func newRegError(code int, frame map[string]any) error {
	// reason 保存原因，供当前处理流程使用
	reason := regErrorReason(frame)
	// lower 保存lower，供当前处理流程使用
	lower := strings.ToLower(reason)
	// kind 保存类型，供当前处理流程使用
	kind := RegErrorAuthentication
	switch {
	case code == 401,
		strings.Contains(lower, "invalid token"),
		strings.Contains(lower, "not auth"),
		strings.Contains(lower, "token invalid"),
		strings.Contains(lower, "device id or appkey is not equal"):
		kind = RegErrorInvalidToken
	case strings.Contains(lower, "connect limit"),
		strings.Contains(lower, "session remove"),
		strings.Contains(lower, "too many"):
		kind = RegErrorConnectLimit
	}
	return &RegError{Kind: kind, Code: code, Reason: reason}
}

// regErrorReason 负责reg错误原因相关处理。
func regErrorReason(frame map[string]any) string {
	// values 保存values，供当前处理流程使用
	values := make([]string, 0, 8)
	// appendValue 保存append值，供当前处理流程使用
	appendValue := func(value any) {
		if value == nil {
			return
		}
		// text 保存文本，供当前处理流程使用
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			values = append(values, text)
		}
	}
	// key 表示当前遍历过程中的key
	for _, key := range []string{"message", "msg", "reason", "ret"} {
		appendValue(frame[key])
	}
	if // body、ok 保存body、ok，供当前处理流程使用
	body, ok := frame["body"].(map[string]any); ok {
		// key 表示当前遍历过程中的key
		for _, key := range []string{"message", "msg", "reason", "moreInfo"} {
			appendValue(body[key])
		}
	}
	if // headers、ok 保存headers、ok，供当前处理流程使用
	headers, ok := frame["headers"].(map[string]any); ok {
		// key 表示当前遍历过程中的key
		for _, key := range []string{"message", "msg", "reason", "error", "error-message"} {
			appendValue(headers[key])
		}
	}
	if len(values) == 0 {
		return "unknown authentication error"
	}
	return strings.Join(values, " | ")
}
