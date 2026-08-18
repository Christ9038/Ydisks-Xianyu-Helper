package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// ItemBatchRepository 将商品批量发布数据库仓储适配为应用层 worker Port。
type ItemBatchRepository struct {
	// store 保存数据库聚合入口；批量 worker 不会接触该字段。
	store *db.Store
}

// NewItemBatchRepository 构造商品批量发布 worker 的数据库适配器。
func NewItemBatchRepository(store *db.Store) *ItemBatchRepository {
	return &ItemBatchRepository{store: store}
}

// CreateBatch 将应用预检模型转换为数据库批次并在单事务内写入。
func (r *ItemBatchRepository) CreateBatch(ctx context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) error {
	// err 表示批次仓储依赖校验错误。
	if err := r.validate(); err != nil {
		return err
	}
	// locationJSON 和 err 分别保存发货地 JSON 与序列化错误。
	locationJSON, err := json.Marshal(batch.Location)
	if err != nil {
		return err
	}
	// status 保存未显式指定时兼容预检批次的默认状态。
	status := batch.Status
	if strings.TrimSpace(status) == "" {
		status = "preview"
	}
	// databaseRows 保存转换后的数据库明细行。
	databaseRows := make([]db.ItemPublishBatchRow, 0, len(rows))
	// row 表示当前待转换的应用预检行。
	for _, row := range rows {
		// imagesJSON 和 imagesErr 分别保存图片字段 JSON 与序列化错误。
		imagesJSON, imagesErr := json.Marshal(row.Images)
		if imagesErr != nil {
			return imagesErr
		}
		// categoryJSON 和 categoryErr 分别保存类目字段 JSON 与序列化错误。
		categoryJSON, categoryErr := json.Marshal(row.Category)
		if categoryErr != nil {
			return categoryErr
		}
		// automationJSON 和 automationErr 分别保存自动化字段 JSON 与序列化错误。
		automationJSON, automationErr := json.Marshal(row.Automation)
		if automationErr != nil {
			return automationErr
		}
		// rawJSON 和 rawErr 分别保存原始行 JSON 与序列化错误。
		rawJSON, rawErr := json.Marshal(row.Raw)
		if rawErr != nil {
			return rawErr
		}
		// status、errorMessage 和 failureKind 保存数据库状态、展示错误与失败分类。
		status, errorMessage, failureKind := "pending", "", ""
		if len(row.Errors) > 0 {
			status, errorMessage, failureKind = "failed", strings.Join(row.Errors, "；"), "validation"
		}
		databaseRows = append(databaseRows, db.ItemPublishBatchRow{
			RowNo: row.RowNo, CookieID: row.CookieID, Title: row.Title, Description: row.Description,
			Price: row.Price, OriginalPrice: row.OriginalPrice, Quantity: row.Quantity,
			PostageMode: row.PostageMode, Postage: row.Postage, ImagesJSON: string(imagesJSON),
			CategoryJSON: string(categoryJSON), AutomationJSON: string(automationJSON), Status: status,
			ErrorMessage: errorMessage, FailureKind: failureKind, RawJSON: string(rawJSON),
		})
	}
	return r.store.PublishBatches.Create(ctx, &db.ItemPublishBatch{ID: batch.ID, UserID: batch.UserID, DefaultCookieID: batch.DefaultCookieID, Filename: batch.Filename, UploadDir: batch.UploadDir, LocationJSON: string(locationJSON), PublishIntervalSeconds: batch.PublishIntervalSeconds, Status: status}, databaseRows)
}

// PendingRows 查询批次待处理明细并转换为应用模型。
func (r *ItemBatchRepository) PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]itemapp.BatchRow, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// rows 保存数据库仓储返回的批量明细。
	rows, err := r.store.PublishBatches.PendingRows(ctx, batchID, failedOnly)
	if err != nil {
		return nil, err
	}
	// result 保存不暴露数据库类型的应用批量明细。
	result := make([]itemapp.BatchRow, 0, len(rows))
	// row 表示当前待转换的数据库批次明细。
	for _, row := range rows {
		result = append(result, batchRowApplicationModel(row))
	}
	return result, nil
}

