package db

import (
	"context"
	"database/sql"
)

// RiskControlLogs 写入风控处理日志。
type RiskControlLogs struct {
	DB      *sql.DB
	Dialect Dialect
}

// Add 新增一条风控日志，返回自增 ID。
func (r *RiskControlLogs) Add(ctx context.Context, log RiskControlLog) (int64, error) {
	if log.EventType == "" {
		log.EventType = "slider_captcha"
	}
	if log.ProcessingStatus == "" {
		log.ProcessingStatus = "processing"
	}
	return insertReturningID(ctx, r.DB, r.Dialect,
		`INSERT INTO risk_control_logs
		 (cookie_id, event_type, event_description, processing_result,
		  processing_status, captcha_engine, error_message, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		log.CookieID, log.EventType, log.EventDescription, log.ProcessingResult,
		log.ProcessingStatus, log.CaptchaEngine, log.ErrorMessage, log.DurationMS)
}

// Update 更新风控日志处理结果。ID 为 0 时安全跳过。
func (r *RiskControlLogs) Update(ctx context.Context, id int64, log RiskControlLog) error {
	if id == 0 {
		return nil
	}
	// err 保存err，供当前处理流程使用
	_, err := r.DB.ExecContext(ctx,
		`UPDATE risk_control_logs
		    SET processing_result=?, processing_status=?, captcha_engine=?,
		        error_message=?, duration_ms=?, updated_at=CURRENT_TIMESTAMP
		  WHERE id=?`,
		log.ProcessingResult, log.ProcessingStatus, log.CaptchaEngine,
		log.ErrorMessage, log.DurationMS, id)
	return err
}
