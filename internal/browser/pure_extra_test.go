package browser

import "testing"

// TestSanitize 特殊字符替换为下划线（用于 userDataDir 命名）。
func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"acc_1":     "acc_1",
		"acc/1:2 3": "acc_1_2_3",
		`a\b:c d`:   "a_b_c_d",
		"":          "",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

// TestGetCSSSelector 始终返回首选器（元素句柄仅占位，nil 与非 nil 同结果）。
func TestGetCSSSelector(t *testing.T) {
	selectors := []string{"#user", "input[name=user]"}
	if got := getCSSSelector(nil, selectors); got != "#user" {
		t.Errorf("应返回首选器，got %q", got)
	}
	// 单元素切片。
	if got := getCSSSelector(nil, []string{"#pwd"}); got != "#pwd" {
		t.Errorf("单元素应返回该项，got %q", got)
	}
}

func TestPureUserIDMatchesReferenceRule(t *testing.T) {
	cases := map[string]string{
		"foo_1234567890":     "foo",
		"foo_bar_1234567890": "foo_bar",
		"foo_123":            "foo_123",
		"foo":                "foo",
		"":                   "unknown",
		"foo/bar_1234567890": "foo_bar",
	}
	for in, want := range cases {
		if got := pureUserID(in); got != want {
			t.Fatalf("pureUserID(%q)=%q want %q", in, got, want)
		}
	}
}

func TestQuickRenewHeadlessUsesArgumentUnlessEnvOverrides(t *testing.T) {
	t.Setenv("BROWSER_HEADLESS", "")
	if !quickRenewHeadless(true) {
		t.Fatal("未设置环境变量时应使用传入的 headless=true")
	}
	if quickRenewHeadless(false) {
		t.Fatal("未设置环境变量时应使用传入的 headless=false")
	}
	t.Setenv("BROWSER_HEADLESS", "true")
	if !quickRenewHeadless(false) {
		t.Fatal("BROWSER_HEADLESS=true 时应使用 headless")
	}
	t.Setenv("BROWSER_HEADLESS", "false")
	if quickRenewHeadless(true) {
		t.Fatal("BROWSER_HEADLESS=false 时应使用可视化浏览器")
	}
}

func TestResolveHeadlessUsesShowBrowserConsistently(t *testing.T) {
	t.Setenv("BROWSER_HEADLESS", "")
	if !ResolveHeadless(false) {
		t.Fatal("show_browser=false should run headless")
	}
	if ResolveHeadless(true) {
		t.Fatal("show_browser=true should run headed")
	}
	t.Setenv("BROWSER_HEADLESS", "true")
	if !ResolveHeadless(true) {
		t.Fatal("env override should force headless")
	}
	t.Setenv("BROWSER_HEADLESS", "false")
	if ResolveHeadless(false) {
		t.Fatal("env override should force headed")
	}
}

func TestCookiesRefreshHeadlessUsesAccountPreference(t *testing.T) {
	t.Setenv("BROWSER_HEADLESS", "")
	if !cookiesRefreshHeadless(true) {
		t.Fatal("定时 COOKIES 续期应尊重 headless=true")
	}
	if cookiesRefreshHeadless(false) {
		t.Fatal("show_browser=true 时定时 COOKIES 续期应使用可视化浏览器")
	}
	t.Setenv("BROWSER_HEADLESS", "true")
	if !cookiesRefreshHeadless(false) {
		t.Fatal("环境变量应仍可强制定时 COOKIES 续期 headless")
	}
}

func TestChromiumExecutablePathFromEnv(t *testing.T) {
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", "")
	if chromiumExecutablePath() != nil {
		t.Fatal("未设置 PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH 时应返回 nil")
	}
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", " /usr/bin/chromium ")
	got := chromiumExecutablePath()
	if got == nil || *got != "/usr/bin/chromium" {
		t.Fatalf("chromiumExecutablePath=%v", got)
	}
}

func TestSkipPlaywrightBrowserDownloadFromEnv(t *testing.T) {
	t.Setenv("PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD", "")
	if skipPlaywrightBrowserDownload() {
		t.Fatal("默认不应跳过浏览器下载")
	}
	t.Setenv("PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD", "true")
	if !skipPlaywrightBrowserDownload() {
		t.Fatal("true 应跳过浏览器下载")
	}
	t.Setenv("PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD", "0")
	if skipPlaywrightBrowserDownload() {
		t.Fatal("0 不应跳过浏览器下载")
	}
}

// TestCalculateSlideDistance_Fallback nil 轨道/按钮时走兜底距离。
func TestCalculateSlideDistance_Fallback(t *testing.T) {
	// 无 scratch：220-259。
	dist, err := calculateSlideDistance(nil, nil, false)
	if err != nil || dist < 220 || dist > 259 {
		t.Fatalf("无 scratch 兜底应 220-259，got %v err=%v", dist, err)
	}
	// scratch：兜底 * 0.25-0.35 → 55-90。
	dist, err = calculateSlideDistance(nil, nil, true)
	if err != nil || dist < 55 || dist > 91 {
		t.Fatalf("scratch 兜底应 55-91，got %v err=%v", dist, err)
	}
}
