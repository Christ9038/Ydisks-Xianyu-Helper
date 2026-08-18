package engine

import (
	"context"
	"sync"
	"time"
)

// credentialState 保存账号 Cookie、Token、设备指纹和刷新诊断状态。
// mu 保护本组件全部字段；持锁时不得执行数据库、网络或通知 I/O。
// refreshGate 通过带令牌的通道串行化完整刷新流程，等待外部 I/O 时不持有互斥锁。
// credentialState 用于本次流程后续判断的credential状态
type credentialState struct {
	// mu 保护 Cookie、Token、设备指纹和刷新诊断字段。
	mu sync.Mutex
	// refreshGate 串行化 Cookie 读取、Token 刷新和缓存清理事务；通道令牌代表刷新所有权。
	refreshGate chan struct{}

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

// newRefreshGate 创建带有初始令牌的账号刷新通道。
func newRefreshGate() chan struct{} {
	// gate 保存刷新流程的单令牌通道。
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return gate
}

// acquireRefreshGate 获取账号刷新流程的独占通道令牌。等待期间会响应 ctx 取消，
// 因此停止账号不会被另一个尚未完成的续期流程无限阻塞。
func (s *credentialState) acquireRefreshGate(ctx context.Context) (func(), error) {
	if ctx == nil {
		// ctx 使用 Background 保持历史未传取消信号调用方的可用性。
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.refreshGate == nil {
		s.refreshGate = make(chan struct{}, 1)
		s.refreshGate <- struct{}{}
	}
	// gate 保存初始化后可在外部等待的刷新令牌通道。
	gate := s.refreshGate
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
		// release 把令牌归还给同一个固定通道；调用者必须在所有提交路径上执行它。
		return func() { gate <- struct{}{} }, nil
	}
}
