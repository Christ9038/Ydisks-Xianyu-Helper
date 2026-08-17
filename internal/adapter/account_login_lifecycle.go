package adapter

import (
	"context"
	"log/slog"

	accountapp "xianyu-go/internal/application/account"
)

// AccountLoginLifecycle 将登录审计、资料刷新、账号重启和扫码清理告警收口到适配器。
// 它只组合应用层端口与日志能力，不持有 Server 或明文凭证。
type AccountLoginLifecycle struct {
	// audit 负责记录登录方式、启用账号和写入审计日志。
	audit *accountapp.LoginAuditService
	// success 负责登录成功后的资料刷新与启用账号运行时重启。
	success *accountapp.LoginSuccessService
	// logger 记录不影响主流程的审计或清理告警，日志中不得包含 Cookie 内容。
	logger *slog.Logger
}

// NewAccountLoginLifecycle 构造手动登录和扫码登录共用的生命周期适配器。
func NewAccountLoginLifecycle(audit *accountapp.LoginAuditService, success *accountapp.LoginSuccessService, logger *slog.Logger) *AccountLoginLifecycle {
	return &AccountLoginLifecycle{audit: audit, success: success, logger: logger}
}

// AfterSuccessfulLogin 在凭证写入并释放凭证锁后记录审计并执行登录后续编排。
func (l *AccountLoginLifecycle) AfterSuccessfulLogin(ctx context.Context, userID int64, accountID, method string) {
	if l == nil {
		return
	}
	// l.recordAudit 记录审计失败但不阻断已完成的凭证登录结果。
	l.recordAudit(ctx, userID, accountID, method, "账号登录成功")
	if l.success != nil {
		l.success.AfterSuccessfulLogin(ctx, userID, accountID)
	}
}

// AfterSuccessfulQRLogin 在扫码凭证写入并释放凭证锁后记录扫码审计并执行后续编排。
func (l *AccountLoginLifecycle) AfterSuccessfulQRLogin(ctx context.Context, userID int64, accountID string) {
	if l == nil {
		return
	}
	// l.recordAudit 记录扫码登录审计失败但不回滚已经写入的凭证。
	l.recordAudit(ctx, userID, accountID, accountapp.LoginMethodQRScan, "扫码登录成功")
	if l.success != nil {
		l.success.AfterSuccessfulLogin(ctx, userID, accountID)
	}
}

// ReportQRLoginCleanupFailure 记录扫码成功后旧 Token 清理失败，不泄露 Cookie 内容。
func (l *AccountLoginLifecycle) ReportQRLoginCleanupFailure(_ context.Context, accountID string, err error) {
	if l != nil && l.logger != nil {
		l.logger.Warn("扫码登录后清理旧连接凭证失败", "cookie_id", accountID, "err", err)
	}
}

// recordAudit 将登录审计错误降级为脱敏告警，保持凭证写入主流程的既有成功语义。
func (l *AccountLoginLifecycle) recordAudit(ctx context.Context, userID int64, accountID, method, message string) {
	if l.audit == nil {
		return
	}
	// auditErr 保存审计应用服务返回的基础设施错误；错误日志不包含明文凭证。
	auditErr := l.audit.RecordSuccessfulLogin(ctx, accountapp.SuccessfulLoginInput{
		AccountID: accountID, UserID: userID, Method: method, Message: message,
	})
	if auditErr != nil && l.logger != nil {
		l.logger.Warn("记录账号登录审计失败", "cookie_id", accountID, "method", method, "err", auditErr)
	}
}

var _ accountapp.LoginLifecyclePort = (*AccountLoginLifecycle)(nil)
var _ accountapp.QRLoginLifecycle = (*AccountLoginLifecycle)(nil)
