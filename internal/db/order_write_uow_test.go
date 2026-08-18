package db

import (
	"context"
	"errors"
	"testing"
)

// TestOrderWriteUnitOfWorkCommitsOrRollsBackBothRepositories 验证订单和商品基础信息只会一起提交或一起回滚。
func TestOrderWriteUnitOfWorkCommitsOrRollsBackBothRepositories(t *testing.T) {
	// store、cleanup 分别是独立 SQLite 仓储和关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试所有数据库操作共享的非取消上下文。
	ctx := context.Background()
	// cookieID 是已建立归属关系、可供订单和商品共同引用的账号标识。
	_, cookieID := seedAccount(t, store)
	// rollbackErr 是事务回调返回的受控错误，用于验证此前两类写入会被整体撤销。
	rollbackErr := errors.New("强制回滚")
	// transactionErr 保存回滚事务对调用方返回的原始错误。
	transactionErr := store.OrderWrites.WithTransaction(ctx, func(writer *OrderWriteTransaction) error {
		// orderErr 保存事务内订单写入错误。
		if orderErr := writer.UpsertOrder(ctx, "uow-rollback", OrderUpsertOpts{CookieID: cookieID, ItemID: "item-rollback", OrderStatus: "paid"}); orderErr != nil {
			return orderErr
		}
		// itemErr 保存事务内商品基础信息写入错误。
		if itemErr := writer.UpsertItemBasic(ctx, &ItemInfoRow{CookieID: cookieID, ItemID: "item-rollback", ItemTitle: "回滚商品"}); itemErr != nil {
			return itemErr
		}
		return rollbackErr
	})
	if !errors.Is(transactionErr, rollbackErr) {
		t.Fatalf("事务回滚错误=%v，期望=%v", transactionErr, rollbackErr)
	}
	// _, orderErr 保存回滚后读取订单的结果；订单不得对事务外可见。
	if _, orderErr := store.Orders.Get(ctx, "uow-rollback"); !errors.Is(orderErr, ErrNotFound) {
		t.Fatalf("回滚后订单仍存在: %v", orderErr)
	}
	// _, itemErr 保存回滚后读取商品的结果；商品不得对事务外可见。
	if _, itemErr := store.Items.Get(ctx, cookieID, "item-rollback"); !errors.Is(itemErr, ErrNotFound) {
		t.Fatalf("回滚后商品仍存在: %v", itemErr)
	}
	// commitErr 保存同时写入订单与商品后的事务提交错误。
	commitErr := store.OrderWrites.WithTransaction(ctx, func(writer *OrderWriteTransaction) error {
		// orderErr 保存事务内订单写入错误。
		if orderErr := writer.UpsertOrder(ctx, "uow-commit", OrderUpsertOpts{CookieID: cookieID, ItemID: "item-commit", OrderStatus: "paid"}); orderErr != nil {
			return orderErr
		}
		// itemErr 保存事务内商品基础信息写入错误。
		return writer.UpsertItemBasic(ctx, &ItemInfoRow{CookieID: cookieID, ItemID: "item-commit", ItemTitle: "提交商品"})
	})
	if commitErr != nil {
		t.Fatalf("事务提交失败: %v", commitErr)
	}
	// order、orderErr 保存提交后读取到的订单及错误。
	order, orderErr := store.Orders.Get(ctx, "uow-commit")
	if orderErr != nil || order.CookieID != cookieID || order.ItemID != "item-commit" {
		t.Fatalf("提交后订单异常: order=%+v err=%v", order, orderErr)
	}
	// item、itemErr 保存提交后读取到的商品及错误。
	item, itemErr := store.Items.Get(ctx, cookieID, "item-commit")
	if itemErr != nil || item.ItemTitle != "提交商品" {
		t.Fatalf("提交后商品异常: item=%+v err=%v", item, itemErr)
	}
}
