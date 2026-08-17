package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/renew"
)

// blockingCredentialUpdateHandler 在凭证更新通知中阻塞，用于探测通知期间是否仍持有数据库凭证锁。
type blockingCredentialUpdateHandler struct {
	recordingHandler
	// started 表示凭证通知已经进入可扩展 Handler。
	started chan struct{}
	// release 允许测试结束通知回调。
	release chan struct{}
}

// OnCredentialUpdated 阻塞凭证通知，模拟真实通知器可能进行的慢速外部 I/O。
func (h *blockingCredentialUpdateHandler) OnCredentialUpdated(context.Context, string) {
	close(h.started)
	<-h.release
}

// blockingTokenMtop 在 Token 请求期间阻塞，用于验证 Engine 不跨越网络 I/O 持有凭证锁。
type blockingTokenMtop struct {
	fakeRunMtop
	// started 表示 Token 外部请求已经开始。
	started chan struct{}
	// release 允许测试结束第一个 Token 请求阻塞。
	release chan struct{}
	// calls 记录 Token 请求次数，用于确认并发凭证变化会触发重试。
	calls atomic.Int32
}

// TestRefreshGateAcquireHonorsContext 验证等待中的续期不会阻塞账号停止流程。
func TestRefreshGateAcquireHonorsContext(t *testing.T) {
	// state 是持有单令牌刷新通道的最小凭证状态。
	state := credentialState{refreshGate: newRefreshGate()}
	// release 是第一个续期所有者归还通道令牌的释放函数。
	release, acquireErr := state.acquireRefreshGate(context.Background())
	if acquireErr != nil {
		t.Fatalf("取得初始刷新通道: %v", acquireErr)
	}
	defer release()
	// ctx、cancel 表示等待第二次续期时由账号停止触发的取消信号。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// blockedRelease、blockedErr 保存已取消等待的返回结果。
	blockedRelease, blockedErr := state.acquireRefreshGate(ctx)
	if blockedRelease != nil || blockedErr != context.Canceled {
		t.Fatalf("已取消的刷新通道等待结果 release=%v err=%v", blockedRelease != nil, blockedErr)
	}
}

// TestPendingRenewNotificationRunsOutsideCredentialLock 验证迟到续期通知在锁外执行。
func TestPendingRenewNotificationRunsOutsideCredentialLock(t *testing.T) {
	// acc、store、cleanup 是待验证凭证通知锁边界的账号、数据库和清理函数。
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "token"})
	defer cleanup()
	// handler 是会阻塞通知的可扩展回调实现。
	handler := &blockingCredentialUpdateHandler{started: make(chan struct{}), release: make(chan struct{})}
	acc.handler = handler
	// finished 表示迟到续期持久化及通知调用结束。
	finished := make(chan struct{})
	go func() {
		// persistErr 是迟到续期写回失败原因。
		persistErr := acc.persistPendingRenewCookies(context.Background(), &renew.Result{SetCookies: []string{"late=1; Path=/"}})
		if persistErr != nil {
			t.Errorf("迟到续期写回失败: %v", persistErr)
		}
		close(finished)
	}()
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("凭证更新通知未开始")
	}
	// acquired 表示通知阻塞期间另一个流程取得并释放了同账号凭证锁。
	acquired := make(chan struct{})
	go func() {
		// unlock 释放探测流程取得的凭证锁。
		unlock := store.LockAccountCredentials(acc.CookieID)
		unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("凭证更新通知仍持有数据库凭证锁")
	}
	close(handler.release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("迟到续期通知未结束")
	}
}

