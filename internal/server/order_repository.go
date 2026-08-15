package server

import (
	"context"
	"database/sql"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// orderRepository 定义订单应用服务执行订单、商品、归属、事务和凭证锁操作所需的最小能力。
type orderRepository interface {
	// ExistsOwned 判断账号是否归属于用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// ListOwnedIDs 返回用户拥有的账号 ID。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// ListOrdersForUser 查询用户范围内的订单列表。
	ListOrdersForUser(ctx context.Context, filter orderapp.ListFilter) ([]orderapp.OrderRow, int, error)
	// GetOrder 查询单个订单。
	GetOrder(ctx context.Context, orderID string) (*db.Order, error)
	// GetItem 查询账号下的商品信息。
	GetItem(ctx context.Context, cookieID, itemID string) (*db.ItemInfo, error)
	// SoftDeleteOrder 逻辑删除订单。
	SoftDeleteOrder(ctx context.Context, orderID string) (bool, error)
	// WithTransaction 在一个事务中执行订单用例的持久化操作。
	WithTransaction(ctx context.Context, work func(*sql.Tx) error) error
	// PatchOrderTx 在事务内更新订单字段。
	PatchOrderTx(ctx context.Context, tx *sql.Tx, orderID string, patch db.OrderPatch) error
	// UpsertItemBasicTx 在事务内写入商品基础信息。
	UpsertItemBasicTx(ctx context.Context, tx *sql.Tx, item *db.ItemInfoRow) error
	// UpsertOrderTx 在事务内写入订单。
	UpsertOrderTx(ctx context.Context, tx *sql.Tx, orderID string, opts db.OrderUpsertOpts) error
	// UpsertOrder 写入订单。
	UpsertOrder(ctx context.Context, orderID string, opts db.OrderUpsertOpts) error
	// LockCredentials 串行化账号凭证状态变更。
	LockCredentials(cookieID string) func()
	// LoadCookiePlatformDetail 读取账号平台凭证详情。
	LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*db.CookieDetail, error)
	// UpdateRenewalCookie 更新账号续期 Cookie 和 metadata。
	UpdateRenewalCookie(ctx context.Context, cookieID, value, metadata string, at int64) error
	// SoftDeleteMissingOrders 删除账号下远端已不存在的订单。
	SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error)
	// ListOrdersByCookiePage 分页读取账号订单。
	ListOrdersByCookiePage(ctx context.Context, cookieID string, limit, offset int) ([]db.OrderRow, error)
}

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

// GetOrder 委托订单详情查询。
func (r storeOrderRepository) GetOrder(ctx context.Context, orderID string) (*db.Order, error) {
	return r.store.Orders.Get(ctx, orderID)
}

// GetItem 委托商品信息查询。
func (r storeOrderRepository) GetItem(ctx context.Context, cookieID, itemID string) (*db.ItemInfo, error) {
	return r.store.Items.Get(ctx, cookieID, itemID)
}

// SoftDeleteOrder 委托订单逻辑删除。
func (r storeOrderRepository) SoftDeleteOrder(ctx context.Context, orderID string) (bool, error) {
	return r.store.Orders.SoftDelete(ctx, orderID)
}

// WithTransaction 创建、提交或回滚订单事务。
func (r storeOrderRepository) WithTransaction(ctx context.Context, work func(*sql.Tx) error) error {
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
	// err 保存事务工作函数的执行错误。
	if err := work(tx); err != nil {
		return err
	}
	// err 保存事务提交错误。
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// PatchOrderTx 委托事务内订单更新。
func (r storeOrderRepository) PatchOrderTx(ctx context.Context, tx *sql.Tx, orderID string, patch db.OrderPatch) error {
	return r.store.Orders.PatchTx(ctx, tx, orderID, patch)
}

// UpsertItemBasicTx 委托事务内商品基础信息写入。
func (r storeOrderRepository) UpsertItemBasicTx(ctx context.Context, tx *sql.Tx, item *db.ItemInfoRow) error {
	return r.store.Items.UpsertBasicTx(ctx, tx, item)
}

// UpsertOrderTx 委托事务内订单写入。
func (r storeOrderRepository) UpsertOrderTx(ctx context.Context, tx *sql.Tx, orderID string, opts db.OrderUpsertOpts) error {
	return r.store.Orders.UpsertTx(ctx, tx, orderID, opts)
}

// UpsertOrder 委托订单写入。
func (r storeOrderRepository) UpsertOrder(ctx context.Context, orderID string, opts db.OrderUpsertOpts) error {
	return r.store.Orders.Upsert(ctx, orderID, opts)
}

// LockCredentials 委托账号凭证锁。
func (r storeOrderRepository) LockCredentials(cookieID string) func() {
	return r.store.LockAccountCredentials(cookieID)
}

// LoadCookiePlatformDetail 委托平台凭证详情查询。
func (r storeOrderRepository) LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*db.CookieDetail, error) {
	// data 和 err 保存平台运行视图查询结果。
	data, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	return &db.CookieDetail{ID: data.ID, UserID: data.UserID, Value: data.Value, MetadataJSON: data.MetadataJSON, ShowBrowser: data.ShowBrowser}, nil
}

// UpdateRenewalCookie 委托续期 Cookie 更新。
func (r storeOrderRepository) UpdateRenewalCookie(ctx context.Context, cookieID, value, metadata string, at int64) error {
	return r.store.Cookies.UpdateRenewalCookie(ctx, cookieID, value, metadata, at)
}

// SoftDeleteMissingOrders 委托账号远端缺失订单清理。
func (r storeOrderRepository) SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	return r.store.Orders.SoftDeleteMissingForCookie(ctx, cookieID, activeIDs)
}

// ListOrdersByCookiePage 委托账号订单分页查询。
func (r storeOrderRepository) ListOrdersByCookiePage(ctx context.Context, cookieID string, limit, offset int) ([]db.OrderRow, error) {
	return r.store.Orders.ByCookiePage(ctx, cookieID, limit, offset)
}

// newStoreOrderRepository 从完整 Store 构造订单应用服务窄 repository。
func newStoreOrderRepository(store *db.Store) orderRepository {
	if store == nil || store.Cookies == nil || store.Orders == nil || store.Items == nil {
		return nil
	}
	return storeOrderRepository{store: store}
}

// 确保 Store 适配器始终覆盖订单应用服务所需的全部能力。
var _ orderRepository = storeOrderRepository{}
