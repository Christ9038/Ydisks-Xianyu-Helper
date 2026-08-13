package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func TestReferencePlaywrightSliderBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to exercise the Playwright slider")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>验证码拦截</title></head><body>
<div class="nc-container">
  <div id="nc_1_n1t" class="nc_scale" style="position:relative;width:420px;height:44px;background:#ddd">
    <span id="nc_1_n1z" style="display:block;width:44px;height:44px;background:#333"></span>
  </div>
</div>
<script>
const slider = document.querySelector('#nc_1_n1z');
let pressed = false;
let released = false;
let startX = 0;
slider.addEventListener('mousedown', event => {
  pressed = true;
  startX = event.clientX;
});
window.addEventListener('mouseup', () => {
  released = pressed;
});
slider.addEventListener('click', event => {
  if (!released || event.clientX - startX < 300) return;
  document.cookie = 'x5sec=playwright-integration-fresh; path=/';
  history.replaceState({}, '', '/done');
  document.querySelector('.nc-container').remove();
  document.title = '验证完成';
});
</script></body></html>`)
	}))
	defer server.Close()

	m := NewManager(nil)
	defer m.Close()
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	bctx, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Goto(server.URL+"/punish", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatal(err)
	}
	if err := solveSliderStrict(page, false, m.logger, map[string]struct{}{}, time.Now().Add(15*time.Second)); err != nil {
		t.Fatalf("Playwright slider: %v", err)
	}
	cookies, err := bctx.Cookies()
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh := freshX5Cookies(cookies, nil); !fresh || isPunishURL(page.URL()) {
		t.Fatalf("slider did not complete reference success flow: url=%s cookies=%v", page.URL(), cookies)
	}
}

func TestPlaywrightSliderRecoversFromHiddenFailureState(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to exercise slider failure recovery")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>验证码拦截</title></head><body>
<div id="nocaptcha" class="nc-container"><div id="nc_1_wrapper"></div></div>
<script>
let dragAttempts = 0;
let retryClicks = 0;
let pressed = false;
let startX = 0;
function renderSlider() {
  document.querySelector('#nc_1_wrapper').innerHTML = '<div id="nc_1_n1t" class="nc_scale" style="position:relative;width:300px;height:34px;background:#ddd"><span id="nc_1_n1z" class="nc_iconfont btn_slide" style="position:absolute;left:0;width:42px;height:34px;background:#333"></span></div>';
}
renderSlider();
document.addEventListener('mousedown', event => {
  if (event.target.id !== 'nc_1_n1z') return;
  pressed = true;
  startX = event.clientX;
});
document.addEventListener('mouseup', event => {
  if (!pressed) return;
  pressed = false;
  dragAttempts++;
  if (dragAttempts === 1) {
    document.querySelector('#nc_1_wrapper').innerHTML = '<div class="errloading">验证失败，点击框体重试</div>';
    return;
  }
  if (event.clientX - startX < 250) return;
  document.cookie = 'x5sec=playwright-retry-fresh; path=/';
  history.replaceState({}, '', '/done');
  document.querySelector('.nc-container').remove();
  document.title = '验证完成';
});
document.addEventListener('click', event => {
  if (!event.target.closest('.errloading')) return;
  retryClicks++;
  renderSlider();
});
</script></body></html>`)
	}))
	defer server.Close()

	m := NewManager(nil)
	defer m.Close()
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	bctx, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Goto(server.URL+"/punish", playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		t.Fatal(err)
	}
	if err := solveSliderStrict(page, false, m.logger, map[string]struct{}{}, time.Now().Add(25*time.Second)); err != nil {
		t.Fatalf("slider recovery: %v", err)
	}
	state, err := page.Evaluate(`() => ({dragAttempts, retryClicks})`)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := state.(map[string]any)
	if !ok || fmt.Sprint(values["dragAttempts"]) != "2" || fmt.Sprint(values["retryClicks"]) != "1" {
		t.Fatalf("unexpected retry state: %#v", state)
	}
}

func TestTokenCaptchaDirectErrorPageBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to exercise direct-error page classification")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>验证码拦截</title></head><body>
<div class="errloading">Oops... something's wrong. Please refresh and try again.(error:6T0DWd)</div>
</body></html>`)
	}))
	defer server.Close()

	m := NewManager(nil)
	defer m.Close()
	if err := m.init(); err != nil {
		t.Fatal(err)
	}
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Goto(server.URL+"/punish", playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		t.Fatal(err)
	}
	if err := tokenCaptchaDirectPageError(page); !errors.Is(err, errTokenCaptchaDirectPageError) {
		t.Fatalf("无滑块错误页应停止自动验证: %v", err)
	}
}

func TestTokenCaptchaCDPFallbackBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("set RUN_BROWSER_INTEGRATION=1 to exercise the direct-CDP Chromium fallback")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>验证码拦截</title></head><body>
<div class="nc-container"><div id="nc_1_wrapper"></div></div>
<script>
let attempts = 0;
let pressed = false;
let startX = 0;
function renderSlider() {
  document.querySelector('#nc_1_wrapper').innerHTML = '<div id="nc_1_n1t" class="nc_scale" style="position:relative;width:300px;height:34px;background:#ddd"><span id="nc_1_n1z" style="position:absolute;left:0;width:42px;height:34px;background:#333"></span></div>';
}
renderSlider();
document.addEventListener('mousedown', event => {
  if (event.target.id !== 'nc_1_n1z') return;
  pressed = true;
  startX = event.clientX;
});
document.addEventListener('mouseup', event => {
  if (!pressed) return;
  pressed = false;
  attempts++;
  if (attempts === 1) {
    document.querySelector('#nc_1_wrapper').innerHTML = '<div class="errloading">验证失败，点击框体重试</div>';
    return;
  }
  if (event.clientX - startX < 250) return;
  document.cookie = 'x5sec=integration-fresh; path=/';
  document.title = '验证完成';
  document.querySelector('.nc-container').remove();
});
document.addEventListener('click', event => {
  if (event.target.closest('.errloading')) renderSlider();
});
</script></body></html>`)
	}))
	defer server.Close()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	t.Setenv("CAPTCHA_DRISSIONPAGE_TIMEOUT", "35")
	m := NewManager(nil)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cookies, err := m.tokenCaptchaCDPFallback(ctx, "integration-account", "unb=1", server.URL, true, nil)
	if err != nil {
		t.Fatalf("direct CDP fallback: %v", err)
	}
	if !strings.Contains(cookies, "x5sec=integration-fresh") {
		t.Fatalf("fallback cookies=%q", cookies)
	}
}
