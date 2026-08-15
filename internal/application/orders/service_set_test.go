package orders

import "testing"

// TestNewServiceSetConstructsAllServices 验证订单应用服务集合由应用层完整构造。
func TestNewServiceSetConstructsAllServices(t *testing.T) {
	// services 保存应用层统一构造的订单服务集合。
	services := NewServiceSet(nil, nil, nil, nil, nil, 100)
	if services == nil || services.List == nil || services.Detail == nil || services.Delete == nil || services.Update == nil || services.Import == nil || services.ManualShip == nil || services.Refresh == nil {
		t.Fatalf("订单应用服务集合未完整构造: %+v", services)
	}
}
