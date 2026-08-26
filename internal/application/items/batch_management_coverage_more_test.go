package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBatchManagementCoversRemainingQueryAndDeleteBranches 验证批次管理查询、删除和租约释放的剩余确定性分支。
func TestBatchManagementCoversRemainingQueryAndDeleteBranches(t *testing.T) {
	// repository 保存取消状态和租约释放错误的测试替身。
	repository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "batch", Status: "canceling"}, failClaimErr: errors.New("release failed")}
	// service 保存完整依赖构造出的批次管理服务。
	service := newBatchManagementServiceForTest(t, repository, &batchManagementRuntimeFake{})
	// deleteErr 保存取消中的批次删除结果。
	deleteErr := service.DeleteBatch(context.Background(), 1, "batch")
	if !errors.Is(deleteErr, ErrBatchConflict) {
		t.Fatalf("canceling delete error=%v", deleteErr)
	}
	// releaseErr 保存租约释放端口返回的底层错误。
	releaseErr := service.releaseClaim(context.Background(), "batch", "worker")
	if !errors.Is(releaseErr, repository.failClaimErr) || !repository.released {
		t.Fatalf("release error=%v released=%v", releaseErr, repository.released)
	}
	// invalidReleaseErr 保存缺少仓储的内部补偿错误。
	invalidReleaseErr := (&BatchManagementService{}).releaseClaim(context.Background(), "batch", "worker")
	if invalidReleaseErr == nil {
		t.Fatal("未装配服务的租约释放不应成功")
	}
	// failClaimResult、failClaimErr 保存公开租约释放入口的仓储结果。
	failClaimResult, failClaimErr := service.FailClaimedBatch(context.Background(), "batch", "worker")
	if failClaimResult || !errors.Is(failClaimErr, repository.failClaimErr) {
		t.Fatalf("公开租约释放结果=%v 错误=%v", failClaimResult, failClaimErr)
	}
}

// TestBatchManagementCoversCleanupDefaultAndContextBranches 验证清理任务使用默认参数并在循环前后响应取消。
func TestBatchManagementCoversCleanupDefaultAndContextBranches(t *testing.T) {
	// repository 保存空清理结果，便于验证零值时间和限制的默认转换。
	repository := &batchManagementRepositoryFake{}
	// service 保存固定时间和令牌源的批次管理服务。
	service := newBatchManagementServiceForTest(t, repository, &batchManagementRuntimeFake{})
	// cleanupErr 保存默认参数清理结果。
	cleanupErr := service.CleanupExpiredUploads(context.Background(), time.Time{}, 0)
	if cleanupErr != nil {
		t.Fatalf("默认清理参数错误=%v", cleanupErr)
	}
	// canceledContext 保存在查询成功后取消的上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// repository.expiredUploads 让清理循环实际检查上下文状态。
	repository.expiredUploads = []BatchInfo{{ID: "expired"}}
	// canceledErr 保存循环检测到的取消错误。
	canceledErr := service.CleanupExpiredUploads(canceledContext, time.Unix(200, 0), 1)
	if !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("循环取消错误=%v", canceledErr)
	}
}

