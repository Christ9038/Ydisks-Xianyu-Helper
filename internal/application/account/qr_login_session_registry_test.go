package account

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestQRLoginSessionRegistryAuthorizesAndExpires 验证扫码会话所有权校验和过期回收不暴露凭证状态。
func TestQRLoginSessionRegistryAuthorizesAndExpires(t *testing.T) {
	// now 固定测试时钟，使会话生命周期断言不依赖真实墙钟。
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	// registry 保存当前测试的扫码会话状态。
	registry := NewQRLoginSessionRegistry()
	registry.now = func() time.Time { return now }
	registry.Register("session-1", 7, now)
	// err 保存会话所有者访问校验结果。
	if err := registry.Authorize("session-1", 7); err != nil {
		t.Fatalf("会话所有者应能访问: %v", err)
	}
	if !errors.Is(registry.Authorize("session-1", 8), ErrQRLoginSessionForbidden) {
		t.Fatal("其他用户访问会话应被拒绝")
	}
	now = now.Add(31 * time.Minute)
	// err 保存过期会话访问校验结果。
	if !errors.Is(registry.Authorize("session-1", 7), ErrQRLoginSessionNotFound) {
		t.Fatal("过期会话应视为不存在")
	}
	// got 保存重复清理返回的平台会话标识列表。
	if got := registry.Cleanup(now); len(got) != 0 {
		t.Fatalf("过期会话已在授权检查中清理，不应重复返回: %v", got)
	}
}

// TestQRLoginSessionRegistryCleanupReturnsPlatformSessions 验证批量清理返回需要释放的平台会话标识。
func TestQRLoginSessionRegistryCleanupReturnsPlatformSessions(t *testing.T) {
	// now 固定测试时钟并构造一个已经超时的会话。
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	// registry 保存当前测试的扫码会话状态。
	registry := NewQRLoginSessionRegistry()
	registry.now = func() time.Time { return now }
	registry.Register("expired", 7, now.Add(-31*time.Minute))
	registry.Register("active", 7, now)
	// expired 保存注册表报告的过期平台会话标识。
	expired := registry.Cleanup(now)
	if len(expired) != 1 || expired[0] != "expired" {
		t.Fatalf("过期会话列表异常: %v", expired)
	}
	// err 保存活动会话访问校验结果。
	if err := registry.Authorize("active", 7); err != nil {
		t.Fatalf("活动会话不应被清理: %v", err)
	}
}

// TestQRLoginSessionRegistryPersistOnceIsIdempotent 验证同一扫码会话并发持久化只执行一次工作函数。
func TestQRLoginSessionRegistryPersistOnceIsIdempotent(t *testing.T) {
	// registry 保存当前测试的扫码会话状态。
	registry := NewQRLoginSessionRegistry()
	// workCalls 统计慢速持久化工作函数被实际执行的次数。
	workCalls := 0
	// workMu 保护测试计数器，避免并发断言产生数据竞争。
	var workMu sync.Mutex
	// work 模拟应用层凭证写入并只返回非敏感结果。
	work := func() (QRLoginSessionPersistence, error) {
		workMu.Lock()
		workCalls++
		workMu.Unlock()
		time.Sleep(time.Millisecond)
		return QRLoginSessionPersistence{AccountID: "account-1", IsNew: true}, nil
	}
	// results 保存两个并发调用得到的幂等结果。
	results := make([]QRLoginSessionPersistence, 2)
	// errs 保存两个并发调用的错误结果。
	errs := make([]error, 2)
	// waitGroup 等待两个并发持久化调用结束。
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	// index 表示当前并发持久化调用在测试结果数组中的位置。
	for index := range results {
		go func(index int) {
			defer waitGroup.Done()
			results[index], errs[index] = registry.PersistOnce("session-1", 7, work)
		}(index)
	}
	waitGroup.Wait()
	if workCalls != 1 {
		t.Fatalf("同一会话工作函数执行次数=%d，期望 1", workCalls)
	}
	// index、err 分别表示当前并发调用下标及其错误结果。
	for index, err := range errs {
		if err != nil {
			t.Fatalf("并发调用 %d 失败: %v", index, err)
		}
		if results[index].AccountID != "account-1" || !results[index].IsNew {
			t.Fatalf("并发调用 %d 返回结果异常: %+v", index, results[index])
		}
	}
}
