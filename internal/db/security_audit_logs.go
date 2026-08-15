package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SecurityAuditLog 记录敏感配置访问动作，但不保存秘密值或秘密内容。
type SecurityAuditLog struct {
	// ID 是审计记录标识。
	ID int64
	// UserID 是执行操作的管理员用户标识。
	UserID int64
	// Action 是敏感操作类型。
	Action string
	// Resource 是被访问的资源名称。
	Resource string
	// Keys 是本次访问涉及的敏感配置键名称。
	Keys []string
	// Outcome 是操作结果，如 accepted 或 rejected。
	Outcome string
	// CreatedAt 是审计记录创建时间的 Unix 秒。
	CreatedAt int64
}

// SecurityAuditLogs 管理敏感配置访问审计记录。
type SecurityAuditLogs struct {
	// DB 是审计记录使用的数据库连接。
	DB *sql.DB
}

// Add 写入一条不包含敏感值的访问审计记录。
func (s *SecurityAuditLogs) Add(ctx context.Context, log SecurityAuditLog) error {
	if s == nil || s.DB == nil {
		return errors.New("敏感访问审计存储未初始化")
	}
	if log.UserID <= 0 || log.Action == "" || log.Resource == "" {
		return errors.New("敏感访问审计字段无效")
	}
	if log.Outcome == "" {
		log.Outcome = "accepted"
	}
	if log.CreatedAt == 0 {
		log.CreatedAt = time.Now().Unix()
	}
	// keysJSON 保存敏感键名称列表，不保存对应的秘密值。
	keysJSON, err := json.Marshal(log.Keys)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO security_audit_logs (user_id,action,resource,keys_json,outcome,created_at) VALUES (?,?,?,?,?,?)`, log.UserID, log.Action, log.Resource, string(keysJSON), log.Outcome, log.CreatedAt)
	return err
}

// ListByUser 返回指定用户最近的敏感访问审计记录，供运维审计查询使用。
func (s *SecurityAuditLogs) ListByUser(ctx context.Context, userID int64, limit int) ([]SecurityAuditLog, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("敏感访问审计存储未初始化")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// rows、err 保存审计记录查询结果集及查询错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT id,user_id,action,resource,keys_json,outcome,created_at FROM security_audit_logs WHERE user_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 保存查询到的审计记录。
	records := make([]SecurityAuditLog, 0)
	for rows.Next() {
		// record 保存当前扫描的审计记录。
		var record SecurityAuditLog
		// keysJSON 保存数据库中的敏感键名称 JSON。
		var keysJSON string
		// err 保存审计记录字段扫描错误。
		if err := rows.Scan(&record.ID, &record.UserID, &record.Action, &record.Resource, &keysJSON, &record.Outcome, &record.CreatedAt); err != nil {
			return nil, err
		}
		// err 保存敏感键名称 JSON 解析错误。
		if err := json.Unmarshal([]byte(keysJSON), &record.Keys); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	// err 保存审计记录结果集遍历错误。
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
