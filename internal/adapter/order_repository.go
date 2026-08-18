package adapter

import (
	"context"
	"errors"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// OrderRepository 将数据库 Store 适配为订单应用服务窄 repository。
type OrderRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// ExistsOwned 委托账号归属查询。
func (r OrderRepository) ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
}

// ListOwnedIDs 委托用户账号列表查询。
func (r OrderRepository) ListOwnedIDs(ctx context.Context, userID int64) ([]string, error) {
	return r.store.Cookies.ListOwnedIDs(ctx, userID)
}

// ListOrdersForUser 委托用户订单列表查询。
func (r OrderRepository) ListOrdersForUser(ctx context.Context, filter orderapp.ListFilter) ([]orderapp.OrderRow, int, error) {
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
func (r OrderRepository) GetOrder(ctx context.Context, orderID string) (*orderapp.Order, error) {
	// order 和 err 保存数据库订单查询结果及其错误。
	order, err := r.store.Orders.Get(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return orderFromDB(order), nil
}

// FindOrder 委托订单查询并把数据库的不存在错误转换为 exists=false。
func (r OrderRepository) FindOrder(ctx context.Context, orderID string) (*orderapp.Order, bool, error) {
	// order、err 保存数据库订单查询结果及其错误。
	order, err := r.store.Orders.Get(ctx, orderID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return orderFromDB(order), true, nil
}

// FindOrdersByIDs 委托数据库批量读取订单并转换为应用实体。
func (r OrderRepository) FindOrdersByIDs(ctx context.Context, orderIDs []string) (map[string]*orderapp.Order, error) {
	// orders、err 保存数据库批量订单及查询错误。
	orders, err := r.store.Orders.FindByIDs(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	// converted 保存转换后的应用订单索引。
	converted := make(map[string]*orderapp.Order, len(orders))
	// orderID、order 保存当前数据库订单的标识和实体。
	for orderID, order := range orders {
		converted[orderID] = orderFromDB(order)
	}
	return converted, nil
}

// GetItem 委托商品信息查询。
func (r OrderRepository) GetItem(ctx context.Context, cookieID, itemID string) (*orderapp.ItemInfo, error) {
	// item 和 err 保存数据库商品查询结果及其错误。
	item, err := r.store.Items.Get(ctx, cookieID, itemID)
	if err != nil {
		return nil, err
	}
	return itemInfoFromDB(item), nil
}

// SoftDeleteOrder 委托订单逻辑删除。
func (r OrderRepository) SoftDeleteOrder(ctx context.Context, orderID string) (bool, error) {
	return r.store.Orders.SoftDelete(ctx, orderID)
}

// WithTransaction 创建、提交或回滚订单事务。
func (r OrderRepository) WithTransaction(ctx context.Context, work func(orderapp.Writer) error) error {
	if r.store == nil || r.store.OrderWrites == nil {
		return errors.New("订单写入 Unit of Work 未初始化")
	}
	if work == nil {
		return errors.New("订单写入事务工作函数不能为空")
	}
	// transaction 是 db 层创建的窄事务写入能力，适配器不会接触或暴露原始 SQL 事务。
	return r.store.OrderWrites.WithTransaction(ctx, func(transaction *db.OrderWriteTransaction) error {
		// writer 将应用订单模型转换为当前事务的 db 写入模型。
		writer := orderWriter{transaction: transaction}
		return work(writer)
	})
}

// orderWriter 将订单应用写入模型适配为数据库事务操作。
type orderWriter struct {
	// transaction 保存订单/商品窄事务写入能力，不包含可供上层执行任意 SQL 的接口。
	transaction *db.OrderWriteTransaction
}

// PatchOrder 委托事务内订单更新。
func (w orderWriter) PatchOrder(ctx context.Context, orderID string, patch orderapp.OrderPatch) error {
	return w.transaction.PatchOrder(ctx, orderID, db.OrderPatch{
		OrderStatus: patch.OrderStatus, ItemID: patch.ItemID, BuyerID: patch.BuyerID,
		SpecName: patch.SpecName, SpecValue: patch.SpecValue, Quantity: patch.Quantity,
		Amount: patch.Amount, ReceiverName: patch.ReceiverName, ReceiverPhone: patch.ReceiverPhone,
		ReceiverAddr: patch.ReceiverAddress, ReceiverCity: patch.ReceiverCity, ChatID: patch.ChatID,
		SystemShipped: patch.SystemShipped,
	})
}

// UpsertItemBasic 委托事务内商品基础信息写入。
func (w orderWriter) UpsertItemBasic(ctx context.Context, item orderapp.ItemWrite) error {
	return w.transaction.UpsertItemBasic(ctx, &db.ItemInfoRow{
		CookieID: item.CookieID, ItemID: item.ItemID, ItemTitle: item.ItemTitle,
		ItemPrice: item.ItemPrice, ItemDetail: item.ItemDetail,
	})
}

// UpsertOrder 委托事务内订单写入。
func (w orderWriter) UpsertOrder(ctx context.Context, orderID string, options orderapp.UpsertOptions) error {
	return w.transaction.UpsertOrder(ctx, orderID, db.OrderUpsertOpts{
		ItemID: options.ItemID, BuyerID: options.BuyerID, CookieID: options.CookieID,
		OrderStatus: options.OrderStatus, SpecName: options.SpecName, SpecValue: options.SpecValue,
		Quantity: options.Quantity, Amount: options.Amount, ReceiverName: options.ReceiverName,
		ReceiverPhone: options.ReceiverPhone, ReceiverAddr: options.ReceiverAddress,
		ReceiverCity: options.ReceiverCity, ChatID: options.ChatID,
		IsBargain: options.IsBargain, SystemShipped: options.SystemShipped,
	})
}

// UpsertOrder 委托订单写入。
func (r OrderRepository) UpsertOrder(ctx context.Context, orderID string, opts orderapp.UpsertOptions) error {
	return r.store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
		ItemID: opts.ItemID, BuyerID: opts.BuyerID, CookieID: opts.CookieID,
		OrderStatus: opts.OrderStatus, SpecName: opts.SpecName, SpecValue: opts.SpecValue,
		Quantity: opts.Quantity, Amount: opts.Amount, ReceiverName: opts.ReceiverName,
		ReceiverPhone: opts.ReceiverPhone, ReceiverAddr: opts.ReceiverAddress,
		ReceiverCity: opts.ReceiverCity, ChatID: opts.ChatID,
		IsBargain: opts.IsBargain, SystemShipped: opts.SystemShipped,
	})
}

// BatchUpsertOrders 委托订单详情分片的单条多值 UPSERT。
func (r OrderRepository) BatchUpsertOrders(ctx context.Context, rows []orderapp.RefreshOrderWrite) error {
	if len(rows) == 0 {
		return nil
	}
	// converted 保存数据库批量写入模型。
	converted := make([]db.BatchOrderUpsert, 0, len(rows))
	// row 是当前待转换的应用层批量订单记录。
	for _, row := range rows {
		converted = append(converted, db.BatchOrderUpsert{OrderID: row.OrderID, Options: db.OrderUpsertOpts{ItemID: row.Options.ItemID, BuyerID: row.Options.BuyerID, CookieID: row.Options.CookieID, OrderStatus: row.Options.OrderStatus, SpecName: row.Options.SpecName, SpecValue: row.Options.SpecValue, Quantity: row.Options.Quantity, Amount: row.Options.Amount, ReceiverName: row.Options.ReceiverName, ReceiverPhone: row.Options.ReceiverPhone, ReceiverAddr: row.Options.ReceiverAddress, ReceiverCity: row.Options.ReceiverCity, ChatID: row.Options.ChatID, IsBargain: row.Options.IsBargain, SystemShipped: row.Options.SystemShipped}})
	}
	if r.store == nil || r.store.OrderWrites == nil {
		return errors.New("订单写入 Unit of Work 未初始化")
	}
	// transaction 是 db 层管理的详情分片事务，批量写入失败时不会留下部分订单。
	return r.store.OrderWrites.WithTransaction(ctx, func(transaction *db.OrderWriteTransaction) error {
		return transaction.UpsertOrders(ctx, converted)
	})
}

// LockCredentials 委托账号凭证锁。
func (r OrderRepository) LockCredentials(cookieID string) func() {
	return r.store.LockAccountCredentials(cookieID)
}

// LoadCookiePlatformDetail 委托平台凭证详情查询。
func (r OrderRepository) LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*orderapp.PlatformRuntimeData, error) {
	// data 和 err 保存平台运行视图查询结果。
	data, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	return platformRuntimeDataFromDB(data), nil
}

// UpdateRenewalCookie 委托续期 Cookie 更新。
func (r OrderRepository) UpdateRenewalCookie(ctx context.Context, cookieID, value, metadata string, at int64) error {
	return r.store.Cookies.UpdateRenewalCookie(ctx, cookieID, value, metadata, at)
}

// SoftDeleteMissingOrders 委托账号远端缺失订单清理。
func (r OrderRepository) SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	return r.store.Orders.SoftDeleteMissingForCookie(ctx, cookieID, activeIDs)
}

