package engine

import (
	"context"
	"sync"
	"time"
)

// agentTurn 是一个会话当前进行中的 Agent 对话轮次。
type agentTurn struct {
	// cancel 取消该轮次的上下文；被新消息顶替或账号停止时调用。
	cancel context.CancelFunc
	// done 在轮次结束登记后关闭，等待方据此确认旧轮次已完全退出。
	done chan struct{}
}

// agentTurnRegistry 保证同一会话同一时刻只有一个 Agent 对话轮次在运行。
//
// 所有权：由 AgentReplier 持有，随账号运行时存在；不创建 goroutine，轮次在调用方（防抖回调）goroutine 内同步运行。
// 锁：mu 只保护 turns 映射；任何等待（旧轮次退出、网络 I/O）都在释放 mu 之后进行。
type agentTurnRegistry struct {
	// mu 保护 turns。
	mu sync.Mutex
	// turns 以聊天会话标识为键保存进行中的轮次。
	turns map[string]*agentTurn
}

// begin 为 chatID 登记新轮次：若已有旧轮次则先取消并等待其退出，再返回带总时长上限的上下文和结束函数。
// 等待期间若 parent 被取消则返回其错误且不登记。结束函数幂等，必须由调用方 defer 调用。
func (r *agentTurnRegistry) begin(parent context.Context, chatID string, limit time.Duration) (context.Context, func(), error) {
	for {
		r.mu.Lock()
		// previous 是同一会话仍在运行的旧轮次。
		previous := r.turns[chatID]
		if previous == nil {
			break
		}
		previous.cancel()
		// done 是旧轮次的退出信号，在释放锁之后等待。
		done := previous.done
		r.mu.Unlock()
		select {
		case <-done:
		case <-parent.Done():
			return nil, nil, parent.Err()
		}
	}
	defer r.mu.Unlock()
	if r.turns == nil {
		r.turns = make(map[string]*agentTurn)
	}
	// turnCtx、cancel 是本轮次的上下文及其取消函数，总时长受 limit 约束。
	turnCtx, cancel := context.WithTimeout(parent, limit)
	// turn 是本轮次的登记项。
	turn := &agentTurn{cancel: cancel, done: make(chan struct{})}
	r.turns[chatID] = turn
	// once 保证结束函数幂等。
	var once sync.Once
	return turnCtx, func() {
		once.Do(func() {
			r.mu.Lock()
			if r.turns[chatID] == turn {
				delete(r.turns, chatID)
			}
			r.mu.Unlock()
			cancel()
			close(turn.done)
		})
	}, nil
}
