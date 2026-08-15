package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ---- 关键字 CRUD ----

// KeywordRow keywords 表完整行（含自增 id，用于按索引删除）。
type KeywordRow struct {
	ID       int64 `json:"id"`
	CookieID string
	Keyword  string
	Reply    string
	ItemID   string
	Type     string
	ImageURL string
}

// UpdateByID 更新ByID。
func (k *Keywords) UpdateByID(ctx context.Context, row KeywordRow) error {
	// kwType 保存kw类型，供当前处理流程使用
	kwType := row.Type
	if kwType == "" {
		kwType = "text"
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := k.DB.ExecContext(ctx, `UPDATE keywords
		SET keyword=?,reply=?,item_id=?,type=?,image_url=?
		WHERE id=? AND cookie_id=?`,
		row.Keyword, row.Reply, nullable(row.ItemID), kwType, nullable(row.ImageURL), row.ID, row.CookieID)
	if err != nil {
		return err
	}
	if // affected、err 保存affected、err，供当前处理流程使用
	affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByID 删除ByID。
func (k *Keywords) DeleteByID(ctx context.Context, cookieID string, id int64) error {
	// res、err 保存res、err，供当前处理流程使用
	res, err := k.DB.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND cookie_id=?`, id, cookieID)
	if err != nil {
		return err
	}
	if // affected、err 保存affected、err，供当前处理流程使用
	affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

// AllRows 取某账号所有关键字（含 id）。
func (k *Keywords) AllRows(ctx context.Context, cookieID string) ([]KeywordRow, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := k.DB.QueryContext(ctx,
		`SELECT id, keyword, reply, COALESCE(item_id,''), COALESCE(type,'text'), COALESCE(image_url,'')
		 FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存out，供当前处理流程使用
	var out []KeywordRow
	for rows.Next() {
		// r 保存r，供当前处理流程使用
		var r KeywordRow
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&r.ID, &r.Keyword, &r.Reply, &r.ItemID, &r.Type, &r.ImageURL); err != nil {
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
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		// kwType 保存kw类型，供当前处理流程使用
		kwType := row.Type
		if kwType == "" {
			kwType = "text"
		}
		if // err 保存err，供当前处理流程使用
		_, err := tx.ExecContext(ctx,
			`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url) VALUES (?,?,?,?,?,?)`,
			cookieID, row.Keyword, row.Reply, nullable(row.ItemID), kwType, nullable(row.ImageURL)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteByIndex 按索引（0-based，按 id 顺序）删除某账号的一个关键字。
func (k *Keywords) DeleteByIndex(ctx context.Context, cookieID string, index int) error {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := k.DB.QueryContext(ctx,
		`SELECT id FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return err
	}
	defer rows.Close()
	// ids 保存ids，供当前处理流程使用
	var ids []int64
	for rows.Next() {
		// id 保存标识，供当前处理流程使用
		var id int64
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&id); err != nil {
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
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 先删后插，跨 SQLite/MySQL/Postgres 一致（item_replay 无自然唯一键）。
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID); err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx,
		`INSERT INTO item_replay (item_id, cookie_id, reply_content, updated_at)
		 VALUES (?,?,?,CURRENT_TIMESTAMP)`, itemID, cookieID, content); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete 删除指定商品回复。
func (i *ItemReplies) Delete(ctx context.Context, cookieID, itemID string) error {
	// err 保存err，供当前处理流程使用
	_, err := i.DB.ExecContext(ctx,
		`DELETE FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID)
	return err
}

// AllForUser 取某账号所有指定商品回复。
func (i *ItemReplies) AllForUser(ctx context.Context, cookieID string) ([]ItemReply, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := i.DB.QueryContext(ctx,
		`SELECT item_id, cookie_id, reply_content FROM item_replay WHERE cookie_id=?`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存out，供当前处理流程使用
	var out []ItemReply
	for rows.Next() {
		// r 保存r，供当前处理流程使用
		var r ItemReply
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&r.ItemID, &r.CookieID, &r.ReplyContent); err != nil {
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
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := n.DB.QueryContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1) FROM notification_channels
		 WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存out，供当前处理流程使用
	var out []NotificationChannelRow
	for rows.Next() {
		// c 保存c，供当前处理流程使用
		var c NotificationChannelRow
		// enabled 保存启用状态，供当前处理流程使用
		var enabled int
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &enabled, &c.UserID); err != nil {
			return nil, err
		}
		c.Config, err = n.codec.decrypt("notification-config", strconv.FormatInt(c.UserID, 10), c.Config)
		if err != nil {
			return nil, err
		}
		c.Enabled = enabled != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateChannel 创建通知渠道。
func (n *Notifications) CreateChannel(ctx context.Context, c *NotificationChannelRow) (int64, error) {
	// config、err 保存config、err，供当前处理流程使用
	config, err := n.codec.encrypt("notification-config", strconv.FormatInt(c.UserID, 10), c.Config)
	if err != nil {
		return 0, err
	}
	return insertReturningID(ctx, n.DB, n.Dialect,
		`INSERT INTO notification_channels (name, type, config, event_types, enabled, user_id) VALUES (?,?,?,?,?,?)`,
		c.Name, c.Type, config, c.EventTypes, boolToInt(c.Enabled), c.UserID)
}

// UpdateChannel 更新通知渠道。
func (n *Notifications) UpdateChannel(ctx context.Context, c *NotificationChannelRow) error {
	if c.UserID == 0 {
		if // err 保存err，供当前处理流程使用
		err := n.DB.QueryRowContext(ctx, `SELECT COALESCE(user_id,1) FROM notification_channels WHERE id=?`, c.ID).Scan(&c.UserID); err != nil {
			return err
		}
	}
	// config、err 保存config、err，供当前处理流程使用
	config, err := n.codec.encrypt("notification-config", strconv.FormatInt(c.UserID, 10), c.Config)
	if err != nil {
		return err
	}
	_, err = n.DB.ExecContext(ctx,
		`UPDATE notification_channels SET name=?, type=?, config=?, event_types=?, enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.Name, c.Type, config, c.EventTypes, boolToInt(c.Enabled), c.ID)
	return err
}

// GetChannelRowForUser 按用户取单个通知渠道。未找到返回 nil。
func (n *Notifications) GetChannelRowForUser(ctx context.Context, id, userID int64) (*NotificationChannelRow, error) {
	// row 保存row，供当前处理流程使用
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1)
		   FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	// c 保存c，供当前处理流程使用
	var c NotificationChannelRow
	// enabled 保存启用状态，供当前处理流程使用
	var enabled int
	if // err 保存err，供当前处理流程使用
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &enabled, &c.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c.Enabled = enabled != 0
	// config、err 保存config、err，供当前处理流程使用
	config, err := n.codec.decrypt("notification-config", strconv.FormatInt(c.UserID, 10), c.Config)
	if err != nil {
		return nil, err
	}
	c.Config = config
	return &c, nil
}

// UpdateChannelForUser 更新指定用户拥有的通知渠道。
func (n *Notifications) UpdateChannelForUser(ctx context.Context, c *NotificationChannelRow, userID int64) error {
	// config、err 保存config、err，供当前处理流程使用
	config, err := n.codec.encrypt("notification-config", strconv.FormatInt(userID, 10), c.Config)
	if err != nil {
		return err
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := n.DB.ExecContext(ctx,
		`UPDATE notification_channels
		    SET name=?, type=?, config=?, event_types=?, enabled=?, updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND user_id=?`,
		c.Name, c.Type, config, c.EventTypes, boolToInt(c.Enabled), c.ID, userID)
	if err != nil {
		return err
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChannel 删除通知渠道。
func (n *Notifications) DeleteChannel(ctx context.Context, id int64) error {
	// err 保存err，供当前处理流程使用
	_, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	return err
}

// DeleteChannelForUser 删除指定用户拥有的通知渠道。
func (n *Notifications) DeleteChannelForUser(ctx context.Context, id, userID int64) error {
	// res、err 保存res、err，供当前处理流程使用
	res, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetChannel 按 ID 取单个通知渠道（含 config）。未找到返回 nil。
func (n *Notifications) GetChannel(ctx context.Context, id int64) (*NotificationChannel, error) {
	// row 保存row，供当前处理流程使用
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), COALESCE(user_id,1) FROM notification_channels WHERE id=?`, id)
	// c 保存c，供当前处理流程使用
	var c NotificationChannel
	// userID 保存用户ID，供当前处理流程使用
	var userID int64
	if // err 保存err，供当前处理流程使用
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// config、err 保存config、err，供当前处理流程使用
	config, err := n.codec.decrypt("notification-config", strconv.FormatInt(userID, 10), c.Config)
	if err != nil {
		return nil, err
	}
	c.Config = config
	return &c, nil
}

// AccountBindings 取某账号已绑定的通知渠道 ID 列表。
func (n *Notifications) AccountBindings(ctx context.Context, cookieID string) ([]int64, error) {
	// rows、err 保存rows、err，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
	var out []int64
	for rows.Next() {
		// id 保存标识，供当前处理流程使用
		var id int64
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetBindings 设置某账号的通知渠道绑定（覆盖式）。
func (n *Notifications) SetBindings(ctx context.Context, cookieID string, channelIDs []int64) error {
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// userID 保存用户ID，供当前处理流程使用
	var userID int64
	if // err 保存err，供当前处理流程使用
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// channelID 表示当前遍历过程中的渠道ID
	for _, channelID := range channelIDs {
		// exists 保存exists，供当前处理流程使用
		var exists bool
		if // err 保存err，供当前处理流程使用
		err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`,
			channelID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrForbidden
		}
	}
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `DELETE FROM message_notifications WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	// cid 表示当前遍历过程中的cid
	for _, cid := range channelIDs {
		if // err 保存err，供当前处理流程使用
		_, err := tx.ExecContext(ctx,
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
	codec   *secretCodec
}

// SensitiveSettingChange 描述敏感系统设置的显式变更命令。
// retain 保留现有密文，replace 写入 Value，clear 删除现有密文。
type SensitiveSettingChange struct {
	// Action 是 retain、replace 或 clear 之一。
	Action string
	// Value 是 replace 操作要加密保存的新秘密。
	Value string
}

// Get 取单项设置。
func (s *SystemSettings) Get(ctx context.Context, key string) (string, error) {
	// v 保存v，供当前处理流程使用
	var v string
	// keyCol 保存keyCol，供当前处理流程使用
	keyCol := dialectQuote(s.Dialect, "key")
	// err 保存err，供当前处理流程使用
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if isSensitiveSettingKey(key) {
		return s.codec.decrypt("system-setting", key, v)
	}
	return v, nil
}

// All 取全部设置（key→value）。
func (s *SystemSettings) All(ctx context.Context) (map[string]string, error) {
	// keyCol 保存keyCol，供当前处理流程使用
	keyCol := dialectQuote(s.Dialect, "key")
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyCol+`, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// m 保存m，供当前处理流程使用
	m := make(map[string]string)
	for rows.Next() {
		// k、v 保存k、v，供当前处理流程使用
		var k, v string
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if isSensitiveSettingKey(k) {
			v, err = s.codec.decrypt("system-setting", k, v)
			if err != nil {
				return nil, err
			}
		}
		m[k] = v
	}
	return m, rows.Err()
}

// Set 设置单项。
func (s *SystemSettings) Set(ctx context.Context, key, value string) error {
	if isSensitiveSettingKey(key) {
		if strings.TrimSpace(value) == "" {
			// keyCol 是当前数据库方言下的设置键列名。
			keyCol := dialectQuote(s.Dialect, "key")
			// err 是删除敏感设置时返回的数据库错误。
			_, err := s.DB.ExecContext(ctx, `DELETE FROM system_settings WHERE `+keyCol+`=?`, key)
			return err
		}
		// encrypted、err 保存encrypted、err，供当前处理流程使用
		encrypted, err := s.codec.encrypt("system-setting", key, value)
		if err != nil {
			return err
		}
		value = encrypted
	}
	// keyCol 保存keyCol，供当前处理流程使用
	keyCol := dialectQuote(s.Dialect, "key")
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO system_settings (`+keyCol+`, value, updated_at) VALUES (?,?,CURRENT_TIMESTAMP)`+
			dialectUpsert(s.Dialect, []string{keyCol}, map[string]string{
				"value":      "EXCLUDED.value",
				"updated_at": "CURRENT_TIMESTAMP",
			}),
		key, value)
	return err
}

// SetMany 在单个事务中原子保存多项设置。
func (s *SystemSettings) SetMany(ctx context.Context, values map[string]string) error {
	// regular 保存普通设置，避免敏感明文进入 ApplyChanges 的普通值参数。
	regular := make(map[string]string, len(values))
	// secrets 保存兼容旧调用方转换出的显式敏感命令。
	secrets := make(map[string]SensitiveSettingChange)
	// key 表示当前兼容设置的键。
	// value 表示当前兼容设置的值。
	for key, value := range values {
		if !isSensitiveSettingKey(key) {
			regular[key] = value
			continue
		}
		if strings.TrimSpace(value) == "" {
			secrets[key] = SensitiveSettingChange{Action: "clear"}
		} else {
			secrets[key] = SensitiveSettingChange{Action: "replace", Value: value}
		}
	}
	return s.ApplyChanges(ctx, regular, secrets)
}

// ApplyChanges 原子保存普通设置和敏感设置命令，避免把敏感明文放入响应模型。
func (s *SystemSettings) ApplyChanges(ctx context.Context, values map[string]string, secrets map[string]SensitiveSettingChange) error {
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// key、value 表示当前遍历过程中的key、value
	for key, value := range values {
		if isSensitiveSettingKey(key) {
			return fmt.Errorf("敏感设置 %q 必须通过显式变更命令提交", key)
		}
		if // err 保存err，供当前处理流程使用
		err := upsertSystemSetting(ctx, tx, s.Dialect, key, value); err != nil {
			return err
		}
	}
	// key 表示当前敏感设置命令的键。
	// change 表示当前敏感设置的变更命令。
	for key, change := range secrets {
		if !isSensitiveSettingKey(key) {
			return fmt.Errorf("设置 %q 不是敏感设置", key)
		}
		switch change.Action {
		case "retain":
			continue
		case "clear":
			// keyCol 是当前数据库方言下的设置键列名。
			keyCol := dialectQuote(s.Dialect, "key")
			// err 是删除敏感设置时返回的数据库错误。
			if _, err := tx.ExecContext(ctx, `DELETE FROM system_settings WHERE `+keyCol+`=?`, key); err != nil {
				return err
			}
		case "replace":
			if strings.TrimSpace(change.Value) == "" {
				return fmt.Errorf("敏感设置 %q 的 replace 值不能为空", key)
			}
			// encrypted 是加密后的敏感设置密文。
			// err 是敏感设置加密错误。
			encrypted, err := s.codec.encrypt("system-setting", key, change.Value)
			if err != nil {
				return err
			}
			// err 是敏感密文写入错误。
			if err := upsertSystemSetting(ctx, tx, s.Dialect, key, encrypted); err != nil {
				return err
			}
		default:
			return fmt.Errorf("敏感设置 %q 的变更命令无效", key)
		}
	}
	return tx.Commit()
}

// upsertSystemSetting 在事务内写入一项已经完成敏感处理的设置值。
func upsertSystemSetting(ctx context.Context, tx *sql.Tx, dialect Dialect, key, value string) error {
	// keyCol 保存当前数据库方言下的设置键列名。
	keyCol := dialectQuote(dialect, "key")
	// query 保存当前数据库方言下的设置 upsert 语句。
	query := `INSERT INTO system_settings (` + keyCol + `, value, updated_at) VALUES (?,?,CURRENT_TIMESTAMP)` +
		dialectUpsert(dialect, []string{keyCol}, map[string]string{
			"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP",
		})
	// err 保存设置写入错误。
	_, err := tx.ExecContext(ctx, query, key, value)
	return err
}

// Redacted 返回可供管理端展示的系统设置，并以 *_configured 标记敏感值是否已配置。
// 该方法只读取数据库中的原始值，不解密敏感配置，确保秘密不会进入 HTTP 响应或前端状态。
func (s *SystemSettings) Redacted(ctx context.Context) (map[string]string, error) {
	// keyCol 是当前数据库方言下的设置键列名。
	keyCol := dialectQuote(s.Dialect, "key")
	// rows 是系统设置原始值查询结果集。
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyCol+`, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 是不含敏感明文的管理端设置响应。
	result := make(map[string]string)
	for rows.Next() {
		// key、value 是数据库返回的设置键和值。
		var key, value string
		// err 是扫描设置行时返回的数据库错误。
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if isSensitiveSettingKey(key) {
			if strings.TrimSpace(value) != "" {
				result[key+"_configured"] = "true"
			}
			continue
		}
		result[key] = value
	}
	return result, rows.Err()
}

// PublicSystemKeys 是公开设置键白名单（前端登录页等无需登录可读）。
var PublicSystemKeys = map[string]bool{
	"theme_color": true,
}

// Public 取公开设置子集。
func (s *SystemSettings) Public(ctx context.Context) (map[string]string, error) {
	// all、err 保存all、err，供当前处理流程使用
	all, err := s.Redacted(ctx)
	if err != nil {
		return nil, err
	}
	// out 保存out，供当前处理流程使用
	out := make(map[string]string)
	// k 表示当前遍历过程中的k
	for k := range PublicSystemKeys {
		if // v、ok 保存v、ok，供当前处理流程使用
		v, ok := all[k]; ok {
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

// ItemSyncResult 是一次远端商品全集同步的结果。
type ItemSyncResult struct {
	Saved   int
	Deleted int
}

// AllForCookie 取某账号全部商品。
func (i *Items) AllForCookie(ctx context.Context, cookieID string) ([]ItemInfoRow, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := i.DB.QueryContext(ctx,
		`SELECT id, cookie_id, item_id, COALESCE(item_title,''), COALESCE(item_description,''),
		        COALESCE(item_category,''), COALESCE(item_price,''), COALESCE(item_detail,''),
		        is_multi_spec, COALESCE(multi_quantity_delivery,0)
		 FROM item_info WHERE cookie_id=? AND deleted_at IS NULL ORDER BY id DESC`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存out，供当前处理流程使用
	var out []ItemInfoRow
	for rows.Next() {
		// r 保存r，供当前处理流程使用
		var r ItemInfoRow
		// isMulti、multiQty 保存isMulti、multiQty，供当前处理流程使用
		var isMulti, multiQty int
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&r.ID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.ItemDescription,
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
	// err 保存err，供当前处理流程使用
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
				"deleted_at":              "NULL",
				"updated_at":              "CURRENT_TIMESTAMP",
			}),
		r.CookieID, r.ItemID, nullable(r.ItemTitle), nullable(r.ItemDescription),
		nullable(r.ItemCategory), nullable(r.ItemPrice), nullable(r.ItemDetail),
		boolToInt(r.IsMultiSpec), boolToInt(r.MultiQuantityDelivery))
	return err
}

// UpsertBasic 插入或补全商品基础信息，不覆盖已有的多规格/多数量发货设置。
func (i *Items) UpsertBasic(ctx context.Context, r *ItemInfoRow) error {
	return i.upsertBasic(ctx, i.DB, r)
}

// UpsertBasicTx 负责UpsertBasicTx相关处理。
func (i *Items) UpsertBasicTx(ctx context.Context, tx *sql.Tx, r *ItemInfoRow) error {
	return i.upsertBasic(ctx, tx, r)
}

// SyncFromRemote 将远端商品全集同步到本地。
//
// 远端列表只提供商品基础信息，因此保留本地的描述和发货配置；基础字段
// （标题、分类、价格、详情）由远端非空值覆盖。整个 reconcile 在一个事务内
// 完成，只有在全部远端商品写入成功后，才会逻辑删除本次全集中不存在的本地商品及其商品级自动化规则。
// SyncFromRemote 同步FromRemote。
func (i *Items) SyncFromRemote(ctx context.Context, cookieID string, rows []ItemInfoRow) (ItemSyncResult, error) {
	cookieID = strings.TrimSpace(cookieID)
	if cookieID == "" {
		return ItemSyncResult{}, errors.New("cookie_id 不能为空")
	}

	// remoteIDs 保存remoteIDs，供当前处理流程使用
	remoteIDs := make(map[string]struct{}, len(rows))
	// validRows 保存有效Rows，供当前处理流程使用
	validRows := make([]ItemInfoRow, 0, len(rows))
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		row.CookieID = cookieID
		row.ItemID = strings.TrimSpace(row.ItemID)
		if row.ItemID == "" {
			continue
		}
		if // exists 保存exists，供当前处理流程使用
		_, exists := remoteIDs[row.ItemID]; exists {
			continue
		}
		remoteIDs[row.ItemID] = struct{}{}
		validRows = append(validRows, row)
	}

	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return ItemSyncResult{}, err
	}
	// rollback 保存rollback，供当前处理流程使用
	rollback := func(err error) (ItemSyncResult, error) {
		_ = tx.Rollback()
		return ItemSyncResult{}, err
	}
	// index 表示当前遍历过程中的index
	for index := range validRows {
		if // err 保存err，供当前处理流程使用
		err := i.UpsertBasicTx(ctx, tx, &validRows[index]); err != nil {
			return rollback(err)
		}
		if validRows[index].IsMultiSpec {
			if // err 保存err，供当前处理流程使用
			_, err := tx.ExecContext(ctx,
				`UPDATE item_info SET is_multi_spec=? WHERE cookie_id=? AND item_id=?`,
				boolToInt(true), cookieID, validRows[index].ItemID); err != nil {
				return rollback(err)
			}
		}
	}

	// args 保存args，供当前处理流程使用
	args := make([]any, 0, len(remoteIDs)+1)
	args = append(args, cookieID)
	// deleteQuery 保存delete查询，供当前处理流程使用
	deleteQuery := `UPDATE item_info SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND deleted_at IS NULL`
	if len(remoteIDs) > 0 {
		// placeholders 保存placeholders，供当前处理流程使用
		placeholders := make([]string, 0, len(remoteIDs))
		// itemID 表示当前遍历过程中的商品ID
		for itemID := range remoteIDs {
			placeholders = append(placeholders, "?")
			args = append(args, itemID)
		}
		deleteQuery += ` AND item_id NOT IN (` + strings.Join(placeholders, ",") + ")"
	}
	// deletedResult、err 保存deletedResult、err，供当前处理流程使用
	deletedResult, err := tx.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return rollback(err)
	}
	// deleted、err 保存deleted、err，供当前处理流程使用
	deleted, err := deletedResult.RowsAffected()
	if err != nil {
		return rollback(err)
	}

	// ruleArgs 保存规则Args，供当前处理流程使用
	ruleArgs := make([]any, 0, len(remoteIDs)+1)
	ruleArgs = append(ruleArgs, cookieID)
	// ruleQuery 保存规则查询，供当前处理流程使用
	ruleQuery := `UPDATE automation_rules
		SET deleted_at=CURRENT_TIMESTAMP, enabled=0, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND item_id<>'' AND deleted_at IS NULL`
	if len(remoteIDs) > 0 {
		// placeholders 保存placeholders，供当前处理流程使用
		placeholders := make([]string, 0, len(remoteIDs))
		// itemID 表示当前遍历过程中的商品ID
		for itemID := range remoteIDs {
			placeholders = append(placeholders, "?")
			ruleArgs = append(ruleArgs, itemID)
		}
		ruleQuery += ` AND item_id NOT IN (` + strings.Join(placeholders, ",") + ")"
	}
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, ruleQuery, ruleArgs...); err != nil {
		return rollback(err)
	}
	if // err 保存err，供当前处理流程使用
	err := tx.Commit(); err != nil {
		return ItemSyncResult{}, err
	}
	return ItemSyncResult{Saved: len(validRows), Deleted: int(deleted)}, nil
}

// upsertBasic 负责upsertBasic相关处理。
func (i *Items) upsertBasic(ctx context.Context, execer sqlExecer, r *ItemInfoRow) error {
	// 三种数据库的条件 upsert：非空才覆盖，空值保留旧值。
	// SQLite/Postgres 用 EXCLUDED.col 引用插入值；MySQL 用 VALUES(col)。
	// conflictClause 保存conflictClause，供当前处理流程使用
	var conflictClause string
	switch i.Dialect {
	case DialectMySQL:
		conflictClause = ` ON DUPLICATE KEY UPDATE
		   item_title=CASE WHEN VALUES(item_title) IS NOT NULL AND VALUES(item_title) != '' THEN VALUES(item_title) ELSE item_info.item_title END,
		   item_description=CASE WHEN VALUES(item_description) IS NOT NULL AND VALUES(item_description) != '' THEN VALUES(item_description) ELSE item_info.item_description END,
		   item_category=CASE WHEN VALUES(item_category) IS NOT NULL AND VALUES(item_category) != '' THEN VALUES(item_category) ELSE item_info.item_category END,
		   item_price=CASE WHEN VALUES(item_price) IS NOT NULL AND VALUES(item_price) != '' THEN VALUES(item_price) ELSE item_info.item_price END,
		   item_detail=CASE WHEN VALUES(item_detail) IS NOT NULL AND VALUES(item_detail) != '' THEN VALUES(item_detail) ELSE item_info.item_detail END,
		   deleted_at=NULL,
		   updated_at=CURRENT_TIMESTAMP`
	default:
		conflictClause = ` ON CONFLICT(cookie_id, item_id) DO UPDATE SET
		   item_title=CASE WHEN EXCLUDED.item_title IS NOT NULL AND EXCLUDED.item_title != '' THEN EXCLUDED.item_title ELSE item_info.item_title END,
		   item_description=CASE WHEN EXCLUDED.item_description IS NOT NULL AND EXCLUDED.item_description != '' THEN EXCLUDED.item_description ELSE item_info.item_description END,
		   item_category=CASE WHEN EXCLUDED.item_category IS NOT NULL AND EXCLUDED.item_category != '' THEN EXCLUDED.item_category ELSE item_info.item_category END,
		   item_price=CASE WHEN EXCLUDED.item_price IS NOT NULL AND EXCLUDED.item_price != '' THEN EXCLUDED.item_price ELSE item_info.item_price END,
		   item_detail=CASE WHEN EXCLUDED.item_detail IS NOT NULL AND EXCLUDED.item_detail != '' THEN EXCLUDED.item_detail ELSE item_info.item_detail END,
		   deleted_at=NULL,
		   updated_at=CURRENT_TIMESTAMP`
	}
	// err 保存err，供当前处理流程使用
	_, err := execer.ExecContext(ctx,
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_description,
		    item_category, item_price, item_detail, updated_at)
		 VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+conflictClause,
		r.CookieID, r.ItemID, nullable(r.ItemTitle), nullable(r.ItemDescription),
		nullable(r.ItemCategory), nullable(r.ItemPrice), nullable(r.ItemDetail))
	return err
}

// Delete 逻辑删除商品及其商品级自动化规则，保留历史数据以便审计和恢复。
func (i *Items) Delete(ctx context.Context, cookieID, itemID string) error {
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `UPDATE item_info
		SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID); err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, `UPDATE automation_rules
		SET deleted_at=CURRENT_TIMESTAMP, enabled=0, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetMultiSpec 设置多规格开关。
func (i *Items) SetMultiSpec(ctx context.Context, cookieID, itemID string, on bool) error {
	// err 保存err，供当前处理流程使用
	_, err := i.DB.ExecContext(ctx,
		`UPDATE item_info SET is_multi_spec=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`,
		boolToInt(on), cookieID, itemID)
	return err
}

// SetMultiQuantity 设置多数量发货开关。
func (i *Items) SetMultiQuantity(ctx context.Context, cookieID, itemID string, on bool) error {
	// err 保存err，供当前处理流程使用
	_, err := i.DB.ExecContext(ctx,
		`UPDATE item_info SET multi_quantity_delivery=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`,
		boolToInt(on), cookieID, itemID)
	return err
}
