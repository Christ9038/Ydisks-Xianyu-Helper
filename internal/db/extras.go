package db

import (
	"context"
	"database/sql"
	"errors"
)

// ---- 关键字 CRUD ----

// KeywordRow keywords 表完整行（含自增 id，用于按索引删除）。
type KeywordRow struct {
	ID       int64
	CookieID string
	Keyword  string
	Reply    string
	ItemID   string
	Type     string
	ImageURL string
}

// AllRows 取某账号所有关键字（含 id）。
func (k *Keywords) AllRows(ctx context.Context, cookieID string) ([]KeywordRow, error) {
	rows, err := k.DB.QueryContext(ctx,
		`SELECT id, keyword, reply, COALESCE(item_id,''), COALESCE(type,'text'), COALESCE(image_url,'')
		 FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeywordRow
	for rows.Next() {
		var r KeywordRow
		if err := rows.Scan(&r.ID, &r.Keyword, &r.Reply, &r.ItemID, &r.Type, &r.ImageURL); err != nil {
			return nil, err
		}
		r.CookieID = cookieID
		out = append(out, r)
	}
	return out, rows.Err()
}

// Add 添加关键字。
func (k *Keywords) Add(ctx context.Context, cookieID, keyword, reply, itemID, kwType, imageURL string) (int64, error) {
	if kwType == "" {
		kwType = "text"
	}
	return insertReturningID(ctx, k.DB, k.Dialect,
		`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url) VALUES (?,?,?,?,?,?)`,
		cookieID, keyword, reply, nullable(itemID), kwType, nullable(imageURL))
}

// ReplaceForCookie 覆盖某账号的全部关键词。
func (k *Keywords) ReplaceForCookie(ctx context.Context, cookieID string, rows []KeywordRow) error {
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	for _, row := range rows {
		kwType := row.Type
		if kwType == "" {
			kwType = "text"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url) VALUES (?,?,?,?,?,?)`,
			cookieID, row.Keyword, row.Reply, nullable(row.ItemID), kwType, nullable(row.ImageURL)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteByIndex 按索引（0-based，按 id 顺序）删除某账号的一个关键字。
func (k *Keywords) DeleteByIndex(ctx context.Context, cookieID string, index int) error {
	rows, err := k.DB.QueryContext(ctx,
		`SELECT id FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if index < 0 || index >= len(ids) {
		return ErrNotFound
	}
	_, err = k.DB.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND cookie_id=?`, ids[index], cookieID)
	return err
}

// ---- 指定商品回复 (item_replay) CRUD ----

// ItemReplies 已在 reply.go 定义 Get。补 Set/Delete。

// Set 设置指定商品回复。
func (i *ItemReplies) Set(ctx context.Context, cookieID, itemID, content string) error {
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 先删后插，跨 SQLite/MySQL/Postgres 一致（item_replay 无自然唯一键）。
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO item_replay (item_id, cookie_id, reply_content, updated_at)
		 VALUES (?,?,?,CURRENT_TIMESTAMP)`, itemID, cookieID, content); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete 删除指定商品回复。
func (i *ItemReplies) Delete(ctx context.Context, cookieID, itemID string) error {
	_, err := i.DB.ExecContext(ctx,
		`DELETE FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID)
	return err
}

