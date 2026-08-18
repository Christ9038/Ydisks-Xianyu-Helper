package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"xianyu-go/internal/adapter"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
)

// freeAddr 获取一个空闲 TCP 端口（立即释放，供测试绑定）。
func freeAddr(t *testing.T) string {
	t.Helper()
	// l、err 用于本次流程后续判断的l、err
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// addr 用于本次流程后续判断的addr
	addr := l.Addr().String()
	l.Close()
	return addr
}

// TestRun_ServesHealthAndShutdowns 启动 HTTP 服务，/health 可访问，ctx 取消后优雅退出。
func TestRun_ServesHealthAndShutdowns(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// runDone 用于本次流程后续判断的运行Done
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// 轮询 /health 直到可用（最多 3s）。
	url := "http://" + srv.Addr + "/health"
	// ok 用于本次流程后续判断的ok
	var ok bool
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// resp、err 用于本次流程后续判断的resp、err
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ok = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		cancel()
		t.Fatal("Run 启动后 /health 3s 内不可访问")
	}

	// 取消 ctx → Run 应优雅退出。
	cancel()
	select {
	case // err 用于本次流程后续判断的err
	err := <-runDone:
		if err != nil {
			t.Fatalf("Run 应返回 nil，got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未在 ctx 取消后 5s 内退出")
	}
}

// TestPublishRecoveryLifecycleStopsBeforeWorkerWait 封装Test发布RecoveryLifecycleStopsBefore工作器Wait业务协调。
func TestPublishRecoveryLifecycleStopsBeforeWorkerWait(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// err 表示测试批量恢复组件启动失败的原因。
	if err := srv.applications.itemBatchCoordinator.StartRecovery(ctx); err != nil {
		t.Fatalf("启动批量发布恢复扫描器: %v", err)
	}
	cancel()
	// done 用于本次流程后续判断的done
	done := make(chan struct{})
	go func() {
		srv.applications.itemBatchCoordinator.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("批量发布恢复扫描器关闭后没有退出")
	}
}

