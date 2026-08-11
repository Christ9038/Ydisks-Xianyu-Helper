package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

// TestChromiumLaunchArgs 验证启动参数含关键安全/反检测项。
func TestChromiumLaunchArgs(t *testing.T) {
	args := chromiumLaunchArgs()
	if len(args) == 0 {
		t.Fatal("应返回非空参数列表")
	}
	want := []string{"--no-sandbox", "--disable-dev-shm-usage", "--disable-blink-features=AutomationControlled", "--lang=zh-CN"}
	for _, w := range want {
		found := false
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

func TestPackagedPlaywrightRuntimeReady(t *testing.T) {
	runtimeRoot := t.TempDir()
	driverDir := filepath.Join(runtimeRoot, "driver")
	browserDir := filepath.Join(runtimeRoot, "browsers")
	if err := os.MkdirAll(filepath.Join(driverDir, "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(browserDir, "chromium-1228"), 0o755); err != nil {
		t.Fatal(err)
	}
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
	}
	for _, path := range []string{
		filepath.Join(driverDir, nodeName),
		filepath.Join(driverDir, "package", "cli.js"),
	} {
		if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
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
	m := newTestManager(3)
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
	m := newTestManager(3)
	m.pool["c1"] = &poolEntry{cookieID: "c1"}
	m.pool["c2"] = &poolEntry{cookieID: "c2"}
	m.evict("c1")
	if _, ok := m.pool["c1"]; ok {
		t.Fatal("evict 应删除 c1")
	}
	if _, ok := m.pool["c2"]; !ok {
		t.Fatal("c2 应保留")
	}
	// evict 不存在的条目不 panic。
	m.evict("no-such")
}

// TestEvictIfNeededEvictsOldest 池满时驱逐最久未用的条目。
func TestEvictIfNeededEvictsOldest(t *testing.T) {
	m := newTestManager(2)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: time.Now().Add(-2 * time.Hour)}
	m.pool["c2"] = &poolEntry{cookieID: "c2", lastUsed: time.Now()}
	m.evictIfNeeded() // 池满（2 == maxSize），应驱逐 c1（最旧）
	if _, ok := m.pool["c1"]; ok {
		t.Fatal("应驱逐最旧的 c1")
	}
	if _, ok := m.pool["c2"]; !ok {
		t.Fatal("c2 应保留")
	}
}

// TestEvictIfNeededNoopWhenUnderLimit 池未满时不驱逐。
func TestEvictIfNeededNoopWhenUnderLimit(t *testing.T) {
	m := newTestManager(5)
	m.pool["c1"] = &poolEntry{cookieID: "c1", lastUsed: time.Now()}
	m.evictIfNeeded()
	if _, ok := m.pool["c1"]; !ok {
		t.Fatal("未满不应驱逐")
	}
}

func TestEvictIfNeededSkipsActiveEntries(t *testing.T) {
	m := newTestManager(2)
	m.pool["active-old"] = &poolEntry{cookieID: "active-old", lastUsed: time.Now().Add(-2 * time.Hour), active: 1}
	m.pool["idle-new"] = &poolEntry{cookieID: "idle-new", lastUsed: time.Now()}
	m.evictIfNeeded()
	if _, ok := m.pool["active-old"]; !ok {
		t.Fatal("正在执行 token 请求的条目不得被淘汰")
	}
	if _, ok := m.pool["idle-new"]; ok {
		t.Fatal("池满时应优先淘汰空闲条目")
	}
}

func TestEvictIfNeededAllowsTemporaryOverflowWhenAllActive(t *testing.T) {
	m := newTestManager(2)
	m.pool["active-1"] = &poolEntry{cookieID: "active-1", lastUsed: time.Now().Add(-2 * time.Hour), active: 1}
	m.pool["active-2"] = &poolEntry{cookieID: "active-2", lastUsed: time.Now().Add(-time.Hour), active: 1}
	m.evictIfNeeded()
	if len(m.pool) != 2 {
		t.Fatalf("所有条目活跃时不得强制淘汰，pool=%d", len(m.pool))
	}
}

func TestCleanupIdleSkipsActiveEntries(t *testing.T) {
	m := newTestManager(3)
	m.idleTTL = time.Minute
	old := time.Now().Add(-time.Hour)
	m.pool["active"] = &poolEntry{cookieID: "active", lastUsed: old, active: 1}
	m.pool["idle"] = &poolEntry{cookieID: "idle", lastUsed: old}
	m.CleanupIdle()
	if _, ok := m.pool["active"]; !ok {
		t.Fatal("CleanupIdle 不得关闭仍有租约的条目")
	}
	if _, ok := m.pool["idle"]; ok {
		t.Fatal("CleanupIdle 应清理过期空闲条目")
	}
}

// TestMarshalCookies 导出包装器等价 cookieMarshal。
func TestMarshalCookies(t *testing.T) {
	got := MarshalCookies(map[string]string{"unb": "1", "cna": "xx"})
	// map 顺序不保证，逐项检查。
	if !contains(got, "unb=1") || !contains(got, "cna=xx") {
		t.Fatalf("MarshalCookies=%q", got)
	}
}

// TestCookiesToMap playwright.Cookie 切片转 map。
func TestCookiesToMap(t *testing.T) {
	cs := []playwright.Cookie{
		{Name: "unb", Value: "123"},
		{Name: "_m_h5_tk", Value: "tok"},
	}
	m := cookiesToMap(cs)
	if m["unb"] != "123" || m["_m_h5_tk"] != "tok" || len(m) != 2 {
		t.Fatalf("cookiesToMap=%+v", m)
	}
	// 空切片。
	if m := cookiesToMap(nil); len(m) != 0 {
		t.Fatalf("空切片应返回空 map，got %+v", m)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