// RenewBatchLease 委托数据库延长当前 worker 的批次租约。
func (r *ItemBatchRepository) RenewBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.RenewBatchLease(ctx, batchID, workerToken, leaseExpiresAt)
}

// ReservePublishSlot 原子预留一次符合批次间隔的最终商品发布时刻。
func (r *ItemBatchRepository) ReservePublishSlot(ctx context.Context, batchID, workerToken string, minimumLastStartedAtMillis, startedAtMillis int64) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.ReservePublishSlot(ctx, batchID, workerToken, minimumLastStartedAtMillis, startedAtMillis)
}

// GetBatch 查询批次并转换为应用状态模型。
func (r *ItemBatchRepository) GetBatch(ctx context.Context, userID int64, batchID string) (itemapp.BatchInfo, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return itemapp.BatchInfo{}, err
	}
	// batch 保存按用户隔离后的数据库批次。
	batch, err := r.store.PublishBatches.Get(ctx, userID, batchID)
	if err != nil {
		return itemapp.BatchInfo{}, err
	}
	return batchInfoApplicationModel(*batch), nil
}

// ListBatchesForUser 查询用户批次摘要并转换为应用模型。
func (r *ItemBatchRepository) ListBatchesForUser(ctx context.Context, userID int64, limit int) ([]itemapp.BatchInfo, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// batches 保存数据库返回的用户批次。
	batches, err := r.store.PublishBatches.ListForUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	// result 保存不暴露数据库模型的应用批次摘要。
	result := make([]itemapp.BatchInfo, 0, len(batches))
	// batch 表示当前待转换的数据库批次。
	for _, batch := range batches {
		result = append(result, batchInfoApplicationModel(batch))
	}
	return result, nil
}

// ListBatchRows 查询批次全部明细并转换为应用模型。
func (r *ItemBatchRepository) ListBatchRows(ctx context.Context, batchID string) ([]itemapp.BatchRow, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// rows 保存数据库返回的批次明细。
	rows, err := r.store.PublishBatches.Rows(ctx, batchID)
	if err != nil {
		return nil, err
	}
	// result 保存不暴露数据库模型的应用批次明细。
	result := make([]itemapp.BatchRow, 0, len(rows))
	// row 表示当前待转换的数据库明细。
	for _, row := range rows {
		result = append(result, batchRowApplicationModel(row))
	}
	return result, nil
}

// ExpiredUploadBatches 查询已超过上传目录保留期限的批次并转换为应用模型。
func (r *ItemBatchRepository) ExpiredUploadBatches(ctx context.Context, cutoff string, limit int) ([]itemapp.BatchInfo, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// batches 保存数据库返回的过期上传批次。
	batches, err := r.store.PublishBatches.ExpiredUploads(ctx, cutoff, limit)
	if err != nil {
		return nil, err
	}
	// result 保存不暴露数据库模型的应用批次状态。
	result := make([]itemapp.BatchInfo, 0, len(batches))
	// batch 表示当前待转换的过期批次。
	for _, batch := range batches {
		result = append(result, batchInfoApplicationModel(batch))
	}
	return result, nil
}

// RequestCancel 请求批次进入取消状态。
func (r *ItemBatchRepository) RequestCancel(ctx context.Context, batchID string) (string, bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return "", false, err
	}
	// token、running、requestErr 保存数据库取消请求的结果。
	token, running, requestErr := r.store.PublishBatches.RequestCancel(ctx, batchID)
	if errors.Is(requestErr, db.ErrPublishBatchChanged) {
		return "", false, itemapp.ErrBatchConflict
	}
	return token, running, requestErr
}

