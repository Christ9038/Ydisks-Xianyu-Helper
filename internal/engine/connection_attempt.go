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

// registerConnection 在锁内复核凭证快照、锁外执行 WebSocket 注册，并在返回前再次确认凭证未变。
// Account facade 继续负责根据 Registered/Err 决定重载 Cookie、重试或结束运行。

// registerConnection 是凭证校验与 WebSocket 注册的生命周期入口。
func (a *Account) registerConnection(ctx context.Context, conn WSConn, deviceID, accessToken, tokenCredentialFP string) registerConnectionResult {
	// credentialUnlock 是当前账号凭证锁的释放函数。
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	if !a.cookieSnapshotMatchesDB(ctx, tokenCredentialFP) {
		credentialUnlock()
		return registerConnectionResult{}
	}
	credentialUnlock()
	// registerErr 保存锁外 WebSocket 注册的结果。
	registerErr := conn.Register(ctx, deviceID, accessToken)
	if a.store != nil {
		// verifyUnlock 保护注册完成后的凭证一致性复核，避免旧连接继续进入在线状态。
		verifyUnlock := a.store.LockAccountCredentials(a.CookieID)
		// stillCurrent 表示注册完成后数据库凭证仍与 Token 绑定指纹一致。
		stillCurrent := a.cookieSnapshotMatchesDB(ctx, tokenCredentialFP)
		verifyUnlock()
		if !stillCurrent {
			return registerConnectionResult{}
		}
	}
	return registerConnectionResult{Registered: true, Err: registerErr}
}
