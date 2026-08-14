package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// countingMtop 包装 fakeRunMtop，计数 RefreshToken 调用次数。
type countingMtop struct {
	fakeRunMtop
	calls int32
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (c *countingMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	atomic.AddInt32(&c.calls, 1)
	return &mtop.RefreshResult{AccessToken: c.token, AccessTokenExpireAt: time.Now().Add(time.Hour).Unix()}, nil
}

// statusMtop 保存状态Mtop，供当前处理流程使用
type statusMtop struct {
	fakeRunMtop
	result *mtop.LoginStatusResult
	err    error
}

// failingCountingMtop 保存failingCountingMtop，供当前处理流程使用
type failingCountingMtop struct {
	fakeRunMtop
	calls int32
	err   error
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (c *failingCountingMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	atomic.AddInt32(&c.calls, 1)
	return nil, c.err
}

// saveBoundToken 负责saveBound令牌相关处理。
func saveBoundToken(t *testing.T, store *db.Store, acc *Account, token string, expireAt int64) {
	t.Helper()
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.SaveBound(context.Background(), acc.CookieID, acc.deviceID, token, expireAt,
		credentialCookieFingerprint(acc.currentCookieStr())); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshTokenNetworkFailureClearsMemoryButPreservesFreshCache 负责TestRefresh令牌NetworkFailureClearsMemoryButPreservesFreshCache相关处理。
func TestRefreshTokenNetworkFailureClearsMemoryButPreservesFreshCache(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &failingCountingMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		err:         errors.New("network connection reset"),
	}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "cached-tok", time.Now().Add(time.Hour).Unix())
	acc.mu.Lock()
	acc.currentToken = "old-memory-token"
	acc.mu.Unlock()
	if // err 保存err，供当前处理流程使用
	_, _, err := acc.refreshToken(context.Background()); err == nil {
		t.Fatal("网络失败应返回错误")
	}
	acc.mu.Lock()
	// current 保存current，供当前处理流程使用
	current := acc.currentToken
	acc.mu.Unlock()
	if current != "" {
		t.Fatalf("刷新网络失败后应清空内存 token: %q", current)
	}
	if // cached、err 保存cached、err，供当前处理流程使用
	cached, err := store.Tokens.Get(context.Background(), "cid"); err != nil || cached.AccessToken != "cached-tok" {
		t.Fatalf("网络失败应保留未过期数据库缓存: cached=%+v err=%v", cached, err)
	}
}

// TestRefreshTokenBusinessFailureClearsMemoryAndCache 负责TestRefresh令牌BusinessFailureClearsMemoryAndCache相关处理。
func TestRefreshTokenBusinessFailureClearsMemoryAndCache(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &failingCountingMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		err:         errors.New("token API business rejected"),
	}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "cached-tok", time.Now().Add(time.Hour).Unix())
	acc.mu.Lock()
	acc.currentToken = "old-memory-token"
	acc.mu.Unlock()
	if // err 保存err，供当前处理流程使用
	_, _, err := acc.refreshToken(context.Background()); err == nil {
		t.Fatal("业务失败应返回错误")
	}
	acc.mu.Lock()
	// current 保存current，供当前处理流程使用
	current := acc.currentToken
	acc.mu.Unlock()
	if current != "" {
		t.Fatalf("业务失败后应清空内存 token: %q", current)
	}
	if // tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != acc.deviceID {
		t.Fatalf("业务失败应清 token 并保留 device ID: tk=%+v err=%v", tk, err)
	}
}

// TestAcquireTokenDeletesExpiredCacheBeforeNetworkAttempt 负责TestAcquire令牌DeletesExpiredCacheBeforeNetwork尝试次数相关处理。
func TestAcquireTokenDeletesExpiredCacheBeforeNetworkAttempt(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &failingCountingMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		err:         errors.New("network connection reset"),
	}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "expired-tok", time.Now().Add(-time.Minute).Unix())
	if // err 保存err，供当前处理流程使用
	_, _, err := acc.acquireToken(context.Background()); err == nil {
		t.Fatal("后续网络请求失败应返回错误")
	}
	if // tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != acc.deviceID {
		t.Fatalf("过期 token 应清空且 device ID 保留: tk=%+v err=%v", tk, err)
	}
}

