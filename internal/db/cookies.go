package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DisableReasonManual 用于本次流程后续判断的Disable原因Manual
const DisableReasonManual = "manual"

// ErrAlreadyExists 表示创建资源时主键已经存在。调用方不得把它解释为可覆盖。
var ErrAlreadyExists = errors.New("记录已存在")

// Cookies 闲鱼账号（cookie）相关操作。
type Cookies struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// AccountSettingsUpdate 表示账号编辑弹窗的一次原子保存。nil 字段保持原值；
// ChannelIDs 非 nil 时覆盖通知绑定（空切片表示明确解绑全部）。
// AccountSettingsUpdate 用于本次流程后续判断的账号设置Update
type AccountSettingsUpdate struct {
	UserID        int64
	Value         *string
	Remark        *string
	AutoConfirm   *bool
	PauseDuration *int
	Username      *string
	Password      *string
	ShowBrowser   *bool
	ChannelIDs    *[]int64
}

// UpdateSettings 在一个事务中更新账号字段及通知绑定，避免前端并行请求只成功一部分。
func (c *Cookies) UpdateSettings(ctx context.Context, cookieID string, input AccountSettingsUpdate) (int64, error) {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// ownerID 用于本次流程后续判断的所有者ID
	var ownerID int64
	// metadataJSON 用于本次流程后续判断的metadataJSON
	var metadataJSON string
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, c.cookieSelectForUpdate(`user_id,COALESCE(metadata_json,'')`), cookieID).Scan(&ownerID, &metadataJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if ownerID != input.UserID {
		return 0, ErrForbidden
	}

	// assignments 用于本次流程后续判断的assignments
	assignments := make([]string, 0, 8)
	// args 用于本次流程后续判断的args
	args := make([]any, 0, 9)
	if input.Value != nil {
		// encrypted、err 用于本次流程后续判断的encrypted、err
		encrypted, err := c.codec.encrypt("cookie", cookieID, *input.Value)
		if err != nil {
			return 0, err
		}
		assignments = append(assignments, "value=?")
		args = append(args, encrypted)
		// plainMetadata、err 用于本次流程后续判断的plainMetadata、err
		plainMetadata, err := c.codec.decrypt(cookieMetadataScope, cookieID, metadataJSON)
		if err != nil {
			return 0, err
		}
		// cleanMetadata、err 用于本次流程后续判断的cleanMetadata、err
		cleanMetadata, err := c.codec.encrypt(cookieMetadataScope, cookieID, stripCookieSnapshotMetadata(plainMetadata))
		if err != nil {
			return 0, err
		}
		assignments = append(assignments, "metadata_json=?")
		args = append(args, cleanMetadata)
	}
	if input.Remark != nil {
		assignments = append(assignments, "remark=?")
		args = append(args, *input.Remark)
	}
	if input.AutoConfirm != nil {
		assignments = append(assignments, "auto_confirm=?")
		args = append(args, boolToInt(*input.AutoConfirm))
	}
	// pausedUntil 用于本次流程后续判断的pausedUntil
	pausedUntil := int64(0)
	if input.PauseDuration != nil {
		if *input.PauseDuration < 0 || *input.PauseDuration > 1440 {
			return 0, fmt.Errorf("暂停时长必须在 0 到 1440 分钟之间")
		}
		if *input.PauseDuration > 0 {
			pausedUntil = time.Now().UTC().Add(time.Duration(*input.PauseDuration) * time.Minute).Unix()
		}
		assignments = append(assignments, "pause_duration=?", "paused_until=?")
		args = append(args, *input.PauseDuration, pausedUntil)
	}
	if input.Username != nil {
		assignments = append(assignments, "username=?")
		args = append(args, *input.Username)
	}
	if input.Password != nil {
		// encrypted、err 用于本次流程后续判断的encrypted、err
		encrypted, err := c.codec.encrypt("login-password", cookieID, *input.Password)
		if err != nil {
			return 0, err
		}
		assignments = append(assignments, "password=?")
		args = append(args, encrypted)
	}
	if input.ShowBrowser != nil {
		assignments = append(assignments, "show_browser=?")
		args = append(args, boolToInt(*input.ShowBrowser))
	}
	if len(assignments) > 0 {
		args = append(args, cookieID, input.UserID)
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, `UPDATE cookies SET `+strings.Join(assignments, ",")+`,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`, args...); err != nil {
			return 0, err
		}
	}
	if input.PauseDuration != nil && *input.PauseDuration == 0 {
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND status='pending'`, cookieID); err != nil {
			return 0, err
		}
	}
	if input.ChannelIDs != nil {
		if len(*input.ChannelIDs) > 100 {
			return 0, fmt.Errorf("单个账号最多绑定 100 个通知渠道")
		}
		// seen 用于本次流程后续判断的seen
		seen := make(map[int64]struct{}, len(*input.ChannelIDs))
		// channelID 表示当前遍历过程中的渠道ID
		for _, channelID := range *input.ChannelIDs {
			if // duplicate 用于本次流程后续判断的duplicate
			_, duplicate := seen[channelID]; duplicate {
				continue
			}
			seen[channelID] = struct{}{}
			// exists 用于本次流程后续判断的exists
			var exists bool
			if // err 用于本次流程后续判断的err
			err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`, channelID, input.UserID).Scan(&exists); err != nil {
				return 0, err
			}
			if !exists {
				return 0, ErrForbidden
			}
		}
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, `DELETE FROM message_notifications WHERE cookie_id=?`, cookieID); err != nil {
			return 0, err
		}
		// channelID 表示当前遍历过程中的渠道ID
		for channelID := range seen {
			if // err 用于本次流程后续判断的err
			_, err := tx.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES (?,?,1)`, cookieID, channelID); err != nil {
				return 0, err
			}
		}
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return 0, err
	}
	return pausedUntil, nil
}

// CreateOwned 创建归属于 userID 的账号。冲突时绝不修改既有账号的 owner 或 cookie。
func (c *Cookies) CreateOwned(ctx context.Context, cookieID, cookieValue string, userID int64) error {
	if cookieID == "" || userID <= 0 {
		return errors.New("账号 ID 和 user_id 不能为空")
	}
	// encrypted、err 用于本次流程后续判断的encrypted、err
	encrypted, err := c.codec.encrypt("cookie", cookieID, cookieValue)
	if err != nil {
		return err
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := c.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(c.Dialect)+` INTO cookies (id, value, user_id, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)`+dialectInsertIgnore(c.Dialect, []string{"id"}),
		cookieID, encrypted, userID)
	if err != nil {
		return err
	}
	if // n、rowsErr 用于本次流程后续判断的n、rowsErr
	n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if n == 1 {
		return nil
	}

	// existingOwner 用于本次流程后续判断的existing所有者
	var existingOwner int64
	if // err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&existingOwner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("查询 cookie user_id: %w", err)
	}
	if existingOwner != userID {
		return ErrForbidden
	}
	return ErrAlreadyExists
}

// UpdateValueOwned 只更新属于 userID 的既有账号，不能创建或转移账号归属。
func (c *Cookies) UpdateValueOwned(ctx context.Context, cookieID, cookieValue string, userID int64) error {
	if cookieID == "" || userID <= 0 {
		return errors.New("账号 ID 和 user_id 不能为空")
	}
	// encrypted、err 用于本次流程后续判断的encrypted、err
	encrypted, err := c.codec.encrypt("cookie", cookieID, cookieValue)
	if err != nil {
		return err
	}
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// existingOwner 用于本次流程后续判断的existing所有者
	var existingOwner int64
	// rawMetadata 用于本次流程后续判断的原始Metadata
	var rawMetadata string
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, c.cookieSelectForUpdate(`user_id,COALESCE(metadata_json,'')`), cookieID).Scan(&existingOwner, &rawMetadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("查询 cookie user_id: %w", err)
	}
	if existingOwner != userID {
		return ErrForbidden
	}
	// metadata、err 用于本次流程后续判断的metadata、err
	metadata, err := c.metadataWithoutCookieSnapshotValue(cookieID, rawMetadata)
	if err != nil {
		return err
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx,
		`UPDATE cookies SET value=?,metadata_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		encrypted, metadata, cookieID, userID)
	if err != nil {
		return err
	}
	if // n、rowsErr 用于本次流程后续判断的n、rowsErr
	n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if n > 1 {
		return fmt.Errorf("更新 cookie 影响了 %d 行", n)
	}
	return tx.Commit()
}

