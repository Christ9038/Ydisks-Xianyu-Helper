package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

// TestManagerCloseContextWaitsForActiveOperation 验证关闭会等待已登记的浏览器调用，且关闭期间拒绝新调用。
func TestManagerCloseContextWaitsForActiveOperation(t *testing.T) {
	// manager 保存不启动 Chromium 的管理器，测试仅验证生命周期状态机。
	manager := newTestManager(1)
	// err 表示测试活动调用登记失败时的状态机错误。
	if err := manager.beginOperation(context.Background()); err != nil {
		t.Fatalf("登记活动调用失败: %v", err)
	}
	// closeDone 保存带超时关闭的结果，避免测试本身留下未观察的 goroutine。
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- manager.CloseContext(context.Background())
	}()
	select {
	// err 表示关闭 goroutine 在活动调用释放前意外完成的结果。
	case err := <-closeDone:
		t.Fatalf("活动调用未释放前不应完成关闭，结果=%v", err)
	case <-time.After(30 * time.Millisecond):
	}
	// err 表示关闭期间尝试创建新活动调用得到的拒绝原因。
	if err := manager.beginOperation(context.Background()); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("关闭期间应拒绝新调用，错误=%v", err)
	}
	manager.endOperation()
	select {
	// err 表示释放活动调用后 CloseContext 的最终结果。
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("活动调用释放后关闭失败: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("关闭等待活动调用超时")
	}
	// err 表示重复关闭的结果，已关闭管理器应返回 nil。
	if err := manager.CloseContext(context.Background()); err != nil {
		t.Fatalf("重复关闭应幂等: %v", err)
	}
}

// TestManagerCloseContextTimeoutIsRetryable 验证关闭超时会显式返回，并允许释放活动调用后重试。
func TestManagerCloseContextTimeoutIsRetryable(t *testing.T) {
	// manager 保存不启动 Chromium 的管理器，测试超时路径不依赖外部浏览器。
	manager := newTestManager(1)
	// err 表示测试活动调用登记失败时的状态机错误。
	if err := manager.beginOperation(context.Background()); err != nil {
		t.Fatalf("登记活动调用失败: %v", err)
	}
	// waitContext 保存短超时上下文，用于验证 CloseContext 不启动游离关闭任务。
	waitContext, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelWait()
	// err 表示等待活动调用超时后返回的明确 Context 错误。
	if err := manager.CloseContext(waitContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("关闭超时应返回 DeadlineExceeded，错误=%v", err)
	}
	// err 表示超时关闭后尝试创建新活动调用得到的拒绝原因。
	if err := manager.beginOperation(context.Background()); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("超时后管理器仍应拒绝新调用，错误=%v", err)
	}
	manager.endOperation()
	// err 表示释放活动调用后重试关闭的结果。
	if err := manager.CloseContext(context.Background()); err != nil {
		t.Fatalf("释放活动调用后重试关闭失败: %v", err)
	}
}

// TestManagerCloseContextWithoutOperations 验证没有活动调用时关闭立即完成且可重复执行。
func TestManagerCloseContextWithoutOperations(t *testing.T) {
	// manager 保存不启动 Chromium 的管理器，验证空池和 nil Playwright 的安全关闭。
	manager := newTestManager(1)
	// err 表示空管理器首次关闭的结果。
	if err := manager.CloseContext(context.Background()); err != nil {
		t.Fatalf("空管理器关闭失败: %v", err)
	}
	// err 表示已关闭管理器重复调用 Close 的结果。
	if err := manager.Close(); err != nil {
		t.Fatalf("Close 重复调用失败: %v", err)
	}
}

// TestChromiumLaunchArgs 验证启动参数含关键安全/反检测项。
func TestChromiumLaunchArgs(t *testing.T) {
	// args 保存args，供当前处理流程使用
	args := chromiumLaunchArgs()
	if len(args) == 0 {
		t.Fatal("应返回非空参数列表")
	}
	// want 保存want，供当前处理流程使用
	want := []string{"--no-sandbox", "--disable-dev-shm-usage", "--disable-blink-features=AutomationControlled", "--lang=zh-CN"}
	// w 表示当前遍历过程中的w
	for _, w := range want {
		// found 保存found，供当前处理流程使用
		found := false
		// a 表示当前遍历过程中的a
		for _, a := range args {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("缺少关键参数 %s", w)
		}
	}
}

