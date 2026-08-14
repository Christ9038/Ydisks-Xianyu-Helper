package server

import (
	"context"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// loginMethodManual 保存登录方法Manual，供当前处理流程使用
const (
	loginMethodManual   = "manual"
	loginMethodPassword = "password"
	loginMethodQRScan   = "qr_scan"
	loginStatusSuccess  = "success"
	loginStatusFailed   = "failed"
)

// normalizeLoginMethod 负责normalize登录方法相关处理。
func normalizeLoginMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "manual", "cookie", "manual_cookie":
		return loginMethodManual
	case "password", "password_login":
		return loginMethodPassword
	case "qr", "qr_login", "qr_scan":
		return loginMethodQRScan
	default:
		return strings.ToLower(strings.TrimSpace(method))
	}
}

// markSuccessfulLogin 负责markSuccessful登录相关处理。
func (s *Server) markSuccessfulLogin(ctx context.Context, cookieID string, userID int64, method, message string) {
	// repository 提供登录审计和账号状态持久化能力。
	repository := s.accountLoginRepositoryForServer()
	if repository == nil {
		return
	}
	method = normalizeLoginMethod(method)
	if method == "" {
		return
	}
	// at 保存at，供当前处理流程使用
	at := time.Now().Unix()
	if // err 保存err，供当前处理流程使用
	err := repository.MarkLogin(ctx, cookieID, method, at); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("记录账号登录方式失败", "cookie_id", cookieID, "method", method, "err", err)
		}
		return
	}
	if method == loginMethodPassword || method == loginMethodQRScan {
		if // err 保存err，供当前处理流程使用
		err := repository.SetStatusWithReason(ctx, cookieID, true, ""); err != nil && s.Logger != nil {
			s.Logger.Warn("成功登录后启用账号失败", "cookie_id", cookieID, "method", method, "err", err)
		}
	}
	s.addLoginLog(ctx, cookieID, userID, method, loginStatusSuccess, "", message, at)
}

// addLoginLog 负责add登录Log相关处理。
func (s *Server) addLoginLog(ctx context.Context, cookieID string, userID int64, method, status, failureReason, message string, at int64) {
	// repository 提供登录日志持久化能力。
	repository := s.accountLoginRepositoryForServer()
	if repository == nil {
		return
	}
	if at == 0 {
		at = time.Now().Unix()
	}
	if // err 保存err，供当前处理流程使用
	err := repository.AddLoginLog(ctx, db.AccountLoginLog{
		CookieID:          cookieID,
		UserID:            userID,
		OwnerID:           userID,
		AccountIdentifier: cookieID,
		Method:            normalizeLoginMethod(method),
		Status:            status,
		Message:           truncate(message, 500),
		TriggerReason:     loginTriggerReason(method),
		FailureReason:     failureReason,
		ErrorMessage:      truncate(message, 500),
		CreatedAt:         at,
	}); err != nil && s.Logger != nil {
		s.Logger.Warn("记录账号登录日志失败", "cookie_id", cookieID, "method", method, "status", status, "err", err)
	}
}

// loginTriggerReason 负责登录Trigger原因相关处理。
func loginTriggerReason(method string) string {
	switch normalizeLoginMethod(method) {
	case loginMethodManual:
		return "手动Cookie录入"
	case loginMethodPassword:
		return "账号密码登录"
	case loginMethodQRScan:
		return "扫码登录"
	default:
		return ""
	}
}