// TestBatchManagementCoversStartAndQueryFailures 验证批次启动和明细查询的补偿错误路径。
func TestBatchManagementCoversStartAndQueryFailures(t *testing.T) {
	// invalidService 保存未装配仓储的启动服务。
	invalidService := &BatchManagementService{}
	// invalidErr 保存未装配服务的稳定错误。
	_, invalidErr := invalidService.StartBatch(context.Background(), 1, "batch", time.Second)
	if !errors.Is(invalidErr, ErrBatchNotFound) {
		t.Fatalf("未装配启动服务错误=%v", invalidErr)
	}
	// invalidStateRepository 保存不允许启动的批次状态。
	invalidStateRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "invalid", Status: "failed"}}
	// invalidStateService 保存非法状态测试服务。
	invalidStateService := newBatchManagementServiceForTest(t, invalidStateRepository, &batchManagementRuntimeFake{})
	// invalidStateErr 保存非法状态错误。
	_, invalidStateErr := invalidStateService.StartBatch(context.Background(), 1, "invalid", time.Second)
	if !errors.Is(invalidStateErr, ErrBatchInvalidState) {
		t.Fatalf("非法状态错误=%v", invalidStateErr)
	}
	// claimErr 是租约声明失败的底层错误。
	claimErr := errors.New("claim failed")
	// claimRepository 保存租约声明失败的仓储替身。
	claimRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "claim", Status: "preview"}, claimErr: claimErr}
	// claimService 保存租约声明失败测试服务。
	claimService := newBatchManagementServiceForTest(t, claimRepository, &batchManagementRuntimeFake{})
	// claimResultErr 保存租约声明失败结果。
	_, claimResultErr := claimService.StartBatch(context.Background(), 1, "claim", time.Second)
	if !errors.Is(claimResultErr, claimErr) {
		t.Fatalf("租约声明错误=%v", claimResultErr)
	}
	// notClaimedRepository 保存数据库拒绝租约声明的仓储替身。
	notClaimedRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "not-claimed", Status: "preview"}}
	// notClaimedService 保存未抢占测试服务。
	notClaimedService := newBatchManagementServiceForTest(t, notClaimedRepository, &batchManagementRuntimeFake{})
	// notClaimedErr 保存未抢占的冲突错误。
	_, notClaimedErr := notClaimedService.StartBatch(context.Background(), 1, "not-claimed", time.Second)
	if !errors.Is(notClaimedErr, ErrBatchConflict) {
		t.Fatalf("未抢占错误=%v", notClaimedErr)
	}
	// pendingErr 是待处理明细读取失败的底层错误。
	pendingErr := errors.New("pending failed")
	// pendingRepository 保存待处理明细失败且可释放租约的仓储替身。
	pendingRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "pending", Status: "preview"}, claimed: true, pendingErr: pendingErr}
	// pendingService 保存待处理明细失败测试服务。
	pendingService := newBatchManagementServiceForTest(t, pendingRepository, &batchManagementRuntimeFake{})
	// pendingResultErr 保存待处理明细失败结果。
	_, pendingResultErr := pendingService.StartBatch(context.Background(), 1, "pending", time.Second)
	if !errors.Is(pendingResultErr, pendingErr) || !pendingRepository.released {
		t.Fatalf("待处理明细错误=%v released=%v", pendingResultErr, pendingRepository.released)
	}
	// joinedRepository 保存主错误和释放错误同时发生的仓储替身。
	joinedRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "joined", Status: "preview"}, claimed: true, pendingErr: pendingErr, failClaimErr: errors.New("release failed")}
	// joinedService 保存错误聚合测试服务。
	joinedService := newBatchManagementServiceForTest(t, joinedRepository, &batchManagementRuntimeFake{})
	// joinedErr 保存聚合后的错误链。
	_, joinedErr := joinedService.StartBatch(context.Background(), 1, "joined", time.Second)
	if !errors.Is(joinedErr, pendingErr) || !errors.Is(joinedErr, joinedRepository.failClaimErr) {
		t.Fatalf("聚合错误=%v", joinedErr)
	}
	// noRuntimeRepository 保存没有运行时但有待处理明细的合法批次。
	noRuntimeRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "no-runtime", Status: "preview"}, claimed: true, pending: []BatchRow{{ID: 1}}}
	// noRuntimeService 保存无运行时测试服务。
	noRuntimeService := newBatchManagementServiceForTest(t, noRuntimeRepository, &batchManagementRuntimeFake{})
	noRuntimeService.runtime = nil
	// noRuntimeID、noRuntimeErr 保存无运行时启动结果。
	noRuntimeID, noRuntimeErr := noRuntimeService.StartBatch(context.Background(), 1, "no-runtime", 0)
	if noRuntimeID != "no-runtime" || noRuntimeErr != nil {
		t.Fatalf("无运行时启动结果=%q err=%v", noRuntimeID, noRuntimeErr)
	}
	// rowsErr 是批次明细读取失败的底层错误。
	rowsErr := errors.New("rows failed")
	// rowsRepository 保存批次明细读取失败的仓储替身。
	rowsRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "rows", Status: "completed"}, rowsErr: rowsErr}
	// rowsService 保存批次明细读取失败测试服务。
	rowsService := newBatchManagementServiceForTest(t, rowsRepository, &batchManagementRuntimeFake{})
	// rowsResultErr 保存明细读取错误。
	_, rowsResultErr := rowsService.GetBatch(context.Background(), 1, "rows")
	if !errors.Is(rowsResultErr, rowsErr) {
		t.Fatalf("明细读取错误=%v", rowsResultErr)
	}
}

