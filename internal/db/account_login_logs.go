package db

import (
	"context"
	"database/sql"
)

// AccountLoginLogs 管理账号登录审计记录。
type AccountLoginLogs struct {
	DB *sql.DB
}

// Add 写入一条账号登录审计记录。
func (l *AccountLoginLogs) Add(ctx context.Context, log AccountLoginLog) error {
	if log.AccountIdentifier == "" {
		log.AccountIdentifier = log.CookieID
	}
	if log.OwnerID == 0 {
		log.OwnerID = log.UserID
	}
	if log.ErrorMessage == "" {
		log.ErrorMessage = log.Message
	}
	// err 表示写入账号登录审计记录的数据库错误；记录内容不包含 Cookie 或密码明文。
	_, err := l.DB.ExecContext(ctx,
		`INSERT INTO account_login_logs (
			cookie_id, user_id, owner_id, account_pk, account_identifier,
			username, method, status, message, trigger_reason, failure_reason,
			error_message, updated_cookie_names, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.CookieID, log.UserID, log.OwnerID, log.AccountPK, log.AccountIdentifier,
		log.Username, log.Method, log.Status, log.Message, log.TriggerReason, log.FailureReason,
		log.ErrorMessage, log.UpdatedCookieNames, log.DurationMS, log.CreatedAt)
	return err
}

// ListByCookie 按账号倒序读取登录审计记录。
func (l *AccountLoginLogs) ListByCookie(ctx context.Context, cookieID string, limit int) ([]AccountLoginLog, error) {
	if limit <= 0 {
		limit = 20
	}
	// rows 是按账号倒序读取的审计记录游标；err 表示查询启动失败。
	rows, err := l.DB.QueryContext(ctx,
		`SELECT id, cookie_id, user_id, COALESCE(owner_id,0), COALESCE(account_pk,0),
		        COALESCE(account_identifier,''), COALESCE(username,''), method, status,
		        COALESCE(message,''), COALESCE(trigger_reason,''), COALESCE(failure_reason,''),
		        COALESCE(error_message,''), COALESCE(updated_cookie_names,''), COALESCE(duration_ms,0),
		        created_at
		 FROM account_login_logs
		 WHERE cookie_id=?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 收集当前账号的登录审计记录，并按数据库返回顺序保持时间倒序。
	var out []AccountLoginLog
	for rows.Next() {
		// log 接收当前游标行的非敏感登录审计字段。
		var log AccountLoginLog
		// err 表示当前审计记录行的字段解码错误。
		if err := rows.Scan(
			&log.ID, &log.CookieID, &log.UserID, &log.OwnerID, &log.AccountPK,
			&log.AccountIdentifier, &log.Username, &log.Method, &log.Status,
			&log.Message, &log.TriggerReason, &log.FailureReason, &log.ErrorMessage,
			&log.UpdatedCookieNames, &log.DurationMS, &log.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, log)
	}
	return out, rows.Err()
}
