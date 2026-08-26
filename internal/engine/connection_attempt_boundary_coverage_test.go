package engine

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/xianyu/ws"
)

// TestRegisterConnectionCoversSnapshotAndRegistrationOutcomes 验证 WebSocket 注册边界的凭证快照与错误结果。
func TestRegisterConnectionCoversSnapshotAndRegistrationOutcomes(t *testing.T) {
	// noStoreAccount 是无数据库兼容模式下的测试账号。
	noStoreAccount := New(Config{CookieID: "no-store", CookieStr: "unb=1"})
	// noStoreConnection 是记录注册参数的本地 WebSocket 连接。
	noStoreConnection := &fakeWSConn{}
	// noStoreResult 是无数据库账号的注册结果。
	noStoreResult := noStoreAccount.registerConnection(context.Background(), noStoreConnection, "device", "token", "")
	if !noStoreResult.Registered || noStoreResult.Err != nil {
		t.Fatalf("无数据库账号应允许注册：%+v", noStoreResult)
	}

	// account、handler、store、cleanup 是带数据库账号及其测试资源。
	account, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "token"})
	defer cleanup()
	// runtimeData 保存注册前的权威凭证快照。
	runtimeData, readErr := store.Cookies.GetCookieRuntimeData(context.Background(), account.CookieID)
	if readErr != nil {
		t.Fatalf("读取凭证快照失败：%v", readErr)
	}
	// fingerprint 是本次 Token 绑定的凭证指纹。
	fingerprint := credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON)

	// staleConnection 是凭证已经变化时不应真正注册的连接。
	staleConnection := &fakeWSConn{}
	// staleResult 是旧凭证指纹的注册结果。
	staleResult := account.registerConnection(context.Background(), staleConnection, account.deviceID, "token", "stale-fingerprint")
	if staleResult.Registered || staleConnection.registeredTok != "" {
		t.Fatalf("旧凭证不应注册：result=%+v token=%q", staleResult, staleConnection.registeredTok)
	}

	// registerError 是 WebSocket 注册返回的服务端拒绝错误。
	registerError := &ws.RegError{Kind: ws.RegErrorInvalidToken, Code: 401, Reason: "invalid token"}
	// errorConnection 是返回服务端拒绝的本地连接。
	errorConnection := &fakeWSConn{registerErr: registerError}
	// errorResult 是注册失败但凭证仍一致时的边界结果。
	errorResult := account.registerConnection(context.Background(), errorConnection, account.deviceID, "token", fingerprint)
	if !errorResult.Registered || !errors.Is(errorResult.Err, registerError) {
		t.Fatalf("注册错误仍应保留 Registered：result=%+v", errorResult)
	}

	// changedConnection 是注册期间等待凭证变化复核的阻塞连接。
	changedConnection := &blockingRegisterConn{started: make(chan struct{}), release: make(chan struct{})}
	// changedResult 保存注册完成后发现凭证变化的结果。
	changedResult := make(chan registerConnectionResult, 1)
	go func() {
		changedResult <- account.registerConnection(context.Background(), changedConnection, account.deviceID, "token", fingerprint)
	}()
	select {
	case <-changedConnection.started:
	case <-context.Background().Done():
		t.Fatal("阻塞注册连接未开始")
	}
	// updateErr 表示模拟注册期间外部凭证更新的持久化结果。
	updateErr := store.Cookies.UpdateRenewalCookie(context.Background(), account.CookieID, "unb=2; changed=1", `{}`, 1)
	if updateErr != nil {
		t.Fatalf("更新测试凭证失败：%v", updateErr)
	}
	close(changedConnection.release)
	// changedFinalResult 是凭证变化后的最终注册结果。
	changedFinalResult := <-changedResult
	if changedFinalResult.Registered {
		t.Fatalf("注册后凭证变化时不应继续使用旧连接：%+v", changedFinalResult)
	}
}
