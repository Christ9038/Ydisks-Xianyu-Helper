package account

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	// LoginMethodManual 表示用户直接录入或更新 Cookie 的登录方式。
	LoginMethodManual = "manual"
	// LoginMethodPassword 表示账号密码登录方式。
	LoginMethodPassword = "password"
	// LoginMethodQRScan 表示扫码登录方式。
	LoginMethodQRScan = "qr_scan"
	// LoginStatusSuccess 表示登录审计成功状态。
	LoginStatusSuccess = "success"
)

var (
	// ErrLoginAuditUnavailable 表示登录审计服务缺少必需的持久化端口。
	ErrLoginAuditUnavailable = errors.New("账号登录审计服务未初始化")
)

// LoginAuditLog 是登录审计写入模型，不携带 Cookie、Token 或登录密码。
type LoginAuditLog struct {
	// AccountID 是产生登录事件的账号稳定标识。
	AccountID string
	// UserID 是触发登录操作的本地用户标识。
	UserID int64
	// Method 是归一化后的登录方式。
	Method string
	// Status 是登录尝试的结果状态。
	Status string
	// Message 是面向运维审计的非敏感结果摘要。
	Message string
	// TriggerReason 是由登录方式推导出的触发原因。
	TriggerReason string
	// FailureReason 是可选的稳定失败分类。
	FailureReason string
	// OccurredAt 是 Unix 秒时间戳，用于保持数据库审计记录的时间顺序。
	OccurredAt int64
}

// SuccessfulLoginInput 是登录凭证写入成功后的审计输入，不包含任何凭证内容。
type SuccessfulLoginInput struct {
	// AccountID 是刚完成登录的账号稳定标识。
	AccountID string
	// UserID 是当前登录操作所属的本地用户标识。
	UserID int64
	// Method 是调用方提供的登录方式或兼容别名。
	Method string
	// Message 是写入审计日志的非敏感成功摘要。
	Message string
	// OccurredAt 是可选的 Unix 秒时间戳；零值由服务使用当前时间填充。
	OccurredAt int64
}

// LoginAuditRepository 定义登录成功审计所需的最小持久化端口。
type LoginAuditRepository interface {
	// MarkLogin 保存账号最近一次成功登录方式和时间。
	MarkLogin(context.Context, string, string, int64) error
	// SetStatusWithReason 在登录成功后启用账号并保存状态原因。
	SetStatusWithReason(context.Context, string, bool, string) error
	// AddLoginLog 写入不含敏感凭证的登录审计记录。
	AddLoginLog(context.Context, LoginAuditLog) error
}

// LoginAuditService 编排登录成功后的方式记录、账号启用和审计日志写入。
type LoginAuditService struct {
	// repository 提供登录审计所需的三项窄持久化能力。
	repository LoginAuditRepository
}

// NewLoginAuditService 构造登录审计应用服务。
func NewLoginAuditService(repository LoginAuditRepository) *LoginAuditService {
	return &LoginAuditService{repository: repository}
}

// RecordSuccessfulLogin 记录成功登录并按登录方式启用账号；后续写入失败不会泄露凭证。
func (s *LoginAuditService) RecordSuccessfulLogin(ctx context.Context, input SuccessfulLoginInput) error {
	if s == nil || s.repository == nil {
		return ErrLoginAuditUnavailable
	}
	if strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.Method) == "" {
		return nil
	}
	// method 是审计和账号状态更新共用的稳定登录方式。
	method := NormalizeLoginMethod(input.Method)
	// occurredAt 是本次成功登录的审计时间；调用方可注入固定时间以保证重放一致。
	occurredAt := input.OccurredAt
	if occurredAt == 0 {
		occurredAt = time.Now().Unix()
	}
	// markErr 表示最近登录方式持久化失败；失败时不应继续伪造成功状态或日志。
	if markErr := s.repository.MarkLogin(ctx, input.AccountID, method, occurredAt); markErr != nil {
		return markErr
	}
	// writeErrors 收集独立写入阶段的错误，同时保证状态写入失败时仍尝试保存审计日志。
	var writeErrors []error
	if method == LoginMethodPassword || method == LoginMethodQRScan {
		// statusErr 表示登录成功后启用账号失败，但不阻止继续留下审计日志。
		if statusErr := s.repository.SetStatusWithReason(ctx, input.AccountID, true, ""); statusErr != nil {
			writeErrors = append(writeErrors, statusErr)
		}
	}
	// log 是由应用层字段组成的审计记录，数据库模型转换留在适配器内。
	log := LoginAuditLog{
		AccountID:     input.AccountID,
		UserID:        input.UserID,
		Method:        method,
		Status:        LoginStatusSuccess,
		Message:       truncateLoginMessage(input.Message, 500),
		TriggerReason: loginTriggerReason(method),
		OccurredAt:    occurredAt,
	}
	// logErr 表示成功登录审计日志写入失败，需要与账号状态错误一起返回。
	if logErr := s.repository.AddLoginLog(ctx, log); logErr != nil {
		writeErrors = append(writeErrors, logErr)
	}
	return errors.Join(writeErrors...)
}

// NormalizeLoginMethod 将兼容的登录方式别名归一化为稳定审计值。
func NormalizeLoginMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "manual", "cookie", "manual_cookie":
		return LoginMethodManual
	case "password", "password_login":
		return LoginMethodPassword
	case "qr", "qr_login", "qr_scan":
		return LoginMethodQRScan
	default:
		return strings.ToLower(strings.TrimSpace(method))
	}
}

// loginTriggerReason 将稳定登录方式转换为审计展示原因。
func loginTriggerReason(method string) string {
	switch NormalizeLoginMethod(method) {
	case LoginMethodManual:
		return "手动Cookie录入"
	case LoginMethodPassword:
		return "账号密码登录"
	case LoginMethodQRScan:
		return "扫码登录"
	default:
		return ""
	}
}

// truncateLoginMessage 限制审计摘要长度，避免外部错误文本膨胀持久化记录。
func truncateLoginMessage(message string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(message) <= limit {
		return message
	}
	return message[:limit]
}
