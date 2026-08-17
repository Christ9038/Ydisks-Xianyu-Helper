package engine

import (
	"context"
	"sync"
)

// accountLifecycle 管理单账号运行上下文和业务任务生命周期。
// mu 保护 stopFn、stopped、runtimeCtx、accepting、任务计数和停止完成信号；
// 持有 mu 时只做内存操作，不执行 I/O。Stop 先禁止新任务、再取消上下文，最后等待已有任务。

// accountLifecycle 是账号业务任务生命周期组件。
type accountLifecycle struct {
	mu sync.Mutex

	// stopFn 取消当前 Run 创建的账号运行上下文。
	stopFn context.CancelFunc
	// stopped 表示 Stop 是否已经完成第一次状态切换。
	stopped bool
	// runtimeCtx 是当前业务任务共享的账号运行上下文。
	runtimeCtx context.Context
	// accepting 表示是否仍允许新的消息或自动化任务进入。
	accepting bool
	// activeTasks 记录尚未完成的任务数量，用于在超时 Stop 后由最后一个任务关闭 stopDone。
	activeTasks int
	// tasksFinished 表示 stopDone 已关闭，避免重复 finishTask 破坏共享停止信号。
	tasksFinished bool
	// stopDone 在 Stop 前已登记的业务任务全部退出后关闭；它由 lifecycle 保存，
	// 所有等待者直接订阅该信号，不为每次带超时的等待创建无法 Join 的协程。
	stopDone chan struct{}
}

// start 注册一次 Run 的上下文和取消函数，并重新开放业务任务入口。
// 若 Stop 已经先于 Run 建立了不可逆的停止 fencing，则返回 false，拒绝迟到的启动。
func (l *accountLifecycle) start(ctx context.Context, cancel context.CancelFunc) bool {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return false
	}
	l.stopFn = cancel
	l.runtimeCtx = ctx
	l.accepting = true
	l.stopDone = nil
	l.tasksFinished = false
	l.mu.Unlock()
	return true
}

// stopContext 原子地禁止新任务并取出取消函数；并发停止调用会受 ctx 限制地等待首次清理完成。
func (l *accountLifecycle) stopContext(ctx context.Context) (context.CancelFunc, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	if l.stopped {
		// done 是第一次 Stop 完成全部清理后关闭的信号。
		done := l.stopDone
		l.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
		}
		return nil, false, nil
	}
	l.stopped = true
	l.accepting = false
	l.stopDone = make(chan struct{})
	if l.activeTasks == 0 {
		close(l.stopDone)
		l.tasksFinished = true
	}
	// cancel 是当前账号 Run 上下文的取消函数。
	cancel := l.stopFn
	l.mu.Unlock()
	return cancel, true, nil
}

// beginTask 在生命周期锁内登记业务任务，避免 Stop 与 WaitGroup.Add 竞争。
func (l *accountLifecycle) beginTask() (context.Context, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.accepting {
		return nil, false
	}
	// ctx 是新业务任务继承的账号运行上下文。
	ctx := l.runtimeCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	l.activeTasks++
	return ctx, true
}

// finishTask 标记一个已登记的业务任务退出。
func (l *accountLifecycle) finishTask() {
	l.mu.Lock()
	if l.activeTasks > 0 {
		l.activeTasks--
	}
	if l.stopped && l.activeTasks == 0 && !l.tasksFinished && l.stopDone != nil {
		close(l.stopDone)
		l.tasksFinished = true
	}
	l.mu.Unlock()
}

// waitContext 等待 Stop 前已登记的业务任务结束，并在 ctx 到期时及时返回。
// stopDone 由最后一个任务关闭，因而超时返回不遗留等待协程；调用方可用新的上下文再次等待同一信号。
func (l *accountLifecycle) waitContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	// done 是 Stop 为当前收束周期创建、由最后一个已登记任务关闭的共享信号。
	done := l.stopDone
	l.mu.Unlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
