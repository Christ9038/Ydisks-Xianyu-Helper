package renewal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apirenew "xianyu-go/internal/xianyu/renew"
)

// TestPendingAPIRenewUsesPlatformRuntimeData 验证迟到接口续期响应不解密登录密码。
func TestPendingAPIRenewUsesPlatformRuntimeData(t *testing.T) {
	// store 是本测试使用的续期数据库；cleanup 负责关闭数据库连接。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// ctx 是迟到续期测试共用的上下文。
	ctx := context.Background()
	// account 是接口续期批次使用的最小续期账号视图。
	account := createSchedulerAccount(t, store, "cid-platform-runtime", "unb=1; havana_lgc_exp="+futureSchedulerMillis())
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"renewal-user", "not-a-password-ciphertext", account.ID); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// server 是返回待处理响应并最终下发 Cookie 的续期服务桩。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "platformLate", Value: "saved", Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer server.Close()
	// starter 记录迟到 Cookie 保存后的账号重启次数。
	starter := &schedulerFakeStarter{}
	// scheduler 是待验证迟到续期流程的调度器。
	scheduler := NewScheduler(store, starter, nil, nil)
	scheduler.api = apirenew.Service{
		HTTPClient: server.Client(), SilentHasLoginURL: server.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond,
	}
	scheduler.apiCookieRenewOne(ctx, "batch-platform-runtime", account)
	// deadline 是等待异步迟到响应完成的最晚时间。
	deadline := time.Now().Add(time.Second)
	for starter.restarts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// got 是迟到 Cookie 合并完成后的账号重启次数。
	if got := starter.restarts.Load(); got != 1 {
		t.Fatalf("损坏登录密码不应阻断迟到 Cookie 合并和重启，restarts=%d", got)
	}
}
