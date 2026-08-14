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

// roundTripFunc 保存roundTripFunc，供当前处理流程使用
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 负责RoundTrip相关处理。
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// failingReadCloser 保存failingReadCloser，供当前处理流程使用
type failingReadCloser struct {
	err error
}

// Read 读取当前值。
func (r failingReadCloser) Read([]byte) (int, error) { return 0, r.err }

// Close 关闭当前值。
func (failingReadCloser) Close() error { return nil }

// futureMillis 负责futureMillis相关处理。
func futureMillis(d time.Duration) string {
	return strconv.FormatInt(time.Now().Add(d).UnixMilli(), 10)
}

// useTestDesktopFingerprint 负责useTestDesktopFingerprint相关处理。
func useTestDesktopFingerprint(t *testing.T) xianyu.BrowserFingerprint {
	t.Helper()
	// old 保存old，供当前处理流程使用
	old := xianyu.CurrentBrowserFingerprint()
	// fingerprint 保存fingerprint，供当前处理流程使用
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

// useTestLinuxFingerprint 负责useTestLinuxFingerprint相关处理。
func useTestLinuxFingerprint(t *testing.T) xianyu.BrowserFingerprint {
	t.Helper()
	// old 保存old，供当前处理流程使用
	old := xianyu.CurrentBrowserFingerprint()
	// fingerprint 保存fingerprint，供当前处理流程使用
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

// TestAutoLoginModeMatchesBrowserPlugin 负责TestAuto登录模式Matches浏览器Plugin相关处理。
func TestAutoLoginModeMatchesBrowserPlugin(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := time.Now()
	// tests 保存tests，供当前处理流程使用
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
			// mode、reason 保存mode、reason，供当前处理流程使用
			mode, reason := autoLoginMode(tt.cookies, now)
			if mode != tt.wantMode || reason != tt.wantReason {
				t.Fatalf("mode=%q reason=%q, want mode=%q reason=%q", mode, reason, tt.wantMode, tt.wantReason)
			}
		})
	}
}

// TestAutoLoginDecisionUsesFirstCookieForDuplicatePaths 负责TestAuto登录DecisionUsesFirst登录凭证ForDuplicatePaths相关处理。
func TestAutoLoginDecisionUsesFirstCookieForDuplicatePaths(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := time.Now()
	// cookies 保存cookies，供当前处理流程使用
	cookies := strings.Join([]string{
		"sdkSilent=" + strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10),
		"sdkSilent=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10),
		"havana_lgc_exp=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10),
		"havana_lgc_exp=" + strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10),
	}, "; ")
	// mode、reason 保存mode、reason，供当前处理流程使用
	mode, reason := autoLoginMode(firstCookieValues(cookies), now)
	if mode != autoLoginModeHavana || reason != "" {
		t.Fatalf("mode=%q reason=%q；应采用浏览器排序后的首个同名 Cookie", mode, reason)
	}
}

