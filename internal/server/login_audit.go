package server

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
)

const (
	// loginMethodPassword 保留 Server 内部调用方使用的账号密码登录方式别名。
	loginMethodPassword = accountapp.LoginMethodPassword
	// loginMethodQRScan 保留 Server 内部调用方使用的扫码登录方式别名。
	loginMethodQRScan = accountapp.LoginMethodQRScan
)

// markSuccessfulLogin 将 Server 登录成功事件交给账号应用服务审计，不在 HTTP 层组装数据库模型。
func (s *Server) markSuccessfulLogin(ctx context.Context, cookieID string, userID int64, method, message string) {
	if s == nil {
		return
	}
	// auditService 保存应用层登录审计服务；它拥有归一化、启用和日志写入编排。
	auditService := s.loginAuditApplication()
	if auditService == nil {
		return
	}
	// auditErr 保存审计服务返回的基础设施错误；登录主流程保持旧的成功后续行为并仅记录告警。
	auditErr := auditService.RecordSuccessfulLogin(ctx, accountapp.SuccessfulLoginInput{
		AccountID: cookieID,
		UserID:    userID,
		Method:    method,
		Message:   message,
	})
	if auditErr != nil && s.Logger != nil {
		s.Logger.Warn("记录账号登录审计失败", "cookie_id", cookieID, "method", method, "err", auditErr)
	}
}
