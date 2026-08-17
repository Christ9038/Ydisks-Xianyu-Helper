package items

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	// defaultBatchWorkerTimeout 限制单个商品批次 worker 的最长运行时间。
	defaultBatchWorkerTimeout = 2 * time.Hour
	// defaultBatchRecoveryInterval 控制商品批次恢复扫描的默认轮询周期。
	defaultBatchRecoveryInterval = time.Minute
)

var (
	// ErrBatchWorkerCoordinatorStopped 表示批次 worker 协调器已关闭，不能再接收新 worker。
	ErrBatchWorkerCoordinatorStopped = errors.New("批量发布 worker 协调器已停止")
	// ErrBatchWorkerCoordinatorInvalidJob 表示批次标识、用户标识或租约令牌无效。
	ErrBatchWorkerCoordinatorInvalidJob = errors.New("批量发布 worker 参数无效")
)

// BatchWorkerCoordinatorOptions 配置批量 worker 超时、恢复扫描和异步错误观测策略。
type BatchWorkerCoordinatorOptions struct {
	// WorkerTimeout 是单个批次 worker 的最长运行时间，非正值使用默认值。
	WorkerTimeout time.Duration
	// RecoveryInterval 是恢复扫描循环两次扫描之间的等待时间，非正值使用默认值。
	RecoveryInterval time.Duration
	// OnWorkerError 接收异步 worker 的业务或终态错误；nil 时错误不向外传播。
	OnWorkerError func(string, error)
	// OnRecoveryError 接收恢复扫描的基础设施错误；nil 时错误只结束当前扫描。
	OnRecoveryError func(error)
}

// batchWorkerHandle 保存批次 worker 的租约令牌和取消控制。
type batchWorkerHandle struct {
	// token 是当前 worker 持有的批次租约令牌。
	token string
	// cancel 取消当前 worker 的超时 Context。
	cancel context.CancelFunc
}

// BatchWorkerCoordinator 拥有批次 worker 取消表、超时、恢复扫描和关闭等待生命周期。
// mu 保护 workers、active、done、recoveryCancel 与 stopped；锁内只修改内存状态或触发取消，不执行仓储、平台或文件 I/O。
type BatchWorkerCoordinator struct {
	// runner 执行逐行发布、租约续期和批次状态收口。
	runner *BatchRunner
	// recovery 编排过期批次的租约接管和恢复准备。
	recovery *BatchRecoveryService
	// options 保存 worker 生命周期与错误观测策略。
	options BatchWorkerCoordinatorOptions
	// mu 保护 worker 表和生命周期计数。
	mu sync.Mutex
	// workers 保存按批次标识索引的可取消 worker。
	workers map[string]*batchWorkerHandle
	// active 保存 worker 与恢复扫描 goroutine 的总数。
	active int
	// done 在 active 归零时关闭；下一批活动任务开始时重新创建。
	done chan struct{}
	// recoveryCancel 取消当前恢复扫描循环；同一协调器最多运行一个循环。
	recoveryCancel context.CancelFunc
	// stopped 表示协调器不再接受新的 worker 或恢复扫描。
	stopped bool
}

// NewBatchWorkerCoordinator 创建批量 worker 生命周期协调器并校验必需应用端口。
func NewBatchWorkerCoordinator(runner *BatchRunner, recovery *BatchRecoveryService, options BatchWorkerCoordinatorOptions) (*BatchWorkerCoordinator, error) {
	if runner == nil {
		return nil, errors.New("批量 worker runner 不能为空")
	}
	if recovery == nil {
		return nil, errors.New("批量 worker 恢复服务不能为空")
	}
	if options.WorkerTimeout <= 0 {
		options.WorkerTimeout = defaultBatchWorkerTimeout
	}
	if options.RecoveryInterval <= 0 {
		options.RecoveryInterval = defaultBatchRecoveryInterval
	}
	return &BatchWorkerCoordinator{runner: runner, recovery: recovery, options: options, workers: make(map[string]*batchWorkerHandle), done: closedBatchWorkerSignal()}, nil
}

