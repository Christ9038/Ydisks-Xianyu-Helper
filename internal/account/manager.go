// Package account 实现多账号生命周期管理（supervisor）：
// 从 DB 加载启用的闲鱼账号，为每个账号起一个 engine.Account goroutine，
// 支持动态启停、状态查询、跨层 GetInstance（供 HTTP 手动发货等操作）。
//
// Manager 管理所有启用账号的运行实例。
package account

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// Manager 管理所有账号运行时。
type Manager struct {
	store   *db.Store
	handler engine.Handler
	logger  *slog.Logger

	mu       sync.Mutex
	accounts map[string]*managedAccount
	// stopping 保存正在执行删除/停止 fencing 的账号，阻止其被并发重新启动。
	stopping map[string]struct{}
	runCtx   context.Context
}

// managedAccount 保存managed账号，供当前处理流程使用
type managedAccount struct {
	cookieID string
	acc      *engine.Account
	cancel   context.CancelFunc
	done     chan struct{} // Run 返回后关闭
	err      error
}

// NewManager 构造管理器。
func NewManager(store *db.Store, handler engine.Handler, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		store:    store,
		handler:  handler,
		logger:   logger,
		accounts: make(map[string]*managedAccount),
		stopping: make(map[string]struct{}),
	}
}

// StartAll 从 DB 加载所有启用的账号并启动。
// 已禁用的账号不启动；启动失败的账号记录错误但不影响其他账号。
// StartAll 启动All。
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	m.runCtx = ctx
	m.mu.Unlock()
	// credentials 是系统启动视角下已过滤启用状态、仅含 Cookie 的最小凭证集合。
	credentials, err := m.store.Cookies.ListEnabledRuntimeCredentials(ctx)
	if err != nil {
		return fmt.Errorf("加载账号失败: %w", err)
	}
	// credential 是当前允许启动的账号及其短暂 Cookie 明文，不得离开运行时边界。
	for _, credential := range credentials {
		if err := m.Start(ctx, credential.ID, credential.Value); err != nil { // err 表示当前账号运行实例启动失败，但不阻断其他账号。
			m.logger.Error("启动账号失败", "account", credential.ID, "err", err)
		}
	}
	return nil
}

/*
StartAll 只把启用账号交给运行时 supervisor。
凭证查询由 db repository 负责解密和范围收敛。
启动失败只记录当前账号错误，继续处理其他账号。
调用方负责提供进程级生命周期 Context。
*/

