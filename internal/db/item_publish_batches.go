package db

import (
	"context"
	"database/sql"
	"errors"
)

// ItemPublishBatches 管理商品批量发布批次及其明细行的持久化。
//
// 一个批次对应一次 Excel/CSV 导入，包含若干明细行（每行一个待发布商品）。
// 发布 worker 按 row_no 顺序逐行发布，通过状态机（pending→running→success/failed）
// 跟踪进度，支持失败重置（ResetFailed）和实时计数（Recount）。
type ItemPublishBatches struct{ DB *sql.DB }

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
	AutomationJSON string // 发布后自动创建的自动化规则配置 JSON
	Status         string // pending/running/success/failed
	ItemID         string // 发布成功后回填的闲鱼商品 ID
	ItemURL        string // 发布成功后回填的商品 URL
	ErrorMessage   string // 失败原因
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
		if row.AutomationJSON == "" {
			row.AutomationJSON = "{}"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_publish_batch_rows
			 (batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
			  images_json,automation_json,status,error_message,raw_json)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			batch.ID, row.RowNo, row.CookieID, row.Title, row.Description, row.Price, row.OriginalPrice,
			row.Quantity, row.PostageMode, row.Postage, row.ImagesJSON, row.AutomationJSON,
			row.Status, row.ErrorMessage, row.RawJSON); err != nil {
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
		        created_at,updated_at
		   FROM item_publish_batches WHERE id=? AND user_id=?`, id, userID).Scan(
		&out.ID, &out.UserID, &out.DefaultCookieID, &out.Filename, &out.UploadDir, &out.Status,
		&out.TotalCount, &out.SuccessCount, &out.FailedCount, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

// Rows 取批次全部明细行，按 row_no 升序。
func (b *ItemPublishBatches) Rows(ctx context.Context, batchID string) ([]ItemPublishBatchRow, error) {
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id,batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
		        images_json,COALESCE(automation_json,'{}'),status,item_id,item_url,error_message,
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
			&r.OriginalPrice, &r.Quantity, &r.PostageMode, &r.Postage, &r.ImagesJSON, &r.AutomationJSON,
			&r.Status, &r.ItemID, &r.ItemURL, &r.ErrorMessage, &r.RawJSON, &r.CreatedAt,
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
		        images_json,COALESCE(automation_json,'{}'),status,item_id,item_url,error_message,
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
			&r.OriginalPrice, &r.Quantity, &r.PostageMode, &r.Postage, &r.ImagesJSON, &r.AutomationJSON,
			&r.Status, &r.ItemID, &r.ItemURL, &r.ErrorMessage, &r.RawJSON, &r.CreatedAt,
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
		`UPDATE item_publish_batches SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, batchID)
	return err
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
		`UPDATE item_publish_batch_rows SET status='running',error_message='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, rowID)
	return err
}

// MarkRowSuccess 标记明细行发布成功，回填闲鱼商品 ID、URL 与原始返回 JSON。
func (b *ItemPublishBatches) MarkRowSuccess(ctx context.Context, rowID int64, itemID, itemURL, rawJSON string) error {
	if rawJSON == "" {
		rawJSON = "{}"
	}
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
		    SET status='success',item_id=?,item_url=?,error_message='',raw_json=?,updated_at=CURRENT_TIMESTAMP
		  WHERE id=?`, itemID, itemURL, rawJSON, rowID)
	return err
}

// MarkRowFailed 标记明细行发布失败并记录错误原因。
func (b *ItemPublishBatches) MarkRowFailed(ctx context.Context, rowID int64, message string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='failed',error_message=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		message, rowID)
	return err
}

// MarkRunningFailed 将批次内仍在 running 的行标为失败。
func (b *ItemPublishBatches) MarkRunningFailed(ctx context.Context, batchID, message string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
		    SET status='failed',error_message=?,updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status='running'`,
		message, batchID)
	return err
}

// MarkUnfinishedFailed 将批次内 pending/running 行标为失败。
func (b *ItemPublishBatches) MarkUnfinishedFailed(ctx context.Context, batchID, message string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
		    SET status='failed',error_message=?,updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status IN ('pending','running')`,
		message, batchID)
	return err
}

// ResetFailed 将批次内所有 failed 行重置为 pending，便于失败重试。
func (b *ItemPublishBatches) ResetFailed(ctx context.Context, batchID string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
		    SET status='pending',error_message='',updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status='failed'`, batchID)
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
