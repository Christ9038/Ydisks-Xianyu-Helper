package orders

import (
	"context"
	"errors"
	"testing"
)

// TestOrderImportAndListCoverRepositoryBoundaries 验证订单导入与列表服务的缺失依赖、归属查询和默认账号分支。
func TestOrderImportAndListCoverRepositoryBoundaries(t *testing.T) {
	// nilImportService 保存空接收者导入服务。
	var nilImportService *ImportService
	// nilImportErr 保存空导入服务的初始化错误。
	_, nilImportErr := nilImportService.Import(context.Background(), 1, nil)
	if nilImportErr == nil {
		t.Fatal("空导入服务不应成功")
	}
	// listErr 是账号列表查询的底层错误。
	listErr := errors.New("owned account lookup failed")
	// listFailureService 保存账号归属查询失败的导入服务。
	listFailureService := NewImportService(&importRepositoryFake{listErr: listErr})
	// listFailureErr 保存账号列表查询错误。
	_, listFailureErr := listFailureService.Import(context.Background(), 1, nil)
	if !errors.Is(listFailureErr, listErr) {
		t.Fatalf("导入账号列表错误=%v", listFailureErr)
	}
	// noItemRepository 保存只有账号字段的导入事务替身。
	noItemRepository := &importRepositoryFake{ownedIDs: []string{"account"}}
	// noItemService 保存不需要补全商品的导入服务。
	noItemService := NewImportService(noItemRepository)
	// noItemResult、noItemErr 保存无商品订单导入结果。
	noItemResult, noItemErr := noItemService.Import(context.Background(), 1, []ImportOrder{{OrderID: "order", CookieID: "account", Amount: "0"}})
	if noItemErr != nil || noItemResult.SuccessCount != 1 {
		t.Fatalf("无商品订单导入结果=%+v err=%v", noItemResult, noItemErr)
	}
	// ownerErr 是订单列表账号归属查询错误。
	ownerErr := errors.New("ownership lookup failed")
	// ownerFailureService 保存账号归属查询失败的列表服务。
	ownerFailureService := NewListService(&listRepositoryStub{ownerErr: ownerErr})
	// ownerFailureErr 保存账号归属查询错误。
	_, ownerFailureErr := ownerFailureService.List(context.Background(), ListQuery{UserID: 1, CookieID: "account"})
	if !errors.Is(ownerFailureErr, ownerErr) {
		t.Fatalf("列表归属查询错误=%v", ownerFailureErr)
	}
	// nilListService 保存空接收者列表服务。
	var nilListService *ListService
	// nilListErr 保存空列表服务初始化错误。
	_, nilListErr := nilListService.List(context.Background(), ListQuery{UserID: 1})
	if nilListErr == nil {
		t.Fatal("空列表服务不应成功")
	}
}

// TestReconciliationCoordinatorCoversNilAndCloseTimeout 验证补偿协调器的空接收者和关闭超时边界。
func TestReconciliationCoordinatorCoversNilAndCloseTimeout(t *testing.T) {
	// nilCoordinator 保存空接收者生命周期协调器。
	var nilCoordinator *ReconciliationRecoveryCoordinator
	nilCoordinator.Stop()
	nilCoordinator.Wait()
	// startErr 保存空协调器启动错误。
	if startErr := nilCoordinator.Start(context.Background()); startErr == nil {
		t.Fatal("空协调器不应启动")
	}
	// closeErr 保存空协调器关闭结果。
	if closeErr := nilCoordinator.Close(context.Background()); closeErr != nil {
		t.Fatalf("空协调器关闭错误=%v", closeErr)
	}
	// recovery 保存可观测的长期补偿扫描器。
	recovery := &reconciliationRecoveryFake{started: make(chan struct{}), stopped: make(chan struct{})}
	// coordinator 保存运行中的补偿协调器。
	coordinator, coordinatorErr := NewReconciliationRecoveryCoordinator(recovery)
	if coordinatorErr != nil {
		t.Fatal(coordinatorErr)
	}
	// startErr 保存补偿协调器启动错误。
	if startErr := coordinator.Start(context.Background()); startErr != nil {
		t.Fatal(startErr)
	}
	<-recovery.started
	// canceledContext 保存已经取消的关闭上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// timeoutErr 保存关闭等待超时错误。
	timeoutErr := coordinator.Close(canceledContext)
	if !errors.Is(timeoutErr, context.Canceled) {
		t.Fatalf("关闭超时错误=%v", timeoutErr)
	}
	// closeErr 保存测试运行器的最终关闭结果。
	if closeErr := coordinator.Close(context.Background()); closeErr != nil {
		t.Fatalf("最终关闭错误=%v", closeErr)
	}
}

// TestRefreshJobServiceCoversFacadeGuards 验证刷新任务 facade 的空接收者、参数和账号归属边界。
func TestRefreshJobServiceCoversFacadeGuards(t *testing.T) {
	// nilService 保存空刷新任务 facade。
	var nilService *RefreshJobService
	// getErr 保存空 facade 查询错误。
	if _, getErr := nilService.GetJob(context.Background(), 1, "job"); getErr == nil {
		t.Fatal("空 facade 查询不应成功")
	}
	// cancelResult、cancelErr 保存空 facade 取消结果和错误。
	if cancelResult, cancelErr := nilService.CancelForUser(context.Background(), 1, "job"); cancelErr == nil || cancelResult.Cancelled {
		t.Fatalf("空 facade 取消结果=%+v err=%v", cancelResult, cancelErr)
	}
	// repository 保存任务服务构造所需的内存仓储。
	repository := completeAppliedRepository()
	// runner 保存最小刷新任务运行器。
	runner, runnerErr := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if runnerErr != nil {
		t.Fatal(runnerErr)
	}
	// service 保存完整依赖的刷新任务 facade。
	service, serviceErr := NewRefreshJobService(repository, &refreshJobOwnerTestDouble{}, runner, RefreshJobServiceOptions{})
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// invalidGetErr 保存缺少请求上下文时的查询错误。
	if _, invalidGetErr := service.GetJob(nilRefreshJobContext(), 1, "job"); !errors.Is(invalidGetErr, ErrRefreshJobNotFound) {
		t.Fatalf("无效查询参数错误=%v", invalidGetErr)
	}
	// closeErr 保存刷新任务运行器的最终关闭结果。
	if closeErr := runner.Close(context.Background()); closeErr != nil {
		t.Fatalf("运行器关闭错误=%v", closeErr)
	}
}
