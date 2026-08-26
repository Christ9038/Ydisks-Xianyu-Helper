package items

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// batchRunBranchRepository 是批量发布分支测试使用的可注入仓储替身。
type batchRunBranchRepository struct {
	*batchRunnerRepository
	// pendingErr 保存待处理明细查询错误。
	pendingErr error
	// renewResult 保存续租是否命中当前 worker。
	renewResult *bool
	// renewErr 保存续租错误。
	renewErr error
	// batchErr 保存批次状态读取错误。
	batchErr error
	// claimResult 保存明细租约是否命中。
	claimResult *bool
	// claimErr 保存明细租约抢占错误。
	claimErr error
	// statusResult 保存失败分类读取到的批次状态。
	statusResult string
	// markResult 保存失败明细状态是否写入。
	markResult *bool
	// markErr 保存失败明细状态写入错误。
	markErr error
	// recountErr 保存统计重算错误。
	recountErr error
	// recountHook 在统计重算前触发测试控制，例如取消 worker Context。
	recountHook func()
	// finalizeStatus、finalizeApplied、finalizeErr 保存正常收口结果。
	finalizeStatus  string
	finalizeApplied bool
	finalizeErr     error
	// canceledApplied 保存取消收口结果。
	canceledApplied bool
	// interruptedStatus、interruptedApplied、interruptedErr 保存中断收口结果。
	interruptedStatus  string
	interruptedApplied bool
	interruptedErr     error
	// deleteUploadErr 保存上传目录清理错误。
	deleteUploadErr error
	// reserveResult 保存发布时隙是否预留成功。
	reserveResult *bool
	// reserveErr 保存发布时隙预留错误。
	reserveErr error
}

// PendingRows 返回注入的待处理明细查询结果。
func (repository *batchRunBranchRepository) PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]BatchRow, error) {
	if repository.pendingErr != nil {
		return nil, repository.pendingErr
	}
	return repository.batchRunnerRepository.PendingRows(ctx, batchID, failedOnly)
}

// RenewBatchLease 返回注入的批次续租结果。
func (repository *batchRunBranchRepository) RenewBatchLease(ctx context.Context, batchID, token string, expiresAt int64) (bool, error) {
	if repository.renewErr != nil {
		return false, repository.renewErr
	}
	if repository.renewResult != nil {
		return *repository.renewResult, nil
	}
	return repository.batchRunnerRepository.RenewBatchLease(ctx, batchID, token, expiresAt)
}

// GetBatch 返回注入的批次状态读取结果。
func (repository *batchRunBranchRepository) GetBatch(ctx context.Context, userID int64, batchID string) (BatchInfo, error) {
	if repository.batchErr != nil {
		return BatchInfo{}, repository.batchErr
	}
	return repository.batchRunnerRepository.GetBatch(ctx, userID, batchID)
}

// ClaimRow 返回注入的明细抢占结果。
func (repository *batchRunBranchRepository) ClaimRow(ctx context.Context, rowID int64, token string) (bool, error) {
	if repository.claimErr != nil {
		return false, repository.claimErr
	}
	if repository.claimResult != nil {
		return *repository.claimResult, nil
	}
	return repository.batchRunnerRepository.ClaimRow(ctx, rowID, token)
}

// BatchStatus 返回注入的批次状态。
func (repository *batchRunBranchRepository) BatchStatus(ctx context.Context, batchID string) (string, error) {
	if repository.statusResult != "" {
		return repository.statusResult, nil
	}
	return repository.batchRunnerRepository.BatchStatus(ctx, batchID)
}

// MarkClaimedRowFailed 返回注入的失败明细写入结果。
func (repository *batchRunBranchRepository) MarkClaimedRowFailed(ctx context.Context, rowID int64, token, message, kind string) (bool, error) {
	if repository.markErr != nil {
		return false, repository.markErr
	}
	if repository.markResult != nil {
		return *repository.markResult, nil
	}
	return repository.batchRunnerRepository.MarkClaimedRowFailed(ctx, rowID, token, message, kind)
}

// RecountBatch 返回注入的统计重算错误。
func (repository *batchRunBranchRepository) RecountBatch(ctx context.Context, batchID string) error {
	if repository.recountHook != nil {
		repository.recountHook()
	}
	if repository.recountErr != nil {
		return repository.recountErr
	}
	return repository.batchRunnerRepository.RecountBatch(ctx, batchID)
}

// FinalizeBatch 返回注入的正常收口结果。
func (repository *batchRunBranchRepository) FinalizeBatch(context.Context, string, string) (string, bool, error) {
	if repository.finalizeStatus != "" || repository.finalizeErr != nil {
		return repository.finalizeStatus, repository.finalizeApplied, repository.finalizeErr
	}
	return "completed", true, nil
}

// FinalizeCanceled 返回注入的取消收口结果。
func (repository *batchRunBranchRepository) FinalizeCanceled(context.Context, string, string) (bool, error) {
	return repository.canceledApplied, nil
}

