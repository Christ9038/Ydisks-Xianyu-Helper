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
	runCtx   context.Context
}

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
	}
}

// StartAll 从 DB 加载所有启用的账号并启动。
// 已禁用的账号不启动；启动失败的账号记录错误但不影响其他账号。
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	m.runCtx = ctx
	m.mu.Unlock()
	// 管理员视角取全部 cookie（userID=0）。
	all, err := m.store.Cookies.AllForUser(ctx, 0)
	if err != nil {
		return fmt.Errorf("加载账号失败: %w", err)
	}
	for cookieID, cookieValue := range all {
		if !m.store.Cookies.GetStatus(ctx, cookieID) {
			m.logger.Info("账号已禁用，跳过启动", "account", cookieID)
			continue
		}
		if err := m.Start(ctx, cookieID, cookieValue); err != nil {
			m.logger.Error("启动账号失败", "account", cookieID, "err", err)
		}
	}
	return nil
}

// Start 启动单个账号（若已在运行则跳过；若上次实例已退出则清理后重启）。
func (m *Manager) Start(ctx context.Context, cookieID, cookieValue string) error {
	m.mu.Lock()
	if m.runCtx != nil {
		ctx = m.runCtx
	}
	if ma, ok := m.accounts[cookieID]; ok {
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
	m.mu.Unlock()

	acc := engine.New(engine.Config{
		CookieID:  cookieID,
		CookieStr: cookieValue,
		Store:     m.store,
		Handler:   m.handler,
		Logger:    m.logger,
	})
	accCtx, cancel := context.WithCancel(ctx)
	ma := &managedAccount{
		cookieID: cookieID,
		acc:      acc,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	m.mu.Lock()
	m.accounts[cookieID] = ma
	m.mu.Unlock()

	m.logger.Info("启动账号", "account", cookieID)
	go func() {
		defer close(ma.done)
		ma.err = acc.Run(accCtx)
		m.logger.Info("账号运行结束", "account", cookieID, "err", ma.err)
	}()
	return nil
}

// Stop 停止单个账号。
func (m *Manager) Stop(cookieID string) {
	m.mu.Lock()
	ma, ok := m.accounts[cookieID]
	m.mu.Unlock()
	if !ok {
		return
	}
	ma.acc.Stop()
	ma.cancel()
	<-ma.done
	m.mu.Lock()
	delete(m.accounts, cookieID)
	m.mu.Unlock()
	m.logger.Info("账号已停止", "account", cookieID)
}

// GetInstance 跨层获取账号运行时的消息发送句柄（供 HTTP 手动发货等操作）。
// 返回 automation.MessageSender 接口而非具体 *engine.Account，避免上层
// 直接依赖 engine 包内部类型；*engine.Account 实现该接口。
func (m *Manager) GetInstance(cookieID string) (automation.MessageSender, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// RuntimeStatuses 返回所有已启动账号的实时状态快照。
func (m *Manager) RuntimeStatuses() map[string]engine.RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	statuses := make(map[string]engine.RuntimeStatus, len(m.accounts))
	for id, ma := range m.accounts {
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
	d, err := m.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		return fmt.Errorf("读取账号详情失败: %w", err)
	}
	return m.Start(ctx, cookieID, d.Value)
}

// StopAll 停止所有运行中的账号，用于进程优雅退出。
// 先在锁内收集 cookieID 列表再解锁逐个停，避免持锁等待 goroutine 退出。
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.accounts))
	for id := range m.accounts {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}
