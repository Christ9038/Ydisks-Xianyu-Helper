package server

import (
	"context"

	"xianyu-go/internal/db"
)

// itemPublishRepository 定义发布应用服务处理商品与批次状态所需的最小持久化能力。
type itemPublishRepository interface {
	// LockCredentials 串行化账号凭证状态变更。
	LockCredentials(cookieID string) func()
	// LoadCookiePlatformDetail 读取平台发布所需的最小账号凭证视图。
	LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*db.CookieDetail, error)
	// UpdateCookieValueOwned 更新用户拥有账号的 Cookie。
	UpdateCookieValueOwned(ctx context.Context, cookieID, value string, userID int64) error
	// UpsertItem 写入发布成功后的商品基础信息。
	UpsertItem(ctx context.Context, item *db.ItemInfoRow) error
	// CreateBatch 创建批次及其明细行。
	CreateBatch(ctx context.Context, batch *db.ItemPublishBatch, rows []db.ItemPublishBatchRow) error
	// RecountBatch 重算批次统计。
	RecountBatch(ctx context.Context, batchID string) error
	// GetBatch 查询用户拥有的批次。
	GetBatch(ctx context.Context, userID int64, batchID string) (*db.ItemPublishBatch, error)
	// ClaimBatch 抢占批次 worker 租约。
	ClaimBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error)
	// PendingRows 查询批次待处理明细。
	PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]db.ItemPublishBatchRow, error)
	// FinalizeBatch 完成批次状态收口。
	FinalizeBatch(ctx context.Context, batchID, workerToken string) (string, bool, error)
	// ListBatchesForUser 查询用户批次列表。
	ListBatchesForUser(ctx context.Context, userID int64, limit int) ([]db.ItemPublishBatch, error)
	// ListBatchRows 查询批次明细行。
	ListBatchRows(ctx context.Context, batchID string) ([]db.ItemPublishBatchRow, error)
	// RequestCancel 请求取消批次。
	RequestCancel(ctx context.Context, batchID string) (string, bool, error)
	// DeleteBatch 删除用户拥有的批次。
	DeleteBatch(ctx context.Context, userID int64, batchID string) error
	// ResetFailed 重置批次失败明细。
	ResetFailed(ctx context.Context, batchID string) error
}

// storeItemPublishRepository 将完整 Store 适配为发布应用服务窄 repository。
type storeItemPublishRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// LockCredentials 委托账号凭证锁。
func (r storeItemPublishRepository) LockCredentials(cookieID string) func() {
	return r.store.LockAccountCredentials(cookieID)
}

// LoadCookiePlatformDetail 委托平台发布凭证查询并转换为 Server 内部模型。
func (r storeItemPublishRepository) LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*db.CookieDetail, error) {
	// data 和 err 保存平台运行视图查询结果。
	data, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	return &db.CookieDetail{ID: data.ID, UserID: data.UserID, Value: data.Value, MetadataJSON: data.MetadataJSON, ShowBrowser: data.ShowBrowser}, nil
}

// UpdateCookieValueOwned 委托账号 Cookie 更新。
func (r storeItemPublishRepository) UpdateCookieValueOwned(ctx context.Context, cookieID, value string, userID int64) error {
	return r.store.Cookies.UpdateValueOwned(ctx, cookieID, value, userID)
}

// UpsertItem 委托商品基础信息写入。
func (r storeItemPublishRepository) UpsertItem(ctx context.Context, item *db.ItemInfoRow) error {
	return r.store.Items.Upsert(ctx, item)
}

// CreateBatch 委托批次创建。
func (r storeItemPublishRepository) CreateBatch(ctx context.Context, batch *db.ItemPublishBatch, rows []db.ItemPublishBatchRow) error {
	return r.store.PublishBatches.Create(ctx, batch, rows)
}

// RecountBatch 委托批次统计重算。
func (r storeItemPublishRepository) RecountBatch(ctx context.Context, batchID string) error {
	return r.store.PublishBatches.Recount(ctx, batchID)
}

// GetBatch 委托用户批次查询。
func (r storeItemPublishRepository) GetBatch(ctx context.Context, userID int64, batchID string) (*db.ItemPublishBatch, error) {
	return r.store.PublishBatches.Get(ctx, userID, batchID)
}

// ClaimBatch 委托批次租约抢占。
func (r storeItemPublishRepository) ClaimBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	return r.store.PublishBatches.ClaimBatch(ctx, batchID, workerToken, leaseExpiresAt)
}

// PendingRows 委托批次待处理明细查询。
func (r storeItemPublishRepository) PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]db.ItemPublishBatchRow, error) {
	return r.store.PublishBatches.PendingRows(ctx, batchID, failedOnly)
}

// FinalizeBatch 委托批次完成状态收口。
func (r storeItemPublishRepository) FinalizeBatch(ctx context.Context, batchID, workerToken string) (string, bool, error) {
	return r.store.PublishBatches.FinalizeBatch(ctx, batchID, workerToken)
}

// ListBatchesForUser 委托用户批次列表查询。
func (r storeItemPublishRepository) ListBatchesForUser(ctx context.Context, userID int64, limit int) ([]db.ItemPublishBatch, error) {
	return r.store.PublishBatches.ListForUser(ctx, userID, limit)
}

// ListBatchRows 委托批次明细查询。
func (r storeItemPublishRepository) ListBatchRows(ctx context.Context, batchID string) ([]db.ItemPublishBatchRow, error) {
	return r.store.PublishBatches.Rows(ctx, batchID)
}

// RequestCancel 委托批次取消请求。
func (r storeItemPublishRepository) RequestCancel(ctx context.Context, batchID string) (string, bool, error) {
	return r.store.PublishBatches.RequestCancel(ctx, batchID)
}

// DeleteBatch 委托批次删除。
func (r storeItemPublishRepository) DeleteBatch(ctx context.Context, userID int64, batchID string) error {
	return r.store.PublishBatches.Delete(ctx, userID, batchID)
}

// ResetFailed 委托失败明细重置。
func (r storeItemPublishRepository) ResetFailed(ctx context.Context, batchID string) error {
	return r.store.PublishBatches.ResetFailed(ctx, batchID)
}

// newStoreItemPublishRepository 从完整 Store 构造发布应用服务窄 repository。
func newStoreItemPublishRepository(store *db.Store) itemPublishRepository {
	if store == nil || store.Cookies == nil || store.Items == nil || store.PublishBatches == nil {
		return nil
	}
	return storeItemPublishRepository{store: store}
}

// 确保 Store 适配器始终覆盖发布应用服务所需的全部能力。
var _ itemPublishRepository = storeItemPublishRepository{}
