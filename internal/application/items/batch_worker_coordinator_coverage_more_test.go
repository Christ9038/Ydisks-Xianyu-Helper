package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBatchWorkerCoordinatorCoversConstructionAndGuards 验证协调器构造、输入校验和恢复入口边界。
func TestBatchWorkerCoordinatorCoversConstructionAndGuards(t *testing.T) {
	// runner 是用于构造协调器的最小批次 runner。
	runner, runnerErr := NewBatchRunner(&batchRunnerRepository{}, batchCoordinatorEmptyPublisher{}, BatchRunOptions{})
	if runnerErr != nil {
		t.Fatal(runnerErr)
	}
	// recovery 是用于构造协调器的最小恢复服务。
	recovery, recoveryErr := NewBatchRecoveryService(&batchRecoveryRepositoryFake{}, BatchRecoveryOptions{StartWorker: func(context.Context, int64, string, string) {}})
	if recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	// missingRunnerErr 保存缺失 runner 的构造错误。
	_, missingRunnerErr := NewBatchWorkerCoordinator(nil, recovery, BatchWorkerCoordinatorOptions{})
	if missingRunnerErr == nil {
		t.Fatal("缺失 runner 应构造失败")
	}
	// missingRecoveryErr 保存缺失恢复服务的构造错误。
	_, missingRecoveryErr := NewBatchWorkerCoordinator(runner, nil, BatchWorkerCoordinatorOptions{})
	if missingRecoveryErr == nil {
		t.Fatal("缺失恢复服务应构造失败")
	}
	// coordinator 是完整依赖构造的协调器。
	coordinator, err := NewBatchWorkerCoordinator(runner, recovery, BatchWorkerCoordinatorOptions{WorkerTimeout: time.Second, RecoveryInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	// invalidStartCases 保存 worker 启动输入错误。
	invalidStartCases := []struct {
		// name 是当前输入错误场景名称。
		name string
		// userID 是测试用户标识。
		userID int64
		// batchID 是测试批次标识。
		batchID string
		// token 是测试租约令牌。
		token string
	}{
		{name: "user", userID: 0, batchID: "batch", token: "token"},
		{name: "batch", userID: 1, batchID: "", token: "token"},
		{name: "token", userID: 1, batchID: "batch", token: ""},
	}
	// testCase 表示当前 worker 启动输入场景。
	for _, testCase := range invalidStartCases {
		t.Run(testCase.name, func(t *testing.T) {
			// startErr 保存当前输入校验错误。
			startErr := coordinator.Start(context.Background(), testCase.userID, testCase.batchID, testCase.token)
			if !errors.Is(startErr, ErrBatchWorkerCoordinatorInvalidJob) {
				t.Fatalf("启动错误=%v", startErr)
			}
		})
	}
	// nilContextStartErr 保存空生命周期上下文错误。
	nilContextStartErr := coordinator.Start(nilItemsContext(), 1, "batch", "token")
	if nilContextStartErr == nil {
		t.Fatal("空生命周期上下文应失败")
	}
	// nilRecoveryErr 保存空恢复上下文错误。
	nilRecoveryErr := coordinator.RunRecovery(nilItemsContext())
	if nilRecoveryErr == nil {
		t.Fatal("空恢复上下文应失败")
	}
	// nilRecoveryParentErr 保存恢复循环缺少生命周期父 Context 的错误。
	nilRecoveryParentErr := coordinator.StartRecovery(nilItemsContext())
	if nilRecoveryParentErr == nil {
		t.Fatal("空恢复生命周期 Context 应失败")
	}
	// recoveryRunErr 保存正常恢复入口结果。
	recoveryRunErr := coordinator.RunRecovery(context.Background())
	if recoveryRunErr != nil {
		t.Fatalf("恢复入口错误=%v", recoveryRunErr)
	}
	// duplicateRecoveryErr 保存重复启动恢复循环的兼容结果。
	duplicateRecoveryErr := coordinator.StartRecovery(context.Background())
	if duplicateRecoveryErr != nil {
		t.Fatal(duplicateRecoveryErr)
	}
	if duplicateRecoveryErr = coordinator.StartRecovery(context.Background()); duplicateRecoveryErr != nil {
		t.Fatal(duplicateRecoveryErr)
	}
	// closeErr 保存协调器关闭结果。
	closeErr := coordinator.Close(context.Background())
	if closeErr != nil {
		t.Fatalf("关闭协调器错误=%v", closeErr)
	}
	// nilCloseErr 保存空关闭上下文边界错误。
	nilCloseErr := coordinator.Close(nilItemsContext())
	if nilCloseErr == nil {
		t.Fatal("空关闭上下文应失败")
	}
	// nilCoordinator 验证空协调器生命周期方法保持幂等。
	var nilCoordinator *BatchWorkerCoordinator
	nilCoordinator.Wait()
	nilCoordinator.Stop()
	// nilCoordinatorCloseErr 保存空协调器关闭结果。
	nilCoordinatorCloseErr := nilCoordinator.Close(context.Background())
	if nilCoordinatorCloseErr != nil {
		t.Fatalf("空协调器关闭错误=%v", nilCoordinatorCloseErr)
	}
}

// TestBatchWorkerCoordinatorCoversNilRecoveryAndActiveCounterGuards 覆盖恢复入口缺失依赖和活动计数防御分支。
func TestBatchWorkerCoordinatorCoversNilRecoveryAndActiveCounterGuards(t *testing.T) {
	// nilCoordinator 表示恢复协调器接收者为空。
	var nilCoordinator *BatchWorkerCoordinator
	if nilCoordinator.RunRecovery(context.Background()) == nil || nilCoordinator.StartRecovery(context.Background()) == nil {
		t.Fatal("nil coordinator should reject recovery operations")
	}
	// incompleteCoordinator 表示 runner 或 recovery 依赖未装配的协调器。
	incompleteCoordinator := &BatchWorkerCoordinator{}
	if incompleteCoordinator.RunRecovery(context.Background()) == nil || incompleteCoordinator.StartRecovery(context.Background()) == nil {
		t.Fatal("incomplete coordinator should reject recovery operations")
	}
	// idleCoordinator 保存活动计数为零的协调器，直接调用收口保护不得关闭不存在的信号。
	idleCoordinator := &BatchWorkerCoordinator{done: make(chan struct{})}
	idleCoordinator.finishActiveLocked()
	select {
	case <-idleCoordinator.done:
		t.Fatal("idle active counter should not close done")
	default:
	}
	// activeCoordinator 保存一个活动任务及其未关闭完成信号。
	activeCoordinator := &BatchWorkerCoordinator{active: 1, done: make(chan struct{})}
	activeCoordinator.finishActiveLocked()
	select {
	case <-activeCoordinator.done:
	default:
		t.Fatal("last active task should close done")
	}
}

// TestBatchWorkerCoordinatorCoversLifecycleErrorBranches 覆盖协调器启动、取消、重复停止和关闭超时分支。
func TestBatchWorkerCoordinatorCoversLifecycleErrorBranches(t *testing.T) {
	// nilCoordinator 表示未初始化的协调器接收者。
	var nilCoordinator *BatchWorkerCoordinator
	if nilCoordinator.Start(context.Background(), 1, "batch", "token") == nil {
		t.Fatal("未初始化协调器不应启动 worker")
	}
	if nilCoordinator.Cancel("batch", "token") {
		t.Fatal("未初始化协调器不应取消 worker")
	}
	// coordinator 保存用于验证重复停止的完整协调器。
	coordinator, _ := newBatchWorkerCoordinatorFixture(t, batchCoordinatorEmptyPublisher{})
	coordinator.Stop()
	coordinator.Stop()

	// recoveryRepository 保存会触发协调器启动回调的恢复批次。
	recoveryRepository := &batchRecoveryRepositoryFake{
		batches: []BatchInfo{{ID: "recovery-start", UserID: 7, Status: "running"}},
		pending: map[string][]BatchRow{"recovery-start": {{ID: 1}}},
	}
	// recovery 保存使用固定令牌的恢复服务。
	recovery, recoveryErr := NewBatchRecoveryService(recoveryRepository, BatchRecoveryOptions{
		NewWorkerToken: func() string { return "recovery-token" },
		StartWorker:    func(context.Context, int64, string, string) {},
	})
	if recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	// runner 保存协调器回调启动的最小批次执行器。
	runner, runnerErr := NewBatchRunner(&batchRunnerRepository{batch: BatchInfo{ID: "recovery-start", UserID: 7, Status: "running", WorkerToken: "recovery-token"}}, batchCoordinatorEmptyPublisher{}, BatchRunOptions{})
	if runnerErr != nil {
		t.Fatal(runnerErr)
	}
	// recoveryCoordinator 保存会接管恢复 worker 的协调器。
	recoveryCoordinator, coordinatorErr := NewBatchWorkerCoordinator(runner, recovery, BatchWorkerCoordinatorOptions{})
	if coordinatorErr != nil {
		t.Fatal(coordinatorErr)
	}
	// err 保存恢复 worker 启动回调的执行结果。
	if err := recoveryCoordinator.RunRecovery(context.Background()); err != nil {
		t.Fatalf("恢复启动回调失败: %v", err)
	}
	recoveryCoordinator.Wait()

	// timeoutCoordinator 保存仍有活动计数但没有后台任务的关闭边界对象。
	timeoutCoordinator := &BatchWorkerCoordinator{active: 1, done: make(chan struct{}), workers: make(map[string]*batchWorkerHandle)}
	// canceledClose 保存立即取消的关闭上下文。
	canceledClose, cancelClose := context.WithCancel(context.Background())
	cancelClose()
	// err 保存取消 Context 下的协调器关闭结果。
	if err := timeoutCoordinator.Close(canceledClose); !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭超时错误=%v", err)
	}
}

// TestBatchWorkerCoordinatorCoversRecoveryTicker 覆盖恢复循环首次扫描和定时扫描的错误观测。
func TestBatchWorkerCoordinatorCoversRecoveryTicker(t *testing.T) {
	// scanErr 保存每次恢复扫描返回的基础设施错误。
	scanErr := errors.New("定时恢复扫描失败")
	// recovery 保存会持续返回扫描错误的恢复服务。
	recovery, recoveryErr := NewBatchRecoveryService(&batchRecoveryRepositoryFake{scanErr: scanErr}, BatchRecoveryOptions{StartWorker: func(context.Context, int64, string, string) {}})
	if recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	// runner 保存恢复循环所需的最小批次执行器。
	runner, runnerErr := NewBatchRunner(&batchRunnerRepository{}, batchCoordinatorEmptyPublisher{}, BatchRunOptions{})
	if runnerErr != nil {
		t.Fatal(runnerErr)
	}
	// observed 保存首次扫描和定时扫描的错误观测结果。
	observed := make(chan error, 4)
	// coordinator 保存使用极短扫描间隔的恢复协调器。
	coordinator, coordinatorErr := NewBatchWorkerCoordinator(runner, recovery, BatchWorkerCoordinatorOptions{
		RecoveryInterval: time.Millisecond,
		OnRecoveryError:  func(err error) { observed <- err },
	})
	if coordinatorErr != nil {
		t.Fatal(coordinatorErr)
	}
	// err 保存定时恢复循环的启动结果。
	if err := coordinator.StartRecovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	// observedCount 统计至少一次首次扫描和一次定时扫描错误。
	observedCount := 0
	// deadline 保存恢复错误观测测试的最长等待信号。
	deadline := time.After(time.Second)
	for observedCount < 2 {
		select {
		// observedErr 保存当前恢复扫描错误观测结果。
		case observedErr := <-observed:
			if !errors.Is(observedErr, scanErr) {
				t.Fatalf("恢复观测错误=%v", observedErr)
			}
			observedCount++
		case <-deadline:
			t.Fatal("未收到定时恢复扫描错误")
		}
	}
	// err 保存恢复循环的关闭结果。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
