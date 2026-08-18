package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ItemPublishBatches 管理商品批量发布批次及其明细行的持久化。
//
// 一个批次对应一次 Excel/CSV 导入，包含若干明细行（每行一个待发布商品）。
// 发布 worker 按 row_no 顺序逐行发布，通过状态机（pending→running→success/failed）
// 跟踪进度，支持失败重置（ResetFailed）和实时计数（Recount）。
// ItemPublishBatches 用于本次流程后续判断的商品发布批次列表
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
	LocationJSON    string // 批次统一使用的发货地 JSON
	// PublishIntervalSeconds 是相邻两次最终商品发布请求的最小间隔秒数。
	PublishIntervalSeconds int
	// LastPublishStartedAtMillis 是最近一次最终商品发布请求开始的 Unix 毫秒时间戳。
	LastPublishStartedAtMillis int64
	Status                     string // 批次状态：pending/running/completed/partially_failed/failed
	TotalCount                 int    // 明细行总数（Recount 维护）
	SuccessCount               int    // 成功数（Recount 维护）
	FailedCount                int    // 失败数（Recount 维护）
	WorkerToken                string
	LeaseExpiresAt             int64
	CreatedAt                  string
	UpdatedAt                  string
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
	CategoryJSON   string // 用户指定的优先类目 JSON；为空时自动识别
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
// Create 创建当前值。
func (b *ItemPublishBatches) Create(ctx context.Context, batch *ItemPublishBatch, rows []ItemPublishBatchRow) error {
	if strings.TrimSpace(batch.LocationJSON) == "" {
		batch.LocationJSON = "{}"
	}
	if batch.PublishIntervalSeconds <= 0 {
		batch.PublishIntervalSeconds = 5
	}
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx,
		`INSERT INTO item_publish_batches
		 (id,user_id,default_cookie_id,filename,upload_dir,location_json,publish_interval_seconds,status,total_count,success_count,failed_count)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		batch.ID, batch.UserID, batch.DefaultCookieID, batch.Filename, batch.UploadDir,
		batch.LocationJSON, batch.PublishIntervalSeconds, batch.Status, len(rows), 0, 0); err != nil {
		return err
	}
	// row 表示当前遍历过程中的row
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
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx,
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
	// out 用于本次流程后续判断的out
	var out ItemPublishBatch
	// err 用于本次流程后续判断的err
	err := b.DB.QueryRowContext(ctx,
		`SELECT id,user_id,default_cookie_id,filename,upload_dir,COALESCE(location_json,'{}'),COALESCE(publish_interval_seconds,5),COALESCE(last_publish_started_at_millis,0),status,total_count,success_count,failed_count,
		        COALESCE(worker_token,''),COALESCE(lease_expires_at,0),
		        created_at,updated_at
		   FROM item_publish_batches WHERE id=? AND user_id=?`, id, userID).Scan(
		&out.ID, &out.UserID, &out.DefaultCookieID, &out.Filename, &out.UploadDir, &out.LocationJSON, &out.PublishIntervalSeconds, &out.LastPublishStartedAtMillis, &out.Status,
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,default_cookie_id,filename,upload_dir,COALESCE(location_json,'{}'),COALESCE(publish_interval_seconds,5),COALESCE(last_publish_started_at_millis,0),status,
		total_count,success_count,failed_count,COALESCE(worker_token,''),COALESCE(lease_expires_at,0),created_at,updated_at
		FROM item_publish_batches WHERE user_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, userID, limit)
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
		err := rows.Scan(&batch.ID, &batch.UserID, &batch.DefaultCookieID, &batch.Filename, &batch.UploadDir, &batch.LocationJSON, &batch.PublishIntervalSeconds, &batch.LastPublishStartedAtMillis,
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,default_cookie_id,filename,upload_dir,COALESCE(location_json,'{}'),COALESCE(publish_interval_seconds,5),COALESCE(last_publish_started_at_millis,0),status,
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
	// out 用于本次流程后续判断的out
	var out []ItemPublishBatch
	for rows.Next() {
		// batch 用于本次流程后续判断的批次
		var batch ItemPublishBatch
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&batch.ID, &batch.UserID, &batch.DefaultCookieID, &batch.Filename, &batch.UploadDir, &batch.LocationJSON, &batch.PublishIntervalSeconds, &batch.LastPublishStartedAtMillis,
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
	// rows、err 用于本次流程后续判断的rows、err
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
	// out 用于本次流程后续判断的out
	out := []ItemPublishBatchRow{}
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r ItemPublishBatchRow
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.Title, &r.Description, &r.Price,
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
	// statuses 用于本次流程后续判断的statuses
	statuses := "('pending')"
	if failedOnly {
		statuses = "('failed')"
	}
	// rows、err 用于本次流程后续判断的rows、err
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
	// out 用于本次流程后续判断的out
	out := []ItemPublishBatchRow{}
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r ItemPublishBatchRow
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.Title, &r.Description, &r.Price,
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
	// err 用于本次流程后续判断的err
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batches SET status=?,worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, batchID)
	return err
}

// RequestCancel 进入两阶段取消。运行中的批次保留 worker token，允许 worker
// 把已经获得的远端商品 ID 落库后再结束；未运行批次可直接取消。
// RequestCancel 封装请求取消业务协调。
func (b *ItemPublishBatches) RequestCancel(ctx context.Context, batchID string) (string, bool, error) {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	// status、token 用于本次流程后续判断的status、token
	var status, token string
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, `SELECT status,worker_token FROM item_publish_batches WHERE id=?`, batchID).Scan(&status, &token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, err
	}
	// running 用于本次流程后续判断的running
	running := (status == "running" || status == "canceling") && token != ""
	if running {
		// res、err 用于本次流程后续判断的res、err
		res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches SET status='canceling',updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status=? AND worker_token=?`, batchID, status, token)
		if err != nil {
			return "", false, err
		}
		if // n、rowsErr 用于本次流程后续判断的n、rowsErr
		n, rowsErr := res.RowsAffected(); rowsErr != nil {
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
		if // n、rowsErr 用于本次流程后续判断的n、rowsErr
		n, rowsErr := res.RowsAffected(); rowsErr != nil {
			return "", false, rowsErr
		} else if n != 1 {
			return "", false, ErrPublishBatchChanged
		}
		if // err 用于本次流程后续判断的err
		err := markUnfinishedFailedTx(ctx, tx, batchID, "任务已取消"); err != nil {
			return "", false, err
		}
		if // err 用于本次流程后续判断的err
		err := recountBatchTx(ctx, tx, batchID); err != nil {
			return "", false, err
		}
		res, err = tx.ExecContext(ctx, `UPDATE item_publish_batches SET status='canceled',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status='finalizing_cancel' AND worker_token=?`, batchID, token)
		if err != nil {
			return "", false, err
		}
		if // n、rowsErr 用于本次流程后续判断的n、rowsErr
		n, rowsErr := res.RowsAffected(); rowsErr != nil {
			return "", false, rowsErr
		} else if n != 1 {
			return "", false, ErrPublishBatchChanged
		}
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return "", false, err
	}
	return token, running, nil
}

