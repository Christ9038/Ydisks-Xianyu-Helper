package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
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

// TestInitializeContextRejectsCancelledContext 验证初始化在启动安装阶段前就会传播调用方取消。
func TestInitializeContextRejectsCancelledContext(t *testing.T) {
	// manager 保存尚未启动 Chromium 的浏览器管理器。
	manager := NewManager(nil)
	// initializeCtx、cancel 分别是已取消的初始化 Context 及其取消函数。
	initializeCtx, cancel := context.WithCancel(context.Background())
	cancel()
	// initErr 保存已取消初始化返回的错误；不得触发 Playwright 安装或启动。
	initErr := manager.InitializeContext(initializeCtx)
	if !errors.Is(initErr, context.Canceled) {
		t.Fatalf("已取消初始化应返回 Context 错误: %v", initErr)
	}
}

// TestInitializeContextCancelsAfterInstall 验证安装阶段结束时收到取消会阻止 Playwright 启动并允许后续关闭。
func TestInitializeContextCancelsAfterInstall(t *testing.T) {
	// manager 保存可注入安装回调的浏览器管理器。
	manager := NewManager(nil)
	// initializeCtx、cancel 分别控制安装阶段完成后的初始化取消。
	initializeCtx, cancel := context.WithCancel(context.Background())
	// installCalled 记录测试安装回调是否已执行。
	installCalled := false
	manager.installFn = func(context.Context) error {
		installCalled = true
		cancel()
		return nil
	}
	manager.runFn = func(context.Context) (*playwright.Playwright, error) {
		t.Fatal("安装后取消不应启动 Playwright")
		return nil, nil
	}
	// initErr 保存安装阶段取消传播的结果。
	initErr := manager.InitializeContext(initializeCtx)
	if !installCalled || !errors.Is(initErr, context.Canceled) {
		t.Fatalf("安装阶段取消未传播: called=%v err=%v", installCalled, initErr)
	}
	// closeErr 验证初始化取消后 CloseContext 仍可幂等完成。
	closeErr := manager.CloseContext(context.Background())
	if closeErr != nil {
		t.Fatalf("初始化取消后关闭失败: %v", closeErr)
	}
}

// TestInstallPlaywrightRuntimeCancelsInstaller 验证完整 runtime 安装由 Context 控制的独立进程执行，取消后不会继续后台下载。
func TestInstallPlaywrightRuntimeCancelsInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用不同的脚本启动语义，本机 Unix 受控子进程已覆盖")
	}
	// installer 保存模拟长期下载的独立安装器；exec 取代 shell 后只保留一个可终止 sleep 进程。
	installer := filepath.Join(t.TempDir(), "browser-install")
	// writeErr 保存写入模拟安装器脚本时的文件系统错误。
	if writeErr := os.WriteFile(installer, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); writeErr != nil {
		t.Fatalf("写入测试安装器失败: %v", writeErr)
	}
	t.Setenv("PLAYWRIGHT_BROWSER_INSTALLER", installer)
	// installCtx、cancel 为安装子进程提供很短的关闭预算。
	installCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	// installErr 保存安装器被 Context 终止后的错误；必须是取消而不是等待六十秒。
	installErr := installPlaywrightRuntime(installCtx, &playwright.RunOptions{DriverDirectory: t.TempDir()})
	if !errors.Is(installErr, context.DeadlineExceeded) {
		t.Fatalf("安装器取消错误=%v，want DeadlineExceeded", installErr)
	}
}

