package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// accountTaskMaxRetries 是账号自动任务在首次执行失败后允许继续尝试的最大次数。
const accountTaskMaxRetries = 5

// accountTaskMaxAttempts 是包含首次执行在内的账号自动任务最大执行次数。
const accountTaskMaxAttempts = accountTaskMaxRetries + 1

// ErrAccountTaskRetryLimit 表示账号任务已经达到自动与人工共用的重试上限。
var ErrAccountTaskRetryLimit = errors.New("账号任务已达到最大重试次数")

// AccountTaskSettings 用于本次流程后续判断的账号任务设置
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

// AccountTaskRun 用于本次流程后续判断的账号任务运行
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
	// AttemptCount 是包含首次执行在内的累计执行次数，用于限制自动和人工重试。
	AttemptCount int   `json:"-"`
	NextRetryAt  int64 `json:"next_retry_at"`
	StartedAt    int64 `json:"started_at"`
	FinishedAt   int64 `json:"finished_at"`
}

// AccountTaskStore 用于本次流程后续判断的账号任务Store
type AccountTaskStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// defaultAccountTaskSettings 封装default账号任务设置业务协调。
func defaultAccountTaskSettings(cookieID string) AccountTaskSettings {
	return AccountTaskSettings{CookieID: cookieID, RateContent: "不错的买家，交易愉快", PolishTime: "03:00"}
}

// Get 读取当前值。
func (s *AccountTaskStore) Get(ctx context.Context, cookieID string) (AccountTaskSettings, error) {
	// result 用于本次流程后续判断的结果
	result := defaultAccountTaskSettings(cookieID)
	// rateEnabled、polishEnabled 用于本次流程后续判断的rateEnabled、polish启用状态
	var rateEnabled, polishEnabled int
	// err 用于本次流程后续判断的err
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

// Upsert 封装Upsert业务协调。
func (s *AccountTaskStore) Upsert(ctx context.Context, settings AccountTaskSettings) error {
	settings.RateContent = strings.TrimSpace(settings.RateContent)
	if settings.RateContent == "" {
		settings.RateContent = "不错的买家，交易愉快"
	}
	if settings.PolishTime == "" {
		settings.PolishTime = "03:00"
	}
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// query 用于本次流程后续判断的查询
	query := `INSERT INTO account_task_settings
		(cookie_id,auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,last_rate_scan_at,last_polish_date,last_polish_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)` + dialectUpsert(s.Dialect, []string{"cookie_id"}, map[string]string{
		"auto_rate_enabled": "EXCLUDED.auto_rate_enabled", "rate_content": "EXCLUDED.rate_content",
		"auto_polish_enabled": "EXCLUDED.auto_polish_enabled", "polish_time": "EXCLUDED.polish_time",
		"updated_at": "EXCLUDED.updated_at",
	})
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, query, settings.CookieID, boolInt(settings.AutoRateEnabled), settings.RateContent,
		boolInt(settings.AutoPolishEnabled), settings.PolishTime, settings.LastRateScanAt, settings.LastPolishDate,
		settings.LastPolishAt, now, now)
	return err
}

// Enabled 封装启用状态业务协调。
func (s *AccountTaskStore) Enabled(ctx context.Context) ([]AccountTaskSettings, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, `SELECT s.cookie_id,s.auto_rate_enabled,s.rate_content,s.auto_polish_enabled,s.polish_time,
		s.last_rate_scan_at,s.last_polish_date,s.last_polish_at
		FROM account_task_settings s JOIN cookies c ON c.id=s.cookie_id
		WHERE s.auto_rate_enabled=1 OR s.auto_polish_enabled=1 ORDER BY s.cookie_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 用于本次流程后续判断的结果
	var result []AccountTaskSettings
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row AccountTaskSettings
		// rate、polish 用于本次流程后续判断的rate、polish
		var rate, polish int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.CookieID, &rate, &row.RateContent, &polish, &row.PolishTime,
			&row.LastRateScanAt, &row.LastPolishDate, &row.LastPolishAt); err != nil {
			return nil, err
		}
		row.AutoRateEnabled, row.AutoPolishEnabled = rate != 0, polish != 0
		result = append(result, row)
	}
	return result, rows.Err()
}

// DueAutoRateOrderIDs 返回由买家确认收货 WebSocket 标记、尚可由本地自动评价任务消费的订单号。
// 它只读取本地 orders 表，绝不为了发现待评价订单向平台发起列表请求。
//
// 候选必须排除 claimRun 永远无法再领取的订单，否则评价成功后 completed_at 仍保留，已办完的订单会按时间升序
// 永久占满 limit 个名额，更新的订单进不了窗口（issue #96）。仍可领取的订单只有：尚无运行记录，或运行为 failed、
// 未达重试上限、未标记永久失败且已过重试冷却时间；其余（success、needs_review、running、重试用尽、永久失败、冷却中）都不返回。
func (s *AccountTaskStore) DueAutoRateOrderIDs(ctx context.Context, cookieID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	// rows、err 分别保存按确认收货时间排序的本地候选订单和查询错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT o.order_id FROM orders o
		WHERE o.cookie_id=? AND o.deleted_at IS NULL AND o.order_status='completed' AND o.completed_at<>''
		AND NOT EXISTS (SELECT 1 FROM account_task_runs r
			WHERE r.cookie_id=o.cookie_id AND r.task_type='auto_rate' AND r.target_id=o.order_id
			AND (r.status<>'failed' OR r.attempt_count>=? OR r.error_message LIKE ? OR r.next_retry_at>?))
		ORDER BY o.completed_at,o.order_id LIMIT ?`, cookieID, accountTaskMaxAttempts, NoRetryErrorPrefix+"%", time.Now().UTC().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// orderIDs 保存本轮允许尝试评价的本地已确认收货订单标识。
	orderIDs := make([]string, 0)
	for rows.Next() {
		// orderID 保存当前已确认收货订单的稳定业务标识。
		var orderID string
		// err 保存读取当前订单标识时的数据库扫描错误。
		if err := rows.Scan(&orderID); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	return orderIDs, rows.Err()
}

// ClaimRun creates a run or atomically reclaims a due failed run.
// ClaimRun 封装Claim运行业务协调。
func (s *AccountTaskStore) ClaimRun(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, false)
}