// UpdateValueExisting 供续期和运行时写回使用，只能更新现存账号。
// owner 由数据库中的记录决定；账号在任务期间被删除时返回 ErrNotFound，禁止复活。
// UpdateValueExisting 更新值Existing。
func (c *Cookies) UpdateValueExisting(ctx context.Context, cookieID, cookieValue string) error {
	if cookieID == "" {
		return errors.New("账号 ID 不能为空")
	}
	// encrypted、err 用于本次流程后续判断的encrypted、err
	encrypted, err := c.codec.encrypt("cookie", cookieID, cookieValue)
	if err != nil {
		return err
	}
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// rawMetadata 用于本次流程后续判断的原始Metadata
	var rawMetadata string
	if // err 用于本次流程后续判断的err
	err := tx.QueryRowContext(ctx, c.cookieSelectForUpdate(`COALESCE(metadata_json,'')`), cookieID).Scan(&rawMetadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// metadata、err 用于本次流程后续判断的metadata、err
	metadata, err := c.metadataWithoutCookieSnapshotValue(cookieID, rawMetadata)
	if err != nil {
		return err
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx,
		`UPDATE cookies SET value=?,metadata_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, encrypted, metadata, cookieID)
	if err != nil {
		return err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 1 {
		return fmt.Errorf("更新 cookie 影响了 %d 行", n)
	}
	return tx.Commit()
}

// cookieSelectForUpdate 封装登录凭证SelectForUpdate业务协调。
func (c *Cookies) cookieSelectForUpdate(columns string) string {
	// query 用于本次流程后续判断的查询
	query := `SELECT ` + columns + ` FROM cookies WHERE id=?`
	if c.Dialect == DialectMySQL || c.Dialect == DialectPostgres {
		query += ` FOR UPDATE`
	}
	return query
}

// metadataWithoutCookieSnapshotValue 封装metadataWithout登录凭证Snapshot值业务协调。
func (c *Cookies) metadataWithoutCookieSnapshotValue(cookieID, raw string) (string, error) {
	// plain、err 用于本次流程后续判断的plain、err
	plain, err := c.codec.decrypt(cookieMetadataScope, cookieID, raw)
	if err != nil {
		return "", err
	}
	return c.codec.encrypt(cookieMetadataScope, cookieID, stripCookieSnapshotMetadata(plain))
}

// stripCookieSnapshotMetadata 封装strip登录凭证SnapshotMetadata业务协调。
func stripCookieSnapshotMetadata(metadata string) string {
	if strings.TrimSpace(metadata) == "" {
		return ""
	}
	// values 用于本次流程后续判断的values
	var values map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(metadata), &values); err != nil {
		// 无法解析的历史 metadata 本来也不能被当作快照使用，原样保留。
		return metadata
	}
	delete(values, "cookies_refresh_snapshot")
	delete(values, "cookie_refresh_snapshot")
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := json.Marshal(values)
	if err != nil {
		return metadata
	}
	return string(raw)
}

// Save 保留给历史调用和测试夹具使用。新业务代码应选择 CreateOwned、
// UpdateValueOwned 或 UpdateValueExisting，避免把创建和后台写回混为一谈。
// Save 保存当前值。
func (c *Cookies) Save(ctx context.Context, cookieID, cookieValue string, userID int64) error {
	if userID == 0 {
		return c.UpdateValueExisting(ctx, cookieID, cookieValue)
	}
	if // err 用于本次流程后续判断的err
	err := c.UpdateValueOwned(ctx, cookieID, cookieValue, userID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if // err 用于本次流程后续判断的err
	err := c.CreateOwned(ctx, cookieID, cookieValue, userID); errors.Is(err, ErrAlreadyExists) {
		// 同一 owner 并发创建时，让最后一次写入拥有和历史 Save 相同的更新语义。
		return c.UpdateValueOwned(ctx, cookieID, cookieValue, userID)
	} else {
		return err
	}
}

// Delete 删除 cookie 及无法通过外键级联清理的关联数据。
func (c *Cookies) Delete(ctx context.Context, cookieID string) error {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if // err 用于本次流程后续判断的err
	err := deleteCookieTx(ctx, tx, cookieID); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteCookieTx 清理一个账号的全部按 cookie_id 关联数据。这里同时处理历史上
// 没有外键的表，确保账号 ID 被复用时不会继承前一个 owner 的数据。
// deleteCookieTx 封装delete登录凭证Tx业务协调。
func deleteCookieTx(ctx context.Context, tx *sql.Tx, cookieID string) error {
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	// table 表示当前遍历过程中的table
	for _, table := range []string{
		"item_replay",
		"scheduled_cookies_refresh_log",
		"scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log",
		"account_login_logs",
	} {
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE cookie_id=?`, cookieID); err != nil {
			return err
		}
	}
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `DELETE FROM cookies WHERE id=?`, cookieID); err != nil {
		return err
	}
	return nil
}

