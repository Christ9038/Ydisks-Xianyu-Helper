package ws

import (
	"errors"
	"fmt"
	"strings"
)

type RegErrorKind string

const (
	RegErrorInvalidToken   RegErrorKind = "invalid_token"
	RegErrorConnectLimit   RegErrorKind = "connect_limit"
	RegErrorAuthentication RegErrorKind = "authentication"
)

// RegError describes a server-side /reg rejection after the WebSocket itself
// has opened successfully.
type RegError struct {
	Kind   RegErrorKind
	Code   int
	Reason string
}

func (e *RegError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("WS /reg 被拒绝: kind=%s code=%d reason=%s", e.Kind, e.Code, e.Reason)
}

func IsInvalidTokenError(err error) bool {
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorInvalidToken
}

func IsConnectLimitError(err error) bool {
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorConnectLimit
}

func IsAuthenticationError(err error) bool {
	var regErr *RegError
	return errors.As(err, &regErr) && regErr.Kind == RegErrorAuthentication
}

func newRegError(code int, frame map[string]any) error {
	reason := regErrorReason(frame)
	lower := strings.ToLower(reason)
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

func regErrorReason(frame map[string]any) string {
	values := make([]string, 0, 8)
	appendValue := func(value any) {
		if value == nil {
			return
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			values = append(values, text)
		}
	}
	for _, key := range []string{"message", "msg", "reason", "ret"} {
		appendValue(frame[key])
	}
	if body, ok := frame["body"].(map[string]any); ok {
		for _, key := range []string{"message", "msg", "reason", "moreInfo"} {
			appendValue(body[key])
		}
	}
	if headers, ok := frame["headers"].(map[string]any); ok {
		for _, key := range []string{"message", "msg", "reason", "error", "error-message"} {
			appendValue(headers[key])
		}
	}
	if len(values) == 0 {
		return "unknown authentication error"
	}
	return strings.Join(values, " | ")
}