// DeleteBatch 删除用户拥有的批次及其受控上传目录。
func (r *ItemBatchRepository) DeleteBatch(ctx context.Context, userID int64, batchID string) error {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return err
	}
	// batch 保存删除前的上传目录，路径只在适配器内使用。
	batch, err := r.store.PublishBatches.Get(ctx, userID, batchID)
	if err != nil {
		return err
	}
	// err 表示数据库批次及其上传目录删除错误。
	if err := r.store.PublishBatches.Delete(ctx, userID, batchID); err != nil {
		return err
	}
	if batch != nil && batch.UploadDir != "" {
		return os.RemoveAll(batch.UploadDir)
	}
	return nil
}

// ResetFailed 重置批次中的可重试失败明细。
func (r *ItemBatchRepository) ResetFailed(ctx context.Context, batchID string) error {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.PublishBatches.ResetFailed(ctx, batchID)
}

// FailClaimedBatch 释放当前 worker 持有的批次租约。
func (r *ItemBatchRepository) FailClaimedBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.FailClaimedBatch(ctx, batchID, workerToken)
}

// RecoverableBatches 查询可恢复批次并转换为应用层状态模型。
func (r *ItemBatchRepository) RecoverableBatches(ctx context.Context, now int64, limit int) ([]itemapp.BatchInfo, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// batches 保存数据库返回的可恢复批次。
	batches, err := r.store.PublishBatches.Recoverable(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	// result 保存不暴露数据库类型的应用批次状态。
	result := make([]itemapp.BatchInfo, 0, len(batches))
	// batch 表示当前待转换的数据库批次。
	for _, batch := range batches {
		result = append(result, batchInfoApplicationModel(batch))
	}
	return result, nil
}

// FinalizeExpiredCancellation 收口已经超过租约截止时间的取消请求。
func (r *ItemBatchRepository) FinalizeExpiredCancellation(ctx context.Context, batchID string, now int64) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.FinalizeExpiredCancellation(ctx, batchID, now)
}

// ClaimBatch 抢占批次恢复租约并返回是否成功。
func (r *ItemBatchRepository) ClaimBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.ClaimBatch(ctx, batchID, workerToken, leaseExpiresAt)
}

// ResetInterrupted 重置可确认由进程中断造成的失败明细。
func (r *ItemBatchRepository) ResetInterrupted(ctx context.Context, batchID string) error {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.PublishBatches.ResetInterrupted(ctx, batchID)
}

// ClaimRow 抢占单条批次明细的 worker 租约。
func (r *ItemBatchRepository) ClaimRow(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.ClaimRow(ctx, rowID, workerToken)
}

// BatchStatus 读取批次状态供应用层决定失败分类。
func (r *ItemBatchRepository) BatchStatus(ctx context.Context, batchID string) (string, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.store.PublishBatches.BatchStatus(ctx, batchID)
}

// MarkClaimedRowFailed 保存当前 worker 持有明细的失败结果。
func (r *ItemBatchRepository) MarkClaimedRowFailed(ctx context.Context, rowID int64, workerToken, message, kind string) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.MarkClaimedRowFailed(ctx, rowID, workerToken, message, kind)
}

// MarkClaimedRowSuccess 保存当前 worker 持有明细的远端成功结果。
func (r *ItemBatchRepository) MarkClaimedRowSuccess(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.MarkClaimedRowSuccess(ctx, rowID, workerToken, itemID, itemURL, rawJSON)
}

// RecountBatch 重算批次的成功与失败统计。
func (r *ItemBatchRepository) RecountBatch(ctx context.Context, batchID string) error {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.PublishBatches.Recount(ctx, batchID)
}

// FinalizeBatch 尝试收口当前 worker 完成的批次。
func (r *ItemBatchRepository) FinalizeBatch(ctx context.Context, batchID, workerToken string) (string, bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return "", false, err
	}
	return r.store.PublishBatches.FinalizeBatch(ctx, batchID, workerToken)
}