// RefreshTokenWithDeviceIDContext 阻塞第一个请求并返回成功 Token。
func (c *blockingTokenMtop) RefreshTokenWithDeviceIDContext(ctx context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	// callNumber 是当前 Token 请求序号。
	callNumber := c.calls.Add(1)
	if callNumber == 1 {
		close(c.started)
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &mtop.RefreshResult{AccessToken: "fresh-token", AccessTokenExpireAt: time.Now().Add(time.Hour).Unix()}, nil
}

// blockingStatusMtop 在登录态检查期间阻塞，用于验证 Engine 不跨越慢速 I/O 持有凭证锁。
type blockingStatusMtop struct {
	fakeRunMtop
	// started 表示登录态检查外部调用已经开始。
	started chan struct{}
	// release 允许测试结束登录态检查阻塞。
	release chan struct{}
	// result 是检查完成后返回的登录态结果。
	result *mtop.LoginStatusResult
}

// blockingRegisterConn 在 WebSocket 注册期间阻塞，用于验证注册网络 I/O 不持有凭证锁。
type blockingRegisterConn struct {
	fakeWSConn
	// started 通知注册外部调用已经开始。
	started chan struct{}
	// release 允许注册外部调用返回。
	release chan struct{}
}

// Register 阻塞到测试显式释放后完成注册。
func (c *blockingRegisterConn) Register(ctx context.Context, _, _ string) error {
	close(c.started)
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CheckLoginStatusContext 阻塞到测试显式释放后返回登录态结果。
func (c *blockingStatusMtop) CheckLoginStatusContext(ctx context.Context, _ string) (*mtop.LoginStatusResult, error) {
	close(c.started)
	select {
	case <-c.release:
		return c.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestTryLoginStatusCheckReleasesCredentialLockAndRejectsStaleResponse 验证 Engine 登录态检查不占锁且不会覆盖并发 Cookie。
func TestTryLoginStatusCheckReleasesCredentialLockAndRejectsStaleResponse(t *testing.T) {
	// mtopClient 是阻塞登录态检查外部调用的测试桩。
	mtopClient := &blockingStatusMtop{
		fakeRunMtop: fakeRunMtop{token: "token"},
		started:     make(chan struct{}),
		release:     make(chan struct{}),
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusTokenRefreshed, UpdatedCookies: "unb=123; stale=old"},
	}
	// acc、store、cleanup 是待验证 Engine 凭证协调行为的账号、数据库和清理函数。
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// finished 表示登录态检查调用已经返回。
	finished := make(chan struct{})
	go func() {
		acc.tryLoginStatusCheck(context.Background())
		close(finished)
	}()
	select {
	case <-mtopClient.started:
	case <-time.After(time.Second):
		t.Fatal("Engine 登录态检查请求未开始")
	}
	// lockReleased 表示其他调用方已经成功取得并释放账号凭证锁。
	lockReleased := make(chan struct{})
	go func() {
		// unlock 是探测调用方取得的账号凭证锁释放函数。
		unlock := store.LockAccountCredentials(acc.CookieID)
		unlock()
		close(lockReleased)
	}()
	select {
	case <-lockReleased:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("慢速 Engine 登录态检查仍持有共享凭证锁")
	}
	// updateErr 表示并发流程写入新 Cookie 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(context.Background(), acc.CookieID, "unb=123; concurrent=kept", `{}`, time.Now().Unix()); updateErr != nil {
		t.Fatalf("写入并发 Cookie: %v", updateErr)
	}
	close(mtopClient.release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Engine 登录态检查调用未完成")
	}
	// saved 是并发更新与旧登录态响应处理后的数据库 Cookie。
	saved, savedErr := store.Cookies.GetValue(context.Background(), acc.CookieID)
	if savedErr != nil {
		t.Fatalf("读取 Engine 登录态检查后的 Cookie: %v", savedErr)
	}
	if !strings.Contains(saved, "concurrent=kept") || strings.Contains(saved, "stale=old") {
		t.Fatalf("旧 Engine 登录态响应覆盖了并发 Cookie: %q", saved)
	}
}

// TestTryAPIRenewReleasesCredentialLockAndRebasesSetCookies 验证 Engine API 续期不占锁且会基于最新 Cookie 重放响应。
func TestTryAPIRenewReleasesCredentialLockAndRebasesSetCookies(t *testing.T) {
	// mtopClient 是创建 Engine 账号所需的最小协议客户端桩。
	mtopClient := &statusMtop{fakeRunMtop: fakeRunMtop{token: "token"}}
	// acc、store、cleanup 是待验证 Engine API 续期协调行为的账号、数据库和清理函数。
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// started 表示外部 API 续期回调已经开始。
	started := make(chan struct{})
	// release 允许测试结束外部 API 续期回调阻塞。
	release := make(chan struct{})
	// finished 表示 API 续期调用已经返回。
	finished := make(chan struct{})
	go func() {
		// callErr 是 API 续期协调调用的错误结果。
		_, callErr := acc.tryAPIRenewUsing(context.Background(), func(ctx context.Context, _ string, _ []cookierefresh.BrowserCookie) (*renew.Result, error) {
			close(started)
			select {
			case <-release:
				return &renew.Result{Success: true, NewCookies: "unb=123; stale=old", SetCookies: []string{"stale=old; Path=/"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		if callErr != nil {
			// callErr 只在测试桩错误时报告，避免异步错误丢失。
			t.Errorf("Engine API 续期失败: %v", callErr)
		}
		close(finished)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Engine API 续期回调未开始")
	}
	// lockReleased 表示其他调用方已经成功取得并释放账号凭证锁。
	lockReleased := make(chan struct{})
	go func() {
		// unlock 是探测调用方取得的账号凭证锁释放函数。
		unlock := store.LockAccountCredentials(acc.CookieID)
		unlock()
		close(lockReleased)
	}()
	select {
	case <-lockReleased:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("慢速 Engine API 续期仍持有共享凭证锁")
	}
	// updateErr 表示并发流程写入新 Cookie 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(context.Background(), acc.CookieID, "unb=123; concurrent=kept", `{}`, time.Now().Unix()); updateErr != nil {
		t.Fatalf("写入并发 Cookie: %v", updateErr)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Engine API 续期调用未完成")
	}
	// saved 是最新并发 Cookie 与续期 Set-Cookie 合并后的数据库值。
	saved, savedErr := store.Cookies.GetValue(context.Background(), acc.CookieID)
	if savedErr != nil {
		t.Fatalf("读取 Engine API 续期后的 Cookie: %v", savedErr)
	}
	if !strings.Contains(saved, "concurrent=kept") || !strings.Contains(saved, "stale=old") {
		t.Fatalf("Engine API 续期覆盖了并发 Cookie: %q", saved)
	}
}

// TestRefreshTokenReleasesCredentialLockAndRetriesAfterConcurrentUpdate 验证 Token 网络请求不占锁且凭证变化会触发最新快照重试。
func TestRefreshTokenReleasesCredentialLockAndRetriesAfterConcurrentUpdate(t *testing.T) {
	// mtopClient 是阻塞第一个 Token 请求的协议客户端桩。
	mtopClient := &blockingTokenMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		started:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	// acc、store、cleanup 是待验证 Engine Token 协调行为的账号、数据库和清理函数。
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// finished 表示 Token 刷新调用已经返回。
	finished := make(chan struct{})
	// tokenResult 保存 Token 刷新的最终结果。
	var tokenResult string
	// tokenErr 保存 Token 刷新的错误结果。
	var tokenErr error
	go func() {
		tokenResult, _, tokenErr = acc.refreshTokenWithMinGap(context.Background(), false)
		close(finished)
	}()
	select {
	case <-mtopClient.started:
	case <-time.After(time.Second):
		t.Fatal("Engine Token 请求未开始")
	}
	// lockReleased 表示其他调用方已经成功取得并释放账号凭证锁。
	lockReleased := make(chan struct{})
	go func() {
		// unlock 是探测调用方取得的账号凭证锁释放函数。
		unlock := store.LockAccountCredentials(acc.CookieID)
		unlock()
		close(lockReleased)
	}()
	select {
	case <-lockReleased:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("慢速 Engine Token 请求仍持有共享凭证锁")
	}
	// updateErr 表示并发流程写入新 Cookie 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(context.Background(), acc.CookieID, "unb=123; concurrent=kept", `{}`, time.Now().Unix()); updateErr != nil {
		t.Fatalf("写入并发 Cookie: %v", updateErr)
	}
	close(mtopClient.release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Engine Token 刷新调用未完成")
	}
	if tokenErr != nil || tokenResult != "fresh-token" {
		t.Fatalf("Engine Token 刷新失败: token=%q err=%v", tokenResult, tokenErr)
	}
	if mtopClient.calls.Load() < 2 {
		t.Fatalf("并发凭证变化后应使用最新快照重试，calls=%d", mtopClient.calls.Load())
	}
	// saved 是最新并发 Cookie 在 Token 重试后的数据库值。
	saved, savedErr := store.Cookies.GetValue(context.Background(), acc.CookieID)
	if savedErr != nil || !strings.Contains(saved, "concurrent=kept") {
		t.Fatalf("并发 Cookie 未保留: value=%q err=%v", saved, savedErr)
	}
}

// TestRegisterConnectionReleasesCredentialLockBeforeExternalIO 验证 WebSocket Register 在锁外执行。
func TestRegisterConnectionReleasesCredentialLockBeforeExternalIO(t *testing.T) {
	// acc、store、cleanup 是待验证 Engine 连接注册行为的账号、数据库和清理函数。
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "token"})
	defer cleanup()
	// runtimeData 保存注册前的权威凭证快照。
	runtimeData, err := store.Cookies.GetCookieRuntimeData(context.Background(), acc.CookieID)
	if err != nil {
		t.Fatal(err)
	}
	// conn 保存可控阻塞的 WebSocket 连接。
	conn := &blockingRegisterConn{started: make(chan struct{}), release: make(chan struct{})}
	// result 保存注册边界的最终结果。
	result := make(chan registerConnectionResult, 1)
	go func() {
		result <- acc.registerConnection(context.Background(), conn, acc.deviceID, "token", credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON))
	}()
	select {
	case <-conn.started:
	case <-time.After(time.Second):
		t.Fatal("WebSocket Register 未开始")
	}
	// acquired 表示另一个流程已经成功获取并释放同账号凭证锁。
	acquired := make(chan struct{})
	go func() {
		// unlock 释放探测流程取得的账号凭证锁。
		unlock := store.LockAccountCredentials(acc.CookieID)
		unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("WebSocket Register 外部调用仍持有账号凭证锁")
	}
	close(conn.release)
	select {
	// registered 保存 WebSocket 注册边界的结果快照。
	case registered := <-result:
		if !registered.Registered || registered.Err != nil {
			t.Fatalf("注册结果异常: %+v", registered)
		}
	case <-time.After(time.Second):
		t.Fatal("注册边界未完成")
	}
}
