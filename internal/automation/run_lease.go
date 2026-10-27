package automation

import (
	"context"
	"fmt"
	"sync"
	"time"
	"xianyu-go/internal/db"
)

const (
	// runExecutionLeaseDuration 是外部动作的数据库租约时长；执行中每分钟续租且每次副作用前复核。
	runExecutionLeaseDuration = 5 * time.Minute
	// runExecutionLeaseInterval 让慢批次在原租约到期前持续保持执行权。
	runExecutionLeaseInterval = time.Minute
	// runExecutionLeaseIOTimeout 限制每次租约查询，失败立即取消动作上下文。
	runExecutionLeaseIOTimeout = 5 * time.Second
)

// runLeaseRepository 是执行器消费的最小租约端口；拒绝已过期、已结束或不同代次的执行者。
type runLeaseRepository interface {
	RenewExecutingRunLease(context.Context, int64, int, int64) error
}

// runLeaseContextKey 将单个动作拥有的租约守卫传给内部逐条消息和取卡入口。
type runLeaseContextKey struct{}

// runExecutionLease 拥有一次动作的续租协程与取消信号。mu 只串行化有五秒超时的租约数据库操作；不跨平台 I/O。
// 调用者必须 stop，先取消再等待 done；后台协程是 done 的唯一关闭者。
type runExecutionLease struct {
	// repository 在构造后固定，只能续租当前运行代次。
	repository runLeaseRepository
	// runID、attempt 固定本次执行权身份，不随外部运行快照改变。
	runID   int64
	attempt int
	// ctx、cancel 将调用者取消或续租失败传递给所有外部动作。
	ctx    context.Context
	cancel context.CancelCauseFunc
	// mu 避免心跳和发送前检查并发写入租约造成时间倒退。
	mu sync.Mutex
	// done 在续租循环结束时关闭，stop 等待它释放资源。
	done chan struct{}
}

// startRunExecutionLease 为 repository 中 runID/attempt 创建守卫；interval 控制续租周期，返回动作上下文、必须调用的停止函数及初始领取错误。
func startRunExecutionLease(parent context.Context, repository runLeaseRepository, runID int64, attempt int, interval time.Duration) (context.Context, func(), error) {
	// ctx、cancel 是当前执行者独占的可带原因取消上下文。
	ctx, cancel := context.WithCancelCause(parent)
	// lease 保存固定执行代次与协程生命周期；初次续租失败不启动后台任务。
	lease := &runExecutionLease{repository: repository, runID: runID, attempt: attempt, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if err := lease.check(); err != nil { // err 表示执行权在实际发送前已失效。
		cancel(err)
		return nil, nil, err
	}
	go lease.run(interval)
	return context.WithValue(ctx, runLeaseContextKey{}, lease), lease.stop, nil
}

// check 在 lease 的固定代次仍有效时续租；任何存储失败或失权都取消当前动作，返回不含凭证的原因。
func (lease *runExecutionLease) check() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if err := context.Cause(lease.ctx); err != nil { // err 保留调用者取消或第一次续租失败原因。
		return err
	}
	// checkCtx、cancel 限制本地数据库操作，不允许租约故障拖住平台动作关闭。
	checkCtx, cancel := context.WithTimeout(lease.ctx, runExecutionLeaseIOTimeout)
	defer cancel()
	if err := lease.repository.RenewExecutingRunLease(checkCtx, lease.runID, lease.attempt, time.Now().Add(runExecutionLeaseDuration).Unix()); err != nil { // err 表示无法证明自己仍有执行权，禁止继续副作用。
		lease.cancel(err)
		return err
	}
	return nil
}

// run 在 interval 驱动下续租 lease，取消或失败时关闭 done；没有平台调用也不会写入运行终态。
func (lease *runExecutionLease) run(interval time.Duration) {
	defer close(lease.done)
	// ticker 由本协程创建和停止，不在动作结束后继续续租。
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-lease.ctx.Done():
			return
		case <-ticker.C:
			if lease.check() != nil {
				return
			}
		}
	}
}

// stop 可重复调用；取消 lease 的心跳并等待其有界数据库操作退出。
func (lease *runExecutionLease) stop() {
	lease.cancel(context.Canceled)
	<-lease.done
}

// checkRunExecution 在逐次副作用之前复核 ctx 携带的执行代次；无持久化运行的独立调用只检查取消。
func checkRunExecution(ctx context.Context) error {
	// 历史独立动作测试允许 nil Context；生产持久化执行由 startRunExecutionLease 强制使用父 Context。
	if ctx == nil {
		return nil
	}
	if err := context.Cause(ctx); err != nil { // err 表示当前调用已经取消或失权。
		return err
	}
	// lease、ok 区分持久化运行与独立动作测试，两者都保留 Context 取消约束。
	lease, ok := ctx.Value(runLeaseContextKey{}).(*runExecutionLease)
	if !ok {
		return nil
	}
	if err := lease.check(); err != nil { // err 保留数据库失权分类，便于上层隔离未知结果。
		return fmt.Errorf("自动化执行权已失效: %w", err)
	}
	return nil
}

// runIDFromContext 返回 ctx 所属持久化自动化运行的标识；独立动作测试或人工路径没有租约时返回零。
// 卡密售出台账用它把消费记录挂到运行上，运行成功收口时再批量确认待核对记录。
func runIDFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	// lease、ok 区分持久化运行与没有租约的独立调用。
	lease, ok := ctx.Value(runLeaseContextKey{}).(*runExecutionLease)
	if !ok || lease == nil {
		return 0
	}
	return lease.runID
}

// executeLeasedAction 为 r 的单个已领取动作维护租约；task/action/proof 保持原业务输入，run 固定执行代次，退出前等待心跳结束。
func (r automationRunCoordinator) executeLeasedAction(ctx context.Context, task Task, action db.AutomationAction, proof shipmentDeliveryProof, run *db.AutomationRun) (actionExecutionResult, error) {
	// executionCtx、stopLease、err 在外部动作开始前确认数据库执行权。
	executionCtx, stopLease, err := startRunExecutionLease(ctx, r.store.Automation, run.ID, run.AttemptCount, runExecutionLeaseInterval)
	if err != nil {
		return actionExecutionResult{}, err
	}
	defer stopLease()
	return r.executeActionNow(executionCtx, task, action, proof)
}