// TestLongLoginSettingsMatchOfficialRequest 负责TestLong登录设置MatchOfficial请求相关处理。
func TestLongLoginSettingsMatchOfficialRequest(t *testing.T) {
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
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
			if // err 保存err，供当前处理流程使用
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

	// service 保存service，供当前处理流程使用
	service := Service{
		HTTPClient:            srv.Client(),
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		DocumentReferer:       "https://www.goofish.com/im",
	}
	// queried、err 保存queried、err，供当前处理流程使用
	queried, err := service.QueryLongLoginSettings(context.Background(), "unb=1")
	if err != nil || !queried.CanOpenLongLogin || !queried.Enabled {
		t.Fatalf("query result=%+v err=%v", queried, err)
	}
	// set、err 保存set、err，供当前处理流程使用
	set, err := service.SetLongLoginSettings(context.Background(), queried.NewCookies, true)
	if err != nil || !set.Enabled || len(set.SetCookies) != 2 || !strings.Contains(set.NewCookies, "havana_lgc_exp=") {
		t.Fatalf("set result=%+v err=%v", set, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

// TestRenewAfterSessionExpiredBypassesOnlyFatigue 负责TestRenewAfter会话ExpiredBypassesOnlyFatigue相关处理。
func TestRenewAfterSessionExpiredBypassesOnlyFatigue(t *testing.T) {
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "recovered"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	// now 保存now，供当前处理流程使用
	now := time.Now()
	// cookies 保存cookies，供当前处理流程使用
	cookies := "unb=1; sdkSilent=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10) +
		"; havana_lgc_exp=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)
	// service 保存service，供当前处理流程使用
	service := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// proactive、err 保存proactive、err，供当前处理流程使用
	proactive, err := service.RenewAPIFirst(context.Background(), cookies)
	if err != nil || !proactive.Skipped || proactive.SkipReason != "fatigue" || calls.Load() != 0 {
		t.Fatalf("proactive=%+v calls=%d err=%v", proactive, calls.Load(), err)
	}
	// recovered、err 保存recovered、err，供当前处理流程使用
	recovered, err := service.RenewAfterSessionExpired(context.Background(), cookies)
	if err != nil || !recovered.Success || calls.Load() != 1 || !strings.Contains(recovered.NewCookies, "havana_lgc2_77=recovered") {
		t.Fatalf("recovered=%+v calls=%d err=%v", recovered, calls.Load(), err)
	}

	// expiredLongLogin 保存expiredLong登录，供当前处理流程使用
	expiredLongLogin := "unb=1; sdkSilent=" + strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10) +
		"; havana_lgc_exp=" + strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10)
	// blocked、err 保存blocked、err，供当前处理流程使用
	blocked, err := service.RenewAfterSessionExpired(context.Background(), expiredLongLogin)
	if err != nil || !blocked.Skipped || blocked.SkipReason != "long_login_expired" || calls.Load() != 1 {
		t.Fatalf("blocked=%+v calls=%d err=%v", blocked, calls.Load(), err)
	}
}

// TestLongLoginRequestKeepsResponseCookiesOnFailures 负责TestLong登录请求Keeps响应CookiesOnFailures相关处理。
func TestLongLoginRequestKeepsResponseCookiesOnFailures(t *testing.T) {
	// tests 保存tests，供当前处理流程使用
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
			// client 保存client，供当前处理流程使用
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Header: http.Header{
						"Set-Cookie": {"rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly"},
					},
					Body: tt.body,
				}, nil
			})}
			// settings、err 保存settings、err，供当前处理流程使用
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

// TestSetLongLoginSettingsMergesSetAndFailedQueryCookies 负责TestSetLong登录设置MergesSetAnd失败查询Cookies相关处理。
func TestSetLongLoginSettingsMergesSetAndFailedQueryCookies(t *testing.T) {
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
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

	// svc 保存svc，供当前处理流程使用
	svc := Service{
		HTTPClient:            srv.Client(),
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
	}
	// settings、err 保存settings、err，供当前处理流程使用
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

// TestSetLongLoginSettingsScopesCompleteJarBetweenSetAndQuery 负责TestSetLong登录设置ScopesCompleteJarBetweenSetAnd查询相关处理。
func TestSetLongLoginSettingsScopesCompleteJarBetweenSetAndQuery(t *testing.T) {
	// queryCookie 保存查询登录凭证，供当前处理流程使用
	var queryCookie string
	// srv 保存srv，供当前处理流程使用
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
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "shared", Value: "old", Domain: "passport.goofish.com", Path: "/ac/account", Secure: true},
		{Name: "im_only", Value: "visible", Domain: "www.goofish.com", Path: "/im", Secure: true},
	}
	// svc 保存svc，供当前处理流程使用
	svc := Service{
		HTTPClient:            srv.Client(),
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
	}
	// settings、err 保存settings、err，供当前处理流程使用
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

