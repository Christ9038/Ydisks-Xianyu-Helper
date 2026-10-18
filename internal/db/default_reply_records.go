package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DefaultReplyRecords 是不可变商品作用域的投递记录仓储；共享连接池但不保存并发可变状态。
// itemID 为空时兼容账号兜底，非空时按账号、商品、会话独立去重。
type DefaultReplyRecords struct {
	// database 由 Store 拥有并关闭，当前轻量作用域对象不管理连接生命周期。
	database *sql.DB
	// dialect 决定原子忽略冲突的 SQL 方言，不可在构造后修改。
	dialect Dialect
	// itemID 是固定商品作用域，空串只匹配历史账号兜底记录。
	itemID string
}

// RecordsForItem 以 itemID 创建不可变记录访问器，不改变 d 的账号配置或其他调用方作用域。
func (d *DefaultReplies) RecordsForItem(itemID string) *DefaultReplyRecords {
	return &DefaultReplyRecords{database: d.DB, dialect: d.Dialect, itemID: itemID}
}

// ClearRecords 在 ctx 下仅清空 cookieID 与当前商品作用域的投递记录，返回数据库错误。
func (d *DefaultReplyRecords) ClearRecords(ctx context.Context, cookieID string) error {
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	_, err := d.database.ExecContext(ctx, `DELETE FROM default_reply_records WHERE cookie_id=? AND item_id=?`, cookieID, d.itemID)
	return err
}

// HasRecord 检查 ctx 下 cookieID/chatID 在当前作用域是否已经完成；读取失败兼容返回 false。
func (d *DefaultReplyRecords) HasRecord(ctx context.Context, cookieID, chatID string) bool {
	// n 是已完成记录存在性查询的哨兵，不加载消息正文。
	var n int
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	err := d.database.QueryRowContext(ctx,
		`SELECT 1 FROM default_reply_records WHERE cookie_id=? AND chat_id=? AND item_id=? AND status='sent' LIMIT 1`,
		cookieID, chatID, d.itemID).Scan(&n)
	return err == nil
}

// ClaimRecord 原子领取一次默认回复投递。新记录初始化为 sending；失败记录允许继续
// 投递尚未成功的部分；sending/pending/sent 记录会阻止并发重复发送。
// ctx 控制本地操作，cookieID/chatID 是账号会话身份；needsText/needsImage 表示待发送分段，返回快照、领取权与错误。
func (d *DefaultReplyRecords) ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (DefaultReplyRecord, bool, error) {
	// now 是当前 UTC Unix 秒，用于判断历史 pending 租约是否过期。
	now := time.Now().UTC().Unix()
	// leaseExpiresAt 是五分钟后的 Unix 秒；sending 状态不会因其过期而重放。
	leaseExpiresAt := now + int64((5*time.Minute)/time.Second)
	// query 用三元唯一键原子插入记录，账号作用域的 item_id 固定为空串。
	query := dialectInsertIgnorePrefix(d.dialect) + ` INTO default_reply_records
		(cookie_id,chat_id,item_id,status,text_sent,image_sent,last_error,lease_expires_at,updated_at)
		VALUES (?,?,?, 'sending', ?, ?, '', ?, CURRENT_TIMESTAMP)` + dialectInsertIgnore(d.dialect, []string{"cookie_id", "chat_id", "item_id"})
	// res、err 保存插入或领取结果，影响行数决定当前调用是否取得发送权。
	res, err := d.database.ExecContext(ctx, query, cookieID, chatID, d.itemID, boolToInt(!needsText), boolToInt(!needsImage), leaseExpiresAt)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if // affected 是本次原子语句实际修改行数，只有大于零才允许发送。
	affected, _ := res.RowsAffected(); affected > 0 {
		return DefaultReplyRecord{Status: defaultReplyStatusSending, TextSent: !needsText, ImageSent: !needsImage}, true, nil
	}

	// record、err 保存已存在记录及读取错误，避免覆盖已发送分段。
	record, err := d.Record(ctx, cookieID, chatID)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if record.Status == "sent" {
		return record, false, nil
	}
	// pending 是旧版本发送任务的短租约。进程崩溃或强制退出后，历史 pending
	// 记录仍可被新实例接管；新建记录使用 sending，避免未知结果自动重发。
	res, err = d.database.ExecContext(ctx, `UPDATE default_reply_records
		SET status='pending',last_error='',lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=? AND item_id=?
		  AND (status='failed' OR (status='pending' AND lease_expires_at<? AND COALESCE(last_error,'') NOT LIKE ?))`,
		leaseExpiresAt, cookieID, chatID, d.itemID, now, uncertainReplyErrorPrefix+"%")
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	// affected 是本次原子语句实际修改行数，只有大于零才允许发送。
	affected, _ := res.RowsAffected()
	return record, affected > 0, nil
}

