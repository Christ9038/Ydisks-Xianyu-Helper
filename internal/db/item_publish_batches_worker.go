package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Delete 删除属于指定用户的批量发布任务及其明细；调用方负责先清理对应上传目录。
func (b *ItemPublishBatches) Delete(ctx context.Context, userID int64, batchID string) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `DELETE FROM item_publish_batches WHERE id=? AND user_id=? AND status NOT IN ('running','canceling')`, batchID, userID)
	if err != nil {
		return err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// ExpiredUploads 封装ExpiredUploads业务协调。
func (b *ItemPublishBatches) ExpiredUploads(ctx context.Context, cutoff string, limit int) ([]ItemPublishBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,default_cookie_id,filename,upload_dir,status,
		total_count,success_count,failed_count,worker_token,lease_expires_at,created_at,updated_at
		FROM item_publish_batches
		WHERE upload_dir<>'' AND status NOT IN ('running','canceling') AND updated_at<?
		ORDER BY updated_at LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	var out []ItemPublishBatch
	for rows.Next() {
		// batch 用于本次流程后续判断的批次
		var batch ItemPublishBatch
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&batch.ID, &batch.UserID, &batch.DefaultCookieID, &batch.Filename, &batch.UploadDir,
			&batch.Status, &batch.TotalCount, &batch.SuccessCount, &batch.FailedCount, &batch.WorkerToken,
			&batch.LeaseExpiresAt, &batch.CreatedAt, &batch.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

// ClearUploadDir 封装ClearUploadDir业务协调。
func (b *ItemPublishBatches) ClearUploadDir(ctx context.Context, batchID string) error {
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches SET upload_dir='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, batchID)
	return err
}

// ResetInterrupted 只重置确认由进程中断造成的失败行，不自动重试业务失败或远端状态不确定的行。
func (b *ItemPublishBatches) ResetInterrupted(ctx context.Context, batchID string) error {
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='pending',error_message='',failure_kind='',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='failed' AND failure_kind='interrupted'`, batchID)
	return err
}

// ClaimBatch 原子领取一个非运行批次。并发请求中只有一个 worker 能成功。
func (b *ItemPublishBatches) ClaimBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='running',worker_token=?,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND (status IN ('preview','pending','failed','partially_failed','completed','canceled')
		 OR (status='running' AND (lease_expires_at=0 OR lease_expires_at<?)))`,
		workerToken, leaseExpiresAt, batchID, time.Now().UTC().Unix())
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	// 远端发布已经开始但尚未保存商品 ID 的行结果未知，绝不能自动重放。
	if _, err := tx.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',worker_token='',error_message='上次任务在远端发布期间中断；结果未知，请人工核对闲鱼商品列表',failure_kind='uncertain_remote',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND worker_token<>? AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID, workerToken); err != nil {
		return false, err
	}
	// 仅把确定尚未进入远端副作用，或已经保存远端商品 ID 的行重新放回队列。
	if _, err := tx.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='pending',worker_token='',error_message='上次任务租约已过期，等待重试',failure_kind='interrupted',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND worker_token<>?`, batchID, workerToken); err != nil {
		return false, err
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// RenewBatchLease 仅允许当前 worker 延长租约，长批次不会因固定截止时间被并发接管。
func (b *ItemPublishBatches) RenewBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, leaseExpiresAt, batchID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// ReservePublishSlot 仅在当前 worker 仍持有运行中批次且上一时隙已满足间隔时原子记录新的发布开始时刻。
func (b *ItemPublishBatches) ReservePublishSlot(ctx context.Context, batchID, workerToken string, minimumLastStartedAtMillis, startedAtMillis int64) (bool, error) {
	// res、err 保存原子时隙更新结果。
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET last_publish_started_at_millis=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=? AND COALESCE(last_publish_started_at_millis,0)<=?`,
		startedAtMillis, batchID, workerToken, minimumLastStartedAtMillis)
	if err != nil {
		return false, err
	}
	// n、rowsErr 保存本次时隙更新影响的行数及读取错误。
	n, rowsErr := res.RowsAffected()
	return rowsErr == nil && n == 1, rowsErr
}

