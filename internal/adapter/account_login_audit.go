package adapter

import (
	"context"
	"errors"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// AccountLoginAuditRepository 将账号登录审计应用 Port 适配为数据库窄写入。
type AccountLoginAuditRepository struct {
	// store 保存数据库聚合入口，仅在基础设施适配器内部使用。
	store *db.Store
}

// NewAccountLoginAuditRepository 创建账号登录审计数据库适配器。
func NewAccountLoginAuditRepository(store *db.Store) *AccountLoginAuditRepository {
	return &AccountLoginAuditRepository{store: store}
}

// MarkLogin 保存账号最近一次成功登录方式和时间。
func (r *AccountLoginAuditRepository) MarkLogin(ctx context.Context, accountID, method string, at int64) error {
	// validateErr 表示适配器或 Cookie 仓储未初始化，阻止审计写入继续访问空依赖。
	if validateErr := r.validateCookies(); validateErr != nil {
		return validateErr
	}
	return r.store.Cookies.MarkLogin(ctx, accountID, method, at)
}

// SetStatusWithReason 在登录成功后启用账号并清空禁用原因。
func (r *AccountLoginAuditRepository) SetStatusWithReason(ctx context.Context, accountID string, enabled bool, reason string) error {
	// validateErr 表示适配器或 Cookie 仓储未初始化，阻止账号状态写入访问空依赖。
	if validateErr := r.validateCookies(); validateErr != nil {
		return validateErr
	}
	return r.store.Cookies.SetStatusWithReason(ctx, accountID, enabled, reason)
}

// AddLoginLog 将应用层审计模型转换为数据库模型并写入；审计仓储未装配时保持旧的兼容空操作。
func (r *AccountLoginAuditRepository) AddLoginLog(ctx context.Context, log accountapp.LoginAuditLog) error {
	// validateErr 表示数据库适配器未初始化，阻止审计日志转换访问空 Store。
	if validateErr := r.validateStore(); validateErr != nil {
		return validateErr
	}
	if r.store.LoginLogs == nil {
		return nil
	}
	return r.store.LoginLogs.Add(ctx, db.AccountLoginLog{
		CookieID:          log.AccountID,
		UserID:            log.UserID,
		OwnerID:           log.UserID,
		AccountIdentifier: log.AccountID,
		Method:            log.Method,
		Status:            log.Status,
		Message:           log.Message,
		TriggerReason:     log.TriggerReason,
		FailureReason:     log.FailureReason,
		ErrorMessage:      log.Message,
		CreatedAt:         log.OccurredAt,
	})
}

// validateStore 检查审计适配器是否具备数据库聚合入口。
func (r *AccountLoginAuditRepository) validateStore() error {
	if r == nil || r.store == nil {
		return errors.New("账号登录审计数据库适配器未初始化")
	}
	return nil
}

// validateCookies 检查账号状态和登录方式写入所需的 Cookie 仓储。
func (r *AccountLoginAuditRepository) validateCookies() error {
	// validateErr 表示数据库聚合入口缺失，需在检查 Cookie 仓储前原样返回。
	if validateErr := r.validateStore(); validateErr != nil {
		return validateErr
	}
	if r.store.Cookies == nil {
		return errors.New("账号登录审计 Cookie 仓储未初始化")
	}
	return nil
}

var _ accountapp.LoginAuditRepository = (*AccountLoginAuditRepository)(nil)