// FinalizeCanceled 封装FinalizeCanceled业务协调。
func (b *ItemPublishBatches) FinalizeCanceled(ctx context.Context, batchID, workerToken string) (bool, error) {
	// tx、err 用于本次流程后续判断的tx、err
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
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if // err 用于本次流程后续判断的err
	err := markUnfinishedFailedTx(ctx, tx, batchID, "任务已取消"); err != nil {
		return false, err
	}
	if // err 用于本次流程后续判断的err
	err := recountBatchTx(ctx, tx, batchID); err != nil {
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
// FinalizeInterrupted 封装FinalizeInterrupted业务协调。
func (b *ItemPublishBatches) FinalizeInterrupted(ctx context.Context, batchID, workerToken, message string) (string, bool, error) {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='finalizing_interrupted',updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, batchID, workerToken)
	if err != nil {
		return "", false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	if // err 用于本次流程后续判断的err
	err := markUnfinishedFailedTx(ctx, tx, batchID, message); err != nil {
		return "", false, err
	}
	// total、success、failed、unfinished 用于本次流程后续判断的total、success、failed、unfinished
	var total, success, failed, unfinished int
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status NOT IN ('success','failed') THEN 1 ELSE 0 END),0)
		FROM item_publish_batch_rows WHERE batch_id=?`, batchID).Scan(&total, &success, &failed, &unfinished); err != nil {
		return "", false, err
	}
	if unfinished != 0 || total != success+failed {
		return "", false, errors.New("中断批次仍有未完成行")
	}
	// status 用于本次流程后续判断的状态
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
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return "", false, err
	}
	return status, true, nil
}

// finalBatchStatus 封装final批次状态业务协调。
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
// FinalizeExpiredCancellation 封装FinalizeExpiredCancellation业务协调。
func (b *ItemPublishBatches) FinalizeExpiredCancellation(ctx context.Context, batchID string, now int64) (bool, error) {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET status='canceled',worker_token='',lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='canceling' AND (lease_expires_at=0 OR lease_expires_at<?)`, batchID, now)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if // err 用于本次流程后续判断的err
	err := markUnfinishedFailedTx(ctx, tx, batchID, "任务已取消"); err != nil {
		return false, err
	}
	if // err 用于本次流程后续判断的err
	err := recountBatchTx(ctx, tx, batchID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// markUnfinishedFailedTx 封装markUnfinished失败Tx业务协调。
func markUnfinishedFailedTx(ctx context.Context, tx *sql.Tx, batchID, message string) error {
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message='任务取消时远端发布结果未知，请人工核对闲鱼商品列表',
		    failure_kind='uncertain_remote',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status='running' AND failure_kind='remote_started' AND COALESCE(item_id,'')=''`, batchID); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `UPDATE item_publish_batch_rows
		SET status='failed',error_message=?,failure_kind='interrupted',worker_token='',updated_at=CURRENT_TIMESTAMP
		WHERE batch_id=? AND status IN ('pending','running')`, message, batchID)
	return err
}

// recountBatchTx 封装recount批次Tx业务协调。
func recountBatchTx(ctx context.Context, tx *sql.Tx, batchID string) error {
	// total、success、failed 用于本次流程后续判断的total、success、failed
	var total, success, failed int
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		FROM item_publish_batch_rows WHERE batch_id=?`, batchID).Scan(&total, &success, &failed); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `UPDATE item_publish_batches
		SET total_count=?,success_count=?,failed_count=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		total, success, failed, batchID)
	return err
}

// Delete 删除当前值。
