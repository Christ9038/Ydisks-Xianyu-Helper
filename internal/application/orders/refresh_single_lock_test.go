package orders

import (
	"context"
	"errors"
	"testing"
)

// singleLockRuntime 在运行时 Cookie 同步时检测账号凭证锁是否仍被调用方持有。
type singleLockRuntime struct {
	// refreshRuntimeFake 提供详情、会话提交等预置结果，测试只在同一调用栈访问。
	*refreshRuntimeFake
	// repository 提供与服务共用的真实互斥凭证锁。
	repository *chatLockRepository
	// heldCalls 记录同步 Cookie 时凭证锁仍被持有的次数；真实 engine 此时会重新取锁而自死锁。
	heldCalls int
	// syncedValues 记录每次同步到运行时的 Cookie 值。
	syncedValues []string
}

// UpdateRunningCookie 模拟 engine 重新获取同账号不可重入凭证锁：取锁失败即表示调用方仍持锁。
func (r *singleLockRuntime) UpdateRunningCookie(_ context.Context, _, value string) {
	if !r.repository.mu.TryLock() {
		r.heldCalls++
		return
	}
	r.repository.mu.Unlock()
	r.syncedValues = append(r.syncedValues, value)
}

// TestRefreshSingleSyncsRunningCookieOutsideCredentialLock 验证单订单刷新只在释放凭证锁后同步运行时 Cookie。
func TestRefreshSingleSyncsRunningCookieOutsideCredentialLock(t *testing.T) {
	// scenario 描述响应 Cookie 的提交方式、第二次读取到的凭证以及期望的同步结果。
	for _, scenario := range []struct {
		// name 是子用例名称。
		name string
		// detail 是远端详情响应，扁平 Cookie 场景通过 UpdatedCookies 携带新值。
		detail *RefreshDetail
		// persistHandled 表示运行时是否接管完整 Cookie Jar。
		persistHandled bool
		// afterFetch 是外部请求结束后重新读取到的凭证。
		afterFetch *PlatformRuntimeData
		// wantSynced 是期望同步给运行时的 Cookie，空表示不得同步。
		wantSynced string
		// wantChanged 表示期望返回凭证已变化错误。
		wantChanged bool
	}{
		{name: "jar", detail: &RefreshDetail{OrderStatus: "2"}, persistHandled: true, afterFetch: &PlatformRuntimeData{UserID: 7, Value: "old-cookie"}, wantSynced: "new-cookie"},
		{name: "flat", detail: &RefreshDetail{OrderStatus: "2", UpdatedCookies: "new-cookie"}, afterFetch: &PlatformRuntimeData{UserID: 7, Value: "old-cookie"}, wantSynced: "new-cookie"},
		{name: "credential_changed", detail: &RefreshDetail{OrderStatus: "2"}, persistHandled: true, afterFetch: &PlatformRuntimeData{UserID: 7, Value: "rotated-elsewhere"}, wantChanged: true},
	} {
		// t 管理当前同步场景的断言。
		t.Run(scenario.name, func(t *testing.T) {
			// repository 以真实互斥保护凭证，首次读取为旧 Cookie，外部请求后读取 afterFetch。
			repository := &chatLockRepository{refreshRepositoryFake: &refreshRepositoryFake{
				owned:       map[string]bool{"cookie-1": true},
				order:       &Order{OrderID: "order-1", CookieID: "cookie-1", OrderStatus: "processing"},
				loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "old-cookie"}, scenario.afterFetch},
			}}
			// runtime 记录同步时的锁状态，persist 结果模拟响应携带轮换 Cookie。
			runtime := &singleLockRuntime{repository: repository, refreshRuntimeFake: &refreshRuntimeFake{
				detailAvailable:   true,
				detailResult:      RefreshDetailFetchResult{Detail: scenario.detail},
				persistConfigured: true,
				persistHandled:    scenario.persistHandled,
				persistChanged:    scenario.persistHandled,
				persistValue:      "new-cookie",
			}}
			// result、err 保存单订单刷新结果；锁泄漏时本调用不会返回，故先以 heldCalls 间接判定。
			result, err := NewRefreshService(repository, runtime, 1).RefreshSingle(context.Background(), 7, "order-1")
			if runtime.heldCalls != 0 {
				t.Fatalf("持有凭证锁时同步运行时 Cookie: %d 次", runtime.heldCalls)
			}
			if scenario.wantChanged {
				if !errors.Is(err, ErrRefreshCredentialChanged) || len(runtime.syncedValues) != 0 {
					t.Fatalf("凭证变化不得同步运行时 Cookie: err=%v synced=%v", err, runtime.syncedValues)
				}
			} else if err != nil || !result.Success || len(runtime.syncedValues) != 1 || runtime.syncedValues[0] != scenario.wantSynced {
				t.Fatalf("运行时 Cookie 同步异常: result=%+v err=%v synced=%v", result, err, runtime.syncedValues)
			}
			if !repository.mu.TryLock() {
				t.Fatal("RefreshSingle 返回后凭证锁仍未释放")
			}
			repository.mu.Unlock()
		})
	}
}
