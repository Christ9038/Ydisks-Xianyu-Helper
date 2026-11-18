package account

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fixedClock 是可手动推进的测试时钟，避免依赖真实等待。
type fixedClock struct {
	// mu 保护 current，因为并发测试会同时读取时钟。
	mu sync.Mutex
	// current 是当前模拟时间。
	current time.Time
}

// Now 返回当前模拟时间。
func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Advance 把模拟时间向前推进 d。
func (c *fixedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// newClockedRegistry 创建使用模拟时钟的登记表，返回登记表与时钟。
func newClockedRegistry() (*PendingVerificationRegistry, *fixedClock) {
	// clock 从固定时刻开始，保证用例可重复。
	clock := &fixedClock{current: time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)}
	// registry 保存 NewPendingVerificationRegistry 的结果，供后续分支和断言使用。
	registry := NewPendingVerificationRegistry()
	registry.now = clock.Now
	return registry, clock
}

// TestPendingVerificationRegistryRegisterDeduplicates 验证登记表区分“新的验证要求”和“仍在有效期内的重复要求”；t 管理断言。
func TestPendingVerificationRegistryRegisterDeduplicates(t *testing.T) {
	// clock 保存 newClockedRegistry 的结果，供后续分支和断言使用；registry 保存 newClockedRegistry 的结果，供后续分支和断言使用。
	registry, clock := newClockedRegistry()
	if !registry.Register("a1", "https://x.invalid/iv/1") {
		t.Fatal("首次登记应视为新的验证要求")
	}
	if registry.Register("a1", "https://x.invalid/iv/2") {
		t.Fatal("有效期内重复登记不应再次视为新要求")
	}
	if !registry.Pending("a1") || registry.Pending("other") {
		t.Fatal("Pending 应只对已登记账号为真")
	}
	clock.Advance(PendingVerificationTTL + time.Second)
	if registry.Pending("a1") {
		t.Fatal("过期登记不应再视为待验证")
	}
	if !registry.Register("a1", "https://x.invalid/iv/3") {
		t.Fatal("过期后再次登记应重新视为新要求")
	}
	if registry.Register("  ", "u") || registry.Register("", "u") {
		t.Fatal("空账号标识不得登记")
	}
	// nilRegistry 是未初始化的登记表，所有方法都应安全返回。
	var nilRegistry *PendingVerificationRegistry
	if nilRegistry.Register("a", "u") || nilRegistry.Pending("a") {
		t.Fatal("空登记表应安全返回 false")
	}
	// ok 标记 nilRegistry.take 取得的条目是否存在或调用是否成功。
	if _, ok := nilRegistry.take("a"); ok {
		t.Fatal("空登记表不应取出任何登记")
	}
	nilRegistry.restore("a", pendingVerification{})
}

// TestPendingVerificationRegistryTakeIsOneShot 验证验证地址只能被取走一次，无地址登记不可取且保留；t 管理断言。
func TestPendingVerificationRegistryTakeIsOneShot(t *testing.T) {
	// registry 保存 newClockedRegistry 的结果，供后续分支和断言使用。
	registry, _ := newClockedRegistry()
	registry.Register("a1", " https://x.invalid/iv/1 ")
	// entry 保存 registry.take 的结果，供后续分支和断言使用；ok 标记 registry.take 取得的条目是否存在或调用是否成功。
	entry, ok := registry.take("a1")
	if !ok || entry.url != "https://x.invalid/iv/1" {
		t.Fatalf("应取出去除空白后的地址: %+v ok=%v", entry, ok)
	}
	// again 标记 registry.take 取得的条目是否存在或调用是否成功。
	if _, again := registry.take("a1"); again {
		t.Fatal("同一登记不得被第二次取走")
	}
	// 没有地址的登记只用于通知去重，不可取，也不应因取用尝试而消失。
	registry.Register("a2", "")
	// ok 标记 registry.take 取得的条目是否存在或调用是否成功。
	if _, ok := registry.take("a2"); ok {
		t.Fatal("无地址登记不应可取")
	}
	if !registry.Pending("a2") {
		t.Fatal("无地址登记应保持待验证")
	}
}

// TestPendingVerificationRegistryRestoreKeepsOriginalTimeAndNewerEntry 验证放回保持原登记时间且不覆盖更新的登记；t 管理断言。
func TestPendingVerificationRegistryRestoreKeepsOriginalTimeAndNewerEntry(t *testing.T) {
	// clock 保存 newClockedRegistry 的结果，供后续分支和断言使用；registry 保存 newClockedRegistry 的结果，供后续分支和断言使用。
	registry, clock := newClockedRegistry()
	registry.Register("a1", "https://x.invalid/iv/1")
	// entry 保存 registry.take 的结果，供后续分支和断言使用。
	entry, _ := registry.take("a1")
	clock.Advance(PendingVerificationTTL - time.Minute)
	registry.restore("a1", entry)
	if !registry.Pending("a1") {
		t.Fatal("放回后应仍在有效期内")
	}
	clock.Advance(2 * time.Minute)
	if registry.Pending("a1") {
		t.Fatal("放回不得延长原有 TTL")
	}
	// 期间出现更新登记时，放回不得覆盖。
	registry.Register("a2", "https://x.invalid/iv/new")
	registry.restore("a2", pendingVerification{url: "https://x.invalid/iv/old", registeredAt: clock.Now()})
	// got 保存 registry.take 的结果，供后续分支和断言使用。
	if got, _ := registry.take("a2"); got.url != "https://x.invalid/iv/new" {
		t.Fatalf("不应覆盖更新的登记: %+v", got)
	}
}

