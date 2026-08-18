package browser

import (
	"fmt"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu"
)

// TestSanitize 特殊字符替换为下划线（用于 userDataDir 命名）。
func TestSanitize(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"acc_1":     "acc_1",
		"acc/1:2 3": "acc_1_2_3",
		`a\b:c d`:   "a_b_c_d",
		"":          "",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := sanitize(in); got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

// TestPureUserIDMatchesReferenceRule 封装TestPure用户IDMatchesReference规则业务协调。
func TestPureUserIDMatchesReferenceRule(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"foo_1234567890":     "foo",
		"foo_bar_1234567890": "foo_bar",
		"foo_123":            "foo_123",
		"foo":                "foo",
		"":                   "unknown",
		"foo/bar_1234567890": "foo_bar",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := pureUserID(in); got != want {
			t.Fatalf("pureUserID(%q)=%q want %q", in, got, want)
		}
	}
}

// TestQuickRenewHeadlessUsesArgumentUnlessEnvOverrides 封装TestQuickRenewHeadlessUsesArgumentUnlessEnvOverrides业务协调。
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

// TestResolveHeadlessUsesShowBrowserConsistently 封装TestResolveHeadlessUsesShow浏览器Consistently业务协调。
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

// TestCookiesRefreshHeadlessUsesAccountPreference 封装TestCookiesRefreshHeadlessUses账号Preference业务协调。
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

// TestChromiumExecutablePathFromEnv 封装TestChromiumExecutable路径FromEnv业务协调。
func TestChromiumExecutablePathFromEnv(t *testing.T) {
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", "")
	if chromiumExecutablePath() != nil {
		t.Fatal("未设置 PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH 时应返回 nil")
	}
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", " /usr/bin/chromium ")
	// got 用于本次流程后续判断的got
	got := chromiumExecutablePath()
	if got == nil || *got != "/usr/bin/chromium" {
		t.Fatalf("chromiumExecutablePath=%v", got)
	}
}

// TestCaptchaIgnoreCertificateErrors 验证证书例外仅在明确环境开关为真时进入浏览器启动参数。
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

// TestCaptchaBrowserProxy 验证验证码浏览器只接受无凭证的受限代理地址，并在配置为空或非法时保留系统代理行为。
func TestCaptchaBrowserProxy(t *testing.T) {
	// testCases 是输入环境值与预期解析结果，覆盖默认、支持协议及敏感/危险输入。
	testCases := []struct {
		// name 是失败时定位具体输入的用例名。
		name string
		// value 是待解析的 CAPTCHA 浏览器代理配置。
		value string
		// want 是允许传给 Chromium 的预期代理地址，空串代表不追加覆盖参数。
		want string
	}{
		{name: "empty", value: "", want: ""},
		{name: "http", value: " http://127.0.0.1:1082 ", want: "http://127.0.0.1:1082"},
		{name: "socks5", value: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{name: "credentials", value: "http://user:secret@127.0.0.1:1082", want: ""},
		{name: "query", value: "http://127.0.0.1:1082?secret=value", want: ""},
		{name: "unsupported", value: "file:///tmp/proxy", want: ""},
	}
	// testCase 是当前要通过独立环境变量验证的代理配置用例。
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("CAPTCHA_BROWSER_PROXY", testCase.value)
			// got 是解析后允许写入 Chromium 参数的代理地址。
			got := captchaBrowserProxy()
			if got != testCase.want {
				t.Fatalf("captchaBrowserProxy()=%q, want %q", got, testCase.want)
			}
		})
	}
	t.Setenv("CAPTCHA_BROWSER_PROXY", "http://127.0.0.1:1082")
	// proxyArgs 是显式代理启用时主 Chromium 引擎的启动参数集合。
	proxyArgs := strings.Join(chromiumLaunchArgs(), " ")
	if !strings.Contains(proxyArgs, "--proxy-server=http://127.0.0.1:1082") {
		t.Fatalf("主引擎未写入显式代理参数: %s", proxyArgs)
	}
	// fallbackArgs 是备用直接 CDP 引擎的独立参数集合，必须使用相同代理而不是回退到 Fake-IP 直连。
	fallbackArgs := strings.Join(fallbackChromiumArgs("/tmp/profile", true), " ")
	if !strings.Contains(fallbackArgs, "--proxy-server=http://127.0.0.1:1082") {
		t.Fatalf("备用引擎未写入显式代理参数: %s", fallbackArgs)
	}
}

// TestNormalizeBrowserFingerprintRemovesOnlyHeadlessMarker 验证规范化不伪造 Chromium 的版本或平台身份。
func TestNormalizeBrowserFingerprintRemovesOnlyHeadlessMarker(t *testing.T) {
	// fingerprint 是含无头标记的实测身份经过规范化后的结果，用于断言只移除产品标记。
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

// TestNormalizeUserAgentMetadataRemovesAndDeduplicatesHeadlessBrand 验证导航前 CDP 覆盖不会泄露无头品牌。
func TestNormalizeUserAgentMetadataRemovesAndDeduplicatesHeadlessBrand(t *testing.T) {
	// metadata 是模拟页面返回的 Client Hints 经规范化后的副本，不应再暴露无头品牌。
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
	// brands 是规范化后的基础品牌列表；ok 表示 CDP 可接收数组结构。
	brands, ok := metadata["brands"].([]any)
	if !ok || len(brands) != 2 {
		t.Fatalf("brands 未正确去重: %#v", metadata["brands"])
	}
	// fullVersions 是规范化后的完整版本品牌列表；ok 表示字段没有被意外改变类型。
	fullVersions, ok := metadata["fullVersionList"].([]any)
	if !ok || len(fullVersions) != 1 {
		t.Fatalf("fullVersionList 未正确去重: %#v", metadata["fullVersionList"])
	}
}

// TestManagerHeadlessUserAgentUsesDetectedRuntimeVersion 验证无头 UA 沿用实测 Chromium 版本而非静态伪造值。
func TestManagerHeadlessUserAgentUsesDetectedRuntimeVersion(t *testing.T) {
	// m 模拟已完成运行时指纹探测的管理器，不需要启动真实 Chromium。
	m := &Manager{browserFingerprint: xianyu.BrowserFingerprint{UserAgent: "Mozilla/5.0 HeadlessChrome/149.0.7827.55 Safari/537.36"}}
	// userAgent 是 m 基于实测版本生成的无头 UA，应只替换产品标记。
	userAgent := m.headlessUserAgent()
	if userAgent == nil || *userAgent != "Mozilla/5.0 Chrome/149.0.7827.55 Safari/537.36" {
		t.Fatalf("headlessUserAgent=%v", userAgent)
	}
}

// TestSkipPlaywrightBrowserDownloadFromEnv 验证打包运行时可显式禁止 Playwright 下载新的浏览器。
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
