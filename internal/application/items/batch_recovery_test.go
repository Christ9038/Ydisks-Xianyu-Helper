package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// batchRecoveryRepositoryFake 记录恢复服务对批次租约端口的调用顺序。
type batchRecoveryRepositoryFake struct {
	// batches 保存本轮扫描返回的批次。
	batches []BatchInfo
	// pending 保存每个批次接管后可处理的明细数量。
	pending map[string][]BatchRow
	// scanErr 模拟恢复批次查询失败。
	scanErr error
	// resetErr 模拟中断明细重置失败。
	resetErr error
	// pendingErr 模拟接管后明细查询失败。
	pendingErr error
	// claimed 保存成功接管的批次租约参数。
	claimed []batchRecoveryClaim
	// released 保存初始化失败后释放的批次租约参数。
	released []batchRecoveryClaim
	// canceled 保存过期取消请求的批次标识。
	canceled []string
	// finalized 保存没有待处理明细的批次标识。
	finalized []string
}

// batchRecoveryClaim 保存一次批次租约抢占或释放调用。
type batchRecoveryClaim struct {
	// batchID 是批次标识。
	batchID string
	// workerToken 是 worker 租约令牌。
	workerToken string
	// leaseExpiresAt 是租约截止 Unix 秒。
	leaseExpiresAt int64
}

// RecoverableBatches 返回预置的可恢复批次。
func (repository *batchRecoveryRepositoryFake) RecoverableBatches(_ context.Context, _ int64, _ int) ([]BatchInfo, error) {
	if repository.scanErr != nil {
		return nil, repository.scanErr
	}
	return repository.batches, nil
}

// FinalizeExpiredCancellation 记录过期取消请求的收口。
func (repository *batchRecoveryRepositoryFake) FinalizeExpiredCancellation(_ context.Context, batchID string, _ int64) (bool, error) {
	repository.canceled = append(repository.canceled, batchID)
	return true, nil
}

// ClaimBatch 记录批次租约抢占并允许测试继续执行。
func (repository *batchRecoveryRepositoryFake) ClaimBatch(_ context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	repository.claimed = append(repository.claimed, batchRecoveryClaim{batchID: batchID, workerToken: workerToken, leaseExpiresAt: leaseExpiresAt})
	return true, nil
}

// ResetInterrupted 返回预置的中断明细重置结果。
func (repository *batchRecoveryRepositoryFake) ResetInterrupted(_ context.Context, _ string) error {
	return repository.resetErr
}

// RecountBatch 表示测试仓储已经完成统计重算。
func (repository *batchRecoveryRepositoryFake) RecountBatch(_ context.Context, _ string) error {
	return nil
}

// PendingRows 返回指定批次接管后的待处理明细。
func (repository *batchRecoveryRepositoryFake) PendingRows(_ context.Context, batchID string, _ bool) ([]BatchRow, error) {
	if repository.pendingErr != nil {
		return nil, repository.pendingErr
	}
	return repository.pending[batchID], nil
}

// FinalizeBatch 记录没有待处理明细的批次收口。
func (repository *batchRecoveryRepositoryFake) FinalizeBatch(_ context.Context, batchID, _ string) (string, bool, error) {
	repository.finalized = append(repository.finalized, batchID)
	return "completed", true, nil
}

// FailClaimedBatch 记录恢复初始化失败后的租约释放。
func (repository *batchRecoveryRepositoryFake) FailClaimedBatch(_ context.Context, batchID, workerToken string) (bool, error) {
	repository.released = append(repository.released, batchRecoveryClaim{batchID: batchID, workerToken: workerToken})
	return true, nil
}

// TestBatchRecoveryClaimsPendingBatch 验证恢复服务接管并启动仍有待处理明细的批次。
func TestBatchRecoveryClaimsPendingBatch(t *testing.T) {
	// fixedNow 保存可复现的恢复扫描时间。
	fixedNow := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	// repository 保存一个仍需恢复的批次和一条待处理明细。
	repository := &batchRecoveryRepositoryFake{
		batches: []BatchInfo{{ID: "batch-1", UserID: 7, Status: "running"}},
		pending: map[string][]BatchRow{"batch-1": {{ID: 11}}},
	}
	// startedUserID、startedBatchID 和 startedToken 保存生命周期边界收到的 worker 参数。
	var startedUserID int64
	// startedBatchID 和 startedToken 保存恢复服务启动的批次标识与租约令牌。
	var startedBatchID, startedToken string
	// service 保存使用固定时钟和令牌的恢复应用服务。
	service, err := NewBatchRecoveryService(repository, BatchRecoveryOptions{
		LeaseDuration:  5 * time.Minute,
		NewWorkerToken: func() string { return "worker-1" },
		Now:            func() time.Time { return fixedNow },
		StartWorker: func(_ context.Context, userID int64, batchID, workerToken string) {
			startedUserID, startedBatchID, startedToken = userID, batchID, workerToken
		},
	})
	if err != nil {
		t.Fatalf("创建恢复服务失败: %v", err)
	}
	// err 保存恢复扫描的执行错误。
	if err := service.Recover(context.Background()); err != nil {
		t.Fatalf("执行恢复扫描失败: %v", err)
	}
	if len(repository.claimed) != 1 || repository.claimed[0].batchID != "batch-1" || repository.claimed[0].workerToken != "worker-1" {
		t.Fatalf("批次租约抢占异常：%+v", repository.claimed)
	}
	if repository.claimed[0].leaseExpiresAt != fixedNow.Add(5*time.Minute).Unix() {
		t.Fatalf("租约截止时间异常：%d", repository.claimed[0].leaseExpiresAt)
	}
	if startedUserID != 7 || startedBatchID != "batch-1" || startedToken != "worker-1" {
		t.Fatalf("worker 启动参数异常：user=%d batch=%s token=%s", startedUserID, startedBatchID, startedToken)
	}
}