// TestRenewAPIFirstHavanaSendsOneSilentRequest 负责TestRenewAPIFirstHavanaSendsOneSilent请求相关处理。
func TestRenewAPIFirstHavanaSendsOneSilentRequest(t *testing.T) {
	// fingerprint 保存fingerprint，供当前处理流程使用
	fingerprint := useTestDesktopFingerprint(t)
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Query().Get("appEntrance") != "xianyu_sdkSilent" || r.URL.Query().Get("ltl") != "true" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("skipSessionFilter") != "" || r.URL.Query().Get("c2r") != "" {
			t.Fatalf("havana mode included cookie3 flags: %s", r.URL.RawQuery)
		}
		if // got 保存got，供当前处理流程使用
		got := r.URL.Query().Get("documentReferer"); got != "https://www.goofish.com/im" {
			t.Fatalf("documentReferer=%q", got)
		}
		if // got 保存got，供当前处理流程使用
		got := r.Header.Get("User-Agent"); got != fingerprint.UserAgent {
			t.Fatalf("User-Agent=%q", got)
		}
		if // got 保存got，供当前处理流程使用
		got := r.Header.Get("Cookie"); !strings.Contains(got, "unb=1") {
			t.Fatalf("Cookie=%q", got)
		}
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureMillis(time.Hour)})
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()

	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, DocumentReferer: "https://www.goofish.com/im", RetryDelay: -1}
	// input 保存input，供当前处理流程使用
	input := "unb=1; havana_lgc_exp=" + futureMillis(time.Hour)
	// res、err 保存res、err，供当前处理流程使用
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

// TestRenewAPIFirstUsesBrowserCookieScopes 负责TestRenewAPIFirstUses浏览器登录凭证Scopes相关处理。
func TestRenewAPIFirstUsesBrowserCookieScopes(t *testing.T) {
	useTestDesktopFingerprint(t)
	// receivedCookie 保存received登录凭证，供当前处理流程使用
	var receivedCookie string
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()
	// host 保存host，供当前处理流程使用
	host := strings.TrimPrefix(srv.URL, "http://")
	host = strings.Split(host, ":")[0]
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", HTTPOnly: true},
		{Name: "request_only", Value: "passport", Domain: host, Path: "/"},
		{Name: "www_only", Value: "private", Domain: "www.goofish.com", Path: "/im"},
		{Name: "http_only_document", Value: "hidden", Domain: ".goofish.com", Path: "/", HTTPOnly: true},
	}
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, DocumentReferer: "https://www.goofish.com/im", RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
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

// TestRenewAPIFirstUsesHTTPOnlyLongLoginCookieForDecision 负责TestRenewAPIFirstUsesHTTPOnlyLong登录登录凭证ForDecision相关处理。
func TestRenewAPIFirstUsesHTTPOnlyLongLoginCookieForDecision(t *testing.T) {
	useTestDesktopFingerprint(t)
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := []cookierefresh.BrowserCookie{{
		Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", HTTPOnly: true,
	}}
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "", snapshot)
	if err != nil || res == nil || !res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", res, calls.Load(), err)
	}
}

// TestRenewAPIFirstRunsWithLinuxChromiumFingerprint 负责TestRenewAPIFirst运行记录WithLinuxChromiumFingerprint相关处理。
func TestRenewAPIFirstRunsWithLinuxChromiumFingerprint(t *testing.T) {
	// fingerprint 保存fingerprint，供当前处理流程使用
	fingerprint := useTestLinuxFingerprint(t)
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if // got 保存got，供当前处理流程使用
		got := r.Header.Get("User-Agent"); got != fingerprint.UserAgent {
			t.Fatalf("User-Agent=%q", got)
		}
		if // got 保存got，供当前处理流程使用
		got := r.Header.Get("sec-ch-ua-platform"); got != `"Linux"` {
			t.Fatalf("sec-ch-ua-platform=%q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil || res == nil || !res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", res, calls.Load(), err)
	}
}

