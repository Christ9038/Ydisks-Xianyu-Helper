package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenCaptchaEngineOrderPrimarySuccess(t *testing.T) {
	fallbackCalls := 0
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "unb=1; x5sec=fresh", nil
		},
		tokenCaptchaFallbackFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			fallbackCalls++
			return "", errors.New("must not run")
		},
	}
	cookies, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", "https://captcha", true, nil)
	if err != nil || engine != "playwright" || !strings.Contains(cookies, "x5sec=fresh") || fallbackCalls != 0 {
		t.Fatalf("cookies=%q engine=%q fallback=%d err=%v", cookies, engine, fallbackCalls, err)
	}
}

func TestTokenCaptchaEngineOrderFallbackUsesFreshURLAndCookies(t *testing.T) {
	t.Setenv("CAPTCHA_DRISSIONPAGE_FALLBACK_ENABLED", "true")
	var gotURL, gotCookies string
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", errors.New("primary failed")
		},
		tokenCaptchaFallbackFn: func(_ context.Context, _ string, cookies, url string, headless bool, _ TokenCaptchaURLProvider) (string, error) {
			gotURL, gotCookies = url, cookies
			if !headless {
				t.Fatal("备用引擎默认应无头")
			}
			return cookies + "; x5sec=fallback-new", nil
		},
	}
	providerCalls := 0
	provider := func(context.Context, string) (string, bool, string, error) {
		providerCalls++
		return "https://fresh.example", false, "unb=1; _m_h5_tk=fresh", nil
	}
	cookies, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1; x5sec=old", "https://old.example", false, provider)
	if err != nil || engine != "drissionpage" || !strings.Contains(cookies, "x5sec=fallback-new") {
		t.Fatalf("cookies=%q engine=%q err=%v", cookies, engine, err)
	}
	if providerCalls != 1 || gotURL != "https://fresh.example" || !strings.Contains(gotCookies, "_m_h5_tk=fresh") {
		t.Fatalf("provider=%d url=%q cookies=%q", providerCalls, gotURL, gotCookies)
	}
}

func TestTokenCaptchaExpiredURLSkipsFallback(t *testing.T) {
	fallbackCalls := 0
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", fmtTokenCaptchaExpired()
		},
		tokenCaptchaFallbackFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			fallbackCalls++
			return "unb=1; x5sec=new", nil
		},
	}
	_, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", "https://expired", true, nil)
	if !errors.Is(err, errTokenCaptchaURLExpired) || engine != "" || fallbackCalls != 0 {
		t.Fatalf("engine=%q fallback=%d err=%v", engine, fallbackCalls, err)
	}
}

func TestTokenCaptchaDirectErrorPageSkipsFallbackAndKeepsManualURL(t *testing.T) {
	fallbackCalls := 0
	verificationURL := "https://h5api.m.goofish.com/punish?x5secdata=full-sensitive-value&action=captcha"
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", fmt.Errorf("%w: Oops... something's wrong", errTokenCaptchaDirectPageError)
		},
		tokenCaptchaFallbackFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			fallbackCalls++
			return "", errors.New("must not run")
		},
	}
	_, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", verificationURL, true, nil)
	if !errors.Is(err, errTokenCaptchaDirectPageError) || engine != "" || fallbackCalls != 0 {
		t.Fatalf("engine=%q fallback=%d err=%v", engine, fallbackCalls, err)
	}
	if got := TokenCaptchaManualVerificationURL(err); got != verificationURL || !strings.Contains(err.Error(), verificationURL) {
		t.Fatalf("manual URL=%q err=%v", got, err)
	}
}

func TestTokenCaptchaSliderFailureReportsLatestFallbackURL(t *testing.T) {
	t.Setenv("CAPTCHA_DRISSIONPAGE_FALLBACK_ENABLED", "true")
	freshURL := "https://h5api.m.goofish.com/punish?x5secdata=latest-full-value&action=captcha"
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", errors.New("slider failed three times")
		},
		tokenCaptchaFallbackFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", errors.New("fallback slider failed three times")
		},
	}
	provider := func(context.Context, string) (string, bool, string, error) {
		return freshURL, false, "unb=1; _m_h5_tk=fresh", nil
	}
	_, _, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", "https://old.example/punish", true, provider)
	if err == nil || TokenCaptchaManualVerificationURL(err) != freshURL || !strings.Contains(err.Error(), freshURL) {
		t.Fatalf("err=%v manual=%q", err, TokenCaptchaManualVerificationURL(err))
	}
}

