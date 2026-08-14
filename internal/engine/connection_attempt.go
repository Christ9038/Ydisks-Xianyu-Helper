package engine

import "context"

// registerConnectionResult 描述凭证快照校验与 WebSocket 注册结果。

// registerConnectionResult 是连接注册边界返回的结果结构。
type registerConnectionResult struct {
	// Registered 表示本次凭证快照是否仍与数据库一致。
	Registered bool
	// Err 是 WebSocket Register 返回的错误。
	Err error
}

// registerConnection 在账号凭证锁内完成快照复核和 WebSocket 注册。
// 凭证锁只覆盖快照读取与 Register，不覆盖后续连接循环或任何通知 I/O；
// Account facade 继续负责根据 Registered/Err 决定重载 Cookie、重试或结束运行。

// registerConnection 是凭证校验与 WebSocket 注册的生命周期入口。
func (a *Account) registerConnection(ctx context.Context, conn WSConn, deviceID, accessToken, tokenCredentialFP string) registerConnectionResult {
	// credentialUnlock 是当前账号凭证锁的释放函数。
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	if !a.cookieSnapshotMatchesDB(ctx, tokenCredentialFP) {
		return registerConnectionResult{}
	}
	return registerConnectionResult{Registered: true, Err: conn.Register(ctx, deviceID, accessToken)}
}