// FailClaimedBatch 释放初始化阶段失败的批次租约。worker token 防止旧 worker
// 覆盖已经被新 worker 接管的状态。
// FailClaimedBatch 封装FailClaimed批次业务协调。
func (b *ItemPublishBatches) FailClaimedBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='failed',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, batchID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FinishBatchStatus 只允许当前持有租约的 worker 结束批次。
func (b *ItemPublishBatches) FinishBatchStatus(ctx context.Context, batchID, workerToken, status string) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET status=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, status, batchID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FinalizeBatch 在单个事务内重算计数，并且只在所有行都进入终态后结束批次。
func (b *ItemPublishBatches) FinalizeBatch(ctx context.Context, batchID, workerToken string) (string, bool, error) {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	// total、success、failed、unfinished 用于本次流程后续判断的total、success、failed、unfinished
	var total, success, failed, unfinished int
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status NOT IN ('success','failed') THEN 1 ELSE 0 END),0)
		FROM item_publish_batch_rows WHERE batch_id=?`, batchID).
		Scan(&total, &success, &failed, &unfinished); err != nil {
		return "", false, err
	}
	if unfinished != 0 || total != success+failed {
		return "", false, errors.New("批次仍有未完成行，不能进入终态")
	}
	// status 用于本次流程后续判断的状态
	status := finalBatchStatus(success, failed)
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status=?,total_count=?,success_count=?,failed_count=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, status, total, success, failed, batchID, workerToken)
	if err != nil {
		return "", false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return "", false, err
	}
	return status, true, nil
}

// BatchStatus 取批次状态。未找到返回 ErrNotFound。
func (b *ItemPublishBatches) BatchStatus(ctx context.Context, batchID string) (string, error) {
	// status 用于本次流程后续判断的状态
	var status string
	// err 用于本次流程后续判断的err
	err := b.DB.QueryRowContext(ctx, `SELECT status FROM item_publish_batches WHERE id=?`, batchID).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return status, nil
}

// MarkRowRunning 将明细行置为 running 并清空历史错误信息（发布 worker 开始处理时调用）。
func (b *ItemPublishBatches) MarkRowRunning(ctx context.Context, rowID int64) error {
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='running',error_message='',failure_kind='',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'`, rowID)
	return err
}