// GetValue 取 cookie 明文值。不存在返回 ErrNotFound。
func (c *Cookies) GetValue(ctx context.Context, cookieID string) (string, error) {
	// v 用于本次流程后续判断的v
	var v string
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT value FROM cookies WHERE id=?`, cookieID).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return c.codec.decrypt("cookie", cookieID, v)
}

// GetDetails 取账号完整详情。不存在返回 ErrNotFound。
func (c *Cookies) GetDetails(ctx context.Context, cookieID string) (*CookieDetail, error) {
	// d 用于本次流程后续判断的d
	var d CookieDetail
	// autoConfirm、showBrowser 用于本次流程后续判断的autoConfirm、show浏览器
	var autoConfirm, showBrowser int
	// pauseDuration 用于本次流程后续判断的pause时长
	var pauseDuration sql.NullInt64
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx,
		`SELECT id, value, user_id, auto_confirm, COALESCE(remark,''), pause_duration, COALESCE(paused_until,0),
		        COALESCE(username,''), COALESCE(password,''),
		        show_browser, COALESCE(nickname,''), COALESCE(avatar_url,''),
		        COALESCE(metadata_json,''), COALESCE(last_refresh_at,0),
		        COALESCE(login_method,''), COALESCE(last_login_at,0), created_at
		 FROM cookies WHERE id=?`, cookieID).Scan(
		&d.ID, &d.Value, &d.UserID, &autoConfirm, &d.Remark, &pauseDuration, &d.PausedUntil,
		&d.Username, &d.Password, &showBrowser, &d.Nickname, &d.AvatarURL,
		&d.MetadataJSON, &d.LastRefreshAt, &d.LoginMethod, &d.LastLoginAt, &d.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.AutoConfirm = autoConfirm != 0
	d.ShowBrowser = showBrowser != 0
	d.PauseDuration = 10
	if pauseDuration.Valid {
		// 0 是有效值，表示不暂停。
		d.PauseDuration = int(pauseDuration.Int64)
	}
	d.Value, err = c.codec.decrypt("cookie", d.ID, d.Value)
	if err != nil {
		return nil, err
	}
	d.MetadataJSON, err = c.codec.decrypt(cookieMetadataScope, d.ID, d.MetadataJSON)
	if err != nil {
		return nil, err
	}
	d.Password, err = c.codec.decrypt("login-password", d.ID, d.Password)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateLoginInfo 保存账号密码登录资料。
func (c *Cookies) UpdateLoginInfo(ctx context.Context, cookieID, username, password string, showBrowser bool) error {
	// v 用于本次流程后续判断的v
	v := 0
	if showBrowser {
		v = 1
	}
	// encrypted、err 用于本次流程后续判断的encrypted、err
	encrypted, err := c.codec.encrypt("login-password", cookieID, password)
	if err != nil {
		return err
	}
	_, err = c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET username=?, password=?, show_browser=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		username, encrypted, v, cookieID)
	return err
}

// MarkLogin 记录账号最近一次成功登录方式。
func (c *Cookies) MarkLogin(ctx context.Context, cookieID, method string, loginAt int64) error {
	// err 用于本次流程后续判断的err
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET login_method=?, last_login_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		method, loginAt, cookieID)
	return err
}

// UpdateProfile 保存账号展示资料。
func (c *Cookies) UpdateProfile(ctx context.Context, cookieID, nickname, avatarURL string) error {
	// err 用于本次流程后续判断的err
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET nickname=?, avatar_url=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		nickname, avatarURL, cookieID)
	return err
}

// GetPauseDuration 取自动回复暂停时长（分钟）。未找到或 NULL 返回 10。
func (c *Cookies) GetPauseDuration(ctx context.Context, cookieID string) int {
	// pd 用于本次流程后续判断的pd
	var pd sql.NullInt64
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT pause_duration FROM cookies WHERE id=?`, cookieID).Scan(&pd)
	if err != nil || !pd.Valid {
		return 10
	}
	return int(pd.Int64) // 0 有效
}