// Start 启动一个已经由应用服务声明租约的批次 worker。
// ctx 必须来自应用生命周期；HTTP 请求 Context 不得直接作为后台 worker 的父 Context。
func (coordinator *BatchWorkerCoordinator) Start(ctx context.Context, userID int64, batchID, workerToken string) error {
	if coordinator == nil || coordinator.runner == nil || coordinator.recovery == nil {
		return errors.New("批量 worker 协调器未初始化")
	}
	if ctx == nil {
		return errors.New("批量 worker 生命周期 Context 不能为空")
	}
	if userID <= 0 || strings.TrimSpace(batchID) == "" || strings.TrimSpace(workerToken) == "" {
		return ErrBatchWorkerCoordinatorInvalidJob
	}
	// workerCtx、cancel 限制批次 worker 最长运行时间并继承应用关闭信号。
	workerCtx, cancel := context.WithTimeout(ctx, coordinator.options.WorkerTimeout)
	coordinator.mu.Lock()
	if coordinator.stopped {
		coordinator.mu.Unlock()
		cancel()
		return ErrBatchWorkerCoordinatorStopped
	}
	// previous 保存同一批次旧 worker 的控制句柄；租约切换时必须先取消旧 worker。
	previous := coordinator.workers[batchID]
	if previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	// worker 保存新登记的批次控制句柄；指针身份防止旧 worker 退出时误删新 worker。
	worker := &batchWorkerHandle{token: workerToken, cancel: cancel}
	coordinator.workers[batchID] = worker
	coordinator.beginActiveLocked()
	coordinator.mu.Unlock()
	go coordinator.runWorker(workerCtx, cancel, userID, batchID, workerToken, worker)
	return nil
}

// Cancel 取消仍登记在协调器中的指定批次 worker，并校验租约令牌避免误取消新 worker。
func (coordinator *BatchWorkerCoordinator) Cancel(batchID, workerToken string) bool {
	if coordinator == nil || strings.TrimSpace(batchID) == "" || strings.TrimSpace(workerToken) == "" {
		return false
	}
	coordinator.mu.Lock()
	// worker 保存当前批次的内存取消控制句柄。
	worker := coordinator.workers[batchID]
	coordinator.mu.Unlock()
	if worker == nil || worker.token != workerToken || worker.cancel == nil {
		return false
	}
	worker.cancel()
	return true
}

// RunRecovery 执行一轮批次恢复扫描，并由协调器接管恢复 worker 的生命周期。
func (coordinator *BatchWorkerCoordinator) RunRecovery(ctx context.Context) error {
	if coordinator == nil || coordinator.runner == nil || coordinator.recovery == nil {
		return errors.New("批量 worker 协调器未初始化")
	}
	if ctx == nil {
		return errors.New("批量恢复 Context 不能为空")
	}
	// startWorker 将恢复服务的 worker 启动回调绑定到当前协调器。
	startWorker := func(startCtx context.Context, userID int64, batchID, workerToken string) error {
		return coordinator.Start(startCtx, userID, batchID, workerToken)
	}
	return coordinator.recovery.RecoverWithStarter(ctx, startWorker)
}

// StartRecovery 启动单一批次恢复扫描循环，并纳入协调器 Stop/Wait 生命周期。
func (coordinator *BatchWorkerCoordinator) StartRecovery(parent context.Context) error {
	if coordinator == nil || coordinator.runner == nil || coordinator.recovery == nil {
		return errors.New("批量 worker 协调器未初始化")
	}
	if parent == nil {
		return errors.New("批量恢复生命周期 Context 不能为空")
	}
	coordinator.mu.Lock()
	if coordinator.stopped {
		coordinator.mu.Unlock()
		return ErrBatchWorkerCoordinatorStopped
	}
	if coordinator.recoveryCancel != nil {
		coordinator.mu.Unlock()
		return nil
	}
	// recoveryCtx、cancel 只负责恢复循环本身；Close 时由协调器统一取消。
	recoveryCtx, cancel := context.WithCancel(parent)
	coordinator.recoveryCancel = cancel
	coordinator.beginActiveLocked()
	coordinator.mu.Unlock()
	go coordinator.runRecoveryLoop(recoveryCtx, cancel)
	return nil
}

// Stop 取消全部批次 worker 和恢复扫描循环；方法可安全重复调用。
func (coordinator *BatchWorkerCoordinator) Stop() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	if coordinator.stopped {
		coordinator.mu.Unlock()
		return
	}
	coordinator.stopped = true
	// cancels 保存锁内快照，解锁后调用取消函数，避免取消操作重入生命周期锁。
	cancels := make([]context.CancelFunc, 0, len(coordinator.workers)+1)
	// worker 表示当前仍登记在内存取消表中的批次控制句柄。
	for _, worker := range coordinator.workers {
		if worker != nil && worker.cancel != nil {
			cancels = append(cancels, worker.cancel)
		}
	}
	if coordinator.recoveryCancel != nil {
		cancels = append(cancels, coordinator.recoveryCancel)
	}
	// coordinator.workers 立即清空，保证 Stop 返回后不会继续暴露已停止的取消句柄。
	coordinator.workers = make(map[string]*batchWorkerHandle)
	coordinator.mu.Unlock()
	// cancel 保存当前待触发的取消函数；取消本身不执行平台、数据库或文件 I/O。
	for _, cancel := range cancels {
		cancel()
	}
}

