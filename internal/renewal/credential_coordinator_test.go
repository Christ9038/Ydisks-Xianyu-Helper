package renewal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apirenew "xianyu-go/internal/xianyu/renew"
)

// TestAPICookieRenewReleasesCredentialLockDuringSlowIO 验证接口续期的慢速请求不占用共享凭证锁。
func TestAPICookieRenewReleasesCredentialLockDuringSlowIO(t *testing.T) {
	// store 是接口续期协调器测试使用的账号数据库。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 是待续期的账号运行视图。
	account := createSchedulerAccount(t, store, "cid-api-coordinator", "unb=1; havana_lgc_exp="+futureSchedulerMillis())
	// started 表示外部 API 请求已经开始执行。
	started := make(chan struct{})
	// release 允许测试结束外部 API 请求阻塞。
	release := make(chan struct{})
	// server 是阻塞接口续期请求的 HTTP 桩。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: "fresh", Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer server.Close()
	// scheduler 是待验证凭证快照协调行为的调度器。
	scheduler := NewScheduler(store, &schedulerFakeStarter{}, nil, nil)
	scheduler.api = apirenew.Service{
		HTTPClient: server.Client(), SilentHasLoginURL: server.URL, RetryDelay: -1,
		PromiseTimeout: time.Second,
	}
	// finished 表示续期调用已经返回。
	finished := make(chan struct{})
	go func() {
		scheduler.apiCookieRenewOne(context.Background(), "batch-api-coordinator", account)
		close(finished)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("接口续期请求未开始")
	}
	// lockReleased 表示其他调用方已经成功取得并释放账号凭证锁。
	lockReleased := make(chan struct{})
	go func() {
		// unlock 是探测调用方取得的账号凭证锁释放函数。
		unlock := store.LockAccountCredentials(account.ID)
		unlock()
		close(lockReleased)
	}()
	select {
	case <-lockReleased:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("慢速接口续期仍持有共享凭证锁")
	}
	// updateErr 表示并发流程写入新 Cookie 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(context.Background(), account.ID, "unb=1; concurrent=kept; havana_lgc_exp="+futureSchedulerMillis(), `{}`, time.Now().Unix()); updateErr != nil {
		t.Fatalf("写入并发 Cookie: %v", updateErr)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("接口续期调用未完成")
	}
	// saved 是并发更新与续期响应合并后的 Cookie 值。
	saved, savedErr := store.Cookies.GetValue(context.Background(), account.ID)
	if savedErr != nil {
		t.Fatalf("读取合并后的 Cookie: %v", savedErr)
	}
	if !strings.Contains(saved, "concurrent=kept") || !strings.Contains(saved, "sdkSilent=fresh") {
		t.Fatalf("并发 Cookie 被旧续期响应覆盖: %q", saved)
	}
}
