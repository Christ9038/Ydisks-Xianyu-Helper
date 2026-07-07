package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/mtop"
)

// countingMtop 包装 fakeRunMtop，计数 RefreshToken 调用次数。
type countingMtop struct {
	fakeRunMtop
	calls int32
}

func (c *countingMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	atomic.AddInt32(&c.calls, 1)
	return &mtop.RefreshResult{AccessToken: c.token}, nil
}

// TestAcquireToken_CacheHitSkipsMtop 缓存命中且未过期时跳过 mtop 调用。
func TestAcquireToken_CacheHitSkipsMtop(t *testing.T) {
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-mtop"}}
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	if err := store.Tokens.Save(context.Background(), "cid", acc.deviceID, "cached-tok",
		time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	token, _, err := acc.acquireToken(context.Background())
	if err != nil {
		t.Fatalf("acquireToken: %v", err)
	}
	if token != "cached-tok" {
		t.Fatalf("应使用缓存 token，got %s", token)
	}
	if calls := atomic.LoadInt32(&mtopClient.calls); calls != 0 {
		t.Fatalf("缓存命中不应调用 mtop，got %d", calls)
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

// TestReloadCookieFromDB_AdoptsNewCookieAndClearsCache DB cookie 变更时采纳新 cookie 并清旧 token 缓存。
func TestReloadCookieFromDB_AdoptsNewCookieAndClearsCache(t *testing.T) {
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	// 预置旧 session 的 token 缓存。
	if err := store.Tokens.Save(context.Background(), "cid", acc.deviceID, "old-tok",
		time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// 外部更新 DB cookie（模拟重新扫码），userID=0 复用现有 user_id。
	newCookie := "unb=123; _m_h5_tk=newtk_2; sgcookie=abc"
	if err := store.Cookies.Save(context.Background(), "cid", newCookie, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	acc.reloadCookieFromDB(context.Background())
	if acc.CookieStr != newCookie {
		t.Fatalf("应采纳 DB 新 cookie，got %s", acc.CookieStr)
	}
	// 新 cookie 对应新 session，旧 token 缓存应被清除。
	if _, err := store.Tokens.Get(context.Background(), "cid"); err == nil {
		t.Fatal("cookie 变更后应清除 token 缓存")
	}
}

// TestClearCacheIfShortConnection 短连接清缓存、长连接与零值不清。
func TestClearCacheIfShortConnection(t *testing.T) {
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()

	// 短连接（1s 前 startedAt）→ 清缓存。
	store.Tokens.Save(context.Background(), "cid", acc.deviceID, "cached",
		time.Now().Add(time.Hour).Unix())
	acc.clearCacheIfShortConnection(context.Background(), time.Now().Add(-1*time.Second))
	if _, err := store.Tokens.Get(context.Background(), "cid"); err == nil {
		t.Fatal("短连接应清除 token 缓存")
	}

	// 长连接（2min 前）→ 不清缓存。
	store.Tokens.Save(context.Background(), "cid", acc.deviceID, "cached2",
		time.Now().Add(time.Hour).Unix())
	acc.clearCacheIfShortConnection(context.Background(), time.Now().Add(-2*time.Minute))
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "cached2" {
		t.Fatalf("长连接不应清缓存，got tk=%+v err=%v", tk, err)
	}

	// 零值 startedAt → 不清缓存。
	acc.clearCacheIfShortConnection(context.Background(), time.Time{})
	if tk, err := store.Tokens.Get(context.Background(), "cid"); err != nil || tk.AccessToken != "cached2" {
		t.Fatalf("零值 startedAt 不应清缓存，got tk=%+v err=%v", tk, err)
	}
}

// TestHandleMaxFailures_AlertOnce 连续两次进入 false 慢重试路径只告警一次。
func TestHandleMaxFailures_AlertOnce(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	h := &failingRefreshHandler{}
	acc.handler = h

	// 两次都进入 false 路径：每次复位密码登录冷却，让第二次也走到 markAuthExpired。
	call := func() {
		acc.mu.Lock()
		acc.lastMsgReceived = time.Time{}
		acc.lastPasswordLogin = time.Time{}
		acc.connFailures = MaxConnectionFailures
		acc.mu.Unlock()
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_ = acc.handleMaxFailures(cctx)
	}
	call()
	call()
	if len(h.alerts) != 1 || h.alerts[0] != AlertLevelCritical {
		t.Fatalf("两次 handleMaxFailures 应只告警一次，got %+v", h.alerts)
	}
}
