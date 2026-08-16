package items

import (
	"context"
	"errors"
	"testing"
)

// catalogRepositoryStub 保存商品读取端口的预设结果。
type catalogRepositoryStub struct {
	// items 保存列表结果。
	items []CatalogItem
	// item 保存详情结果。
	item CatalogItem
	// err 保存底层错误。
	err error
}

// ListForUser 返回预设的用户商品列表。
func (stub catalogRepositoryStub) ListForUser(context.Context, int64, string) ([]CatalogItem, error) {
	return stub.items, stub.err
}

// ListByCookie 返回预设的账号商品列表。
func (stub catalogRepositoryStub) ListByCookie(context.Context, string) ([]CatalogItem, error) {
	return stub.items, stub.err
}

// Get 返回预设的商品详情。
func (stub catalogRepositoryStub) Get(context.Context, string, string) (CatalogItem, error) {
	return stub.item, stub.err
}

// TestCatalogServiceDelegatesReads 验证商品读取服务转发列表与详情结果。
func TestCatalogServiceDelegatesReads(t *testing.T) {
	// wantItem 保存测试期望的商品模型。
	wantItem := CatalogItem{CookieID: "account-1", ItemID: "item-1"}
	// service 和 err 保存商品读取服务及构造错误。
	service, err := NewCatalogService(catalogRepositoryStub{items: []CatalogItem{wantItem}, item: wantItem})
	if err != nil {
		t.Fatalf("NewCatalogService() error=%v", err)
	}
	// items 和 err 保存用户范围读取结果及错误。
	items, err := service.ListForUser(context.Background(), 7, "account-1")
	if err != nil || len(items) != 1 || items[0].ItemID != wantItem.ItemID {
		t.Fatalf("ListForUser() items=%v error=%v", items, err)
	}
	// item 和 err 保存商品详情读取结果及错误。
	item, err := service.Get(context.Background(), "account-1", "item-1")
	if err != nil || item.ItemID != wantItem.ItemID {
		t.Fatalf("Get() item=%v error=%v", item, err)
	}
}

// TestCatalogServicePropagatesErrors 验证商品读取服务不吞底层错误。
func TestCatalogServicePropagatesErrors(t *testing.T) {
	// wantErr 保存底层仓储返回的错误。
	wantErr := errors.New("catalog unavailable")
	// service 和 err 保存商品读取服务及构造错误。
	service, err := NewCatalogService(catalogRepositoryStub{err: wantErr})
	if err != nil {
		t.Fatalf("NewCatalogService() error=%v", err)
	}
	// callErr 保存账号范围读取错误。
	if _, callErr := service.ListByCookie(context.Background(), "account-1"); !errors.Is(callErr, wantErr) {
		t.Fatalf("ListByCookie() error=%v want=%v", callErr, wantErr)
	}
	// callErr 保存详情读取错误。
	if _, callErr := service.Get(context.Background(), "account-1", "item-1"); !errors.Is(callErr, wantErr) {
		t.Fatalf("Get() error=%v want=%v", callErr, wantErr)
	}
}