// TestPendingVerificationRegistryEvictsOldestAtCapacity 验证超过上限时淘汰最早登记的条目；t 管理断言。
func TestPendingVerificationRegistryEvictsOldestAtCapacity(t *testing.T) {
	// clock 保存 newClockedRegistry 的结果，供后续分支和断言使用；registry 保存 newClockedRegistry 的结果，供后续分支和断言使用。
	registry, clock := newClockedRegistry()
	// i 是依次登记的账号序号；每次推进一秒使登记时间严格递增。
	for i := 0; i < maxPendingVerifications+5; i++ {
		registry.Register("acc-"+strconv.Itoa(i), "https://x.invalid/iv")
		clock.Advance(time.Second)
	}
	if registry.Pending("acc-0") || registry.Pending("acc-4") {
		t.Fatal("最早登记的条目应被淘汰")
	}
	if !registry.Pending("acc-5") || !registry.Pending("acc-"+strconv.Itoa(maxPendingVerifications+4)) {
		t.Fatal("较新的条目应保留")
	}
	registry.mu.Lock()
	// size 保存 len 的结果，供后续分支和断言使用。
	size := len(registry.entries)
	registry.mu.Unlock()
	if size != maxPendingVerifications {
		t.Fatalf("登记表大小应被限制为上限: %d", size)
	}
}

// TestPendingVerificationRegistryConcurrent 在竞态检测下验证并发登记、查询与取走的安全性；t 管理断言。
func TestPendingVerificationRegistryConcurrent(t *testing.T) {
	// registry 保存 NewPendingVerificationRegistry 的结果，供后续分支和断言使用。
	registry := NewPendingVerificationRegistry()
	// wg 等待全部并发协程结束。
	var wg sync.WaitGroup
	// worker 是并发协程序号。
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			// accountID 让多个协程竞争同一批账号。
			accountID := "acc-" + strconv.Itoa(worker%3)
			// i 是并发压力循环的轮次序号。
			for i := 0; i < 200; i++ {
				registry.Register(accountID, "https://x.invalid/iv")
				registry.Pending(accountID)
				// entry 保存 registry.take 的结果，供后续分支和断言使用；ok 标记 registry.take 取得的条目是否存在或调用是否成功。
				if entry, ok := registry.take(accountID); ok && i%2 == 0 {
					registry.restore(accountID, entry)
				}
			}
		}(worker)
	}
	wg.Wait()
}

// credentialVerificationPortFake 是平台适配端口替身，记录调用并返回预设结果。
type credentialVerificationPortFake struct {
	// sessionID 是成功时返回的会话标识。
	sessionID string
	// err 是失败时返回的错误。
	err error
	// calls 记录平台调用次数。
	calls int
	// gotAccount、gotURL 记录最近一次调用参数。
	gotAccount, gotURL string
}

// StartCredentialVerification 记录参数并返回预设结果。
func (f *credentialVerificationPortFake) StartCredentialVerification(_ context.Context, accountID, verificationURL string) (string, error) {
	f.calls++
	f.gotAccount, f.gotURL = accountID, verificationURL
	return f.sessionID, f.err
}

// TestNewCredentialVerificationServiceValidatesDependencies 验证构造期校验全部必需依赖；t 管理断言。
func TestNewCredentialVerificationServiceValidatesDependencies(t *testing.T) {
	// repository、registry、port 是合法依赖，每个子用例只置空其中一项。
	repository := longLoginSummaryRepositoryFake{}
	// registry 保存 NewPendingVerificationRegistry 的结果，供后续分支和断言使用。
	registry := NewPendingVerificationRegistry()
	// port 保存 该表达式 的结果，供后续分支和断言使用。
	port := &credentialVerificationPortFake{}
	// err 是 NewCredentialVerificationService 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := NewCredentialVerificationService(nil, registry, port); err == nil {
		t.Fatal("缺少 repository 应报错")
	}
	// err 是 NewCredentialVerificationService 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := NewCredentialVerificationService(repository, nil, port); err == nil {
		t.Fatal("缺少 registry 应报错")
	}
	// err 是 NewCredentialVerificationService 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := NewCredentialVerificationService(repository, registry, nil); err == nil {
		t.Fatal("缺少 port 应报错")
	}
	// err 是 NewCredentialVerificationService 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := NewCredentialVerificationService(repository, registry, port); err != nil {
		t.Fatalf("完整依赖应构造成功: %v", err)
	}
	// nilService 是未初始化的服务，调用应返回错误而不是 panic。
	var nilService *CredentialVerificationService
	// err 是 nilService.Start 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := nilService.Start(context.Background(), 1, "a"); err == nil {
		t.Fatal("空服务 Start 应报错")
	}
	// got 保存 nilService.PendingAmong 的结果，供后续分支和断言使用。
	if got := nilService.PendingAmong([]string{"a"}); got == nil || len(got) != 0 {
		t.Fatalf("空服务 PendingAmong 应返回空集合: %v", got)
	}
}

