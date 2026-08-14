package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// AccountTaskSettings 保存账号任务设置，供当前处理流程使用
type AccountTaskSettings struct {
	CookieID          string `json:"account_id"`
	AutoRateEnabled   bool   `json:"auto_rate_enabled"`
	RateContent       string `json:"rate_content"`
	AutoPolishEnabled bool   `json:"auto_polish_enabled"`
	PolishTime        string `json:"polish_time"`
	LastRateScanAt    int64  `json:"last_rate_scan_at"`
	LastPolishDate    string `json:"last_polish_date"`
	LastPolishAt      int64  `json:"last_polish_at"`
}

// AccountTaskRun 保存账号任务运行，供当前处理流程使用
type AccountTaskRun struct {
	ID           int64  `json:"id"`
	RunKey       string `json:"run_key"`
	CookieID     string `json:"account_id"`
	TaskType     string `json:"task_type"`
	TargetID     string `json:"target_id"`
	RunDate      string `json:"run_date"`
	Status       string `json:"status"`
	SuccessCount int    `json:"success_count"`
	FailedCount  int    `json:"failed_count"`
	ErrorMessage string `json:"error_message"`
	NextRetryAt  int64  `json:"next_retry_at"`
	StartedAt    int64  `json:"started_at"`
	FinishedAt   int64  `json:"finished_at"`
}

// AccountTaskStore 保存账号任务Store，供当前处理流程使用
type AccountTaskStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// defaultAccountTaskSettings 负责default账号任务设置相关处理。
func defaultAccountTaskSettings(cookieID string) AccountTaskSettings {
	return AccountTaskSettings{CookieID: cookieID, RateContent: "不错的买家，交易愉快", PolishTime: "03:00"}
}

// Get 读取当前值。
func (s *AccountTaskStore) Get(ctx context.Context, cookieID string) (AccountTaskSettings, error) {
	// result 保存结果，供当前处理流程使用
	result := defaultAccountTaskSettings(cookieID)
	// rateEnabled、polishEnabled 保存rateEnabled、polish启用状态，供当前处理流程使用
	var rateEnabled, polishEnabled int
	// err 保存err，供当前处理流程使用
	err := s.DB.QueryRowContext(ctx, `SELECT auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,
		last_rate_scan_at,last_polish_date,last_polish_at FROM account_task_settings WHERE cookie_id=?`, cookieID).Scan(
		&rateEnabled, &result.RateContent, &polishEnabled, &result.PolishTime, &result.LastRateScanAt,
		&result.LastPolishDate, &result.LastPolishAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	result.AutoRateEnabled = rateEnabled != 0
	result.AutoPolishEnabled = polishEnabled != 0
	return result, err
}

// Upsert 负责Upsert相关处理。
func (s *AccountTaskStore) Upsert(ctx context.Context, settings AccountTaskSettings) error {
	settings.RateContent = strings.TrimSpace(settings.RateContent)
	if settings.RateContent == "" {
		settings.RateContent = "不错的买家，交易愉快"
	}
	if settings.PolishTime == "" {
		settings.PolishTime = "03:00"
	}
	// now 保存now，供当前处理流程使用
	now := time.Now().UTC().Unix()
	// query 保存查询，供当前处理流程使用
	query := `INSERT INTO account_task_settings
		(cookie_id,auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,last_rate_scan_at,last_polish_date,last_polish_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)` + dialectUpsert(s.Dialect, []string{"cookie_id"}, map[string]string{
		"auto_rate_enabled": "EXCLUDED.auto_rate_enabled", "rate_content": "EXCLUDED.rate_content",
		"auto_polish_enabled": "EXCLUDED.auto_polish_enabled", "polish_time": "EXCLUDED.polish_time",
		"updated_at": "EXCLUDED.updated_at",
	})
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, query, settings.CookieID, boolInt(settings.AutoRateEnabled), settings.RateContent,
		boolInt(settings.AutoPolishEnabled), settings.PolishTime, settings.LastRateScanAt, settings.LastPolishDate,
		settings.LastPolishAt, now, now)
	return err
}