// SetPause 设置账号暂停截止时间。minutes=0 立即取消暂停。
func (c *Cookies) SetPause(ctx context.Context, cookieID string, minutes int) (int64, error) {
	if minutes < 0 || minutes > 1440 {
		return 0, fmt.Errorf("暂停时长必须在 0 到 1440 分钟之间")
	}
	// pausedUntil 用于本次流程后续判断的pausedUntil
	pausedUntil := int64(0)
	if minutes > 0 {
		pausedUntil = time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Unix()
	}
	// err 用于本次流程后续判断的err
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies SET pause_duration=?,paused_until=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		minutes, pausedUntil, cookieID)
	if err == nil && minutes == 0 {
		// 用户主动恢复时无需等待原 paused_until；让持久化事件在下一轮调度立即可认领。
		_, err = c.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND status='pending'`, cookieID)
	}
	return pausedUntil, err
}

// IsPaused 返回账号当前是否暂停以及暂停截止 Unix 秒。
func (c *Cookies) IsPaused(ctx context.Context, cookieID string) (bool, int64, error) {
	// pausedUntil 用于本次流程后续判断的pausedUntil
	var pausedUntil int64
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT COALESCE(paused_until,0) FROM cookies WHERE id=?`, cookieID).Scan(&pausedUntil)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, 0, ErrNotFound
		}
		return false, 0, err
	}
	return pausedUntil > time.Now().UTC().Unix(), pausedUntil, nil
}