// TestBatchManagementCoversRetryCompensation 验证重试重置、统计和明细读取失败时的租约补偿。
func TestBatchManagementCoversRetryCompensation(t *testing.T) {
	// originalRandomReader 保存批次租约随机读取器的原始实现。
	originalRandomReader := readBatchLeaseRandomBytes
	readBatchLeaseRandomBytes = func([]byte) (int, error) { return 0, errors.New("随机源失败") }
	t.Cleanup(func() { readBatchLeaseRandomBytes = originalRandomReader })
	// fallbackToken 保存随机源失败时生成的批次租约降级令牌。
	if fallbackToken := randomBatchToken(); fallbackToken == "" {
		t.Fatal("随机源失败时批次租约令牌不应为空")
	}
	// resetErr 是失败明细重置错误。
	resetErr := errors.New("reset failed")
	// resetRepository 保存重置失败测试仓储。
	resetRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "reset", Status: "completed"}, claimed: true, resetErr: resetErr}
	// resetService 保存重置失败测试服务。
	resetService := newBatchManagementServiceForTest(t, resetRepository, &batchManagementRuntimeFake{})
	// resetResultErr 保存重置失败结果。
	_, resetResultErr := resetService.RetryFailedBatch(context.Background(), 1, "reset", time.Second)
	if !errors.Is(resetResultErr, resetErr) || !resetRepository.released {
		t.Fatalf("重置错误=%v released=%v", resetResultErr, resetRepository.released)
	}
	// resetJoinedRepository 保存重置错误与租约释放错误同时发生的仓储。
	resetJoinedRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "reset-joined", Status: "completed"}, claimed: true, resetErr: resetErr, failClaimErr: errors.New("重置释放失败")}
	// resetJoinedService 保存重置错误聚合场景的服务。
	resetJoinedService := newBatchManagementServiceForTest(t, resetJoinedRepository, &batchManagementRuntimeFake{})
	// resetJoinedResultErr 保存重置和释放错误的合并结果。
	_, resetJoinedResultErr := resetJoinedService.RetryFailedBatch(context.Background(), 1, "reset-joined", time.Second)
	if !errors.Is(resetJoinedResultErr, resetErr) || !errors.Is(resetJoinedResultErr, resetJoinedRepository.failClaimErr) {
		t.Fatalf("重置聚合错误=%v", resetJoinedResultErr)
	}
	// recountErr 是统计重算错误。
	recountErr := errors.New("recount failed")
	// recountRepository 保存统计重算失败测试仓储。
	recountRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "recount", Status: "completed"}, claimed: true, recountErr: recountErr}
	// recountService 保存统计重算失败测试服务。
	recountService := newBatchManagementServiceForTest(t, recountRepository, &batchManagementRuntimeFake{})
	// recountResultErr 保存统计重算失败结果。
	_, recountResultErr := recountService.RetryFailedBatch(context.Background(), 1, "recount", time.Second)
	if !errors.Is(recountResultErr, recountErr) || !recountRepository.released {
		t.Fatalf("统计重算错误=%v released=%v", recountResultErr, recountRepository.released)
	}
	// recountJoinedRepository 保存统计重算错误与租约释放错误同时发生的仓储。
	recountJoinedRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "recount-joined", Status: "completed"}, claimed: true, recountErr: recountErr, failClaimErr: errors.New("重算释放失败")}
	// recountJoinedService 保存统计重算错误聚合场景的服务。
	recountJoinedService := newBatchManagementServiceForTest(t, recountJoinedRepository, &batchManagementRuntimeFake{})
	// recountJoinedResultErr 保存统计重算和释放错误的合并结果。
	_, recountJoinedResultErr := recountJoinedService.RetryFailedBatch(context.Background(), 1, "recount-joined", time.Second)
	if !errors.Is(recountJoinedResultErr, recountErr) || !errors.Is(recountJoinedResultErr, recountJoinedRepository.failClaimErr) {
		t.Fatalf("统计重算聚合错误=%v", recountJoinedResultErr)
	}
	// pendingErr 是重试明细查询错误。
	pendingErr := errors.New("retry pending failed")
	// pendingRepository 保存重试明细查询失败测试仓储。
	pendingRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "pending", Status: "completed"}, claimed: true, pendingErr: pendingErr}
	// pendingService 保存重试明细查询失败测试服务。
	pendingService := newBatchManagementServiceForTest(t, pendingRepository, &batchManagementRuntimeFake{})
	// pendingResultErr 保存重试明细查询失败结果。
	_, pendingResultErr := pendingService.RetryFailedBatch(context.Background(), 1, "pending", time.Second)
	if !errors.Is(pendingResultErr, pendingErr) || !pendingRepository.released {
		t.Fatalf("重试明细错误=%v released=%v", pendingResultErr, pendingRepository.released)
	}
	// emptyRepository 保存重试后没有可处理明细的批次。
	emptyRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "empty", Status: "completed"}, claimed: true}
	// emptyService 保存空重试批次服务。
	emptyService := newBatchManagementServiceForTest(t, emptyRepository, &batchManagementRuntimeFake{})
	// emptyID、emptyErr 保存空重试结果。
	emptyID, emptyErr := emptyService.RetryFailedBatch(context.Background(), 1, "empty", 0)
	if emptyID != "" || !errors.Is(emptyErr, ErrBatchNoRows) || !emptyRepository.finalized {
		t.Fatalf("空重试结果=%q err=%v finalized=%v", emptyID, emptyErr, emptyRepository.finalized)
	}
}
