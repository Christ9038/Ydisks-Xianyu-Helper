package adapter

import (
	"context"

	accountmanager "xianyu-go/internal/account"
	accountapp "xianyu-go/internal/application/account"
)

// AccountRuntimePort 将账号 Manager 的 engine 状态和生命周期能力映射为应用层运行时端口。
type AccountRuntimePort struct {
	// manager 持有账号运行实例及其并发关闭语义；Server 不直接读取其 engine 状态。
	manager *accountmanager.Manager
}

// NewAccountRuntimePort 创建账号运行时适配器；nil Manager 表示当前进程未启用账号运行实例。
func NewAccountRuntimePort(manager *accountmanager.Manager) accountapp.RuntimePort {
	if manager == nil {
		return nil
	}
	return AccountRuntimePort{manager: manager}
}

// AccountRunningLookup 创建订单等应用适配器使用的账号在线查询函数。
// Manager 的具体运行实例和锁语义只在 adapter 内部读取，调用方只获得布尔结果。
func AccountRunningLookup(manager *accountmanager.Manager) func(string) bool {
	return func(accountID string) bool {
		if manager == nil {
			return false
		}
		// _, running 保存账号运行实例是否存在。
		_, running := manager.GetInstance(accountID)
		return running
	}
}

// UpdateCookie 将凭证同步到已运行的账号实例；不存在实例时保持幂等成功。
func (p AccountRuntimePort) UpdateCookie(ctx context.Context, accountID, value string) error {
	// contextErr 保存同步前检查到的取消错误。
	if contextErr := contextError(ctx); contextErr != nil {
		return contextErr
	}
	if p.manager == nil {
		return nil
	}
	// sender、ok 保存账号运行实例发送句柄及其存在性。
	sender, ok := p.manager.GetInstance(accountID)
	if !ok || sender == nil {
		return nil
	}
	sender.UpdateCookie(value)
	return nil
}

// RuntimeStatuses 返回应用层状态快照，避免上层依赖 engine.RuntimeStatus。
func (p AccountRuntimePort) RuntimeStatuses(ctx context.Context) (map[string]accountapp.RuntimeStatus, error) {
	// contextErr 保存状态读取前检查到的取消错误。
	if contextErr := contextError(ctx); contextErr != nil {
		return nil, contextErr
	}
	// result 保存转换为应用 DTO 后的账号状态快照。
	result := make(map[string]accountapp.RuntimeStatus)
	if p.manager == nil {
		return result, nil
	}
	// accountID、status 表示当前遍历的账号标识及其 engine 状态快照。
	for accountID, status := range p.manager.RuntimeStatuses() {
		result[accountID] = accountapp.RuntimeStatus{
			State: status.State, Message: status.Message, Connected: status.Connected, Failures: status.Failures,
			UpdatedAt: status.UpdatedAt, TokenAcquiredAt: status.TokenAcquiredAt, TokenExpiresAt: status.TokenExpiresAt,
			TokenRefreshAt: status.TokenRefreshAt, TokenRemainingSeconds: status.TokenRemainingSeconds,
			TokenRefreshStatus: status.TokenRefreshStatus,
		}
	}
	return result, nil
}

// Restart 重启账号运行实例；取消和数据库读取错误沿 Manager 原语向上返回。
func (p AccountRuntimePort) Restart(ctx context.Context, accountID string) error {
	// contextErr 保存重启前检查到的取消错误。
	if contextErr := contextError(ctx); contextErr != nil {
		return contextErr
	}
	if p.manager == nil {
		return nil
	}
	return p.manager.Restart(ctx, accountID)
}

// RecoverExpiredCredential 将平台会话失效恢复请求转发给账号 Manager。
func (p AccountRuntimePort) RecoverExpiredCredential(ctx context.Context, accountID string) bool {
	if contextError(ctx) != nil || p.manager == nil {
		return false
	}
	return p.manager.RecoverExpiredCredential(ctx, accountID)
}

// contextError 检查运行时适配器执行前的取消状态，避免取消请求仍触发实例副作用。
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