// TestPackagedPlaywrightRuntimeReady 负责TestPackagedPlaywrightRuntimeReady相关处理。
func TestPackagedPlaywrightRuntimeReady(t *testing.T) {
	// runtimeRoot 保存runtimeRoot，供当前处理流程使用
	runtimeRoot := t.TempDir()
	// driverDir 保存driverDir，供当前处理流程使用
	driverDir := filepath.Join(runtimeRoot, "driver")
	// browserDir 保存浏览器Dir，供当前处理流程使用
	browserDir := filepath.Join(runtimeRoot, "browsers")
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(filepath.Join(driverDir, "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(filepath.Join(browserDir, "chromium-1228"), 0o755); err != nil {
		t.Fatal(err)
	}
	// nodeName 保存node名称，供当前处理流程使用
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
	}
	// path 表示当前遍历过程中的路径
	for _, path := range []string{
		filepath.Join(driverDir, nodeName),
		filepath.Join(driverDir, "package", "cli.js"),
	} {
		if // err 保存err，供当前处理流程使用
		err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", driverDir)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserDir)
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", "")
	if !packagedPlaywrightRuntimeReady() {
		t.Fatal("预置 Playwright runtime 应被识别")
	}
}

// TestPackagedPlaywrightRuntimeReadyWithExternalNode 负责TestPackagedPlaywrightRuntimeReadyWithExternalNode相关处理。
func TestPackagedPlaywrightRuntimeReadyWithExternalNode(t *testing.T) {
	// runtimeRoot 保存runtimeRoot，供当前处理流程使用
	runtimeRoot := t.TempDir()
	// driverDir 保存driverDir，供当前处理流程使用
	driverDir := filepath.Join(runtimeRoot, "driver")
	// browserDir 保存浏览器Dir，供当前处理流程使用
	browserDir := filepath.Join(runtimeRoot, "browsers")
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(filepath.Join(driverDir, "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(filepath.Join(browserDir, "chromium-1228"), 0o755); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := os.WriteFile(filepath.Join(driverDir, "package", "cli.js"), []byte("runtime"), 0o644); err != nil {
		t.Fatal(err)
	}
	// nodePath 保存node路径，供当前处理流程使用
	nodePath := filepath.Join(runtimeRoot, "node")
	if // err 保存err，供当前处理流程使用
	err := os.WriteFile(nodePath, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", driverDir)
	t.Setenv("PLAYWRIGHT_NODEJS_PATH", nodePath)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserDir)
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", "")
	if !packagedPlaywrightRuntimeReady() {
		t.Fatal("配置外部 Node.js 的预置 Playwright runtime 应被识别")
	}
}

// newTestManager 构造一个不触网的 Manager（pool 空，maxSize 小便于测试驱逐）。
func newTestManager(maxSize int) *Manager {
	return &Manager{
		logger:  nil,
		pool:    make(map[string]*poolEntry),
		maxSize: maxSize,
		idleTTL: 5 * time.Minute,
	}
}

// TestTouchUpdatesLastUsed touch 命中池中条目时更新 lastUsed。
func TestTouchUpdatesLastUsed(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(3)
	// old 保存old，供当前处理流程使用
	old := time.Now().Add(-time.Hour)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: old}
	m.touch("c1")
	if m.pool["c1"].lastUsed.Equal(old) {
		t.Fatal("touch 应更新 lastUsed")
	}
	// touch 不存在的条目应 no-op 不 panic。
	m.touch("no-such")
}

// TestEvictRemovesEntry evict 删除指定条目（nil browser 时 closeEntry 为 no-op）。
func TestEvictRemovesEntry(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(3)
	m.pool["c1"] = &poolEntry{cookieID: "c1"}
	m.pool["c2"] = &poolEntry{cookieID: "c2"}
	m.evict("c1")
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["c1"]; ok {
		t.Fatal("evict 应删除 c1")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["c2"]; !ok {
		t.Fatal("c2 应保留")
	}
	// evict 不存在的条目不 panic。
	m.evict("no-such")
}

// TestEvictIfNeededEvictsOldest 池满时驱逐最久未用的条目。
func TestEvictIfNeededEvictsOldest(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(2)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: time.Now().Add(-2 * time.Hour)}
	m.pool["c2"] = &poolEntry{cookieID: "c2", lastUsed: time.Now()}
	m.evictIfNeeded() // 池满（2 == maxSize），应驱逐 c1（最旧）
	if _, ok := m.pool["c1"]; ok {
		t.Fatal("应驱逐最旧的 c1")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["c2"]; !ok {
		t.Fatal("c2 应保留")
	}
}

// TestEvictIfNeededNoopWhenUnderLimit 池未满时不驱逐。
func TestEvictIfNeededNoopWhenUnderLimit(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(5)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: time.Now()}
	m.evictIfNeeded()
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["c1"]; !ok {
		t.Fatal("未满不应驱逐")
	}
}

// TestEvictIfNeededSkipsActiveEntries 负责TestEvictIfNeededSkipsActiveEntries相关处理。
func TestEvictIfNeededSkipsActiveEntries(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(2)
	m.pool["active-old"] = &poolEntry{cookieID: "active-old", lastUsed: time.Now().Add(-2 * time.Hour), active: 1}
	m.pool["idle-new"] = &poolEntry{cookieID: "idle-new", lastUsed: time.Now()}
	m.evictIfNeeded()
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["active-old"]; !ok {
		t.Fatal("正在执行 token 请求的条目不得被淘汰")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["idle-new"]; ok {
		t.Fatal("池满时应优先淘汰空闲条目")
	}
}

// TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive 负责TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive相关处理。
func TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(2)
	m.pool["active-1"] = &poolEntry{cookieID: "active-1", lastUsed: time.Now().Add(-2 * time.Hour), active: 1}
	m.pool["active-2"] = &poolEntry{cookieID: "active-2", lastUsed: time.Now().Add(-time.Hour), active: 1}
	m.evictIfNeeded()
	if len(m.pool) != 2 {
		t.Fatalf("所有条目活跃时不得强制淘汰，pool=%d", len(m.pool))
	}
}

// TestCleanupIdleSkipsActiveEntries 负责TestCleanupIdleSkipsActiveEntries相关处理。
func TestCleanupIdleSkipsActiveEntries(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := newTestManager(3)
	m.idleTTL = time.Minute
	// old 保存old，供当前处理流程使用
	old := time.Now().Add(-time.Hour)
	m.pool["active"] = &poolEntry{cookieID: "active", lastUsed: old, active: 1}
	m.pool["idle"] = &poolEntry{cookieID: "idle", lastUsed: old}
	m.CleanupIdle()
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["active"]; !ok {
		t.Fatal("CleanupIdle 不得关闭仍有租约的条目")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := m.pool["idle"]; ok {
		t.Fatal("CleanupIdle 应清理过期空闲条目")
	}
}

// TestMarshalCookies 导出包装器等价 cookieMarshal。
func TestMarshalCookies(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := MarshalCookies(map[string]string{"unb": "1", "cna": "xx"})
	// map 顺序不保证，逐项检查。
	if !contains(got, "unb=1") || !contains(got, "cna=xx") {
		t.Fatalf("MarshalCookies=%q", got)
	}
}

// TestCookiesToMap playwright.Cookie 切片转 map。
func TestCookiesToMap(t *testing.T) {
	// cs 保存cs，供当前处理流程使用
	cs := []playwright.Cookie{
		{Name: "unb", Value: "123"},
		{Name: "_m_h5_tk", Value: "tok"},
	}
	// m 保存m，供当前处理流程使用
	m := cookiesToMap(cs)
	if m["unb"] != "123" || m["_m_h5_tk"] != "tok" || len(m) != 2 {
		t.Fatalf("cookiesToMap=%+v", m)
	}
	// 空切片。
	if m := cookiesToMap(nil); len(m) != 0 {
		t.Fatalf("空切片应返回空 map，got %+v", m)
	}
}

// contains 负责contains相关处理。
func contains(s, sub string) bool {
	for // i 保存i，供当前处理流程使用
	i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