// TestBrowserInstallerCommandUsesControlledSourceFallback 验证源码开发环境未部署二进制安装器时，仍会选择可被 Context 终止的同一安装入口。
func TestBrowserInstallerCommandUsesControlledSourceFallback(t *testing.T) {
	// 无显式安装器时测试二进制同目录不存在 browser-install，解析器必须退回源码入口。
	t.Setenv("PLAYWRIGHT_BROWSER_INSTALLER", "")
	// driverDir 保存测试专用 Playwright driver 目录，用于验证参数会无损传入独立安装器。
	driverDir := t.TempDir()
	// command、args、resolveErr 分别保存解析后的 Go 工具链、安装器参数和解析错误。
	command, args, resolveErr := browserInstallerCommand(&playwright.RunOptions{DriverDirectory: driverDir})
	if resolveErr != nil {
		t.Fatalf("源码安装器解析失败: %v", resolveErr)
	}
	// expectedCommand 是当前测试工具链的绝对 Go 命令路径，源码 fallback 不应依赖调用方 PATH 的后续变化。
	expectedCommand, lookErr := exec.LookPath("go")
	if lookErr != nil {
		t.Fatalf("测试环境找不到 Go 工具链: %v", lookErr)
	}
	if command != expectedCommand {
		t.Fatalf("源码 fallback 命令=%q，want %q", command, expectedCommand)
	}
	// sourcePath 是从当前测试源码反推的模块安装器目录，必须和生产安装器共用同一 main 包。
	_, sourcePath, _, callerOK := runtime.Caller(0)
	if !callerOK {
		t.Fatal("无法读取测试源码路径")
	}
	// moduleRoot 是源码 fallback 传给 Go -C 的模块根，避免调用方切换目录后丢失 go.mod。
	moduleRoot := filepath.Join(filepath.Dir(sourcePath), "..", "..")
	// wantInstaller 是模块根下的唯一安装器源码目录。
	wantInstaller := filepath.Join(filepath.Dir(sourcePath), "..", "..", "cmd", "browser-install")
	if len(args) != 7 || args[0] != "-C" || args[1] != moduleRoot || args[2] != "run" || args[3] != wantInstaller || args[4] != "--" || args[5] != "-driver-dir" || args[6] != driverDir {
		t.Fatalf("源码 fallback 参数=%q，不符合受控安装器契约", args)
	}
}

// TestInitializeContextFingerprintFailureReleasesPartialState 验证指纹探测失败不会把尚未完整初始化的 Playwright 发布为可用实例。
func TestInitializeContextFingerprintFailureReleasesPartialState(t *testing.T) {
	// manager 使用确定性替身隔离安装和启动，测试只验证发布与关闭语义。
	manager := NewManager(nil)
	manager.installFn = func(context.Context) error { return nil }
	manager.runFn = func(context.Context) (*playwright.Playwright, error) { return nil, nil }
	// fingerprintErr 是指纹探测的确定性失败原因。
	fingerprintErr := errors.New("fingerprint failed")
	manager.fingerprintFn = func() error { return fingerprintErr }

	// initErr 保存探测失败的初始化错误，并确认 Manager 未发布半成品实例。
	initErr := manager.InitializeContext(context.Background())
	if !errors.Is(initErr, fingerprintErr) || manager.pw != nil || manager.installed {
		t.Fatalf("指纹失败后发布了半成品: err=%v pw=%p installed=%v", initErr, manager.pw, manager.installed)
	}
	// closeErr 是半成品资源释放后的关闭结果，验证 CloseContext 仍可幂等收束。
	if closeErr := manager.CloseContext(context.Background()); closeErr != nil {
		t.Fatalf("指纹失败后关闭失败: %v", closeErr)
	}
}

