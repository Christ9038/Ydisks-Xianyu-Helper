package account

import (
	"context"
	"errors"
	"strings"
	"time"
)

// RuntimeStatus 是账号运行时对外提供的非持久化连接状态快照；不包含 Cookie、Token 或其他凭证。
type RuntimeStatus struct {
	// State 是运行时状态机当前状态，例如 online、error 或 auth_expired。
	State string `json:"state"`
	// Message 是面向管理页面的非敏感诊断信息。
	Message string `json:"message,omitempty"`
	// Connected 表示当前账号是否仍有可用的实时连接。
	Connected bool `json:"connected"`
	// Failures 是最近连接失败次数，具体统计窗口由运行时实现决定。
	Failures int `json:"failures"`
	// UpdatedAt 是最近一次状态变化的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
	// TokenAcquiredAt 是最近一次获得平台会话令牌的时间。
	TokenAcquiredAt time.Time `json:"token_acquired_at,omitempty"`
	// TokenExpiresAt 是当前平台会话令牌的预计过期时间。
	TokenExpiresAt time.Time `json:"token_expires_at,omitempty"`
	// TokenRefreshAt 是最近一次或下一次令牌续期时间，具体语义由运行时实现保持兼容。
	TokenRefreshAt time.Time `json:"token_refresh_at,omitempty"`
	// TokenRemainingSeconds 是当前令牌剩余有效秒数。
	TokenRemainingSeconds int64 `json:"token_remaining_seconds,omitempty"`
	// TokenRefreshStatus 是令牌续期阶段的非敏感状态标签。
	TokenRefreshStatus string `json:"token_refresh_status,omitempty"`
}

// RuntimePort 定义账号应用层需要的最小运行时控制能力；实现方不得把 HTTP 或 engine 类型泄漏给调用者。
type RuntimePort interface {
	// UpdateCookie 把新 Cookie 同步到已存在的账号运行实例；缺少实例时应保持幂等无副作用。
	UpdateCookie(context.Context, string, string) error
	// RuntimeStatuses 返回当前已登记账号的状态快照；调用方可安全修改返回的 map。
	RuntimeStatuses(context.Context) (map[string]RuntimeStatus, error)
	// Restart 使用持久化凭证重启指定账号运行实例，并在完成后返回运行时错误。
	Restart(context.Context, string) error
	// RecoverExpiredCredential 在平台会话失效时触发账号凭证恢复；返回是否接受了恢复请求。
	RecoverExpiredCredential(context.Context, string) bool
}

// CredentialWakePort 定义凭证恢复后唤醒自动化任务的最小能力，避免账号应用层依赖自动化实现包。
type CredentialWakePort interface {
	// WakeCredentialBlocked 唤醒指定账号因凭证失效而暂停的任务。
	WakeCredentialBlocked(context.Context, string) error
}

// RuntimeService 编排账号凭证写回后的运行时同步、重启和状态快照读取。
type RuntimeService struct {
	// runtime 提供账号运行实例的生命周期与状态能力；可为空以兼容离线测试或未启用运行时的进程。
	runtime RuntimePort
	// wake 在凭证更新或重启完成后唤醒被凭证失效阻塞的自动化任务。
	wake CredentialWakePort
}

// NewRuntimeService 构造账号运行时应用服务；运行时和唤醒端口都允许为空以保持无浏览器测试可运行。
func NewRuntimeService(runtime RuntimePort, wake CredentialWakePort) *RuntimeService {
	return &RuntimeService{runtime: runtime, wake: wake}
}

// UpdateCookie 同步运行时 Cookie 并唤醒凭证阻塞任务；外部端口错误向调用者保留但不泄漏敏感值。
func (s *RuntimeService) UpdateCookie(ctx context.Context, accountID, value string) error {
	if s == nil {
		return errors.New("账号运行时服务未初始化")
	}
	// validationErr 保存 Cookie 同步输入校验错误。
	if validationErr := validateRuntimeInput(ctx, accountID); validationErr != nil {
		return validationErr
	}
	// wakeErr 保存自动化任务唤醒失败；该失败不阻断 Cookie 同步，避免凭证已更新但运行实例仍持有旧值。
	var wakeErr error
	if s.wake != nil {
		wakeErr = s.wake.WakeCredentialBlocked(ctx, accountID)
	}
	if s.runtime == nil {
		return wakeErr
	}
	// updateErr 保存运行实例 Cookie 同步错误；运行时错误优先于非阻断的唤醒错误返回。
	updateErr := s.runtime.UpdateCookie(ctx, accountID, value)
	if updateErr != nil {
		return updateErr
	}
	return wakeErr
}

// RuntimeStatuses 读取账号运行状态快照；未装配运行时返回空集合而不是伪造在线状态。
func (s *RuntimeService) RuntimeStatuses(ctx context.Context) (map[string]RuntimeStatus, error) {
	if s == nil {
		return nil, errors.New("账号运行时服务未初始化")
	}
	// contextErr 保存状态快照读取前的取消错误。
	if contextErr := contextError(ctx); contextErr != nil {
		return nil, contextErr
	}
	if s.runtime == nil {
		return map[string]RuntimeStatus{}, nil
	}
	return s.runtime.RuntimeStatuses(ctx)
}

// Restart 重启指定账号运行实例，并在成功后唤醒凭证阻塞任务。
func (s *RuntimeService) Restart(ctx context.Context, accountID string) error {
	if s == nil {
		return errors.New("账号运行时服务未初始化")
	}
	// validationErr 保存账号运行时重启输入校验错误。
	if validationErr := validateRuntimeInput(ctx, accountID); validationErr != nil {
		return validationErr
	}
	if s.runtime != nil {
		// restartErr 保存运行实例重启错误；失败时不执行后续唤醒。
		if restartErr := s.runtime.Restart(ctx, accountID); restartErr != nil {
			return restartErr
		}
	}
	if s.wake != nil {
		// wakeErr 保存重启成功后的自动化任务唤醒结果。
		wakeErr := s.wake.WakeCredentialBlocked(ctx, accountID)
		return wakeErr
	}
	return nil
}

// RecoverExpiredCredential 将平台会话失效恢复请求转发给运行时端口，不让 Server 直接持有 Manager 业务调用。
func (s *RuntimeService) RecoverExpiredCredential(ctx context.Context, accountID string) bool {
	if s == nil || s.runtime == nil || validateRuntimeInput(ctx, accountID) != nil {
		return false
	}
	return s.runtime.RecoverExpiredCredential(ctx, accountID)
}

// validateRuntimeInput 统一校验账号运行时调用的上下文和账号标识。
func validateRuntimeInput(ctx context.Context, accountID string) error {
	// contextErr 保存账号标识校验前检查到的取消错误。
	if contextErr := contextError(ctx); contextErr != nil {
		return contextErr
	}
	if strings.TrimSpace(accountID) == "" {
		return errors.New("账号标识不能为空")
	}
	return nil
}

// contextError 将 nil Context 视为可继续执行，并在已取消时阻止任何运行时副作用。
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
