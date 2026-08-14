package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrInvalidUserID 表示调用方没有提供可用于所有权过滤的正数用户 ID。
var ErrInvalidUserID = errors.New("user_id 必须大于 0")

// CookieSummary 表示不包含 Cookie、密码和加密 metadata 的账号摘要。
type CookieSummary struct {
	// ID 是闲鱼账号的稳定标识，不是 Cookie 明文。
	ID string
	// UserID 是账号所属的本地用户 ID。
	UserID int64
	// AutoConfirm 表示账号是否启用自动确认收货。
	AutoConfirm bool
	// Remark 是用户为账号设置的备注。
	Remark string
	// PauseDuration 是账号暂停时长，单位为分钟。
	PauseDuration int
	// PausedUntil 是暂停结束时间的 Unix 秒；0 表示当前未设置结束时间。
	PausedUntil int64
	// Username 是账号关联的登录用户名，不包含登录密码。
	Username string
	// ShowBrowser 表示密码登录流程是否允许显示浏览器。
	ShowBrowser bool
	// Nickname 是平台账号昵称缓存。
	Nickname string
	// AvatarURL 是平台账号头像缓存地址。
	AvatarURL string
	// LastRefreshAt 是最近一次资料刷新时间的 Unix 秒。
	LastRefreshAt int64
	// LoginMethod 是最近一次成功登录方式。
	LoginMethod string
	// LastLoginAt 是最近一次成功登录时间的 Unix 秒。
	LastLoginAt int64
	// CreatedAt 是账号记录创建时间的数据库字符串。
	CreatedAt string
}

// ListSummaries 返回指定用户的账号摘要，严格不读取或解密敏感凭证字段。
func (c *Cookies) ListSummaries(ctx context.Context, userID int64) ([]CookieSummary, error) {
	// ownerErr 表示用户 ID 未通过正数所有权校验的原因。
	if ownerErr := validateCookieOwnerID(userID); ownerErr != nil {
		return nil, ownerErr
	}
	// rows 是只选择非敏感列的账号摘要结果集。
	rows, err := c.DB.QueryContext(ctx, `
		SELECT id, user_id, auto_confirm, COALESCE(remark,''), pause_duration,
		       COALESCE(paused_until,0), COALESCE(username,''), show_browser,
		       COALESCE(nickname,''), COALESCE(avatar_url,''), COALESCE(last_refresh_at,0),
		       COALESCE(login_method,''), COALESCE(last_login_at,0), created_at
		FROM cookies WHERE user_id=? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// summaries 保存按创建时间倒序排列的非敏感账号摘要。
	summaries := make([]CookieSummary, 0)
	for rows.Next() {
		// summary 是当前数据库行对应的账号摘要。
		var summary CookieSummary
		// autoConfirm 和 showBrowser 将 SQLite 整数布尔值转换为 Go bool。
		var autoConfirm, showBrowser int
		// pauseDuration 允许兼容历史 NULL 值，同时保留默认暂停时长 10 分钟。
		var pauseDuration sql.NullInt64
		// scanErr 表示当前摘要行无法映射到非敏感模型的原因。
		if scanErr := rows.Scan(
			&summary.ID, &summary.UserID, &autoConfirm, &summary.Remark, &pauseDuration,
			&summary.PausedUntil, &summary.Username, &showBrowser, &summary.Nickname,
			&summary.AvatarURL, &summary.LastRefreshAt, &summary.LoginMethod,
			&summary.LastLoginAt, &summary.CreatedAt,
		); scanErr != nil {
			return nil, scanErr
		}
		summary.AutoConfirm = autoConfirm != 0
		summary.ShowBrowser = showBrowser != 0
		summary.PauseDuration = 10
		if pauseDuration.Valid {
			summary.PauseDuration = int(pauseDuration.Int64)
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// ListOwnedIDs 返回指定用户拥有的账号 ID，不读取 Cookie 明文或其他凭证字段。
func (c *Cookies) ListOwnedIDs(ctx context.Context, userID int64) ([]string, error) {
	// ownerErr 表示用户 ID 未通过正数所有权校验的原因。
	if ownerErr := validateCookieOwnerID(userID); ownerErr != nil {
		return nil, ownerErr
	}
	// rows 是只选择账号 ID 的所有权查询结果集。
	rows, err := c.DB.QueryContext(ctx, `SELECT id FROM cookies WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// cookieIDs 保存当前用户拥有的账号 ID。
	cookieIDs := make([]string, 0)
	for rows.Next() {
		// cookieID 是当前结果行的账号标识。
		var cookieID string
		// scanErr 表示当前账号 ID 无法从数据库行读取的原因。
		if scanErr := rows.Scan(&cookieID); scanErr != nil {
			return nil, scanErr
		}
		cookieIDs = append(cookieIDs, cookieID)
	}
	return cookieIDs, rows.Err()
}

// ExistsOwned 判断账号是否属于指定用户，仅返回存在性而不返回任何敏感值。
func (c *Cookies) ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	// ownerErr 表示用户 ID 未通过正数所有权校验的原因。
	if ownerErr := validateCookieOwnerID(userID); ownerErr != nil {
		return false, ownerErr
	}
	// exists 表示指定账号是否由指定用户拥有。
	var exists bool
	// queryErr 表示执行所有权存在性查询失败的原因。
	queryErr := c.DB.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM cookies WHERE id=? AND user_id=?)`, cookieID, userID).Scan(&exists)
	return exists, queryErr
}

// validateCookieOwnerID 拒绝使用 0 代表管理员的隐式所有权查询。
func validateCookieOwnerID(userID int64) error {
	if userID <= 0 {
		return ErrInvalidUserID
	}
	return nil
}
