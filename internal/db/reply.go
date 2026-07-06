package db

import (
	"context"
	"database/sql"
	"errors"
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
		 FROM keywords WHERE cookie_id=?`, cookieID)
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

// HasRecord 是否已对该 chat_id 回复过（reply_once 用）。
func (d *DefaultReplies) HasRecord(ctx context.Context, cookieID, chatID string) bool {
	var n int
	err := d.DB.QueryRowContext(ctx,
		`SELECT 1 FROM default_reply_records WHERE cookie_id=? AND chat_id=? LIMIT 1`,
		cookieID, chatID).Scan(&n)
	return err == nil
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
