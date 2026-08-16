package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// batchManagementRepositoryFake 保存批次管理用例测试所需的状态和调用记录。
type batchManagementRepositoryFake struct {
	// batch 是当前用户可见的批次。
	batch BatchInfo
	// batches 是批次列表查询结果。
	batches []BatchInfo
	// rows 是批次明细查询结果。
	rows []BatchRow
	// pending 是待处理明细查询结果。
	pending []BatchRow
	// err 是所有未单独配置方法的预置错误。
	err error
	// claimed 表示租约抢占结果。
	claimed bool
	// cancelToken 是取消请求返回的 worker 令牌。
	cancelToken string
	// cancelRunning 表示取消请求时 worker 是否仍在运行。
	cancelRunning bool
	// deleted 表示批次删除是否被调用。
	deleted bool
	// resetFailedCalled 表示失败明细重置是否被调用。
	resetFailedCalled bool
	// recountCalled 表示批次统计重算是否被调用。
	recountCalled bool
	// finalized 表示无明细时批次是否被收口。
	finalized bool
	// released 表示异常阶段是否释放了批次租约。
	released bool
	// expiredUploads 保存过期上传批次查询结果。
	expiredUploads []BatchInfo
	// deletedUploads 保存上传目录清理调用的批次标识。
	deletedUploads []string
	// uploadCleanupErr 是上传目录清理预置错误。
	uploadCleanupErr error
}

// GetBatch 返回预置批次或仓储错误。
func (repository *batchManagementRepositoryFake) GetBatch(context.Context, int64, string) (BatchInfo, error) {
	if repository.err != nil {
		return BatchInfo{}, repository.err
	}
	return repository.batch, nil
}

// ClaimBatch 返回预置租约抢占结果。
func (repository *batchManagementRepositoryFake) ClaimBatch(context.Context, string, string, int64) (bool, error) {
	if repository.err != nil {
		return false, repository.err
	}
	return repository.claimed, nil
}

// PendingRows 返回预置待处理明细。
func (repository *batchManagementRepositoryFake) PendingRows(context.Context, string, bool) ([]BatchRow, error) {
	if repository.err != nil {
		return nil, repository.err
	}
	return repository.pending, nil
}

// FinalizeBatch 记录无明细时的批次收口。
func (repository *batchManagementRepositoryFake) FinalizeBatch(context.Context, string, string) (string, bool, error) {
	repository.finalized = true
	return "completed", true, nil
}

// ListBatchesForUser 返回预置批次列表。
func (repository *batchManagementRepositoryFake) ListBatchesForUser(context.Context, int64, int) ([]BatchInfo, error) {
	if repository.err != nil {
		return nil, repository.err
	}
	return repository.batches, nil
}

// ListBatchRows 返回预置批次明细。
func (repository *batchManagementRepositoryFake) ListBatchRows(context.Context, string) ([]BatchRow, error) {
	if repository.err != nil {
		return nil, repository.err
	}
	return repository.rows, nil
}

// RequestCancel 返回预置取消状态。
func (repository *batchManagementRepositoryFake) RequestCancel(context.Context, string) (string, bool, error) {
	if repository.err != nil {
		return "", false, repository.err
	}
	return repository.cancelToken, repository.cancelRunning, nil
}

// DeleteBatch 记录批次删除调用。
func (repository *batchManagementRepositoryFake) DeleteBatch(context.Context, int64, string) error {
	repository.deleted = true
	return repository.err
}

// ResetFailed 记录失败明细重置调用。
func (repository *batchManagementRepositoryFake) ResetFailed(context.Context, string) error {
	repository.resetFailedCalled = true
	return repository.err
}

// RecountBatch 记录批次统计重算调用。
func (repository *batchManagementRepositoryFake) RecountBatch(context.Context, string) error {
	repository.recountCalled = true
	return repository.err
}

// ExpiredUploadBatches 返回预置的过期上传批次。
func (repository *batchManagementRepositoryFake) ExpiredUploadBatches(context.Context, string, int) ([]BatchInfo, error) {
	if repository.err != nil {
		return nil, repository.err
	}
	return repository.expiredUploads, nil
}

// DeleteUpload 记录上传目录清理调用并返回预置错误。
func (repository *batchManagementRepositoryFake) DeleteUpload(_ context.Context, batchID, _ string) error {
	repository.deletedUploads = append(repository.deletedUploads, batchID)
	return repository.uploadCleanupErr
}

// FailClaimedBatch 记录批次租约释放调用。
func (repository *batchManagementRepositoryFake) FailClaimedBatch(context.Context, string, string) (bool, error) {
	repository.released = true
	return true, repository.err
}

// batchManagementRuntimeFake 记录应用服务发出的 worker 控制命令。
type batchManagementRuntimeFake struct {
	// started 保存启动 worker 的参数。
	started []string
	// canceled 保存取消 worker 的参数。
	canceled []string
}

// StartBatch 记录批次 worker 启动。
func (runtime *batchManagementRuntimeFake) StartBatch(userID int64, batchID, workerToken string) {
	// command 保存便于断言的 worker 启动参数。
	command := batchID + ":" + workerToken
	runtime.started = append(runtime.started, command)
}

