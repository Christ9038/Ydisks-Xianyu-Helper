package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	tokenCaptchaCookieWait = 15 * time.Second
	tokenCaptchaCookiePoll = 250 * time.Millisecond
	tokenCaptchaBrowserTTL = 45 * time.Second
)

type browserCookieReader interface {
	Cookies(urls ...string) ([]playwright.Cookie, error)
}

// TokenCaptchaURLProvider obtains a fresh punish URL between browser attempts,
// after the previous persistent profile has been closed.
type TokenCaptchaURLProvider func(ctx context.Context, currentCookies string) (url string, tokenOK bool, updatedCookies string, err error)

type tokenCaptchaEngineFunc func(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider TokenCaptchaURLProvider) (string, error)

var errTokenCaptchaURLExpired = errors.New("token 风控验证链接已过期")

// TokenCaptchaRecover solves a token-refresh captcha and returns cookies with x5sec merged in.
func (m *Manager) TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider TokenCaptchaURLProvider) (string, error) {
	cookies, _, err := m.TokenCaptchaRecoverWithEngine(ctx, cookieID, cookieStr, verificationURL, headless, provider)
	return cookies, err
}

// TokenCaptchaRecoverWithEngine 按参考项目顺序执行 Playwright 主引擎，再执行直接 CDP 备用引擎。
func (m *Manager) TokenCaptchaRecoverWithEngine(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider TokenCaptchaURLProvider) (string, string, error) {
	primary := m.tokenCaptchaPrimaryFn
	if primary == nil {
		primary = m.tokenCaptchaPlaywrightRecover
	}
	currentCookies := cookieStr
	primaryURL := verificationURL
	var primaryErr error
	for refreshCount := 0; ; refreshCount++ {
		var cookies string
		// The provider may issue another browser token request. Calling it while
		// the primary engine owns this account's persistent profile deadlocks on
		// the same account lock, so URL refresh is orchestrated only after the
		// primary context has closed.
		cookies, primaryErr = primary(ctx, cookieID, currentCookies, primaryURL, headless, nil)
		if primaryErr == nil {
			return cookies, "playwright", nil
		}
		if !errors.Is(primaryErr, errTokenCaptchaURLExpired) || provider == nil || refreshCount >= 2 {
			break
		}
		freshURL, tokenOK, updated, err := provider(ctx, currentCookies)
		if err != nil {
			primaryErr = fmt.Errorf("%w且重取失败: %v", errTokenCaptchaURLExpired, err)
			break
		}
		if strings.TrimSpace(updated) != "" {
			currentCookies = updated
		}
		if tokenOK {
			return currentCookies, "playwright", nil
		}
		if strings.TrimSpace(freshURL) == "" {
			primaryErr = fmt.Errorf("%w且接口未返回新链接", errTokenCaptchaURLExpired)
			break
		}
		primaryURL = freshURL
	}
	// An expired URL cannot be repaired by replaying it through the fallback.
	if errors.Is(primaryErr, errTokenCaptchaURLExpired) {
		return "", "", primaryErr
	}

	if realMouseRequested() {
		// 参考实现仅在 Windows 桌面且 pyautogui 可用时启用；Go/Docker 构建没有物理桌面驱动，
		// 因此按其“开启但不可用则回退原逻辑”分支继续备用引擎。
		m.logger.Error("CAPTCHA_REAL_MOUSE 已开启但物理鼠标引擎不可用，回退备用滑块逻辑", "goos", runtime.GOOS)
	}
	if !drissionFallbackEnabled() {
		return "", "playwright", primaryErr
	}

	fallbackURL := verificationURL
	if provider != nil {
		freshURL, tokenOK, updated, err := provider(ctx, currentCookies)
		if err != nil {
			m.logger.Warn("备用引擎启动前重取验证链接失败，沿用原链接", "cookieID", cookieID, "err", err)
		} else {
			if strings.TrimSpace(updated) != "" {
				currentCookies = updated
			}
			if tokenOK {
				return currentCookies, "playwright", nil
			}
			if strings.TrimSpace(freshURL) != "" {
				fallbackURL = freshURL
			}
		}
	}

	fallback := m.tokenCaptchaFallbackFn
	if fallback == nil {
		fallback = m.tokenCaptchaCDPFallback
	}
	fallbackCookies, fallbackErr := fallback(
		ctx, cookieID, currentCookies, fallbackURL, drissionFallbackHeadless(), nil,
	)
	if fallbackErr == nil && hasFreshX5InCookieString(currentCookies, fallbackCookies) {
		return fallbackCookies, "drissionpage", nil
	}
	if fallbackErr == nil {
		fallbackErr = fmt.Errorf("备用滑块引擎未获取到新的 x5sec Cookie")
	}
	return "", "", fmt.Errorf("Playwright 主引擎失败: %v；备用滑块引擎失败: %w", primaryErr, fallbackErr)
}

func (m *Manager) tokenCaptchaPlaywrightRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, _ TokenCaptchaURLProvider) (string, error) {
	if strings.TrimSpace(cookieStr) == "" {
		return "", fmt.Errorf("Cookie为空，无法处理 token 风控验证")
	}
	if strings.TrimSpace(verificationURL) == "" {
		return "", fmt.Errorf("验证链接为空")
	}

	currentCookies := cookieStr
	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, currentCookies, nil, quickRenewHeadless(headless), true)
	if err != nil {
		return "", err
	}
	defer release()
	browserDeadline := time.Now().Add(tokenCaptchaBrowserTTL)

	beforeCookies, err := bctx.Cookies()
	if err != nil {
		return "", fmt.Errorf("读取 token 风控验证前 Cookie 失败: %w", err)
	}
	previousX5SecValues := x5SecValues(beforeCookies)

	page, err := bctx.NewPage()
	if err != nil {
		return "", fmt.Errorf("新建 page 失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	if time.Now().After(browserDeadline) {
		return "", fmt.Errorf("token 风控浏览器验证超过 %s", tokenCaptchaBrowserTTL)
	}
	if _, err := page.Goto(verificationURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(10000),
	}); err != nil {
		m.logger.Warn("token风控验证页面访问异常", "cookieID", cookieID, "err", err)
	}
	time.Sleep(secondsDuration(randomFloat(0.3, 0.8)))
	_ = page.Mouse().Move(640, 360)
	time.Sleep(secondsDuration(randomFloat(0.02, 0.05)))
	_ = page.Mouse().Wheel(0, float64(200+rng.Intn(301)))
	time.Sleep(secondsDuration(randomFloat(0.02, 0.05)))
	if title, titleErr := page.Title(); titleErr == nil {
		m.logger.Info("token风控验证页面已加载", "cookieID", cookieID, "title", title)
	}
	content, _ := page.Content()
	if captchaURLExpired(content) {
		return "", errTokenCaptchaURLExpired
	}
	if strings.Contains(content, "STATUS_BREAKPOINT") || strings.Contains(content, "崩溃") {
		return "", fmt.Errorf("token 风控验证页面崩溃")
	}

	scratch := isScratchCaptcha(content)
	if err := solveSliderStrict(page, scratch, m.logger, previousX5SecValues, browserDeadline); err != nil {
		return "", err
	}

	x5, err := waitForFreshX5SecCookie(ctx, bctx, previousX5SecValues, tokenCaptchaCookieWait, tokenCaptchaCookiePoll)
	if err != nil {
		return "", err
	}
	merged := parseCookieStr(currentCookies)
	for k, v := range x5 {
		merged[k] = v
	}
	m.logger.Info("token风控验证成功", "cookieID", cookieID, "x5_cookie_count", len(x5))
	return cookieMarshal(merged), nil
}

func realMouseRequested() bool {
	value := strings.TrimSpace(os.Getenv("CAPTCHA_REAL_MOUSE"))
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func drissionFallbackEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CAPTCHA_DRISSIONPAGE_FALLBACK_ENABLED"))
	if value == "" {
		return true
	}
	parsed, err := strconv.ParseBool(value)
	return err != nil || parsed
}

func drissionFallbackHeadless() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("BROWSER_HEADLESS")), "true") {
		return true
	}
	value := strings.TrimSpace(os.Getenv("CAPTCHA_DRISSIONPAGE_HEADLESS"))
	if value == "" {
		return true
	}
	parsed, err := strconv.ParseBool(value)
	return err != nil || parsed
}

func drissionFallbackTimeout() time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CAPTCHA_DRISSIONPAGE_TIMEOUT")))
	if err != nil || value <= 0 {
		value = 25
	}
	return time.Duration(value) * time.Second
}

func hasFreshX5InCookieString(before, after string) bool {
	oldValues := make(map[string]struct{})
	for name, value := range parseCookieStr(before) {
		if strings.EqualFold(name, "x5sec") && strings.TrimSpace(value) != "" {
			oldValues[value] = struct{}{}
		}
	}
	for name, value := range parseCookieStr(after) {
		lower := strings.ToLower(name)
		if !(strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec")) || strings.TrimSpace(value) == "" {
			continue
		}
		if strings.EqualFold(name, "x5sec") {
			if _, stale := oldValues[value]; stale {
				continue
			}
		}
		return true
	}
	return false
}

func captchaURLExpired(content string) bool {
	return strings.Contains(content, "抱歉，页面访问出现了问题")
}

func waitForFreshX5SecCookie(ctx context.Context, reader browserCookieReader, previousValues map[string]struct{}, timeout, pollInterval time.Duration) (map[string]string, error) {
	if timeout <= 0 || pollInterval <= 0 {
		return nil, fmt.Errorf("等待 x5sec Cookie 的超时和轮询间隔必须大于 0")
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("等待新的 x5sec Cookie: %w", err)
		}
		all, err := reader.Cookies()
		if err != nil {
			return nil, fmt.Errorf("提取 token 风控 Cookie 失败: %w", err)
		}
		x5, fresh := freshX5Cookies(all, previousValues)
		if fresh {
			return x5, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("等待新的 x5sec Cookie: %w", ctx.Err())
		case <-timer.C:
			return nil, fmt.Errorf("滑块验证完成后 %s 内未获取到新的 x5sec Cookie", timeout)
		case <-ticker.C:
		}
	}
}

func freshX5Cookies(cookies []playwright.Cookie, previousValues map[string]struct{}) (map[string]string, bool) {
	x5 := make(map[string]string)
	var freshName, freshValue string
	for _, cookie := range cookies {
		name := strings.ToLower(cookie.Name)
		if strings.HasPrefix(name, "x5") || strings.Contains(name, "x5sec") {
			x5[cookie.Name] = cookie.Value
		}
		if name == "x5sec" && strings.TrimSpace(cookie.Value) != "" {
			if _, stale := previousValues[cookie.Value]; !stale {
				freshName = cookie.Name
				freshValue = cookie.Value
			}
		}
	}
	if freshValue == "" {
		return x5, false
	}
	// Cookies with the same name may coexist on several domains. Ensure the
	// value merged into the HTTP Cookie string is the newly issued one.
	x5[freshName] = freshValue
	return x5, true
}

func x5SecValues(cookies []playwright.Cookie) map[string]struct{} {
	values := make(map[string]struct{})
	for _, cookie := range cookies {
		if strings.EqualFold(cookie.Name, "x5sec") && strings.TrimSpace(cookie.Value) != "" {
			values[cookie.Value] = struct{}{}
		}
	}
	return values
}
