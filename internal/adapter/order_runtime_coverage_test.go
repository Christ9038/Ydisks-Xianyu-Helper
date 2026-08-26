package adapter

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// TestOrderRuntimeNilInfrastructurePaths 验证订单运行时在未装配外部基础设施时的确定性保护路径。
func TestOrderRuntimeNilInfrastructurePaths(t *testing.T) {
	// ctx 是本测试订单运行时调用共用的上下文。
	ctx := context.Background()
	// hooks 保存未装配平台、账号和自动化能力的运行时回调集合。
	hooks := NewOrderRuntimeHooks(nil, nil, nil, nil, nil, nil)
	// recoverable 表示可选会话恢复回调是否报告成功。
	recoverable := hooks.RecoverExpiredSession != nil && hooks.RecoverExpiredSession(ctx, "cid", nil)
	if hooks.ClientAvailable() || hooks.AccountRunning("cid") || hooks.AutomationReady() || recoverable {
		t.Fatal("空订单运行时 hooks 不应报告能力可用")
	}
	// _, manualErr 保存空自动化中心的手动发货错误。
	if _, manualErr := hooks.ManualFullDelivery(ctx, &orderapp.Order{}); manualErr == nil {
		t.Fatal("空自动化中心应返回手动发货错误")
	}
	// runtime 保存没有 Store 和平台客户端的订单运行时。
	runtime := NewOrderRuntime(nil, hooks, nil, nil)
	if runtime.AccountRunning("cid") || runtime.AutomationReady() || runtime.MTopAvailable() || runtime.DetailAvailable() || runtime.SoldAvailable() {
		t.Fatal("空订单运行时不应报告外部能力可用")
	}
	// _, confirmErr 保存缺少凭证存储时的确认发货错误。
	if result := runtime.ConfirmShipment(ctx, "cid", "order", 1); result.Err == nil {
		t.Fatal("缺少凭证存储应返回确认发货错误")
	}
	// _, deliveryErr 保存缺少自动化中心时的手动发货错误。
	if _, deliveryErr := runtime.ManualFullDelivery(ctx, &orderapp.Order{}); deliveryErr == nil {
		t.Fatal("缺少自动化中心应返回手动发货错误")
	}
	// _, reconcileErr 保存缺少补偿仓储时的记录错误。
	if _, reconcileErr := runtime.RecordOrderReconciliation(ctx, "order", "cid", "kind", "message"); reconcileErr == nil {
		t.Fatal("缺少补偿仓储应返回记录错误")
	}
	// _, aliasErr 保存补偿记录别名方法的错误。
	if _, aliasErr := runtime.RecordReconciliation(ctx, "order", "cid", "kind", "message"); aliasErr == nil {
		t.Fatal("补偿记录别名应返回记录错误")
	}
	runtime.UpdateRunningCookie(ctx, "cid", "cookie")
	runtime.NotifyDelivery("cid", "buyer", "item", "chat", "message")
	runtime.ReportPersistenceFailure("order", errors.New("persist"))
	if runtime.RecoverExpiredSession(ctx, "cid", errors.New("expired")) {
		t.Fatal("未装配恢复回调不应成功")
	}
	// validDetail、emptyDetail 保存凭证存在性判断的有效和空值输入。
	validDetail := &orderapp.PlatformRuntimeData{Value: "unb=id"}
	// emptyDetail 表示没有平台 Cookie 的凭证视图。
	emptyDetail := &orderapp.PlatformRuntimeData{}
	if !runtime.CredentialAvailable(validDetail) || runtime.CredentialAvailable(emptyDetail) || runtime.CredentialAvailable(nil) {
		t.Fatal("订单凭证可用性判断异常")
	}
	// _, detailErr 保存缺少平台详情接口时的刷新错误。
	if _, detailErr := runtime.FetchOrderDetail(ctx, validDetail, "order"); detailErr == nil {
		t.Fatal("缺少详情接口应返回错误")
	}
	// _, soldErr 保存缺少平台订单列表接口时的刷新错误。
	if _, soldErr := runtime.FetchSoldOrders(ctx, validDetail); soldErr == nil {
		t.Fatal("缺少订单列表接口应返回错误")
	}
	// value、changed、handled、persistErr 保存未处理 Cookie 更新的零值结果。
	if value, changed, handled, persistErr := runtime.PersistCookieSession(ctx, nil, orderapp.RefreshCookieUpdate{}); value != "" || changed || handled || persistErr != nil {
		t.Fatalf("未处理 Cookie 更新异常 value=%q changed=%v handled=%v err=%v", value, changed, handled, persistErr)
	}
	if runtime.IsSessionExpired(nil) {
		t.Fatal("nil 平台错误不应被识别为会话过期")
	}
}