// ClaimRow 原子领取单行，防止多个 worker 重复发布同一商品。
func (b *ItemPublishBatches) ClaimRow(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='running',worker_token=?,error_message='',failure_kind='',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='pending'
		  AND EXISTS (SELECT 1 FROM item_publish_batches b
		              WHERE b.id=item_publish_batch_rows.batch_id AND b.status='running' AND b.worker_token=?)`,
		workerToken, rowID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkClaimedRemoteStarted 在调用闲鱼发布接口前落盘。进程若在远端返回前硬退出，
// 租约接管方会把该行隔离为 uncertain_remote，而不是再次发布。
// MarkClaimedRemoteStarted 封装MarkClaimedRemoteStarted业务协调。
func (b *ItemPublishBatches) MarkClaimedRemoteStarted(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET failure_kind='remote_started',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=? AND COALESCE(item_id,'')=''`, rowID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkClaimedRowSuccess 只允许领取该行的 worker 写入成功结果。
func (b *ItemPublishBatches) MarkClaimedRowSuccess(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	if rawJSON == "" {
		rawJSON = "{}"
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='success',item_id=?,item_url=?,error_message='',failure_kind='',worker_token='',raw_json=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, itemID, itemURL, rawJSON, rowID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// SaveClaimedRemoteResult 在闲鱼发布成功后第一时间保存远端商品标识。
// 后续本地商品/自动化规则写入失败时，重试会从该断点继续而不会再次调用发布接口。
// SaveClaimedRemoteResult 保存ClaimedRemote结果。
func (b *ItemPublishBatches) SaveClaimedRemoteResult(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	if strings.TrimSpace(itemID) == "" {
		return false, errors.New("远端发布结果缺少商品ID")
	}
	if rawJSON == "" {
		rawJSON = "{}"
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET item_id=?,item_url=?,raw_json=?,failure_kind='post_publish',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, itemID, itemURL, rawJSON, rowID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkClaimedRowFailed 只允许领取该行的 worker 写入失败结果。
func (b *ItemPublishBatches) MarkClaimedRowFailed(ctx context.Context, rowID int64, workerToken, message, kind string) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message=?,failure_kind=?,worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, message, kind, rowID, workerToken)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkRowSuccess 标记明细行发布成功，回填闲鱼商品 ID、URL 与原始返回 JSON。
func (b *ItemPublishBatches) MarkRowSuccess(ctx context.Context, rowID int64, itemID, itemURL, rawJSON string) error {
	if rawJSON == "" {
		rawJSON = "{}"
	}
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='success',item_id=?,item_url=?,error_message='',failure_kind='',worker_token='',raw_json=?,updated_at=CURRENT_TIMESTAMP
		  WHERE id=?`, itemID, itemURL, rawJSON, rowID)
	return err
}

// MarkRowFailed 标记明细行发布失败并记录错误原因。
func (b *ItemPublishBatches) MarkRowFailed(ctx context.Context, rowID int64, message string) error {
	return b.MarkRowFailedKind(ctx, rowID, message, "publish")
}

// MarkRowFailedKind 封装MarkRow失败类型业务协调。
func (b *ItemPublishBatches) MarkRowFailedKind(ctx context.Context, rowID int64, message, kind string) error {
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='failed',error_message=?,failure_kind=?,worker_token='',updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		message, kind, rowID)
	return err
}

// MarkRunningFailed 将批次内仍在 running 的行标为失败。
func (b *ItemPublishBatches) MarkRunningFailed(ctx context.Context, batchID, message string) error {
	if // err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message='远端发布结果未知，请人工核对闲鱼商品列表',failure_kind='uncertain_remote',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='failed',error_message=?,failure_kind='interrupted',worker_token='',updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status='running'`,
		message, batchID)
	return err
}

// MarkUnfinishedFailed 将批次内 pending/running 行标为失败。
func (b *ItemPublishBatches) MarkUnfinishedFailed(ctx context.Context, batchID, message string) error {
	if // err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message='远端发布结果未知，请人工核对闲鱼商品列表',failure_kind='uncertain_remote',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='failed',error_message=?,failure_kind='interrupted',worker_token='',updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status IN ('pending','running')`,
		message, batchID)
	return err
}

// ResetFailed 将批次内所有 failed 行重置为 pending，便于失败重试。
func (b *ItemPublishBatches) ResetFailed(ctx context.Context, batchID string) error {
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='pending',error_message='',failure_kind='',worker_token='',updated_at=CURRENT_TIMESTAMP
			  WHERE batch_id=? AND status='failed'
			    AND COALESCE(failure_kind,'') NOT IN ('validation','uncertain_remote')`, batchID)
	return err
}

// Recount 按明细行实际状态重算批次的 total/success/failed 计数。
// worker 每完成一行后调用，保证前端进度与 DB 一致。
// Recount 封装Recount业务协调。
func (b *ItemPublishBatches) Recount(ctx context.Context, batchID string) error {
	// total、success、failed 用于本次流程后续判断的total、success、failed
	var total, success, failed int
	if // err 用于本次流程后续判断的err
	err := b.DB.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		   FROM item_publish_batch_rows WHERE batch_id=?`, batchID).Scan(&total, &success, &failed); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batches
		    SET total_count=?,success_count=?,failed_count=?,updated_at=CURRENT_TIMESTAMP
		  WHERE id=?`, total, success, failed, batchID)
	return err
}
