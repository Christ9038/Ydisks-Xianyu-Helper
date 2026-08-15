package orders

import (
	"context"
	"errors"
	"testing"
)

// deleteRepositoryFake 是订单删除应用服务测试使用的内存 repository。
type deleteRepositoryFake struct {
	// order 保存待删除订单。
	order *Order
	// owned 表示订单账号是否属于当前用户。
	owned bool
	// getErr 保存订单读取错误。
	getErr error
	// ownedErr 保存账号归属查询错误。
	ownedErr error
	// deleteErr 保存逻辑删除错误。
	deleteErr error
	// deletedID 保存最后一次逻辑删除的订单标识。
	deletedID string
}

// GetOrder 返回测试订单。
func (f *deleteRepositoryFake) GetOrder(context.Context, string) (*Order, error) {
	return f.order, f.getErr
}

// ExistsOwned 返回测试账号归属结果。
func (f *deleteRepositoryFake) ExistsOwned(context.Context, int64, string) (bool, error) {
	return f.owned, f.ownedErr
}

// SoftDeleteOrder 记录测试逻辑删除请求。
func (f *deleteRepositoryFake) SoftDeleteOrder(_ context.Context, orderID string) (bool, error) {
	f.deletedID = orderID
	return true, f.deleteErr
}

// TestDeleteServiceDeletesOwnedOrder 验证归属校验通过后只执行逻辑删除。
func TestDeleteServiceDeletesOwnedOrder(t *testing.T) {
	// repository 保存本用例使用的内存依赖。
	repository := &deleteRepositoryFake{order: &Order{OrderID: "order-1", CookieID: "cookie-1"}, owned: true}
	// service 保存待测试的订单删除服务。
	service := NewDeleteService(repository)
	// err 保存订单删除结果。
	if err := service.Delete(context.Background(), 7, "order-1"); err != nil {
		t.Fatalf("删除订单失败: %v", err)
	}
	if repository.deletedID != "order-1" {
		t.Fatalf("未按预期执行逻辑删除: %q", repository.deletedID)
	}
}

// TestDeleteServiceRejectsMissingOrForbiddenOrder 验证不存在和无权订单不会被删除。
func TestDeleteServiceRejectsMissingOrForbiddenOrder(t *testing.T) {
	// missingService 保存订单不存在场景的应用服务。
	missingService := NewDeleteService(&deleteRepositoryFake{})
	if !errors.Is(missingService.Delete(context.Background(), 1, "missing"), ErrNotFound) {
		t.Fatal("订单不存在时未返回 ErrNotFound")
	}
	// forbiddenRepository 保存账号无权场景的内存依赖。
	forbiddenRepository := &deleteRepositoryFake{order: &Order{CookieID: "cookie-1"}}
	// forbiddenService 保存账号无权场景的应用服务。
	forbiddenService := NewDeleteService(forbiddenRepository)
	if !errors.Is(forbiddenService.Delete(context.Background(), 1, "order-1"), ErrForbidden) {
		t.Fatal("账号无权时未返回 ErrForbidden")
	}
	if forbiddenRepository.deletedID != "" {
		t.Fatal("无权订单不应执行逻辑删除")
	}
}

// TestDeleteServicePropagatesStorageErrors 验证读取和删除存储错误会原样返回。
func TestDeleteServicePropagatesStorageErrors(t *testing.T) {
	// readErr 保存订单读取错误。
	readErr := errors.New("读取失败")
	// readService 保存订单读取失败场景的应用服务。
	readService := NewDeleteService(&deleteRepositoryFake{getErr: readErr})
	if !errors.Is(readService.Delete(context.Background(), 1, "order-1"), readErr) {
		t.Fatal("订单读取错误未透传")
	}
	// deleteErr 保存逻辑删除错误。
	deleteErr := errors.New("删除失败")
	// deleteService 保存逻辑删除失败场景的应用服务。
	deleteService := NewDeleteService(&deleteRepositoryFake{order: &Order{CookieID: "cookie-1"}, owned: true, deleteErr: deleteErr})
	if !errors.Is(deleteService.Delete(context.Background(), 1, "order-1"), deleteErr) {
		t.Fatal("逻辑删除错误未透传")
	}
}