// CancelBatch 记录批次 worker 取消。
func (runtime *batchManagementRuntimeFake) CancelBatch(batchID, workerToken string) {
	// command 保存便于断言的 worker 取消参数。
	command := batchID + ":" + workerToken
	runtime.canceled = append(runtime.canceled, command)
}

// newBatchManagementServiceForTest 创建使用固定时间和令牌的批次管理服务。
func newBatchManagementServiceForTest(t *testing.T, repository *batchManagementRepositoryFake, runtime *batchManagementRuntimeFake) *BatchManagementService {
	// service、err 保存批次管理服务构造结果。
	service, err := NewBatchManagementService(repository, runtime)
	if err != nil {
		t.Fatalf("NewBatchManagementService error: %v", err)
	}
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.tokenFactory = func() string { return "worker-fixed" }
	return service
}

// TestBatchManagementStartAndCancel 验证批次启动声明租约、启动 worker 和取消通知顺序。
func TestBatchManagementStartAndCancel(t *testing.T) {
	// repository、runtime 和 service 保存本测试的应用端口替身。
	repository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "batch-1", Status: "preview"}, claimed: true, pending: []BatchRow{{ID: 1}}}
	// runtime 保存批次 worker 控制调用记录。
	runtime := &batchManagementRuntimeFake{}
	// service 保存固定时间和租约令牌的批次管理服务。
	service := newBatchManagementServiceForTest(t, repository, runtime)
	// batchID、startErr 保存批次启动结果。
	batchID, startErr := service.StartBatch(context.Background(), 7, "batch-1", time.Minute)
	if startErr != nil || batchID != "batch-1" || len(runtime.started) != 1 || runtime.started[0] != "batch-1:worker-fixed" {
		t.Fatalf("start result=%q err=%v runtime=%+v", batchID, startErr, runtime)
	}
	repository.cancelToken = "worker-fixed"
	repository.cancelRunning = true
	// status、cancelErr 保存批次取消结果。
	status, cancelErr := service.CancelBatch(context.Background(), 7, "batch-1")
	if cancelErr != nil || status != "canceling" || len(runtime.canceled) != 1 || runtime.canceled[0] != "batch-1:worker-fixed" {
		t.Fatalf("cancel result=%q err=%v runtime=%+v", status, cancelErr, runtime)
	}
}

// TestBatchManagementRejectsConflictAndNoRows 验证活动租约与空批次均不会启动 worker。
func TestBatchManagementRejectsConflictAndNoRows(t *testing.T) {
	// activeRepository 保存仍在有效租约内的批次。
	activeRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "active", Status: "running", LeaseExpiresAt: 200}, claimed: true, pending: []BatchRow{{ID: 1}}}
	// activeRuntime 保存活动租约测试的运行时替身。
	activeRuntime := &batchManagementRuntimeFake{}
	// activeService 保存活动租约测试的批次管理服务。
	activeService := newBatchManagementServiceForTest(t, activeRepository, activeRuntime)
	// activeErr 保存活动租约拒绝结果。
	_, activeErr := activeService.StartBatch(context.Background(), 7, "active", time.Minute)
	if !errors.Is(activeErr, ErrBatchConflict) {
		t.Fatalf("active error=%v", activeErr)
	}
	// emptyRepository 保存已经没有可处理明细的批次。
	emptyRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "empty", Status: "preview"}, claimed: true}
	// emptyRuntime 保存空批次测试的运行时替身。
	emptyRuntime := &batchManagementRuntimeFake{}
	// emptyService 保存空批次测试的批次管理服务。
	emptyService := newBatchManagementServiceForTest(t, emptyRepository, emptyRuntime)
	// _, emptyErr 保存空批次启动结果。
	_, emptyErr := emptyService.StartBatch(context.Background(), 7, "empty", time.Minute)
	if !errors.Is(emptyErr, ErrBatchNoRows) || !emptyRepository.finalized || len(emptyRuntime.started) != 0 {
		t.Fatalf("empty result err=%v finalized=%v runtime=%+v", emptyErr, emptyRepository.finalized, emptyRuntime)
	}
}

