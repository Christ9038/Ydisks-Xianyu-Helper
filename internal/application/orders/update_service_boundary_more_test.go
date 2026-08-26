package orders

import (
	"context"
	"errors"
	"testing"
)

// TestUpdateServiceCoversRepositoryAndOrderGuards 覆盖订单更新服务的空依赖、订单实体和归属错误边界。
func TestUpdateServiceCoversRepositoryAndOrderGuards(t *testing.T) {
	// nilService 表示订单更新服务接收者为空。
	var nilService *UpdateService
	if nilService.Update(context.Background(), 1, "order", UpdateRequest{}) == nil {
		t.Fatal("nil update service should fail")
	}
	// emptyService 表示订单更新服务的 repository 未装配。
	emptyService := NewUpdateService(nil)
	if emptyService.Update(context.Background(), 1, "order", UpdateRequest{}) == nil {
		t.Fatal("nil update repository should fail")
	}
	// nilOrderService 表示仓储没有返回订单实体且没有底层错误。
	nilOrderService := NewUpdateService(&updateRepositoryFake{order: nil})
	if !errors.Is(nilOrderService.Update(context.Background(), 1, "order", UpdateRequest{}), ErrNotFound) {
		t.Fatal("nil order should map to not found")
	}
	// blankCookieService 表示订单缺少账号关联标识。
	blankCookieService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "  "}})
	if !errors.Is(blankCookieService.Update(context.Background(), 1, "order", UpdateRequest{}), ErrForbidden) {
		t.Fatal("blank cookie should be forbidden")
	}
	// ownershipErr 保存账号归属查询失败的底层错误。
	ownershipErr := errors.New("ownership lookup failed")
	// ownershipService 保存返回归属错误的更新服务。
	ownershipService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie"}, ownedErr: ownershipErr})
	if !errors.Is(ownershipService.Update(context.Background(), 1, "order", UpdateRequest{}), ownershipErr) {
		t.Fatal("ownership error should propagate")
	}
	// itemTitle 保存触发商品标题校验的标题文本。
	itemTitle := "标题"
	// emptyItemIDService 表示商品标题更新时订单最终商品标识为空。
	emptyItemIDService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie"}, owned: true})
	if emptyItemIDService.Update(context.Background(), 1, "order", UpdateRequest{ItemTitle: &itemTitle}) == nil {
		t.Fatal("item title without item ID should fail")
	}
}

// TestUpdateServiceCoversWriterAndTransactionErrors 覆盖订单补丁、商品标题和事务边界错误传播。
func TestUpdateServiceCoversWriterAndTransactionErrors(t *testing.T) {
	// transactionErr 保存事务创建或执行阶段的错误。
	transactionErr := errors.New("transaction failed")
	// transactionService 保存事务边界返回错误的更新服务。
	transactionService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie"}, owned: true, txErr: transactionErr})
	if !errors.Is(transactionService.Update(context.Background(), 1, "order", UpdateRequest{}), transactionErr) {
		t.Fatal("transaction error should propagate")
	}
	// patchErr 保存订单补丁写入失败的错误。
	patchErr := errors.New("patch failed")
	// patchService 保存订单补丁返回错误的更新服务。
	patchService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie"}, owned: true, patchErr: patchErr})
	if !errors.Is(patchService.Update(context.Background(), 1, "order", UpdateRequest{}), patchErr) {
		t.Fatal("patch error should propagate")
	}
	// itemErr 保存商品标题写入失败的错误。
	itemErr := errors.New("item write failed")
	// itemService 保存商品标题写入返回错误的更新服务。
	itemService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie", ItemID: "item"}, owned: true, itemErr: itemErr})
	// itemTitle 保存触发商品标题写入的规范化标题。
	itemTitle := "新标题"
	// updateErr 保存商品标题错误的包装结果。
	updateErr := itemService.Update(context.Background(), 1, "order", UpdateRequest{ItemTitle: &itemTitle})
	if !errors.Is(updateErr, itemErr) || updateErr == nil {
		t.Fatalf("item error=%v", updateErr)
	}
	// successService 保存无商品标题更新的成功事务场景。
	successService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie"}, owned: true})
	// err 保存无商品标题更新的事务结果。
	if err := successService.Update(context.Background(), 1, "order", UpdateRequest{}); err != nil {
		t.Fatalf("无商品标题更新不应失败: %v", err)
	}
}
