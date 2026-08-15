package orders

import (
	"context"
	"errors"
	"testing"
)

// detailRepositoryStub 是订单详情服务测试使用的最小数据访问替身。
type detailRepositoryStub struct {
	// order 保存待返回的订单实体。
	order *Order
	// orderErr 保存订单读取错误。
	orderErr error
	// owned 表示账号是否归属于当前用户。
	owned bool
	// ownershipErr 保存账号归属查询错误。
	ownershipErr error
	// item 保存待返回的商品信息。
	item *ItemInfo
	// itemErr 保存商品读取错误。
	itemErr error
}

// ExistsOwned 返回测试替身预设的账号归属结果。
func (s *detailRepositoryStub) ExistsOwned(context.Context, int64, string) (bool, error) {
	return s.owned, s.ownershipErr
}

// GetOrder 返回测试替身预设的订单结果。
func (s *detailRepositoryStub) GetOrder(context.Context, string) (*Order, error) {
	return s.order, s.orderErr
}

// GetItem 返回测试替身预设的商品结果。
func (s *detailRepositoryStub) GetItem(context.Context, string, string) (*ItemInfo, error) {
	return s.item, s.itemErr
}

// TestDetailServiceChecksOwnership 验证订单详情服务阻止跨用户读取。
func TestDetailServiceChecksOwnership(t *testing.T) {
	// service 保存不拥有目标账号的详情服务。
	service := NewDetailService(&detailRepositoryStub{order: &Order{CookieID: "other"}, owned: false})
	// err 保存详情读取返回的错误。
	_, err := service.Get(context.Background(), 7, "order-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error=%v, want ErrForbidden", err)
	}
}

// TestDetailServiceReturnsNotFound 验证订单为空时返回稳定的不存在错误。
func TestDetailServiceReturnsNotFound(t *testing.T) {
	// service 保存返回空订单的详情服务。
	service := NewDetailService(&detailRepositoryStub{owned: true})
	// err 保存详情读取返回的错误。
	_, err := service.Get(context.Background(), 7, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error=%v, want ErrNotFound", err)
	}
}

// TestDetailServiceKeepsOrderWhenItemLookupFails 验证商品读取失败不丢失订单主体。
func TestDetailServiceKeepsOrderWhenItemLookupFails(t *testing.T) {
	// expected 保存详情服务应返回的订单主体。
	expected := &Order{OrderID: "order-1", CookieID: "cookie-1", ItemID: "item-1"}
	// service 保存商品读取失败的详情服务。
	service := NewDetailService(&detailRepositoryStub{order: expected, owned: true, itemErr: errors.New("商品暂不可用")})
	// result、err 保存详情查询结果及错误。
	result, err := service.GetView(context.Background(), 7, expected.OrderID)
	if err != nil || result.Order != expected || result.Item != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
