package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// batchFinishCoverageRepository 记录批次最终收口和上传文件清理调用。
type batchFinishCoverageRepository struct {
	*batchRunBranchRepository
	// canceledCalls 记录取消状态收口次数。
	canceledCalls int
	// interruptedCalls 记录中断状态收口次数。
	interruptedCalls int
	// deleteCalls 记录上传目录清理次数。
	deleteCalls int
}

// FinalizeCanceled 记录取消状态收口调用并返回预置结果。
func (repository *batchFinishCoverageRepository) FinalizeCanceled(context.Context, string, string) (bool, error) {
	repository.canceledCalls++
	return repository.canceledApplied, nil
}

// FinalizeInterrupted 记录中断状态收口调用并返回预置结果。
func (repository *batchFinishCoverageRepository) FinalizeInterrupted(context.Context, string, string, string) (string, bool, error) {
	repository.interruptedCalls++
	return repository.interruptedStatus, repository.interruptedApplied, repository.interruptedErr
}

// DeleteUpload 记录上传目录清理调用并返回预置错误。
func (repository *batchFinishCoverageRepository) DeleteUpload(context.Context, string, string) error {
	repository.deleteCalls++
	return repository.deleteUploadErr
}

// TestBatchRunnerFinishBoundaries 覆盖批次正常、中断、取消和租约失效的收口分支。
func TestBatchRunnerFinishBoundaries(t *testing.T) {
	// baseBatch 保存所有收口样例共用的批次快照。
	baseBatch := BatchInfo{ID: "batch-finish", UserID: 7, Status: "running", WorkerToken: "worker", UploadDir: "uploads"}
	// baseOptions 保存无实际等待的 worker 配置。
	baseOptions := BatchRunOptions{Wait: func(context.Context, time.Duration) error { return nil }}
	// newRunner 构造指定仓储的批次 worker。
	newRunner := func(repository BatchRepository) *BatchRunner {
		// runner、err 保存批次 worker 的构造结果。
		runner, err := NewBatchRunner(repository, &batchRunnerPublisher{}, baseOptions)
		if err != nil {
			t.Fatalf("构造 worker 失败: %v", err)
		}
		return runner
	}
	// errorRepository 保存批次读取错误样例。
	errorRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: baseBatch}, batchErr: errors.New("batch read")}}
	newRunner(errorRepository).finish(context.Background(), 7, baseBatch.ID, "worker")
	if errorRepository.finalized != 0 || errorRepository.deleteCalls != 0 {
		t.Fatal("读取批次失败后不应继续收口")
	}
	newRunner(errorRepository).finishInterrupted(context.Background(), 7, baseBatch.ID, "worker")
	if errorRepository.interruptedCalls != 0 {
		t.Fatal("读取批次失败后不应中断收口")
	}
	// completedRepository 保存正常完成收口样例。
	completedRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: baseBatch}}}
	newRunner(completedRepository).finish(context.Background(), 7, baseBatch.ID, "worker")
	if completedRepository.deleteCalls != 1 {
		t.Fatalf("completed finish delete=%d", completedRepository.deleteCalls)
	}
	// finalizeErr 保存正常终态写入失败样例的基础设施错误。
	finalizeErr := errors.New("finalize failed")
	// finalizeErrorRepository 保存终态失败后可成功执行中断补偿的仓储。
	finalizeErrorRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: baseBatch}, finalizeErr: finalizeErr, interruptedApplied: true}}
	// finalizeResultErr 保存终态失败和补偿结果，原始错误必须保留在错误链中。
	finalizeResultErr := newRunner(finalizeErrorRepository).finish(context.Background(), 7, baseBatch.ID, "worker")
	if !errors.Is(finalizeResultErr, finalizeErr) || finalizeErrorRepository.interruptedCalls != 1 {
		t.Fatalf("finalize error=%v interrupted=%d", finalizeResultErr, finalizeErrorRepository.interruptedCalls)
	}
	// wrongTokenRepository 保存租约令牌不匹配的完成样例仓储。
	wrongTokenRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: baseBatch}}}
	newRunner(wrongTokenRepository).finish(context.Background(), 7, baseBatch.ID, "other-worker")
	if wrongTokenRepository.deleteCalls != 0 {
		t.Fatal("旧 worker 不应清理上传目录")
	}
	// cancelingRepository 保存二阶段取消收口样例。
	cancelingBatch := baseBatch
	cancelingBatch.Status = "canceling"
	// cancelingRepository 保存二阶段取消收口样例仓储。
	cancelingRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: cancelingBatch}, canceledApplied: true}}
	newRunner(cancelingRepository).finish(context.Background(), 7, baseBatch.ID, "worker")
	if cancelingRepository.canceledCalls != 1 {
		t.Fatal("canceling batch must finalize cancellation")
	}
	// interruptedRepository 保存中断收口样例。
	interruptedRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: baseBatch}, interruptedStatus: "failed", interruptedApplied: true}}
	newRunner(interruptedRepository).finishInterrupted(context.Background(), 7, baseBatch.ID, "worker")
	if interruptedRepository.interruptedCalls != 1 {
		t.Fatal("interrupted batch must finalize interruption")
	}
	// cancelingInterruptedRepository 保存中断时发现二阶段取消的批次仓储。
	cancelingInterruptedBatch := baseBatch
	cancelingInterruptedBatch.Status = "canceling"
	// cancelingInterruptedRepository 保存二阶段取消中断收口样例仓储。
	cancelingInterruptedRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: cancelingInterruptedBatch}, canceledApplied: true}}
	newRunner(cancelingInterruptedRepository).finishInterrupted(context.Background(), 7, baseBatch.ID, "worker")
	if cancelingInterruptedRepository.canceledCalls != 1 || cancelingInterruptedRepository.interruptedCalls != 0 {
		t.Fatal("canceling interruption must finalize cancellation")
	}
	// canceledRepository 保存已经终态取消的批次样例。
	canceledBatch := baseBatch
	canceledBatch.Status = "canceled"
	// canceledRepository 保存已经终态取消的批次样例仓储。
	canceledRepository := &batchFinishCoverageRepository{batchRunBranchRepository: &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: canceledBatch}}}
	newRunner(canceledRepository).finishInterrupted(context.Background(), 7, baseBatch.ID, "worker")
	if canceledRepository.interruptedCalls != 0 || canceledRepository.canceledCalls != 0 {
		t.Fatal("canceled batch must not be finalized again")
	}
}
