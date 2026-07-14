package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteCaptchaSuccessWorksWithoutLocalBrowser(t *testing.T) {
	var payload map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		_, _ = io.WriteString(w, `{"success":true,"data":{"cookies":{"x5sec":"fresh","other":"must-not-merge"}}}`)
	}))
	defer remote.Close()

	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Settings.SetMany(ctx, map[string]string{
		"captcha.remote_service_url":  remote.URL,
		"captcha.remote_secret_key":   "remote-secret",
		"captcha.remote_pass_cookies": "false",
	}); err != nil {
		t.Fatal(err)
	}

	a := New(store, nil, nil)
	result, ok := a.OnTokenCaptchaVerification(ctx, "cid", "unb=1; old=keep", "https://punish.example", "device-private")
	if !ok || result == nil || !strings.Contains(result.UpdatedCookies, "x5sec=fresh") {
		t.Fatalf("remote result=%+v ok=%v", result, ok)
	}
	if strings.Contains(result.UpdatedCookies, "must-not-merge") {
		t.Fatalf("非 x5 Cookie 不应从远程结果合入: %q", result.UpdatedCookies)
	}
	if payload["secret_key"] != "remote-secret" || payload["account_id"] != "cid" || payload["browser_timeout"] != float64(20) {
		t.Fatalf("remote payload=%#v", payload)
	}
	if _, exists := payload["cookies"]; exists {
		t.Fatalf("关闭传递 Cookie 时不应发送账号 Cookie: %#v", payload)
	}
	if _, exists := payload["device_id"]; exists {
		t.Fatalf("关闭传递 Cookie 时不应发送设备 ID: %#v", payload)
	}
	var status, engineName string
	if err := store.DB.QueryRowContext(ctx,
		`SELECT processing_status,captcha_engine FROM risk_control_logs WHERE cookie_id='cid' ORDER BY id DESC LIMIT 1`).
		Scan(&status, &engineName); err != nil {
		t.Fatal(err)
	}
	if status != "success" || engineName != "remote" {
		t.Fatalf("risk log status=%q engine=%q", status, engineName)
	}
}

func TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost(t *testing.T) {
	var calls int
	var gotURLs []string
	var gotCookies []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotURLs = append(gotURLs, payload["url"].(string))
		gotCookies = append(gotCookies, payload["cookies"].(string))
		if calls == 1 {
			_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"data":{"cookies":{"x5sec":"new-x5"}}}`)
	}))
	defer remote.Close()

	providerCalls := 0
	provider := func(_ context.Context, current string) (string, bool, string, error) {
		providerCalls++
		if current != "unb=1" {
			t.Fatalf("provider current=%q", current)
		}
		return "https://fresh.example", false, "unb=1; _m_h5_tk=fresh", nil
	}
	cookies, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret", PassCookies: true,
	}, "cid", "https://expired.example", "unb=1", "device-1", provider)
	if err != nil || !handled || !strings.Contains(cookies, "x5sec=new-x5") {
		t.Fatalf("cookies=%q handled=%v err=%v", cookies, handled, err)
	}
	if calls != 2 || providerCalls != 1 || gotURLs[1] != "https://fresh.example" {
		t.Fatalf("calls=%d provider=%d urls=%v", calls, providerCalls, gotURLs)
	}
	if gotCookies[0] != "unb=1" || !strings.Contains(gotCookies[1], "_m_h5_tk=fresh") {
		t.Fatalf("remote cookies=%v", gotCookies)
	}
}

func TestRemoteCaptchaTokenAlreadyUsableReturnsUpdatedCookies(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
	}))
	defer remote.Close()
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", true, "unb=1; _m_h5_tk=renewed", nil
	}
	cookies, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret",
	}, "cid", "https://expired.example", "unb=1", "device", provider)
	if err != nil || !handled || !strings.Contains(cookies, "_m_h5_tk=renewed") {
		t.Fatalf("cookies=%q handled=%v err=%v", cookies, handled, err)
	}
}

func TestRemoteCaptchaExplicitFailureDoesNotFallbackToBrowser(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":false}}`)
	}))
	defer remote.Close()
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	_ = store.Settings.SetMany(ctx, map[string]string{
		"captcha.remote_service_url": remote.URL, "captcha.remote_secret_key": "secret",
	})
	fb := &fakeBrowser{tokenCaptchaResult: "unb=1; x5sec=local"}
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	if result, ok := a.OnTokenCaptchaVerification(ctx, "cid", "unb=1", "https://punish.example", "device"); ok || result != nil {
		t.Fatalf("明确远程失败应直接失败: result=%+v ok=%v", result, ok)
	}
	if fb.tokenCaptchaCalls != 0 {
		t.Fatalf("明确远程失败不应回退本机，browser calls=%d", fb.tokenCaptchaCalls)
	}
}

type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network unavailable")
}

func TestRemoteCaptchaNetworkErrorRequestsLocalFallback(t *testing.T) {
	client := &http.Client{Transport: failingRoundTripper{}}
	_, handled, err := solveRemoteCaptcha(context.Background(), client, remoteCaptchaConfig{
		URL: "https://remote.example", Secret: "secret",
	}, "cid", "https://punish.example", "unb=1", "device", nil)
	if handled || err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestRemoteCaptchaProviderErrorIsHandledFailure(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
	}))
	defer remote.Close()
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", false, "", errors.New("token request failed")
	}
	_, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret",
	}, "cid", "https://expired.example", "unb=1", "device", provider)
	if !handled || err == nil || !strings.Contains(err.Error(), "token request failed") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}