// CheckLoginStatusContext 检查登录状态上下文。
func (s *statusMtop) CheckLoginStatusContext(context.Context, string) (*mtop.LoginStatusResult, error) {
	return s.result, s.err
}

// TestAcquireToken_CacheHitStillFetchesFreshToken matches the official web
// client: every connection attempt calls login.token even when a persisted
// token has not reached its advertised expiry.
// TestAcquireToken_CacheHitStillFetchesFreshToken 负责TestAcquire令牌CacheHitStillFetchesFresh令牌相关处理。
func TestAcquireToken_CacheHitStillFetchesFreshToken(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	saveBoundToken(t, store, acc, "cached-tok", time.Now().Add(time.Hour).Unix())
	// token、cookies、err 保存token、cookies、err，供当前处理流程使用
	token, cookies, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatalf("acquireToken: %v", err)
	}
	if token != "tok-mtop" {
		t.Fatalf("应使用新获取 token，got %s", token)
	}
	if // calls 保存calls，供当前处理流程使用
	calls := atomic.LoadInt32(&mtopClient.calls); calls != 1 {
		t.Fatalf("每次连接都应调用 mtop，got %d", calls)
	}
	if !strings.Contains(cookies, "_m_h5_tk=tk_1") {
		t.Fatalf("缓存命中应返回其绑定的 Cookie: %q", cookies)
	}
}

// TestAcquireToken_RejectsCacheBoundToDifferentCookie 负责TestAcquire令牌RejectsCacheBoundToDifferent登录凭证相关处理。
func TestAcquireToken_RejectsCacheBoundToDifferentCookie(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "fresh-token"}}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "stale-token", time.Now().Add(time.Hour).Unix())
	// newCookie 保存new登录凭证，供当前处理流程使用
	newCookie := "unb=123; _m_h5_tk=renewed_2; cookie2=new"
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateValueExisting(context.Background(), "cid", newCookie); err != nil {
		t.Fatal(err)
	}
	acc.replaceCookieStr(newCookie)

	// token、err 保存token、err，供当前处理流程使用
	token, _, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh-token" || atomic.LoadInt32(&mtopClient.calls) != 1 {
		t.Fatalf("token=%q calls=%d want fresh token", token, atomic.LoadInt32(&mtopClient.calls))
	}
}

// TestAcquireRuntimeTokenReplacesMemoryToken 负责TestAcquireRuntime令牌ReplacesMemory令牌相关处理。
func TestAcquireRuntimeTokenReplacesMemoryToken(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	acc.currentToken = "runtime-token"
	_ = store.Tokens.Clear(context.Background(), "cid")

	// token、err 保存token、err，供当前处理流程使用
	token, _, err := acc.acquireRuntimeToken(context.Background())
	if err != nil || token != "tok-mtop" {
		t.Fatalf("runtime token=%q err=%v", token, err)
	}
	if // calls 保存calls，供当前处理流程使用
	calls := atomic.LoadInt32(&mtopClient.calls); calls != 1 {
		t.Fatalf("网络重连应获取新 token，calls=%d", calls)
	}
}

// TestAcquireToken_CacheMissCallsMtopAndSavesCache 缓存未命中时调 mtop 并把结果落库。
func TestAcquireToken_CacheMissCallsMtopAndSavesCache(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	// token、err 保存token、err，供当前处理流程使用
	token, _, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatalf("acquireToken: %v", err)
	}
	if token != "tok-mtop" {
		t.Fatalf("应使用 mtop token，got %s", token)
	}
	if // calls 保存calls，供当前处理流程使用
	calls := atomic.LoadInt32(&mtopClient.calls); calls != 1 {
		t.Fatalf("缓存未命中应调一次 mtop，got %d", calls)
	}
	// tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(context.Background(), "cid")
	if err != nil {
		t.Fatalf("缓存应已落库: %v", err)
	}
	if tk.AccessToken != "tok-mtop" {
		t.Fatalf("缓存 token 应为 tok-mtop，got %s", tk.AccessToken)
	}
	if tk.ExpireAt <= time.Now().Unix() {
		t.Fatalf("缓存 expire_at 应在未来，got %d", tk.ExpireAt)
	}
}

