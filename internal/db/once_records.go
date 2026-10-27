package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// onceReplyRecords 封装“每个 (cookie_id, chat_id) 只投递一次”的持久化状态机。
// 账号默认回复与商品首次回复共用同一套领取、分段确认和隔离语义，仅表名不同。
// table 只能由本包内的常量赋值，禁止来自外部输入，因为它会被拼接进 SQL。
type onceReplyRecords struct {
	// db 是记录表所在的数据库连接。
	db *sql.DB
	// dialect 决定 INSERT IGNORE 的方言写法。
	dialect Dialect
	// table 是记录表名，取值固定为 default_reply_records 或 item_greeting_records。
	table string
}

// ClaimRecord 原子领取一次默认回复投递。新记录初始化为 sending；失败记录允许继续
// 投递尚未成功的部分；sending/pending/sent 记录会阻止并发重复发送。
// ClaimRecord 封装ClaimRecord业务协调。
func (d onceReplyRecords) ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (DefaultReplyRecord, bool, error) {
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// leaseExpiresAt 用于本次流程后续判断的leaseExpiresAt
	leaseExpiresAt := now + int64((5*time.Minute)/time.Second)
	// query 用于本次流程后续判断的查询
	query := dialectInsertIgnorePrefix(d.dialect) + ` INTO ` + d.table + `
		(cookie_id,chat_id,status,text_sent,image_sent,last_error,lease_expires_at,updated_at)
		VALUES (?,?, 'sending', ?, ?, '', ?, CURRENT_TIMESTAMP)` + dialectInsertIgnore(d.dialect, []string{"cookie_id", "chat_id"})
	// res、err 用于本次流程后续判断的res、err
	res, err := d.db.ExecContext(ctx, query, cookieID, chatID, boolToInt(!needsText), boolToInt(!needsImage), leaseExpiresAt)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if // affected 用于本次流程后续判断的affected
	affected, _ := res.RowsAffected(); affected > 0 {
		return DefaultReplyRecord{Status: defaultReplyStatusSending, TextSent: !needsText, ImageSent: !needsImage}, true, nil
	}

	// record、err 用于本次流程后续判断的record、err
	record, err := d.Record(ctx, cookieID, chatID)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if record.Status == "sent" {
		return record, false, nil
	}
	// pending 是旧版本发送任务的短租约。进程崩溃或强制退出后，历史 pending
	// 记录仍可被新实例接管；新建记录使用 sending，避免未知结果自动重发。
	res, err = d.db.ExecContext(ctx, `UPDATE `+d.table+`
		SET status='pending',last_error='',lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?
		  AND (status='failed' OR (status='pending' AND lease_expires_at<? AND COALESCE(last_error,'') NOT LIKE ?))`,
		leaseExpiresAt, cookieID, chatID, now, uncertainReplyErrorPrefix+"%")
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	// affected 用于本次流程后续判断的affected
	affected, _ := res.RowsAffected()
	return record, affected > 0, nil
}

// Record 查询一次默认回复的投递状态。
func (d onceReplyRecords) Record(ctx context.Context, cookieID, chatID string) (DefaultReplyRecord, error) {
	// record 用于本次流程后续判断的record
	var record DefaultReplyRecord
	// textSent、imageSent 用于本次流程后续判断的文本Sent、imageSent
	var textSent, imageSent int
	// err 用于本次流程后续判断的err
	err := d.db.QueryRowContext(ctx, `SELECT status,text_sent,image_sent
		FROM `+d.table+` WHERE cookie_id=? AND chat_id=?`, cookieID, chatID).
		Scan(&record.Status, &textSent, &imageSent)
	record.TextSent = textSent != 0
	record.ImageSent = imageSent != 0
	return record, err
}

// MarkPartSent 标记图片或文字已经成功投递。
func (d onceReplyRecords) MarkPartSent(ctx context.Context, cookieID, chatID, part string) error {
	// column 用于本次流程后续判断的column
	column := ""
	switch part {
	case "text":
		column = "text_sent"
	case "image":
		column = "image_sent"
	default:
		return errors.New("未知默认回复部分")
	}
	// err 用于本次流程后续判断的err
	_, err := d.db.ExecContext(ctx, `UPDATE `+d.table+` SET `+column+`=1,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// MarkRecordFailed 封装MarkRecord失败业务协调。
func (d onceReplyRecords) MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error {
	// err 用于本次流程后续判断的err
	_, err := d.db.ExecContext(ctx, `UPDATE `+d.table+`
		SET status='failed',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=?`, message, cookieID, chatID)
	return err
}

// MarkRecordUncertain 隔离可能已经发送但未得到可靠本地确认的默认回复，避免自动重发。
func (d onceReplyRecords) MarkRecordUncertain(ctx context.Context, cookieID, chatID, message string) error {
	// err 保存将一次性回复转换为人工核对状态时的数据库错误。
	_, err := d.db.ExecContext(ctx, `UPDATE `+d.table+`
		SET status='uncertain',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=?`, message, cookieID, chatID)
	if err == nil {
		return nil
	}
	// fallbackMessage 保存无法写入 uncertain 状态时仍可持久化的隔离标记。
	fallbackMessage := uncertainReplyErrorPrefix + message
	// fallbackErr 保存降级为 pending 隔离记录时的数据库错误。
	_, fallbackErr := d.db.ExecContext(ctx, `UPDATE `+d.table+`
		SET status='pending',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=?`, fallbackMessage, cookieID, chatID)
	if fallbackErr == nil {
		return nil
	}
	return errors.Join(err, fallbackErr)
}

// MarkRecordSent 封装MarkRecordSent业务协调。
func (d onceReplyRecords) MarkRecordSent(ctx context.Context, cookieID, chatID string) error {
	// err 用于本次流程后续判断的err
	_, err := d.db.ExecContext(ctx, `UPDATE `+d.table+`
		SET status='sent',last_error='',lease_expires_at=0,replied_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// Exists 判断会话是否已有任意状态的投递记录；sent、sending 和 uncertain 都视为已触发。
func (d onceReplyRecords) Exists(ctx context.Context, cookieID, chatID string) (bool, error) {
	// n 保存存在性探测读取到的占位值。
	var n int
	// err 保存探测查询错误；无行表示该会话尚未触发。
	err := d.db.QueryRowContext(ctx,
		`SELECT 1 FROM `+d.table+` WHERE cookie_id=? AND chat_id=? LIMIT 1`, cookieID, chatID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Clear 删除指定账号的全部投递记录，使买家下次咨询时重新触发。
func (d onceReplyRecords) Clear(ctx context.Context, cookieID string) error {
	// err 保存清理记录的数据库错误。
	_, err := d.db.ExecContext(ctx, `DELETE FROM `+d.table+` WHERE cookie_id=?`, cookieID)
	return err
}
