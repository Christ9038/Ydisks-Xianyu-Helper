package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBatchManagementCoversDefaultTokenAndLookupErrors 验证默认令牌源、批次查询错误和启动补偿错误。
func TestBatchManagementCoversDefaultTokenAndLookupErrors(t *testing.T) {
	// repository 保存默认令牌源启动所需的合法批次。
	repository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "default", Status: "preview"}, claimed: true, pending: []BatchRow{{ID: 1}}}
	// service 保存生产默认令牌源构造出的批次管理服务。
	service, serviceErr := NewBatchManagementService(repository, nil)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// batchID、startErr 保存默认随机令牌启动结果。
	batchID, startErr := service.StartBatch(context.Background(), 1, "default", time.Second)
	if startErr != nil || batchID != "default" {
		t.Fatalf("默认令牌启动结果=%q err=%v", batchID, startErr)
	}
	// lookupErr 是批次归属查询失败的底层错误。
	lookupErr := errors.New("batch lookup failed")
	// lookupRepository 保存批次查询错误场景。
	lookupRepository := &batchManagementRepositoryFake{err: lookupErr}
	// lookupService 保存批次查询错误服务。
	lookupService := newBatchManagementServiceForTest(t, lookupRepository, &batchManagementRuntimeFake{})
	// startLookupErr 保存启动查询错误映射。
	_, startLookupErr := lookupService.StartBatch(context.Background(), 1, "batch", time.Second)
	if !errors.Is(startLookupErr, ErrBatchNotFound) {
		t.Fatalf("启动查询错误=%v", startLookupErr)
	}
	// deleteLookupErr 保存删除前归属查询错误映射。
	if deleteLookupErr := lookupService.DeleteBatch(context.Background(), 1, "batch"); !errors.Is(deleteLookupErr, ErrBatchNotFound) {
		t.Fatalf("删除查询错误=%v", deleteLookupErr)
	}
	// cancelLookupErr 保存取消前归属查询错误映射。
	if _, cancelLookupErr := lookupService.CancelBatch(context.Background(), 1, "batch"); !errors.Is(cancelLookupErr, ErrBatchNotFound) {
		t.Fatalf("取消查询错误=%v", cancelLookupErr)
	}
	// startError 是 worker 登记失败的底层错误。
	startError := errors.New("worker start failed")
	// compensatedRepository 保存启动失败且租约释放也失败的仓储替身。
	compensatedRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "compensated", Status: "preview"}, claimed: true, pending: []BatchRow{{ID: 1}}, failClaimErr: errors.New("release failed")}
	// compensatedService 保存启动补偿错误服务。
	compensatedService := newBatchManagementServiceForTest(t, compensatedRepository, &batchManagementRuntimeFake{startErr: startError})
	// compensatedErr 保存两个阶段的聚合错误。
	_, compensatedErr := compensatedService.StartBatch(context.Background(), 1, "compensated", time.Second)
	if !errors.Is(compensatedErr, startError) || !errors.Is(compensatedErr, compensatedRepository.failClaimErr) {
		t.Fatalf("启动补偿错误=%v", compensatedErr)
	}
}

