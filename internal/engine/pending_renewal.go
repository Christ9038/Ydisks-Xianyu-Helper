package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"xianyu-go/internal/xianyu/renew"
)

// pendingRenewalCoordinator 拥有静默续期迟到响应的后台等待与 Join 生命周期。
type pendingRenewalCoordinator struct {
	// mu 保护 activeTasks 与 done；持锁时只维护任务计数和完成信号，绝不等待外部 I/O。
	mu sync.Mutex
	// activeTasks 记录当前仍在等待或持久化迟到续期响应的任务数量。
	activeTasks int
	// done 由最后一个已登记任务关闭；等待者只订阅该共享信号，超时返回时不会遗留等待协程。
	done chan struct{}
}

// beginTask 登记一个已获账号生命周期接纳的迟到续期任务，并在新一轮任务开始时创建完成信号。
func (c *pendingRenewalCoordinator) beginTask() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.activeTasks == 0 {
		c.done = make(chan struct{})
	}
	c.activeTasks++
	c.mu.Unlock()
}

// finishTask 标记一条迟到续期任务已退出，并在最后一条任务收束时通知所有 Join 等待者。
func (c *pendingRenewalCoordinator) finishTask() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.activeTasks == 0 {
		c.mu.Unlock()
		return
	}
	c.activeTasks--
	// done 保存本轮迟到续期任务的共享完成信号；只有任务计数归零时才允许关闭。
	done := c.done
	if c.activeTasks == 0 && done != nil {
		close(done)
	}
	c.mu.Unlock()
}

// waitContext 在 ctx 的关闭预算内等待所有已登记迟到续期任务退出。
// 调用方取消账号运行 Context 后仍可使用独立关闭预算等待；任务不响应取消时返回 false，
// 但组件继续保留同一 done 信号，后续 StopContext 可再次 Join，且不创建旁路等待协程。
func (c *pendingRenewalCoordinator) waitContext(ctx context.Context) bool {
	if c == nil {
		return true
	}
	if ctx == nil {
		return false
	}
	c.mu.Lock()
	// done 保存当前任务轮次的共享完成信号；nil 表示没有任何待收束的迟到续期任务。
	done := c.done
	c.mu.Unlock()
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

// watch 登记并异步等待一条迟到续期响应，所有持久化工作继承账号生命周期 Context。
func (c *pendingRenewalCoordinator) watch(
	parent context.Context,
	beginTask func() (context.Context, func(), bool),
	result *renew.Result,
	persist func(context.Context, *renew.Result) error,
	logger *slog.Logger,
) {
	if c == nil || result == nil || !result.HasPending() {
		return
	}
	// taskCtx、finish、accepted 分别是账号生命周期任务上下文、释放函数与接纳结果。
	taskCtx, finish, accepted := beginTask()
	if !accepted {
		return
	}
	if parent != nil {
		taskCtx = parent
	}
	if logger == nil {
		logger = slog.Default()
	}
	c.beginTask()
	go func() {
		defer c.finishTask()
		defer finish()
		// ctx、cancel 保存迟到响应等待的限时上下文及其取消函数。
		ctx, cancel := context.WithTimeout(taskCtx, 35*time.Second)
		defer cancel()
		// late、waitErr 保存底层迟到响应及等待错误。
		late, waitErr := result.AwaitPending(ctx)
		if late == nil {
			if waitErr != nil {
				logger.Warn("等待静默续期底层响应失败", "err", waitErr)
			}
			return
		}
		// persistErr 保存迟到 Cookie 合并持久化错误。
		if persistErr := persist(ctx, late); persistErr != nil {
			logger.Warn("保存静默续期迟到 Cookie 失败", "err", persistErr)
			return
		}
		if waitErr != nil {
			logger.Warn("静默续期底层响应失败，已保存响应 Cookie", "err", waitErr)
		}
	}()
}
