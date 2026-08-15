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

// TestRemoteCaptchaSuccessWorksWithoutLocalBrowser 负责TestRemoteCaptchaSuccessWorksWithoutLocal浏览器相关处理。
func TestRemoteCaptchaSuccessWorksWithoutLocalBrowser(t *testing.T) {
	// payload 保存请求载荷，供当前处理流程使用
	var payload map[string]any
	// remote 保存remote，供当前处理流程使用
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if // err 保存err，供当前处理流程使用
		err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		_, _ = io.WriteString(w, `{"success":true,"data":{"cookies":{"x5sec":"fresh","other":"must-not-merge"}}}`)
	}))
	defer remote.Close()

	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.Settings.SetMany(ctx, map[string]string{
		"captcha.remote_service_url":  remote.URL,
		"captcha.remote_secret_key":   "remote-secret",
		"captcha.remote_pass_cookies": "false",
	}); err != nil {
		t.Fatal(err)
	}

	// a 保存a，供当前处理流程使用
	a := New(store, nil, nil)
	// result、ok 保存result、ok，供当前处理流程使用
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
	if // exists 保存exists，供当前处理流程使用
	_, exists := payload["cookies"]; exists {
		t.Fatalf("关闭传递 Cookie 时不应发送账号 Cookie: %#v", payload)
	}
	if // exists 保存exists，供当前处理流程使用
	_, exists := payload["device_id"]; exists {
		t.Fatalf("关闭传递 Cookie 时不应发送设备 ID: %#v", payload)
	}
	// status、engineName 保存status、engine名称，供当前处理流程使用
	var status, engineName string
	if // err 保存err，供当前处理流程使用
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

// TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost 负责TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost相关处理。
func TestRemoteCaptchaURLExpiredRefreshesTwiceAtMost(t *testing.T) {
	// calls 保存calls，供当前处理流程使用
	var calls int
	// gotURLs 保存gotURLs，供当前处理流程使用
	var gotURLs []string
	// gotCookies 保存gotCookies，供当前处理流程使用
	var gotCookies []string
	// remote 保存remote，供当前处理流程使用
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// payload 保存请求载荷，供当前处理流程使用
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

	// providerCalls 保存providerCalls，供当前处理流程使用
	providerCalls := 0
	// provider 保存provider，供当前处理流程使用
	provider := func(_ context.Context, current string) (string, bool, string, error) {
		providerCalls++
		if current != "unb=1" {
			t.Fatalf("provider current=%q", current)
		}
		return "https://fresh.example", false, "unb=1; _m_h5_tk=fresh", nil
	}
	// cookies、handled、err 保存cookies、handled、err，供当前处理流程使用
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

// TestRemoteCaptchaTokenAlreadyUsableReturnsUpdatedCookies 负责TestRemoteCaptcha令牌AlreadyUsableReturnsUpdatedCookies相关处理。
func TestRemoteCaptchaTokenAlreadyUsableReturnsUpdatedCookies(t *testing.T) {
	// remote 保存remote，供当前处理流程使用
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
	}))
	defer remote.Close()
	// provider 保存provider，供当前处理流程使用
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", true, "unb=1; _m_h5_tk=renewed", nil
	}
	// cookies、handled、err 保存cookies、handled、err，供当前处理流程使用
	cookies, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret",
	}, "cid", "https://expired.example", "unb=1", "device", provider)
	if err != nil || !handled || !strings.Contains(cookies, "_m_h5_tk=renewed") {
		t.Fatalf("cookies=%q handled=%v err=%v", cookies, handled, err)
	}
}

// TestRemoteCaptchaExplicitFailureDoesNotFallbackToBrowser 负责TestRemoteCaptchaExplicitFailureDoesNotFallbackTo浏览器相关处理。
func TestRemoteCaptchaExplicitFailureDoesNotFallbackToBrowser(t *testing.T) {
	// remote 保存remote，供当前处理流程使用
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":false}}`)
	}))
	defer remote.Close()
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_ = store.Settings.SetMany(ctx, map[string]string{
		"captcha.remote_service_url": remote.URL, "captcha.remote_secret_key": "secret",
	})
	// fb 保存fb，供当前处理流程使用
	fb := &fakeBrowser{tokenCaptchaResult: "unb=1; x5sec=local"}
	// a 保存a，供当前处理流程使用
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	if // result、ok 保存result、ok，供当前处理流程使用
	result, ok := a.OnTokenCaptchaVerification(ctx, "cid", "unb=1", "https://punish.example", "device"); ok || result != nil {
		t.Fatalf("明确远程失败应直接失败: result=%+v ok=%v", result, ok)
	}
	if fb.tokenCaptchaCalls != 0 {
		t.Fatalf("明确远程失败不应回退本机，browser calls=%d", fb.tokenCaptchaCalls)
	}
}

// failingRoundTripper 保存failingRoundTripper，供当前处理流程使用
type failingRoundTripper struct{}

// RoundTrip 负责RoundTrip相关处理。
func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network unavailable")
}

// TestRemoteCaptchaNetworkErrorRequestsLocalFallback 负责TestRemoteCaptchaNetwork错误请求列表LocalFallback相关处理。
func TestRemoteCaptchaNetworkErrorRequestsLocalFallback(t *testing.T) {
	// client 保存client，供当前处理流程使用
	client := &http.Client{Transport: failingRoundTripper{}}
	// handled、err 保存handled、err，供当前处理流程使用
	_, handled, err := solveRemoteCaptcha(context.Background(), client, remoteCaptchaConfig{
		URL: "https://remote.example", Secret: "secret",
	}, "cid", "https://punish.example", "unb=1", "device", nil)
	if handled || err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

// TestRemoteCaptchaProviderErrorIsHandledFailure 负责TestRemoteCaptchaProvider错误IsHandledFailure相关处理。
func TestRemoteCaptchaProviderErrorIsHandledFailure(t *testing.T) {
	// remote 保存remote，供当前处理流程使用
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"data":{"url_expired":true}}`)
	}))
	defer remote.Close()
	// provider 保存provider，供当前处理流程使用
	provider := func(context.Context, string) (string, bool, string, error) {
		return "", false, "", errors.New("token request failed")
	}
	// handled、err 保存handled、err，供当前处理流程使用
	_, handled, err := solveRemoteCaptcha(context.Background(), remote.Client(), remoteCaptchaConfig{
		URL: remote.URL, Secret: "secret",
	}, "cid", "https://expired.example", "unb=1", "device", provider)
	if !handled || err == nil || !strings.Contains(err.Error(), "token request failed") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}
