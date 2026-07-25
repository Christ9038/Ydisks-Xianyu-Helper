package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ItemPublishBatches 管理商品批量发布批次及其明细行的持久化。
//
// 一个批次对应一次 Excel/CSV 导入，包含若干明细行（每行一个待发布商品）。
// 发布 worker 按 row_no 顺序逐行发布，通过状态机（pending→running→success/failed）
// 跟踪进度，支持失败重置（ResetFailed）和实时计数（Recount）。
type ItemPublishBatches struct{ DB *sql.DB }

// ErrPublishBatchChanged 表示批次在读取与状态切换之间被其他 worker 更新，调用方可安全重试。
var ErrPublishBatchChanged = errors.New("批量发布任务状态已变化")

// ItemPublishBatch 是一个发布批次的元信息（不含明细行）。
type ItemPublishBatch struct {
	ID              string // 批次 ID（上传时生成，UUID 形式）
	UserID          int64  // 所属用户
	DefaultCookieID string // 默认发布账号（明细行未指定账号时回退到此）
	Filename        string // 原始上传文件名
	UploadDir       string // 图片资源目录（发布时读取商品图片的根目录）
	Status          string // 批次状态：pending/running/completed/partially_failed/failed
	TotalCount      int    // 明细行总数（Recount 维护）
	SuccessCount    int    // 成功数（Recount 维护）
	FailedCount     int    // 失败数（Recount 维护）
	WorkerToken     string
	LeaseExpiresAt  int64
	CreatedAt       string
	UpdatedAt       string
}

// ItemPublishBatchRow 是一条待发布的商品明细。
type ItemPublishBatchRow struct {
	ID             int64  // 自增主键，worker 按此标记状态
	BatchID        string // 所属批次 ID
	RowNo          int    // 批次内序号（1 起，按导入顺序）
	CookieID       string // 发布到哪个账号
	Title          string
	Description    string
	Price          string
	OriginalPrice  string
	Quantity       int
	PostageMode    string // 邮费模式：free/buyer/seller
	Postage        string
	ImagesJSON     string // 图片引用 JSON 数组（相对 UploadDir 的路径）
	CategoryJSON   string // 自动识别失败时使用的指定类目 JSON
	AutomationJSON string // 发布后自动创建的自动化规则配置 JSON
	Status         string // pending/running/success/failed
	ItemID         string // 发布成功后回填的闲鱼商品 ID
	ItemURL        string // 发布成功后回填的商品 URL
	ErrorMessage   string // 失败原因
	FailureKind    string // validation/publish/interrupted
	WorkerToken    string
	RawJSON        string // 发布接口原始返回 JSON
	CreatedAt      string
	UpdatedAt      string
}

