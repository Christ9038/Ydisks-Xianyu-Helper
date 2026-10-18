package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ---- 指定商品回复 (item_replay) CRUD ----

// ItemReplies 已在 reply.go 定义 Get。补 Set/Delete。

// Set 只更新 cookieID 下 itemID 的正文 content，保留新增图片字段以兼容旧调用方。
func (i *ItemReplies) Set(ctx context.Context, cookieID, itemID, content string) error {
	return i.UpdateWithCurrent(ctx, cookieID, itemID, func(current ItemReply) (ItemReply, error) {
		// current 是事务内读取的完整配置，旧入口只能修改正文。
		current.ReplyContent = content
		return current, nil
	})
}

// Delete 删除指定商品回复。
func (i *ItemReplies) Delete(ctx context.Context, cookieID, itemID string) error {
	// err 用于本次流程后续判断的err
	_, err := i.DB.ExecContext(ctx,
		`DELETE FROM item_replay WHERE cookie_id=? AND item_id=?`, cookieID, itemID)
	return err
}

// AllForUser 根据 ctx 和 cookieID 返回商品图文与独立去重配置，读取失败不返回部分列表。
func (i *ItemReplies) AllForUser(ctx context.Context, cookieID string) ([]ItemReply, error) {
	// rows、err 保存当前账号商品配置游标及查询错误。
	rows, err := i.DB.QueryContext(ctx,
		`SELECT item_id, cookie_id, COALESCE(reply_content,''), COALESCE(reply_image_url,''), COALESCE(reply_image_path,''), reply_once FROM item_replay WHERE cookie_id=?`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 汇总当前账号的非敏感商品配置，调用方负责用户归属校验。
	var out []ItemReply
	for rows.Next() {
		// r 是当前商品的完整配置，包含独立 reply_once 开关。
		var r ItemReply
		if // err 是当前行扫描失败原因，失败时不暴露部分结果。
		err := rows.Scan(&r.ItemID, &r.CookieID, &r.ReplyContent, &r.ReplyImageURL, &r.ReplyImagePath, &r.ReplyOnce); err != nil {
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

// NotificationChannelSummaryRow 是通知渠道列表的非敏感摘要行，刻意不包含加密配置。
type NotificationChannelSummaryRow struct {
	// ID 是渠道稳定标识。
	ID int64
	// Name 是用户可识别的渠道名称。
	Name string
	// Type 是通知渠道协议类型。
	Type string
	// EventTypes 是渠道订阅事件编码。
	EventTypes string
	// Enabled 表示渠道是否启用。
	Enabled bool
	// UserID 是渠道所属用户，用于保留归属信息但不读取秘密配置。
	UserID int64
}

// AllChannelsForUser 取某用户全部通知渠道。
func (n *Notifications) AllChannelsForUser(ctx context.Context, userID int64) ([]NotificationChannelRow, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := n.DB.QueryContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1) FROM notification_channels
		 WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	var out []NotificationChannelRow
	for rows.Next() {
		// c 用于本次流程后续判断的c
		var c NotificationChannelRow
		// enabled 用于本次流程后续判断的启用状态
		var enabled int
		if // err 用于本次流程后续判断的err
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

// ListChannelSummariesForUser 查询用户渠道摘要，不读取或解密 Config。
func (n *Notifications) ListChannelSummariesForUser(ctx context.Context, userID int64) ([]NotificationChannelSummaryRow, error) {
	// rows、err 保存仅包含非敏感字段的渠道查询结果及数据库错误。
	rows, err := n.DB.QueryContext(ctx,
		`SELECT id, name, type, COALESCE(event_types,''), enabled, COALESCE(user_id,1) FROM notification_channels
		 WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// summaries 保存不含渠道配置的用户渠道摘要。
	summaries := make([]NotificationChannelSummaryRow, 0)
	for rows.Next() {
		// summary 保存当前遍历到的非敏感渠道字段；enabledValue 保存数据库布尔值。
		var summary NotificationChannelSummaryRow
		// enabledValue 保存数据库中启用标记的整数表示。
		var enabledValue int
		// scanErr 保存当前摘要行字段转换失败的数据库错误。
		if scanErr := rows.Scan(&summary.ID, &summary.Name, &summary.Type, &summary.EventTypes, &enabledValue, &summary.UserID); scanErr != nil {
			return nil, scanErr
		}
		summary.Enabled = enabledValue != 0
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// CreateChannel 创建通知渠道。
func (n *Notifications) CreateChannel(ctx context.Context, c *NotificationChannelRow) (int64, error) {
	// config、err 用于本次流程后续判断的config、err
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
		if // err 用于本次流程后续判断的err
		err := n.DB.QueryRowContext(ctx, `SELECT COALESCE(user_id,1) FROM notification_channels WHERE id=?`, c.ID).Scan(&c.UserID); err != nil {
			return err
		}
	}
	// config、err 用于本次流程后续判断的config、err
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
	// row 用于本次流程后续判断的row
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1)
		   FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	// c 用于本次流程后续判断的c
	var c NotificationChannelRow
	// enabled 用于本次流程后续判断的启用状态
	var enabled int
	if // err 用于本次流程后续判断的err
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &enabled, &c.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c.Enabled = enabled != 0
	// config、err 用于本次流程后续判断的config、err
	config, err := n.codec.decrypt("notification-config", strconv.FormatInt(c.UserID, 10), c.Config)
	if err != nil {
		return nil, err
	}
	c.Config = config
	return &c, nil
}

// UpdateChannelForUser 更新指定用户拥有的通知渠道。
func (n *Notifications) UpdateChannelForUser(ctx context.Context, c *NotificationChannelRow, userID int64) error {
	// config、err 用于本次流程后续判断的config、err
	config, err := n.codec.encrypt("notification-config", strconv.FormatInt(userID, 10), c.Config)
	if err != nil {
		return err
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := n.DB.ExecContext(ctx,
		`UPDATE notification_channels
		    SET name=?, type=?, config=?, event_types=?, enabled=?, updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND user_id=?`,
		c.Name, c.Type, config, c.EventTypes, boolToInt(c.Enabled), c.ID, userID)
	if err != nil {
		return err
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChannel 删除通知渠道。
func (n *Notifications) DeleteChannel(ctx context.Context, id int64) error {
	// err 用于本次流程后续判断的err
	_, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	return err
}

// DeleteChannelForUser 删除指定用户拥有的通知渠道。
func (n *Notifications) DeleteChannelForUser(ctx context.Context, id, userID int64) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetChannel 按 ID 取单个通知渠道（含 config）。未找到返回 nil。
func (n *Notifications) GetChannel(ctx context.Context, id int64) (*NotificationChannel, error) {
	// row 用于本次流程后续判断的row
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), COALESCE(user_id,1) FROM notification_channels WHERE id=?`, id)
	// c 用于本次流程后续判断的c
	var c NotificationChannel
	// userID 用于本次流程后续判断的用户ID
	var userID int64
	if // err 用于本次流程后续判断的err
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// c.UserID 保存渠道所属用户，供全局通知器按所有者读取系统敏感配置。
	c.UserID = userID
	// config、err 用于本次流程后续判断的config、err
	config, err := n.codec.decrypt("notification-config", strconv.FormatInt(userID, 10), c.Config)
	if err != nil {
		return nil, err
	}
	c.Config = config
	return &c, nil
}

// AccountBindings 取某账号已绑定的通知渠道 ID 列表。
func (n *Notifications) AccountBindings(ctx context.Context, cookieID string) ([]int64, error) {
	// rows、err 用于本次流程后续判断的rows、err
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
	// out 用于本次流程后续判断的out
	var out []int64
	for rows.Next() {
		// id 用于本次流程后续判断的标识
		var id int64
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetBindings 设置某账号的通知渠道绑定（覆盖式）。
func (n *Notifications) SetBindings(ctx context.Context, cookieID string, channelIDs []int64) error {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// userID 用于本次流程后续判断的用户ID
	var userID int64
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// channelID 表示当前遍历过程中的渠道ID
	for _, channelID := range channelIDs {
		// exists 用于本次流程后续判断的exists
		var exists bool
		if // err 用于本次流程后续判断的err
		err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`,
			channelID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrForbidden
		}
	}
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `DELETE FROM message_notifications WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	// cid 表示当前遍历过程中的cid
	for _, cid := range channelIDs {
		if // err 用于本次流程后续判断的err
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
	// v 用于本次流程后续判断的v
	var v string
	// keyCol 用于本次流程后续判断的keyCol
	keyCol := dialectQuote(s.Dialect, "key")
	// err 用于本次流程后续判断的err
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
	// keyCol 用于本次流程后续判断的keyCol
	keyCol := dialectQuote(s.Dialect, "key")
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyCol+`, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// m 用于本次流程后续判断的m
	m := make(map[string]string)
	for rows.Next() {
		// k、v 用于本次流程后续判断的k、v
		var k, v string
		if // err 用于本次流程后续判断的err
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
		// encrypted、err 用于本次流程后续判断的encrypted、err
		encrypted, err := s.codec.encrypt("system-setting", key, value)
		if err != nil {
			return err
		}
		value = encrypted
	}
	// keyCol 用于本次流程后续判断的keyCol
	keyCol := dialectQuote(s.Dialect, "key")
	// err 用于本次流程后续判断的err
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
	// tx、err 用于本次流程后续判断的tx、err
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
		if // err 用于本次流程后续判断的err
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
	// all、err 用于本次流程后续判断的all、err
	all, err := s.Redacted(ctx)
	if err != nil {
		return nil, err
	}
	// out 用于本次流程后续判断的out
	out := make(map[string]string)
	// k 表示当前遍历过程中的k
	for k := range PublicSystemKeys {
		if // v、ok 用于本次流程后续判断的v、ok
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
	AIOverride            string
}

// ItemSyncResult 是一次远端商品全集同步的结果。
type ItemSyncResult struct {
	Saved   int
	Deleted int
}

// AllForCookie 取某账号全部商品。
