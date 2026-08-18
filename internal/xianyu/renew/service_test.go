package renew

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// roundTripFunc 用于本次流程后续判断的roundTripFunc
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 封装RoundTrip业务协调。
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// failingReadCloser 用于本次流程后续判断的failingReadCloser
type failingReadCloser struct {
	err error
}

// Read 读取当前值。
func (r failingReadCloser) Read([]byte) (int, error) { return 0, r.err }

// Close 关闭当前值。
func (failingReadCloser) Close() error { return nil }

// futureMillis 封装futureMillis业务协调。
func futureMillis(d time.Duration) string {
	return strconv.FormatInt(time.Now().Add(d).UnixMilli(), 10)
}

// useTestDesktopFingerprint 封装useTestDesktopFingerprint业务协调。
func useTestDesktopFingerprint(t *testing.T) xianyu.BrowserFingerprint {
	t.Helper()
	// old 用于本次流程后续判断的old
	old := xianyu.CurrentBrowserFingerprint()
	// fingerprint 用于本次流程后续判断的fingerprint
	fingerprint := xianyu.BrowserFingerprint{
		UserAgent: `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/999.0.0.0 Safari/537.36`,
		SecChUA:   `"Chromium";v="999", "Google Chrome";v="999", "Not_A Brand";v="24"`,
		Platform:  "macOS",
		Mobile:    "?0",
	}
	xianyu.SetBrowserFingerprint(fingerprint)
	t.Cleanup(func() { xianyu.SetBrowserFingerprint(old) })
	return fingerprint
}

// useTestLinuxFingerprint 封装useTestLinuxFingerprint业务协调。
func useTestLinuxFingerprint(t *testing.T) xianyu.BrowserFingerprint {
	t.Helper()
	// old 用于本次流程后续判断的old
	old := xianyu.CurrentBrowserFingerprint()
	// fingerprint 用于本次流程后续判断的fingerprint
	fingerprint := xianyu.BrowserFingerprint{
		UserAgent: `Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36`,
		SecChUA:   `"Chromium";v="138", "Not_A Brand";v="24"`,
		Platform:  "Linux",
		Mobile:    "?0",
	}
	xianyu.SetBrowserFingerprint(fingerprint)
	t.Cleanup(func() { xianyu.SetBrowserFingerprint(old) })
	return fingerprint
}

