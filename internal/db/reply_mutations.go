package db

import (
	"context"
	"database/sql"
	"errors"
)

// replyQueryer 允许配置读取在普通连接和持有账号锁的事务中共用实现。
type replyQueryer interface {
	// QueryRowContext 根据 ctx 和参数化 query 读取单行，参数 args 不包含凭证。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// beginReplyWrite 为 database 中 cookieID 的配置开启写事务；调用方负责提交或回滚。
// SQLite 先取得写锁，其他方言锁定非敏感账号行；锁内仅允许本地数据库操作与纯计算。
func beginReplyWrite(ctx context.Context, database *sql.DB, dialect Dialect, cookieID string) (*sql.Tx, error) {
	// transaction、err 保存写事务及开启错误。
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// query 仅核对账号存在，不读取 Cookie 或加密字段。
	query := `SELECT id FROM cookies WHERE id=?`
	if dialect == DialectMySQL || dialect == DialectPostgres {
		query += ` FOR UPDATE`
	} else {
		// lockErr 是 SQLite 写锁获取失败原因，失败后不能继续读取快照。
		if _, lockErr := transaction.ExecContext(ctx, `UPDATE cookies SET id=id WHERE id=?`, cookieID); lockErr != nil {
			_ = transaction.Rollback()
			return nil, lockErr
		}
	}
	// existingID 是锁定账号的非敏感标识。
	var existingID string
	if err := transaction.QueryRowContext(ctx, query, cookieID).Scan(&existingID); err != nil { // err 是账号不存在或锁定读取错误。
		_ = transaction.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return transaction, nil
}

// readDefaultReply 从 queryer 读取 cookieID 的完整配置；NULL 图片兼容为空串。
func readDefaultReply(ctx context.Context, queryer replyQueryer, cookieID string) (*DefaultReply, error) {
	// reply 保存非敏感配置，enabled、once 保存数据库布尔整型。
	var reply DefaultReply
	// enabled、once 是数据库布尔整型，扫描后转换为应用布尔值。
	var enabled, once int
	// err 是当前配置扫描错误，缺失统一映射为仓储不存在错误。
	err := queryer.QueryRowContext(ctx, `SELECT enabled, COALESCE(reply_content,''), COALESCE(reply_image_url,''), COALESCE(reply_image_path,''), reply_once FROM default_replies WHERE cookie_id=?`, cookieID).
		Scan(&enabled, &reply.ReplyContent, &reply.ReplyImageURL, &reply.ReplyImagePath, &once)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	reply.Enabled, reply.ReplyOnce = enabled != 0, once != 0
	return &reply, nil
}

// UpdateWithCurrent 在 d 的账号锁内读取 cookieID 配置，调用纯计算 build 后原子写回。
// 未配置时以零值调用 build；build 不能执行外部 I/O 或重入仓储，失败不会写入。
func (d *DefaultReplies) UpdateWithCurrent(ctx context.Context, cookieID string, build func(DefaultReply) (DefaultReply, error)) error {
	// transaction、err 保存已持有账号行锁的事务。
	transaction, err := beginReplyWrite(ctx, d.DB, d.Dialect, cookieID)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	// current、readErr 保存事务中的最新配置，缺失配置可由本次操作创建。
	current, readErr := readDefaultReply(ctx, transaction, cookieID)
	if errors.Is(readErr, ErrNotFound) {
		current = &DefaultReply{}
	} else if readErr != nil {
		return readErr
	}
	// reply、buildErr 是应用校验后的完整配置，拒绝时维持原状态。
	reply, buildErr := build(*current)
	if buildErr != nil {
		return buildErr
	}
	// writeErr 是写入最终配置的数据库错误。
	_, writeErr := transaction.ExecContext(ctx,
		`INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_image_url,reply_image_path,reply_once,updated_at)
		 VALUES (?,?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(d.Dialect, []string{"cookie_id"}, map[string]string{
			"enabled": "EXCLUDED.enabled", "reply_content": "EXCLUDED.reply_content", "reply_image_url": "EXCLUDED.reply_image_url",
			"reply_image_path": "EXCLUDED.reply_image_path", "reply_once": "EXCLUDED.reply_once", "updated_at": "CURRENT_TIMESTAMP",
		}), cookieID, boolToInt(reply.Enabled), reply.ReplyContent, defaultReplyNullableString(reply.ReplyImageURL), defaultReplyNullableString(reply.ReplyImagePath), boolToInt(reply.ReplyOnce))
	if writeErr != nil {
		return writeErr
	}
	return transaction.Commit()
}

// readItemReply 从 queryer 读取 cookieID/itemID 的图文及单会话开关，身份始终来自查询条件。
func readItemReply(ctx context.Context, queryer replyQueryer, cookieID, itemID string) (*ItemReply, error) {
	// reply 保存正文和两种互斥图片来源，历史 NULL 被归一为空串。
	reply := ItemReply{CookieID: cookieID, ItemID: itemID}
	// err 是单条商品配置读取错误。
	err := queryer.QueryRowContext(ctx, `SELECT COALESCE(reply_content,''), COALESCE(reply_image_url,''), COALESCE(reply_image_path,''), reply_once FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID).
		Scan(&reply.ReplyContent, &reply.ReplyImageURL, &reply.ReplyImagePath, &reply.ReplyOnce)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

// UpdateWithCurrent 在 i 的账号锁内读取 itemID 配置，由 build 合并校验后写回。
// 缺失时传入空配置；build 不能改变账号或商品身份、执行外部 I/O 或重入数据库。
func (i *ItemReplies) UpdateWithCurrent(ctx context.Context, cookieID, itemID string, build func(ItemReply) (ItemReply, error)) error {
	// transaction、err 保存防止同账号并发补丁丢字段的事务。
	transaction, err := beginReplyWrite(ctx, i.DB, i.Dialect, cookieID)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	// current、readErr 是持锁后的当前完整配置，缺失时允许创建。
	current, readErr := readItemReply(ctx, transaction, cookieID, itemID)
	if errors.Is(readErr, ErrNotFound) {
		current = &ItemReply{CookieID: cookieID, ItemID: itemID}
	} else if readErr != nil {
		return readErr
	}
	// reply、buildErr 是合并后的配置或应用校验错误。
	reply, buildErr := build(*current)
	if buildErr != nil {
		return buildErr
	}
	// deleteErr 是清理旧记录错误；历史表无唯一键，持锁后按原先删后插语义写入。
	if _, deleteErr := transaction.ExecContext(ctx, `DELETE FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID); deleteErr != nil {
		return deleteErr
	}
	// insertErr 是图文与去重开关的整体写入错误，失败时回滚旧配置删除。
	if _, insertErr := transaction.ExecContext(ctx, `INSERT INTO item_replay (item_id,cookie_id,reply_content,reply_image_url,reply_image_path,reply_once,updated_at) VALUES (?,?,?,?,?,?,CURRENT_TIMESTAMP)`, itemID, cookieID, reply.ReplyContent, defaultReplyNullableString(reply.ReplyImageURL), defaultReplyNullableString(reply.ReplyImagePath), boolToInt(reply.ReplyOnce)); insertErr != nil {
		return insertErr
	}
	return transaction.Commit()
}
