package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/mtop"
)

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