// Enabled 负责启用状态相关处理。
func (s *AccountTaskStore) Enabled(ctx context.Context) ([]AccountTaskSettings, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.DB.QueryContext(ctx, `SELECT s.cookie_id,s.auto_rate_enabled,s.rate_content,s.auto_polish_enabled,s.polish_time,
		s.last_rate_scan_at,s.last_polish_date,s.last_polish_at
		FROM account_task_settings s JOIN cookies c ON c.id=s.cookie_id
		WHERE s.auto_rate_enabled=1 OR s.auto_polish_enabled=1 ORDER BY s.cookie_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 保存结果，供当前处理流程使用
	var result []AccountTaskSettings
	for rows.Next() {
		// row 保存row，供当前处理流程使用
		var row AccountTaskSettings
		// rate、polish 保存rate、polish，供当前处理流程使用
		var rate, polish int
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&row.CookieID, &rate, &row.RateContent, &polish, &row.PolishTime,
			&row.LastRateScanAt, &row.LastPolishDate, &row.LastPolishAt); err != nil {
			return nil, err
		}
		row.AutoRateEnabled, row.AutoPolishEnabled = rate != 0, polish != 0
		result = append(result, row)
	}
	return result, rows.Err()
}

// ClaimRun creates a run or atomically reclaims a due failed run.
// ClaimRun 负责Claim运行相关处理。
func (s *AccountTaskStore) ClaimRun(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, false)
}

// ClaimRunImmediately creates a run or immediately reclaims a failed run. It is
// intended for an explicit user retry; scheduled workers should keep using
// ClaimRun so repeated platform failures still honor their retry delay.
// ClaimRunImmediately 负责Claim运行Immediately相关处理。
func (s *AccountTaskStore) ClaimRunImmediately(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, true)
}

// claimRun 负责claim运行相关处理。
func (s *AccountTaskStore) claimRun(ctx context.Context, run AccountTaskRun, now int64, immediate bool) (bool, error) {
	// retryCondition 保存重试Condition，供当前处理流程使用
	retryCondition := "next_retry_at<=?"
	// args 保存args，供当前处理流程使用
	args := []any{now, run.RunKey, now}
	if immediate {
		retryCondition = "1=1"
		args = args[:2]
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status='running',started_at=?,finished_at=0,error_message=''
		WHERE run_key=? AND status='failed' AND `+retryCondition, args...)
	if err != nil {
		return false, err
	}
	if // n 保存n，供当前处理流程使用
	n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	// query 保存查询，供当前处理流程使用
	query := dialectInsertIgnorePrefix(s.Dialect) + ` INTO account_task_runs
		(run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,error_message,next_retry_at,started_at,finished_at)
		VALUES(?,?,?,?,?,'running',0,0,'',0,?,0)` + dialectInsertIgnore(s.Dialect, []string{"run_key"})
	res, err = s.DB.ExecContext(ctx, query, run.RunKey, run.CookieID, run.TaskType, run.TargetID, run.RunDate, now)
	if err != nil {
		return false, err
	}
	// n 保存n，供当前处理流程使用
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FinishRun 负责Finish运行相关处理。
func (s *AccountTaskStore) FinishRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status=?,success_count=?,failed_count=?,error_message=?,next_retry_at=?,finished_at=? WHERE run_key=?`,
		status, success, failed, message, nextRetryAt, time.Now().UTC().Unix(), runKey)
	return err
}

// MarkRateScan 负责MarkRateScan相关处理。
func (s *AccountTaskStore) MarkRateScan(ctx context.Context, cookieID string, at int64) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_rate_scan_at=?,updated_at=? WHERE cookie_id=?`, at, at, cookieID)
	return err
}

// MarkPolished 负责MarkPolished相关处理。
func (s *AccountTaskStore) MarkPolished(ctx context.Context, cookieID, date string, at int64) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_polish_date=?,last_polish_at=?,updated_at=? WHERE cookie_id=?`, date, at, at, cookieID)
	return err
}

// RecentRuns 负责Recent运行记录相关处理。
func (s *AccountTaskStore) RecentRuns(ctx context.Context, cookieID string, limit int) ([]AccountTaskRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.DB.QueryContext(ctx, `SELECT id,run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,
		error_message,next_retry_at,started_at,finished_at FROM account_task_runs WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 保存结果，供当前处理流程使用
	var result []AccountTaskRun
	for rows.Next() {
		// row 保存row，供当前处理流程使用
		var row AccountTaskRun
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&row.ID, &row.RunKey, &row.CookieID, &row.TaskType, &row.TargetID, &row.RunDate,
			&row.Status, &row.SuccessCount, &row.FailedCount, &row.ErrorMessage, &row.NextRetryAt,
			&row.StartedAt, &row.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// boolInt 负责boolInt相关处理。
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