// TestTryLoginStatusCheck_AdoptsUpdatedCookie loginuser.get 下发新 Cookie 时应采纳并清旧 token。
func TestTryLoginStatusCheck_AdoptsUpdatedCookie(t *testing.T) {
	// newCookie 保存new登录凭证，供当前处理流程使用
	newCookie := "unb=123; _m_h5_tk=loginuser_new; sgcookie=abc"
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result: &mtop.LoginStatusResult{
			Status:         mtop.LoginStatusTokenRefreshed,
			UpdatedCookies: newCookie,
			Message:        "令牌已刷新",
		},
	}
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	// oldDeviceID 保存oldDeviceID，供当前处理流程使用
	oldDeviceID := acc.deviceID
	saveBoundToken(t, store, acc, "old-tok", time.Now().Add(time.Hour).Unix())
	// res 保存响应，供当前处理流程使用
	res := acc.tryLoginStatusCheck(context.Background())
	if !res.recovered || res.riskRequired {
		t.Fatalf("登录态检查应恢复 Cookie，got %+v", res)
	}
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != newCookie {
		t.Fatalf("内存 Cookie 未更新: got %q want %q", got, newCookie)
	}
	// saved、err 保存saved、err，供当前处理流程使用
	saved, err := store.Cookies.GetValue(context.Background(), "cid")
	if err != nil || saved != newCookie {
		t.Fatalf("DB Cookie 未更新: got %q err=%v", saved, err)
	}
	if // tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != oldDeviceID || acc.deviceID != oldDeviceID {
		t.Fatalf("Cookie 更新后应清 token 并保持页面 device ID: runtime=%q tk=%+v err=%v", acc.deviceID, tk, err)
	}
}

// TestTryLoginStatusCheck_RiskRequired loginuser.get 命中风控时应进入验证态，不继续普通续期。
func TestTryLoginStatusCheck_RiskRequired(t *testing.T) {
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result: &mtop.LoginStatusResult{
			Status:          mtop.LoginStatusRiskRequired,
			Ret:             []string{"FAIL_SYS_USER_VALIDATE::RGV587"},
			VerificationURL: "https://passport.goofish.com/punish?x5secdata=1",
			Message:         "闲鱼要求安全验证",
		},
	}
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	// res 保存响应，供当前处理流程使用
	res := acc.tryLoginStatusCheck(context.Background())
	if !res.riskRequired || res.recovered {
		t.Fatalf("登录态检查应返回风控状态，got %+v", res)
	}
	if res.verificationURL == "" {
		t.Fatal("应保留风控验证 URL")
	}
	if // got 保存got，供当前处理流程使用
	got := acc.RuntimeStatus().State; got != RuntimeVerificationRequired {
		t.Fatalf("风控时状态应为 verification_required，got %q", got)
	}
}