// TestNewRejectsMissingRequiredDependencies 确保 HTTP 服务构造阶段拒绝缺失核心依赖。
func TestNewRejectsMissingRequiredDependencies(t *testing.T) {
	// err 是缺少基础设施容器时的构造校验错误。
	if _, err := New(nil, nil, "", ":0", nil, nil, nil); err == nil {
		t.Fatal("缺少基础设施容器时应返回构造错误")
	}
	// srv 是用于提供合法 Store 的测试服务；cleanup 负责释放测试数据库。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	if srv == nil {
		t.Fatal("测试服务不应为空")
	}
	// authentication 保存构造校验需要的会话中间件依赖。
	authentication := &auth.Service{Store: store}
	// err 是缺少 Manager 时的构造校验错误。
	if _, err := New(authentication, nil, "", ":0", nil, nil, nil); err == nil {
		t.Fatal("缺少 account.Manager 时应返回构造错误")
	}
	// err 是缺少订单专用装配能力时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil); err == nil {
		t.Fatal("缺少订单专用依赖时应返回构造错误")
	}
	// orderDependencies 保存账号依赖缺失测试所需的合法订单装配能力。
	orderDependencies, orderDependencyErr := adapter.NewOrderDependencies(store)
	if orderDependencyErr != nil {
		t.Fatalf("NewOrderDependencies: %v", orderDependencyErr)
	}
	// err 是缺少账号专用装配能力时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithOrderDependencies(orderDependencies)); err == nil {
		t.Fatal("缺少账号专用依赖时应返回构造错误")
	}
	// accountDependencies 保存账号与订单依赖均合法时的商品依赖缺失测试输入。
	accountDependencies, accountDependencyErr := adapter.NewAccountDependencies(store)
	if accountDependencyErr != nil {
		t.Fatalf("NewAccountDependencies: %v", accountDependencyErr)
	}
	// err 是缺少商品专用装配能力时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies)); err == nil {
		t.Fatal("缺少商品专用依赖时应返回构造错误")
	}
	// err 是订单、账号和商品依赖齐全但平台客户端缺失时的构造校验错误。
	itemDependencies, itemDependencyErr := adapter.NewItemDependencies(store)
	if itemDependencyErr != nil {
		t.Fatalf("NewItemDependencies: %v", itemDependencyErr)
	}
	// platformDependencies 保存依赖缺失分支测试使用的显式平台能力。
	platformDependencies, platformDependencyErr := adapter.NewDefaultPlatformDependencies(nil)
	if platformDependencyErr != nil {
		t.Fatalf("NewPlatformDependencies: %v", platformDependencyErr)
	}
	// chatDependencies 保存平台缺失校验所需的聊天专用依赖。
	chatDependencies := adapter.NewChatDependencies(store)
	// systemDependencies 保存平台缺失校验所需的系统专用依赖。
	systemDependencies := adapter.NewSystemDependencies(store)
	// databaseHealth 是进程组合根提供给 Server 的数据库健康检查端口。
	databaseHealth := systemDependencies.NewDatabaseHealth()
	// automationDependencies 保存平台缺失校验所需的自动化专用依赖及构造错误。
	automationDependencies, automationDependencyErr := adapter.NewAutomationDependencies(store)
	if automationDependencyErr != nil {
		t.Fatalf("NewAutomationDependencies: %v", automationDependencyErr)
	}
	// miscDependencies 保存平台缺失校验所需的杂项专用依赖及构造错误。
	miscDependencies, miscDependencyErr := adapter.NewMiscDependencies(store)
	if miscDependencyErr != nil {
		t.Fatalf("NewMiscDependencies: %v", miscDependencyErr)
	}
	// adminSettingsDependencies 保存平台缺失校验所需的管理员设置依赖。
	adminSettingsDependencies := adapter.NewAdminSettingsDependencies(store)
	if chatDependencies == nil || systemDependencies == nil || databaseHealth == nil || adminSettingsDependencies == nil {
		t.Fatal("显式 Server 依赖构造失败")
	}
	// transportApplications 是构造失败测试使用的完整 transport-facing 服务集合。
	transportApplications, transportApplicationsErr := adapter.NewTransportApplicationServices(adapter.TransportApplicationServiceOptions{
		AutomationDependencies:    automationDependencies,
		MiscDependencies:          miscDependencies,
		AdminSettingsDependencies: adminSettingsDependencies,
		AccountTaskRunner:         adapter.NewAccountTaskRunner(nil),
		ModelClient:               adapter.NewAIModelClient(),
	})
	if transportApplicationsErr != nil {
		t.Fatalf("NewTransportApplicationServices: %v", transportApplicationsErr)
	}
	// orderReconciliationRecovery 是完整构造路径需要的订单补偿扫描应用服务。
	orderReconciliationRecovery, reconciliationErr := orderapp.NewReconciliationRecoveryCoordinator(systemDependencies.NewReconciliationService(nil))
	if reconciliationErr != nil {
		t.Fatalf("NewReconciliationRecoveryCoordinator: %v", reconciliationErr)
	}
	// err 是聊天或健康检查端口等显式依赖缺失时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies)); err == nil {
		t.Fatal("缺少聊天或健康检查端口时应返回构造错误")
	}
	// err 是全部业务依赖齐全但遗漏健康检查端口时的构造错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithOrderReconciliationRecovery(orderReconciliationRecovery), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithTransportApplicationServices(transportApplications), WithPlatformDependencies(platformDependencies)); err == nil {
		t.Fatal("缺少数据库健康检查端口时应返回构造错误")
	}
	// err 是缺少平台依赖注入时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithDatabaseHealth(databaseHealth), WithOrderReconciliationRecovery(orderReconciliationRecovery), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithTransportApplicationServices(transportApplications)); err == nil {
		t.Fatal("缺少平台依赖时应返回构造错误")
	}
	// err 是缺少自动化专用依赖时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithDatabaseHealth(databaseHealth), WithOrderReconciliationRecovery(orderReconciliationRecovery), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithTransportApplicationServices(transportApplications), WithPlatformDependencies(platformDependencies)); err == nil {
		t.Fatal("缺少自动化专用依赖时应返回构造错误")
	}
	// err 是缺少 transport 应用服务集合时的构造校验错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithDatabaseHealth(databaseHealth), WithOrderReconciliationRecovery(orderReconciliationRecovery), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithPlatformDependencies(platformDependencies)); err == nil {
		t.Fatal("缺少 transport 应用服务集合时应返回构造错误")
	}
	// err 是全部基础设施依赖齐全但遗漏进程装配应用服务时的构造错误。
	if _, err := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithDatabaseHealth(databaseHealth), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithTransportApplicationServices(transportApplications), WithPlatformDependencies(platformDependencies)); err == nil {
		t.Fatal("缺少订单补偿扫描应用服务时应返回构造错误")
	}
	// fullyConstructedServer、constructErr 分别是完整注入后的 HTTP 服务及构造错误。
	fullyConstructedServer, constructErr := New(authentication, srv.Manager, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithDatabaseHealth(databaseHealth), WithOrderReconciliationRecovery(orderReconciliationRecovery), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithTransportApplicationServices(transportApplications), WithPlatformDependencies(platformDependencies))
	if constructErr != nil || fullyConstructedServer == nil {
		t.Fatalf("完整依赖注入应构造成功: server=%v err=%v", fullyConstructedServer, constructErr)
	}
}