// FinalizeInterrupted 返回注入的中断收口结果。
func (repository *batchRunBranchRepository) FinalizeInterrupted(context.Context, string, string, string) (string, bool, error) {
	return repository.interruptedStatus, repository.interruptedApplied, repository.interruptedErr
}

// DeleteUpload 返回注入的上传目录清理错误。
func (repository *batchRunBranchRepository) DeleteUpload(context.Context, string, string) error {
	return repository.deleteUploadErr
}

// ReservePublishSlot 返回注入的发布时隙预留结果。
func (repository *batchRunBranchRepository) ReservePublishSlot(ctx context.Context, batchID, token string, minimum, started int64) (bool, error) {
	if repository.reserveErr != nil {
		return false, repository.reserveErr
	}
	if repository.reserveResult != nil {
		return *repository.reserveResult, nil
	}
	return repository.batchRunnerRepository.ReservePublishSlot(ctx, batchID, token, minimum, started)
}

// newBranchRunner 构造使用单条明细和零等待策略的批量发布 worker。
func newBranchRunner(repository BatchRepository, publisher BatchPublisher, options BatchRunOptions) (*BatchRunner, error) {
	// runner、err 保存批量 worker 构造结果。
	runner, err := NewBatchRunner(repository, publisher, options)
	return runner, err
}

