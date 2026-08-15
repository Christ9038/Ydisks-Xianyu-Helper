package server

import (
	"context"
	"database/sql"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// storeOrderRepository 将完整 Store 适配为订单应用服务窄 repository。
type storeOrderRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// ExistsOwned 委托账号归属查询。
func (r storeOrderRepository) ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
}

// ListOwnedIDs 委托用户账号列表查询。
func (r storeOrderRepository) ListOwnedIDs(ctx context.Context, userID int64) ([]string, error) {
	return r.store.Cookies.ListOwnedIDs(ctx, userID)
}

// ListOrdersForUser 委托用户订单列表查询。
func (r storeOrderRepository) ListOrdersForUser(ctx context.Context, filter orderapp.ListFilter) ([]orderapp.OrderRow, int, error) {
	// rows、total、err 是数据库订单列表查询结果及其错误。
	rows, total, err := r.store.Orders.ListForUser(ctx, db.OrderListFilter{
		UserID: filter.UserID, CookieID: filter.CookieID, Status: filter.Status,
		Search: filter.Search, Limit: filter.Limit, Offset: filter.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return orderRowsFromDB(rows), total, nil
}

// orderRowsFromDB 将数据库列表模型转换为订单应用层模型。
func orderRowsFromDB(rows []db.OrderRow) []orderapp.OrderRow {
	// converted 保存转换后的应用层订单行。
	converted := make([]orderapp.OrderRow, 0, len(rows))
	for _, row := range rows { // row 是待转换的数据库订单列表行。
		converted = append(converted, orderapp.OrderRow{
			OrderID: row.OrderID, ItemID: row.ItemID, ItemTitle: row.ItemTitle,
			ItemDetail: row.ItemDetail, BuyerID: row.BuyerID, SpecName: row.SpecName,
			SpecValue: row.SpecValue, Quantity: row.Quantity, Amount: row.Amount,
			OrderStatus: row.OrderStatus, CookieID: row.CookieID, IsBargain: row.IsBargain,
			SystemShipped: row.SystemShipped, ReceiverName: row.ReceiverName,
			ReceiverPhone: row.ReceiverPhone, ReceiverAddr: row.ReceiverAddr,
			ReceiverCity: row.ReceiverCity, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return converted
}

// orderFromDB 将数据库订单实体转换为不暴露存储层字段命名的应用实体。
func orderFromDB(order *db.Order) *orderapp.Order {
	if order == nil {
		return nil
	}
	return &orderapp.Order{
		OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID,
		SpecName: order.SpecName, SpecValue: order.SpecValue, Quantity: order.Quantity,
		Amount: order.Amount, OrderStatus: order.OrderStatus, CookieID: order.CookieID,
		IsBargain: order.IsBargain, ReceiverName: order.ReceiverName,
		ReceiverPhone: order.ReceiverPhone, ReceiverAddress: order.ReceiverAddr,
		ReceiverCity: order.ReceiverCity, Version: order.Version, ChatID: order.ChatID,
		SystemShipped: order.SystemShipped, PaidAt: order.PaidAt, ShippedAt: order.ShippedAt,
		CompletedAt: order.CompletedAt, BuyerReviewedAt: order.BuyerReviewedAt,
		LastReviewRequestAt: order.LastReviewRequestAt, ReviewRequestCount: order.ReviewRequestCount,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}

// itemInfoFromDB 将数据库商品实体转换为订单应用层商品模型。
func itemInfoFromDB(item *db.ItemInfo) *orderapp.ItemInfo {
	if item == nil {
		return nil
	}
	return &orderapp.ItemInfo{
		ID: item.ID, CookieID: item.CookieID, ItemID: item.ItemID,
		ItemTitle: item.ItemTitle, ItemDescription: item.ItemDescription,
		ItemCategory: item.ItemCategory, ItemPrice: item.ItemPrice,
		ItemDetail: item.ItemDetail, IsMultiSpec: item.IsMultiSpec,
		MultiQuantityDelivery: item.MultiQuantityDelivery,
	}
}

// platformRuntimeDataFromDB 将数据库平台运行视图转换为订单应用层模型。
func platformRuntimeDataFromDB(data db.CookiePlatformRuntimeData) *orderapp.PlatformRuntimeData {
	return &orderapp.PlatformRuntimeData{
		ID: data.ID, UserID: data.UserID, Value: data.Value,
		MetadataJSON: data.MetadataJSON, ShowBrowser: data.ShowBrowser,
	}
}

// GetOrder 委托订单详情查询。
func (r storeOrderRepository) GetOrder(ctx context.Context, orderID string) (*orderapp.Order, error) {
	// order 和 err 保存数据库订单查询结果及其错误。
	order, err := r.store.Orders.Get(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return orderFromDB(order), nil
}

// GetItem 委托商品信息查询。
func (r storeOrderRepository) GetItem(ctx context.Context, cookieID, itemID string) (*orderapp.ItemInfo, error) {
	// item 和 err 保存数据库商品查询结果及其错误。
	item, err := r.store.Items.Get(ctx, cookieID, itemID)
	if err != nil {
		return nil, err
	}
	return itemInfoFromDB(item), nil
}

// SoftDeleteOrder 委托订单逻辑删除。
func (r storeOrderRepository) SoftDeleteOrder(ctx context.Context, orderID string) (bool, error) {
	return r.store.Orders.SoftDelete(ctx, orderID)
}

// WithTransaction 创建、提交或回滚订单事务。
func (r storeOrderRepository) WithTransaction(ctx context.Context, work func(orderapp.Writer) error) error {
	// tx 和 err 保存事务创建结果。
	tx, err := r.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// committed 标识事务是否已经提交成功。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// writer 是隐藏数据库事务细节的订单写入适配器。
	writer := storeOrderWriter{store: r.store, tx: tx}
	// err 保存事务工作函数的执行错误。
	if err := work(writer); err != nil {
		return err
	}
	// err 保存事务提交错误。
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// storeOrderWriter 将订单应用写入模型适配为数据库事务操作。
type storeOrderWriter struct {
	// store 保存数据库 repository 聚合入口。
	store *db.Store
	// tx 保存当前事务，仅在基础设施适配器内部可见。
	tx *sql.Tx
}

// PatchOrder 委托事务内订单更新。
func (w storeOrderWriter) PatchOrder(ctx context.Context, orderID string, patch orderapp.OrderPatch) error {
	return w.store.Orders.PatchTx(ctx, w.tx, orderID, db.OrderPatch{
		OrderStatus: patch.OrderStatus, ItemID: patch.ItemID, BuyerID: patch.BuyerID,
		SpecName: patch.SpecName, SpecValue: patch.SpecValue, Quantity: patch.Quantity,
		Amount: patch.Amount, ReceiverName: patch.ReceiverName, ReceiverPhone: patch.ReceiverPhone,
		ReceiverAddr: patch.ReceiverAddress, ReceiverCity: patch.ReceiverCity, ChatID: patch.ChatID,
		SystemShipped: patch.SystemShipped,
	})
}

// UpsertItemBasic 委托事务内商品基础信息写入。
func (w storeOrderWriter) UpsertItemBasic(ctx context.Context, item orderapp.ItemWrite) error {
	return w.store.Items.UpsertBasicTx(ctx, w.tx, &db.ItemInfoRow{
		CookieID: item.CookieID, ItemID: item.ItemID, ItemTitle: item.ItemTitle,
		ItemPrice: item.ItemPrice, ItemDetail: item.ItemDetail,
	})
}

// UpsertOrder 委托事务内订单写入。
func (w storeOrderWriter) UpsertOrder(ctx context.Context, orderID string, options orderapp.UpsertOptions) error {
	return w.store.Orders.UpsertTx(ctx, w.tx, orderID, db.OrderUpsertOpts{
		ItemID: options.ItemID, BuyerID: options.BuyerID, CookieID: options.CookieID,
		OrderStatus: options.OrderStatus, SpecName: options.SpecName, SpecValue: options.SpecValue,
		Quantity: options.Quantity, Amount: options.Amount, ReceiverName: options.ReceiverName,
		ReceiverPhone: options.ReceiverPhone, ReceiverAddr: options.ReceiverAddress,
		ReceiverCity: options.ReceiverCity, ChatID: options.ChatID,
		IsBargain: options.IsBargain, SystemShipped: options.SystemShipped,
	})
}

// UpsertOrder 委托订单写入。
func (r storeOrderRepository) UpsertOrder(ctx context.Context, orderID string, opts orderapp.UpsertOptions) error {
	return r.store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
		ItemID: opts.ItemID, BuyerID: opts.BuyerID, CookieID: opts.CookieID,
		OrderStatus: opts.OrderStatus, SpecName: opts.SpecName, SpecValue: opts.SpecValue,
		Quantity: opts.Quantity, Amount: opts.Amount, ReceiverName: opts.ReceiverName,
		ReceiverPhone: opts.ReceiverPhone, ReceiverAddr: opts.ReceiverAddress,
		ReceiverCity: opts.ReceiverCity, ChatID: opts.ChatID,
		IsBargain: opts.IsBargain, SystemShipped: opts.SystemShipped,
	})
}

// LockCredentials 委托账号凭证锁。
func (r storeOrderRepository) LockCredentials(cookieID string) func() {
	return r.store.LockAccountCredentials(cookieID)
}

// LoadCookiePlatformDetail 委托平台凭证详情查询。
func (r storeOrderRepository) LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*orderapp.PlatformRuntimeData, error) {
	// data 和 err 保存平台运行视图查询结果。
	data, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	return platformRuntimeDataFromDB(data), nil
}

// UpdateRenewalCookie 委托续期 Cookie 更新。
func (r storeOrderRepository) UpdateRenewalCookie(ctx context.Context, cookieID, value, metadata string, at int64) error {
	return r.store.Cookies.UpdateRenewalCookie(ctx, cookieID, value, metadata, at)
}

// SoftDeleteMissingOrders 委托账号远端缺失订单清理。
func (r storeOrderRepository) SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	return r.store.Orders.SoftDeleteMissingForCookie(ctx, cookieID, activeIDs)
}

// ListOrdersByCookieCursor 委托订单复合游标查询并转换应用层模型。
func (r storeOrderRepository) ListOrdersByCookieCursor(ctx context.Context, cookieID string, limit int, afterCreatedAt, afterOrderID string) ([]orderapp.OrderRow, error) {
	// rows、err 保存数据库游标查询结果及错误。
	rows, err := r.store.Orders.ByCookieCursor(ctx, cookieID, limit, afterCreatedAt, afterOrderID)
	if err != nil {
		return nil, err
	}
	return orderRowsFromDB(rows), nil
}

// newStoreOrderRepository 从完整 Store 构造订单应用服务窄 repository。
func newStoreOrderRepository(store *db.Store) orderapp.Repository {
	if store == nil || store.Cookies == nil || store.Orders == nil || store.Items == nil {
		return nil
	}
	return storeOrderRepository{store: store}
}

// 确保 Store 适配器始终覆盖订单应用服务所需的全部能力。
var _ orderapp.Repository = storeOrderRepository{}
var _ orderapp.UnitOfWork = storeOrderRepository{}
var _ orderapp.Writer = storeOrderWriter{}