// TestServerStartStopIsIdempotentAndWaitsForWorkers 验证 Start/Stop 可重复调用且 Stop 等待 worker。
func TestServerStartStopIsIdempotentAndWaitsForWorkers(t *testing.T) {
	// srv 是待验证幂等生命周期的 HTTP 服务；cleanup 负责释放测试资源。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)
	if // err 用于本次流程后续判断的err
	err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := srv.Start(context.Background()); err != nil {
		t.Fatalf("重复 Start: %v", err)
	}
	// workerDone 是模拟批量发布 worker 完成时调用的释放函数。
	workerDone := srv.beginWorker()
	// stopDone 是显式 Stop 完成时关闭的测试信号。
	stopDone := make(chan struct{})
	go func() {
		// 显式停止过程的错误不影响本测试对等待语义的判断。
		_ = srv.Stop(context.Background())
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop 在 worker 完成前提前返回")
	case <-time.After(50 * time.Millisecond):
	}
	workerDone()
	select {
	case <-stopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 未在 worker 完成后返回")
	}
	// err 是重复 Stop 返回的关闭错误。
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("重复 Stop: %v", err)
	}
}

// TestServerStopContextBoundsWorkerWait 验证关闭上下文到期时不会无限等待后台 worker。
func TestServerStopContextBoundsWorkerWait(t *testing.T) {
	// srv 是用于验证关闭超时的 HTTP 服务；cleanup 负责释放测试数据库。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)
	// err 表示 HTTP 服务启动失败。
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// workerDone 保持 worker 未完成，模拟不响应关闭的后台任务。
	workerDone := srv.beginWorker()
	// stopCtx 是刻意很短的关闭上下文，用于验证 Stop 的等待边界。
	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	// started 记录停止开始时间，用于验证上下文确实限制等待时长。
	started := time.Now()
	// err 表示 worker 未完成时停止上下文到期返回的错误。
	err := srv.Stop(stopCtx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error=%v, want deadline exceeded", err)
	}
	// elapsed 表示停止调用实际耗时。
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Stop waited too long: %s", elapsed)
	}
	workerDone()
	// err 表示释放 worker 后第二次幂等停止的返回错误。
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
