package composition

import "testing"

// TestNewRejectsIncompleteDependencies 验证组合根会拒绝缺失的必需基础设施，避免半初始化服务进入 transport。
func TestNewRejectsIncompleteDependencies(t *testing.T) {
	// services、buildErr 分别是缺失依赖时的构造结果和预期错误。
	services, buildErr := New(Dependencies{})
	if buildErr == nil || services != nil {
		t.Fatalf("缺少组合依赖时应失败: services=%v err=%v", services, buildErr)
	}
}

// TestServicesLifecycleComponentsNilSafe 验证空组合服务不会伪造后台组件所有权。
func TestServicesLifecycleComponentsNilSafe(t *testing.T) {
	// services 是空组合服务指针；components 是其不应伪造的生命周期组件列表。
	var services *Services
	// components 是空服务返回的生命周期组件，空组合根不得声明后台组件所有权。
	if components := services.LifecycleComponents(); components != nil {
		t.Fatalf("空服务不应返回生命周期组件: %v", components)
	}
}
