package server

import "testing"

// TestNewServerAssemblesApplicationServices 验证 Server 构造时统一装配全部应用服务。
func TestNewServerAssemblesApplicationServices(t *testing.T) {
	// srv 是使用测试依赖创建的 Server。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	if srv.applications == nil {
		t.Fatal("Server 应在构造时装配应用服务集合")
	}
	// services 是统一应用服务集合。
	services := srv.applicationServiceSet()
	if services.orders == nil || services.orderRefreshJobs == nil || services.itemBatchCoordinator == nil || services.itemSinglePublish == nil || services.itemCatalog == nil || services.itemCatalogMutation == nil || services.accountLogin == nil || services.authentication == nil || services.loginAudit == nil || services.passwordLogin == nil || services.accountDelete == nil || services.accountSettings == nil || services.uncertainNotifications == nil || services.notificationChannels == nil || services.analytics == nil || services.cards == nil || services.publishAutomationRules == nil || services.defaultReplies == nil || services.keywords == nil {
		t.Fatal("应用服务集合存在未装配的服务")
	}
	if services.orders.services == nil || services.orders.services.List == nil || services.orders.services.Detail == nil || services.orders.services.Refresh == nil || services.orders.services.RefreshJobs == nil {
		t.Fatal("订单应用服务应由应用层 ServiceSet 完整装配")
	}
	if srv.orders() != services.orders || srv.itemCatalogApplication() != services.itemCatalog || srv.itemCatalogMutationApplication() != services.itemCatalogMutation || srv.accountLoginApplication() != services.accountLogin || srv.analyticsApplication() != services.analytics || srv.defaultReplyApplication() != services.defaultReplies || srv.keywordApplication() != services.keywords {
		t.Fatal("应用服务访问器未返回统一装配实例")
	}
}

// TestApplicationLifecycleComponentsExposeWorkerOwnership 验证 Server 只暴露应用 worker 生命周期端口，不再提供启动入口。
func TestApplicationLifecycleComponentsExposeWorkerOwnership(t *testing.T) {
	// srv、cleanup 保存带完整应用服务装配的测试 Server 及其资源清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// components 保存交给进程生命周期协调器登记的应用 worker 组件。
	components := srv.ApplicationLifecycleComponents()
	if len(components) != 3 {
		t.Fatalf("应用生命周期组件数量=%d，期望=3", len(components))
	}
	// names 保存组件稳定名称，顺序同时表达启动依赖与关闭逆序。
	names := make([]string, 0, len(components))
	// component 表示当前应用 worker 生命周期组件。
	for _, component := range components {
		names = append(names, component.Name)
		if component.Component == nil {
			t.Fatalf("组件 %q 缺少生命周期实现", component.Name)
		}
	}
	// want 是 cmd 应登记的应用 worker 稳定名称与启动顺序。
	want := []string{"order-refresh-recovery", "publish-batch-workers", "order-reconciliation-recovery"}
	if len(names) != len(want) {
		t.Fatalf("组件名称数量=%d，期望=%d", len(names), len(want))
	}
	// index 表示当前比较的生命周期组件下标。
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("组件名称[%d]=%q，期望=%q", index, names[index], want[index])
		}
	}
}
