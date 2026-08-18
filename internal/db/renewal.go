package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CookieRefreshSchedule 对应 cookie_refresh_schedules。
type CookieRefreshSchedule struct {
	CookieID            string
	ExpireAt            int64
	Disabled            bool
	ConsecutiveFailures int
	LastError           string
	LastStatus          string
	LastErrorMessage    string
	LastRefreshAt       int64
}

// RenewalLog 是三类续期任务日志的通用写入模型。
type RenewalLog struct {
	BatchID            string
	CookieID           string
	Status             string
	Message            string
	ErrorMessage       string
	UpdatedCookieNames []string
	UpdatedCookieCount int
	ResponseContent    string
	StepDetails        string
	RenewMethod        string
	DurationMS         int64
	RequestCount       int
	NextExpireAt       int64
}

// RenewalStore 保存续期任务计划与日志。
type RenewalStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// UpdateRenewalCookie 保存续期后的 Cookie，同时写入浏览器快照和最后续期时间。
func (c *Cookies) UpdateRenewalCookie(ctx context.Context, cookieID, cookieValue, metadataJSON string, lastRefreshAt int64) error {
	if strings.TrimSpace(cookieID) == "" {
		return errors.New("账号 ID 不能为空")
	}
	if lastRefreshAt <= 0 {
		lastRefreshAt = time.Now().Unix()
	}
	// encryptedCookie、err 用于本次流程后续判断的encryptedCookie、err
	encryptedCookie, err := c.codec.encrypt("cookie", cookieID, cookieValue)
	if err != nil {
		return fmt.Errorf("加密 Cookie: %w", err)
	}
	// encryptedMetadata、err 用于本次流程后续判断的encryptedMetadata、err
	encryptedMetadata, err := c.codec.encrypt(cookieMetadataScope, cookieID, metadataJSON)
	if err != nil {
		return fmt.Errorf("加密 Cookie metadata: %w", err)
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET value=?, metadata_json=?, last_refresh_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		encryptedCookie, encryptedMetadata, lastRefreshAt, cookieID)
	if err != nil {
		return err
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 1 {
		return fmt.Errorf("更新续期 Cookie 影响了 %d 行", rows)
	}
	if rows == 0 {
		// MySQL 默认报告“实际变更行数”，同值且同秒更新可能返回 0；
		// 只有记录确实不存在时才应映射为 ErrNotFound。
		// exists 用于本次流程后续判断的exists
		var exists bool
		if // err 用于本次流程后续判断的err
		err := c.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cookies WHERE id=?)`, cookieID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

// GetCookieRefreshSchedule 读取浏览器 Cookie 续期计划。
func (r *RenewalStore) GetCookieRefreshSchedule(ctx context.Context, cookieID string) (*CookieRefreshSchedule, error) {
	// s 用于本次流程后续判断的s
	var s CookieRefreshSchedule
	// disabled 用于本次流程后续判断的disabled
	var disabled int
	// err 用于本次流程后续判断的err
	err := r.DB.QueryRowContext(ctx,
		`SELECT cookie_id, expire_at, disabled, consecutive_failures, COALESCE(last_error,''),
		        COALESCE(last_status,''), COALESCE(last_error_message,''), COALESCE(last_refresh_at,0)
		 FROM cookie_refresh_schedules WHERE cookie_id=?`, cookieID).
		Scan(&s.CookieID, &s.ExpireAt, &disabled, &s.ConsecutiveFailures, &s.LastError,
			&s.LastStatus, &s.LastErrorMessage, &s.LastRefreshAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Disabled = disabled != 0
	return &s, nil
}

// UpsertCookieRefreshSchedule 写入浏览器 Cookie 续期计划。
func (r *RenewalStore) UpsertCookieRefreshSchedule(ctx context.Context, s CookieRefreshSchedule) error {
	// disabled 用于本次流程后续判断的disabled
	disabled := 0
	if s.Disabled {
		disabled = 1
	}
	// err 用于本次流程后续判断的err
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO cookie_refresh_schedules
		 (cookie_id, expire_at, disabled, consecutive_failures, last_error,
		  last_status, last_error_message, last_refresh_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`+
			dialectUpsert(r.Dialect, []string{"cookie_id"}, map[string]string{
				"expire_at":            "EXCLUDED.expire_at",
				"disabled":             "EXCLUDED.disabled",
				"consecutive_failures": "EXCLUDED.consecutive_failures",
				"last_error":           "EXCLUDED.last_error",
				"last_status":          "EXCLUDED.last_status",
				"last_error_message":   "EXCLUDED.last_error_message",
				"last_refresh_at":      "EXCLUDED.last_refresh_at",
				"updated_at":           "CURRENT_TIMESTAMP",
			}),
		s.CookieID, s.ExpireAt, disabled, s.ConsecutiveFailures, s.LastError,
		s.LastStatus, s.LastErrorMessage, s.LastRefreshAt)
	return err
}

