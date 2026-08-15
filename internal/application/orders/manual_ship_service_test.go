package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// manualShipRepositoryFake 是手动发货应用服务测试使用的内存 repository。
type manualShipRepositoryFake struct {
	// ownedIDs 保存测试用户拥有的账号标识。
	ownedIDs []string
	// orders 保存测试订单。
	orders map[string]*Order
	// listErr 保存账号列表查询错误。
	listErr error
	// upsertErr 保存订单状态写入错误。
	upsertErr error
	// upsertID 保存最近一次写入订单标识。
	upsertID string
	// upsertOptions 保存最近一次订单写入字段。
	upsertOptions UpsertOptions
}

// ListOwnedIDs 返回测试用户拥有的账号标识。
func (f *manualShipRepositoryFake) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return f.ownedIDs, f.listErr
}

// GetOrder 返回测试订单。
func (f *manualShipRepositoryFake) GetOrder(_ context.Context, orderID string) (*Order, error) {
	return f.orders[orderID], nil
}

// UpsertOrder 记录测试订单状态写入。
func (f *manualShipRepositoryFake) UpsertOrder(_ context.Context, orderID string, options UpsertOptions) error {
	f.upsertID = orderID
	f.upsertOptions = options
	return f.upsertErr
}

// manualShipRuntimeFake 是手动发货应用服务测试使用的平台运行时 Port。
type manualShipRuntimeFake struct {
	// mtopAvailable 表示平台客户端是否可用。
	mtopAvailable bool
	// automationReady 表示完整自动化依赖是否可用。
	automationReady bool
	// accountRunning 表示订单账号运行时是否在线。
	accountRunning bool
	// sentCount 保存完整自动化发货发送数量。
	sentCount int
	// fullDeliveryErr 保存完整自动化发货错误。
	fullDeliveryErr error
	// consign 保存平台确认发货结果。
	consign ConsignResult
	// updatedCookie 保存同步到运行时的 Cookie。
	updatedCookie string
	// notifications 保存发送的通知文本。
	notifications []string
	// reconciliationID 保存补偿记录标识。
	reconciliationID string
	// reconciliationErr 保存补偿记录写入错误。
	reconciliationErr error
	// reportedPersistenceErr 保存报告的本地持久化错误。
	reportedPersistenceErr error
}

// MTopAvailable 返回测试平台客户端可用状态。
func (f *manualShipRuntimeFake) MTopAvailable() bool {
	return f.mtopAvailable
}

// AutomationReady 返回测试自动化依赖可用状态。
func (f *manualShipRuntimeFake) AutomationReady() bool {
	return f.automationReady
}

// AccountRunning 返回测试账号运行状态。
func (f *manualShipRuntimeFake) AccountRunning(string) bool {
	return f.accountRunning
}

// ManualFullDelivery 返回测试完整自动化发货结果。
func (f *manualShipRuntimeFake) ManualFullDelivery(context.Context, *Order) (int, error) {
	return f.sentCount, f.fullDeliveryErr
}

// ConfirmShipment 返回测试平台确认发货结果。
func (f *manualShipRuntimeFake) ConfirmShipment(context.Context, string, string, int64) ConsignResult {
	return f.consign
}

// UpdateRunningCookie 记录测试运行时 Cookie 更新。
func (f *manualShipRuntimeFake) UpdateRunningCookie(_ context.Context, _, value string) {
	f.updatedCookie = value
}

// NotifyDelivery 记录测试发货通知。
func (f *manualShipRuntimeFake) NotifyDelivery(_, _, _, _, message string) {
	f.notifications = append(f.notifications, message)
}

// RecordReconciliation 返回测试补偿记录结果。
func (f *manualShipRuntimeFake) RecordReconciliation(context.Context, string, string, string, string) (string, error) {
	return f.reconciliationID, f.reconciliationErr
}

// ReportPersistenceFailure 记录测试本地持久化错误。
func (f *manualShipRuntimeFake) ReportPersistenceFailure(_ string, err error) {
	f.reportedPersistenceErr = err
}

// TestManualShipServiceStatusSuccess 验证状态发货成功会同步 Cookie 和本地订单状态。
func TestManualShipServiceStatusSuccess(t *testing.T) {
	// repository 保存本用例使用的内存依赖。
	repository := &manualShipRepositoryFake{ownedIDs: []string{"cookie-1"}, orders: map[string]*Order{
		"order-1": {OrderID: "order-1", CookieID: "cookie-1", ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", OrderStatus: "2"},
	}}
	// runtime 保存本用例使用的平台运行时依赖。
	runtime := &manualShipRuntimeFake{mtopAvailable: true, consign: ConsignResult{Success: true, RuntimeCookie: "new-cookie", RuntimeCookieChanged: true}}
	// service 保存待测试的手动发货服务。
	service := NewManualShipService(repository, runtime)
	// result、err 保存手动发货结果和错误。
	result, err := service.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}, ShipMode: "status_only"})
	if err != nil || result.SuccessCount != 1 || result.FailedCount != 0 {
		t.Fatalf("发货结果异常: result=%+v err=%v", result, err)
	}
	if runtime.updatedCookie != "new-cookie" || repository.upsertID != "order-1" || repository.upsertOptions.OrderStatus != "shipped" {
		t.Fatalf("发货副作用异常: runtime=%+v repository=%+v", runtime, repository)
	}
	if result.Results[0].Status != "succeeded" || !result.Results[0].Success {
		t.Fatalf("成功结果异常: %+v", result.Results[0])
	}
}

