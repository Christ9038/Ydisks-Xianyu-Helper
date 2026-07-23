package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RenewalAccount 是续期调度所需的账号视图。
type RenewalAccount struct {
	ID            string
	Value         string
	UserID        int64
	Enabled       bool
	DisableReason string
	Username      string
	Password      string
	ShowBrowser   bool
	MetadataJSON  string
	LastRefreshAt int64
}

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

// AllRenewalAccounts 返回所有账号，包含启用状态；浏览器 cookie 续期会用到禁用账号。
func (c *Cookies) AllRenewalAccounts(ctx context.Context) ([]RenewalAccount, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT c.id, c.value, c.user_id, COALESCE(cs.enabled, 1), COALESCE(cs.disable_reason,''),
			        COALESCE(c.username,''), COALESCE(c.password,''), COALESCE(c.show_browser,0),
		        COALESCE(c.metadata_json,''), COALESCE(c.last_refresh_at,0)
		 FROM cookies c
		 LEFT JOIN cookie_status cs ON cs.cookie_id = c.id
		 ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return c.scanRenewalAccounts(rows)
}

// ActiveRenewalAccounts 返回启用账号，用于 WS/API 续期类任务。
func (c *Cookies) ActiveRenewalAccounts(ctx context.Context) ([]RenewalAccount, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT c.id, c.value, c.user_id, COALESCE(cs.enabled, 1), COALESCE(cs.disable_reason,''),
			        COALESCE(c.username,''), COALESCE(c.password,''), COALESCE(c.show_browser,0),
		        COALESCE(c.metadata_json,''), COALESCE(c.last_refresh_at,0)
		 FROM cookies c
		 LEFT JOIN cookie_status cs ON cs.cookie_id = c.id
		 WHERE COALESCE(cs.enabled, 1) <> 0
		 ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return c.scanRenewalAccounts(rows)
}

func (c *Cookies) scanRenewalAccounts(rows *sql.Rows) ([]RenewalAccount, error) {
	var out []RenewalAccount
	for rows.Next() {
		var a RenewalAccount
		var enabled, showBrowser int
		if err := rows.Scan(&a.ID, &a.Value, &a.UserID, &enabled, &a.DisableReason, &a.Username, &a.Password, &showBrowser, &a.MetadataJSON, &a.LastRefreshAt); err != nil {
			return nil, err
		}
		a.Enabled = enabled != 0
		a.ShowBrowser = showBrowser != 0
		var err error
		a.Value, err = c.codec.decrypt("cookie", a.ID, a.Value)
		if err != nil {
			return nil, fmt.Errorf("解密账号 %s Cookie: %w", a.ID, err)
		}
		a.Password, err = c.codec.decrypt("login-password", a.ID, a.Password)
		if err != nil {
			return nil, fmt.Errorf("解密账号 %s 登录密码: %w", a.ID, err)
		}
		a.MetadataJSON, err = c.codec.decrypt(cookieMetadataScope, a.ID, a.MetadataJSON)
		if err != nil {
			return nil, fmt.Errorf("解密账号 %s Cookie metadata: %w", a.ID, err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateRenewalCookie 保存续期后的 Cookie，同时写入浏览器快照和最后续期时间。
func (c *Cookies) UpdateRenewalCookie(ctx context.Context, cookieID, cookieValue, metadataJSON string, lastRefreshAt int64) error {
	if strings.TrimSpace(cookieID) == "" {
		return errors.New("账号 ID 不能为空")
	}
	if lastRefreshAt <= 0 {
		lastRefreshAt = time.Now().Unix()
	}
	encryptedCookie, err := c.codec.encrypt("cookie", cookieID, cookieValue)
	if err != nil {
		return fmt.Errorf("加密 Cookie: %w", err)
	}
	encryptedMetadata, err := c.codec.encrypt(cookieMetadataScope, cookieID, metadataJSON)
	if err != nil {
		return fmt.Errorf("加密 Cookie metadata: %w", err)
	}
	res, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET value=?, metadata_json=?, last_refresh_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		encryptedCookie, encryptedMetadata, lastRefreshAt, cookieID)
	if err != nil {
		return err
	}
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
		var exists bool
		if err := c.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cookies WHERE id=?)`, cookieID).Scan(&exists); err != nil {
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
	var s CookieRefreshSchedule
	var disabled int
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
	disabled := 0
	if s.Disabled {
		disabled = 1
	}
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

// CleanupLogs deletes renewal logs older than retentionDays. Non-positive values skip cleanup.
func (r *RenewalStore) CleanupLogs(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, table := range []string{
		"scheduled_cookies_refresh_log",
		"scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log",
	} {
		if _, err := r.DB.ExecContext(ctx, `DELETE FROM `+table+` WHERE created_at < ?`, cutoff); err != nil {
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
	rows, err := r.DB.QueryContext(ctx,
		`SELECT status FROM scheduled_cookies_refresh_log
		 WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var statuses []string
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
