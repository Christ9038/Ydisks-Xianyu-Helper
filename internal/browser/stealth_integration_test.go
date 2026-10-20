package browser

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestStealthScriptBrowserIntegration 在真实无头 Chromium 中验证 stealth 脚本补齐页面对象，且不改变主引擎归一化后的 UA；仅在显式开启浏览器集成测试时运行。
func TestStealthScriptBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to verify the stealth script in Chromium")
	}
	// m 管理本次测试创建的 Playwright runtime，defer Close 负责同步回收浏览器资源。
	m := NewManager(nil)
	defer m.Close()
	// initErr 保存 Playwright runtime 初始化错误。
	if initErr := m.init(); initErr != nil {
		t.Fatal(initErr)
	}
	// userAgent 是主引擎使用的、已移除无头标记的实测 UA。
	userAgent := m.headlessUserAgent()
	if userAgent == nil {
		t.Fatal("初始化后无头 UA 为空")
	}
	// browser 是独立启动的无头 Chromium，测试结束后关闭。
	browser, launchErr := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if launchErr != nil {
		t.Fatal(launchErr)
	}
	defer func() { _ = browser.Close() }()
	// bctx 使用主引擎相同的 UA 创建，页面由 newBrowserPage 打开。
	bctx, contextErr := browser.NewContext(playwright.BrowserNewContextOptions{UserAgent: userAgent})
	if contextErr != nil {
		t.Fatal(contextErr)
	}
	defer func() { _ = bctx.Close() }()
	// scriptErr 保存与生产 createEntry 相同的 stealth 初始化脚本注入结果。
	if scriptErr := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); scriptErr != nil {
		t.Fatal(scriptErr)
	}
	// page 是已注入 stealth 脚本和 UA 覆盖的页面。
	page, pageErr := m.newBrowserPage(bctx, true)
	if pageErr != nil {
		t.Fatal(pageErr)
	}
	// gotoErr 保存空白页导航错误，init script 会在该文档运行。
	if _, gotoErr := page.Goto("about:blank"); gotoErr != nil {
		t.Fatal(gotoErr)
	}
	// state 汇总页面内可观察到的对象形状与 UA。
	state, evalErr := page.Evaluate(`() => ({
		webdriver: navigator.webdriver,
		hasChromeApp: !!(window.chrome && window.chrome.app),
		hasCsi: typeof (window.chrome && window.chrome.csi) === 'function',
		hasLoadTimes: typeof (window.chrome && window.chrome.loadTimes) === 'function',
		hasRuntime: !!(window.chrome && window.chrome.runtime),
		outerOK: window.outerWidth > 0 && window.outerHeight > 0,
		languages: navigator.languages.length,
		userAgent: navigator.userAgent,
		brands: JSON.stringify(navigator.userAgentData ? navigator.userAgentData.brands : []),
		cores: navigator.hardwareConcurrency,
		plugins: navigator.plugins.length,
		h264: document.createElement('video').canPlayType('video/mp4; codecs="avc1.42E01E"'),
		gl: (() => {
			const gl = document.createElement('canvas').getContext('webgl');
			const ext = gl && gl.getExtension('WEBGL_debug_renderer_info');
			return ext ? gl.getParameter(ext.UNMASKED_VENDOR_WEBGL) + '|' + gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) : 'unavailable';
		})(),
		toStringNative: Function.prototype.toString.call(navigator.permissions.query).includes('[native code]'),
	})`)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	// values 是页面状态的键值形式；类型不符说明探针返回异常。
	values, ok := state.(map[string]any)
	if !ok {
		t.Fatalf("页面状态类型=%T", state)
	}
	// key 是必须为 true 的页面探针字段名。
	for _, key := range []string{"hasChromeApp", "hasCsi", "hasLoadTimes", "hasRuntime", "outerOK", "toStringNative"} {
		if values[key] != true {
			t.Fatalf("%s 应为 true: %#v", key, values)
		}
	}
	if values["cores"] != float64(4) && values["cores"] != 4 {
		t.Fatalf("hardwareConcurrency 应为 4: %#v", values["cores"])
	}
	// pluginCount 是页面读取到的插件数量文本，Playwright 可能把 JS 数字解码为不同 Go 数字类型。
	pluginCount, pluginErr := strconv.ParseFloat(fmt.Sprint(values["plugins"]), 64)
	if pluginErr != nil || pluginCount <= 0 {
		t.Fatalf("插件列表不应为空: %#v", values["plugins"])
	}
	if values["h264"] != "probably" {
		t.Fatalf("H.264 canPlayType 应为 probably: %#v", values["h264"])
	}
	// glInfo 是页面读取到的图形未遮蔽厂商与渲染器；环境不支持图形接口时跳过该项断言。
	if glInfo, _ := values["gl"].(string); glInfo != "unavailable" && glInfo != "Intel Inc.|Intel Iris OpenGL Engine" {
		t.Fatalf("图形厂商与渲染器应被对齐: %q", glInfo)
	}
	if values["webdriver"] != nil && values["webdriver"] != false {
		t.Fatalf("navigator.webdriver 应为 undefined: %#v", values["webdriver"])
	}
	if values["userAgent"] != *userAgent {
		t.Fatalf("stealth 不得改变主引擎 UA: got=%v want=%s", values["userAgent"], *userAgent)
	}
	// brands 是页面读取到的 Client Hints 品牌 JSON 文本。
	if brands, _ := values["brands"].(string); brands == "" || containsHeadlessBrand(brands) {
		t.Fatalf("Client Hints 品牌异常: %v", values["brands"])
	}
}

// containsHeadlessBrand 判断 Client Hints 品牌文本是否仍含无头标记。
func containsHeadlessBrand(brands string) bool {
	return len(brands) >= 8 && (indexFold(brands, "HeadlessChrome") >= 0)
}

// indexFold 返回 substr 在 s 中首次出现的位置（区分大小写），不存在返回 -1。
func indexFold(s, substr string) int {
	// i 是当前比较的起始下标。
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
