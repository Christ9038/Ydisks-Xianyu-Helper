package db

import (
	"context"
	"database/sql"
	"errors"
)

type ItemPublishBatches struct{ DB *sql.DB }

type ItemPublishBatch struct {
	ID              string
	UserID          int64
	DefaultCookieID string
	Filename        string
	UploadDir       string
	Status          string
	TotalCount      int
	SuccessCount    int
	FailedCount     int
	CreatedAt       string
	UpdatedAt       string
}

type ItemPublishBatchRow struct {
	ID                     int64
	BatchID                string
	RowNo                  int
	CookieID               string
	Title                  string
	Description            string
	Price                  string
	OriginalPrice          string
	Quantity               int
	PostageMode            string
	Postage                string
	ImagesJSON             string
	AutoCreateDeliveryRule bool
	CardGroupID            int64
	DeliveryCount          int
	Status                 string
	ItemID                 string
	ItemURL                string
	ErrorMessage           string
	RawJSON                string
	CreatedAt              string
	UpdatedAt              string
}

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
		if row.DeliveryCount <= 0 {
			row.DeliveryCount = 1
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
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_publish_batch_rows
			 (batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
			  images_json,auto_create_delivery_rule,card_group_id,delivery_count,status,error_message,raw_json)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			batch.ID, row.RowNo, row.CookieID, row.Title, row.Description, row.Price, row.OriginalPrice,
			row.Quantity, row.PostageMode, row.Postage, row.ImagesJSON, boolToInt(row.AutoCreateDeliveryRule),
			row.CardGroupID, row.DeliveryCount, row.Status, row.ErrorMessage, row.RawJSON); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *ItemPublishBatches) Get(ctx context.Context, userID int64, id string) (*ItemPublishBatch, error) {
	var out ItemPublishBatch
	err := b.DB.QueryRowContext(ctx,
		`SELECT id,user_id,default_cookie_id,filename,upload_dir,status,total_count,success_count,failed_count,
		        COALESCE(created_at,''),COALESCE(updated_at,'')
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

func (b *ItemPublishBatches) Rows(ctx context.Context, batchID string) ([]ItemPublishBatchRow, error) {
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id,batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
		        images_json,auto_create_delivery_rule,card_group_id,delivery_count,status,item_id,item_url,error_message,
		        raw_json,COALESCE(created_at,''),COALESCE(updated_at,'')
		   FROM item_publish_batch_rows WHERE batch_id=? ORDER BY row_no`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemPublishBatchRow{}
	for rows.Next() {
		var r ItemPublishBatchRow
		var auto int
		if err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.Title, &r.Description, &r.Price,
			&r.OriginalPrice, &r.Quantity, &r.PostageMode, &r.Postage, &r.ImagesJSON, &auto, &r.CardGroupID,
			&r.DeliveryCount, &r.Status, &r.ItemID, &r.ItemURL, &r.ErrorMessage, &r.RawJSON, &r.CreatedAt,
			&r.UpdatedAt); err != nil {
			return nil, err
		}
		r.AutoCreateDeliveryRule = auto != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *ItemPublishBatches) PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]ItemPublishBatchRow, error) {
	statuses := "('pending')"
	if failedOnly {
		statuses = "('failed')"
	}
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id,batch_id,row_no,cookie_id,title,description,price,original_price,quantity,postage_mode,postage,
		        images_json,auto_create_delivery_rule,card_group_id,delivery_count,status,item_id,item_url,error_message,
		        raw_json,COALESCE(created_at,''),COALESCE(updated_at,'')
		   FROM item_publish_batch_rows WHERE batch_id=? AND status IN `+statuses+` ORDER BY row_no`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemPublishBatchRow{}
	for rows.Next() {
		var r ItemPublishBatchRow
		var auto int
		if err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.Title, &r.Description, &r.Price,
			&r.OriginalPrice, &r.Quantity, &r.PostageMode, &r.Postage, &r.ImagesJSON, &auto, &r.CardGroupID,
			&r.DeliveryCount, &r.Status, &r.ItemID, &r.ItemURL, &r.ErrorMessage, &r.RawJSON, &r.CreatedAt,
			&r.UpdatedAt); err != nil {
			return nil, err
		}
		r.AutoCreateDeliveryRule = auto != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *ItemPublishBatches) SetBatchStatus(ctx context.Context, batchID, status string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batches SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, batchID)
	return err
}

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

func (b *ItemPublishBatches) MarkRowRunning(ctx context.Context, rowID int64) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='running',error_message='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, rowID)
	return err
}

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

func (b *ItemPublishBatches) MarkRowFailed(ctx context.Context, rowID int64, message string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows SET status='failed',error_message=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		message, rowID)
	return err
}

func (b *ItemPublishBatches) ResetFailed(ctx context.Context, batchID string) error {
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_publish_batch_rows
		    SET status='pending',error_message='',updated_at=CURRENT_TIMESTAMP
		  WHERE batch_id=? AND status='failed'`, batchID)
	return err
}

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
