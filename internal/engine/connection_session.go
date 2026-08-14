package engine

import (
	"context"
	"time"
)

// connectionSessionResult 是一次已注册 WebSocket 会话的收束结果。
// ReceiveErr 和 HeartbeatErr 由 Account facade 负责解释；Rotated 表示是否由 Token
// 轮换定时器主动关闭连接，ConnectedDuration 用于短连接诊断。

// connectionSessionResult 是已注册连接的生命周期结果结构。
type connectionSessionResult struct {
	// ReceiveErr 是 ReceiveLoop 返回的连接结果。
	ReceiveErr error
	// HeartbeatErr 是心跳 goroutine 返回的连接结果。
	HeartbeatErr error
	// Rotated 表示会话是否因 Token 即将过期而主动轮换。
	Rotated bool
	// ConnectedDuration 是本次连接从建立到收束的持续时间。
	ConnectedDuration time.Duration
}

// runConnectionSession 运行一次已完成注册的 WebSocket 会话。
// 本组件只拥有心跳、接收、Token 轮换 goroutine 的创建/取消/等待责任；不持有凭证锁，
// 不执行凭证 I/O，也不改变 Account facade 对连接错误、风控和重连结果的解释顺序。

// runConnectionSession 是已注册 WebSocket 会话的生命周期入口。
func (a *Account) runConnectionSession(ctx context.Context, conn WSConn, refreshAt time.Time) connectionSessionResult {
	// heartbeatCtx 是心跳与轮换 goroutine 共享的可取消上下文。
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	// heartbeatErr 是心跳 goroutine 的最终错误，必须在 heartbeatDone 后读取。
	var heartbeatErr error
	// heartbeatDone 表示心跳 goroutine 已经写入 heartbeatErr 并退出。
	heartbeatDone := make(chan struct{})
	go func() {
		heartbeatErr = conn.HeartbeatLoop(heartbeatCtx, HeartbeatInterval)
		_ = conn.Close()
		heartbeatCancel()
		close(heartbeatDone)
	}()

	// rotateCh 是 Token 到达轮换时间后发送的单次信号。
	rotateCh := make(chan struct{}, 1)
	if refreshAt.IsZero() || !refreshAt.After(time.Now()) {
		refreshAt = time.Now()
	}
	// rotateTimer 是本次连接的 Token 主动轮换定时器。
	rotateTimer := time.NewTimer(time.Until(refreshAt))
	// rotateDone 表示轮换监视 goroutine 已经退出。
	rotateDone := make(chan struct{})
	go func() {
		defer close(rotateDone)
		select {
		case <-heartbeatCtx.Done():
		case <-rotateTimer.C:
			select {
			case rotateCh <- struct{}{}:
			default:
			}
			_ = conn.Close()
		}
	}()

	// receiveErr 是当前连接接收循环返回的错误。
	receiveErr := conn.ReceiveLoop(ctx, a.dispatch)
	if !rotateTimer.Stop() {
		select {
		case <-rotateTimer.C:
		default:
		}
	}
	heartbeatCancel()
	<-rotateDone
	<-heartbeatDone
	_ = conn.Close()

	// rotated 表示接收循环结束前是否收到主动轮换信号。
	rotated := false
	select {
	case <-rotateCh:
		rotated = true
	default:
	}
	// connectedDuration 是从运行状态组件读取的连接持续时间快照。
	a.accountRuntimeState.mu.Lock()
	startedAt := a.connStartedAt
	a.conn = nil
	a.accountRuntimeState.mu.Unlock()
	connectedDuration := time.Since(startedAt)
	return connectionSessionResult{
		ReceiveErr:        receiveErr,
		HeartbeatErr:      heartbeatErr,
		Rotated:           rotated,
		ConnectedDuration: connectedDuration,
	}
}
