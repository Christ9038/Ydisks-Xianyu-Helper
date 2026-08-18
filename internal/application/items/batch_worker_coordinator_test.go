package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// batchCoordinatorBlockingPublisher 在收到 worker Context 后等待取消，用于验证协调器取消传播。
type batchCoordinatorBlockingPublisher struct {
	// started 通知测试 worker 已经进入平台发布阶段。
	started chan struct{}
}

// PublishRow 等待 Context 取消并返回取消错误，模拟不可控外部发布调用。
func (publisher *batchCoordinatorBlockingPublisher) PublishRow(ctx context.Context, _ int64, _ BatchRow, _ string, _ func(context.Context) error) error {
	select {
	case publisher.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// batchCoordinatorEmptyPublisher 立即返回成功，用于恢复扫描的生命周期收口。
type batchCoordinatorEmptyPublisher struct{}

// PublishRow 返回成功，避免测试等待真实平台调用。
func (batchCoordinatorEmptyPublisher) PublishRow(context.Context, int64, BatchRow, string, func(context.Context) error) error {
	return nil
}

// newBatchWorkerCoordinatorFixture 创建批次协调器测试共用的 runner 和仓储替身。
func newBatchWorkerCoordinatorFixture(t *testing.T, publisher BatchPublisher) (*BatchWorkerCoordinator, *batchRunnerRepository) {
	// repository 保存一个正在运行且包含一条待发布明细的批次。
	repository := &batchRunnerRepository{rows: []BatchRow{{ID: 1, BatchID: "batch-1"}}, batch: BatchInfo{ID: "batch-1", UserID: 7, Status: "running", WorkerToken: "token-1"}}
	// runner、err 保存应用层批次 worker。
	runner, err := NewBatchRunner(repository, publisher, BatchRunOptions{Wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatalf("构造批次 runner: %v", err)
	}
	// recovery、recoveryErr 保存最小恢复服务；协调器通过 RecoverWithStarter 注入自身启动回调。
	recovery, recoveryErr := NewBatchRecoveryService(&batchRecoveryRepositoryFake{}, BatchRecoveryOptions{StartWorker: func(context.Context, int64, string, string) {}})
	if recoveryErr != nil {
		t.Fatalf("构造批次恢复服务: %v", recoveryErr)
	}
	// coordinator、coordinatorErr 保存批次 worker 生命周期协调器。
	coordinator, coordinatorErr := NewBatchWorkerCoordinator(runner, recovery, BatchWorkerCoordinatorOptions{WorkerTimeout: time.Minute})
	if coordinatorErr != nil {
		t.Fatalf("构造批次协调器: %v", coordinatorErr)
	}
	return coordinator, repository
}

// TestBatchWorkerCoordinatorCancelClose 验证 worker 取消表、超时 Context 和 Close/Wait 生命周期。
func TestBatchWorkerCoordinatorCancelClose(t *testing.T) {
	// publisher 等待协调器取消，以证明后台 worker 没有脱离 Context。
	publisher := &batchCoordinatorBlockingPublisher{started: make(chan struct{}, 1)}
	// coordinator、repository 保存生命周期测试对象。
	coordinator, repository := newBatchWorkerCoordinatorFixture(t, publisher)
	// ctx 是协调器 worker 的应用生命周期 Context。
	ctx := context.Background()
	// err 保存 worker 启动阶段的生命周期校验错误。
	if err := coordinator.Start(ctx, 7, "batch-1", "token-1"); err != nil {
		t.Fatalf("启动批次 worker: %v", err)
	}
	select {
	case <-publisher.started:
	case <-time.After(time.Second):
		t.Fatal("批次 worker 未进入发布阶段")
	}
	if coordinator.Cancel("batch-1", "stale-token") {
		t.Fatal("旧租约令牌不应取消当前 worker")
	}
	if !coordinator.Cancel("batch-1", "token-1") {
		t.Fatal("当前租约令牌未取消 worker")
	}
	// closeCtx 限制关闭等待，防止 worker 生命周期回归测试无限阻塞。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// err 保存批次协调器关闭错误。
	if err := coordinator.Close(closeCtx); err != nil {
		t.Fatalf("关闭批次协调器: %v", err)
	}
	coordinator.Wait()
	if coordinator.Cancel("batch-1", "token-1") {
		t.Fatal("worker 退出后不应仍可取消")
	}
	if repository.finalized == 0 {
		t.Fatal("取消 worker 未执行批次中断收口")
	}
}

// TestBatchWorkerCoordinatorTokenFencing 验证同一批次的新租约会取消旧 worker，旧 token 不能取消新 worker。
func TestBatchWorkerCoordinatorTokenFencing(t *testing.T) {
	// publisher 等待 Context 取消，方便观察旧 worker 是否被 fencing。
	publisher := &batchCoordinatorBlockingPublisher{started: make(chan struct{}, 4)}
	// coordinator、repository 保存 token fencing 测试对象。
	coordinator, _ := newBatchWorkerCoordinatorFixture(t, publisher)
	// err 保存旧 worker 启动阶段的错误。
	if err := coordinator.Start(context.Background(), 7, "batch-1", "token-old"); err != nil {
		t.Fatalf("启动旧 worker: %v", err)
	}
	select {
	case <-publisher.started:
	case <-time.After(time.Second):
		t.Fatal("旧 worker 未进入发布阶段")
	}
	// err 保存新 worker 替换旧 worker 时的错误。
	if err := coordinator.Start(context.Background(), 7, "batch-1", "token-new"); err != nil {
		t.Fatalf("启动新 worker: %v", err)
	}
	if coordinator.Cancel("batch-1", "token-old") {
		t.Fatal("旧 token 不应取消新 worker")
	}
	if !coordinator.Cancel("batch-1", "token-new") {
		t.Fatal("新 token 未取消当前 worker")
	}
	// closeCtx 限制 fencing worker 的退出等待时间。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// err 保存 fencing 协调器关闭错误。
	if err := coordinator.Close(closeCtx); err != nil {
		t.Fatalf("关闭 fencing 协调器: %v", err)
	}
}

// TestBatchWorkerCoordinatorRecoveryLifecycle 验证恢复扫描由协调器启动、观测错误并纳入 Close 等待。
func TestBatchWorkerCoordinatorRecoveryLifecycle(t *testing.T) {
	// scanErr 模拟恢复扫描数据库错误。
	scanErr := errors.New("恢复扫描失败")
	// recoveryRepository 保存错误配置和恢复扫描端口。
	recoveryRepository := &batchRecoveryRepositoryFake{scanErr: scanErr}
	// recovery、err 保存恢复服务。
	recovery, err := NewBatchRecoveryService(recoveryRepository, BatchRecoveryOptions{StartWorker: func(context.Context, int64, string, string) {}})
	if err != nil {
		t.Fatalf("构造恢复服务: %v", err)
	}
	// runner、runnerErr 保存不执行商品行的最小 worker。
	runner, runnerErr := NewBatchRunner(&batchRunnerRepository{}, batchCoordinatorEmptyPublisher{}, BatchRunOptions{})
	if runnerErr != nil {
		t.Fatalf("构造 runner: %v", runnerErr)
	}
	// observed 通知测试已收到恢复错误回调。
	observed := make(chan error, 1)
	// coordinator、coordinatorErr 保存恢复生命周期协调器。
	coordinator, coordinatorErr := NewBatchWorkerCoordinator(runner, recovery, BatchWorkerCoordinatorOptions{RecoveryInterval: time.Hour, OnRecoveryError: func(err error) { observed <- err }})
	if coordinatorErr != nil {
		t.Fatalf("构造协调器: %v", coordinatorErr)
	}
	// err 保存恢复循环启动错误。
	if err := coordinator.StartRecovery(context.Background()); err != nil {
		t.Fatalf("启动恢复循环: %v", err)
	}
	select {
	// observedErr 保存恢复错误回调传递的基础设施错误。
	case observedErr := <-observed:
		if !errors.Is(observedErr, scanErr) {
			t.Fatalf("恢复错误不匹配: %v", observedErr)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到恢复错误回调")
	}
	// closeCtx 限制恢复循环关闭等待时间。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// err 保存恢复协调器关闭错误。
	if err := coordinator.Close(closeCtx); err != nil {
		t.Fatalf("关闭恢复协调器: %v", err)
	}
}

// TestBatchWorkerCoordinatorRejectsAfterStop 验证 Stop 后不再接受 worker 或恢复扫描。
func TestBatchWorkerCoordinatorRejectsAfterStop(t *testing.T) {
	// coordinator 保存最小生命周期协调器。
	coordinator, _ := newBatchWorkerCoordinatorFixture(t, batchCoordinatorEmptyPublisher{})
	coordinator.Stop()
	// err 保存 Stop 后启动 worker 的错误。
	if err := coordinator.Start(context.Background(), 7, "batch-1", "token-1"); !errors.Is(err, ErrBatchWorkerCoordinatorStopped) {
		t.Fatalf("Stop 后仍接受 worker: %v", err)
	}
	// err 保存 Stop 后启动恢复循环的错误。
	if err := coordinator.StartRecovery(context.Background()); !errors.Is(err, ErrBatchWorkerCoordinatorStopped) {
		t.Fatalf("Stop 后仍接受恢复循环: %v", err)
	}
}