// TestRenewAPIFirstUsesTopSiteAndAppliesPartitionedSetCookie 负责TestRenewAPIFirstUsesTopSiteAndAppliesPartitionedSet登录凭证相关处理。
func TestRenewAPIFirstUsesTopSiteAndAppliesPartitionedSetCookie(t *testing.T) {
	useTestDesktopFingerprint(t)
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", Secure: true, PartitionKey: goofishTopSite},
		{Name: "passport_partitioned", Value: "right", Domain: "passport.goofish.com", Path: "/", Secure: true, PartitionKey: goofishTopSite},
		{Name: "wrong_partition", Value: "hidden", Domain: "passport.goofish.com", Path: "/", Secure: true, PartitionKey: "https://example.com"},
	}
	// client 保存client，供当前处理流程使用
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// cookies 保存cookies，供当前处理流程使用
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
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: client, RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "", snapshot)
	if err != nil || !res.Success {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !res.CookieSnapshotComplete || !strings.Contains(res.NewCookies, "rotated=fresh") {
		t.Fatalf("authoritative renewal result=%+v", res)
	}
	// found 保存found，供当前处理流程使用
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

// TestRenewAPIFirstKeepsSetCookieWhenBodyReadFails 负责TestRenewAPIFirstKeepsSet登录凭证When请求体ReadFails相关处理。
func TestRenewAPIFirstKeepsSetCookieWhenBodyReadFails(t *testing.T) {
	useTestDesktopFingerprint(t)
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "havana_lgc_exp", Value: futureMillis(time.Hour), Domain: ".goofish.com", Path: "/", Secure: true},
	}
	// client 保存client，供当前处理流程使用
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Set-Cookie": {"rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly"},
			},
			Body: failingReadCloser{err: errors.New("broken response body")},
		}, nil
	})}
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: client, RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "", snapshot)
	if err == nil || res == nil {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !res.CookieSnapshotComplete || res.RequestCount != 1 || len(res.SetCookies) != 1 || !strings.Contains(res.NewCookies, "rotated=fresh") {
		t.Fatalf("response Cookie was lost after body error: %+v", res)
	}
}

