package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrInvalidUserID 表示调用方没有提供可用于所有权过滤的正数用户 ID。
var ErrInvalidUserID = errors.New("user_id 必须大于 0")

// RuntimeCookieCredential 表示账号运行实例启动所需的最小凭证视图；Value 仅允许在启动流程内部短暂传递，不得写入日志、响应或持久化状态。
type RuntimeCookieCredential struct {
	// ID 是闲鱼账号的稳定标识，用于绑定运行实例和解密作用域。
	ID string
	// Value 是已经由 repository 解密的 Cookie 明文，仅供账号运行时使用。
	Value string
}

// ListEnabledRuntimeCredentials 返回所有启用账号的运行时 Cookie 凭证；该系统启动视图只选择并解密 Cookie，不读取密码、metadata 或其他账号资料。
func (c *Cookies) ListEnabledRuntimeCredentials(ctx context.Context) ([]RuntimeCookieCredential, error) {
	// rows 是按启用状态过滤后仅包含账号 ID 和 Cookie 密文的查询结果集。
	rows, err := c.DB.QueryContext(ctx, `
		SELECT c.id, c.value
		FROM cookies c
		LEFT JOIN cookie_status cs ON cs.cookie_id = c.id
		WHERE COALESCE(cs.enabled, 1) <> 0
		ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// credentials 保存供账号 supervisor 启动运行实例的最小明文凭证集合。
	credentials := make([]RuntimeCookieCredential, 0)
	for rows.Next() {
		// credential 保存当前行的账号 ID 和待解密 Cookie。
		var credential RuntimeCookieCredential
		// encryptedValue 保存数据库中按账号作用域加密的 Cookie 密文。
		var encryptedValue string
		// scanErr 表示当前运行时凭证行无法映射到模型的原因。
		if scanErr := rows.Scan(&credential.ID, &encryptedValue); scanErr != nil {
			return nil, scanErr
		}
		// decryptErr 表示当前账号 Cookie 密文无法用 repository 的数据密钥解密。
		var decryptErr error
		credential.Value, decryptErr = c.codec.decrypt("cookie", credential.ID, encryptedValue)
		if decryptErr != nil {
			return nil, fmt.Errorf("解密账号 %s Cookie: %w", credential.ID, decryptErr)
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

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

// GetSummaryOwned 返回指定用户拥有的单个账号摘要，不读取或解密任何敏感凭证。
func (c *Cookies) GetSummaryOwned(ctx context.Context, userID int64, cookieID string) (CookieSummary, error) {
	// ownerErr 表示用户 ID 未通过正数所有权校验的原因。
	if ownerErr := validateCookieOwnerID(userID); ownerErr != nil {
		return CookieSummary{}, ownerErr
	}
	// summary 保存按账号和用户联合过滤得到的非敏感摘要。
	var summary CookieSummary
	// autoConfirm 和 showBrowser 将 SQLite 整数布尔值转换为 Go bool。
	var autoConfirm, showBrowser int
	// pauseDuration 允许兼容历史 NULL 值，同时保留默认暂停时长 10 分钟。
	var pauseDuration sql.NullInt64
	// queryErr 表示按账号和用户联合条件读取摘要失败的原因。
	queryErr := c.DB.QueryRowContext(ctx, `
		SELECT id, user_id, auto_confirm, COALESCE(remark,''), pause_duration,
		       COALESCE(paused_until,0), COALESCE(username,''), show_browser,
		       COALESCE(nickname,''), COALESCE(avatar_url,''), COALESCE(last_refresh_at,0),
		       COALESCE(login_method,''), COALESCE(last_login_at,0), created_at
		FROM cookies WHERE id=? AND user_id=?`, cookieID, userID).Scan(
		&summary.ID, &summary.UserID, &autoConfirm, &summary.Remark, &pauseDuration,
		&summary.PausedUntil, &summary.Username, &showBrowser, &summary.Nickname,
		&summary.AvatarURL, &summary.LastRefreshAt, &summary.LoginMethod,
		&summary.LastLoginAt, &summary.CreatedAt)
	if queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return CookieSummary{}, ErrNotFound
		}
		return CookieSummary{}, queryErr
	}
	summary.AutoConfirm = autoConfirm != 0
	summary.ShowBrowser = showBrowser != 0
	summary.PauseDuration = 10
	if pauseDuration.Valid {
		summary.PauseDuration = int(pauseDuration.Int64)
	}
	return summary, nil
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

// GetOwnerID 返回指定账号的所有者 ID，不读取或解密账号凭证。
func (c *Cookies) GetOwnerID(ctx context.Context, cookieID string) (int64, error) {
	// ownerID 保存数据库中指定账号的所有者 ID。
	var ownerID int64
	// queryErr 表示按账号 ID 查询所有者失败的原因。
	if queryErr := c.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&ownerID); queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, queryErr
	}
	return ownerID, nil
}

// GetValueOwned 原子查询并解密指定用户拥有的单个账号 Cookie，避免先查所有权再读取凭证的竞态窗口。
func (c *Cookies) GetValueOwned(ctx context.Context, userID int64, cookieID string) (string, error) {
	// ownerErr 表示用户 ID 未通过正数所有权校验的原因。
	if ownerErr := validateCookieOwnerID(userID); ownerErr != nil {
		return "", ownerErr
	}
	// encryptedValue 保存经过所有权过滤后读取到的单个 Cookie 密文。
	var encryptedValue string
	// queryErr 表示按账号和用户联合条件读取 Cookie 失败的原因。
	if queryErr := c.DB.QueryRowContext(ctx,
		`SELECT value FROM cookies WHERE id=? AND user_id=?`, cookieID, userID).Scan(&encryptedValue); queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", queryErr
	}
	return c.codec.decrypt("cookie", cookieID, encryptedValue)
}

// validateCookieOwnerID 拒绝使用 0 代表管理员的隐式所有权查询。
func validateCookieOwnerID(userID int64) error {
	if userID <= 0 {
		return ErrInvalidUserID
	}
	return nil
}

// CookieRuntimeData 表示运行时操作所需的 Cookie 与 metadata，不包含登录密码或账号资料。
type CookieRuntimeData struct {
	// Value 是 repository 解密后的 Cookie 明文，仅在受控的运行时操作中短暂使用，不得写入日志或响应。
	Value string
	// MetadataJSON 是 repository 解密后的 Cookie 运行 metadata，用于恢复或识别完整 Cookie Jar 的变化。
	MetadataJSON string
}

// GetCookieRuntimeData 返回运行时所需的最小 Cookie 与 metadata 字段，并严格跳过登录密码、用户名等其他列。
func (c *Cookies) GetCookieRuntimeData(ctx context.Context, cookieID string) (CookieRuntimeData, error) {
	// data 保存按账号 ID 查询到的运行时字段。
	var data CookieRuntimeData
	// encryptedValue 和 encryptedMetadata 保存仅供本次解密的数据库密文。
	var encryptedValue, encryptedMetadata string
	// queryErr 表示账号不存在或指纹输入查询失败的原因。
	if queryErr := c.DB.QueryRowContext(ctx,
		`SELECT value, COALESCE(metadata_json,'') FROM cookies WHERE id=?`, cookieID).
		Scan(&encryptedValue, &encryptedMetadata); queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return CookieRuntimeData{}, ErrNotFound
		}
		return CookieRuntimeData{}, queryErr
	}
	// decryptErr 表示 Cookie 或 metadata 密文无法解密的原因。
	var decryptErr error
	data.Value, decryptErr = c.codec.decrypt("cookie", cookieID, encryptedValue)
	if decryptErr != nil {
		return CookieRuntimeData{}, fmt.Errorf("解密账号 %s Cookie: %w", cookieID, decryptErr)
	}
	data.MetadataJSON, decryptErr = c.codec.decrypt(cookieMetadataScope, cookieID, encryptedMetadata)
	if decryptErr != nil {
		return CookieRuntimeData{}, fmt.Errorf("解密账号 %s Cookie metadata: %w", cookieID, decryptErr)
	}
	return data, nil
}
