package adapter

import (
	"context"
	"errors"
	"testing"
)

// TestSessionRecoveryHandlerFiltersNonExpiredErrors 验证普通平台错误不会触发账号恢复。
func TestSessionRecoveryHandlerFiltersNonExpiredErrors(t *testing.T) {
	// calls 记录恢复端口被调用次数。
	calls := 0
	// handler 是绑定测试恢复端口的会话恢复适配器。
	handler := NewSessionRecoveryHandler(nil, func(context.Context, string) bool {
		calls++
		return true
	})
	// recovered 表示普通错误的恢复结果。
	recovered := handler(context.Background(), "acc1", errors.New("ordinary failure"))
	if recovered || calls != 0 {
		t.Fatalf("普通错误不应触发恢复: recovered=%v calls=%d", recovered, calls)
	}
}

// TestSessionRecoveryHandlerDelegatesExpiredErrors 验证 Session 失效只触发一次恢复端口。
func TestSessionRecoveryHandlerDelegatesExpiredErrors(t *testing.T) {
	// calls 记录恢复端口被调用次数。
	calls := 0
	// handler 是绑定测试恢复端口的会话恢复适配器。
	handler := NewSessionRecoveryHandler(nil, func(_ context.Context, accountID string) bool {
		if accountID != "acc1" {
			t.Fatalf("恢复账号错误: %q", accountID)
		}
		calls++
		return true
	})
	// recovered 表示已识别 Session 失效后的恢复结果。
	recovered := handler(context.Background(), "acc1", errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if !recovered || calls != 1 {
		t.Fatalf("Session 失效未正确委托: recovered=%v calls=%d", recovered, calls)
	}
}
