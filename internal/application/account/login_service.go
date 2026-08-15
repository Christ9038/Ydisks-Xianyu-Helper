package account

import (
	"context"
	"errors"
	"strings"
)

// CreateCookieInput 是手动录入账号 Cookie 用例的非敏感输入；明文 Cookie 由调用方专用端口封装。
type CreateCookieInput struct {
	// AccountID 是待创建的平台账号稳定标识。
	AccountID string
	// UserID 是当前用户的本地身份标识。
	UserID int64
	// LoginMethod 是触发本次登录的方式；空值按手动 Cookie 录入处理。
	LoginMethod string
}

// CookieWriter 定义一次性写入账号凭证的端口；实现方负责在最小作用域内持有明文 Cookie。
type CookieWriter interface {
	// CreateOwnedCookie 原子校验账号归属并写入凭证；明文不应进入应用层 DTO 或日志。
	CreateOwnedCookie(context.Context, string, int64) error
}

// LoginLifecyclePort 定义凭证成功写入后的审计、启用、资料刷新和运行时同步边界。
type LoginLifecyclePort interface {
	// AfterSuccessfulLogin 在凭证写入并释放凭证锁后完成后续登录编排。
	AfterSuccessfulLogin(context.Context, int64, string, string)
}

// LoginService 编排手动 Cookie 登录用例，不保存任何敏感凭证状态。
type LoginService struct {
	// lifecycle 负责凭证写入后的审计、资料和运行时适配。
	lifecycle LoginLifecyclePort
}

// NewLoginService 构造账号登录应用服务并校验必需的生命周期端口。
func NewLoginService(lifecycle LoginLifecyclePort) (*LoginService, error) {
	if lifecycle == nil {
		return nil, errors.New("账号登录生命周期端口未初始化")
	}
	return &LoginService{lifecycle: lifecycle}, nil
}

// CreateCookie 执行一次手动 Cookie 登录；凭证仅通过调用方提供的专用写入端口传递。
func (s *LoginService) CreateCookie(ctx context.Context, input CreateCookieInput, writer CookieWriter) error {
	if s == nil || s.lifecycle == nil {
		return errors.New("账号登录服务未初始化")
	}
	if writer == nil {
		return errors.New("账号 Cookie 写入端口未初始化")
	}
	if strings.TrimSpace(input.AccountID) == "" {
		return errors.New("缺少账号 ID")
	}
	// writeErr 保存基础设施端口写入凭证时返回的错误。
	writeErr := writer.CreateOwnedCookie(ctx, input.AccountID, input.UserID)
	if writeErr != nil {
		return writeErr
	}
	// method 是归一化后的登录方式，供审计和运行时适配器保持统一语义。
	method := normalizeLoginMethod(input.LoginMethod)
	s.lifecycle.AfterSuccessfulLogin(ctx, input.UserID, input.AccountID, method)
	return nil
}

// normalizeLoginMethod 将兼容的登录方式别名归一化为审计使用的稳定值。
func normalizeLoginMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "manual", "cookie", "manual_cookie":
		return "manual"
	case "password", "password_login":
		return "password"
	case "qr", "qr_login", "qr_scan":
		return "qr_scan"
	default:
		return strings.ToLower(strings.TrimSpace(method))
	}
}