// ListOrdersByCookieCursor 委托订单复合游标查询并转换应用层模型。
func (r OrderRepository) ListOrdersByCookieCursor(ctx context.Context, cookieID string, limit int, afterCreatedAt, afterOrderID string) ([]orderapp.OrderRow, error) {
	// rows、err 保存数据库游标查询结果及错误。
	rows, err := r.store.Orders.ByCookieCursor(ctx, cookieID, limit, afterCreatedAt, afterOrderID)
	if err != nil {
		return nil, err
	}
	return orderRowsFromDB(rows), nil
}

// Create 创建订单刷新后台任务并同步应用层模型默认值。
func (r OrderRepository) Create(ctx context.Context, job *orderapp.RefreshJob) error {
	// dbJob 保存待写入的数据库任务模型。
	dbJob := &db.OrderRefreshJob{
		ID: job.ID, UserID: job.UserID, CookieID: job.CookieID, FilterStatus: job.FilterStatus,
		Status: job.Status, ResultJSON: job.ResultJSON, ErrorMessage: job.ErrorMessage,
		WorkerToken: job.WorkerToken, LeaseExpiresAt: job.LeaseExpiresAt,
	}
	// err 表示数据库任务创建错误。
	if err := r.store.OrderRefreshJobs.Create(ctx, dbJob); err != nil {
		return err
	}
	job.Status, job.ResultJSON = dbJob.Status, dbJob.ResultJSON
	return nil
}