// Record 查询 ctx 下 cookieID/chatID 在当前商品作用域的投递快照；不存在返回 sql.ErrNoRows。
func (d *DefaultReplyRecords) Record(ctx context.Context, cookieID, chatID string) (DefaultReplyRecord, error) {
	// record 保存当前作用域的一次性状态及已确认分段。
	var record DefaultReplyRecord
	// textSent、imageSent 是数据库布尔值，转换后用于跳过已经确认的分段。
	var textSent, imageSent int
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	err := d.database.QueryRowContext(ctx, `SELECT status,text_sent,image_sent
		FROM default_reply_records WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, d.itemID).
		Scan(&record.Status, &textSent, &imageSent)
	record.TextSent = textSent != 0
	record.ImageSent = imageSent != 0
	return record, err
}

// MarkPartSent 标记 ctx 下 cookieID/chatID 的已确认 part 分段；仅接受 text/image，拒绝未知列名。
func (d *DefaultReplyRecords) MarkPartSent(ctx context.Context, cookieID, chatID, part string) error {
	// column 只从固定白名单选择分段列名，不能拼接调用方原始输入。
	column := ""
	switch part {
	case "text":
		column = "text_sent"
	case "image":
		column = "image_sent"
	default:
		return errors.New("未知默认回复部分")
	}
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	_, err := d.database.ExecContext(ctx, `UPDATE default_reply_records SET `+column+`=1,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, d.itemID)
	return err
}

// MarkRecordFailed 以 message 记录确定未发送的错误；ctx 控制写入，cookieID/chatID 与仓储商品作用域共同限定记录。
func (d *DefaultReplyRecords) MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error {
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	_, err := d.database.ExecContext(ctx, `UPDATE default_reply_records
		SET status='failed',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=? AND item_id=?`, message, cookieID, chatID, d.itemID)
	return err
}

// MarkRecordUncertain 用 message 隔离 ctx 下 cookieID/chatID 的未知投递；当前作用域始终参与写入和降级隔离。
func (d *DefaultReplyRecords) MarkRecordUncertain(ctx context.Context, cookieID, chatID, message string) error {
	// err 保存将一次性回复转换为人工核对状态时的数据库错误。
	_, err := d.database.ExecContext(ctx, `UPDATE default_reply_records
		SET status='uncertain',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=? AND item_id=?`, message, cookieID, chatID, d.itemID)
	if err == nil {
		return nil
	}
	// fallbackMessage 保存无法写入 uncertain 状态时仍可持久化的隔离标记。
	fallbackMessage := uncertainReplyErrorPrefix + message
	// fallbackErr 保存降级为 pending 隔离记录时的数据库错误。
	_, fallbackErr := d.database.ExecContext(ctx, `UPDATE default_reply_records
		SET status='pending',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=? AND item_id=?`, fallbackMessage, cookieID, chatID, d.itemID)
	if fallbackErr == nil {
		return nil
	}
	return errors.Join(err, fallbackErr)
}

// MarkRecordSent 将 ctx 下 cookieID/chatID 的完整回复收口为已发送，保留已有分段事实。
func (d *DefaultReplyRecords) MarkRecordSent(ctx context.Context, cookieID, chatID string) error {
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	_, err := d.database.ExecContext(ctx, `UPDATE default_reply_records
		SET status='sent',last_error='',lease_expires_at=0,replied_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, d.itemID)
	return err
}

// AddRecord 在 ctx 下按 cookieID/chatID 与当前商品作用域幂等记录完成状态，供兼容入口使用。
func (d *DefaultReplyRecords) AddRecord(ctx context.Context, cookieID, chatID string) error {
	// err 保存本次限定账号、商品作用域的数据库操作错误。
	_, err := d.database.ExecContext(ctx,
		dialectInsertIgnorePrefix(d.dialect)+` INTO default_reply_records (cookie_id, chat_id, item_id) VALUES (?, ?, ?)`+dialectInsertIgnore(d.dialect, []string{"cookie_id", "chat_id", "item_id"}),
		cookieID, chatID, d.itemID)
	return err
}