// AllForUser 取某账号所有指定商品回复。
func (i *ItemReplies) AllForUser(ctx context.Context, cookieID string) ([]ItemReply, error) {
	rows, err := i.DB.QueryContext(ctx,
		`SELECT item_id, cookie_id, reply_content FROM item_replay WHERE cookie_id=?`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemReply
	for rows.Next() {
		var r ItemReply
		if err := rows.Scan(&r.ItemID, &r.CookieID, &r.ReplyContent); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- 通知渠道 CRUD ----

// NotificationChannelRow 通知渠道完整行（含 id）。
type NotificationChannelRow struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Config     string `json:"config"`
	EventTypes string `json:"event_types,omitempty"`
	Enabled    bool   `json:"enabled"`
	UserID     int64  `json:"user_id,omitempty"`
}

// AllChannelsForUser 取某用户全部通知渠道。
func (n *Notifications) AllChannelsForUser(ctx context.Context, userID int64) ([]NotificationChannelRow, error) {
	rows, err := n.DB.QueryContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1) FROM notification_channels
		 WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationChannelRow
	for rows.Next() {
		var c NotificationChannelRow
		var enabled int
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &enabled, &c.UserID); err != nil {
			return nil, err
		}
		c.Enabled = enabled != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateChannel 创建通知渠道。
func (n *Notifications) CreateChannel(ctx context.Context, c *NotificationChannelRow) (int64, error) {
	return insertReturningID(ctx, n.DB, n.Dialect,
		`INSERT INTO notification_channels (name, type, config, event_types, enabled, user_id) VALUES (?,?,?,?,?,?)`,
		c.Name, c.Type, c.Config, c.EventTypes, boolToInt(c.Enabled), c.UserID)
}

// UpdateChannel 更新通知渠道。
func (n *Notifications) UpdateChannel(ctx context.Context, c *NotificationChannelRow) error {
	_, err := n.DB.ExecContext(ctx,
		`UPDATE notification_channels SET name=?, type=?, config=?, event_types=?, enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.Name, c.Type, c.Config, c.EventTypes, boolToInt(c.Enabled), c.ID)
	return err
}

// GetChannelRowForUser 按用户取单个通知渠道。未找到返回 nil。
func (n *Notifications) GetChannelRowForUser(ctx context.Context, id, userID int64) (*NotificationChannelRow, error) {
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1)
		   FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	var c NotificationChannelRow
	var enabled int
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &enabled, &c.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c.Enabled = enabled != 0
	return &c, nil
}

// UpdateChannelForUser 更新指定用户拥有的通知渠道。
func (n *Notifications) UpdateChannelForUser(ctx context.Context, c *NotificationChannelRow, userID int64) error {
	res, err := n.DB.ExecContext(ctx,
		`UPDATE notification_channels
		    SET name=?, type=?, config=?, event_types=?, enabled=?, updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND user_id=?`,
		c.Name, c.Type, c.Config, c.EventTypes, boolToInt(c.Enabled), c.ID, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChannel 删除通知渠道。
func (n *Notifications) DeleteChannel(ctx context.Context, id int64) error {
	_, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	return err
}

// DeleteChannelForUser 删除指定用户拥有的通知渠道。
func (n *Notifications) DeleteChannelForUser(ctx context.Context, id, userID int64) error {
	res, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetChannel 按 ID 取单个通知渠道（含 config）。未找到返回 nil。
func (n *Notifications) GetChannel(ctx context.Context, id int64) (*NotificationChannel, error) {
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,'') FROM notification_channels WHERE id=?`, id)
	var c NotificationChannel
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// AccountBindings 取某账号已绑定的通知渠道 ID 列表。
func (n *Notifications) AccountBindings(ctx context.Context, cookieID string) ([]int64, error) {
	rows, err := n.DB.QueryContext(ctx,
		`SELECT mn.channel_id
		   FROM message_notifications mn
		   JOIN cookies c ON c.id=mn.cookie_id
		   JOIN notification_channels nc ON nc.id=mn.channel_id AND nc.user_id=c.user_id
		  WHERE mn.cookie_id=? AND mn.enabled=1`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetBindings 设置某账号的通知渠道绑定（覆盖式）。
func (n *Notifications) SetBindings(ctx context.Context, cookieID string, channelIDs []int64) error {
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	for _, channelID := range channelIDs {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`,
			channelID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrForbidden
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_notifications WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	for _, cid := range channelIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES (?,?,1)`,
			cookieID, cid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- 系统设置 ----

// SystemSettings 系统设置操作。
type SystemSettings struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取单项设置。
func (s *SystemSettings) Get(ctx context.Context, key string) (string, error) {
	var v string
	keyCol := dialectQuote(s.Dialect, "key")
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// All 取全部设置（key→value）。
func (s *SystemSettings) All(ctx context.Context) (map[string]string, error) {
	keyCol := dialectQuote(s.Dialect, "key")
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyCol+`, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// Set 设置单项。
func (s *SystemSettings) Set(ctx context.Context, key, value string) error {
	keyCol := dialectQuote(s.Dialect, "key")
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO system_settings (`+keyCol+`, value, updated_at) VALUES (?,?,CURRENT_TIMESTAMP)`+
			dialectUpsert(s.Dialect, []string{keyCol}, map[string]string{
				"value":      "EXCLUDED.value",
				"updated_at": "CURRENT_TIMESTAMP",
			}),
		key, value)
	return err
}

// PublicSystemKeys 是公开设置键白名单（前端登录页等无需登录可读）。
var PublicSystemKeys = map[string]bool{
	"theme_color": true,
}

// Public 取公开设置子集。
func (s *SystemSettings) Public(ctx context.Context) (map[string]string, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for k := range PublicSystemKeys {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// ---- 物品 (item_info) CRUD ----

// ItemInfoRow item_info 完整行。
type ItemInfoRow struct {
	ID                    int64
	CookieID              string
	ItemID                string
	ItemTitle             string
	ItemDescription       string
	ItemCategory          string
	ItemPrice             string
	ItemDetail            string
	IsMultiSpec           bool
	MultiQuantityDelivery bool
}

// AllForCookie 取某账号全部商品。
func (i *Items) AllForCookie(ctx context.Context, cookieID string) ([]ItemInfoRow, error) {
	rows, err := i.DB.QueryContext(ctx,
		`SELECT id, cookie_id, item_id, COALESCE(item_title,''), COALESCE(item_description,''),
		        COALESCE(item_category,''), COALESCE(item_price,''), COALESCE(item_detail,''),
		        is_multi_spec, COALESCE(multi_quantity_delivery,0)
		 FROM item_info WHERE cookie_id=? ORDER BY id DESC`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemInfoRow
	for rows.Next() {
		var r ItemInfoRow
		var isMulti, multiQty int
		if err := rows.Scan(&r.ID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.ItemDescription,
			&r.ItemCategory, &r.ItemPrice, &r.ItemDetail, &isMulti, &multiQty); err != nil {
			return nil, err
		}
		r.IsMultiSpec = isMulti != 0
		r.MultiQuantityDelivery = multiQty != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// Upsert 插入或更新商品信息。
func (i *Items) Upsert(ctx context.Context, r *ItemInfoRow) error {
	_, err := i.DB.ExecContext(ctx,
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_description,
		    item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+
			dialectUpsert(i.Dialect, []string{"cookie_id", "item_id"}, map[string]string{
				"item_title":              "EXCLUDED.item_title",
				"item_description":        "EXCLUDED.item_description",
				"item_category":           "EXCLUDED.item_category",
				"item_price":              "EXCLUDED.item_price",
				"item_detail":             "EXCLUDED.item_detail",
				"is_multi_spec":           "EXCLUDED.is_multi_spec",
				"multi_quantity_delivery": "EXCLUDED.multi_quantity_delivery",
				"updated_at":              "CURRENT_TIMESTAMP",
			}),
		r.CookieID, r.ItemID, nullable(r.ItemTitle), nullable(r.ItemDescription),
		nullable(r.ItemCategory), nullable(r.ItemPrice), nullable(r.ItemDetail),
		boolToInt(r.IsMultiSpec), boolToInt(r.MultiQuantityDelivery))
	return err
}

// UpsertBasic 插入或补全商品基础信息，不覆盖已有的多规格/多数量发货设置。
func (i *Items) UpsertBasic(ctx context.Context, r *ItemInfoRow) error {
	// 三种数据库的条件 upsert：非空才覆盖，空值保留旧值。
	// SQLite/Postgres 用 EXCLUDED.col 引用插入值；MySQL 用 VALUES(col)。
	var conflictClause string
	switch i.Dialect {
	case DialectMySQL:
		conflictClause = ` ON DUPLICATE KEY UPDATE
		   item_title=CASE WHEN VALUES(item_title) IS NOT NULL AND VALUES(item_title) != '' THEN VALUES(item_title) ELSE item_info.item_title END,
		   item_description=CASE WHEN VALUES(item_description) IS NOT NULL AND VALUES(item_description) != '' THEN VALUES(item_description) ELSE item_info.item_description END,
		   item_category=CASE WHEN VALUES(item_category) IS NOT NULL AND VALUES(item_category) != '' THEN VALUES(item_category) ELSE item_info.item_category END,
		   item_price=CASE WHEN VALUES(item_price) IS NOT NULL AND VALUES(item_price) != '' THEN VALUES(item_price) ELSE item_info.item_price END,
		   item_detail=CASE WHEN VALUES(item_detail) IS NOT NULL AND VALUES(item_detail) != '' THEN VALUES(item_detail) ELSE item_info.item_detail END,
		   updated_at=CURRENT_TIMESTAMP`
	default:
		conflictClause = ` ON CONFLICT(cookie_id, item_id) DO UPDATE SET
		   item_title=CASE WHEN EXCLUDED.item_title IS NOT NULL AND EXCLUDED.item_title != '' THEN EXCLUDED.item_title ELSE item_info.item_title END,
		   item_description=CASE WHEN EXCLUDED.item_description IS NOT NULL AND EXCLUDED.item_description != '' THEN EXCLUDED.item_description ELSE item_info.item_description END,
		   item_category=CASE WHEN EXCLUDED.item_category IS NOT NULL AND EXCLUDED.item_category != '' THEN EXCLUDED.item_category ELSE item_info.item_category END,
		   item_price=CASE WHEN EXCLUDED.item_price IS NOT NULL AND EXCLUDED.item_price != '' THEN EXCLUDED.item_price ELSE item_info.item_price END,
		   item_detail=CASE WHEN EXCLUDED.item_detail IS NOT NULL AND EXCLUDED.item_detail != '' THEN EXCLUDED.item_detail ELSE item_info.item_detail END,
		   updated_at=CURRENT_TIMESTAMP`
	}
	_, err := i.DB.ExecContext(ctx,
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_description,
		    item_category, item_price, item_detail, updated_at)
		 VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+conflictClause,
		r.CookieID, r.ItemID, nullable(r.ItemTitle), nullable(r.ItemDescription),
		nullable(r.ItemCategory), nullable(r.ItemPrice), nullable(r.ItemDetail))
	return err
}

// Delete 删除商品。
func (i *Items) Delete(ctx context.Context, cookieID, itemID string) error {
	_, err := i.DB.ExecContext(ctx, `DELETE FROM item_info WHERE cookie_id=? AND item_id=?`, cookieID, itemID)
	return err
}

// SetMultiSpec 设置多规格开关。
func (i *Items) SetMultiSpec(ctx context.Context, cookieID, itemID string, on bool) error {
	_, err := i.DB.ExecContext(ctx,
		`UPDATE item_info SET is_multi_spec=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND item_id=?`,
		boolToInt(on), cookieID, itemID)
	return err
}

// SetMultiQuantity 设置多数量发货开关。
func (i *Items) SetMultiQuantity(ctx context.Context, cookieID, itemID string, on bool) error {
	_, err := i.DB.ExecContext(ctx,
		`UPDATE item_info SET multi_quantity_delivery=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND item_id=?`,
		boolToInt(on), cookieID, itemID)
	return err
}