// TestAutoLoginModeMatchesBrowserPlugin 封装TestAuto登录模式Matches浏览器Plugin业务协调。
func TestAutoLoginModeMatchesBrowserPlugin(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Now()
	// tests 用于本次流程后续判断的tests
	tests := []struct {
		name       string
		cookies    map[string]string
		wantMode   string
		wantReason string
	}{
		{name: "fatigue", cookies: map[string]string{"sdkSilent": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10), "havana_lgc_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantReason: "fatigue"},
		{name: "malformed sdkSilent does not cause fatigue", cookies: map[string]string{"sdkSilent": "invalid", "havana_lgc_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeHavana},
		{name: "havana", cookies: map[string]string{"havana_lgc_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeHavana},
		{name: "cookie3 backup", cookies: map[string]string{"havana_lgc_exp": strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10), "cookie3_bak_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeCookie3},
		{name: "malformed long-login expiry follows browser Invalid Date branch", cookies: map[string]string{"havana_lgc_exp": "bad", "cookie3_bak_exp": strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeHavana},
	}
	// tt 表示当前遍历过程中的tt
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// mode、reason 用于本次流程后续判断的mode、reason
			mode, reason := autoLoginMode(tt.cookies, now)
			if mode != tt.wantMode || reason != tt.wantReason {
				t.Fatalf("mode=%q reason=%q, want mode=%q reason=%q", mode, reason, tt.wantMode, tt.wantReason)
			}
		})
	}
}

// TestAutoLoginDecisionUsesFirstCookieForDuplicatePaths 封装TestAuto登录DecisionUsesFirst登录凭证ForDuplicatePaths业务协调。
func TestAutoLoginDecisionUsesFirstCookieForDuplicatePaths(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Now()
	// cookies 用于本次流程后续判断的cookies
	cookies := strings.Join([]string{
		"sdkSilent=" + strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10),
		"sdkSilent=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10),
		"havana_lgc_exp=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10),
		"havana_lgc_exp=" + strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10),
	}, "; ")
	// mode、reason 用于本次流程后续判断的mode、reason
	mode, reason := autoLoginMode(firstCookieValues(cookies), now)
	if mode != autoLoginModeHavana || reason != "" {
		t.Fatalf("mode=%q reason=%q；应采用浏览器排序后的首个同名 Cookie", mode, reason)
	}
}

// TestLongLoginSettingsMatchOfficialRequest 封装TestLong登录设置MatchOfficial请求业务协调。
func TestLongLoginSettingsMatchOfficialRequest(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if r.URL.Query().Get("fromSite") != "77" || r.URL.Query().Get("appName") != "xianyu" || r.URL.Query().Get("bizEntrance") != "web" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("Origin") != "https://www.goofish.com" || r.Header.Get("Referer") != "https://www.goofish.com/im" {
			t.Fatalf("origin/referer=%q/%q", r.Header.Get("Origin"), r.Header.Get("Referer"))
		}
		if !strings.Contains(r.Header.Get("Cookie"), "unb=1") {
			t.Fatalf("Cookie=%q", r.Header.Get("Cookie"))
		}
		if strings.Contains(r.URL.Path, "set") {
			if // err 用于本次流程后续判断的err
			err := r.ParseForm(); err != nil || r.Form.Get("status") != "0" {
				t.Fatalf("set form=%v err=%v", r.Form, err)
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "havana_lgc_exp", Value: futureMillis(24 * time.Hour), Path: "/", HttpOnly: true})
		if strings.Contains(r.URL.Path, "set") {
			_, _ = w.Write([]byte(`{"data":{"success":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer srv.Close()

	// service 用于本次流程后续判断的service
	service := Service{
		HTTPClient:            srv.Client(),
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		DocumentReferer:       "https://www.goofish.com/im",
	}
	// queried、err 用于本次流程后续判断的queried、err
	queried, err := service.QueryLongLoginSettings(context.Background(), "unb=1")
	if err != nil || !queried.CanOpenLongLogin || !queried.Enabled {
		t.Fatalf("query result=%+v err=%v", queried, err)
	}
	// set、err 用于本次流程后续判断的set、err
	set, err := service.SetLongLoginSettings(context.Background(), queried.NewCookies, true)
	if err != nil || !set.Enabled || len(set.SetCookies) != 2 || !strings.Contains(set.NewCookies, "havana_lgc_exp=") {
		t.Fatalf("set result=%+v err=%v", set, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

// TestRenewAfterSessionExpiredBypassesOnlyFatigue 封装TestRenewAfter会话ExpiredBypassesOnlyFatigue业务协调。
func TestRenewAfterSessionExpiredBypassesOnlyFatigue(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "recovered"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	// now 用于本次流程后续判断的now
	now := time.Now()
	// cookies 用于本次流程后续判断的cookies
	cookies := "unb=1; sdkSilent=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10) +
		"; havana_lgc_exp=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)
	// service 用于本次流程后续判断的service
	service := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// proactive、err 用于本次流程后续判断的proactive、err
	proactive, err := service.RenewAPIFirst(context.Background(), cookies)
	if err != nil || !proactive.Skipped || proactive.SkipReason != "fatigue" || calls.Load() != 0 {
		t.Fatalf("proactive=%+v calls=%d err=%v", proactive, calls.Load(), err)
	}
	// recovered、err 用于本次流程后续判断的recovered、err
	recovered, err := service.RenewAfterSessionExpired(context.Background(), cookies)
	if err != nil || !recovered.Success || calls.Load() != 1 || !strings.Contains(recovered.NewCookies, "havana_lgc2_77=recovered") {
		t.Fatalf("recovered=%+v calls=%d err=%v", recovered, calls.Load(), err)
	}

	// expiredLongLogin 用于本次流程后续判断的expiredLong登录
	expiredLongLogin := "unb=1; sdkSilent=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10) +
		"; havana_lgc_exp=" + strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10)
	// blocked、err 用于本次流程后续判断的blocked、err
	blocked, err := service.RenewAfterSessionExpired(context.Background(), expiredLongLogin)
	if err != nil || !blocked.Skipped || blocked.SkipReason != "long_login_expired" || calls.Load() != 1 {
		t.Fatalf("blocked=%+v calls=%d err=%v", blocked, calls.Load(), err)
	}
}

// TestLongLoginRequestKeepsResponseCookiesOnFailures 封装TestLong登录请求Keeps响应CookiesOnFailures业务协调。
func TestLongLoginRequestKeepsResponseCookiesOnFailures(t *testing.T) {
	// tests 用于本次流程后续判断的tests
	tests := []struct {
		name       string
		statusCode int
		body       io.ReadCloser
	}{
		{
			name:       "body read",
			statusCode: http.StatusOK,
			body:       failingReadCloser{err: errors.New("broken body")},
		},
		{
			name:       "business parse",
			statusCode: http.StatusOK,
			body:       io.NopCloser(strings.NewReader(`not-json`)),
		},
		{
			name:       "http status",
			statusCode: http.StatusServiceUnavailable,
			body:       io.NopCloser(strings.NewReader(`{"error":"busy"}`)),
		},
	}
	// tt 表示当前遍历过程中的tt
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// client 用于本次流程后续判断的client
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Header: http.Header{
						"Set-Cookie": {"rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly"},
					},
					Body: tt.body,
				}, nil
			})}
			// settings、err 用于本次流程后续判断的settings、err
			settings, err := (Service{HTTPClient: client}).QueryLongLoginSettings(context.Background(), "unb=1")
			if err == nil || settings == nil {
				t.Fatalf("settings=%+v err=%v", settings, err)
			}
			if len(settings.SetCookies) != 1 || !strings.Contains(settings.NewCookies, "rotated=fresh") {
				t.Fatalf("response Cookie was lost: %+v", settings)
			}
		})
	}
}

// TestSetLongLoginSettingsMergesSetAndFailedQueryCookies 封装TestSetLong登录设置MergesSetAnd失败查询Cookies业务协调。
func TestSetLongLoginSettingsMergesSetAndFailedQueryCookies(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Path, "set") {
			http.SetCookie(w, &http.Cookie{Name: "set_cookie", Value: "one", Path: "/"})
			_, _ = w.Write([]byte(`{"data":{"success":true}}`))
			return
		}
		if !strings.Contains(r.Header.Get("Cookie"), "set_cookie=one") {
			t.Fatalf("QUERY did not receive SET Cookie: %q", r.Header.Get("Cookie"))
		}
		http.SetCookie(w, &http.Cookie{Name: "query_cookie", Value: "two", Path: "/"})
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream"}`))
	}))
	defer srv.Close()

	// svc 用于本次流程后续判断的svc
	svc := Service{
		HTTPClient:            srv.Client(),
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := svc.SetLongLoginSettings(context.Background(), "unb=1", true)
	if err == nil || settings == nil {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
	if calls.Load() != 2 || len(settings.SetCookies) != 2 || !settings.Enabled {
		t.Fatalf("settings=%+v calls=%d", settings, calls.Load())
	}
	if !strings.Contains(settings.NewCookies, "set_cookie=one") || !strings.Contains(settings.NewCookies, "query_cookie=two") {
		t.Fatalf("SET/QUERY Cookie was lost: %q", settings.NewCookies)
	}
}

// TestSetLongLoginSettingsScopesCompleteJarBetweenSetAndQuery 封装TestSetLong登录设置ScopesCompleteJarBetweenSetAnd查询业务协调。
func TestSetLongLoginSettingsScopesCompleteJarBetweenSetAndQuery(t *testing.T) {
	// queryCookie 用于本次流程后续判断的查询登录凭证
	var queryCookie string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "set") {
			w.Header().Add("Set-Cookie", "set_only=hidden; Path=/ac/account/setLoginSettings.do; Secure")
			w.Header().Add("Set-Cookie", "shared=next; Path=/ac/account; Secure")
			_, _ = w.Write([]byte(`{"data":{"success":true}}`))
			return
		}
		queryCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer srv.Close()
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "shared", Value: "old", Domain: "passport.goofish.com", Path: "/ac/account", Secure: true},
		{Name: "im_only", Value: "visible", Domain: "www.goofish.com", Path: "/im", Secure: true},
	}
	// svc 用于本次流程后续判断的svc
	svc := Service{
		HTTPClient:            srv.Client(),
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := svc.SetLongLoginSettings(context.Background(), "fallback=must-not-leak", true, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(queryCookie, "set_only=hidden") || !strings.Contains(queryCookie, "shared=next") {
		t.Fatalf("QUERY Cookie 未按更新后 Jar 重新做 Path scope: %q", queryCookie)
	}
	if !settings.CookieSnapshotComplete || len(settings.CookieSnapshot) != 3 {
		t.Fatalf("最终完整 Jar 未返回: %+v", settings)
	}
	if settings.NewCookies != "im_only=visible" {
		t.Fatalf("/im canonical Cookie=%q", settings.NewCookies)
	}
}

// TestRenewAPIFirstHavanaSendsOneSilentRequest 封装TestRenewAPIFirstHavanaSendsOneSilent请求业务协调。
func TestRenewAPIFirstHavanaSendsOneSilentRequest(t *testing.T) {
	// fingerprint 用于本次流程后续判断的fingerprint
	fingerprint := useTestDesktopFingerprint(t)
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Query().Get("appEntrance") != "xianyu_sdkSilent" || r.URL.Query().Get("ltl") != "true" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("skipSessionFilter") != "" || r.URL.Query().Get("c2r") != "" {
			t.Fatalf("havana mode included cookie3 flags: %s", r.URL.RawQuery)
		}
		if // got 用于本次流程后续判断的got
		got := r.URL.Query().Get("documentReferer"); got != "https://www.goofish.com/im" {
			t.Fatalf("documentReferer=%q", got)
		}
		if // got 用于本次流程后续判断的got
		got := r.Header.Get("User-Agent"); got != fingerprint.UserAgent {
			t.Fatalf("User-Agent=%q", got)
		}
		if // got 用于本次流程后续判断的got
		got := r.Header.Get("Cookie"); !strings.Contains(got, "unb=1") {
			t.Fatalf("Cookie=%q", got)
		}
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureMillis(time.Hour)})
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()

	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, DocumentReferer: "https://www.goofish.com/im", RetryDelay: -1}
	// input 用于本次流程后续判断的input
	input := "unb=1; havana_lgc_exp=" + futureMillis(time.Hour)
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), input)
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if !res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%#v calls=%d", res, calls.Load())
	}
	if !strings.Contains(res.NewCookies, "sdkSilent=") || res.RenewMethod != "auto_login_plugin" {
		t.Fatalf("result=%#v", res)
	}
}

