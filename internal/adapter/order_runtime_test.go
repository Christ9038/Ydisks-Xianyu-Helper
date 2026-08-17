package adapter

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// orderRuntimeAutomationFake 是订单运行时测试使用的自动化能力替身。
type orderRuntimeAutomationFake struct {
	// calls 记录自动化完整发货调用次数。
	calls int
}

// ManualFullDelivery 返回固定发送数量，验证 adapter 的订单模型转换回调。
func (f *orderRuntimeAutomationFake) ManualFullDelivery(context.Context, *db.Order) (int, error) {
	f.calls++
	return 2, nil
}

// orderRuntimeNotifierFake 是订单运行时测试使用的通知能力替身。
type orderRuntimeNotifierFake struct {
	// calls 记录发货通知调用次数。
	calls int
}

// NotifyDelivery 记录一次发货通知调用。
func (f *orderRuntimeNotifierFake) NotifyDelivery(string, string, string, string, string, string) {
	f.calls++
}

// orderRuntimeMTopFake 是订单运行时测试使用的平台客户端替身。
type orderRuntimeMTopFake struct {
	// consign 保存确认发货调用结果函数。
	consign func(context.Context, string, string) (bool, []string, string, error)
	// consignCalls 记录确认发货调用次数。
	consignCalls int
}

// FetchUserProfile 满足基础 MTOP 客户端接口但不参与订单测试。
func (f *orderRuntimeMTopFake) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, errors.New("订单测试未预期资料请求")
}

// ConsignContext 返回测试预置的确认发货结果。
func (f *orderRuntimeMTopFake) ConsignContext(ctx context.Context, cookies, orderID string) (bool, []string, string, error) {
	f.consignCalls++
	if f.consign == nil {
		return false, nil, "", errors.New("订单测试未配置确认发货结果")
	}
	return f.consign(ctx, cookies, orderID)
}

// FetchItemsPage 满足基础 MTOP 客户端接口但不参与订单测试。
func (f *orderRuntimeMTopFake) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, errors.New("订单测试未预期商品分页请求")
}

// FetchAllItems 满足基础 MTOP 客户端接口但不参与订单测试。
func (f *orderRuntimeMTopFake) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, errors.New("订单测试未预期商品列表请求")
}

// PublishItem 满足基础 MTOP 客户端接口但不参与订单测试。
func (f *orderRuntimeMTopFake) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, errors.New("订单测试未预期商品发布请求")
}

// RefreshTokenWithDeviceIDContext 满足基础 MTOP 客户端接口但不参与订单测试。
func (f *orderRuntimeMTopFake) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	return nil, errors.New("订单测试未预期令牌请求")
}

// TestNewOrderRuntimeHooksUsesTypedOptionalServices 验证订单运行时依赖由 adapter typed port 统一转换并安全处理空依赖。
func TestNewOrderRuntimeHooksUsesTypedOptionalServices(t *testing.T) {
	// emptyHooks 保存缺少可选服务时的回调集合。
	emptyHooks := NewOrderRuntimeHooks(nil, nil, nil, nil, nil, nil)
	if emptyHooks.AutomationReady() || emptyHooks.ClientAvailable() || emptyHooks.AccountRunning("missing") {
		t.Fatal("空依赖不应报告订单运行时能力可用")
	}
	// err 保存空自动化能力执行完整发货时返回的初始化错误。
	if _, err := emptyHooks.ManualFullDelivery(context.Background(), &orderapp.Order{}); err == nil {
		t.Fatal("空自动化依赖应返回错误")
	}
	emptyHooks.NotifyDelivery("cookie", "buyer", "item", "chat", "message")

	// automation、notifier 保存 typed port 测试替身。
	automation := &orderRuntimeAutomationFake{}
	// notifier 保存记录通知调用次数的 typed port 测试替身。
	notifier := &orderRuntimeNotifierFake{}
	// hooks 保存已装配可选服务的订单运行时回调集合。
	hooks := NewOrderRuntimeHooks(nil, nil, automation, notifier, nil, nil)
	if hooks.AutomationReady() {
		t.Fatal("缺少账号 Manager 时不应报告完整自动化已就绪")
	}
	// sent、err 保存完整发货回调的发送数量和执行错误。
	if sent, err := hooks.ManualFullDelivery(context.Background(), &orderapp.Order{OrderID: "order-1"}); err != nil || sent != 2 || automation.calls != 1 {
		t.Fatalf("自动化回调未正确转发: sent=%d err=%v calls=%d", sent, err, automation.calls)
	}
	hooks.NotifyDelivery("cookie", "buyer", "item", "chat", "message")
	if notifier.calls != 1 {
		t.Fatalf("通知回调次数=%d，期望 1", notifier.calls)
	}
}

