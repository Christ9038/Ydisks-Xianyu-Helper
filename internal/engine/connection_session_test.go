package engine

import (
	"context"
	"testing"
	"time"
)

// TestRunConnectionSessionOwnsHeartbeatAndReceiveCleanup 验证会话组件统一取消、等待心跳与接收 goroutine，并清理连接状态。
func TestRunConnectionSessionOwnsHeartbeatAndReceiveCleanup(t *testing.T) {
	// account 是用于直接调用会话组件的测试 facade。
	account := New(Config{CookieID: "session-test", CookieStr: "unb=1"})
	// heartbeatDone 是 fake WebSocket 心跳循环退出时关闭的信号。
	heartbeatDone := make(chan struct{})
	// conn 是可通过关闭信号结束接收循环的测试连接。
	conn := &fakeWSConn{recvBlock: true, closeCh: make(chan struct{}), heartbeatDone: heartbeatDone}
	// accountRuntimeState 记录本次会话的连接和建立时间，模拟注册成功后的状态。
	account.accountRuntimeState.mu.Lock()
	account.conn = conn
	account.connStartedAt = time.Now().Add(-time.Second)
	account.accountRuntimeState.mu.Unlock()
	// ctx 是控制会话 goroutine 退出的上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// resultDone 是会话组件返回结果时关闭的信号。
	resultDone := make(chan connectionSessionResult, 1)
	go func() {
		resultDone <- account.runConnectionSession(ctx, conn, time.Now().Add(time.Minute))
	}()
	cancel()
	// result 是会话组件完成所有 goroutine 收束后的结果。
	var result connectionSessionResult
	select {
	case result = <-resultDone:
	case <-time.After(time.Second):
		t.Fatal("会话组件未在取消后完成收束")
	}
	select {
	case <-heartbeatDone:
	case <-time.After(time.Second):
		t.Fatal("心跳 goroutine 未退出")
	}
	if result.Rotated {
		t.Fatal("取消会话不应被识别为 Token 主动轮换")
	}
	account.accountRuntimeState.mu.Lock()
	// currentConn 是会话组件清理后的连接状态快照。
	currentConn := account.conn
	account.accountRuntimeState.mu.Unlock()
	if currentConn != nil {
		t.Fatal("会话结束后连接状态未清理")
	}
}
