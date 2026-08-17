package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// TestHeadlessFingerprintBrowserIntegration 在真实 Chromium 中验证 Playwright 与直连 CDP 的请求和页面指纹均不暴露无头标记；仅在显式开启浏览器集成测试时运行。
func TestHeadlessFingerprintBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to verify the effective headless browser fingerprint")
	}

	// requests 接收两个浏览器路径访问本地探针时的请求头，由测试 goroutine 创建且不关闭。
	requests := make(chan http.Header, 2)
	// server 是本地指纹探针，只记录目标路径请求而不涉及真实平台或凭证。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fingerprint" {
			requests <- r.Header.Clone()
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>fingerprint</title>")
	}))
	defer server.Close()

	// m 管理本次测试创建的 Playwright runtime，defer Close 负责同步回收其浏览器资源。
	m := NewManager(nil)
	defer m.Close()
	// err 表示 runtime 初始化或实测 Chromium 指纹读取失败。
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	// userAgent 是从当前打包 Chromium 实测并移除 HeadlessChrome 标记后的 UA。
	userAgent := m.headlessUserAgent()
	if userAgent == nil {
		t.Fatal("初始化后无头 UA 为空")
	}

	t.Run("playwright-context", func(t *testing.T) {
		// browser 是 Playwright 独立启动的无头 Chromium，子测试结束时关闭。
		browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
			Headless:       playwright.Bool(true),
			Args:           chromiumLaunchArgs(),
			ExecutablePath: chromiumExecutablePath(),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = browser.Close() }()
		// bctx 是 browser 的隔离上下文，使用实测 UA 并在子测试结束时释放。
		bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{UserAgent: userAgent})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = bctx.Close() }()
		// page 是导航前已应用 Client Hints 覆盖的探针页面；err 表示页面或覆盖创建失败。
		page, err := m.newBrowserPage(bctx, true)
		if err != nil {
			t.Fatal(err)
		}
		verifyEffectiveHeadlessFingerprint(t, page, server.URL+"/fingerprint", requests, *userAgent)
	})

	t.Run("direct-cdp-context", func(t *testing.T) {
		// profileDir 是直连 CDP Chromium 的临时用户目录，测试结束后由 testing 清理。
		profileDir := t.TempDir()
		// executable 默认使用 Playwright 管理的 Chromium，环境配置可指定同一兼容运行时。
		executable := m.pw.Chromium.ExecutablePath()
		// configured 是可选的环境指定可执行路径，存在时覆盖默认 runtime 路径。
		if configured := chromiumExecutablePath(); configured != nil {
			executable = *configured
		}
		// cmd 是直连 CDP 的 Chromium 进程，defer 中终止并等待其退出。
		cmd := exec.Command(executable, fallbackChromiumArgs(profileDir, true, userAgent)...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		// err 表示 Chromium 子进程未能启动。
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		// processDone 由等待 goroutine 发送一次子进程退出结果，defer 接收以避免遗留 goroutine。
		processDone := make(chan error, 1)
		// 等待 goroutine 只负责把 cmd.Wait 的结果转交给测试清理逻辑。
		go func() { processDone <- cmd.Wait() }()
		defer func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			select {
			case <-processDone:
			case <-time.After(2 * time.Second):
			}
		}()

		// ctx 限制 DevTools 发现总时长；cancel 在子测试退出时释放 timer 资源。
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// endpoint 是发现到的本地 DevTools 地址；err 表示进程提前退出或在限定时间内未就绪。
		endpoint, err := waitForDevToolsEndpoint(ctx, profileDir, processDone, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		// browser 是附着到外部 Chromium 的 CDP 句柄；关闭仅断开客户端连接。
		browser, err := m.pw.Chromium.ConnectOverCDP(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = browser.Close() }()
		// contexts 是外部 Chromium 已创建的 context 列表，直连模式应至少提供默认 context。
		contexts := browser.Contexts()
		if len(contexts) == 0 {
			t.Fatal("直接 CDP 浏览器无默认 context")
		}
		// page 是默认 context 中附加 CDP 指纹覆盖的验证页；err 表示覆盖建立失败。
		page, err := m.newBrowserPage(contexts[0], true)
		if err != nil {
			t.Fatal(err)
		}
		verifyEffectiveHeadlessFingerprint(t, page, server.URL+"/fingerprint", requests, *userAgent)
	})
}

// verifyEffectiveHeadlessFingerprint 导航 page 到本地 target，读取 requests 中的 HTTP 指纹并与 expectedUserAgent 对比；失败即终止所属测试。
func verifyEffectiveHeadlessFingerprint(t *testing.T, page playwright.Page, target string, requests <-chan http.Header, expectedUserAgent string) {
	t.Helper()
	// err 表示探针页面导航失败，页面未完成 DOM 加载时不能继续断言。
	if _, err := page.Goto(target, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		t.Fatal(err)
	}
	// headers 保存本次导航由本地探针捕获的 HTTP 请求头。
	var headers http.Header
	select {
	case headers = <-requests:
	case <-time.After(5 * time.Second):
		t.Fatal("等待浏览器指纹请求超时")
	}
	// pageIdentity 是页面内 navigator 暴露的 UA 与 Client Hints；err 表示脚本执行失败。
	pageIdentity, err := page.Evaluate(`() => ({
		userAgent: navigator.userAgent,
		userAgentData: navigator.userAgentData ? navigator.userAgentData.toJSON() : null
	})`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("effective user-agent=%q sec-ch-ua=%q page=%v", headers.Get("User-Agent"), headers.Get("Sec-CH-UA"), pageIdentity)
	// source 标识待检查的 HTTP 或页面指纹来源；value 是该来源暴露给目标站点的文本。
	for source, value := range map[string]string{
		"http-user-agent": headers.Get("User-Agent"),
		"http-sec-ch-ua":  headers.Get("Sec-CH-UA"),
		"page-identity":   fmt.Sprint(pageIdentity),
	} {
		if strings.Contains(strings.ToLower(value), "headless") {
			t.Fatalf("%s 仍暴露 headless 标记: %s", source, value)
		}
	}
	if !strings.Contains(headers.Get("User-Agent"), "Chrome/") {
		t.Fatalf("HTTP UA 缺少 Chrome 版本: %q", headers.Get("User-Agent"))
	}
	if headers.Get("User-Agent") != expectedUserAgent {
		t.Fatalf("HTTP UA 与实测 Chromium 版本不一致: got %q want %q", headers.Get("User-Agent"), expectedUserAgent)
	}
	// identity 是页面脚本结果的对象形式；ok 证明 Playwright 未改变预期的对象编码。
	identity, ok := pageIdentity.(map[string]any)
	if !ok || identity["userAgent"] != expectedUserAgent {
		t.Fatalf("navigator.userAgent 与实测 Chromium 版本不一致: %#v", pageIdentity)
	}
}
