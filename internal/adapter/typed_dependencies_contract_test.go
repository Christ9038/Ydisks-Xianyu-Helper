package adapter

import "testing"

// TestTypedDependenciesCoverServerAssembly 验证领域依赖组覆盖 Server 应用装配所需的全部基础设施边界。
func TestTypedDependenciesCoverServerAssembly(t *testing.T) {
	// store、cleanup 保存测试用数据库聚合及其资源释放函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// orderDependencies 保存订单应用所需的显式依赖组。
	orderDependencies, orderErr := NewOrderDependencies(store)
	// accountDependencies 保存账号应用所需的显式依赖组。
	accountDependencies, accountErr := NewAccountDependencies(store)
	// itemDependencies 保存商品应用所需的显式依赖组。
	itemDependencies, itemErr := NewItemDependencies(store)
	// automationDependencies 保存自动化应用所需的显式依赖组。
	automationDependencies, automationErr := NewAutomationDependencies(store)
	// miscDependencies 保存通知、分析和卡券应用所需的显式依赖组。
	miscDependencies, miscErr := NewMiscDependencies(store)
	// chatDependencies 保存聊天应用所需的显式依赖组。
	chatDependencies := NewChatDependencies(store)
	// systemDependencies 保存系统健康检查和补偿扫描所需的显式依赖组。
	systemDependencies := NewSystemDependencies(store)
	// adminSettingsDependencies 保存管理员和设置应用所需的显式依赖组。
	adminSettingsDependencies := NewAdminSettingsDependencies(store)
	// platformDependencies 保存平台客户端和二维码能力所需的显式依赖组。
	platformDependencies, platformErr := NewDefaultPlatformDependencies(nil)
	if orderErr != nil || accountErr != nil || itemErr != nil || automationErr != nil || miscErr != nil || orderDependencies == nil || accountDependencies == nil || itemDependencies == nil || automationDependencies == nil || miscDependencies == nil || chatDependencies == nil || systemDependencies == nil || adminSettingsDependencies == nil || platformErr != nil || platformDependencies == nil {
		t.Fatalf("显式领域依赖组未完整覆盖 Server 装配: order=%v account=%v item=%v automation=%v misc=%v chat=%v system=%v admin=%v platform=%v", orderErr, accountErr, itemErr, automationErr, miscErr, chatDependencies != nil, systemDependencies != nil, adminSettingsDependencies != nil, platformErr)
	}
}
