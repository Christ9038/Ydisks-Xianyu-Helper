package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// coordinatorErrorBatchRepository 让批次 worker 在读取批次快照阶段返回确定错误。
type coordinatorErrorBatchRepository struct {
	// batchRunnerRepository 提供其余批次仓储端口的默认行为。
	*batchRunnerRepository
	// getErr 保存批次快照读取错误。
	getErr error
}

// GetBatch 返回预置错误，直接触发协调器的异步错误观测路径。
func (repository *coordinatorErrorBatchRepository) GetBatch(context.Context, int64, string) (BatchInfo, error) {
	return BatchInfo{}, repository.getErr
}

// TestBatchWorkerCoordinatorReportsAsyncWorkerError 验证异步批次 worker 的业务错误会通过观测回调传递。
func TestBatchWorkerCoordinatorReportsAsyncWorkerError(t *testing.T) {
	// workerError 是平台发布失败时应交给观测回调的底层错误。
	workerError := errors.New("worker publish failed")
	// repository 保存异步 worker 所需的单批次测试状态。
	repository := &coordinatorErrorBatchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{{ID: 1, BatchID: "batch-error"}}}, getErr: workerError}
	// publisher 返回预置的平台发布失败。
	publisher := &batchRunnerPublisher{err: workerError}
	// runner 保存批次执行器及其零等待策略。
	runner, runnerErr := NewBatchRunner(repository, publisher, BatchRunOptions{Wait: func(context.Context, time.Duration) error { return nil }})
	if runnerErr != nil {
		t.Fatal(runnerErr)
	}
	// recovery 保存最小恢复服务，协调器需要该依赖才能启动 worker。
	recovery, recoveryErr := NewBatchRecoveryService(&batchRecoveryRepositoryFake{}, BatchRecoveryOptions{StartWorker: func(context.Context, int64, string, string) {}})
	if recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	// observed 保存异步 worker 回调收到的错误。
	observed := make(chan error, 1)
	// coordinator 保存配置错误观测回调的批次协调器。
	coordinator, coordinatorErr := NewBatchWorkerCoordinator(runner, recovery, BatchWorkerCoordinatorOptions{
		WorkerTimeout: time.Second,
		OnWorkerError: func(string, error) { observed <- workerError },
	})
	if coordinatorErr != nil {
		t.Fatal(coordinatorErr)
	}
	// startErr 保存异步 worker 启动登记错误。
	if startErr := coordinator.Start(context.Background(), 7, "batch-error", "token"); startErr != nil {
		t.Fatal(startErr)
	}
	select {
	// observedErr 保存异步 worker 错误观测回调收到的错误。
	case observedErr := <-observed:
		if !errors.Is(observedErr, workerError) {
			t.Fatalf("异步 worker 错误=%v", observedErr)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到异步 worker 错误回调")
	}
	// closeErr 保存 worker 错误回调完成后的协调器关闭结果。
	closeErr := coordinator.Close(context.Background())
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