// TestRenewAPIFirstTreatsNonNilEmptySnapshotAsAuthoritative 负责TestRenewAPIFirstTreatsNonNilEmptySnapshotAsAuthoritative相关处理。
func TestRenewAPIFirstTreatsNonNilEmptySnapshotAsAuthoritative(t *testing.T) {
	useTestDesktopFingerprint(t)
	// emptySnapshot 保存emptySnapshot，供当前处理流程使用
	emptySnapshot := make([]cookierefresh.BrowserCookie, 0)
	// res、err 保存res、err，供当前处理流程使用
	res, err := (Service{RetryDelay: -1}).RenewAPIFirst(context.Background(), "", emptySnapshot)
	if err != nil || !res.Skipped || res.SkipReason != "long_login_expired" {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if !res.CookieSnapshotComplete || res.CookieSnapshot == nil || len(res.CookieSnapshot) != 0 || res.RenewMethod != "auto_login_plugin" {
		t.Fatalf("empty authoritative snapshot was downgraded: %+v", res)
	}
}

// TestRenewAPIFirstCookie3UsesBackupFlags 负责TestRenewAPIFirstCookie3UsesBackupFlags相关处理。
func TestRenewAPIFirstCookie3UsesBackupFlags(t *testing.T) {
	useTestDesktopFingerprint(t)
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("skipSessionFilter") != "true" || r.URL.Query().Get("c2r") != "true" || r.URL.Query().Get("ltl") != "" {
			t.Fatalf("cookie3 query=%s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	defer srv.Close()
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "cookie3_bak_exp="+futureMillis(time.Hour))
	if err != nil || !res.Success || res.RequestCount != 1 {
		t.Fatalf("result=%#v err=%v", res, err)
	}
}

// TestRenewAPIFirstSkipsFatigueAndExpiredCookies 负责TestRenewAPIFirstSkipsFatigueAndExpiredCookies相关处理。
func TestRenewAPIFirstSkipsFatigueAndExpiredCookies(t *testing.T) {
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}

	// input 表示当前遍历过程中的input
	for _, input := range []string{
		"sdkSilent=" + futureMillis(time.Hour) + "; havana_lgc_exp=" + futureMillis(2*time.Hour),
		"havana_lgc_exp=1; cookie3_bak_exp=1",
	} {
		// res、err 保存res、err，供当前处理流程使用
		res, err := svc.RenewAPIFirst(context.Background(), input)
		if err != nil || !res.Skipped || res.Success || res.RequestCount != 0 {
			t.Fatalf("input=%q result=%#v err=%v", input, res, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("skipped renewal made %d requests", calls.Load())
	}
}

// TestRenewAPIFirstDoesNotRetryOrEscalateFailure 负责TestRenewAPIFirstDoesNot重试OrEscalateFailure相关处理。
func TestRenewAPIFirstDoesNotRetryOrEscalateFailure(t *testing.T) {
	useTestDesktopFingerprint(t)
	// calls 保存calls，供当前处理流程使用
	var calls atomic.Int32
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"content":{"success":false}}`))
	}))
	defer srv.Close()
	// svc 保存svc，供当前处理流程使用
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%#v calls=%d", res, calls.Load())
	}
}

// TestMergeSetCookies 负责TestMergeSetCookies相关处理。
func TestMergeSetCookies(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := MergeSetCookies("unb=1; old=a", []string{"old=b; Path=/; HttpOnly", "new=c; Domain=.goofish.com; Path=/", "bad-cookie"})
	if !strings.Contains(got, "old=b") || !strings.Contains(got, "new=c") || !strings.Contains(got, "unb=1") {
		t.Fatalf("MergeSetCookies=%q", got)
	}
	if // changed 保存changed，供当前处理流程使用
	changed := strings.Join(ChangedCookieNames("unb=1; old=a", got), ","); changed != "new,old" {
		t.Fatalf("ChangedCookieNames=%s", changed)
	}
}

// TestMergeSetCookiesAppliesServerDeletion 负责TestMergeSetCookiesAppliesServerDeletion相关处理。
func TestMergeSetCookiesAppliesServerDeletion(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := MergeSetCookies("unb=1; stale=a; expired=b", []string{
		"stale=; Max-Age=0; Path=/",
		"expired=; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	})
	if strings.Contains(got, "stale=") || strings.Contains(got, "expired=") {
		t.Fatalf("deleted cookies survived: %q", got)
	}
	if // changed 保存changed，供当前处理流程使用
	changed := strings.Join(ChangedCookieNames("unb=1; stale=a; expired=b", got), ","); changed != "expired,stale" {
		t.Fatalf("ChangedCookieNames=%s", changed)
	}
}

// TestMergeSetCookiesMaxAgeOverridesPastExpires 负责TestMergeSetCookiesMaxAgeOverridesPastExpires相关处理。
func TestMergeSetCookiesMaxAgeOverridesPastExpires(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := MergeSetCookies("session=old", []string{
		"session=fresh; Max-Age=3600; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	})
	if !strings.Contains(got, "session=fresh") {
		t.Fatalf("positive Max-Age must override past Expires: %q", got)
	}
}

// TestRenewBusinessOKMatchesOfficialPlugin 负责TestRenewBusinessOKMatchesOfficialPlugin相关处理。
func TestRenewBusinessOKMatchesOfficialPlugin(t *testing.T) {
	if renewBusinessOK([]byte(`{"content":{"success":true}}`)) {
		t.Fatal("official plugin requires processFinished=true and resultCode=100")
	}
	if !renewBusinessOK([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`)) {
		t.Fatal("official success payload was rejected")
	}
}

