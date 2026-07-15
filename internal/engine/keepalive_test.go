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

func (c *countingMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	atomic.AddInt32(&c.calls, 1)
	return &mtop.RefreshResult{AccessToken: c.token, AccessTokenExpireAt: time.Now().Add(time.Hour).Unix()}, nil
}

type statusMtop struct {
	fakeRunMtop
	result *mtop.LoginStatusResult
	err    error
}

type failingCountingMtop struct {
	fakeRunMtop
	calls int32
	err   error
}

func (c *failingCountingMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	atomic.AddInt32(&c.calls, 1)
	return nil, c.err
}

func saveBoundToken(t *testing.T, store *db.Store, acc *Account, token string, expireAt int64) {
	t.Helper()
	if err := store.Tokens.SaveBound(context.Background(), acc.CookieID, acc.deviceID, token, expireAt,
		credentialCookieFingerprint(acc.currentCookieStr())); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshTokenNetworkFailureClearsMemoryButPreservesFreshCache(t *testing.T) {
	mtopClient := &failingCountingMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		err:         errors.New("network connection reset"),
	}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "cached-tok", time.Now().Add(time.Hour).Unix())
	acc.mu.Lock()
	acc.currentToken = "old-memory-token"
	acc.mu.Unlock()
	if _, _, err := acc.refreshToken(context.Background()); err == nil {
		t.Fatal("网络失败应返回错误")
	}
	acc.mu.Lock()
	current := acc.currentToken
	acc.mu.Unlock()
	if current != "" {
		t.Fatalf("刷新网络失败后应清空内存 token: %q", current)
	}
	if cached, err := store.Tokens.Get(context.Background(), "cid"); err != nil || cached.AccessToken != "cached-tok" {
		t.Fatalf("网络失败应保留未过期数据库缓存: cached=%+v err=%v", cached, err)
	}
}

func TestRefreshTokenBusinessFailureClearsMemoryAndCache(t *testing.T) {
	mtopClient := &failingCountingMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		err:         errors.New("token API business rejected"),
	}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "cached-tok", time.Now().Add(time.Hour).Unix())
	acc.mu.Lock()
	acc.currentToken = "old-memory-token"
	acc.mu.Unlock()
	if _, _, err := acc.refreshToken(context.Background()); err == nil {
		t.Fatal("业务失败应返回错误")
	}
	acc.mu.Lock()
	current := acc.currentToken
	acc.mu.Unlock()
	if current != "" {
		t.Fatalf("业务失败后应清空内存 token: %q", current)
	}
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != acc.deviceID {
		t.Fatalf("业务失败应清 token 并保留 device ID: tk=%+v err=%v", tk, err)
	}
}

func TestAcquireTokenDeletesExpiredCacheBeforeNetworkAttempt(t *testing.T) {
	mtopClient := &failingCountingMtop{
		fakeRunMtop: fakeRunMtop{token: "unused"},
		err:         errors.New("network connection reset"),
	}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "expired-tok", time.Now().Add(-time.Minute).Unix())
	if _, _, err := acc.acquireToken(context.Background()); err == nil {
		t.Fatal("后续网络请求失败应返回错误")
	}
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != acc.deviceID {
		t.Fatalf("过期 token 应清空且 device ID 保留: tk=%+v err=%v", tk, err)
	}
}

func (s *statusMtop) CheckLoginStatusContext(context.Context, string) (*mtop.LoginStatusResult, error) {
	return s.result, s.err
}

// TestAcquireToken_CacheHitSkipsMtop 缓存命中且未过期时跳过 mtop 调用。
func TestAcquireToken_CacheHitSkipsMtop(t *testing.T) {
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	saveBoundToken(t, store, acc, "cached-tok", time.Now().Add(time.Hour).Unix())
	token, cookies, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatalf("acquireToken: %v", err)
	}
	if token != "cached-tok" {
		t.Fatalf("应使用缓存 token，got %s", token)
	}
	if calls := atomic.LoadInt32(&mtopClient.calls); calls != 0 {
		t.Fatalf("缓存命中不应调用 mtop，got %d", calls)
	}
	if !strings.Contains(cookies, "_m_h5_tk=tk_1") {
		t.Fatalf("缓存命中应返回其绑定的 Cookie: %q", cookies)
	}
}

func TestAcquireToken_RejectsCacheBoundToDifferentCookie(t *testing.T) {
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "fresh-token"}}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	saveBoundToken(t, store, acc, "stale-token", time.Now().Add(time.Hour).Unix())
	newCookie := "unb=123; _m_h5_tk=renewed_2; cookie2=new"
	if err := store.Cookies.UpdateValueExisting(context.Background(), "cid", newCookie); err != nil {
		t.Fatal(err)
	}
	acc.replaceCookieStr(newCookie)

	token, _, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh-token" || atomic.LoadInt32(&mtopClient.calls) != 1 {
		t.Fatalf("token=%q calls=%d want fresh token", token, atomic.LoadInt32(&mtopClient.calls))
	}
}

