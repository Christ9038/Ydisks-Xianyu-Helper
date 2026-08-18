package ws

import (
	"errors"
	"fmt"
	"strings"
)

// RegErrorKind 用于本次流程后续判断的Reg错误类型
type RegErrorKind string

// RegErrorInvalidToken 用于本次流程后续判断的Reg错误Invalid令牌
const (
	RegErrorInvalidToken   RegErrorKind = "invalid_token"
	RegErrorConnectLimit   RegErrorKind = "connect_limit"
	RegErrorAuthentication RegErrorKind = "authentication"
)

// RegError describes a server-side /reg rejection after the WebSocket itself
// has opened successfully.
// RegError 用于本次流程后续判断的Reg错误
type RegError struct {
	Kind   RegErrorKind
	Code   int
	Reason string
}

// Error 封装错误业务协调。
func (e *RegError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("WS /reg 被拒绝: kind=%s code=%d reason=%s", e.Kind, e.Code, e.Reason)
}

// IsInvalidTokenError 封装IsInvalid令牌错误业务协调。
func IsInvalidTokenError(err error) bool {
	// regErr 用于本次流程后续判断的regErr
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorInvalidToken
}

// IsConnectLimitError 封装IsConnect上限错误业务协调。
func IsConnectLimitError(err error) bool {
	// regErr 用于本次流程后续判断的regErr
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorConnectLimit
}

// IsAuthenticationError 封装IsAuthentication错误业务协调。
func IsAuthenticationError(err error) bool {
	// regErr 用于本次流程后续判断的regErr
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorAuthentication
}

// newRegError 封装newReg错误业务协调。
func newRegError(code int, frame map[string]any) error {
	// reason 用于本次流程后续判断的原因
	reason := regErrorReason(frame)
	// lower 用于本次流程后续判断的lower
	lower := strings.ToLower(reason)
	// kind 用于本次流程后续判断的类型
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

// regErrorReason 封装reg错误原因业务协调。
func regErrorReason(frame map[string]any) string {
	// values 用于本次流程后续判断的values
	values := make([]string, 0, 8)
	// appendValue 用于本次流程后续判断的append值
	appendValue := func(value any) {
		if value == nil {
			return
		}
		// text 用于本次流程后续判断的文本
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			values = append(values, text)
		}
	}
	// key 表示当前遍历过程中的key
	for _, key := range []string{"message", "msg", "reason", "ret"} {
		appendValue(frame[key])
	}
	if // body、ok 用于本次流程后续判断的body、ok
	body, ok := frame["body"].(map[string]any); ok {
		// key 表示当前遍历过程中的key
		for _, key := range []string{"message", "msg", "reason", "moreInfo"} {
			appendValue(body[key])
		}
	}
	if // headers、ok 用于本次流程后续判断的headers、ok
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
