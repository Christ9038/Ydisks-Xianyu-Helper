package renewal

import (
	"sync"
	"time"
)

// CooldownManager 保存登录恢复相关冷却状态。
// Scheduler 和运行时 Adapter 共用同一实例，避免同一账号被重复触发密码登录。
// CooldownManager 用于本次流程后续判断的CooldownManager
type CooldownManager struct {
	mu sync.Mutex

	sessionExpiredByCookie map[string]time.Time
	passwordLoginByCookie  map[string]time.Time
	passwordErrorByCookie  map[string]time.Time
}

// GlobalCooldown 是服务进程内的统一续期冷却状态。
var GlobalCooldown = NewCooldownManager()

// NewCooldownManager 封装NewCooldownManager业务协调。
func NewCooldownManager() *CooldownManager {
	return &CooldownManager{
		sessionExpiredByCookie: make(map[string]time.Time),
		passwordLoginByCookie:  make(map[string]time.Time),
		passwordErrorByCookie:  make(map[string]time.Time),
	}
}

// MarkSessionExpired 封装Mark会话Expired业务协调。
func (m *CooldownManager) MarkSessionExpired(cookieID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionExpiredByCookie[cookieID] = time.Now()
}

// IsSessionCooled 封装Is会话Cooled业务协调。
func (m *CooldownManager) IsSessionCooled(cookieID string) (bool, time.Duration) {
	if m == nil {
		return false, 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// last 用于本次流程后续判断的last
	last := m.sessionExpiredByCookie[cookieID]
	if last.IsZero() {
		return false, 0
	}
	// remain 用于本次流程后续判断的remain
	remain := sessionExpiredCooldown - time.Since(last)
	return remain > 0, remain
}

// TryPasswordLogin 封装Try密码登录业务协调。
func (m *CooldownManager) TryPasswordLogin(cookieID string) (bool, time.Duration) {
	// ok、remain 用于本次流程后续判断的ok、remain
	ok, remain, _ := m.PasswordLoginAllowed(cookieID, passwordLoginCooldown)
	if !ok {
		return false, remain
	}
	m.MarkPasswordLogin(cookieID)
	return true, 0
}

// PasswordLoginAllowed 只检查冷却而不写入时间。参考运行时只在真实密码
// 登录拿到 Cookie 后记录登录时间，接口/浏览器轻量续期和失败尝试都不能启动
// 登录冷却。reason 为 login_cooldown 或 password_error_cooldown。
// PasswordLoginAllowed 封装密码登录Allowed业务协调。
func (m *CooldownManager) PasswordLoginAllowed(cookieID string, loginCooldown time.Duration) (bool, time.Duration, string) {
	if m == nil {
		return true, 0, ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// now 用于本次流程后续判断的now
	now := time.Now()
	// last 用于本次流程后续判断的last
	last := m.passwordLoginByCookie[cookieID]
	if !last.IsZero() {
		// remain 用于本次流程后续判断的remain
		remain := loginCooldown - now.Sub(last)
		if remain > 0 {
			return false, remain, "login_cooldown"
		}
	}
	// lastErr 用于本次流程后续判断的lastErr
	lastErr := m.passwordErrorByCookie[cookieID]
	if !lastErr.IsZero() {
		// remain 用于本次流程后续判断的remain
		remain := passwordErrorCooldown - now.Sub(lastErr)
		if remain > 0 {
			return false, remain, "password_error_cooldown"
		}
	}
	return true, 0, ""
}

// MarkPasswordLogin 封装Mark密码登录业务协调。
func (m *CooldownManager) MarkPasswordLogin(cookieID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// now 用于本次流程后续判断的now
	now := time.Now()
	m.passwordLoginByCookie[cookieID] = now
}

// MarkPasswordError 封装Mark密码错误业务协调。
func (m *CooldownManager) MarkPasswordError(cookieID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.passwordErrorByCookie[cookieID] = time.Now()
}

// Reset 封装Reset业务协调。
func (m *CooldownManager) Reset(cookieID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessionExpiredByCookie, cookieID)
	delete(m.passwordLoginByCookie, cookieID)
	delete(m.passwordErrorByCookie, cookieID)
}
