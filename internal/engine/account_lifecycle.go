package engine

import (
	"context"
	"sync"
	"time"
)

// accountLifecycle 管理单账号运行上下文和业务任务生命周期。
// mu 保护 stopFn、stopped、runtimeCtx、accepting 以及 taskWG.Add 与停止切换的顺序；
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
	// taskWG 等待所有已经通过 beginTask 的业务任务退出。
	taskWG sync.WaitGroup
	// stopDone 在第一次 Stop 的清理完成后关闭，保证并发 Stop 调用也等待完整收束。
	stopDone chan struct{}
}

// start 注册一次 Run 的上下文和取消函数，并重新开放业务任务入口。
func (l *accountLifecycle) start(ctx context.Context, cancel context.CancelFunc) {
	l.mu.Lock()
	l.stopFn = cancel
	l.runtimeCtx = ctx
	l.accepting = true
	l.mu.Unlock()
}

// stop 原子地禁止新任务并取出运行上下文取消函数。
func (l *accountLifecycle) stop() (context.CancelFunc, bool) {
	// cancel、first 分别表示运行上下文取消函数和是否由本次调用负责首次停止。
	cancel, first, _ := l.stopContext(context.Background())
	return cancel, first
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
	// cancel 是当前账号 Run 上下文的取消函数。
	cancel := l.stopFn
	l.mu.Unlock()
	return cancel, true, nil
}

// finishStop 标记第一次 Stop 的清理已完成，并唤醒并发 Stop 调用者。
func (l *accountLifecycle) finishStop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopDone != nil {
		close(l.stopDone)
		l.stopDone = nil
	}
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
	l.taskWG.Add(1)
	return ctx, true
}

// finishTask 标记一个已登记的业务任务退出。
func (l *accountLifecycle) finishTask() {
	l.taskWG.Done()
}

// wait 等待所有已登记业务任务结束；timeout 小于等于零表示无限等待。
func (l *accountLifecycle) wait(timeout time.Duration) bool {
	if timeout <= 0 {
		return l.waitContext(context.Background())
	}
	// ctx、cancel 分别表示有限等待上下文和释放定时器的函数。
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return l.waitContext(ctx)
}

// waitContext 等待已登记业务任务结束，并在 ctx 到期时及时返回。
func (l *accountLifecycle) waitContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	// done 是所有已登记任务完成后关闭的等待信号。
	done := make(chan struct{})
	go func() {
		l.taskWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