// FinalizeCanceled 收口当前 worker 持有的取消批次。
func (r *ItemBatchRepository) FinalizeCanceled(ctx context.Context, batchID, workerToken string) (bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.PublishBatches.FinalizeCanceled(ctx, batchID, workerToken)
}

// FinalizeInterrupted 收口被取消或租约中断的批次。
func (r *ItemBatchRepository) FinalizeInterrupted(ctx context.Context, batchID, workerToken, message string) (string, bool, error) {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return "", false, err
	}
	return r.store.PublishBatches.FinalizeInterrupted(ctx, batchID, workerToken, message)
}

// DeleteUpload 删除批次上传目录并清除数据库中的目录记录。
func (r *ItemBatchRepository) DeleteUpload(ctx context.Context, batchID, uploadDir string) error {
	// err 表示适配器依赖校验错误。
	if err := r.validate(); err != nil {
		return err
	}
	if uploadDir == "" {
		return nil
	}
	// removeErr 保存上传目录删除结果；即使删除失败也继续清理数据库记录。
	removeErr := os.RemoveAll(uploadDir)
	// clearErr 保存数据库上传目录字段清理结果。
	clearErr := r.store.PublishBatches.ClearUploadDir(ctx, batchID)
	if removeErr != nil {
		return removeErr
	}
	return clearErr
}

// validate 检查批量发布数据库适配器的必需依赖。
func (r *ItemBatchRepository) validate() error {
	if r == nil || r.store == nil || r.store.PublishBatches == nil {
		return errors.New("商品批量发布数据库适配器未初始化")
	}
	return nil
}

// batchRowApplicationModel 将数据库明细完整转换为应用层批量明细。
func batchRowApplicationModel(row db.ItemPublishBatchRow) itemapp.BatchRow {
	return itemapp.BatchRow{
		ID: row.ID, BatchID: row.BatchID, RowNo: row.RowNo, CookieID: row.CookieID,
		Title: row.Title, Description: row.Description, Price: row.Price, OriginalPrice: row.OriginalPrice,
		Quantity: row.Quantity, PostageMode: row.PostageMode, Postage: row.Postage,
		ImagesJSON: row.ImagesJSON, CategoryJSON: row.CategoryJSON, AutomationJSON: row.AutomationJSON,
		RawJSON: row.RawJSON, ItemID: row.ItemID, ItemURL: row.ItemURL, Status: row.Status,
		ErrorMessage: row.ErrorMessage, FailureKind: row.FailureKind, WorkerToken: row.WorkerToken,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// batchInfoApplicationModel 将数据库批次完整转换为应用层批次状态。
func batchInfoApplicationModel(batch db.ItemPublishBatch) itemapp.BatchInfo {
	return itemapp.BatchInfo{
		ID: batch.ID, UserID: batch.UserID, Status: batch.Status, WorkerToken: batch.WorkerToken,
		DefaultCookieID: batch.DefaultCookieID, Filename: batch.Filename, UploadDir: batch.UploadDir,
		LocationJSON: batch.LocationJSON, PublishIntervalSeconds: batch.PublishIntervalSeconds, LastPublishStartedAtMillis: batch.LastPublishStartedAtMillis,
		TotalCount: batch.TotalCount, SuccessCount: batch.SuccessCount,
		FailedCount: batch.FailedCount, LeaseExpiresAt: batch.LeaseExpiresAt,
		CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt,
	}
}

// 确保适配器覆盖批量发布 worker 的全部持久化端口。
var _ itemapp.BatchRepository = (*ItemBatchRepository)(nil)

// 确保适配器覆盖批次管理应用服务的全部持久化端口。
var _ itemapp.BatchManagementRepository = (*ItemBatchRepository)(nil)

// 确保适配器实现批量恢复扫描所需的状态端口。
var _ itemapp.BatchRecoveryRepository = (*ItemBatchRepository)(nil)