// TestManualShipServiceRejectsInvalidOrders 验证不存在、无权和非待发货订单不会调用远端。
func TestManualShipServiceRejectsInvalidOrders(t *testing.T) {
	// repository 保存本用例使用的订单依赖。
	repository := &manualShipRepositoryFake{ownedIDs: []string{"cookie-1"}, orders: map[string]*Order{
		"forbidden": {CookieID: "other", OrderStatus: "2"},
		"cancelled": {CookieID: "cookie-1", OrderStatus: "cancelled"},
	}}
	// runtime 保存本用例使用的平台运行时依赖。
	runtime := &manualShipRuntimeFake{mtopAvailable: true}
	// service 保存待测试的手动发货服务。
	service := NewManualShipService(repository, runtime)
	// result、err 保存混合订单处理结果和错误。
	result, err := service.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"", "missing", "forbidden", "cancelled"}, ShipMode: "status_only"})
	if err != nil || result.FailedCount != 3 || len(result.Results) != 3 {
		t.Fatalf("失败结果异常: result=%+v err=%v", result, err)
	}
	if repository.upsertID != "" || runtime.updatedCookie != "" {
		t.Fatal("无效订单不应执行远端或本地写入")
	}
}

// TestManualShipServiceRejectsUninitializedDependencies 验证未装配依赖时不会执行发货。
func TestManualShipServiceRejectsUninitializedDependencies(t *testing.T) {
	// service 保存 repository 未装配场景的应用服务。
	service := NewManualShipService(nil, &manualShipRuntimeFake{})
	// result、err 保存未装配依赖结果和错误。
	result, err := service.ManualShip(context.Background(), ManualShipRequest{UserID: 7})
	if err == nil || result.SuccessCount != 0 {
		t.Fatalf("repository 未装配时应返回错误: result=%+v err=%v", result, err)
	}
	// runtimeService 保存 runtime 未装配场景的应用服务。
	runtimeService := NewManualShipService(&manualShipRepositoryFake{}, nil)
	// result、err 保存 runtime 未装配结果和错误。
	result, err = runtimeService.ManualShip(context.Background(), ManualShipRequest{UserID: 7})
	if err == nil || result.SuccessCount != 0 {
		t.Fatalf("runtime 未装配时应返回错误: result=%+v err=%v", result, err)
	}
}

// TestManualShipServicePropagatesAccountQueryError 验证账号列表读取错误会阻止批次执行。
func TestManualShipServicePropagatesAccountQueryError(t *testing.T) {
	// expectedErr 保存账号列表查询错误。
	expectedErr := errors.New("账号查询失败")
	// service 保存账号列表查询失败场景的应用服务。
	service := NewManualShipService(&manualShipRepositoryFake{listErr: expectedErr}, &manualShipRuntimeFake{})
	// result、err 保存账号列表查询结果和错误。
	result, err := service.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}})
	if !errors.Is(err, expectedErr) || result.SuccessCount != 0 || result.FailedCount != 0 {
		t.Fatalf("账号列表错误未正确透传: result=%+v err=%v", result, err)
	}
}

// TestManualShipServiceReconciliationRequired 验证远端成功、本地写入失败时生成补偿结果。
func TestManualShipServiceReconciliationRequired(t *testing.T) {
	// repository 保存本地订单写入失败依赖。
	repository := &manualShipRepositoryFake{ownedIDs: []string{"cookie-1"}, upsertErr: errors.New("本地写入失败"), orders: map[string]*Order{
		"order-1": {CookieID: "cookie-1", ItemID: "item-1", OrderStatus: "pending_ship"},
	}}
	// runtime 保存远端成功和补偿记录依赖。
	runtime := &manualShipRuntimeFake{mtopAvailable: true, consign: ConsignResult{Success: true}, reconciliationID: "reconcile-1", reconciliationErr: errors.New("补偿写入失败")}
	// service 保存待测试的手动发货服务。
	service := NewManualShipService(repository, runtime)
	// result、err 保存补偿场景结果和错误。
	result, err := service.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}})
	if err != nil || result.SuccessCount != 1 || result.Results[0].Status != "reconciliation_required" {
		t.Fatalf("补偿结果异常: result=%+v err=%v", result, err)
	}
	if result.Results[0].ReconciliationID != "reconcile-1" || !strings.Contains(result.Results[0].ReconciliationWarning, "补偿写入失败") || runtime.reportedPersistenceErr == nil {
		t.Fatalf("补偿副作用异常: result=%+v runtime=%+v", result, runtime)
	}
}