// TestOrderRuntimeConfirmShipmentPersistsCookie 验证确认发货成功会写回平台返回的新 Cookie。
func TestOrderRuntimeConfirmShipmentPersistsCookie(t *testing.T) {
	// store、cleanup 保存测试数据库和资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 保存返回成功及新 Cookie 的平台客户端替身。
	client := &orderRuntimeMTopFake{consign: func(context.Context, string, string) (bool, []string, string, error) {
		return true, []string{"SUCCESS"}, "sid=new", nil
	}}
	// runtime 保存绑定数据库和平台客户端的订单运行时适配器。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }, ClientAvailable: func() bool { return true }}, nil, nil)
	// result 保存确认发货的应用层结果。
	result := runtime.ConfirmShipment(context.Background(), "cid", "order-1", 1)
	if !result.Success || result.Err != nil || result.RuntimeCookie != "sid=new" || !result.RuntimeCookieChanged {
		t.Fatalf("确认发货结果异常: %+v", result)
	}
	// savedCookie 保存凭证适配器写回数据库的 Cookie。
	savedCookie, savedErr := store.Cookies.GetValue(context.Background(), "cid")
	if savedErr != nil || savedCookie != "sid=new" {
		t.Fatalf("Cookie 写回异常: value=%q err=%v", savedCookie, savedErr)
	}
}

// TestOrderRuntimeConfirmShipmentPropagatesPlatformError 验证平台错误会透传且不会伪造成功。
func TestOrderRuntimeConfirmShipmentPropagatesPlatformError(t *testing.T) {
	// store、cleanup 保存测试数据库和资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// expectedErr 保存平台确认发货错误。
	expectedErr := errors.New("平台确认发货失败")
	// client 保存返回平台错误的客户端替身。
	client := &orderRuntimeMTopFake{consign: func(context.Context, string, string) (bool, []string, string, error) {
		return false, []string{"FAIL"}, "", expectedErr
	}}
	// runtime 保存订单运行时适配器。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }, ClientAvailable: func() bool { return true }}, nil, nil)
	// result 保存平台错误对应的应用层结果。
	result := runtime.ConfirmShipment(context.Background(), "cid", "order-1", 1)
	if result.Success || !errors.Is(result.Err, expectedErr) || client.consignCalls != 1 {
		t.Fatalf("平台错误未正确透传: result=%+v calls=%d", result, client.consignCalls)
	}
}

// TestOrderRuntimeConfirmShipmentRejectsOtherOwner 验证跨用户确认发货会在平台调用前拒绝。
func TestOrderRuntimeConfirmShipmentRejectsOtherOwner(t *testing.T) {
	// store、cleanup 保存测试数据库和资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 保存用于确认平台未被调用的客户端替身。
	client := &orderRuntimeMTopFake{consign: func(context.Context, string, string) (bool, []string, string, error) {
		return true, nil, "sid=unexpected", nil
	}}
	// runtime 保存订单运行时适配器。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }, ClientAvailable: func() bool { return true }}, nil, nil)
	// result 保存跨用户请求的应用层结果。
	result := runtime.ConfirmShipment(context.Background(), "cid", "order-1", 999)
	if !errors.Is(result.Err, orderapp.ErrForbidden) || client.consignCalls != 0 {
		t.Fatalf("跨用户请求未被拒绝: result=%+v calls=%d", result, client.consignCalls)
	}
}

// TestOrderRuntimeConfirmShipmentHonorsCancellation 验证请求取消会传入平台调用并返回取消错误。
func TestOrderRuntimeConfirmShipmentHonorsCancellation(t *testing.T) {
	// store、cleanup 保存测试数据库和资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 保存检查 Context 取消状态的客户端替身。
	client := &orderRuntimeMTopFake{consign: func(ctx context.Context, _, _ string) (bool, []string, string, error) {
		return false, nil, "", ctx.Err()
	}}
	// runtime 保存订单运行时适配器。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }, ClientAvailable: func() bool { return true }}, nil, nil)
	// ctx 保存已经取消的订单请求上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// result 保存取消请求对应的应用层结果。
	result := runtime.ConfirmShipment(ctx, "cid", "order-1", 1)
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("取消错误未透传: %+v", result)
	}
}

// 确保测试平台替身覆盖订单运行时依赖的基础客户端接口。
var _ mtop.Client = (*orderRuntimeMTopFake)(nil)
