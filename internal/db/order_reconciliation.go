package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// OrderReconciliation 保存外部订单动作成功但本地状态尚未完成同步的补偿记录。
type OrderReconciliation struct {
	// ID 是补偿记录唯一标识。
	ID string
	// OrderID 是需要重新核对的订单标识。
	OrderID string
	// CookieID 是执行外部动作的账号标识。
	CookieID string
	// Kind 是外部动作类型，例如 manual_status_ship。
	Kind string
	// Status 是补偿状态：pending、resolved 或 failed。
	Status string
	// ErrorMessage 保存最近一次本地持久化失败原因。
	ErrorMessage string
	// Attempts 记录补偿任务尝试次数。
	Attempts int
	// CreatedAt 是记录创建时间文本。
	CreatedAt string
	// UpdatedAt 是记录最近更新时间文本。
	UpdatedAt string
}

// OrderReconciliations 管理订单外部动作与本地状态之间的补偿记录。
type OrderReconciliations struct {
	// DB 是补偿记录使用的数据库连接。
	DB *sql.DB
}

// CreatePending 创建一条待补偿记录，并返回可用于审计和后续 worker 的记录 ID。
func (r *OrderReconciliations) CreatePending(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	if r == nil || r.DB == nil {
		return "", errors.New("补偿记录存储未初始化")
	}
	if strings.TrimSpace(orderID) == "" || strings.TrimSpace(cookieID) == "" || strings.TrimSpace(kind) == "" {
		return "", errors.New("补偿记录缺少订单、账号或动作类型")
	}
	// id 是本次补偿记录的稳定唯一标识。
	id := uuid.NewString()
	// err 表示补偿记录写入失败。
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO order_reconciliations
			(id, order_id, cookie_id, kind, status, error_message, attempts)
		 VALUES (?, ?, ?, ?, 'pending', ?, 0)`,
		id, orderID, cookieID, kind, message)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListPending 返回待补偿记录，供后续恢复 worker 扫描和审计使用。
func (r *OrderReconciliations) ListPending(ctx context.Context, limit int) ([]OrderReconciliation, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("补偿记录存储未初始化")
	}
	if limit <= 0 {
		limit = 100
	}
	// rows、err 保存待补偿查询结果及其错误。
	rows, err := r.DB.QueryContext(ctx, `
		SELECT id, order_id, cookie_id, kind, status, error_message, attempts, created_at, updated_at
		FROM order_reconciliations WHERE status='pending' ORDER BY created_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 保存当前待补偿记录列表。
	records := make([]OrderReconciliation, 0)
	for rows.Next() {
		// record 保存当前扫描到的补偿记录。
		var record OrderReconciliation
		// err 表示当前补偿记录扫描失败。
		if err := rows.Scan(&record.ID, &record.OrderID, &record.CookieID, &record.Kind, &record.Status, &record.ErrorMessage, &record.Attempts, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	// err 表示数据库游标迭代失败。
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// MarkResolved 将补偿记录标记为已完成，并保留最终处理说明。
func (r *OrderReconciliations) MarkResolved(ctx context.Context, id, message string) error {
	if r == nil || r.DB == nil {
		return errors.New("补偿记录存储未初始化")
	}
	// result、err 保存补偿状态更新结果及其错误。
	result, err := r.DB.ExecContext(ctx,
		`UPDATE order_reconciliations SET status='resolved', error_message=?, attempts=attempts+1, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'`,
		message, id)
	if err != nil {
		return err
	}
	// affected、err 保存实际更新行数及读取行数错误。
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordAttempt 记录一次补偿失败并保持 pending 状态，供后续 worker 重试。
func (r *OrderReconciliations) RecordAttempt(ctx context.Context, id, message string) error {
	if r == nil || r.DB == nil {
		return errors.New("补偿记录存储未初始化")
	}
	// result、err 保存补偿尝试写入结果及错误。
	result, err := r.DB.ExecContext(ctx,
		`UPDATE order_reconciliations SET error_message=?, attempts=attempts+1, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'`,
		message, id)
	if err != nil {
		return err
	}
	// affected、err 保存受影响行数及读取行数错误。
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