// TestManualShipServiceStatusFailures 验证平台未装配、远端异常和业务失败分支。
func TestManualShipServiceStatusFailures(t *testing.T) {
	// repository 保存状态失败测试订单。
	repository := &manualShipRepositoryFake{ownedIDs: []string{"cookie-1"}, orders: map[string]*Order{
		"order-1": {CookieID: "cookie-1", ItemID: "item-1", OrderStatus: "pending_ship"},
	}}
	// unavailableRuntime 保存平台客户端未装配场景依赖。
	unavailableRuntime := &manualShipRuntimeFake{}
	// unavailableService 保存平台客户端未装配场景服务。
	unavailableService := NewManualShipService(repository, unavailableRuntime)
	// result、err 保存平台未装配结果和错误。
	result, err := unavailableService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "mtop") {
		t.Fatalf("平台未装配结果异常: result=%+v err=%v", result, err)
	}
	// remoteErrorRuntime 保存远端调用异常场景依赖。
	remoteErrorRuntime := &manualShipRuntimeFake{mtopAvailable: true, consign: ConsignResult{Err: errors.New("网络异常")}}
	// remoteErrorService 保存远端调用异常场景服务。
	remoteErrorService := NewManualShipService(repository, remoteErrorRuntime)
	// result、err 保存远端调用异常结果和错误。
	result, err = remoteErrorService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "网络异常") {
		t.Fatalf("远端异常结果异常: result=%+v err=%v", result, err)
	}
	// businessFailureRuntime 保存远端业务失败场景依赖。
	businessFailureRuntime := &manualShipRuntimeFake{mtopAvailable: true, consign: ConsignResult{Messages: []string{"订单不存在", "状态不允许"}}}
	// businessFailureService 保存远端业务失败场景服务。
	businessFailureService := NewManualShipService(repository, businessFailureRuntime)
	// result、err 保存远端业务失败结果和错误。
	result, err = businessFailureService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "订单不存在; 状态不允许") {
		t.Fatalf("远端业务失败结果异常: result=%+v err=%v", result, err)
	}
}

// TestManualShipServiceFullDeliveryBranches 验证完整自动化发货的依赖、成功和失败分支。
func TestManualShipServiceFullDeliveryBranches(t *testing.T) {
	// repository 保存完整发货测试订单。
	repository := &manualShipRepositoryFake{ownedIDs: []string{"cookie-1"}, orders: map[string]*Order{"order-1": {CookieID: "cookie-1", OrderStatus: "pending_ship"}}}
	// unavailableRuntime 保存自动化未装配场景依赖。
	unavailableRuntime := &manualShipRuntimeFake{}
	// unavailableService 保存自动化未装配场景服务。
	unavailableService := NewManualShipService(repository, unavailableRuntime)
	// result、err 保存自动化未装配结果和错误。
	result, err := unavailableService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}, ShipMode: "full_delivery"})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "自动化") {
		t.Fatalf("自动化未装配结果异常: result=%+v err=%v", result, err)
	}
	// offlineRuntime 保存账号运行时离线场景依赖。
	offlineRuntime := &manualShipRuntimeFake{automationReady: true}
	// offlineService 保存账号运行时离线场景服务。
	offlineService := NewManualShipService(repository, offlineRuntime)
	// result、err 保存账号运行时离线结果和错误。
	result, err = offlineService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}, ShipMode: "full_delivery"})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "未在线") {
		t.Fatalf("账号离线结果异常: result=%+v err=%v", result, err)
	}
	// successRuntime 保存完整发货成功场景依赖。
	successRuntime := &manualShipRuntimeFake{automationReady: true, accountRunning: true, sentCount: 2}
	// successService 保存完整发货成功场景服务。
	successService := NewManualShipService(repository, successRuntime)
	// result、err 保存完整发货成功结果和错误。
	result, err = successService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}, ShipMode: "full_delivery"})
	if err != nil || result.SuccessCount != 1 || !strings.Contains(result.Results[0].Message, "2条") {
		t.Fatalf("完整发货成功结果异常: result=%+v err=%v", result, err)
	}
	// failureRuntime 保存完整发货失败场景依赖。
	failureRuntime := &manualShipRuntimeFake{automationReady: true, accountRunning: true, fullDeliveryErr: errors.New("自动化失败")}
	// failureService 保存完整发货失败场景服务。
	failureService := NewManualShipService(repository, failureRuntime)
	// result、err 保存完整发货失败结果和错误。
	result, err = failureService.ManualShip(context.Background(), ManualShipRequest{UserID: 7, OrderIDs: []string{"order-1"}, ShipMode: "full_delivery"})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "自动化失败") {
		t.Fatalf("完整发货失败结果异常: result=%+v err=%v", result, err)
	}
}