// TestBatchRecoveryFinalizesCancelingAndEmptyBatches 验证取消和空批次分支不会启动 worker。
func TestBatchRecoveryFinalizesCancelingAndEmptyBatches(t *testing.T) {
	// repository 保存一个取消中的批次和一个已无待处理明细的批次。
	repository := &batchRecoveryRepositoryFake{
		batches: []BatchInfo{{ID: "canceling", Status: "canceling"}, {ID: "empty", Status: "running"}},
		pending: map[string][]BatchRow{"empty": nil},
	}
	// startedCount 统计恢复服务启动 worker 的次数。
	startedCount := 0
	// service 保存测试用的恢复应用服务。
	service, err := NewBatchRecoveryService(repository, BatchRecoveryOptions{
		NewWorkerToken: func() string { return "worker" },
		StartWorker:    func(context.Context, int64, string, string) { startedCount++ },
	})
	if err != nil {
		t.Fatalf("创建恢复服务失败: %v", err)
	}
	// err 保存恢复扫描的执行错误。
	if err := service.Recover(context.Background()); err != nil {
		t.Fatalf("执行恢复扫描失败: %v", err)
	}
	if len(repository.canceled) != 1 || repository.canceled[0] != "canceling" {
		t.Fatalf("取消批次未收口：%v", repository.canceled)
	}
	if len(repository.finalized) != 1 || repository.finalized[0] != "empty" {
		t.Fatalf("空批次未收口：%v", repository.finalized)
	}
	if startedCount != 0 {
		t.Fatalf("空批次不应启动 worker：%d", startedCount)
	}
}

// TestBatchRecoveryReleasesClaimWhenResetFails 验证接管后重置失败会释放租约。
func TestBatchRecoveryReleasesClaimWhenResetFails(t *testing.T) {
	// repository 模拟接管后无法重置进程中断明细。
	repository := &batchRecoveryRepositoryFake{
		batches:  []BatchInfo{{ID: "batch-1", Status: "running"}},
		resetErr: errors.New("reset failed"),
	}
	// service 保存只验证租约释放的恢复服务。
	service, err := NewBatchRecoveryService(repository, BatchRecoveryOptions{
		NewWorkerToken: func() string { return "worker-1" },
		StartWorker:    func(context.Context, int64, string, string) {},
	})
	if err != nil {
		t.Fatalf("创建恢复服务失败: %v", err)
	}
	// err 保存恢复扫描的执行错误。
	if err := service.Recover(context.Background()); err != nil {
		t.Fatalf("单批次初始化失败不应阻断扫描：%v", err)
	}
	if len(repository.released) != 1 || repository.released[0].batchID != "batch-1" || repository.released[0].workerToken != "worker-1" {
		t.Fatalf("初始化失败后未释放租约：%+v", repository.released)
	}
}

// TestBatchRecoveryReportsScanFailure 验证恢复批次查询失败会返回全局错误。
func TestBatchRecoveryReportsScanFailure(t *testing.T) {
	// scanErr 保存恢复批次查询的基础设施错误。
	scanErr := errors.New("scan failed")
	// repository 保存预置的查询错误。
	repository := &batchRecoveryRepositoryFake{scanErr: scanErr}
	// service 保存可执行恢复扫描的服务。
	service, err := NewBatchRecoveryService(repository, BatchRecoveryOptions{StartWorker: func(context.Context, int64, string, string) {}})
	if err != nil {
		t.Fatalf("创建恢复服务失败: %v", err)
	}
	// runErr 保存恢复扫描返回的查询错误。
	runErr := service.Recover(context.Background())
	if !errors.Is(runErr, scanErr) {
		t.Fatalf("恢复扫描错误=%v，期望=%v", runErr, scanErr)
	}
}
