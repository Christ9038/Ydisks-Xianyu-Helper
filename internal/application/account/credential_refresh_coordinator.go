package account

import (
	"context"
	"errors"
	"sync"
)

// ErrCredentialRefreshInFlight 表示同一账号已有凭证恢复操作正在执行。
var ErrCredentialRefreshInFlight = errors.New("账号凭证恢复正在执行")

// CredentialRefreshWork 执行一次账号凭证恢复；返回值表示是否已恢复可用凭证，错误保留底层失败原因。
type CredentialRefreshWork func(context.Context) (bool, error)

// CredentialRefreshCoordinator 按账号串行化凭证恢复，保证慢速外部 I/O 不在锁内执行。
// mu 只保护 inFlight；work 在释放协调状态后仍由调用方负责其自身凭证快照和提交边界。
type CredentialRefreshCoordinator struct {
	// mu 保护 inFlight 的账号集合；持锁期间只做集合操作，不调用外部服务。
	mu sync.Mutex
	// inFlight 记录当前正在恢复的账号，任务结束、报错、取消或 panic 后必须移除。
	inFlight map[string]struct{}
}

// NewCredentialRefreshCoordinator 创建空的账号凭证恢复协调器。
func NewCredentialRefreshCoordinator() *CredentialRefreshCoordinator {
	return &CredentialRefreshCoordinator{inFlight: make(map[string]struct{})}
}

// TryBegin 尝试登记账号恢复；成功时调用方必须在 defer 中执行 Finish，即使工作返回错误或 panic。
func (c *CredentialRefreshCoordinator) TryBegin(accountID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inFlight == nil {
		c.inFlight = make(map[string]struct{})
	}
	// exists 表示该账号是否已有恢复工作登记，命中时拒绝并发外部续期。
	if _, exists := c.inFlight[accountID]; exists {
		return false
	}
	c.inFlight[accountID] = struct{}{}
	return true
}

// Finish 清除账号恢复登记；重复调用保持幂等，便于兼容旧调用方的收尾路径。
func (c *CredentialRefreshCoordinator) Finish(accountID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inFlight, accountID)
}

// Run 在账号范围内执行一项恢复工作，并在所有返回路径后释放登记状态。
// accepted 为 false 时表示已有同账号工作在执行；work 的错误和取消上下文由调用方继续记录。
func (c *CredentialRefreshCoordinator) Run(ctx context.Context, accountID string, work CredentialRefreshWork) (accepted, renewed bool, err error) {
	if c == nil {
		return false, false, errors.New("账号凭证恢复协调器未初始化")
	}
	if !c.TryBegin(accountID) {
		return false, false, ErrCredentialRefreshInFlight
	}
	defer c.Finish(accountID)
	if work == nil {
		return true, false, errors.New("账号凭证恢复工作未提供")
	}
	renewed, err = work(ctx)
	return true, renewed, err
}