// ClaimRunImmediately creates a run or immediately reclaims a failed run. It is
// intended for an explicit user retry; scheduled workers should keep using
// ClaimRun so repeated platform failures still honor their retry delay.
// ClaimRunImmediately 封装Claim运行Immediately业务协调。
func (s *AccountTaskStore) ClaimRunImmediately(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, true)
}

// claimRun 封装claim运行业务协调。
func (s *AccountTaskStore) claimRun(ctx context.Context, run AccountTaskRun, now int64, immediate bool) (bool, error) {
	// retryCondition 限制累计执行次数、冷却时间，并排除已明确标记为永久失败的运行记录。
	retryCondition := "attempt_count<? AND next_retry_at<=? AND error_message NOT LIKE ?"
	// args 保存更新时间、运行键、最大执行次数、当前时间和永久失败前缀参数。
	args := []any{now, run.RunKey, accountTaskMaxAttempts, now, NoRetryErrorPrefix + "%"}
	if immediate {
		retryCondition = "attempt_count<? AND error_message NOT LIKE ?"
		args = []any{now, run.RunKey, accountTaskMaxAttempts, NoRetryErrorPrefix + "%"}
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status='running',attempt_count=attempt_count+1,started_at=?,finished_at=0,error_message=''
		WHERE run_key=? AND status='failed' AND `+retryCondition, args...)
	if err != nil {
		return false, err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	// query 用于本次流程后续判断的查询
	query := dialectInsertIgnorePrefix(s.Dialect) + ` INTO account_task_runs
		(run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,error_message,attempt_count,next_retry_at,started_at,finished_at)
		VALUES(?,?,?,?,?,'running',0,0,'',1,0,?,0)` + dialectInsertIgnore(s.Dialect, []string{"run_key"})
	res, err = s.DB.ExecContext(ctx, query, run.RunKey, run.CookieID, run.TaskType, run.TargetID, run.RunDate, now)
	if err != nil {
		return false, err
	}
	// n 用于本次流程后续判断的n
	n, _ := res.RowsAffected()
	if n == 0 && immediate {
		// attemptCount、status 保存人工重试未抢占时的累计次数和运行状态，用于判断是否达到上限。
		var (
			attemptCount int
			status       string
		)
		// queryErr 保存读取人工重试状态时的数据库错误。
		if queryErr := s.DB.QueryRowContext(ctx, `SELECT attempt_count,status FROM account_task_runs WHERE run_key=?`, run.RunKey).Scan(&attemptCount, &status); queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
			return false, queryErr
		}
		if status == "failed" && attemptCount >= accountTaskMaxAttempts {
			return false, ErrAccountTaskRetryLimit
		}
	}
	return n > 0, nil
}

// FinishRun 封装Finish运行业务协调。
func (s *AccountTaskStore) FinishRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status=?,success_count=?,failed_count=?,error_message=?,next_retry_at=CASE WHEN ?='failed' AND attempt_count>=? THEN 0 ELSE ? END,finished_at=? WHERE run_key=?`,
		status, success, failed, message, status, accountTaskMaxAttempts, nextRetryAt, time.Now().UTC().Unix(), runKey)
	return err
}

// MarkRateScan 封装MarkRateScan业务协调。
func (s *AccountTaskStore) MarkRateScan(ctx context.Context, cookieID string, at int64) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_rate_scan_at=?,updated_at=? WHERE cookie_id=?`, at, at, cookieID)
	return err
}

// MarkPolished 封装MarkPolished业务协调。
func (s *AccountTaskStore) MarkPolished(ctx context.Context, cookieID, date string, at int64) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_polish_date=?,last_polish_at=?,updated_at=? WHERE cookie_id=?`, date, at, at, cookieID)
	return err
}

// RecentRuns 封装Recent运行记录业务协调。
func (s *AccountTaskStore) RecentRuns(ctx context.Context, cookieID string, limit int) ([]AccountTaskRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, `SELECT id,run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,
		error_message,attempt_count,next_retry_at,started_at,finished_at FROM account_task_runs WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 用于本次流程后续判断的结果
	var result []AccountTaskRun
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row AccountTaskRun
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.ID, &row.RunKey, &row.CookieID, &row.TaskType, &row.TargetID, &row.RunDate,
			&row.Status, &row.SuccessCount, &row.FailedCount, &row.ErrorMessage, &row.AttemptCount, &row.NextRetryAt,
			&row.StartedAt, &row.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// boolInt 封装boolInt业务协调。
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