// TestRenewAPIFirstUsesBrowserCookieScopes 封装TestRenewAPIFirstUses浏览器登录凭证Scopes业务协调。
func TestRenewAPIFirstUsesBrowserCookieScopes(t *testing.T) {
	useTestDesktopFingerprint(t)
	// receivedCookie 用于本次流程后续判断的received登录凭证
	var receivedCookie string
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()
	// host 用于本次流程后续判断的host
	host := strings.TrimPrefix(srv.URL, "http://")
	host = strings.Split(host, ":")[0]
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", HTTPOnly: true},
		{Name: "request_only", Value: "passport", Domain: host, Path: "/"},
		{Name: "www_only", Value: "private", Domain: "www.goofish.com", Path: "/im"},
		{Name: "http_only_document", Value: "hidden", Domain: ".goofish.com", Path: "/", HTTPOnly: true},
	}
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, DocumentReferer: "https://www.goofish.com/im", RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour)+"; request_only=passport; www_only=private", snapshot)
	if err != nil || !res.Success {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !strings.Contains(receivedCookie, "request_only=passport") || strings.Contains(receivedCookie, "www_only=private") {
		t.Fatalf("passport 请求未遵守 Cookie Domain/Path: %q", receivedCookie)
	}
	if !res.CookieSnapshotComplete || res.CookieSnapshot == nil {
		t.Fatalf("authoritative snapshot was not returned: %+v", res)
	}
}

