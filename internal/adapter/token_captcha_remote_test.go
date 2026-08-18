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

// TestRemoteCaptchaSuccessWorksWithoutLocalBrowser 封装TestRemoteCaptchaSuccessWorksWithoutLocal浏览器业务协调。
func TestRemoteCaptchaSuccessWorksWithoutLocalBrowser(t *testing.T) {
	// payload 用于本次流程后续判断的请求载荷
	var payload map[string]any
	// remote 用于本次流程后续判断的remote
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if // err 用于本次流程后续判断的err
		err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		_, _ = io.WriteString(w, `{"success":true,"data":{"cookies":{"x5sec":"fresh","other":"must-not-merge"}}}`)
	}))
	defer remote.Close()

	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.Settings.SetMany(ctx, map[string]string{
		"captcha.remote_service_url":  remote.URL,
		"captcha.remote_secret_key":   "remote-secret",
		"captcha.remote_pass_cookies": "false",
	}); err != nil {
		t.Fatal(err)
	}

	// a 用于本次流程后续判断的a
	a := New(store, nil, nil)
	// result、ok 用于本次流程后续判断的result、ok
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
	if // exists 用于本次流程后续判断的exists
	_, exists := payload["cookies"]; exists {
		t.Fatalf("关闭传递 Cookie 时不应发送账号 Cookie: %#v", payload)
	}
	if // exists 用于本次流程后续判断的exists
	_, exists := payload["device_id"]; exists {
		t.Fatalf("关闭传递 Cookie 时不应发送设备 ID: %#v", payload)
	}
	// status、engineName 用于本次流程后续判断的status、engine名称
	var status, engineName string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx,
		`SELECT processing_status,captcha_engine FROM risk_control_logs WHERE cookie_id='cid' ORDER BY id DESC LIMIT 1`).
		Scan(&status, &engineName); err != nil {
		t.Fatal(err)
	}
	if status != "success" || engineName != "remote" {
		t.Fatalf("risk log status=%q engine=%q", status, engineName)
	}
	// auditRecords、auditErr 保存远程过滑块读取系统密钥产生的访问审计记录及查询错误。
	auditRecords, auditErr := store.SecurityAudit.ListByUser(ctx, 1, 10)
	if auditErr != nil || len(auditRecords) != 1 {
		t.Fatalf("远程过滑块密钥审计记录异常: records=%+v err=%v", auditRecords, auditErr)
	}
	if auditRecords[0].Action != "settings.use" || auditRecords[0].Resource != "captcha_remote" || len(auditRecords[0].Keys) != 1 || auditRecords[0].Keys[0] != "captcha.remote_secret_key" {
		t.Fatalf("远程过滑块密钥审计上下文异常: %+v", auditRecords[0])
	}
}

// TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost 封装TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost业务协调。
func TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls int
	// gotURLs 用于本次流程后续判断的gotURLs
	var gotURLs []string
	// gotCookies 用于本次流程后续判断的gotCookies
	var gotCookies []string
	// remote 用于本次流程后续判断的remote
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// payload 用于本次流程后续判断的请求载荷
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

	// providerCalls 用于本次流程后续判断的providerCalls
	providerCalls := 0
	// provider 用于本次流程后续判断的provider
	provider := func(_ context.Context, current string) (string, bool, string, error) {
		providerCalls++
		if current != "unb=1" {
			t.Fatalf("provider current=%q", current)
		}
		return "https://fresh.example", false, "unb=1; _m_h5_tk=fresh", nil
	}
	// cookies、handled、err 用于本次流程后续判断的cookies、handled、err
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

// TestRemoteCaptchaTokenAlreadyUsableReturnsUpdatedCookies 封装TestRemoteCaptcha令牌AlreadyUsableReturnsUpdatedCookies业务协调。
func TestRemoteCaptchaTokenAlreadyUsableReturnsUpdatedCookies(t *testing.T) {
	// remote 用于本次流程后续判断的remote
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
	}))
	defer remote.Close()
	// provider 用于本次流程后续判断的provider
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", true, "unb=1; _m_h5_tk=renewed", nil
	}
	// cookies、handled、err 用于本次流程后续判断的cookies、handled、err
	cookies, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret",
	}, "cid", "https://expired.example", "unb=1", "device", provider)
	if err != nil || !handled || !strings.Contains(cookies, "_m_h5_tk=renewed") {
		t.Fatalf("cookies=%q handled=%v err=%v", cookies, handled, err)
	}
}

// TestRemoteCaptchaExplicitFailureDoesNotFallbackToBrowser 封装TestRemoteCaptchaExplicitFailureDoesNotFallbackTo浏览器业务协调。
func TestRemoteCaptchaExplicitFailureDoesNotFallbackToBrowser(t *testing.T) {
	// remote 用于本次流程后续判断的remote
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":false}}`)
	}))
	defer remote.Close()
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	_ = store.Settings.SetMany(ctx, map[string]string{
		"captcha.remote_service_url": remote.URL, "captcha.remote_secret_key": "secret",
	})
	// fb 用于本次流程后续判断的fb
	fb := &fakeBrowser{tokenCaptchaResult: "unb=1; x5sec=local"}
	// a 用于本次流程后续判断的a
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	if // result、ok 用于本次流程后续判断的result、ok
	result, ok := a.OnTokenCaptchaVerification(ctx, "cid", "unb=1", "https://punish.example", "device"); ok || result != nil {
		t.Fatalf("明确远程失败应直接失败: result=%+v ok=%v", result, ok)
	}
	if fb.tokenCaptchaCalls != 0 {
		t.Fatalf("明确远程失败不应回退本机，browser calls=%d", fb.tokenCaptchaCalls)
	}
}

// failingRoundTripper 用于本次流程后续判断的failingRoundTripper
type failingRoundTripper struct{}

// RoundTrip 封装RoundTrip业务协调。
func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network unavailable")
}

// TestRemoteCaptchaNetworkErrorRequestsLocalFallback 封装TestRemoteCaptchaNetwork错误请求列表LocalFallback业务协调。
func TestRemoteCaptchaNetworkErrorRequestsLocalFallback(t *testing.T) {
	// client 用于本次流程后续判断的client
	client := &http.Client{Transport: failingRoundTripper{}}
	// handled、err 用于本次流程后续判断的handled、err
	_, handled, err := solveRemoteCaptcha(context.Background(), client, remoteCaptchaConfig{
		URL: "https://remote.example", Secret: "secret",
	}, "cid", "https://punish.example", "unb=1", "device", nil)
	if handled || err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

// TestRemoteCaptchaProviderErrorIsHandledFailure 封装TestRemoteCaptchaProvider错误IsHandledFailure业务协调。
func TestRemoteCaptchaProviderErrorIsHandledFailure(t *testing.T) {
	// remote 用于本次流程后续判断的remote
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
	}))
	defer remote.Close()
	// provider 用于本次流程后续判断的provider
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", false, "", errors.New("token request failed")
	}
	// handled、err 用于本次流程后续判断的handled、err
	_, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret",
	}, "cid", "https://expired.example", "unb=1", "device", provider)
	if !handled || err == nil || !strings.Contains(err.Error(), "token request failed") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}
