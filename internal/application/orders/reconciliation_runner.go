package orders

import (
	"context"
	"errors"
	"sync"
)

// ReconciliationRecovery 定义订单补偿扫描器的最小生命周期能力。
type ReconciliationRecovery interface {
	// Run 持续扫描待补偿订单，直到传入 Context 被取消。
	Run(context.Context)
}

// ErrReconciliationRecoveryStopped 表示补偿扫描器已经关闭，不能再次启动。
var ErrReconciliationRecoveryStopped = errors.New("订单补偿扫描器已关闭")

// ReconciliationRecoveryCoordinator 拥有订单补偿扫描器的启动、取消、等待和关闭生命周期。
// mu 保护 cancel、done 与 stopped；锁内只更新状态，不调用补偿数据库或平台 I/O。
type ReconciliationRecoveryCoordinator struct {
	// recovery 执行补偿扫描业务，具体数据库访问由 adapter 实现。
	recovery ReconciliationRecovery
	// mu 保护生命周期字段并串行化重复 Start/Stop 调用。
	mu sync.Mutex
	// cancel 取消当前补偿扫描循环；为空表示当前未运行。
	cancel context.CancelFunc
	// done 在当前扫描循环退出时关闭；无运行循环时保持已关闭信号。
	done chan struct{}
	// stopped 表示协调器已拒绝后续启动。
	stopped bool
}

// NewReconciliationRecoveryCoordinator 创建订单补偿扫描生命周期协调器。
func NewReconciliationRecoveryCoordinator(recovery ReconciliationRecovery) (*ReconciliationRecoveryCoordinator, error) {
	if recovery == nil {
		return nil, errors.New("订单补偿扫描器不能为空")
	}
	return &ReconciliationRecoveryCoordinator{recovery: recovery, done: closedReconciliationSignal()}, nil
}

// Start 启动单一补偿扫描循环；重复启动保持幂等，生命周期 Context 由调用方提供。
func (coordinator *ReconciliationRecoveryCoordinator) Start(parent context.Context) error {
	if coordinator == nil || coordinator.recovery == nil {
		return errors.New("订单补偿扫描协调器未初始化")
	}
	if parent == nil {
		return errors.New("订单补偿扫描生命周期 Context 不能为空")
	}
	coordinator.mu.Lock()
	if coordinator.stopped {
		coordinator.mu.Unlock()
		return ErrReconciliationRecoveryStopped
	}
	if coordinator.cancel != nil {
		coordinator.mu.Unlock()
		return nil
	}
	// recoveryCtx、cancel 控制当前补偿扫描循环；Stop/Close 通过 cancel 触发退出。
	recoveryCtx, cancel := context.WithCancel(parent)
	// done 保存当前补偿扫描循环的退出信号。
	done := make(chan struct{})
	coordinator.cancel = cancel
	coordinator.done = done
	coordinator.mu.Unlock()
	go coordinator.run(recoveryCtx, done)
	return nil
}

// Stop 取消补偿扫描循环并拒绝后续启动；重复调用保持幂等。
func (coordinator *ReconciliationRecoveryCoordinator) Stop() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	if coordinator.stopped {
		coordinator.mu.Unlock()
		return
	}
	coordinator.stopped = true
	// cancel 保存当前循环取消函数；解锁后调用避免回调重入生命周期锁。
	cancel := coordinator.cancel
	coordinator.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Wait 等待当前补偿扫描循环退出；未启动时立即返回。
func (coordinator *ReconciliationRecoveryCoordinator) Wait() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	// done 保存当前生命周期的退出信号；读取后立即解锁以允许扫描器收尾。
	done := coordinator.done
	coordinator.mu.Unlock()
	<-done
}

// Close 停止补偿扫描器并在 Context 截止前等待 goroutine 退出。
func (coordinator *ReconciliationRecoveryCoordinator) Close(ctx context.Context) error {
	if coordinator == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("关闭订单补偿扫描器的 Context 不能为空")
	}
	coordinator.Stop()
	coordinator.mu.Lock()
	// done 保存 Stop 后仍可能运行的补偿扫描循环退出信号。
	done := coordinator.done
	coordinator.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run 执行补偿扫描并在退出时发布生命周期信号。
func (coordinator *ReconciliationRecoveryCoordinator) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	coordinator.recovery.Run(ctx)
	coordinator.mu.Lock()
	if coordinator.done == done {
		coordinator.cancel = nil
	}
	coordinator.mu.Unlock()
}

// closedReconciliationSignal 创建已关闭信号，供未启动的 Wait/Close 立即返回。
func closedReconciliationSignal() chan struct{} {
	// done 保存无活动扫描器状态的关闭信号。
	done := make(chan struct{})
	close(done)
	return done
}