func TestAcquireRuntimeTokenPreservesMemoryTokenWithoutDatabaseCache(t *testing.T) {
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	acc.currentToken = "runtime-token"
	_ = store.Tokens.Clear(context.Background(), "cid")

	token, _, err := acc.acquireRuntimeToken(context.Background())
	if err != nil || token != "runtime-token" {
		t.Fatalf("runtime token=%q err=%v", token, err)
	}
	if calls := atomic.LoadInt32(&mtopClient.calls); calls != 0 {
		t.Fatalf("网络重连复用内存 token 时不应调用 mtop，calls=%d", calls)
	}
}

// TestAcquireToken_CacheMissCallsMtopAndSavesCache 缓存未命中时调 mtop 并把结果落库。
func TestAcquireToken_CacheMissCallsMtopAndSavesCache(t *testing.T) {
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	token, _, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatalf("acquireToken: %v", err)
	}
	if token != "tok-mtop" {
		t.Fatalf("应使用 mtop token，got %s", token)
	}
	if calls := atomic.LoadInt32(&mtopClient.calls); calls != 1 {
		t.Fatalf("缓存未命中应调一次 mtop，got %d", calls)
	}
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
	newCookie := "unb=123; _m_h5_tk=loginuser_new; sgcookie=abc"
	mtopClient := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result: &mtop.LoginStatusResult{
			Status:         mtop.LoginStatusTokenRefreshed,
			UpdatedCookies: newCookie,
			Message:        "令牌已刷新",
		},
	}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	saveBoundToken(t, store, acc, "old-tok", time.Now().Add(time.Hour).Unix())
	res := acc.tryLoginStatusCheck(context.Background())
	if !res.recovered || res.riskRequired {
		t.Fatalf("登录态检查应恢复 Cookie，got %+v", res)
	}
	if got := acc.currentCookieStr(); got != newCookie {
		t.Fatalf("内存 Cookie 未更新: got %q want %q", got, newCookie)
	}
	saved, err := store.Cookies.GetValue(context.Background(), "cid")
	if err != nil || saved != newCookie {
		t.Fatalf("DB Cookie 未更新: got %q err=%v", saved, err)
	}
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != acc.deviceID {
		t.Fatalf("Cookie 更新后应清 token 并保留 device ID: tk=%+v err=%v", tk, err)
	}
}

// TestTryLoginStatusCheck_RiskRequired loginuser.get 命中风控时应进入验证态，不继续普通续期。
func TestTryLoginStatusCheck_RiskRequired(t *testing.T) {
	mtopClient := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result: &mtop.LoginStatusResult{
			Status:          mtop.LoginStatusRiskRequired,
			Ret:             []string{"FAIL_SYS_USER_VALIDATE::RGV587"},
			VerificationURL: "https://passport.goofish.com/punish?x5secdata=1",
			Message:         "闲鱼要求安全验证",
		},
	}
	acc, _, _, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	res := acc.tryLoginStatusCheck(context.Background())
	if !res.riskRequired || res.recovered {
		t.Fatalf("登录态检查应返回风控状态，got %+v", res)
	}
	if res.verificationURL == "" {
		t.Fatal("应保留风控验证 URL")
	}
	if got := acc.RuntimeStatus().State; got != RuntimeVerificationRequired {
		t.Fatalf("风控时状态应为 verification_required，got %q", got)
	}
}

// TestReloadCookieFromDB_AdoptsNewCookieAndClearsCache DB cookie 变更时采纳新 cookie 并清旧 token 缓存。
func TestReloadCookieFromDB_AdoptsNewCookieAndClearsCache(t *testing.T) {
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	// 预置旧 session 的 token 缓存。
	saveBoundToken(t, store, acc, "old-tok", time.Now().Add(time.Hour).Unix())
	// 外部更新 DB cookie（模拟重新扫码），userID=0 复用现有 user_id。
	newCookie := "unb=123; _m_h5_tk=newtk_2; sgcookie=abc"
	if err := store.Cookies.Save(context.Background(), "cid", newCookie, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	acc.reloadCookieFromDB(context.Background())
	if got := acc.CurrentCookieStr(); got != newCookie {
		t.Fatalf("应采纳 DB 新 cookie，got %s", got)
	}
	// 新 cookie 对应新 session，旧 token 应清除但 device ID 必须保留。
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "" || tk.DeviceID != acc.deviceID {
		t.Fatalf("cookie 变更后应清 token 并保留 device ID: tk=%+v err=%v", tk, err)
	}
}

// TestHandleMaxFailures_AlertOnce 连续两次进入 false 终止路径只告警一次。
func TestHandleMaxFailures_AlertOnce(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	h := &failingRefreshHandler{}
	acc.handler = h

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
