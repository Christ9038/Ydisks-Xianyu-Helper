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
	// RecoverableBatches 查询可恢复的批次。
	RecoverableBatches(ctx context.Context, now int64, limit int) ([]db.ItemPublishBatch, error)
	// FinalizeExpiredCancellation 收口已过期的取消请求。
	FinalizeExpiredCancellation(ctx context.Context, batchID string, now int64) (bool, error)
	// ResetInterrupted 重置进程中断的批次。
	ResetInterrupted(ctx context.Context, batchID string) error
	// FailClaimedBatch 标记当前 worker 持有的批次失败。
	FailClaimedBatch(ctx context.Context, batchID, workerToken string) (bool, error)
	// RenewBatchLease 续租批次 worker。
	RenewBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error)
	// ClaimRow 抢占批次明细行。
	ClaimRow(ctx context.Context, rowID int64, workerToken string) (bool, error)
	// BatchStatus 查询批次状态。
	BatchStatus(ctx context.Context, batchID string) (string, error)
	// MarkClaimedRowFailed 标记 worker 持有的明细行失败。
	MarkClaimedRowFailed(ctx context.Context, rowID int64, workerToken, message, kind string) (bool, error)
	// FinalizeCanceled 收口已取消批次。
	FinalizeCanceled(ctx context.Context, batchID, workerToken string) (bool, error)
	// FinalizeInterrupted 收口被中断批次。
	FinalizeInterrupted(ctx context.Context, batchID, workerToken, message string) (string, bool, error)
	// MarkClaimedRemoteStarted 记录远端发布已开始。
	MarkClaimedRemoteStarted(ctx context.Context, rowID int64, workerToken string) (bool, error)
	// SaveClaimedRemoteResult 保存远端发布结果。
	SaveClaimedRemoteResult(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error)
	// MarkClaimedRowSuccess 标记 worker 持有的明细行成功。
	MarkClaimedRowSuccess(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error)
	// ClearUploadDir 清理批次上传目录记录。
	ClearUploadDir(ctx context.Context, batchID string) error
	// ExpiredUploads 查询过期上传目录。
	ExpiredUploads(ctx context.Context, cutoff string, limit int) ([]db.ItemPublishBatch, error)
	// GetCookieValueOwned 读取用户拥有账号的 Cookie。
	GetCookieValueOwned(ctx context.Context, userID int64, cookieID string) (string, error)
	// ExistsOwned 判断账号是否归属于用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
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

// RecoverableBatches 委托可恢复批次查询。
func (r storeItemPublishRepository) RecoverableBatches(ctx context.Context, now int64, limit int) ([]db.ItemPublishBatch, error) {
	return r.store.PublishBatches.Recoverable(ctx, now, limit)
}

// FinalizeExpiredCancellation 委托过期取消收口。
func (r storeItemPublishRepository) FinalizeExpiredCancellation(ctx context.Context, batchID string, now int64) (bool, error) {
	return r.store.PublishBatches.FinalizeExpiredCancellation(ctx, batchID, now)
}

// ResetInterrupted 委托中断批次重置。
func (r storeItemPublishRepository) ResetInterrupted(ctx context.Context, batchID string) error {
	return r.store.PublishBatches.ResetInterrupted(ctx, batchID)
}

// FailClaimedBatch 委托 worker 持有批次失败标记。
func (r storeItemPublishRepository) FailClaimedBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	return r.store.PublishBatches.FailClaimedBatch(ctx, batchID, workerToken)
}

// RenewBatchLease 委托批次租约续期。
func (r storeItemPublishRepository) RenewBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	return r.store.PublishBatches.RenewBatchLease(ctx, batchID, workerToken, leaseExpiresAt)
}

// ClaimRow 委托批次明细行抢占。
func (r storeItemPublishRepository) ClaimRow(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	return r.store.PublishBatches.ClaimRow(ctx, rowID, workerToken)
}

// BatchStatus 委托批次状态查询。
func (r storeItemPublishRepository) BatchStatus(ctx context.Context, batchID string) (string, error) {
	return r.store.PublishBatches.BatchStatus(ctx, batchID)
}

// MarkClaimedRowFailed 委托 worker 持有明细行失败标记。
func (r storeItemPublishRepository) MarkClaimedRowFailed(ctx context.Context, rowID int64, workerToken, message, kind string) (bool, error) {
	return r.store.PublishBatches.MarkClaimedRowFailed(ctx, rowID, workerToken, message, kind)
}

// FinalizeCanceled 委托已取消批次收口。
func (r storeItemPublishRepository) FinalizeCanceled(ctx context.Context, batchID, workerToken string) (bool, error) {
	return r.store.PublishBatches.FinalizeCanceled(ctx, batchID, workerToken)
}

// FinalizeInterrupted 委托中断批次收口。
func (r storeItemPublishRepository) FinalizeInterrupted(ctx context.Context, batchID, workerToken, message string) (string, bool, error) {
	return r.store.PublishBatches.FinalizeInterrupted(ctx, batchID, workerToken, message)
}

// MarkClaimedRemoteStarted 委托远端发布开始标记。
func (r storeItemPublishRepository) MarkClaimedRemoteStarted(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	return r.store.PublishBatches.MarkClaimedRemoteStarted(ctx, rowID, workerToken)
}

// SaveClaimedRemoteResult 委托远端发布结果保存。
func (r storeItemPublishRepository) SaveClaimedRemoteResult(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	return r.store.PublishBatches.SaveClaimedRemoteResult(ctx, rowID, workerToken, itemID, itemURL, rawJSON)
}

// MarkClaimedRowSuccess 委托明细行成功标记。
func (r storeItemPublishRepository) MarkClaimedRowSuccess(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	return r.store.PublishBatches.MarkClaimedRowSuccess(ctx, rowID, workerToken, itemID, itemURL, rawJSON)
}

// ClearUploadDir 委托批次上传目录清理。
func (r storeItemPublishRepository) ClearUploadDir(ctx context.Context, batchID string) error {
	return r.store.PublishBatches.ClearUploadDir(ctx, batchID)
}

// ExpiredUploads 委托过期上传目录查询。
func (r storeItemPublishRepository) ExpiredUploads(ctx context.Context, cutoff string, limit int) ([]db.ItemPublishBatch, error) {
	return r.store.PublishBatches.ExpiredUploads(ctx, cutoff, limit)
}

// GetCookieValueOwned 委托用户账号 Cookie 查询。
func (r storeItemPublishRepository) GetCookieValueOwned(ctx context.Context, userID int64, cookieID string) (string, error) {
	return r.store.Cookies.GetValueOwned(ctx, userID, cookieID)
}

// ExistsOwned 委托账号归属查询。
func (r storeItemPublishRepository) ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
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
