package items

import (
	"context"
	"errors"
	"testing"
)

// nilItemsContext 返回用于覆盖批量服务兼容 nil Context 分支的空上下文接口。
func nilItemsContext() context.Context { return nil }

// TestBatchRecoveryCoversBoundaryAndClaimBranches 验证恢复服务的依赖、上下文、令牌和租约分支。
func TestBatchRecoveryCoversBoundaryAndClaimBranches(t *testing.T) {
	// nilService、nilErr 保存缺少恢复仓储端口的构造结果。
	nilService, nilErr := NewBatchRecoveryService(nil, BatchRecoveryOptions{})
	if nilService != nil || nilErr == nil {
		t.Fatal("空恢复仓储应被拒绝")
	}
	// originalRandomReader 保存恢复令牌随机读取器的原始实现。
	originalRandomReader := readBatchWorkerRandomBytes
	readBatchWorkerRandomBytes = func([]byte) (int, error) { return 0, errors.New("随机源失败") }
	t.Cleanup(func() { readBatchWorkerRandomBytes = originalRandomReader })
	// fallbackToken 保存随机源失败时生成的降级恢复令牌。
	fallbackToken := randomBatchWorkerToken()
	if fallbackToken == "" {
		t.Fatal("随机源失败时恢复令牌不应为空")
	}
	// invalidService 是未装配仓储和 worker 回调的恢复服务。
	invalidService := &BatchRecoveryService{}
	// invalidRunErr 保存未装配恢复服务的边界错误。
	invalidRunErr := invalidService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { return nil })
	if invalidRunErr == nil {
		t.Fatal("未装配恢复服务应返回错误")
	}
	// repository 是无批次的本地恢复仓储。
	repository := &batchRecoveryRepositoryFake{}
	// service 是使用本地仓储的恢复服务。
	service, err := NewBatchRecoveryService(repository, BatchRecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// nilRecoverErr 保存 Recover 入口对空 Context 的拒绝结果。
	service.options.StartWorker = func(context.Context, int64, string, string) {}
	// nilRecoverErr 保存 Recover 入口对空 Context 的拒绝结果。
	if nilRecoverErr := service.Recover(nilItemsContext()); nilRecoverErr == nil {
		t.Fatal("Recover 空 Context 应返回错误")
	}
	// nilContextErr 保存空上下文边界错误。
	nilContextErr := service.RecoverWithStarter(nilItemsContext(), func(context.Context, int64, string, string) error { return nil })
	if nilContextErr == nil {
		t.Fatal("空上下文应返回错误")
	}
	// nilStarterErr 保存空 worker 回调边界错误。
	nilStarterErr := service.RecoverWithStarter(context.Background(), nil)
	if nilStarterErr == nil {
		t.Fatal("空 worker 回调应返回错误")
	}
	// claimFalse 保存数据库拒绝租约的结果。
	claimFalse := false
	// claimRepository 保存未抢占租约的批次。
	claimRepository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "claim-false", UserID: 1, Status: "running"}}, claimSuccess: &claimFalse}
	// claimService 是租约抢占失败场景的恢复服务。
	claimService, err := NewBatchRecoveryService(claimRepository, BatchRecoveryOptions{NewWorkerToken: func() string { return "worker" }})
	if err != nil {
		t.Fatal(err)
	}
	// claimRunErr 保存租约拒绝后的扫描结果。
	claimRunErr := claimService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { return nil })
	if claimRunErr != nil || len(claimRepository.claimed) != 1 {
		t.Fatalf("租约拒绝结果=%v claims=%v", claimRunErr, claimRepository.claimed)
	}
	// claimError 是租约抢占的基础设施错误。
	claimError := errors.New("claim error")
	// claimErrorRepository 保存租约错误场景。
	claimErrorRepository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "claim-error", Status: "running"}}, claimErr: claimError}
	// claimErrorService 是租约错误场景的恢复服务。
	claimErrorService, err := NewBatchRecoveryService(claimErrorRepository, BatchRecoveryOptions{NewWorkerToken: func() string { return "worker" }})
	if err != nil {
		t.Fatal(err)
	}
	// claimErrorRunErr 保存租约错误后的扫描结果；单批次错误不应升级为全局错误。
	claimErrorRunErr := claimErrorService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { return nil })
	if claimErrorRunErr != nil {
		t.Fatalf("租约错误不应阻断扫描: %v", claimErrorRunErr)
	}
	// emptyTokenRepository 保存恢复令牌为空的批次。
	emptyTokenRepository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "empty-token", Status: "running"}}}
	// emptyTokenService 是令牌为空场景的恢复服务。
	emptyTokenService, err := NewBatchRecoveryService(emptyTokenRepository, BatchRecoveryOptions{NewWorkerToken: func() string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	// emptyTokenErr 保存空令牌场景的扫描结果。
	emptyTokenErr := emptyTokenService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { return nil })
	if emptyTokenErr != nil || len(emptyTokenRepository.claimed) != 0 {
		t.Fatalf("空令牌结果=%v claims=%v", emptyTokenErr, emptyTokenRepository.claimed)
	}
}

