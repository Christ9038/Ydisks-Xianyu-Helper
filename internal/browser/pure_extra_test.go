package browser

import (
	"fmt"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu"
)

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

func TestCaptchaIgnoreCertificateErrors(t *testing.T) {
	t.Setenv("CAPTCHA_IGNORE_CERT_ERRORS", "")
	if captchaIgnoreCertificateErrors() {
		t.Fatal("默认不应忽略证书错误")
	}
	t.Setenv("CAPTCHA_IGNORE_CERT_ERRORS", "true")
	if !captchaIgnoreCertificateErrors() {
		t.Fatal("true 应启用 CAPTCHA 证书错误忽略开关")
	}
	t.Setenv("CAPTCHA_IGNORE_CERT_ERRORS", "not-a-bool")
	if captchaIgnoreCertificateErrors() {
		t.Fatal("无效值不应启用证书错误忽略开关")
	}
}

func TestNormalizeBrowserFingerprintRemovesOnlyHeadlessMarker(t *testing.T) {
	fingerprint := normalizeBrowserFingerprint(xianyu.BrowserFingerprint{
		UserAgent: " Mozilla/5.0 HeadlessChrome/149.0.7827.55 Safari/537.36 ",
		SecChUA:   `"HeadlessChrome";v="149", "Chromium";v="149"`,
		Platform:  "macOS",
		Mobile:    "?0",
	})
	if strings.Contains(fingerprint.UserAgent, "HeadlessChrome") || strings.Contains(fingerprint.SecChUA, "HeadlessChrome") {
		t.Fatalf("无头标记未清除: %+v", fingerprint)
	}
	if !strings.Contains(fingerprint.UserAgent, "Chrome/149.0.7827.55") {
		t.Fatalf("Chromium 版本不应变化: %q", fingerprint.UserAgent)
	}
	if fingerprint.Platform != "macOS" || fingerprint.Mobile != "?0" {
		t.Fatalf("非无头字段不应变化: %+v", fingerprint)
	}
	if strings.Count(fingerprint.SecChUA, `"Chromium"`) != 1 {
		t.Fatalf("Client Hints 品牌应去重: %q", fingerprint.SecChUA)
	}
}

func TestNormalizeUserAgentMetadataRemovesAndDeduplicatesHeadlessBrand(t *testing.T) {
	metadata := normalizeUserAgentMetadata(map[string]any{
		"brands": []any{
			map[string]any{"brand": "HeadlessChrome", "version": "149"},
			map[string]any{"brand": "Chromium", "version": "149"},
			map[string]any{"brand": "Not)A;Brand", "version": "24"},
		},
		"fullVersionList": []any{
			map[string]any{"brand": "HeadlessChrome", "version": "149.0.7827.55"},
			map[string]any{"brand": "Chromium", "version": "149.0.7827.55"},
		},
		"platform": "macOS",
		"mobile":   false,
	})
	if strings.Contains(strings.ToLower(fmt.Sprint(metadata)), "headless") {
		t.Fatalf("User-Agent metadata 仍暴露 headless: %#v", metadata)
	}
	brands, ok := metadata["brands"].([]any)
	if !ok || len(brands) != 2 {
		t.Fatalf("brands 未正确去重: %#v", metadata["brands"])
	}
	fullVersions, ok := metadata["fullVersionList"].([]any)
	if !ok || len(fullVersions) != 1 {
		t.Fatalf("fullVersionList 未正确去重: %#v", metadata["fullVersionList"])
	}
}

func TestManagerHeadlessUserAgentUsesDetectedRuntimeVersion(t *testing.T) {
	m := &Manager{browserFingerprint: xianyu.BrowserFingerprint{UserAgent: "Mozilla/5.0 HeadlessChrome/149.0.7827.55 Safari/537.36"}}
	userAgent := m.headlessUserAgent()
	if userAgent == nil || *userAgent != "Mozilla/5.0 Chrome/149.0.7827.55 Safari/537.36" {
		t.Fatalf("headlessUserAgent=%v", userAgent)
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