// TestBatchRunnerCoversRunErrorBranches 覆盖批量发布 worker 的查询、续租、抢占、失败落库和统计错误。
func TestBatchRunnerCoversRunErrorBranches(t *testing.T) {
	// row 保存所有分支复用的批量明细。
	row := BatchRow{ID: 1, BatchID: "batch-1"}
	// batch 保存所有分支复用的运行批次快照。
	batch := BatchInfo{ID: "batch-1", UserID: 7, Status: "running", WorkerToken: "worker"}
	// baseOptions 保存无真实等待的 worker 策略。
	baseOptions := BatchRunOptions{Wait: func(context.Context, time.Duration) error { return nil }}
	// pendingErr 保存待处理明细查询错误。
	pendingErr := errors.New("待处理明细失败")
	// pendingRepository 保存查询错误仓储。
	pendingRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, pendingErr: pendingErr}
	// pendingRunner、err 保存查询错误 worker。
	pendingRunner, err := newBranchRunner(pendingRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造查询错误 worker 失败: %v", err)
	}
	if !errors.Is(pendingRunner.Run(context.Background(), 7, "batch-1", "worker", false), pendingErr) {
		t.Fatal("待处理明细错误未返回")
	}

	// renewErr 保存续租错误。
	renewErr := errors.New("续租失败")
	// renewRepository 保存续租错误仓储。
	renewRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, renewErr: renewErr}
	// renewRunner 保存续租错误 worker。
	renewRunner, err := newBranchRunner(renewRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造续租错误 worker 失败: %v", err)
	}
	if !errors.Is(renewRunner.Run(context.Background(), 7, "batch-1", "worker", false), renewErr) {
		t.Fatal("续租错误未返回")
	}

	// lostRepository 保存批次读取错误仓储。
	lostRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, batchErr: errors.New("批次读取失败")}
	// lostRunner 保存批次租约丢失 worker。
	lostRunner, err := newBranchRunner(lostRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造批次读取错误 worker 失败: %v", err)
	}
	if !errors.Is(lostRunner.Run(context.Background(), 7, "batch-1", "worker", false), ErrBatchLeaseLost) {
		t.Fatal("批次读取错误未转换为租约丢失")
	}

	// claimErr 保存明细抢占错误。
	claimErr := errors.New("明细抢占失败")
	// claimRepository 保存明细抢占错误仓储。
	claimRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, claimErr: claimErr}
	// claimRunner 保存明细抢占错误 worker。
	claimRunner, err := newBranchRunner(claimRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造明细抢占错误 worker 失败: %v", err)
	}
	if !errors.Is(claimRunner.Run(context.Background(), 7, "batch-1", "worker", false), claimErr) {
		t.Fatal("明细抢占错误未返回")
	}

	// markErr 保存失败状态写入错误。
	markErr := errors.New("失败状态写入失败")
	// markRepository 保存发布失败后的状态写入错误仓储。
	markRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, markErr: markErr}
	// markRunner 保存发布失败状态错误 worker。
	markRunner, err := newBranchRunner(markRepository, &batchRunnerPublisher{err: errors.New("平台发布失败")}, baseOptions)
	if err != nil {
		t.Fatalf("构造失败状态错误 worker 失败: %v", err)
	}
	// err 保存失败状态写入错误分支结果。
	if err := markRunner.Run(context.Background(), 7, "batch-1", "worker", false); err == nil || !strings.Contains(err.Error(), "保存批量发布失败状态失败") {
		t.Fatalf("失败状态写入错误异常: %v", err)
	}

	// recountErr 保存统计重算错误。
	recountErr := errors.New("统计重算失败")
	// recountRepository 保存统计重算错误仓储。
	recountRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, recountErr: recountErr}
	// recountRunner 保存统计重算错误 worker。
	recountRunner, err := newBranchRunner(recountRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造统计重算错误 worker 失败: %v", err)
	}
	if !errors.Is(recountRunner.Run(context.Background(), 7, "batch-1", "worker", false), recountErr) {
		t.Fatal("统计重算错误未返回")
	}

	// canceledPending 保存查询阶段已经取消的 worker Context。
	canceledPending, cancelPending := context.WithCancel(context.Background())
	cancelPending()
	// canceledPendingRepository 保存查询错误和取消收口分支的仓储。
	canceledPendingRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{batch: batch}, pendingErr: pendingErr, interruptedApplied: true}
	// canceledPendingRunner 保存查询阶段被取消的 worker。
	canceledPendingRunner, err := newBranchRunner(canceledPendingRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造取消查询 worker 失败: %v", err)
	}
	if !errors.Is(canceledPendingRunner.Run(canceledPending, 7, "batch-1", "worker", false), pendingErr) || !canceledPendingRepository.interruptedApplied {
		t.Fatal("取消查询未执行中断收口")
	}

	// claimFalse 保存明细已被其他 worker 抢占的结果。
	claimFalse := false
	// claimFalseRepository 保存抢占失败但不报错的仓储。
	claimFalseRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, claimResult: &claimFalse}
	// claimFalseRunner 保存跳过已被其他 worker 抢占明细的 worker。
	claimFalseRunner, err := newBranchRunner(claimFalseRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造跳过明细 worker 失败: %v", err)
	}
	// err 保存跳过已抢占明细后的 worker 结果。
	if err := claimFalseRunner.Run(context.Background(), 7, "batch-1", "worker", false); err != nil {
		t.Fatalf("跳过已抢占明细失败: %v", err)
	}

	// renewFalse 保存租约续期未命中的结果。
	renewFalse := false
	// renewFalseRepository 保存租约失效的仓储。
	renewFalseRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, renewResult: &renewFalse}
	// renewFalseRunner 保存租约失效 worker。
	renewFalseRunner, err := newBranchRunner(renewFalseRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造租约失效 worker 失败: %v", err)
	}
	if !errors.Is(renewFalseRunner.Run(context.Background(), 7, "batch-1", "worker", false), ErrBatchLeaseLost) {
		t.Fatal("租约未命中未转换为租约丢失")
	}

	// beforePublishPublisher 保存会调用发布前时隙预留回调的发布替身。
	beforePublishPublisher := &batchRunnerBeforePublishPublisher{}
	// beforePublishRepository 保存发布前时隙预留使用的批次仓储。
	beforePublishRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}}
	// beforePublishRunner 保存覆盖发布前回调的 worker。
	beforePublishRunner, err := newBranchRunner(beforePublishRepository, beforePublishPublisher, baseOptions)
	if err != nil {
		t.Fatalf("构造发布前回调 worker 失败: %v", err)
	}
	// err 保存执行发布前回调后的 worker 结果。
	if err := beforePublishRunner.Run(context.Background(), 7, "batch-1", "worker", false); err != nil || !beforePublishPublisher.called {
		t.Fatalf("发布前回调未执行: called=%v err=%v", beforePublishPublisher.called, err)
	}

	// canceledRecount、cancelRecount 保存统计重算阶段取消控制。
	canceledRecount, cancelRecount := context.WithCancel(context.Background())
	// canceledRecountRepository 保存统计重算错误和取消回调的仓储。
	canceledRecountRepository := &batchRunBranchRepository{batchRunnerRepository: &batchRunnerRepository{rows: []BatchRow{row}, batch: batch}, recountErr: recountErr, recountHook: cancelRecount, interruptedApplied: true}
	// canceledRecountRunner 保存统计重算阶段被取消的 worker。
	canceledRecountRunner, err := newBranchRunner(canceledRecountRepository, &batchRunnerPublisher{}, baseOptions)
	if err != nil {
		t.Fatalf("构造取消统计 worker 失败: %v", err)
	}
	if !errors.Is(canceledRecountRunner.Run(canceledRecount, 7, "batch-1", "worker", false), recountErr) || !canceledRecountRepository.interruptedApplied {
		t.Fatal("取消统计未执行中断收口")
	}
}

// batchRunnerBeforePublishPublisher 验证 BatchRunner 向平台发布器传递的时隙预留回调。
type batchRunnerBeforePublishPublisher struct {
	// called 表示发布器是否执行了发布前回调。
	called bool
}

// PublishRow 执行发布前回调并模拟平台发布成功。
func (publisher *batchRunnerBeforePublishPublisher) PublishRow(ctx context.Context, _ int64, _ BatchRow, _ string, beforePublish func(context.Context) error) error {
	publisher.called = true
	return beforePublish(ctx)
}
