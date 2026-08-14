package engine

import (
	"sync"
	"time"
)

// credentialState 保存账号 Cookie、Token、设备指纹和刷新诊断状态。
// mu 保护本组件全部字段；持锁时不得执行数据库、网络或通知 I/O。
// refreshMu 串行化完整的凭证刷新事务，允许在锁内执行必要的凭证 I/O，
// 但调用方不得在持有它时再次获取同一锁。
type credentialState struct {
	// mu 保护 Cookie、Token、设备指纹和刷新诊断字段。
	mu sync.Mutex
	// refreshMu 串行化 Cookie 读取、Token 刷新和缓存清理事务。
	refreshMu sync.Mutex

	// CookieStr 是当前运行时使用的扁平 Cookie 快照。
	CookieStr string
	// UserID 是从 Cookie 的 unb 字段解析出的闲鱼用户标识。
	UserID string
	// currentToken 是最近一次获取的连接级访问 Token。
	currentToken string
	// deviceID 是页面生命周期内复用的设备标识。
	deviceID string
	// lastTokenRefresh 是最近一次开始 Token 刷新的时间。
	lastTokenRefresh time.Time
	// lastCaptchaFailure 是最近一次 Token 风控验证失败时间。
	lastCaptchaFailure time.Time
	// lastTokenStatus 是最近一次 Token 刷新状态。
	lastTokenStatus string
	// tokenFetchFailures 是当前连接周期内 Token 获取失败次数。
	tokenFetchFailures int
	// credentialFP 是当前 Cookie 与权威 Cookie Jar 的完整状态指纹。
	credentialFP string
	// tokenCredentialFP 是当前 Token 获取时绑定的凭证状态指纹。
	tokenCredentialFP string
	// tokenAcquiredAt 是最近一次成功获取 Token 的时间。
	tokenAcquiredAt time.Time
	// tokenExpiresAt 是服务端声明的 Token 过期时间。
	tokenExpiresAt time.Time
	// tokenRefreshAt 是本地提前轮换 Token 的时间。
	tokenRefreshAt time.Time
	// tokenFingerprint 是 Token 的不可逆诊断指纹，不保存 Token 原文。
	tokenFingerprint string
}
