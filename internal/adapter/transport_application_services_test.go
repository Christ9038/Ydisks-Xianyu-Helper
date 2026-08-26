package adapter

import (
	"testing"

	"xianyu-go/internal/db"
)

// TestNewTransportApplicationServicesRejectsMissingDependencies 确保组合根不会接受缺失领域依赖的服务集合。
func TestNewTransportApplicationServicesRejectsMissingDependencies(t *testing.T) {
	// services、err 分别保存缺少自动化依赖时的服务集合和构造错误。
	services, err := NewTransportApplicationServices(TransportApplicationServiceOptions{})
	if services != nil {
		t.Fatal("缺少依赖时不应返回 transport 应用服务集合")
	}
	if err == nil {
		t.Fatal("缺少依赖时应返回构造错误")
	}
	// services 保存显式构造但字段不完整的集合，用于验证 Server 前置校验可复用。
	services = &TransportApplicationServices{}
	// validationErr 表示不完整服务集合未被前置校验拒绝。
	if validationErr := services.Validate(); validationErr == nil {
		t.Fatal("不完整服务集合应验证失败")
	}
}

// TestNewTransportApplicationServicesRejectsEachMissingRequiredDependency 验证每个构造期必需依赖缺失时都能立即失败。
func TestNewTransportApplicationServicesRejectsEachMissingRequiredDependency(t *testing.T) {
	// store 是只用于创建 typed 依赖的空数据库入口。
	store := db.NewStore(nil, db.DialectSQLite)
	// automationDependencies、automationErr 保存自动化领域依赖及构造错误。
	automationDependencies, automationErr := NewAutomationDependencies(store)
	if automationErr != nil {
		t.Fatal(automationErr)
	}
	// miscDependencies、miscErr 保存通知、分析和卡券领域依赖及构造错误。
	miscDependencies, miscErr := NewMiscDependencies(store)
	if miscErr != nil {
		t.Fatal(miscErr)
	}
	// adminSettingsDependencies 保存管理员和系统设置领域依赖。
	adminSettingsDependencies := NewAdminSettingsDependencies(store)
	// base 保存除当前故意置空字段外的完整构造参数。
	base := TransportApplicationServiceOptions{
		AutomationDependencies: automationDependencies, MiscDependencies: miscDependencies,
		AdminSettingsDependencies: adminSettingsDependencies, AccountTaskRunner: NewAccountTaskRunner(nil), ModelClient: NewAIModelClient(),
	}
	// cases 保存每个必需依赖缺失时的构造参数。
	cases := []TransportApplicationServiceOptions{
		{MiscDependencies: base.MiscDependencies, AdminSettingsDependencies: base.AdminSettingsDependencies, AccountTaskRunner: base.AccountTaskRunner, ModelClient: base.ModelClient},
		{AutomationDependencies: base.AutomationDependencies, AdminSettingsDependencies: base.AdminSettingsDependencies, AccountTaskRunner: base.AccountTaskRunner, ModelClient: base.ModelClient},
		{AutomationDependencies: base.AutomationDependencies, MiscDependencies: base.MiscDependencies, AccountTaskRunner: base.AccountTaskRunner, ModelClient: base.ModelClient},
		{AutomationDependencies: base.AutomationDependencies, MiscDependencies: base.MiscDependencies, AdminSettingsDependencies: base.AdminSettingsDependencies, ModelClient: base.ModelClient},
		{AutomationDependencies: base.AutomationDependencies, MiscDependencies: base.MiscDependencies, AdminSettingsDependencies: base.AdminSettingsDependencies, AccountTaskRunner: base.AccountTaskRunner},
	}
	// options 表示当前缺少一个必需依赖的构造参数。
	for _, options := range cases {
		// services、err 保存缺少依赖时的构造结果。
		services, err := NewTransportApplicationServices(options)
		if services != nil || err == nil {
			t.Errorf("缺少依赖时构造结果异常: services=%v err=%v", services, err)
		}
	}
}

// TestNewTransportApplicationServicesCreatesCompleteSet 确保有效窄依赖能一次性构造全部 transport-facing 服务。
func TestNewTransportApplicationServicesCreatesCompleteSet(t *testing.T) {
	// store 仅用于构造 typed adapter，不执行数据库读写。
	store := db.NewStore(nil, db.DialectSQLite)
	// automationDependencies、err 保存自动化领域依赖及其构造错误。
	automationDependencies, err := NewAutomationDependencies(store)
	if err != nil {
		t.Fatalf("NewAutomationDependencies: %v", err)
	}
	// miscDependencies、err 保存通知、分析和卡券领域依赖及其构造错误。
	miscDependencies, err := NewMiscDependencies(store)
	if err != nil {
		t.Fatalf("NewMiscDependencies: %v", err)
	}
	// adminSettingsDependencies 保存管理员和系统设置领域依赖。
	adminSettingsDependencies := NewAdminSettingsDependencies(store)
	// services、err 保存完整 transport-facing 服务集合及其构造错误。
	services, err := NewTransportApplicationServices(TransportApplicationServiceOptions{
		AutomationDependencies:    automationDependencies,
		MiscDependencies:          miscDependencies,
		AdminSettingsDependencies: adminSettingsDependencies,
		AccountTaskRunner:         NewAccountTaskRunner(nil),
		ModelClient:               NewAIModelClient(),
	})
	if err != nil {
		t.Fatalf("NewTransportApplicationServices: %v", err)
	}
	// validationErr 表示完整服务集合的字段校验失败。
	if validationErr := services.Validate(); validationErr != nil {
		t.Fatalf("Validate: %v", err)
	}
}
