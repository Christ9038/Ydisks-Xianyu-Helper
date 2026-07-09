package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Cookies 闲鱼账号（cookie）相关操作。
type Cookies struct {
	DB      *sql.DB
	Dialect Dialect
}

// Save 保存/更新 cookie。user_id 为 0 时：复用现有记录的 user_id，若无则报错
// 系统未初始化时不兜底到默认用户。
func (c *Cookies) Save(ctx context.Context, cookieID, cookieValue string, userID int64) error {
	if userID != 0 {
		var existing int64
		err := c.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&existing)
		if err == nil && existing != userID {
			return ErrForbidden
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("查询 cookie user_id: %w", err)
		}
	}
	if userID == 0 {
		var existing int64
		err := c.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&existing)
		if err == nil {
			userID = existing
		} else if errors.Is(err, sql.ErrNoRows) {
			return errors.New("系统未初始化：admin 用户不存在，请先执行 init-admin 初始化管理员")
		} else {
			return fmt.Errorf("查询 cookie user_id: %w", err)
		}
	}
	_, err := c.DB.ExecContext(ctx,
		`INSERT INTO cookies (id, value, user_id, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)`+
			dialectUpsert(c.Dialect, []string{"id"}, map[string]string{
				"value":      "excluded.value",
				"user_id":    "excluded.user_id",
				"updated_at": "CURRENT_TIMESTAMP",
			}),
		cookieID, cookieValue, userID)
	return err
}

// Delete 删除 cookie 及其关联关键字（级联由外键处理，keywords 显式删以兼容未开外键的库）。
func (c *Cookies) Delete(ctx context.Context, cookieID string) error {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cookies WHERE id=?`, cookieID); err != nil {
		return err
	}
	return tx.Commit()
}

// GetValue 取 cookie 明文值。不存在返回 ErrNotFound。
func (c *Cookies) GetValue(ctx context.Context, cookieID string) (string, error) {
	var v string
	err := c.DB.QueryRowContext(ctx, `SELECT value FROM cookies WHERE id=?`, cookieID).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return v, nil
}

// AllForUser 取某用户的所有 cookie（id→value）。userID 为 0 时取全部（管理员视图）。
func (c *Cookies) AllForUser(ctx context.Context, userID int64) (map[string]string, error) {
	var rows *sql.Rows
	var err error
	if userID == 0 {
		rows, err = c.DB.QueryContext(ctx, `SELECT id, value FROM cookies`)
	} else {
		rows, err = c.DB.QueryContext(ctx, `SELECT id, value FROM cookies WHERE user_id=?`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var id, v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		m[id] = v
	}
	return m, rows.Err()
}

// GetDetails 取账号完整详情。不存在返回 ErrNotFound。
func (c *Cookies) GetDetails(ctx context.Context, cookieID string) (*CookieDetail, error) {
	var d CookieDetail
	var autoConfirm, showBrowser int
	var pauseDuration sql.NullInt64
	err := c.DB.QueryRowContext(ctx,
		`SELECT id, value, user_id, auto_confirm, COALESCE(remark,''), pause_duration,
		        COALESCE(username,''), COALESCE(password,''),
		        show_browser, COALESCE(nickname,''), COALESCE(avatar_url,''),
		        COALESCE(metadata_json,''), COALESCE(last_refresh_at,0),
		        COALESCE(login_method,''), COALESCE(last_login_at,0), created_at
		 FROM cookies WHERE id=?`, cookieID).Scan(
		&d.ID, &d.Value, &d.UserID, &autoConfirm, &d.Remark, &pauseDuration,
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
	return &d, nil
}

// UpdateLoginInfo 保存账号密码登录资料。
func (c *Cookies) UpdateLoginInfo(ctx context.Context, cookieID, username, password string, showBrowser bool) error {
	v := 0
	if showBrowser {
		v = 1
	}
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET username=?, password=?, show_browser=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		username, password, v, cookieID)
	return err
}

// MarkLogin 记录账号最近一次成功登录方式。
func (c *Cookies) MarkLogin(ctx context.Context, cookieID, method string, loginAt int64) error {
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET login_method=?, last_login_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		method, loginAt, cookieID)
	return err
}

// UpdateProfile 保存账号展示资料。
func (c *Cookies) UpdateProfile(ctx context.Context, cookieID, nickname, avatarURL string) error {
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET nickname=?, avatar_url=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		nickname, avatarURL, cookieID)
	return err
}

// GetPauseDuration 取自动回复暂停时长（分钟）。未找到或 NULL 返回 10。
func (c *Cookies) GetPauseDuration(ctx context.Context, cookieID string) int {
	var pd sql.NullInt64
	err := c.DB.QueryRowContext(ctx, `SELECT pause_duration FROM cookies WHERE id=?`, cookieID).Scan(&pd)
	if err != nil || !pd.Valid {
		return 10
	}
	return int(pd.Int64) // 0 有效
}

// GetAutoConfirm 读取账号是否自动将订单确认成已发货状态。
func (c *Cookies) GetAutoConfirm(ctx context.Context, cookieID string) (bool, error) {
	var enabled int
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
	v := 0
	if enabled {
		v = 1
	}
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

// GetStatus 取启用状态，默认 true（出错或无记录时）。
func (c *Cookies) GetStatus(ctx context.Context, cookieID string) bool {
	var enabled int
	err := c.DB.QueryRowContext(ctx, `SELECT enabled FROM cookie_status WHERE cookie_id=?`, cookieID).Scan(&enabled)
	if err != nil {
		return true
	}
	return enabled != 0
}