// TestRenewAPIFirstUsesHTTPOnlyLongLoginCookieForDecision 封装TestRenewAPIFirstUsesHTTPOnlyLong登录登录凭证ForDecision业务协调。
func TestRenewAPIFirstUsesHTTPOnlyLongLoginCookieForDecision(t *testing.T) {
	useTestDesktopFingerprint(t)
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{{
		Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", HTTPOnly: true,
	}}
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "", snapshot)
	if err != nil || res == nil || !res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", res, calls.Load(), err)
	}
}

// TestRenewAPIFirstRunsWithLinuxChromiumFingerprint 封装TestRenewAPIFirst运行记录WithLinuxChromiumFingerprint业务协调。
func TestRenewAPIFirstRunsWithLinuxChromiumFingerprint(t *testing.T) {
	// fingerprint 用于本次流程后续判断的fingerprint
	fingerprint := useTestLinuxFingerprint(t)
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if // got 用于本次流程后续判断的got
		got := r.Header.Get("User-Agent"); got != fingerprint.UserAgent {
			t.Fatalf("User-Agent=%q", got)
		}
		if // got 用于本次流程后续判断的got
		got := r.Header.Get("sec-ch-ua-platform"); got != `"Linux"` {
			t.Fatalf("sec-ch-ua-platform=%q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil || res == nil || !res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", res, calls.Load(), err)
	}
}