// TestBatchManagementCoversCancelAndRetryErrors 验证取消、重试抢占和各阶段失败的错误透传与租约释放。
func TestBatchManagementCoversCancelAndRetryErrors(t *testing.T) {
	// requestError 是取消请求失败的底层错误。
	requestError := errors.New("cancel request failed")
	// requestRepository 保存取消请求失败场景。
	requestRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "request", Status: "completed"}, requestErr: requestError}
	// requestService 保存取消请求失败服务。
	requestService := newBatchManagementServiceForTest(t, requestRepository, &batchManagementRuntimeFake{})
	// requestResultErr 保存取消请求错误。
	_, requestResultErr := requestService.CancelBatch(context.Background(), 1, "request")
	if !errors.Is(requestResultErr, requestError) {
		t.Fatalf("取消请求错误=%v", requestResultErr)
	}
	// noRuntimeRepository 保存 worker 仍运行但未装配运行时的批次。
	noRuntimeRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "no-runtime", Status: "completed"}, cancelToken: "worker", cancelRunning: true}
	// noRuntimeService 保存无运行时取消服务。
	noRuntimeService := newBatchManagementServiceForTest(t, noRuntimeRepository, &batchManagementRuntimeFake{})
	noRuntimeService.runtime = nil
	// noRuntimeStatus、noRuntimeErr 保存无运行时取消结果。
	noRuntimeStatus, noRuntimeErr := noRuntimeService.CancelBatch(context.Background(), 1, "no-runtime")
	if noRuntimeErr != nil || noRuntimeStatus != "canceled" {
		t.Fatalf("无运行时取消结果=%q err=%v", noRuntimeStatus, noRuntimeErr)
	}
	// retryLookupRepository 保存重试前查询失败场景。
	retryLookupRepository := &batchManagementRepositoryFake{err: errors.New("retry lookup failed")}
	// retryLookupService 保存重试查询失败服务。
	retryLookupService := newBatchManagementServiceForTest(t, retryLookupRepository, &batchManagementRuntimeFake{})
	// retryLookupErr 保存重试查询错误映射。
	_, retryLookupErr := retryLookupService.RetryFailedBatch(context.Background(), 1, "retry", time.Second)
	if !errors.Is(retryLookupErr, ErrBatchNotFound) {
		t.Fatalf("重试查询错误=%v", retryLookupErr)
	}
	// activeRepository 保存仍在有效租约内的重试批次。
	activeRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "active", Status: "running", LeaseExpiresAt: 200}}
	// activeService 保存活动租约重试服务。
	activeService := newBatchManagementServiceForTest(t, activeRepository, &batchManagementRuntimeFake{})
	// activeErr 保存活动租约冲突错误。
	_, activeErr := activeService.RetryFailedBatch(context.Background(), 1, "active", time.Second)
	if !errors.Is(activeErr, ErrBatchConflict) {
		t.Fatalf("活动重试错误=%v", activeErr)
	}
	// claimError 是重试租约声明失败错误。
	claimError := errors.New("retry claim failed")
	// claimRepository 保存重试租约声明失败仓储。
	claimRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "claim", Status: "completed"}, claimErr: claimError}
	// claimService 保存重试租约声明失败服务。
	claimService := newBatchManagementServiceForTest(t, claimRepository, &batchManagementRuntimeFake{})
	// claimResultErr 保存重试租约声明错误。
	_, claimResultErr := claimService.RetryFailedBatch(context.Background(), 1, "claim", time.Second)
	if !errors.Is(claimResultErr, claimError) {
		t.Fatalf("重试租约错误=%v", claimResultErr)
	}
	// notClaimedRepository 保存重试租约被并发占用场景。
	notClaimedRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "not-claimed", Status: "completed"}}
	// notClaimedService 保存未抢占重试服务。
	notClaimedService := newBatchManagementServiceForTest(t, notClaimedRepository, &batchManagementRuntimeFake{})
	// notClaimedErr 保存未抢占冲突错误。
	_, notClaimedErr := notClaimedService.RetryFailedBatch(context.Background(), 1, "not-claimed", time.Second)
	if !errors.Is(notClaimedErr, ErrBatchConflict) {
		t.Fatalf("重试未抢占错误=%v", notClaimedErr)
	}
}