// Wait 等待当前协调器登记的 worker 和恢复扫描循环全部退出。
func (coordinator *BatchWorkerCoordinator) Wait() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	// done 保存当前 active 归零信号；读取后立即解锁，避免阻塞生命周期变更。
	done := coordinator.done
	coordinator.mu.Unlock()
	<-done
}

// Close 停止协调器并在 Context 截止前等待全部后台任务退出。
func (coordinator *BatchWorkerCoordinator) Close(ctx context.Context) error {
	if coordinator == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("关闭批量 worker 协调器的 Context 不能为空")
	}
	coordinator.Stop()
	coordinator.mu.Lock()
	// done 保存 Stop 后仍在退出的 worker 与恢复循环完成信号。
	done := coordinator.done
	coordinator.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runWorker 执行批次发布业务，并在退出时清除租约对应的取消句柄和活动计数。
func (coordinator *BatchWorkerCoordinator) runWorker(ctx context.Context, cancel context.CancelFunc, userID int64, batchID, workerToken string, worker *batchWorkerHandle) {
	defer cancel()
	defer coordinator.finishWorker(batchID, workerToken, worker)
	// err 保存批次发布业务或状态收口错误，交给装配层观测回调。
	err := coordinator.runner.Run(ctx, userID, batchID, workerToken, false)
	if err != nil && coordinator.options.OnWorkerError != nil {
		coordinator.options.OnWorkerError(batchID, err)
	}
}

// runRecoveryLoop 按固定间隔执行批次恢复扫描，直到生命周期 Context 取消。
func (coordinator *BatchWorkerCoordinator) runRecoveryLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	defer coordinator.finishRecovery()
	// err 保存首次恢复扫描错误；报告后继续等待下一轮。
	if err := coordinator.RunRecovery(ctx); err != nil && coordinator.options.OnRecoveryError != nil && !errors.Is(err, context.Canceled) {
		coordinator.options.OnRecoveryError(err)
	}
	// ticker 保存恢复扫描间隔计时器，由当前 goroutine 独占并负责停止。
	ticker := time.NewTicker(coordinator.options.RecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// err 保存本轮恢复扫描错误；单轮失败不阻塞后续轮次。
			if err := coordinator.RunRecovery(ctx); err != nil && coordinator.options.OnRecoveryError != nil && !errors.Is(err, context.Canceled) {
				coordinator.options.OnRecoveryError(err)
			}
		}
	}
}

// beginActiveLocked 登记一个批次 worker 或恢复循环；调用方必须持有 coordinator.mu。
func (coordinator *BatchWorkerCoordinator) beginActiveLocked() {
	if coordinator.active == 0 {
		coordinator.done = make(chan struct{})
	}
	coordinator.active++
}

// finishWorker 清除仍匹配当前租约的 worker 控制句柄并减少活动计数。
func (coordinator *BatchWorkerCoordinator) finishWorker(batchID, workerToken string, worker *batchWorkerHandle) {
	coordinator.mu.Lock()
	// current 保存批次当前登记的控制句柄，用于防止旧 worker 删除新租约。
	current := coordinator.workers[batchID]
	if current == worker && current.token == workerToken {
		delete(coordinator.workers, batchID)
	}
	coordinator.finishActiveLocked()
	coordinator.mu.Unlock()
}

// finishRecovery 清除恢复循环句柄并减少活动计数。
func (coordinator *BatchWorkerCoordinator) finishRecovery() {
	coordinator.mu.Lock()
	coordinator.recoveryCancel = nil
	coordinator.finishActiveLocked()
	coordinator.mu.Unlock()
}

// finishActiveLocked 减少活动计数并在归零时关闭 done；调用方必须持有 coordinator.mu。
func (coordinator *BatchWorkerCoordinator) finishActiveLocked() {
	if coordinator.active <= 0 {
		return
	}
	coordinator.active--
	if coordinator.active == 0 {
		close(coordinator.done)
	}
}

// closedBatchWorkerSignal 创建已关闭的生命周期信号，供没有活动任务的 Wait 立即返回。
func closedBatchWorkerSignal() chan struct{} {
	// done 保存初始无活动任务状态的关闭信号。
	done := make(chan struct{})
	close(done)
	return done
}
