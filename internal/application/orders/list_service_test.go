package orders

import (
	"context"
	"errors"
	"testing"
)

// listRepositoryStub 是订单列表服务测试使用的最小数据访问替身。
type listRepositoryStub struct {
	// owned 表示账号是否属于当前用户。
	owned bool
	// ownerErr 保存账号归属查询错误。
	ownerErr error
	// rows 保存待返回的订单行。
	rows []OrderRow
	// total 保存待返回的订单总数。
	total int
	// listErr 保存订单列表查询错误。
	listErr error
	// filter 保存最近一次收到的列表条件。
	filter ListFilter
}

// ExistsOwned 返回测试替身预设的账号归属结果。
func (s *listRepositoryStub) ExistsOwned(context.Context, int64, string) (bool, error) {
	return s.owned, s.ownerErr
}

// ListOrdersForUser 返回测试替身预设的订单列表结果。
func (s *listRepositoryStub) ListOrdersForUser(_ context.Context, filter ListFilter) ([]OrderRow, int, error) {
	s.filter = filter
	return s.rows, s.total, s.listErr
}

// TestListServiceNormalizesPagination 验证订单列表服务规范化分页并传递筛选条件。
func TestListServiceNormalizesPagination(t *testing.T) {
	// repository 保存列表服务使用的数据访问替身。
	repository := &listRepositoryStub{owned: true, rows: []OrderRow{{OrderID: "order-1"}}, total: 401}
	// service 保存待测试的订单列表服务。
	service := NewListService(repository)
	// result、err 保存列表查询结果及错误。
	result, err := service.List(context.Background(), ListQuery{UserID: 7, CookieID: "cookie-1", Search: "buyer", Page: 0, PageSize: 999})
	if err != nil {
		t.Fatalf("List error=%v", err)
	}
	if result.Page != 1 || result.PageSize != 200 || result.TotalPages != 3 || len(result.Rows) != 1 {
		t.Fatalf("unexpected result=%+v", result)
	}
	if repository.filter.Offset != 0 || repository.filter.Limit != 200 || repository.filter.Search != "buyer" {
		t.Fatalf("unexpected filter=%+v", repository.filter)
	}
}

// TestListServiceRejectsUnownedCookie 验证订单列表服务阻止跨用户账号筛选。
func TestListServiceRejectsUnownedCookie(t *testing.T) {
	// service 保存不拥有目标账号的列表服务。
	service := NewListService(&listRepositoryStub{owned: false})
	// err 保存跨用户账号筛选返回的错误。
	_, err := service.List(context.Background(), ListQuery{UserID: 7, CookieID: "other"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error=%v, want ErrForbidden", err)
	}
}

// TestListServicePropagatesRepositoryError 验证订单列表服务保留数据访问错误。
func TestListServicePropagatesRepositoryError(t *testing.T) {
	// expected 保存测试预设的数据访问错误。
	expected := errors.New("数据库不可用")
	// service 保存返回数据访问错误的列表服务。
	service := NewListService(&listRepositoryStub{owned: true, listErr: expected})
	// err 保存列表查询返回的错误。
	_, err := service.List(context.Background(), ListQuery{UserID: 7})
	if !errors.Is(err, expected) {
		t.Fatalf("error=%v, want repository error", err)
	}
}