// TestCredentialVerificationServiceStart 验证归属校验、一次性取走、失败放回与成功路径；t 管理断言。
func TestCredentialVerificationServiceStart(t *testing.T) {
	// registry 保存 NewPendingVerificationRegistry 的结果，供后续分支和断言使用。
	registry := NewPendingVerificationRegistry()
	// port 保存 该表达式 的结果，供后续分支和断言使用。
	port := &credentialVerificationPortFake{sessionID: "session-1"}
	// err 是 NewCredentialVerificationService 返回的失败原因，用于断言成功或被拒绝的路径；service 保存 NewCredentialVerificationService 的结果，供后续分支和断言使用。
	service, err := NewCredentialVerificationService(longLoginSummaryRepositoryFake{summary: Summary{ID: "a1"}}, registry, port)
	if err != nil {
		t.Fatal(err)
	}
	// 没有登记：回退到重新扫码，且不访问平台。
	if _, startErr := service.Start(context.Background(), 1, "a1"); !errors.Is(startErr, ErrVerificationNotPending) || port.calls != 0 {
		t.Fatalf("无登记应返回 ErrVerificationNotPending 且不访问平台: err=%v calls=%d", startErr, port.calls)
	}
	registry.Register("a1", "https://x.invalid/iv/1")
	// got 保存 service.PendingAmong 的结果，供后续分支和断言使用。
	if got := service.PendingAmong([]string{"a1", "other"}); len(got) != 1 || !got["a1"] {
		t.Fatalf("应只报告已登记账号为待验证: %v", got)
	}
	// 平台失败：登记被放回，用户可以重试。
	port.err = errors.New("平台暂时不可用")
	// startErr 是 service.Start 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, startErr := service.Start(context.Background(), 1, "a1"); startErr == nil || !registry.Pending("a1") {
		t.Fatalf("平台失败应透传错误并放回登记: err=%v pending=%v", startErr, registry.Pending("a1"))
	}
	// 成功：返回会话并消费登记；参数为账号与取走的地址。
	port.err = nil
	// sessionID 保存 service.Start 的结果，供后续分支和断言使用；startErr 是 service.Start 返回的失败原因，用于断言成功或被拒绝的路径。
	sessionID, startErr := service.Start(context.Background(), 1, "a1")
	if startErr != nil || sessionID != "session-1" || port.gotAccount != "a1" || port.gotURL != "https://x.invalid/iv/1" {
		t.Fatalf("session=%q err=%v account=%q url=%q", sessionID, startErr, port.gotAccount, port.gotURL)
	}
	if registry.Pending("a1") {
		t.Fatal("成功启动后登记应被消费")
	}
}

// TestCredentialVerificationServiceRejectsOtherUsersAccount 验证归属失败不会取走登记，也不会访问平台；t 管理断言。
func TestCredentialVerificationServiceRejectsOtherUsersAccount(t *testing.T) {
	// registry 保存 NewPendingVerificationRegistry 的结果，供后续分支和断言使用。
	registry := NewPendingVerificationRegistry()
	registry.Register("a1", "https://x.invalid/iv/1")
	// port 保存 该表达式 的结果，供后续分支和断言使用。
	port := &credentialVerificationPortFake{sessionID: "s"}
	// err 是 NewCredentialVerificationService 返回的失败原因，用于断言成功或被拒绝的路径；service 保存 NewCredentialVerificationService 的结果，供后续分支和断言使用。
	service, err := NewCredentialVerificationService(longLoginSummaryRepositoryFake{err: ErrForbidden}, registry, port)
	if err != nil {
		t.Fatal(err)
	}
	// startErr 是 service.Start 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, startErr := service.Start(context.Background(), 2, "a1"); !errors.Is(startErr, ErrForbidden) {
		t.Fatalf("越权应返回 ErrForbidden: %v", startErr)
	}
	if port.calls != 0 || !registry.Pending("a1") {
		t.Fatalf("越权不得访问平台或消费登记: calls=%d pending=%v", port.calls, registry.Pending("a1"))
	}
}