// TestRenewAPIFirstUsesTopSiteAndAppliesPartitionedSetCookie 封装TestRenewAPIFirstUsesTopSiteAndAppliesPartitionedSet登录凭证业务协调。
func TestRenewAPIFirstUsesTopSiteAndAppliesPartitionedSetCookie(t *testing.T) {
	useTestDesktopFingerprint(t)
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", Secure: true, PartitionKey: goofishTopSite},
		{Name: "passport_partitioned", Value: "right", Domain: "passport.goofish.com", Path: "/", Secure: true, PartitionKey: goofishTopSite},
		{Name: "wrong_partition", Value: "hidden", Domain: "passport.goofish.com", Path: "/", Secure: true, PartitionKey: "https://example.com"},
	}
	// client 用于本次流程后续判断的client
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// cookies 用于本次流程后续判断的cookies
		cookies := req.Header.Get("Cookie")
		if !strings.Contains(cookies, "passport_partitioned=right") || strings.Contains(cookies, "wrong_partition=hidden") {
			t.Fatalf("partitioned request Cookie=%q", cookies)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Set-Cookie": {"rotated=fresh; Domain=.goofish.com; Path=/; Secure; Partitioned"},
			},
			Body: io.NopCloser(strings.NewReader(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`)),
		}, nil
	})}
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: client, RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "", snapshot)
	if err != nil || !res.Success {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !res.CookieSnapshotComplete || !strings.Contains(res.NewCookies, "rotated=fresh") {
		t.Fatalf("authoritative renewal result=%+v", res)
	}
	// found 用于本次流程后续判断的found
	found := false
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range res.CookieSnapshot {
		if cookie.Name == "rotated" && cookie.Value == "fresh" && cookie.PartitionKey == goofishTopSite {
			found = true
		}
	}
	if !found || len(res.UpdatedCookieNames) == 0 {
		t.Fatalf("partitioned Set-Cookie was not applied exactly: %+v", res)
	}
}

// TestRenewAPIFirstKeepsSetCookieWhenBodyReadFails 封装TestRenewAPIFirstKeepsSet登录凭证When请求体ReadFails业务协调。
func TestRenewAPIFirstKeepsSetCookieWhenBodyReadFails(t *testing.T) {
	useTestDesktopFingerprint(t)
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", Secure: true},
	}
	// client 用于本次流程后续判断的client
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Set-Cookie": {"rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly"},
			},
			Body: failingReadCloser{err: errors.New("broken response body")},
		}, nil
	})}
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: client, RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "", snapshot)
	if err == nil || res == nil {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !res.CookieSnapshotComplete || res.RequestCount != 1 || len(res.SetCookies) != 1 || !strings.Contains(res.NewCookies, "rotated=fresh") {
		t.Fatalf("response Cookie was lost after body error: %+v", res)
	}
}

// TestRenewAPIFirstTreatsNonNilEmptySnapshotAsAuthoritative 封装TestRenewAPIFirstTreatsNonNilEmptySnapshotAsAuthoritative业务协调。
func TestRenewAPIFirstTreatsNonNilEmptySnapshotAsAuthoritative(t *testing.T) {
	useTestDesktopFingerprint(t)
	// emptySnapshot 用于本次流程后续判断的emptySnapshot
	emptySnapshot := make([]cookierefresh.BrowserCookie, 0)
	// res、err 用于本次流程后续判断的res、err
	res, err := (Service{RetryDelay: -1}).RenewAPIFirst(context.Background(), "", emptySnapshot)
	if err != nil || !res.Skipped || res.SkipReason != "long_login_expired" {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !res.CookieSnapshotComplete || res.CookieSnapshot == nil || len(res.CookieSnapshot) != 0 || res.RenewMethod != "auto_login_plugin" {
		t.Fatalf("empty authoritative snapshot was downgraded: %+v", res)
	}
}

// TestRenewAPIFirstCookie3UsesBackupFlags 封装TestRenewAPIFirstCookie3UsesBackupFlags业务协调。
func TestRenewAPIFirstCookie3UsesBackupFlags(t *testing.T) {
	useTestDesktopFingerprint(t)
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("skipSessionFilter") != "true" || r.URL.Query().Get("c2r") != "true" || r.URL.Query().Get("ltl") != "" {
			t.Fatalf("cookie3 query=%s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "cookie3_bak_exp="+futureMillis(time.Hour))
	if err != nil || !res.Success || res.RequestCount != 1 {
		t.Fatalf("result=%#v err=%v", res, err)
	}
}

// TestRenewAPIFirstSkipsFatigueAndExpiredCookies 封装TestRenewAPIFirstSkipsFatigueAndExpiredCookies业务协调。
func TestRenewAPIFirstSkipsFatigueAndExpiredCookies(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}

	// input 表示当前遍历过程中的input
	for _, input := range []string{
		"sdkSilent=" + futureMillis(time.Hour) + "; havana_lgc_exp=" + futureMillis(2*time.Hour),
		"havana_lgc_exp=1; cookie3_bak_exp=1",
	} {
		// res、err 用于本次流程后续判断的res、err
		res, err := svc.RenewAPIFirst(context.Background(), input)
		if err != nil || !res.Skipped || res.Success || res.RequestCount != 0 {
			t.Fatalf("input=%q result=%#v err=%v", input, res, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("skipped renewal made %d requests", calls.Load())
	}
}

// TestRenewAPIFirstDoesNotRetryOrEscalateFailure 封装TestRenewAPIFirstDoesNot重试OrEscalateFailure业务协调。
func TestRenewAPIFirstDoesNotRetryOrEscalateFailure(t *testing.T) {
	useTestDesktopFingerprint(t)
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"content":{"success":false}}`))
	}))
	defer srv.Close()
	// svc 用于本次流程后续判断的svc
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%#v calls=%d", res, calls.Load())
	}
}