// AddBrowserCookieRenewLog 记录浏览器 Cookie 续期日志。
func (r *RenewalStore) AddBrowserCookieRenewLog(ctx context.Context, log RenewalLog) error {
	if log.UpdatedCookieCount == 0 {
		log.UpdatedCookieCount = len(log.UpdatedCookieNames)
	}
	// err 用于本次流程后续判断的err
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO scheduled_cookies_refresh_log
		 (batch_id, cookie_id, status, message, error_message, updated_cookie_names,
		  updated_cookie_count, next_expire_at, step_details, renew_method, duration_ms,
		  request_count, response_content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		log.BatchID, log.CookieID, log.Status, log.Message, firstNonEmpty(log.ErrorMessage, log.Message),
		strings.Join(log.UpdatedCookieNames, ","), log.UpdatedCookieCount, log.NextExpireAt,
		log.StepDetails, log.RenewMethod, log.DurationMS, log.RequestCount, log.ResponseContent)
	return err
}

// AddLoginRenewLog 记录 login_renew 任务日志。
func (r *RenewalStore) AddLoginRenewLog(ctx context.Context, log RenewalLog) error {
	if log.UpdatedCookieCount == 0 {
		log.UpdatedCookieCount = len(log.UpdatedCookieNames)
	}
	// err 用于本次流程后续判断的err
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO scheduled_login_renew_log
		 (batch_id, cookie_id, status, message, error_message, updated_cookie_names,
		  updated_cookie_count, step_details, renew_method, duration_ms, request_count,
		  response_content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		log.BatchID, log.CookieID, log.Status, log.Message, firstNonEmpty(log.ErrorMessage, log.Message),
		strings.Join(log.UpdatedCookieNames, ","), log.UpdatedCookieCount, log.StepDetails,
		log.RenewMethod, log.DurationMS, log.RequestCount, log.ResponseContent)
	return err
}

// AddAPICookieRenewLog 记录 api_cookie_renew 任务日志。
func (r *RenewalStore) AddAPICookieRenewLog(ctx context.Context, log RenewalLog) error {
	if log.UpdatedCookieCount == 0 {
		log.UpdatedCookieCount = len(log.UpdatedCookieNames)
	}
	// err 用于本次流程后续判断的err
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO scheduled_api_cookie_renew_log
		 (batch_id, cookie_id, status, message, error_message, updated_cookie_names,
		  updated_cookie_count, response_content, step_details, renew_method, duration_ms,
		  request_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		log.BatchID, log.CookieID, log.Status, log.Message, firstNonEmpty(log.ErrorMessage, log.Message),
		strings.Join(log.UpdatedCookieNames, ","), log.UpdatedCookieCount, log.ResponseContent,
		log.StepDetails, log.RenewMethod, log.DurationMS, log.RequestCount)
	return err
}