func TestTokenCaptchaExpiredURLRefreshesAfterPrimaryCloses(t *testing.T) {
	primaryCalls := 0
	providerCalls := 0
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(_ context.Context, _ string, cookies, url string, _ bool, provider TokenCaptchaURLProvider) (string, error) {
			if provider != nil {
				t.Fatal("primary must not call provider while holding the persistent profile lock")
			}
			primaryCalls++
			if primaryCalls == 1 {
				return "", errTokenCaptchaURLExpired
			}
			if url != "https://fresh.example" || !strings.Contains(cookies, "_m_h5_tk=fresh") {
				t.Fatalf("url=%q cookies=%q", url, cookies)
			}
			return cookies + "; x5sec=new", nil
		},
	}
	provider := func(context.Context, string) (string, bool, string, error) {
		providerCalls++
		return "https://fresh.example", false, "unb=1; _m_h5_tk=fresh", nil
	}
	cookies, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", "https://expired", true, provider)
	if err != nil || engine != "playwright" || primaryCalls != 2 || providerCalls != 1 || !strings.Contains(cookies, "x5sec=new") {
		t.Fatalf("cookies=%q engine=%q primary=%d provider=%d err=%v", cookies, engine, primaryCalls, providerCalls, err)
	}
}

func fmtTokenCaptchaExpired() error {
	return errors.Join(errTokenCaptchaURLExpired, errors.New("cannot refresh"))
}

func TestTokenCaptchaFallbackCanBeDisabled(t *testing.T) {
	t.Setenv("CAPTCHA_DRISSIONPAGE_FALLBACK_ENABLED", "false")
	fallbackCalls := 0
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", errors.New("primary failed")
		},
		tokenCaptchaFallbackFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			fallbackCalls++
			return "", nil
		},
	}
	_, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", "https://captcha", true, nil)
	if err == nil || engine != "playwright" || fallbackCalls != 0 {
		t.Fatalf("engine=%q fallback=%d err=%v", engine, fallbackCalls, err)
	}
}

func TestTokenCaptchaProviderDetectsTokenBeforeFallback(t *testing.T) {
	fallbackCalls := 0
	m := &Manager{
		logger: slog.Default(),
		tokenCaptchaPrimaryFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			return "", errors.New("primary failed")
		},
		tokenCaptchaFallbackFn: func(context.Context, string, string, string, bool, TokenCaptchaURLProvider) (string, error) {
			fallbackCalls++
			return "", nil
		},
	}
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", true, "unb=1; _m_h5_tk=renewed", nil
	}
	cookies, engine, err := m.TokenCaptchaRecoverWithEngine(context.Background(), "cid", "unb=1", "https://captcha", true, provider)
	if err != nil || engine != "playwright" || fallbackCalls != 0 || !strings.Contains(cookies, "_m_h5_tk=renewed") {
		t.Fatalf("cookies=%q engine=%q fallback=%d err=%v", cookies, engine, fallbackCalls, err)
	}
}

func TestFallbackConfigurationMatchesReferenceDefaults(t *testing.T) {
	t.Setenv("CAPTCHA_DRISSIONPAGE_FALLBACK_ENABLED", "")
	t.Setenv("CAPTCHA_DRISSIONPAGE_HEADLESS", "")
	t.Setenv("CAPTCHA_DRISSIONPAGE_TIMEOUT", "")
	t.Setenv("BROWSER_HEADLESS", "")
	if !drissionFallbackEnabled() || !drissionFallbackHeadless() || drissionFallbackTimeout() != 25*time.Second {
		t.Fatalf("fallback defaults enabled=%v headless=%v timeout=%s", drissionFallbackEnabled(), drissionFallbackHeadless(), drissionFallbackTimeout())
	}
	t.Setenv("CAPTCHA_DRISSIONPAGE_TIMEOUT", "7")
	t.Setenv("CAPTCHA_DRISSIONPAGE_HEADLESS", "false")
	if drissionFallbackHeadless() || drissionFallbackTimeout() != 7*time.Second {
		t.Fatalf("fallback overrides headless=%v timeout=%s", drissionFallbackHeadless(), drissionFallbackTimeout())
	}
	t.Setenv("BROWSER_HEADLESS", "true")
	if !drissionFallbackHeadless() {
		t.Fatal("Docker BROWSER_HEADLESS=true 必须强制备用引擎无头")
	}
}