// TestBatchRecoveryCoversPostClaimFailures 验证接管后重置、明细查询和 worker 启动失败的租约补偿。
func TestBatchRecoveryCoversPostClaimFailures(t *testing.T) {
	// pendingError 是接管后明细查询错误。
	pendingError := errors.New("pending error")
	// pendingRepository 保存明细查询失败的恢复批次。
	pendingRepository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "pending", UserID: 1, Status: "running"}}, pendingErr: pendingError}
	// pendingService 是明细查询失败场景使用的恢复服务。
	pendingService, err := NewBatchRecoveryService(pendingRepository, BatchRecoveryOptions{NewWorkerToken: func() string { return "pending-worker" }})
	if err != nil {
		t.Fatal(err)
	}
	// pendingRunErr 保存明细查询失败后的扫描结果。
	pendingRunErr := pendingService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { return nil })
	if pendingRunErr != nil || len(pendingRepository.released) != 1 {
		t.Fatalf("明细失败结果=%v released=%v", pendingRunErr, pendingRepository.released)
	}
	// startError 是生命周期协调器启动 worker 返回的错误。
	startError := errors.New("start error")
	// startRepository 保存 worker 启动失败的恢复批次。
	startRepository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "start", UserID: 2, Status: "running"}}, pending: map[string][]BatchRow{"start": {{ID: 1}}}}
	// startService 是 worker 启动失败场景使用的恢复服务。
	startService, err := NewBatchRecoveryService(startRepository, BatchRecoveryOptions{NewWorkerToken: func() string { return "start-worker" }})
	if err != nil {
		t.Fatal(err)
	}
	// startRunErr 保存 worker 启动失败后的扫描结果。
	startRunErr := startService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { return startError })
	if startRunErr != nil || len(startRepository.released) != 1 {
		t.Fatalf("worker 启动失败结果=%v released=%v", startRunErr, startRepository.released)
	}
	// recountError 是统计重算错误，恢复流程应继续使用待处理明细作为权威结果。
	recountError := errors.New("recount error")
	// recountRepository 保存统计重算失败但仍有明细的批次。
	recountRepository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "recount", UserID: 3, Status: "running"}}, pending: map[string][]BatchRow{"recount": {{ID: 2}}}, recountErr: recountError}
	// recountService 是统计重算失败兼容场景的恢复服务。
	recountService, err := NewBatchRecoveryService(recountRepository, BatchRecoveryOptions{NewWorkerToken: func() string { return "recount-worker" }})
	if err != nil {
		t.Fatal(err)
	}
	// startedCount 统计统计重算失败后仍然启动的 worker。
	startedCount := 0
	// recountRunErr 保存统计重算失败但恢复继续的扫描结果。
	recountRunErr := recountService.RecoverWithStarter(context.Background(), func(context.Context, int64, string, string) error { startedCount++; return nil })
	if recountRunErr != nil || startedCount != 1 {
		t.Fatalf("统计重算失败恢复结果=%v started=%d", recountRunErr, startedCount)
	}
}

// TestBatchRecoveryStopsWhenContextCanceledDuringScan 验证恢复扫描在批次间尊重取消并返回上下文错误。
func TestBatchRecoveryStopsWhenContextCanceledDuringScan(t *testing.T) {
	// repository 保存两个待恢复批次，用于触发批次间取消检查。
	repository := &batchRecoveryRepositoryFake{batches: []BatchInfo{{ID: "first", UserID: 1, Status: "running"}, {ID: "second", UserID: 1, Status: "running"}}, pending: map[string][]BatchRow{"first": {{ID: 1}}, "second": {{ID: 2}}}}
	// service 是可取消恢复扫描的服务。
	service, err := NewBatchRecoveryService(repository, BatchRecoveryOptions{NewWorkerToken: func() string { return "worker" }})
	if err != nil {
		t.Fatal(err)
	}
	// ctx、cancel 是扫描过程中由 worker 回调触发取消的上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// runErr 保存扫描遇到取消后的返回错误。
	runErr := service.RecoverWithStarter(ctx, func(context.Context, int64, string, string) error { cancel(); return nil })
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("取消扫描错误=%v", runErr)
	}
}