// Get 按用户读取订单刷新后台任务并转换为应用层模型。
func (r OrderRepository) Get(ctx context.Context, userID int64, id string) (*orderapp.RefreshJob, error) {
	// job、err 保存数据库任务读取结果及错误。
	job, err := r.store.OrderRefreshJobs.Get(ctx, userID, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, orderapp.ErrRefreshJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return refreshJobFromDB(job), nil
}

// Claim 原子抢占订单刷新后台任务。
func (r OrderRepository) Claim(ctx context.Context, id, token string, leaseExpiresAt int64) (bool, error) {
	return r.store.OrderRefreshJobs.Claim(ctx, id, token, leaseExpiresAt)
}

// Cancel 按用户归属委托订单刷新任务取消。
func (r OrderRepository) Cancel(ctx context.Context, userID int64, id string) (bool, error) {
	return r.store.OrderRefreshJobs.Cancel(ctx, userID, id)
}

// Complete 以租约令牌安全写入订单刷新后台任务终态。
func (r OrderRepository) Complete(ctx context.Context, id, token, status, resultJSON, errorMessage string) (bool, error) {
	return r.store.OrderRefreshJobs.Complete(ctx, id, token, status, resultJSON, errorMessage)
}

// Recoverable 查询租约过期的订单刷新后台任务。
func (r OrderRepository) Recoverable(ctx context.Context, now int64, limit int) ([]orderapp.RefreshJob, error) {
	// jobs、err 保存数据库层可恢复任务及查询错误。
	jobs, err := r.store.OrderRefreshJobs.Recoverable(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	// converted 保存转换后的应用层任务列表。
	converted := make([]orderapp.RefreshJob, 0, len(jobs))
	for _, job := range jobs { // job 表示当前待转换的数据库任务。
		converted = append(converted, *refreshJobFromDB(&job))
	}
	return converted, nil
}

// RequeueExpired 将过期订单刷新后台任务恢复为 queued。
func (r OrderRepository) RequeueExpired(ctx context.Context, id string, now int64) (bool, error) {
	return r.store.OrderRefreshJobs.RequeueExpired(ctx, id, now)
}

// refreshJobFromDB 将数据库任务模型转换为应用层任务模型。
func refreshJobFromDB(job *db.OrderRefreshJob) *orderapp.RefreshJob {
	if job == nil {
		return nil
	}
	return &orderapp.RefreshJob{
		ID: job.ID, UserID: job.UserID, CookieID: job.CookieID, FilterStatus: job.FilterStatus,
		Status: job.Status, ResultJSON: job.ResultJSON, ErrorMessage: job.ErrorMessage,
		WorkerToken: job.WorkerToken, LeaseExpiresAt: job.LeaseExpiresAt,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

// NewOrderRepository 从数据库 Store 构造订单应用服务适配器。
func NewOrderRepository(store *db.Store) *OrderRepository {
	if store == nil || store.Cookies == nil || store.Orders == nil || store.Items == nil {
		return nil
	}
	return &OrderRepository{store: store}
}

// NewOrderRefreshJobRepository 构造订单刷新任务持久化适配器。
func NewOrderRefreshJobRepository(store *db.Store) orderapp.RefreshJobRepository {
	if store == nil || store.OrderRefreshJobs == nil {
		return nil
	}
	return OrderRepository{store: store}
}

// 确保 Store 适配器始终覆盖订单应用服务所需的全部能力。
var _ orderapp.Repository = OrderRepository{}
var _ orderapp.UnitOfWork = OrderRepository{}
var _ orderapp.Writer = orderWriter{}
var _ orderapp.RefreshJobRepository = OrderRepository{}
