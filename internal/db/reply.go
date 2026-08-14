package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Keyword 对应 keywords 表。
type Keyword struct {
	Keyword  string
	Reply    string
	ItemID   string
	Type     string // text/image
	ImageURL string
}

// DefaultReply 对应 default_replies 表。
type DefaultReply struct {
	Enabled       bool
	ReplyContent  string
	ReplyImageURL string
	ReplyOnce     bool
}

// DefaultReplySummary 是按账号查询默认回复列表时使用的带账号标识视图。
type DefaultReplySummary struct {
	// CookieID 是默认回复所属账号标识。
	CookieID string
	// Enabled 表示默认回复是否启用。
	Enabled bool
	// ReplyContent 是默认回复文字。
	ReplyContent string
	// ReplyImageURL 是默认回复图片地址。
	ReplyImageURL string
	// ReplyOnce 表示同一聊天是否只发送一次。
	ReplyOnce bool
}

// DefaultReplyRecord 记录 reply_once 消息各部分的投递状态。
type DefaultReplyRecord struct {
	Status    string
	TextSent  bool
	ImageSent bool
}

// ItemReply 对应 item_replay 表（指定商品回复）。
type ItemReply struct {
	ItemID       string
	CookieID     string
	ReplyContent string
}

// Keywords 关键字操作。
type Keywords struct {
	DB      *sql.DB
	Dialect Dialect
}

