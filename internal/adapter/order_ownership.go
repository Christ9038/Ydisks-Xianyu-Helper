package adapter

import (
	"context"
	"errors"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// FindOrderOwnership 为 userID 查询 orderID 的非敏感归属，包括软删除记录；跨用户结果不会暴露旧账号身份。
func (r OrderRepository) FindOrderOwnership(ctx context.Context, userID int64, orderID string) (orderapp.RefreshOwnership, error) {
	// snapshot、err 保存数据库的最小归属视图与错误，不读取凭证或地址。
	snapshot, err := r.store.Orders.FindOwnership(ctx, userID, orderID)
	if err != nil {
		return orderapp.RefreshOwnership{}, NormalizeOrderError(err)
	}
	return orderapp.RefreshOwnership{OrderID: snapshot.OrderID, CookieID: snapshot.CookieID, ItemID: snapshot.ItemID, BuyerID: snapshot.BuyerID,
		Version: snapshot.Version, Deleted: snapshot.Deleted, Owned: snapshot.Owned}, nil
}

// RecoverSoldOwnership 将应用层已授权的 expected 归属修正转换为数据库原子事务；options 只包含已售平台事实。
// userID 与 cookieID 必须在事务内再次复核，已知副作用与并发冲突转换为可展示的恢复拒绝，其他错误原样返回。
func (r OrderRepository) RecoverSoldOwnership(ctx context.Context, userID int64, cookieID string, expected orderapp.RefreshOwnership, options orderapp.UpsertOptions) error {
	// err 保存数据库原子修复结果，数据库错误不会被伪装为普通成功。
	err := r.store.Orders.RecoverSoldOwnership(ctx, userID, cookieID,
		db.OrderOwnership{OrderID: expected.OrderID, CookieID: expected.CookieID, ItemID: expected.ItemID, BuyerID: expected.BuyerID,
			Version: expected.Version, Deleted: expected.Deleted, Owned: expected.Owned},
		db.OrderUpsertOpts{CookieID: cookieID, ItemID: options.ItemID, BuyerID: options.BuyerID, CreatedAt: options.CreatedAt,
			OrderStatus: options.OrderStatus, Quantity: options.Quantity, Amount: options.Amount, ReceiverName: options.ReceiverName,
			ReceiverPhone: options.ReceiverPhone, ReceiverAddr: options.ReceiverAddress, ReceiverCity: options.ReceiverCity, IsBargain: options.IsBargain})
	if errors.Is(err, db.ErrOrderRecoveryUnsafe) || errors.Is(err, db.ErrOrderConflict) {
		return orderapp.ErrSoldRecoveryUnsafe
	}
	return NormalizeOrderError(err)
}