// GetAutoConfirm 读取账号是否自动将订单确认成已发货状态。
func (c *Cookies) GetAutoConfirm(ctx context.Context, cookieID string) (bool, error) {
	// enabled 用于本次流程后续判断的启用状态
	var enabled int
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT auto_confirm FROM cookies WHERE id=?`, cookieID).Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return enabled != 0, nil
}

// SetStatus 启用/禁用账号（cookie_status 表）。
func (c *Cookies) SetStatus(ctx context.Context, cookieID string, enabled bool) error {
	return c.SetStatusWithReason(ctx, cookieID, enabled, "")
}

// SetStatusWithReason 启用/禁用账号并记录禁用原因。
func (c *Cookies) SetStatusWithReason(ctx context.Context, cookieID string, enabled bool, reason string) error {
	// v 用于本次流程后续判断的v
	v := 0
	if enabled {
		v = 1
	}
	// err 用于本次流程后续判断的err
	_, err := c.DB.ExecContext(ctx,
		`INSERT INTO cookie_status (cookie_id, enabled, disable_reason, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`+
			dialectUpsert(c.Dialect, []string{"cookie_id"}, map[string]string{
				"enabled":        "EXCLUDED.enabled",
				"disable_reason": "EXCLUDED.disable_reason",
				"updated_at":     "CURRENT_TIMESTAMP",
			}),
		cookieID, v, reason)
	return err
}

// GetStatus 取启用状态。无状态记录仍按启用处理；数据库错误则安全地按停用处理。
func (c *Cookies) GetStatus(ctx context.Context, cookieID string) bool {
	// enabled、err 用于本次流程后续判断的enabled、err
	enabled, err := c.Status(ctx, cookieID)
	if err != nil {
		return false
	}
	return enabled
}

// Status 返回账号启用状态；没有 cookie_status 记录时按启用处理，数据库错误不再静默放行。
func (c *Cookies) Status(ctx context.Context, cookieID string) (bool, error) {
	// enabled、err 用于本次流程后续判断的enabled、err
	enabled, _, err := c.StatusWithReason(ctx, cookieID)
	return enabled, err
}

// StatusWithReason 原子语义地读取账号当前启用状态和禁用原因；调用方应在
// Store.LockAccountCredentials 保护下使用，以复核列表查询后的最新状态。
// StatusWithReason 封装状态With原因业务协调。
func (c *Cookies) StatusWithReason(ctx context.Context, cookieID string) (bool, string, error) {
	// exists 用于本次流程后续判断的exists
	var exists bool
	if // err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cookies WHERE id=?)`, cookieID).Scan(&exists); err != nil {
		return false, "", err
	}
	if !exists {
		return false, "", ErrNotFound
	}
	// enabled 用于本次流程后续判断的启用状态
	var enabled int
	// reason 用于本次流程后续判断的原因
	var reason string
	// err 用于本次流程后续判断的err
	err := c.DB.QueryRowContext(ctx, `SELECT enabled,COALESCE(disable_reason,'') FROM cookie_status WHERE cookie_id=?`, cookieID).Scan(&enabled, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return enabled != 0, reason, nil
}