// TestBatchManagementRetryCompensationCoversAllFailureStages 覆盖重试重置、重算、明细查询和 worker 启动失败的租约补偿。
func TestBatchManagementRetryCompensationCoversAllFailureStages(t *testing.T) {
	// resetErr 是失败明细重置阶段的底层错误。
	resetErr := errors.New("reset failed")
	// resetRepository 保存重置失败但租约释放成功的仓储替身。
	resetRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "reset", Status: "completed"}, claimed: true, pending: []BatchRow{{ID: 1}}, resetErr: resetErr}
	// resetService 保存重置失败测试服务。
	resetService := newBatchManagementServiceForTest(t, resetRepository, &batchManagementRuntimeFake{})
	// resetResultErr 保存重置阶段返回的错误。
	_, resetResultErr := resetService.RetryFailedBatch(context.Background(), 1, "reset", time.Second)
	if !errors.Is(resetResultErr, resetErr) || !resetRepository.released {
		t.Fatalf("重置失败补偿异常 err=%v released=%v", resetResultErr, resetRepository.released)
	}

	// recountErr 是批次统计重算阶段的底层错误。
	recountErr := errors.New("recount failed")
	// recountRepository 保存重算失败且租约释放失败的仓储替身。
	recountRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "recount", Status: "completed"}, claimed: true, pending: []BatchRow{{ID: 1}}, recountErr: recountErr, failClaimErr: errors.New("recount release failed")}
	// recountService 保存重算失败测试服务。
	recountService := newBatchManagementServiceForTest(t, recountRepository, &batchManagementRuntimeFake{})
	// recountResultErr 保存重算及租约补偿聚合错误。
	_, recountResultErr := recountService.RetryFailedBatch(context.Background(), 1, "recount", time.Second)
	if !errors.Is(recountResultErr, recountErr) || !errors.Is(recountResultErr, recountRepository.failClaimErr) {
		t.Fatalf("重算失败补偿异常 err=%v", recountResultErr)
	}

	// pendingErr 是重试明细查询阶段的底层错误。
	pendingErr := errors.New("pending failed")
	// pendingRepository 保存明细查询失败且租约释放失败的仓储替身。
	pendingRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "pending", Status: "completed"}, claimed: true, pendingErr: pendingErr, failClaimErr: errors.New("pending release failed")}
	// pendingService 保存明细查询失败测试服务。
	pendingService := newBatchManagementServiceForTest(t, pendingRepository, &batchManagementRuntimeFake{})
	// pendingResultErr 保存明细查询及租约补偿聚合错误。
	_, pendingResultErr := pendingService.RetryFailedBatch(context.Background(), 1, "pending", time.Second)
	if !errors.Is(pendingResultErr, pendingErr) || !errors.Is(pendingResultErr, pendingRepository.failClaimErr) {
		t.Fatalf("明细查询失败补偿异常 err=%v", pendingResultErr)
	}

	// startErr 是重试 worker 登记阶段的底层错误。
	startErr := errors.New("start failed")
	// startRepository 保存 worker 启动失败且租约释放失败的仓储替身。
	startRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "start", Status: "completed"}, claimed: true, pending: []BatchRow{{ID: 1}}, failClaimErr: errors.New("start release failed")}
	// startRuntime 保存 worker 启动失败的生命周期替身。
	startRuntime := &batchManagementRuntimeFake{startErr: startErr}
	// startService 保存 worker 启动失败测试服务。
	startService := newBatchManagementServiceForTest(t, startRepository, startRuntime)
	// startResultErr 保存 worker 启动及租约补偿聚合错误。
	_, startResultErr := startService.RetryFailedBatch(context.Background(), 1, "start", time.Second)
	if !errors.Is(startResultErr, startErr) || !errors.Is(startResultErr, startRepository.failClaimErr) {
		t.Fatalf("worker 启动失败补偿异常 err=%v", startResultErr)
	}

	// expiredErr 是过期上传批次查询阶段的底层错误。
	expiredErr := errors.New("expired lookup failed")
	// expiredRepository 保存过期上传查询失败的仓储替身。
	expiredRepository := &batchManagementRepositoryFake{expiredErr: expiredErr}
	// expiredService 保存过期上传查询失败测试服务。
	expiredService := newBatchManagementServiceForTest(t, expiredRepository, &batchManagementRuntimeFake{})
	// expiredResultErr 保存过期上传查询错误。
	// limit 表示本次过期上传查询的最大批次数量。
	limit := 1
	// expiredResultErr 保存过期上传查询错误。
	expiredResultErr := expiredService.CleanupExpiredUploads(context.Background(), time.Unix(100, 0), limit)
	if !errors.Is(expiredResultErr, expiredErr) {
		t.Fatalf("过期上传查询错误=%v", expiredResultErr)
	}
}
