package ws

import (
	"errors"
	"testing"
)

// TestRegErrorAndClassifiers 验证 WebSocket 注册拒绝错误的文本和稳定分类判断。
func TestRegErrorAndClassifiers(t *testing.T) {
	// invalidToken 是无效令牌分类的注册拒绝错误。
	invalidToken := newRegError(401, map[string]any{"message": "invalid token"})
	if !IsInvalidTokenError(invalidToken) || IsConnectLimitError(invalidToken) || IsAuthenticationError(invalidToken) {
		t.Fatalf("invalid token classification failed: %v", invalidToken)
	}
	// connectLimit 是连接数限制分类的注册拒绝错误。
	connectLimit := newRegError(500, map[string]any{"body": map[string]any{"reason": "too many connections"}})
	if !IsConnectLimitError(connectLimit) || IsInvalidTokenError(connectLimit) {
		t.Fatalf("connect limit classification failed: %v", connectLimit)
	}
	// authentication 是普通认证失败分类的注册拒绝错误。
	authentication := newRegError(500, map[string]any{"headers": map[string]any{"error-message": "challenge"}})
	if !IsAuthenticationError(authentication) {
		t.Fatalf("authentication classification failed: %v", authentication)
	}
	// wrapped 是包裹注册错误后的错误链。
	wrapped := errors.New("wrapped")
	if IsInvalidTokenError(wrapped) || IsConnectLimitError(wrapped) || IsAuthenticationError(wrapped) {
		t.Fatal("unrelated error must not match a registration classification")
	}
	// rendered 保存普通注册拒绝错误的稳定脱敏文本。
	rendered := (&RegError{Kind: RegErrorAuthentication, Code: 403, Reason: "challenge"}).Error()
	if rendered != "WS /reg 被拒绝: kind=authentication code=403 reason=challenge" {
		t.Fatalf("注册拒绝文本=%q", rendered)
	}
	// var nilError 表示未初始化的注册错误指针。
	var nilError *RegError
	if nilError.Error() != "" {
		t.Fatalf("nil error text=%q", nilError.Error())
	}
}