// TestMergeSetCookies 封装TestMergeSetCookies业务协调。
func TestMergeSetCookies(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := MergeSetCookies("unb=1; old=a", []string{"old=b; Path=/; HttpOnly", "new=c; Domain=.goofish.com; Path=/", "bad-cookie"})
	if !strings.Contains(got, "old=b") || !strings.Contains(got, "new=c") || !strings.Contains(got, "unb=1") {
		t.Fatalf("MergeSetCookies=%q", got)
	}
	if // changed 用于本次流程后续判断的changed
	changed := strings.Join(ChangedCookieNames("unb=1; old=a", got), ","); changed != "new,old" {
		t.Fatalf("ChangedCookieNames=%s", changed)
	}
}

// TestMergeSetCookiesAppliesServerDeletion 封装TestMergeSetCookiesAppliesServerDeletion业务协调。
func TestMergeSetCookiesAppliesServerDeletion(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := MergeSetCookies("unb=1; stale=a; expired=b", []string{
		"stale=; Max-Age=0; Path=/",
		"expired=; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	})
	if strings.Contains(got, "stale=") || strings.Contains(got, "expired=") {
		t.Fatalf("deleted cookies survived: %q", got)
	}
	if // changed 用于本次流程后续判断的changed
	changed := strings.Join(ChangedCookieNames("unb=1; stale=a; expired=b", got), ","); changed != "expired,stale" {
		t.Fatalf("ChangedCookieNames=%s", changed)
	}
}

// TestMergeSetCookiesMaxAgeOverridesPastExpires 封装TestMergeSetCookiesMaxAgeOverridesPastExpires业务协调。
func TestMergeSetCookiesMaxAgeOverridesPastExpires(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := MergeSetCookies("session=old", []string{
		"session=fresh; Max-Age=3600; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	})
	if !strings.Contains(got, "session=fresh") {
		t.Fatalf("positive Max-Age must override past Expires: %q", got)
	}
}

// TestRenewBusinessOKMatchesOfficialPlugin 封装TestRenewBusinessOKMatchesOfficialPlugin业务协调。
func TestRenewBusinessOKMatchesOfficialPlugin(t *testing.T) {
	if renewBusinessOK([]byte(`{"content":{"success":true}}`)) {
		t.Fatal("official plugin requires processFinished=true and resultCode=100")
	}
	if !renewBusinessOK([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`)) {
		t.Fatal("official success payload was rejected")
	}
}

// TestRenewAPIFirstReturnsAtPromiseTimeoutAndKeepsLateCookies 封装TestRenewAPIFirstReturnsAtPromiseTimeoutAndKeepsLateCookies业务协调。
func TestRenewAPIFirstReturnsAtPromiseTimeoutAndKeepsLateCookies(t *testing.T) {
	useTestDesktopFingerprint(t)
	// completed 用于本次流程后续判断的completed
	var completed atomic.Bool
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureMillis(time.Hour), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
		completed.Store(true)
	}))
	defer srv.Close()
	// svc 用于本次流程后续判断的svc
	svc := Service{
		HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1,
		PromiseTimeout: 20 * time.Millisecond,
	}
	// started 用于本次流程后续判断的started
	started := time.Now()
	// res、err 用于本次流程后续判断的res、err
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil || res == nil || !res.HasPending() {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if // elapsed 用于本次流程后续判断的elapsed
	elapsed := time.Since(started); elapsed >= 70*time.Millisecond {
		t.Fatalf("外层 Promise 没有按时返回: %s", elapsed)
	}
	if completed.Load() || len(res.SetCookies) != 0 || res.Success || res.NeedPasswordLogin {
		t.Fatalf("超时瞬间不应伪造底层结果或要求重新登录: %+v completed=%v", res, completed.Load())
	}
	// waitCtx、cancel 用于本次流程后续判断的waitCtx、cancel
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// late、lateErr 用于本次流程后续判断的late、lateErr
	late, lateErr := res.AwaitPending(waitCtx)
	if lateErr != nil || late == nil || len(late.SetCookies) != 1 || !strings.Contains(late.NewCookies, "sdkSilent=") {
		t.Fatalf("迟到响应 Cookie 丢失: result=%+v err=%v", late, lateErr)
	}
	if !completed.Load() || !late.Success || late.NeedPasswordLogin {
		t.Fatalf("底层请求完成后应返回真实业务终态: %+v completed=%v", late, completed.Load())
	}
}

// TestRebaseResponseCookiesUsesLatestAuthoritativeJar 封装TestRebase响应CookiesUsesLatestAuthoritativeJar业务协调。
func TestRebaseResponseCookiesUsesLatestAuthoritativeJar(t *testing.T) {
	// current 用于本次流程后续判断的current
	current := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "concurrent", Value: "kept", Domain: ".goofish.com", Path: "/", Secure: true},
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"keep"}`, current)
	// late 用于本次流程后续判断的late
	late := &Result{
		SetCookies:        []string{"sdkSilent=9999999999999; Domain=goofish.com; Path=/; Secure; HttpOnly"},
		responseCookieURL: SilentHasLoginURL,
	}
	// value、updatedMetadata、changed 用于本次流程后续判断的value、updatedMetadata、changed
	value, updatedMetadata, changed := RebaseResponseCookies("unb=1; concurrent=kept", metadata, late)
	if !changed || !strings.Contains(value, "concurrent=kept") || !strings.Contains(value, "sdkSilent=9999999999999") {
		t.Fatalf("迟到响应覆盖了并发 Cookie: value=%q changed=%v", value, changed)
	}
	// snapshot、complete 用于本次流程后续判断的snapshot、complete
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(updatedMetadata)
	if !complete {
		t.Fatalf("权威快照被降级: %s", updatedMetadata)
	}
	// found 用于本次流程后续判断的found
	var found bool
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range snapshot {
		if cookie.Name == "sdkSilent" && cookie.HTTPOnly && cookie.Domain == ".goofish.com" {
			found = true
		}
	}
	if !found || !strings.Contains(updatedMetadata, `"note":"keep"`) {
		t.Fatalf("迟到 Cookie 属性或其他 metadata 丢失: %+v metadata=%s", snapshot, updatedMetadata)
	}
}

// TestRenewBodyLimit 封装TestRenew请求体上限业务协调。
func TestRenewBodyLimit(t *testing.T) {
	// err 用于本次流程后续判断的err
	_, err := readRenewBody(strings.NewReader(strings.Repeat("x", maxRenewBodyBytes+1)))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxRenewBodyBytes>>20)) {
		t.Fatalf("err=%v", err)
	}
}
