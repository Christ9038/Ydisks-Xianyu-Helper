package orders

import (
	"context"
	"errors"
	"testing"
)

// TestOrderReadServicesCoverNilAndOwnershipGuards 覆盖订单读取服务的 nil、所有权和商品补全分支。
func TestOrderReadServicesCoverNilAndOwnershipGuards(t *testing.T) {
	// nilDetailService 验证 nil 详情服务不会触发 panic。
	var nilDetailService *DetailService
	// nilDetailErr 保存 nil 详情服务错误。
	if _, nilDetailErr := nilDetailService.Get(context.Background(), 1, "o"); nilDetailErr == nil {
		t.Fatal("nil detail service should fail")
	}
	// missingRepositoryService 验证缺少 repository 的详情服务错误。
	missingRepositoryService := NewDetailService(nil)
	// missingRepositoryErr 保存缺少详情 repository 的错误。
	if _, missingRepositoryErr := missingRepositoryService.GetView(context.Background(), 1, "o"); missingRepositoryErr == nil {
		t.Fatal("missing detail repository should fail")
	}
	// readErr 保存订单读取错误。
	readErr := errors.New("read")
	// readErrorService 保存订单读取失败的详情服务。
	readErrorService := NewDetailService(&detailRepositoryStub{orderErr: readErr})
	// returnedErr 保存详情服务透传的订单读取错误。
	if _, returnedErr := readErrorService.Get(context.Background(), 1, "o"); !errors.Is(returnedErr, readErr) {
		t.Fatalf("read error=%v", returnedErr)
	}
	// forbiddenService 保存订单 Cookie 为空的详情服务。
	forbiddenService := NewDetailService(&detailRepositoryStub{order: &Order{CookieID: "  "}})
	// forbiddenErr 保存空 Cookie 的访问错误。
	if _, forbiddenErr := forbiddenService.Get(context.Background(), 1, "o"); !errors.Is(forbiddenErr, ErrForbidden) {
		t.Fatalf("empty cookie error=%v", forbiddenErr)
	}
	// ownershipErr 保存账号归属读取错误。
	ownershipErr := errors.New("ownership")
	// ownershipErrorService 保存账号归属查询失败的详情服务。
	ownershipErrorService := NewDetailService(&detailRepositoryStub{order: &Order{CookieID: "c"}, ownershipErr: ownershipErr})
	// returnedErr 保存详情服务透传的归属查询错误。
	if _, returnedErr := ownershipErrorService.Get(context.Background(), 1, "o"); !errors.Is(returnedErr, ownershipErr) {
		t.Fatalf("ownership error=%v", returnedErr)
	}
	// item 保存详情视图成功时的关联商品。
	item := &ItemInfo{ItemID: "i", ItemTitle: "商品"}
	// successService 保存订单和商品都读取成功的详情服务。
	successService := NewDetailService(&detailRepositoryStub{order: &Order{OrderID: "o", CookieID: "c", ItemID: "i"}, owned: true, item: item})
	// result、resultErr 保存详情视图结果及错误。
	result, resultErr := successService.GetView(context.Background(), 1, "o")
	if resultErr != nil || result.Order == nil || result.Item != item {
		t.Fatalf("result=%+v err=%v", result, resultErr)
	}
}

// TestOrderDeleteServiceCoversNilAndOwnershipErrors 覆盖订单删除服务的依赖、空账号和归属错误。
func TestOrderDeleteServiceCoversNilAndOwnershipErrors(t *testing.T) {
	// nilDeleteService 验证 nil 删除服务不会触发 panic。
	var nilDeleteService *DeleteService
	if nilDeleteService.Delete(context.Background(), 1, "o") == nil {
		t.Fatal("nil delete service should fail")
	}
	// missingRepositoryService 验证缺少 repository 的删除服务错误。
	missingRepositoryService := NewDeleteService(nil)
	if missingRepositoryService.Delete(context.Background(), 1, "o") == nil {
		t.Fatal("missing delete repository should fail")
	}
	// emptyCookieService 保存订单账号为空的删除服务。
	emptyCookieService := NewDeleteService(&deleteRepositoryFake{order: &Order{CookieID: "\t"}})
	if !errors.Is(emptyCookieService.Delete(context.Background(), 1, "o"), ErrForbidden) {
		t.Fatal("empty cookie should be forbidden")
	}
	// ownershipErr 保存账号归属查询错误。
	ownershipErr := errors.New("ownership")
	// ownershipErrorService 保存账号归属查询失败的删除服务。
	ownershipErrorService := NewDeleteService(&deleteRepositoryFake{order: &Order{CookieID: "c"}, ownedErr: ownershipErr})
	if !errors.Is(ownershipErrorService.Delete(context.Background(), 1, "o"), ownershipErr) {
		t.Fatal("ownership error should propagate")
	}
}

// TestOrderListServiceCoversNilDependenciesAndDefaultPagination 覆盖订单列表服务的 nil 依赖和默认分页。
func TestOrderListServiceCoversNilDependenciesAndDefaultPagination(t *testing.T) {
	// nilListService 验证 nil 列表服务不会触发 panic。
	var nilListService *ListService
	// nilListErr 保存 nil 列表服务错误。
	if _, nilListErr := nilListService.List(context.Background(), ListQuery{}); nilListErr == nil {
		t.Fatal("nil list service should fail")
	}
	// missingRepositoryService 验证缺少 repository 的列表服务错误。
	missingRepositoryService := NewListService(nil)
	// missingRepositoryErr 保存缺少列表 repository 的错误。
	if _, missingRepositoryErr := missingRepositoryService.List(context.Background(), ListQuery{}); missingRepositoryErr == nil {
		t.Fatal("missing list repository should fail")
	}
	// repository 保存空结果列表依赖，触发默认页码和页大小。
	repository := &listRepositoryStub{owned: true, total: 0}
	// result、resultErr 保存默认分页结果。
	result, resultErr := NewListService(repository).List(context.Background(), ListQuery{Page: 0, PageSize: 0})
	if resultErr != nil || result.Page != 1 || result.PageSize != 20 || result.TotalPages != 0 {
		t.Fatalf("result=%+v err=%v", result, resultErr)
	}
}
