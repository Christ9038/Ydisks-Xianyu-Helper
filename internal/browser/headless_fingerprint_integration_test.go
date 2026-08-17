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

func TestHeadlessFingerprintBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to verify the effective headless browser fingerprint")
	}

	requests := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fingerprint" {
			requests <- r.Header.Clone()
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>fingerprint</title>")
	}))
	defer server.Close()

	m := NewManager(nil)
	defer m.Close()
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	userAgent := m.headlessUserAgent()
	if userAgent == nil {
		t.Fatal("初始化后无头 UA 为空")
	}

	t.Run("playwright-context", func(t *testing.T) {
		browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
			Headless:       playwright.Bool(true),
			Args:           chromiumLaunchArgs(),
			ExecutablePath: chromiumExecutablePath(),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = browser.Close() }()
		bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{UserAgent: userAgent})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = bctx.Close() }()
		page, err := m.newBrowserPage(bctx, true)
		if err != nil {
			t.Fatal(err)
		}
		verifyEffectiveHeadlessFingerprint(t, page, server.URL+"/fingerprint", requests, *userAgent)
	})

	t.Run("direct-cdp-context", func(t *testing.T) {
		profileDir := t.TempDir()
		executable := m.pw.Chromium.ExecutablePath()
		if configured := chromiumExecutablePath(); configured != nil {
			executable = *configured
		}
		cmd := exec.Command(executable, fallbackChromiumArgs(profileDir, true, userAgent)...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		processDone := make(chan error, 1)
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

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		endpoint, err := waitForDevToolsEndpoint(ctx, profileDir, processDone, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		browser, err := m.pw.Chromium.ConnectOverCDP(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = browser.Close() }()
		contexts := browser.Contexts()
		if len(contexts) == 0 {
			t.Fatal("直接 CDP 浏览器无默认 context")
		}
		page, err := m.newBrowserPage(contexts[0], true)
		if err != nil {
			t.Fatal(err)
		}
		verifyEffectiveHeadlessFingerprint(t, page, server.URL+"/fingerprint", requests, *userAgent)
	})
}

func verifyEffectiveHeadlessFingerprint(t *testing.T, page playwright.Page, target string, requests <-chan http.Header, expectedUserAgent string) {
	t.Helper()
	if _, err := page.Goto(target, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		t.Fatal(err)
	}
	var headers http.Header
	select {
	case headers = <-requests:
	case <-time.After(5 * time.Second):
		t.Fatal("等待浏览器指纹请求超时")
	}
	pageIdentity, err := page.Evaluate(`() => ({
		userAgent: navigator.userAgent,
		userAgentData: navigator.userAgentData ? navigator.userAgentData.toJSON() : null
	})`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("effective user-agent=%q sec-ch-ua=%q page=%v", headers.Get("User-Agent"), headers.Get("Sec-CH-UA"), pageIdentity)
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
	identity, ok := pageIdentity.(map[string]any)
	if !ok || identity["userAgent"] != expectedUserAgent {
		t.Fatalf("navigator.userAgent 与实测 Chromium 版本不一致: %#v", pageIdentity)
	}
}