// TestRenewAPIFirstReturnsAtPromiseTimeoutAndKeepsLateCookies 负责TestRenewAPIFirstReturnsAtPromiseTimeoutAndKeepsLateCookies相关处理。
func TestRenewAPIFirstReturnsAtPromiseTimeoutAndKeepsLateCookies(t *testing.T) {
	useTestDesktopFingerprint(t)
	// completed 保存completed，供当前处理流程使用
	var completed atomic.Bool
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureMillis(time.Hour), Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
		completed.Store(true)
	}))
	defer srv.Close()
	// svc 保存svc，供当前处理流程使用
	svc := Service{
		HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1,
		PromiseTimeout: 20 * time.Millisecond,
	}
	// started 保存started，供当前处理流程使用
	started := time.Now()
	// res、err 保存res、err，供当前处理流程使用
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil || res == nil || !res.HasPending() {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if // elapsed 保存elapsed，供当前处理流程使用
	elapsed := time.Since(started); elapsed >= 70*time.Millisecond {
		t.Fatalf("外层 Promise 没有按时返回: %s", elapsed)
	}
	if completed.Load() || len(res.SetCookies) != 0 || res.Success || res.NeedPasswordLogin {
		t.Fatalf("超时瞬间不应伪造底层结果或要求重新登录: %+v completed=%v", res, completed.Load())
	}
	// waitCtx、cancel 保存waitCtx、cancel，供当前处理流程使用
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// late、lateErr 保存late、lateErr，供当前处理流程使用
	late, lateErr := res.AwaitPending(waitCtx)
	if lateErr != nil || late == nil || len(late.SetCookies) != 1 || !strings.Contains(late.NewCookies, "sdkSilent=") {
		t.Fatalf("迟到响应 Cookie 丢失: result=%+v err=%v", late, lateErr)
	}
	if !completed.Load() || !late.Success || late.NeedPasswordLogin {
		t.Fatalf("底层请求完成后应返回真实业务终态: %+v completed=%v", late, completed.Load())
	}
}

// TestRebaseResponseCookiesUsesLatestAuthoritativeJar 负责TestRebase响应CookiesUsesLatestAuthoritativeJar相关处理。
func TestRebaseResponseCookiesUsesLatestAuthoritativeJar(t *testing.T) {
	// current 保存current，供当前处理流程使用
	current := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "concurrent", Value: "kept", Domain: ".goofish.com", Path: "/", Secure: true},
	}
	// metadata 保存metadata，供当前处理流程使用
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"keep"}`, current)
	// late 保存late，供当前处理流程使用
	late := &Result{
		SetCookies:        []string{"sdkSilent=9999999999999; Domain=goofish.com; Path=/; Secure; HttpOnly"},
		responseCookieURL: SilentHasLoginURL,
	}
	// value、updatedMetadata、changed 保存value、updatedMetadata、changed，供当前处理流程使用
	value, updatedMetadata, changed := RebaseResponseCookies("unb=1; concurrent=kept", metadata, late)
	if !changed || !strings.Contains(value, "concurrent=kept") || !strings.Contains(value, "sdkSilent=9999999999999") {
		t.Fatalf("迟到响应覆盖了并发 Cookie: value=%q changed=%v", value, changed)
	}
	// snapshot、complete 保存snapshot、complete，供当前处理流程使用
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(updatedMetadata)
	if !complete {
		t.Fatalf("权威快照被降级: %s", updatedMetadata)
	}
	// found 保存found，供当前处理流程使用
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

// TestRenewBodyLimit 负责TestRenew请求体上限相关处理。
func TestRenewBodyLimit(t *testing.T) {
	// err 保存err，供当前处理流程使用
	_, err := readRenewBody(strings.NewReader(strings.Repeat("x", maxRenewBodyBytes+1)))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxRenewBodyBytes>>20)) {
		t.Fatalf("err=%v", err)
	}
}