// TestInitializeAndCloseContextShareCancellation 验证初始化与关闭并发时，生命周期取消会让安装退出并让 CloseContext 等待收束。
func TestInitializeAndCloseContextShareCancellation(t *testing.T) {
	// manager 保存阻塞安装阶段的浏览器管理器。
	manager := NewManager(nil)
	// entered 通知安装替身已开始等待生命周期取消。
	entered := make(chan struct{})
	manager.installFn = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	// lifecycleCtx、cancelLifecycle 模拟协调器拥有的进程生命周期。
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	defer cancelLifecycle()
	// initDone 保存初始化调用的取消结果。
	initDone := make(chan error, 1)
	go func() { initDone <- manager.InitializeContext(lifecycleCtx) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("初始化未进入安装阶段")
	}
	// closeDone 保存关闭等待结果；关闭应先拒绝新操作并等待正在初始化的调用结束。
	closeDone := make(chan error, 1)
	go func() { closeDone <- manager.CloseContext(context.Background()) }()
	cancelLifecycle()
	// initErr 保存生命周期取消后初始化 goroutine 的最终错误。
	if initErr := <-initDone; !errors.Is(initErr, context.Canceled) {
		t.Fatalf("初始化取消错误=%v", initErr)
	}
	select {
	// closeErr 保存初始化已退出后并发关闭操作的最终结果。
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatalf("并发关闭失败: %v", closeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("并发关闭未等待初始化收束")
	}
}

// TestChromiumLaunchArgs 验证启动参数含关键安全/反检测项。
func TestChromiumLaunchArgs(t *testing.T) {
	// args 用于本次流程后续判断的args
	args := chromiumLaunchArgs()
	if len(args) == 0 {
		t.Fatal("应返回非空参数列表")
	}
	// want 用于本次流程后续判断的want
	want := []string{"--no-sandbox", "--disable-dev-shm-usage", "--disable-blink-features=AutomationControlled", "--lang=zh-CN"}
	// w 表示当前遍历过程中的w
	for _, w := range want {
		// found 用于本次流程后续判断的found
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

// TestPackagedPlaywrightRuntimeReady 封装TestPackagedPlaywrightRuntimeReady业务协调。
func TestPackagedPlaywrightRuntimeReady(t *testing.T) {
	// runtimeRoot 用于本次流程后续判断的runtimeRoot
	runtimeRoot := t.TempDir()
	// driverDir 用于本次流程后续判断的driverDir
	driverDir := filepath.Join(runtimeRoot, "driver")
	// browserDir 用于本次流程后续判断的浏览器Dir
	browserDir := filepath.Join(runtimeRoot, "browsers")
	if // err 用于本次流程后续判断的err
	err := os.MkdirAll(filepath.Join(driverDir, "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := os.MkdirAll(filepath.Join(browserDir, "chromium-1228"), 0o755); err != nil {
		t.Fatal(err)
	}
	// nodeName 用于本次流程后续判断的node名称
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
	}
	// path 表示当前遍历过程中的路径
	for _, path := range []string{
		filepath.Join(driverDir, nodeName),
		filepath.Join(driverDir, "package", "cli.js"),
	} {
		if // err 用于本次流程后续判断的err
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

// TestPackagedPlaywrightRuntimeReadyWithExternalNode 封装TestPackagedPlaywrightRuntimeReadyWithExternalNode业务协调。
func TestPackagedPlaywrightRuntimeReadyWithExternalNode(t *testing.T) {
	// runtimeRoot 用于本次流程后续判断的runtimeRoot
	runtimeRoot := t.TempDir()
	// driverDir 用于本次流程后续判断的driverDir
	driverDir := filepath.Join(runtimeRoot, "driver")
	// browserDir 用于本次流程后续判断的浏览器Dir
	browserDir := filepath.Join(runtimeRoot, "browsers")
	if // err 用于本次流程后续判断的err
	err := os.MkdirAll(filepath.Join(driverDir, "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := os.MkdirAll(filepath.Join(browserDir, "chromium-1228"), 0o755); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := os.WriteFile(filepath.Join(driverDir, "package", "cli.js"), []byte("runtime"), 0o644); err != nil {
		t.Fatal(err)
	}
	// nodePath 用于本次流程后续判断的node路径
	nodePath := filepath.Join(runtimeRoot, "node")
	if // err 用于本次流程后续判断的err
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
	// m 用于本次流程后续判断的m
	m := newTestManager(3)
	// old 用于本次流程后续判断的old
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
	// m 用于本次流程后续判断的m
	m := newTestManager(3)
	m.pool["c1"] = &poolEntry{cookieID: "c1"}
	m.pool["c2"] = &poolEntry{cookieID: "c2"}
	m.evict("c1")
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["c1"]; ok {
		t.Fatal("evict 应删除 c1")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["c2"]; !ok {
		t.Fatal("c2 应保留")
	}
	// evict 不存在的条目不 panic。
	m.evict("no-such")
}

// TestEvictIfNeededEvictsOldest 池满时驱逐最久未用的条目。
func TestEvictIfNeededEvictsOldest(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := newTestManager(2)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: time.Now().Add(-2 * time.Hour)}
	m.pool["c2"] = &poolEntry{cookieID: "c2", lastUsed: time.Now()}
	m.evictIfNeeded() // 池满（2 == maxSize），应驱逐 c1（最旧）
	if _, ok := m.pool["c1"]; ok {
		t.Fatal("应驱逐最旧的 c1")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["c2"]; !ok {
		t.Fatal("c2 应保留")
	}
}

// TestEvictIfNeededNoopWhenUnderLimit 池未满时不驱逐。
func TestEvictIfNeededNoopWhenUnderLimit(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := newTestManager(5)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: time.Now()}
	m.evictIfNeeded()
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["c1"]; !ok {
		t.Fatal("未满不应驱逐")
	}
}

// TestEvictIfNeededSkipsActiveEntries 封装TestEvictIfNeededSkipsActiveEntries业务协调。
func TestEvictIfNeededSkipsActiveEntries(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := newTestManager(2)
	m.pool["active-old"] = &poolEntry{cookieID: "active-old", lastUsed: time.Now().Add(-2 * time.Hour), active: 1}
	m.pool["idle-new"] = &poolEntry{cookieID: "idle-new", lastUsed: time.Now()}
	m.evictIfNeeded()
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["active-old"]; !ok {
		t.Fatal("正在执行 token 请求的条目不得被淘汰")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["idle-new"]; ok {
		t.Fatal("池满时应优先淘汰空闲条目")
	}
}

// TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive 封装TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive业务协调。
func TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := newTestManager(2)
	m.pool["active-1"] = &poolEntry{cookieID: "active-1", lastUsed: time.Now().Add(-2 * time.Hour), active: 1}
	m.pool["active-2"] = &poolEntry{cookieID: "active-2", lastUsed: time.Now().Add(-time.Hour), active: 1}
	m.evictIfNeeded()
	if len(m.pool) != 2 {
		t.Fatalf("所有条目活跃时不得强制淘汰，pool=%d", len(m.pool))
	}
}

// TestCleanupIdleSkipsActiveEntries 封装TestCleanupIdleSkipsActiveEntries业务协调。
func TestCleanupIdleSkipsActiveEntries(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := newTestManager(3)
	m.idleTTL = time.Minute
	// old 用于本次流程后续判断的old
	old := time.Now().Add(-time.Hour)
	m.pool["active"] = &poolEntry{cookieID: "active", lastUsed: old, active: 1}
	m.pool["idle"] = &poolEntry{cookieID: "idle", lastUsed: old}
	m.CleanupIdle()
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["active"]; !ok {
		t.Fatal("CleanupIdle 不得关闭仍有租约的条目")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := m.pool["idle"]; ok {
		t.Fatal("CleanupIdle 应清理过期空闲条目")
	}
}

// TestMarshalCookies 导出包装器等价 cookieMarshal。
func TestMarshalCookies(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := MarshalCookies(map[string]string{"unb": "1", "cna": "xx"})
	// map 顺序不保证，逐项检查。
	if !contains(got, "unb=1") || !contains(got, "cna=xx") {
		t.Fatalf("MarshalCookies=%q", got)
	}
}

// TestCookiesToMap playwright.Cookie 切片转 map。
func TestCookiesToMap(t *testing.T) {
	// cs 用于本次流程后续判断的cs
	cs := []playwright.Cookie{
		{Name: "unb", Value: "123"},
		{Name: "_m_h5_tk", Value: "tok"},
	}
	// m 用于本次流程后续判断的m
	m := cookiesToMap(cs)
	if m["unb"] != "123" || m["_m_h5_tk"] != "tok" || len(m) != 2 {
		t.Fatalf("cookiesToMap=%+v", m)
	}
	// 空切片。
	if m := cookiesToMap(nil); len(m) != 0 {
		t.Fatalf("空切片应返回空 map，got %+v", m)
	}
}

// contains 封装contains业务协调。
func contains(s, sub string) bool {
	for // i 用于本次流程后续判断的i
	i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