// Create 在单事务内创建批次及其全部明细行。
// 明细行的 quantity/postage_mode/status/images_json/raw_json/automation_json 缺省值在此补齐。
// total_count 取 len(rows)，success/failed 初始为 0。
func (b *ItemPublishBatches) Create(ctx context.Context, batch *ItemPublishBatch, rows []ItemPublishBatchRow) error {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO item_publish_batches
		 (id,user_id,default_cookie_id,filename,upload_dir,status,total_count,success_count,failed_count)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		batch.ID, batch.UserID, batch.DefaultCookieID, batch.Filename, batch.UploadDir,
		batch.Status, len(rows), 0, 0); err != nil {
		return err
	}
	for _, row := range rows {
		if row.Quantity <= 0 {
			row.Quantity = 1
		}
		if row.PostageMode == "" {
			row.PostageMode = "free"
		}
		if row.Status == "" {
			row.Status = "pending"
		}
		if row.ImagesJSON == "" {
			row.ImagesJSON = "[]"
		}
		if row.RawJSON == "" {
			row.RawJSON = "{}"
		}
		if row.CategoryJSON == "" {
			row.CategoryJSON = "{}"
		}
		if row.AutomationJSON == "" {
			row.AutomationJSON = "{}"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_publish_batch_rows
			 (batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
			  images_json,category_json,automation_json,status,item_id,item_url,error_message,failure_kind,raw_json)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			batch.ID, row.RowNo, row.CookieID, row.Title, row.Description, row.Price, row.OriginalPrice,
			row.Quantity, row.PostageMode, row.Postage, row.ImagesJSON, row.CategoryJSON, row.AutomationJSON,
			row.Status, row.ItemID, row.ItemURL, row.ErrorMessage, row.FailureKind, row.RawJSON); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Get 按 ID 取批次（含 user_id 隔离校验）。未找到返回 ErrNotFound。
func (b *ItemPublishBatches) Get(ctx context.Context, userID int64, id string) (*ItemPublishBatch, error) {
	var out ItemPublishBatch
	err := b.DB.QueryRowContext(ctx,
		`SELECT id,user_id,default_cookie_id,filename,upload_dir,status,total_count,success_count,failed_count,
		        COALESCE(worker_token,''),COALESCE(lease_expires_at,0),
		        created_at,updated_at
		   FROM item_publish_batches WHERE id=? AND user_id=?`, id, userID).Scan(
		&out.ID, &out.UserID, &out.DefaultCookieID, &out.Filename, &out.UploadDir, &out.Status,
		&out.TotalCount, &out.SuccessCount, &out.FailedCount, &out.WorkerToken, &out.LeaseExpiresAt,
		&out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ListForUser 返回用户最近的批量任务，供页面重载后重新发现运行记录。
func (b *ItemPublishBatches) ListForUser(ctx context.Context, userID int64, limit int) ([]ItemPublishBatch, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,default_cookie_id,filename,upload_dir,status,
		total_count,success_count,failed_count,COALESCE(worker_token,''),COALESCE(lease_expires_at,0),created_at,updated_at
		FROM item_publish_batches WHERE user_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemPublishBatch
	for rows.Next() {
		var batch ItemPublishBatch
		if err := rows.Scan(&batch.ID, &batch.UserID, &batch.DefaultCookieID, &batch.Filename, &batch.UploadDir,
			&batch.Status, &batch.TotalCount, &batch.SuccessCount, &batch.FailedCount, &batch.WorkerToken,
			&batch.LeaseExpiresAt, &batch.CreatedAt, &batch.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

// Recoverable 返回租约过期或因进程退出中断、可安全自动续跑的任务。
func (b *ItemPublishBatches) Recoverable(ctx context.Context, now int64, limit int) ([]ItemPublishBatch, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,default_cookie_id,filename,upload_dir,status,
		total_count,success_count,failed_count,COALESCE(worker_token,''),COALESCE(lease_expires_at,0),created_at,updated_at
		FROM item_publish_batches b
		WHERE (b.status IN ('running','canceling') AND (b.lease_expires_at=0 OR b.lease_expires_at<?))
		   OR (b.status IN ('failed','partially_failed') AND EXISTS (
		       SELECT 1 FROM item_publish_batch_rows r WHERE r.batch_id=b.id AND r.status='failed' AND r.failure_kind='interrupted'))
		ORDER BY b.updated_at,b.id LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemPublishBatch
	for rows.Next() {
		var batch ItemPublishBatch
		if err := rows.Scan(&batch.ID, &batch.UserID, &batch.DefaultCookieID, &batch.Filename, &batch.UploadDir,
			&batch.Status, &batch.TotalCount, &batch.SuccessCount, &batch.FailedCount, &batch.WorkerToken,
			&batch.LeaseExpiresAt, &batch.CreatedAt, &batch.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

// Rows 取批次全部明细行，按 row_no 升序。
func (b *ItemPublishBatches) Rows(ctx context.Context, batchID string) ([]ItemPublishBatchRow, error) {
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id,batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
		        images_json,COALESCE(category_json,'{}'),COALESCE(automation_json,'{}'),status,item_id,item_url,error_message,
		        COALESCE(failure_kind,''),COALESCE(worker_token,''),
		        raw_json,created_at,updated_at
		   FROM item_publish_batch_rows WHERE batch_id=? ORDER BY row_no`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemPublishBatchRow{}
	for rows.Next() {
		var r ItemPublishBatchRow
		if err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.Title, &r.Description, &r.Price,
			&r.OriginalPrice, &r.Quantity, &r.PostageMode, &r.Postage, &r.ImagesJSON, &r.CategoryJSON, &r.AutomationJSON,
			&r.Status, &r.ItemID, &r.ItemURL, &r.ErrorMessage, &r.FailureKind, &r.WorkerToken,
			&r.RawJSON, &r.CreatedAt,
			&r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingRows 取待处理明细行。failedOnly=true 只取失败行（用于重试），否则取 pending 行。
func (b *ItemPublishBatches) PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]ItemPublishBatchRow, error) {
	statuses := "('pending')"
	if failedOnly {
		statuses = "('failed')"
	}
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id,batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
		        images_json,COALESCE(category_json,'{}'),COALESCE(automation_json,'{}'),status,item_id,item_url,error_message,
		        COALESCE(failure_kind,''),COALESCE(worker_token,''),
		        raw_json,created_at,updated_at
		   FROM item_publish_batch_rows WHERE batch_id=? AND status IN `+statuses+` ORDER BY row_no`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemPublishBatchRow{}
	for rows.Next() {
		var r ItemPublishBatchRow
		if err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.Title, &r.Description, &r.Price,
			&r.OriginalPrice, &r.Quantity, &r.PostageMode, &r.Postage, &r.ImagesJSON, &r.CategoryJSON, &r.AutomationJSON,
			&r.Status, &r.ItemID, &r.ItemURL, &r.ErrorMessage, &r.FailureKind, &r.WorkerToken,
			&r.RawJSON, &r.CreatedAt,
			&r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetBatchStatus 更新批次状态（如 running/completed/failed）。
func (b *ItemPublishBatches) SetBatchStatus(ctx context.Context, batchID, status string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batches SET status=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, batchID)
	return err
}

// RequestCancel 进入两阶段取消。运行中的批次保留 worker token，允许 worker
// 把已经获得的远端商品 ID 落库后再结束；未运行批次可直接取消。
func (b *ItemPublishBatches) RequestCancel(ctx context.Context, batchID string) (string, bool, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var status, token string
	if err := tx.QueryRowContext(ctx, `SELECT status,worker_token FROM item_publish_batches WHERE id=?`, batchID).Scan(&status, &token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, err
	}
	running := (status == "running" || status == "canceling") && token != ""
	if running {
		res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches SET status='canceling',updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status=? AND worker_token=?`, batchID, status, token)
		if err != nil {
			return "", false, err
		}
		if n, rowsErr := res.RowsAffected(); rowsErr != nil {
			return "", false, rowsErr
		} else if n != 1 {
			return "", false, ErrPublishBatchChanged
		}
	} else {
		// 先用读取到的状态和 token 把批次切到事务内不可领取的状态；CAS 失败时绝不触碰明细行。
		res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
			SET status='finalizing_cancel',updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status=? AND worker_token=?`, batchID, status, token)
		if err != nil {
			return "", false, err
		}
		if n, rowsErr := res.RowsAffected(); rowsErr != nil {
			return "", false, rowsErr
		} else if n != 1 {
			return "", false, ErrPublishBatchChanged
		}
		if err := markUnfinishedFailedTx(ctx, tx, batchID, "任务已取消"); err != nil {
			return "", false, err
		}
		if err := recountBatchTx(ctx, tx, batchID); err != nil {
			return "", false, err
		}
		res, err = tx.ExecContext(ctx, `UPDATE item_publish_batches SET status='canceled',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status='finalizing_cancel' AND worker_token=?`, batchID, token)
		if err != nil {
			return "", false, err
		}
		if n, rowsErr := res.RowsAffected(); rowsErr != nil {
			return "", false, rowsErr
		} else if n != 1 {
			return "", false, ErrPublishBatchChanged
		}
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return token, running, nil
}

func (b *ItemPublishBatches) FinalizeCanceled(ctx context.Context, batchID, workerToken string) (bool, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// 必须先取得当前 worker 的所有权并阻止租约接管，再修改任何明细行。
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='finalizing_cancel',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='canceling' AND worker_token=?`, batchID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if err := markUnfinishedFailedTx(ctx, tx, batchID, "任务已取消"); err != nil {
		return false, err
	}
	if err := recountBatchTx(ctx, tx, batchID); err != nil {
		return false, err
	}
	res, err = tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='canceled',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='finalizing_cancel' AND worker_token=?`, batchID, workerToken)
	if err != nil {
		return false, err
	}
	n, err = res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	return true, tx.Commit()
}

// FinalizeInterrupted 只允许当前 worker 在一个事务内把未完成行标记为中断并结束批次。
// 先切换到事务内的 finalizing_interrupted 状态，避免 token 检查与明细更新之间发生租约接管。
func (b *ItemPublishBatches) FinalizeInterrupted(ctx context.Context, batchID, workerToken, message string) (string, bool, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='finalizing_interrupted',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, batchID, workerToken)
	if err != nil {
		return "", false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	if err := markUnfinishedFailedTx(ctx, tx, batchID, message); err != nil {
		return "", false, err
	}
	var total, success, failed, unfinished int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status NOT IN ('success','failed') THEN 1 ELSE 0 END),0)
		FROM item_publish_batch_rows WHERE batch_id=?`, batchID).Scan(&total, &success, &failed, &unfinished); err != nil {
		return "", false, err
	}
	if unfinished != 0 || total != success+failed {
		return "", false, errors.New("中断批次仍有未完成行")
	}
	status := finalBatchStatus(success, failed)
	res, err = tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status=?,total_count=?,success_count=?,failed_count=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='finalizing_interrupted' AND worker_token=?`, status, total, success, failed, batchID, workerToken)
	if err != nil {
		return "", false, err
	}
	n, err = res.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return status, true, nil
}

func finalBatchStatus(success, failed int) string {
	if failed == 0 {
		return "completed"
	}
	if success > 0 {
		return "partially_failed"
	}
	return "failed"
}

// FinalizeExpiredCancellation 接管租约已过期的两阶段取消任务。远端调用已经开始但
// 结果尚未落库的行必须保持为 uncertain_remote，其余未完成行统一标记为已取消。
func (b *ItemPublishBatches) FinalizeExpiredCancellation(ctx context.Context, batchID string, now int64) (bool, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='canceled',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='canceling' AND (lease_expires_at=0 OR lease_expires_at<?)`, batchID, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if err := markUnfinishedFailedTx(ctx, tx, batchID, "任务已取消"); err != nil {
		return false, err
	}
	if err := recountBatchTx(ctx, tx, batchID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func markUnfinishedFailedTx(ctx context.Context, tx *sql.Tx, batchID, message string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message='任务取消时远端发布结果未知，请人工核对闲鱼商品列表',
		    failure_kind='uncertain_remote',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message=?,failure_kind='interrupted',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status IN ('pending','running')`, message, batchID)
	return err
}

func recountBatchTx(ctx context.Context, tx *sql.Tx, batchID string) error {
	var total, success, failed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		FROM item_publish_batch_rows WHERE batch_id=?`, batchID).Scan(&total, &success, &failed); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET total_count=?,success_count=?,failed_count=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		total, success, failed, batchID)
	return err
}

func (b *ItemPublishBatches) Delete(ctx context.Context, userID int64, batchID string) error {
	res, err := b.DB.ExecContext(ctx, `DELETE FROM item_publish_batches WHERE id=? AND user_id=? AND status NOT IN ('running','canceling')`, batchID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func (b *ItemPublishBatches) ExpiredUploads(ctx context.Context, cutoff string, limit int) ([]ItemPublishBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,default_cookie_id,filename,upload_dir,status,
		total_count,success_count,failed_count,worker_token,lease_expires_at,created_at,updated_at
		FROM item_publish_batches
		WHERE upload_dir<>'' AND status NOT IN ('running','canceling') AND updated_at<?
		ORDER BY updated_at LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemPublishBatch
	for rows.Next() {
		var batch ItemPublishBatch
		if err := rows.Scan(&batch.ID, &batch.UserID, &batch.DefaultCookieID, &batch.Filename, &batch.UploadDir,
			&batch.Status, &batch.TotalCount, &batch.SuccessCount, &batch.FailedCount, &batch.WorkerToken,
			&batch.LeaseExpiresAt, &batch.CreatedAt, &batch.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

func (b *ItemPublishBatches) ClearUploadDir(ctx context.Context, batchID string) error {
	_, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches SET upload_dir='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, batchID)
	return err
}

// ResetInterrupted 只重置确认由进程中断造成的失败行，不自动重试业务失败或远端状态不确定的行。
func (b *ItemPublishBatches) ResetInterrupted(ctx context.Context, batchID string) error {
	_, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='pending',error_message='',failure_kind='',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='failed' AND failure_kind='interrupted'`, batchID)
	return err
}

// ClaimBatch 原子领取一个非运行批次。并发请求中只有一个 worker 能成功。
func (b *ItemPublishBatches) ClaimBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='running',worker_token=?,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND (status IN ('preview','pending','failed','partially_failed','completed','canceled')
		 OR (status='running' AND (lease_expires_at=0 OR lease_expires_at<?)))`,
		workerToken, leaseExpiresAt, batchID, time.Now().UTC().Unix())
	if err != nil {
		return false, err
	}
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
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// RenewBatchLease 仅允许当前 worker 延长租约，长批次不会因固定截止时间被并发接管。
func (b *ItemPublishBatches) RenewBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, leaseExpiresAt, batchID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FailClaimedBatch 释放初始化阶段失败的批次租约。worker token 防止旧 worker
// 覆盖已经被新 worker 接管的状态。
func (b *ItemPublishBatches) FailClaimedBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='failed',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, batchID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FinishBatchStatus 只允许当前持有租约的 worker 结束批次。
func (b *ItemPublishBatches) FinishBatchStatus(ctx context.Context, batchID, workerToken, status string) (bool, error) {
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batches
		SET status=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, status, batchID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FinalizeBatch 在单个事务内重算计数，并且只在所有行都进入终态后结束批次。
func (b *ItemPublishBatches) FinalizeBatch(ctx context.Context, batchID, workerToken string) (string, bool, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var total, success, failed, unfinished int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
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
	status := finalBatchStatus(success, failed)
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status=?,total_count=?,success_count=?,failed_count=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, status, total, success, failed, batchID, workerToken)
	if err != nil {
		return "", false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return status, true, nil
}

// BatchStatus 取批次状态。未找到返回 ErrNotFound。
func (b *ItemPublishBatches) BatchStatus(ctx context.Context, batchID string) (string, error) {
	var status string
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
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='running',error_message='',failure_kind='',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'`, rowID)
	return err
}

// ClaimRow 原子领取单行，防止多个 worker 重复发布同一商品。
func (b *ItemPublishBatches) ClaimRow(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='running',worker_token=?,error_message='',failure_kind='',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='pending'
		  AND EXISTS (SELECT 1 FROM item_publish_batches b
		              WHERE b.id=item_publish_batch_rows.batch_id AND b.status='running' AND b.worker_token=?)`,
		workerToken, rowID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkClaimedRemoteStarted 在调用闲鱼发布接口前落盘。进程若在远端返回前硬退出，
// 租约接管方会把该行隔离为 uncertain_remote，而不是再次发布。
func (b *ItemPublishBatches) MarkClaimedRemoteStarted(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET failure_kind='remote_started',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=? AND COALESCE(item_id,'')=''`, rowID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkClaimedRowSuccess 只允许领取该行的 worker 写入成功结果。
func (b *ItemPublishBatches) MarkClaimedRowSuccess(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	if rawJSON == "" {
		rawJSON = "{}"
	}
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='success',item_id=?,item_url=?,error_message='',failure_kind='',worker_token='',raw_json=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, itemID, itemURL, rawJSON, rowID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// SaveClaimedRemoteResult 在闲鱼发布成功后第一时间保存远端商品标识。
// 后续本地商品/自动化规则写入失败时，重试会从该断点继续而不会再次调用发布接口。
func (b *ItemPublishBatches) SaveClaimedRemoteResult(ctx context.Context, rowID int64, workerToken, itemID, itemURL, rawJSON string) (bool, error) {
	if strings.TrimSpace(itemID) == "" {
		return false, errors.New("远端发布结果缺少商品ID")
	}
	if rawJSON == "" {
		rawJSON = "{}"
	}
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET item_id=?,item_url=?,raw_json=?,failure_kind='post_publish',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, itemID, itemURL, rawJSON, rowID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkClaimedRowFailed 只允许领取该行的 worker 写入失败结果。
func (b *ItemPublishBatches) MarkClaimedRowFailed(ctx context.Context, rowID int64, workerToken, message, kind string) (bool, error) {
	res, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message=?,failure_kind=?,worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, message, kind, rowID, workerToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// MarkRowSuccess 标记明细行发布成功，回填闲鱼商品 ID、URL 与原始返回 JSON。
func (b *ItemPublishBatches) MarkRowSuccess(ctx context.Context, rowID int64, itemID, itemURL, rawJSON string) error {
	if rawJSON == "" {
		rawJSON = "{}"
	}
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

func (b *ItemPublishBatches) MarkRowFailedKind(ctx context.Context, rowID int64, message, kind string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='failed',error_message=?,failure_kind=?,worker_token='',updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		message, kind, rowID)
	return err
}

// MarkRunningFailed 将批次内仍在 running 的行标为失败。
func (b *ItemPublishBatches) MarkRunningFailed(ctx context.Context, batchID, message string) error {
	if _, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message='远端发布结果未知，请人工核对闲鱼商品列表',failure_kind='uncertain_remote',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID); err != nil {
		return err
	}
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='failed',error_message=?,failure_kind='interrupted',worker_token='',updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status='running'`,
		message, batchID)
	return err
}

// MarkUnfinishedFailed 将批次内 pending/running 行标为失败。
func (b *ItemPublishBatches) MarkUnfinishedFailed(ctx context.Context, batchID, message string) error {
	if _, err := b.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message='远端发布结果未知，请人工核对闲鱼商品列表',failure_kind='uncertain_remote',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID); err != nil {
		return err
	}
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='failed',error_message=?,failure_kind='interrupted',worker_token='',updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status IN ('pending','running')`,
		message, batchID)
	return err
}

// ResetFailed 将批次内所有 failed 行重置为 pending，便于失败重试。
func (b *ItemPublishBatches) ResetFailed(ctx context.Context, batchID string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
			    SET status='pending',error_message='',failure_kind='',worker_token='',updated_at=CURRENT_TIMESTAMP
			  WHERE batch_id=? AND status='failed'
			    AND COALESCE(failure_kind,'') NOT IN ('validation','uncertain_remote')`, batchID)
	return err
}

// Recount 按明细行实际状态重算批次的 total/success/failed 计数。
// worker 每完成一行后调用，保证前端进度与 DB 一致。
func (b *ItemPublishBatches) Recount(ctx context.Context, batchID string) error {
	var total, success, failed int
	if err := b.DB.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		   FROM item_publish_batch_rows WHERE batch_id=?`, batchID).Scan(&total, &success, &failed); err != nil {
		return err
	}
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batches
		    SET total_count=?,success_count=?,failed_count=?,updated_at=CURRENT_TIMESTAMP
		  WHERE id=?`, total, success, failed, batchID)
	return err
}