// TestOrderRuntimeAdapterDelegatesNilRuntimePaths 验证订单应用适配器在空运行时下的统一错误和零值结果。
func TestOrderRuntimeAdapterDelegatesNilRuntimePaths(t *testing.T) {
	// ctx 是本测试订单应用适配器共用的上下文。
	ctx := context.Background()
	// adapter 保存组合根未提供工厂时构造的订单应用适配器。
	adapter := NewOrderRuntimeAdapter(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if adapter.AccountRunning("cid") || adapter.AutomationReady() || adapter.MTopAvailable() || adapter.DetailAvailable() || adapter.SoldAvailable() || !adapter.CredentialAvailable(&orderapp.PlatformRuntimeData{Value: "cookie"}) || adapter.CredentialAvailable(nil) {
		t.Fatal("空订单适配器不应报告能力可用")
	}
	// _, manualErr 保存空运行时手动发货错误。
	if _, manualErr := adapter.ManualFullDelivery(ctx, &orderapp.Order{}); manualErr == nil {
		t.Fatal("空订单适配器应返回手动发货错误")
	}
	// confirmResult 保存空运行时确认发货结果。
	confirmResult := adapter.ConfirmShipment(ctx, "cid", "order", 1)
	if confirmResult.Err == nil {
		t.Fatal("空订单适配器应返回确认发货错误")
	}
	adapter.UpdateRunningCookie(ctx, "cid", "cookie")
	adapter.NotifyDelivery("cid", "buyer", "item", "chat", "message")
	// _, reconcileErr 保存空运行时补偿记录错误。
	if _, reconcileErr := adapter.RecordOrderReconciliation(ctx, "order", "cid", "kind", "message"); reconcileErr == nil {
		t.Fatal("空订单适配器应返回补偿记录错误")
	}
	// _, aliasErr 保存空运行时补偿记录别名错误。
	if _, aliasErr := adapter.RecordReconciliation(ctx, "order", "cid", "kind", "message"); aliasErr == nil {
		t.Fatal("空订单适配器别名应返回补偿记录错误")
	}
	adapter.ReportPersistenceFailure("order", errors.New("persist"))
	if adapter.RecoverExpiredSession(ctx, "cid", errors.New("expired")) {
		t.Fatal("空订单适配器不应恢复会话")
	}
	// _, detailErr 保存空运行时详情刷新错误。
	if _, detailErr := adapter.FetchOrderDetail(ctx, nil, "order"); detailErr == nil {
		t.Fatal("空订单适配器应返回详情刷新错误")
	}
	// _, soldErr 保存空运行时订单列表刷新错误。
	if _, soldErr := adapter.FetchSoldOrders(ctx, nil); soldErr == nil {
		t.Fatal("空订单适配器应返回订单列表刷新错误")
	}
	// value、changed、handled、persistErr 保存空运行时 Cookie 更新结果。
	if value, changed, handled, persistErr := adapter.PersistCookieSession(ctx, nil, orderapp.RefreshCookieUpdate{}); value != "" || changed || handled || persistErr != nil {
		t.Fatalf("空运行时 Cookie 更新异常 value=%q changed=%v handled=%v err=%v", value, changed, handled, persistErr)
	}
	if adapter.IsSessionExpired(nil) {
		t.Fatal("空订单适配器不应识别 nil 会话错误")
	}
}

// TestOrderRuntimePureCredentialHelpers 验证订单 Cookie 辅助转换和状态判断的纯业务分支。
func TestOrderRuntimePureCredentialHelpers(t *testing.T) {
	// nilData 保存 nil 平台运行数据的转换结果。
	if converted := platformRuntimeDataForOrder(nil); converted.ID != "" || converted.Value != "" {
		t.Fatalf("nil 平台运行数据转换异常=%+v", converted)
	}
	// data 保存待转换的非敏感平台运行数据。
	data := &orderapp.PlatformRuntimeData{ID: "cid", UserID: 7, Value: "cookie", ShowBrowser: true}
	// converted 保存数据库适配器内部平台运行数据。
	converted := platformRuntimeDataForOrder(data)
	if converted.ID != "cid" || converted.UserID != 7 || converted.Value != "cookie" || !converted.ShowBrowser {
		t.Fatalf("平台运行数据转换异常=%+v", converted)
	}
	if hasStoredOrderCredential(converted) == false || hasStoredOrderCredential(platformRuntimeDataForOrder(&orderapp.PlatformRuntimeData{})) {
		t.Fatal("订单凭证存在性判断异常")
	}
	// emptyUpdate 保存缺少详情或会话时的空 Cookie 更新结果。
	if update := orderCookieUpdate(nil, nil); update.Handled || update.Changed {
		t.Fatalf("空订单 Cookie 更新异常=%+v", update)
	}
}