// TestHandleMaxFailuresRunsLoginStatusCheckBeforeProtocolRenew 负责TestHandleMaxFailures运行记录登录状态CheckBeforeProtocolRenew相关处理。
func TestHandleMaxFailuresRunsLoginStatusCheckBeforeProtocolRenew(t *testing.T) {
	// newCookie 保存new登录凭证，供当前处理流程使用
	newCookie := "unb=123; _m_h5_tk=loginuser_recovered; sgcookie=abc"
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result: &mtop.LoginStatusResult{
			Status:         mtop.LoginStatusTokenRefreshed,
			UpdatedCookies: newCookie,
			Message:        "令牌已刷新",
		},
	}
	// acc、handler、cleanup 保存acc、handler、cleanup，供当前处理流程使用
	acc, handler, _, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	acc.mu.Lock()
	acc.connFailures = MaxConnectionFailures
	acc.mu.Unlock()
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if // err 保存err，供当前处理流程使用
	err := acc.handleMaxFailures(ctx); err != ctx.Err() {
		t.Fatalf("登录态恢复后的等待应响应上下文取消: %v", err)
	}
	if handler.refresh != 0 {
		t.Fatalf("loginuser.get 已恢复时不应继续协议续期，calls=%d", handler.refresh)
	}
	if // got 保存got，供当前处理流程使用
	got := acc.currentCookieStr(); got != newCookie {
		t.Fatalf("登录态检查恢复 Cookie=%q want %q", got, newCookie)
	}
	if acc.connFailures != 0 {
		t.Fatalf("登录态恢复后失败计数=%d want 0", acc.connFailures)
	}
}

// TestReloadCookieFromDB_AdoptsNewCookieAndClearsCache DB cookie 变更时采纳新 cookie 并清旧 token 缓存。
func TestReloadCookieFromDB_AdoptsNewCookieAndClearsCache(t *testing.T) {
	// acc、store、cleanup 保存acc、store、cleanup，供当前处理流程使用
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	// 预置旧 session 的 token 缓存。
	oldDeviceID := acc.deviceID
	saveBoundToken(t, store, acc, "old-tok", time.Now().Add(time.Hour).Unix())
	// 外部更新 DB cookie（模拟重新扫码），userID=0 复用现有 user_id。
	newCookie := "unb=123; _m_h5_tk=newtk_2; sgcookie=abc"
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(context.Background(), "cid", newCookie, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	acc.reloadCookieFromDB(context.Background())
	if // got 保存got，供当前处理流程使用
	got := acc.CurrentCookieStr(); got != newCookie {
		t.Fatalf("应采纳 DB 新 cookie，got %s", got)
	}
	// 普通 Cookie Jar 更新不等于页面 reload：旧 token 清除，device ID 保持。
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != oldDeviceID || acc.deviceID != oldDeviceID {
		t.Fatalf("cookie 变更后应清 token 并保持运行时 device ID: runtime=%q tk=%+v err=%v", acc.deviceID, tk, err)
	}
}

// TestHandleMaxFailures_AlertOnce 连续两次进入 false 终止路径只告警一次。
func TestHandleMaxFailures_AlertOnce(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := &failingRefreshHandler{}
	acc.handler = h

	// call 保存call，供当前处理流程使用
	call := func() {
		acc.mu.Lock()
		acc.lastMsgReceived = time.Time{}
		acc.connFailures = MaxConnectionFailures
		acc.mu.Unlock()
		_ = acc.handleMaxFailures(context.Background())
	}
	call()
	call()
	if len(h.alerts) != 2 || h.alerts[0] != AlertLevelWarn || h.alerts[1] != AlertLevelCritical {
		t.Fatalf("两次 handleMaxFailures 应只发一次掉线 warn 和一次恢复失败 critical，got %+v", h.alerts)
	}
}

// TestTryLoginStatusCheckUsesRuntimeDataWithoutLoginSecrets 验证登录态检查不读取损坏的登录密码字段。
func TestTryLoginStatusCheckUsesRuntimeDataWithoutLoginSecrets(t *testing.T) {
	// mtopClient 返回登录态正常结果，测试只关注凭证查询边界。
	mtopClient := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-runtime-data"},
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusSuccess, Message: "登录状态正常"},
	}
	t.Setenv("XIANYU_DATA_KEY", "engine-runtime-query-key")
	// acc 是使用窄凭证读取路径执行登录态检查的测试账号；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// ctx 是测试登录态检查共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-user", "not-a-password-ciphertext", "cid"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// result 是登录态检查在窄凭证查询后返回的状态。
	result := acc.tryLoginStatusCheck(ctx)
	if result.recovered || result.riskRequired {
		t.Fatalf("正常登录态不应触发恢复或风控: %+v", result)
	}
}