func TestFallbackTracksAreResampledAndBounded(t *testing.T) {
	for _, points := range []int{30, 60, 90, 150} {
		tracks := generateFallbackTracks(320, points)
		if len(tracks) != points {
			t.Fatalf("points=%d got=%d", points, len(tracks))
		}
		if tracks[0] < -1 || tracks[len(tracks)-1] < 300 || tracks[len(tracks)-1] > 345 {
			t.Fatalf("points=%d first=%.1f last=%.1f", points, tracks[0], tracks[len(tracks)-1])
		}
		for i := 1; i < len(tracks); i++ {
			if tracks[i] < tracks[i-1]-10 {
				t.Fatalf("轨迹大幅回退: points=%d index=%d %.1f -> %.1f", points, i, tracks[i-1], tracks[i])
			}
		}
	}
}

func TestFallbackDistanceMatchesReferenceRange(t *testing.T) {
	for i := 0; i < 100; i++ {
		distance := fallbackDistanceFromTrackWidth(420)
		if distance < 274 || distance > 398 {
			t.Fatalf("420px 轨道距离 %.1f 超出 70%%-90%% 加正负20px范围", distance)
		}
	}
	if got := fallbackDistanceFromTrackWidth(100); got != 200 {
		t.Fatalf("短轨道距离应限制为 200，got %.1f", got)
	}
	for i := 0; i < 20; i++ {
		if got := fallbackDistanceFromTrackWidth(1000); got > 600 {
			t.Fatalf("长轨道距离应限制为 600，got %.1f", got)
		}
	}
}

func TestFallbackUsableDistanceMatchesStandardNCDOM(t *testing.T) {
	if got := fallbackUsableDistance(300, 42); got != 258 {
		t.Fatalf("标准 NC DOM 可用距离=%.1f want 258", got)
	}
	for _, invalid := range [][2]float64{{0, 42}, {300, 0}, {42, 42}, {40, 42}} {
		if got := fallbackUsableDistance(invalid[0], invalid[1]); got != 0 {
			t.Fatalf("无效尺寸 %v 不应产生距离，got %.1f", invalid, got)
		}
	}
}

func TestFallbackChromiumArgsUsePersistentCDPProfile(t *testing.T) {
	userAgent := "Mozilla/5.0 HeadlessChrome/149.0.7827.55 Safari/537.36"
	args := strings.Join(fallbackChromiumArgs("/tmp/profile", true, &userAgent), " ")
	for _, want := range []string{"--headless=new", "--user-data-dir=/tmp/profile", "--remote-debugging-port=0", "--window-size=1920,1080", "--disable-blink-features=AutomationControlled", "--user-agent=Mozilla/5.0 Chrome/149.0.7827.55 Safari/537.36"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args 缺少 %q: %s", want, args)
		}
	}
	if strings.Contains(args, "HeadlessChrome") {
		t.Fatalf("备用 Chromium 参数仍暴露 HeadlessChrome: %s", args)
	}
}

func TestWaitForDevToolsEndpoint(t *testing.T) {
	dir := t.TempDir()
	processDone := make(chan error)
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("9223\n/devtools/browser/id\n"), 0o600)
	}()
	endpoint, err := waitForDevToolsEndpoint(context.Background(), dir, processDone, time.Second)
	if err != nil || endpoint != "http://127.0.0.1:9223" {
		t.Fatalf("endpoint=%q err=%v", endpoint, err)
	}
}

func TestHasFreshX5InCookieString(t *testing.T) {
	if hasFreshX5InCookieString("x5sec=old", "x5sec=old") {
		t.Fatal("旧 x5sec 不应被备用引擎当作成功")
	}
	if !hasFreshX5InCookieString("x5sec=old", "x5sec=new; x5sectag=tag") {
		t.Fatal("新的 x5sec 应被识别")
	}
}
