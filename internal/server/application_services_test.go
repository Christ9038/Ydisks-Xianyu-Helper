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
	if services.orders == nil || services.itemPublish == nil || services.accountLogin == nil || services.communication == nil || services.analytics == nil {
		t.Fatal("应用服务集合存在未装配的服务")
	}
	if srv.orders() != services.orders || srv.itemPublishApplication() != services.itemPublish || srv.accountLoginApplication() != services.accountLogin || srv.communicationApplication() != services.communication || srv.analyticsApplication() != services.analytics {
		t.Fatal("应用服务访问器未返回统一装配实例")
	}
}