// Start 启动单个账号（若已在运行则跳过；若上次实例已退出则清理后重启）。
func (m *Manager) Start(ctx context.Context, cookieID, cookieValue string) error {
	m.mu.Lock()
	// stopping 表示当前账号是否已进入删除/停止 fencing。
	if _, stopping := m.stopping[cookieID]; stopping {
		m.mu.Unlock()
		return fmt.Errorf("账号 %s 正在停止", cookieID)
	}
	if m.runCtx != nil {
		ctx = m.runCtx
	}
	if // ma、ok 保存ma、ok，供当前处理流程使用
	ma, ok := m.accounts[cookieID]; ok {
		// 已存在：若已退出则清理后重启，否则跳过。select 非阻塞，持锁安全。
		select {
		case <-ma.done:
			delete(m.accounts, cookieID)
		default:
			m.mu.Unlock()
			m.logger.Info("账号已在运行，跳过", "account", cookieID)
			return nil
		}
	}
	// acc 保存acc，供当前处理流程使用
	acc := engine.New(engine.Config{
		CookieID:  cookieID,
		CookieStr: cookieValue,
		Store:     m.store,
		Handler:   m.handler,
		Logger:    m.logger,
	})
	// accCtx、cancel 保存accCtx、cancel，供当前处理流程使用
	accCtx, cancel := context.WithCancel(ctx)
	// ma 保存ma，供当前处理流程使用
	ma := &managedAccount{
		cookieID: cookieID,
		acc:      acc,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	m.accounts[cookieID] = ma
	m.mu.Unlock()

	m.logger.Info("启动账号", "account", cookieID)
	go func() {
		// err 保存err，供当前处理流程使用
		err := acc.Run(accCtx)
		m.mu.Lock()
		ma.err = err
		m.mu.Unlock()
		close(ma.done)
		m.logger.Info("账号运行结束", "account", cookieID, "err", err)
	}()
	return nil
}

// BeginStopping 建立账号停止 fencing，阻止新的运行实例在删除流程中启动。
func (m *Manager) BeginStopping(cookieID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	// exists 表示当前账号是否已有其他删除流程建立 fencing。
	if _, exists := m.stopping[cookieID]; exists {
		return false
	}
	m.stopping[cookieID] = struct{}{}
	return true
}

// EndStopping 释放账号停止 fencing，供删除成功或失败后的收束路径调用。
func (m *Manager) EndStopping(cookieID string) {
	m.mu.Lock()
	delete(m.stopping, cookieID)
	m.mu.Unlock()
}

// Stop 停止单个账号，并兼容不需要错误返回值的旧调用方。
func (m *Manager) Stop(cookieID string) {
	_ = m.StopContext(context.Background(), cookieID)
}

// StopContext 在 ctx 约束内停止单个账号；超时后保留运行实例记录，避免误报已清理。
func (m *Manager) StopContext(ctx context.Context, cookieID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	// ma、ok 保存ma、ok，供当前处理流程使用
	ma, ok := m.accounts[cookieID]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	// err 表示账号运行时停止失败或停止等待超时。
	if err := ma.acc.StopContext(ctx); err != nil {
		return fmt.Errorf("停止账号 %s 失败: %w", cookieID, err)
	}
	ma.cancel()
	select {
	case <-ma.done:
	case <-ctx.Done():
		return fmt.Errorf("等待账号 %s 运行协程退出失败: %w", cookieID, ctx.Err())
	}
	m.mu.Lock()
	if // current 保存current，供当前处理流程使用
	current := m.accounts[cookieID]; current == ma {
		delete(m.accounts, cookieID)
	}
	m.mu.Unlock()
	m.logger.Info("账号已停止", "account", cookieID)
	return nil
}

// GetInstance 跨层获取账号运行时的消息发送句柄（供 HTTP 手动发货等操作）。
// 返回 automation.MessageSender 接口而非具体 *engine.Account，避免上层
// 直接依赖 engine 包内部类型；*engine.Account 实现该接口。
// GetInstance 读取Instance。
func (m *Manager) GetInstance(cookieID string) (automation.MessageSender, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// ma、ok 保存ma、ok，供当前处理流程使用
	ma, ok := m.accounts[cookieID]
	if !ok {
		return nil, false
	}
	return ma.acc, true
}

// Sender 实现 automation.SenderProvider，供自动化中心复用当前在线账号的 WS 发送能力。
func (m *Manager) Sender(cookieID string) (automation.MessageSender, bool) {
	return m.GetInstance(cookieID)
}

// RecoverExpiredCredential 把任意上层 MTOP API 检测到的 Session 失效统一
// 转交给账号 Handler 的协议续期流程。调用方必须先释放账号凭证锁。
// RecoverExpiredCredential 负责RecoverExpiredCredential相关处理。
func (m *Manager) RecoverExpiredCredential(ctx context.Context, cookieID string) bool {
	if m == nil || m.handler == nil {
		return false
	}
	return m.handler.OnPasswordLoginRefresh(ctx, cookieID)
}

// RuntimeStatuses 返回所有已启动账号的实时状态快照。
func (m *Manager) RuntimeStatuses() map[string]engine.RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	// statuses 保存statuses，供当前处理流程使用
	statuses := make(map[string]engine.RuntimeStatus, len(m.accounts))
	// id、ma 表示当前遍历过程中的id、ma
	for id, ma := range m.accounts {
		// status 保存状态，供当前处理流程使用
		status := ma.acc.RuntimeStatus()
		select {
		case <-ma.done:
			if status.State != engine.RuntimeAuthExpired && status.State != engine.RuntimeVerificationRequired {
				status.State = engine.RuntimeError
				status.Connected = false
				status.Message = "账号服务已退出"
				if ma.err != nil && ma.err != context.Canceled {
					status.Message = ma.err.Error()
				}
				status.UpdatedAt = time.Now()
			}
		default:
		}
		statuses[id] = status
	}
	return statuses
}

// Restart 重启账号（停后用最新 DB cookie 重启）。
func (m *Manager) Restart(ctx context.Context, cookieID string) error {
	m.Stop(cookieID)
	cookieValue, err := m.store.Cookies.GetValue(ctx, cookieID) // cookieValue 是重启运行实例所需的单值 Cookie 明文，不包含登录密码或账号资料。
	if err != nil {
		return fmt.Errorf("读取账号详情失败: %w", err)
	}
	return m.Start(ctx, cookieID, cookieValue)
}

// StopAll 停止所有运行中的账号，用于进程优雅退出，并兼容旧调用方。
// 先在锁内收集 cookieID 列表再解锁逐个停，避免持锁等待 goroutine 退出。
// StopAll 停止All。
func (m *Manager) StopAll() {
	_ = m.StopAllContext(context.Background())
}

// StopAllContext 在同一个关闭上下文内停止所有账号，遇到超时立即返回并保留未完成实例。
func (m *Manager) StopAllContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	// ids 保存ids，供当前处理流程使用
	ids := make([]string, 0, len(m.accounts))
	// id 表示当前遍历过程中的标识
	for id := range m.accounts {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	// id 表示当前遍历过程中的标识
	for _, id := range ids {
		// err 表示当前账号停止失败或关闭上下文已到期。
		if err := m.StopContext(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