// TestBatchManagementListGetDelete 验证批次列表、明细查询和删除状态约束。
func TestBatchManagementListGetDelete(t *testing.T) {
	// repository 保存查询和删除测试数据。
	repository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "batch-1", Status: "completed"}, batches: []BatchInfo{{ID: "batch-1"}}, rows: []BatchRow{{ID: 2, Status: "failed"}}}
	// service 保存批次管理应用服务。
	service := newBatchManagementServiceForTest(t, repository, &batchManagementRuntimeFake{})
	// batches、listErr 保存用户批次列表结果。
	batches, listErr := service.ListBatches(context.Background(), 7, 20)
	if listErr != nil || len(batches) != 1 || batches[0].ID != "batch-1" {
		t.Fatalf("list result=%+v err=%v", batches, listErr)
	}
	// details、getErr 保存批次及明细查询结果。
	details, getErr := service.GetBatch(context.Background(), 7, "batch-1")
	if getErr != nil || details.Batch.ID != "batch-1" || len(details.Rows) != 1 {
		t.Fatalf("get result=%+v err=%v", details, getErr)
	}
	// deleteErr 保存批次删除结果。
	deleteErr := service.DeleteBatch(context.Background(), 7, "batch-1")
	if deleteErr != nil || !repository.deleted {
		t.Fatalf("delete err=%v deleted=%v", deleteErr, repository.deleted)
	}
	// runningRepository 保存运行中批次，验证删除会被拒绝。
	runningRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "running", Status: "running"}}
	// runningService 保存运行中批次测试的应用服务。
	runningService := newBatchManagementServiceForTest(t, runningRepository, &batchManagementRuntimeFake{})
	// runningErr 保存运行中批次删除结果。
	runningErr := runningService.DeleteBatch(context.Background(), 7, "running")
	if !errors.Is(runningErr, ErrBatchConflict) {
		t.Fatalf("running delete error=%v", runningErr)
	}
}

// TestBatchManagementFailClaimedBatch 验证异常退出路径只释放匹配的批次租约。
func TestBatchManagementFailClaimedBatch(t *testing.T) {
	// repository 保存租约释放调用记录。
	repository := &batchManagementRepositoryFake{}
	// service 保存批次管理应用服务。
	service := newBatchManagementServiceForTest(t, repository, &batchManagementRuntimeFake{})
	// released、releaseErr 保存有效租约释放结果。
	released, releaseErr := service.FailClaimedBatch(context.Background(), "batch-1", "worker-1")
	if releaseErr != nil || !released || !repository.released {
		t.Fatalf("release result=%v err=%v repository=%+v", released, releaseErr, repository)
	}
	// invalidReleased、invalidErr 保存缺少租约标识时的拒绝结果。
	invalidReleased, invalidErr := service.FailClaimedBatch(context.Background(), "", "worker-1")
	if invalidReleased || !errors.Is(invalidErr, ErrBatchNotFound) {
		t.Fatalf("invalid release result=%v err=%v", invalidReleased, invalidErr)
	}
}

// TestBatchManagementRetryFailed 验证失败明细重置、统计重算和重试 worker 启动。
func TestBatchManagementRetryFailed(t *testing.T) {
	// repository、runtime 和 service 保存重试测试依赖。
	repository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "batch-retry", Status: "completed"}, claimed: true, pending: []BatchRow{{ID: 3}}}
	// runtime 保存重试 worker 控制调用记录。
	runtime := &batchManagementRuntimeFake{}
	// service 保存固定时间和租约令牌的批次管理服务。
	service := newBatchManagementServiceForTest(t, repository, runtime)
	// batchID、retryErr 保存失败重试结果。
	batchID, retryErr := service.RetryFailedBatch(context.Background(), 7, "batch-retry", time.Minute)
	if retryErr != nil || batchID != "batch-retry" || !repository.resetFailedCalled || !repository.recountCalled || len(runtime.started) != 1 {
		t.Fatalf("retry result=%q err=%v repository=%+v runtime=%+v", batchID, retryErr, repository, runtime)
	}
}

// TestBatchManagementCleanupExpiredUploads 验证过期目录清理的成功、失败聚合与取消语义。
func TestBatchManagementCleanupExpiredUploads(t *testing.T) {
	// repository 保存过期批次和清理错误替身。
	repository := &batchManagementRepositoryFake{expiredUploads: []BatchInfo{{ID: "expired-1", UploadDir: "/tmp/one"}, {ID: "expired-2", UploadDir: "/tmp/two"}}}
	// service 保存固定时间的批次管理服务。
	service := newBatchManagementServiceForTest(t, repository, &batchManagementRuntimeFake{})
	// cleanupErr 保存成功清理结果。
	cleanupErr := service.CleanupExpiredUploads(context.Background(), time.Unix(200, 0), 2)
	if cleanupErr != nil || len(repository.deletedUploads) != 2 {
		t.Fatalf("cleanup result err=%v calls=%v", cleanupErr, repository.deletedUploads)
	}

	// repository.uploadCleanupErr 验证单个目录失败不会阻止后续目录处理。
	repository.uploadCleanupErr = errors.New("目录清理失败")
	repository.deletedUploads = nil
	cleanupErr = service.CleanupExpiredUploads(context.Background(), time.Unix(200, 0), 2)
	if cleanupErr == nil || len(repository.deletedUploads) != 2 {
		t.Fatalf("cleanup failure err=%v calls=%v", cleanupErr, repository.deletedUploads)
	}

	// canceledContext 验证清理循环尊重取消信号。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	repository.uploadCleanupErr = nil
	repository.deletedUploads = nil
	cleanupErr = service.CleanupExpiredUploads(canceledContext, time.Unix(200, 0), 2)
	if !errors.Is(cleanupErr, context.Canceled) {
		t.Fatalf("canceled cleanup err=%v", cleanupErr)
	}
}