// AllWithType 取某账号所有关键字（含类型/图片）。
func (k *Keywords) AllWithType(ctx context.Context, cookieID string) ([]Keyword, error) {
	rows, err := k.DB.QueryContext(ctx,
		`SELECT keyword, reply, COALESCE(item_id,''), COALESCE(type,'text'), COALESCE(image_url,'')
			 FROM keywords WHERE cookie_id=? ORDER BY LENGTH(keyword) DESC,id ASC`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Keyword
	for rows.Next() {
		var kw Keyword
		if err := rows.Scan(&kw.Keyword, &kw.Reply, &kw.ItemID, &kw.Type, &kw.ImageURL); err != nil {
			return nil, err
		}
		out = append(out, kw)
	}
	return out, rows.Err()
}

// DefaultReplies 默认回复操作。
type DefaultReplies struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取某账号默认回复设置。不存在返回 ErrNotFound。
func (d *DefaultReplies) Get(ctx context.Context, cookieID string) (*DefaultReply, error) {
	var dr DefaultReply
	var enabled, replyOnce int
	var content, imageURL sql.NullString
	err := d.DB.QueryRowContext(ctx,
		`SELECT enabled, reply_content, reply_image_url, reply_once FROM default_replies WHERE cookie_id=?`,
		cookieID).Scan(&enabled, &content, &imageURL, &replyOnce)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	dr.Enabled = enabled != 0
	dr.ReplyContent = content.String
	dr.ReplyImageURL = imageURL.String
	dr.ReplyOnce = replyOnce != 0
	return &dr, nil
}

// Upsert 保存或覆盖指定账号的默认回复配置。
func (d *DefaultReplies) Upsert(ctx context.Context, cookieID string, reply DefaultReply) error {
	_, err := d.DB.ExecContext(ctx,
		`INSERT INTO default_replies (cookie_id, enabled, reply_content, reply_image_url, reply_once, updated_at)
		 VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(d.Dialect, []string{"cookie_id"}, map[string]string{
			"enabled":         "EXCLUDED.enabled",
			"reply_content":   "EXCLUDED.reply_content",
			"reply_image_url": "EXCLUDED.reply_image_url",
			"reply_once":      "EXCLUDED.reply_once",
			"updated_at":      "CURRENT_TIMESTAMP",
		}),
		cookieID, boolToInt(reply.Enabled), reply.ReplyContent, defaultReplyNullableString(reply.ReplyImageURL), boolToInt(reply.ReplyOnce))
	return err
}

// defaultReplyNullableString 将空图片地址转换为数据库 NULL，保持历史存储语义。
func defaultReplyNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// ListForUser 查询用户所有账号的默认回复配置。
func (d *DefaultReplies) ListForUser(ctx context.Context, userID int64) ([]DefaultReplySummary, error) {
	rows, err := d.DB.QueryContext(ctx, `
		SELECT dr.cookie_id, dr.enabled, COALESCE(dr.reply_content,''), dr.reply_once, COALESCE(dr.reply_image_url,'')
		  FROM default_replies dr JOIN cookies c ON c.id=dr.cookie_id WHERE c.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DefaultReplySummary
	for rows.Next() {
		var item DefaultReplySummary
		var enabled, replyOnce int
		if err := rows.Scan(&item.CookieID, &enabled, &item.ReplyContent, &replyOnce, &item.ReplyImageURL); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		item.ReplyOnce = replyOnce != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// Delete 删除指定账号的默认回复配置。
func (d *DefaultReplies) Delete(ctx context.Context, cookieID string) error {
	_, err := d.DB.ExecContext(ctx, `DELETE FROM default_replies WHERE cookie_id=?`, cookieID)
	return err
}

// ClearRecords 清空指定账号的默认回复投递记录。
func (d *DefaultReplies) ClearRecords(ctx context.Context, cookieID string) error {
	_, err := d.DB.ExecContext(ctx, `DELETE FROM default_reply_records WHERE cookie_id=?`, cookieID)
	return err
}

// HasRecord 是否已对该 chat_id 回复过（reply_once 用）。
func (d *DefaultReplies) HasRecord(ctx context.Context, cookieID, chatID string) bool {
	var n int
	err := d.DB.QueryRowContext(ctx,
		`SELECT 1 FROM default_reply_records WHERE cookie_id=? AND chat_id=? AND status='sent' LIMIT 1`,
		cookieID, chatID).Scan(&n)
	return err == nil
}

// ClaimRecord 原子领取一次默认回复投递。新记录初始化为 pending；失败记录允许继续
// 投递尚未成功的部分；pending/sent 记录会阻止并发重复发送。
func (d *DefaultReplies) ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (DefaultReplyRecord, bool, error) {
	now := time.Now().UTC().Unix()
	leaseExpiresAt := now + int64((5*time.Minute)/time.Second)
	query := dialectInsertIgnorePrefix(d.Dialect) + ` INTO default_reply_records
		(cookie_id,chat_id,status,text_sent,image_sent,last_error,lease_expires_at,updated_at)
		VALUES (?,?, 'pending', ?, ?, '', ?, CURRENT_TIMESTAMP)` + dialectInsertIgnore(d.Dialect, []string{"cookie_id", "chat_id"})
	res, err := d.DB.ExecContext(ctx, query, cookieID, chatID, boolToInt(!needsText), boolToInt(!needsImage), leaseExpiresAt)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		return DefaultReplyRecord{Status: "pending", TextSent: !needsText, ImageSent: !needsImage}, true, nil
	}

	record, err := d.Record(ctx, cookieID, chatID)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if record.Status == "sent" {
		return record, false, nil
	}
	// pending 是发送任务的短租约。进程崩溃或强制退出后，过期租约必须可被
	// 新实例接管，否则该会话会永久失去默认回复。
	res, err = d.DB.ExecContext(ctx, `UPDATE default_reply_records
		SET status='pending',last_error='',lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?
		  AND (status='failed' OR (status='pending' AND lease_expires_at<?))`,
		leaseExpiresAt, cookieID, chatID, now)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	affected, _ := res.RowsAffected()
	return record, affected > 0, nil
}

// Record 查询一次默认回复的投递状态。
func (d *DefaultReplies) Record(ctx context.Context, cookieID, chatID string) (DefaultReplyRecord, error) {
	var record DefaultReplyRecord
	var textSent, imageSent int
	err := d.DB.QueryRowContext(ctx, `SELECT status,text_sent,image_sent
		FROM default_reply_records WHERE cookie_id=? AND chat_id=?`, cookieID, chatID).
		Scan(&record.Status, &textSent, &imageSent)
	record.TextSent = textSent != 0
	record.ImageSent = imageSent != 0
	return record, err
}

// MarkPartSent 标记图片或文字已经成功投递。
func (d *DefaultReplies) MarkPartSent(ctx context.Context, cookieID, chatID, part string) error {
	column := ""
	switch part {
	case "text":
		column = "text_sent"
	case "image":
		column = "image_sent"
	default:
		return errors.New("未知默认回复部分")
	}
	_, err := d.DB.ExecContext(ctx, `UPDATE default_reply_records SET `+column+`=1,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

func (d *DefaultReplies) MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error {
	_, err := d.DB.ExecContext(ctx, `UPDATE default_reply_records
		SET status='failed',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=?`, message, cookieID, chatID)
	return err
}

func (d *DefaultReplies) MarkRecordSent(ctx context.Context, cookieID, chatID string) error {
	_, err := d.DB.ExecContext(ctx, `UPDATE default_reply_records
		SET status='sent',last_error='',lease_expires_at=0,replied_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// AddRecord 记录已回复（reply_once 防重复）。
func (d *DefaultReplies) AddRecord(ctx context.Context, cookieID, chatID string) error {
	_, err := d.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(d.Dialect)+` INTO default_reply_records (cookie_id, chat_id) VALUES (?, ?)`+dialectInsertIgnore(d.Dialect, []string{"cookie_id", "chat_id"}),
		cookieID, chatID)
	return err
}

// ItemReplies 指定商品回复操作。
type ItemReplies struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取某账号某商品的指定回复。
func (i *ItemReplies) Get(ctx context.Context, cookieID, itemID string) (*ItemReply, error) {
	var ir ItemReply
	var content sql.NullString
	err := i.DB.QueryRowContext(ctx,
		`SELECT item_id, cookie_id, reply_content FROM item_replay WHERE cookie_id=? AND item_id=?`,
		cookieID, itemID).Scan(&ir.ItemID, &ir.CookieID, &content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ir.ReplyContent = content.String
	return &ir, nil
}
