package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// batchRunnerSlotRepository 为发布时隙测试覆盖原子抢占成功、等待和租约失效分支。
type batchRunnerSlotRepository struct {
	// batchRunnerRepository 提供批次 worker 所需的其余仓储能力。
	*batchRunnerRepository
	// reserved 保存连续时隙抢占结果。
	reserved []bool
	// reserveErr 保存时隙抢占错误。
	reserveErr error
	// reserveCalls 统计时隙抢占次数。
	reserveCalls int
	// getErr 保存时隙等待期间的批次查询错误。
	getErr error
}

// ReservePublishSlot 返回预设的时隙抢占结果。
func (r *batchRunnerSlotRepository) ReservePublishSlot(context.Context, string, string, int64, int64) (bool, error) {
	r.reserveCalls++
	if r.reserveErr != nil {
		return false, r.reserveErr
	}
	if len(r.reserved) == 0 {
		return true, nil
	}
	// result 保存本次原子时隙抢占的预设结果。
	result := r.reserved[0]
	r.reserved = r.reserved[1:]
	return result, nil
}

// GetBatch 返回时隙等待期间的批次快照或预设错误。
func (r *batchRunnerSlotRepository) GetBatch(ctx context.Context, userID int64, batchID string) (BatchInfo, error) {
	if r.getErr != nil {
		return BatchInfo{}, r.getErr
	}
	return r.batchRunnerRepository.GetBatch(ctx, userID, batchID)
}

// TestBatchRunnerReservePublishSlotBranches 验证发布时隙原子抢占的重试、租约失效和错误分支。
func TestBatchRunnerReservePublishSlotBranches(t *testing.T) {
	// runnerTime 是时隙测试使用的固定时间源。
	runnerTime := time.UnixMilli(1_800_000_000_000)
	// retryRepository 保存先失败后成功的时隙结果。
	retryRepository := &batchRunnerSlotRepository{batchRunnerRepository: &batchRunnerRepository{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}, reserved: []bool{false, true}}
	// retryRunner 是使用可立即返回等待的批次 worker。
	retryRunner, err := NewBatchRunner(retryRepository, &batchRunnerPublisher{}, BatchRunOptions{Now: func() time.Time { return runnerTime }, Wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	// retryErr 保存时隙重试结果。
	retryErr := retryRunner.reservePublishSlot(context.Background(), 1, "batch", "worker", 1)
	if retryErr != nil || retryRepository.reserveCalls != 2 {
		t.Fatalf("时隙重试错误=%v calls=%d", retryErr, retryRepository.reserveCalls)
	}
	// leaseRepository 保存等待期间失去批次租约的结果。
	leaseRepository := &batchRunnerSlotRepository{batchRunnerRepository: &batchRunnerRepository{batch: BatchInfo{Status: "canceling", WorkerToken: "other"}}, reserved: []bool{false}}
	// leaseRunner 是租约失效场景的批次 worker。
	leaseRunner, err := NewBatchRunner(leaseRepository, &batchRunnerPublisher{}, BatchRunOptions{Now: func() time.Time { return runnerTime }, Wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	// leaseErr 保存租约失效错误。
	leaseErr := leaseRunner.reservePublishSlot(context.Background(), 1, "batch", "worker", 1)
	if !errors.Is(leaseErr, ErrBatchLeaseLost) {
		t.Fatalf("时隙租约错误=%v", leaseErr)
	}
	// reserveError 是原子时隙持久化错误。
	reserveError := errors.New("reserve failed")
	// errorRepository 保存时隙持久化错误场景。
	errorRepository := &batchRunnerSlotRepository{batchRunnerRepository: &batchRunnerRepository{}, reserveErr: reserveError}
	// errorRunner 是时隙持久化错误场景的批次 worker。
	errorRunner, err := NewBatchRunner(errorRepository, &batchRunnerPublisher{}, BatchRunOptions{Now: func() time.Time { return runnerTime }})
	if err != nil {
		t.Fatal(err)
	}
	// reserveResultErr 保存时隙持久化错误。
	reserveResultErr := errorRunner.reservePublishSlot(context.Background(), 1, "batch", "worker", 1)
	if !errors.Is(reserveResultErr, reserveError) {
		t.Fatalf("时隙持久化错误=%v", reserveResultErr)
	}
	// waitError 是时隙等待函数返回的取消错误。
	waitError := errors.New("wait canceled")
	// waitRepository 保存等待分支的时隙结果。
	waitRepository := &batchRunnerSlotRepository{batchRunnerRepository: &batchRunnerRepository{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}, reserved: []bool{false}}
	// waitRunner 是等待函数错误场景的批次 worker。
	waitRunner, err := NewBatchRunner(waitRepository, &batchRunnerPublisher{}, BatchRunOptions{Now: func() time.Time { return runnerTime }, Wait: func(context.Context, time.Duration) error { return waitError }})
	if err != nil {
		t.Fatal(err)
	}
	// waitResultErr 保存等待函数错误。
	waitResultErr := waitRunner.reservePublishSlot(context.Background(), 1, "batch", "worker", 1)
	if !errors.Is(waitResultErr, waitError) {
		t.Fatalf("时隙等待错误=%v", waitResultErr)
	}
}

// TestBatchRunnerConstructionGuards 验证批次 worker 构造器和失败分类默认端口的边界。
func TestBatchRunnerConstructionGuards(t *testing.T) {
	// missingRepositoryErr 保存缺失批次仓储的构造错误。
	_, missingRepositoryErr := NewBatchRunner(nil, &batchRunnerPublisher{}, BatchRunOptions{})
	if missingRepositoryErr == nil {
		t.Fatal("缺失仓储应构造失败")
	}
	// missingPublisherErr 保存缺失平台发布端口的构造错误。
	_, missingPublisherErr := NewBatchRunner(&batchRunnerRepository{}, nil, BatchRunOptions{})
	if missingPublisherErr == nil {
		t.Fatal("缺失平台端口应构造失败")
	}
	// runner 是完整依赖和默认策略构造出的批次 worker。
	runner, err := NewBatchRunner(&batchRunnerRepository{}, &batchRunnerPublisher{}, BatchRunOptions{})
	if err != nil || runner.options.Wait == nil || runner.options.Now == nil || runner.options.ClassifyFailure == nil || runner.options.IsSessionExpired == nil {
		t.Fatalf("默认策略构造异常: runner=%+v err=%v", runner, err)
	}
}