// RecentAPICookieRenewStatuses 返回账号最近的 API Cookie 续期状态，最新记录在前。
func (r *RenewalStore) RecentAPICookieRenewStatuses(ctx context.Context, cookieID string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := r.DB.QueryContext(ctx,
		`SELECT status FROM scheduled_api_cookie_renew_log
		 WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// statuses 用于本次流程后续判断的statuses
	statuses := make([]string, 0, limit)
	for rows.Next() {
		// status 用于本次流程后续判断的状态
		var status string
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&status); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

// CleanupLogs deletes renewal logs older than retentionDays. Non-positive values skip cleanup.
// CleanupLogs 封装CleanupLogs业务协调。
func (r *RenewalStore) CleanupLogs(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	// cutoff 用于本次流程后续判断的cutoff
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	// table 表示当前遍历过程中的table
	for _, table := range []string{
		"scheduled_cookies_refresh_log",
		"scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log",
	} {
		if // err 用于本次流程后续判断的err
		_, err := r.DB.ExecContext(ctx, `DELETE FROM `+table+` WHERE created_at < ?`, cutoff); err != nil {
			return err
		}
	}
	return nil
}

// RecentBrowserCookieRenewStatuses 返回最近 limit 条浏览器续期日志状态。
func (r *RenewalStore) RecentBrowserCookieRenewStatuses(ctx context.Context, cookieID string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := r.DB.QueryContext(ctx,
		`SELECT status FROM scheduled_cookies_refresh_log
		 WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// statuses 用于本次流程后续判断的statuses
	var statuses []string
	for rows.Next() {
		// status 用于本次流程后续判断的状态
		var status string
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&status); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

// firstNonEmpty 封装firstNonEmpty业务协调。
func firstNonEmpty(values ...string) string {
	// v 表示当前遍历过程中的v
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// RenewalRuntimeAccount 表示续期调度器运行一次 Cookie 续期所需的最小账号视图，不包含登录密码或用户名。
type RenewalRuntimeAccount struct {
	// ID 是闲鱼账号的稳定标识，用于绑定续期任务和运行实例。
	ID string
	// Value 是 repository 解密后的 Cookie 明文，仅在续期调用和账号重启边界内短暂使用。
	Value string
	// Enabled 表示账号当前是否允许续期；状态由 cookie_status 缺省值推导为启用。
	Enabled bool
	// MetadataJSON 是 Cookie 快照等续期运行元数据，不包含登录密码。
	MetadataJSON string
}

// ActiveRenewalRuntimeAccounts 返回启用账号的续期运行视图，只解密 Cookie 和续期 metadata。
func (c *Cookies) ActiveRenewalRuntimeAccounts(ctx context.Context) ([]RenewalRuntimeAccount, error) {
	// rows 和 err 分别表示续期运行视图查询结果集及其数据库错误。
	rows, err := c.DB.QueryContext(ctx, `
		SELECT c.id, c.value, COALESCE(cs.enabled, 1), COALESCE(c.metadata_json,'')
		FROM cookies c
		LEFT JOIN cookie_status cs ON cs.cookie_id = c.id
		WHERE COALESCE(cs.enabled, 1) <> 0
		ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// accounts 保存按账号 ID 排序的启用续期运行视图。
	accounts := make([]RenewalRuntimeAccount, 0)
	for rows.Next() {
		// account 保存当前数据库行对应的续期账号。
		var account RenewalRuntimeAccount
		// enabledInt 保存数据库中的整数启用状态。
		var enabledInt int
		// encryptedValue 和 encryptedMetadata 保存仅供本次解密的数据库密文。
		var encryptedValue, encryptedMetadata string
		// scanErr 表示当前续期账号行无法映射到窄模型的原因。
		if scanErr := rows.Scan(&account.ID, &encryptedValue, &enabledInt, &encryptedMetadata); scanErr != nil {
			return nil, scanErr
		}
		account.Enabled = enabledInt != 0
		// decryptErr 表示当前账号 Cookie 或续期 metadata 无法解密的原因。
		var decryptErr error
		account.Value, decryptErr = c.codec.decrypt("cookie", account.ID, encryptedValue)
		if decryptErr != nil {
			return nil, fmt.Errorf("解密账号 %s Cookie: %w", account.ID, decryptErr)
		}
		account.MetadataJSON, decryptErr = c.codec.decrypt(cookieMetadataScope, account.ID, encryptedMetadata)
		if decryptErr != nil {
			return nil, fmt.Errorf("解密账号 %s Cookie metadata: %w", account.ID, decryptErr)
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

// GetRenewalRuntimeAccount 返回指定账号的续期运行视图，并原子读取当前启用状态。
func (c *Cookies) GetRenewalRuntimeAccount(ctx context.Context, cookieID string) (RenewalRuntimeAccount, error) {
	// account 保存按账号 ID 读取的续期窄模型。
	var account RenewalRuntimeAccount
	// enabledInt 保存数据库中的整数启用状态。
	var enabledInt int
	// encryptedValue 和 encryptedMetadata 保存仅供本次解密的数据库密文。
	var encryptedValue, encryptedMetadata string
	// queryErr 表示账号不存在或续期窄查询执行失败的原因。
	if queryErr := c.DB.QueryRowContext(ctx, `
		SELECT c.id, c.value, COALESCE(cs.enabled, 1), COALESCE(c.metadata_json,'')
		FROM cookies c
		LEFT JOIN cookie_status cs ON cs.cookie_id = c.id
		WHERE c.id=?`, cookieID).Scan(&account.ID, &encryptedValue, &enabledInt, &encryptedMetadata); queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return RenewalRuntimeAccount{}, ErrNotFound
		}
		return RenewalRuntimeAccount{}, queryErr
	}
	account.Enabled = enabledInt != 0
	// decryptErr 表示当前账号 Cookie 或续期 metadata 无法解密的原因。
	var decryptErr error
	account.Value, decryptErr = c.codec.decrypt("cookie", account.ID, encryptedValue)
	if decryptErr != nil {
		return RenewalRuntimeAccount{}, fmt.Errorf("解密账号 %s Cookie: %w", account.ID, decryptErr)
	}
	account.MetadataJSON, decryptErr = c.codec.decrypt(cookieMetadataScope, account.ID, encryptedMetadata)
	if decryptErr != nil {
		return RenewalRuntimeAccount{}, fmt.Errorf("解密账号 %s Cookie metadata: %w", account.ID, decryptErr)
	}
	return account, nil
}
