package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// RenewalAccount 是续期调度所需的账号视图。
type RenewalAccount struct {
	ID            string
	Value         string
	UserID        int64
	Enabled       bool
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
		`SELECT c.id, c.value, c.user_id, COALESCE(cs.enabled, 1),
		        COALESCE(c.username,''), COALESCE(c.password,''), COALESCE(c.show_browser,0),
		        COALESCE(c.metadata_json,''), COALESCE(c.last_refresh_at,0)
		 FROM cookies c
		 LEFT JOIN cookie_status cs ON cs.cookie_id = c.id
		 ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRenewalAccounts(rows)
}

// ActiveRenewalAccounts 返回启用账号，用于 WS/API 续期类任务。
func (c *Cookies) ActiveRenewalAccounts(ctx context.Context) ([]RenewalAccount, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT c.id, c.value, c.user_id, COALESCE(cs.enabled, 1),
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
	return scanRenewalAccounts(rows)
}

func scanRenewalAccounts(rows *sql.Rows) ([]RenewalAccount, error) {
	var out []RenewalAccount
	for rows.Next() {
		var a RenewalAccount
		var enabled, showBrowser int
		if err := rows.Scan(&a.ID, &a.Value, &a.UserID, &enabled, &a.Username, &a.Password, &showBrowser, &a.MetadataJSON, &a.LastRefreshAt); err != nil {
			return nil, err
		}
		a.Enabled = enabled != 0
		a.ShowBrowser = showBrowser != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateRenewalCookie 保存续期后的 Cookie，同时写入浏览器快照和最后续期时间。
func (c *Cookies) UpdateRenewalCookie(ctx context.Context, cookieID, cookieValue, metadataJSON string, lastRefreshAt int64) error {
	if lastRefreshAt <= 0 {
		lastRefreshAt = time.Now().Unix()
	}
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cookies
		 SET value=?, metadata_json=?, last_refresh_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		cookieValue, metadataJSON, lastRefreshAt, cookieID)
	if err != nil {
		return err
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
		  updated_cookie_count, next_expire_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		log.BatchID, log.CookieID, log.Status, log.Message, firstNonEmpty(log.ErrorMessage, log.Message),
		strings.Join(log.UpdatedCookieNames, ","), log.UpdatedCookieCount, log.NextExpireAt)
	return err
}

// AddLoginRenewLog 记录 login_renew 任务日志。
func (r *RenewalStore) AddLoginRenewLog(ctx context.Context, log RenewalLog) error {
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO scheduled_login_renew_log
		 (batch_id, cookie_id, status, message, error_message, updated_cookie_names, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		log.BatchID, log.CookieID, log.Status, log.Message, firstNonEmpty(log.ErrorMessage, log.Message),
		strings.Join(log.UpdatedCookieNames, ","))
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
		  updated_cookie_count, response_content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		log.BatchID, log.CookieID, log.Status, log.Message, firstNonEmpty(log.ErrorMessage, log.Message),
		strings.Join(log.UpdatedCookieNames, ","), log.UpdatedCookieCount, log.ResponseContent)
	return err
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
