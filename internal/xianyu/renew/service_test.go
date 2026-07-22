package renew

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/xianyu"
)

func futureMillis(d time.Duration) string {
	return strconv.FormatInt(time.Now().Add(d).UnixMilli(), 10)
}

func TestAutoLoginModeMatchesBrowserPlugin(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		cookies    map[string]string
		wantMode   string
		wantReason string
	}{
		{name: "fatigue", cookies: map[string]string{"sdkSilent": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10), "havana_lgc_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantReason: "fatigue"},
		{name: "havana", cookies: map[string]string{"havana_lgc_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeHavana},
		{name: "cookie3 backup", cookies: map[string]string{"havana_lgc_exp": strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10), "cookie3_bak_exp": strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeCookie3},
		{name: "malformed long-login expiry follows browser Invalid Date branch", cookies: map[string]string{"havana_lgc_exp": "bad", "cookie3_bak_exp": strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10)}, wantMode: autoLoginModeHavana},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, reason := autoLoginMode(tt.cookies, now)
			if mode != tt.wantMode || reason != tt.wantReason {
				t.Fatalf("mode=%q reason=%q, want mode=%q reason=%q", mode, reason, tt.wantMode, tt.wantReason)
			}
		})
	}
}

func TestLongLoginSettingsMatchOfficialRequest(t *testing.T) {
	var calls atomic.Int32
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
			if err := r.ParseForm(); err != nil || r.Form.Get("status") != "0" {
				t.Fatalf("set form=%v err=%v", r.Form, err)
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "havana_lgc_exp", Value: futureMillis(24 * time.Hour), Path: "/", HttpOnly: true})
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer srv.Close()

	service := Service{
		HTTPClient:            srv.Client(),
		QueryLoginSettingsURL: srv.URL + "/queryLoginSettings.do",
		SetLoginSettingsURL:   srv.URL + "/setLoginSettings.do",
		DocumentReferer:       "https://www.goofish.com/im",
	}
	queried, err := service.QueryLongLoginSettings(context.Background(), "unb=1")
	if err != nil || !queried.CanOpenLongLogin || !queried.Enabled {
		t.Fatalf("query result=%+v err=%v", queried, err)
	}
	set, err := service.SetLongLoginSettings(context.Background(), queried.NewCookies, true)
	if err != nil || !set.Enabled || len(set.SetCookies) != 1 || !strings.Contains(set.NewCookies, "havana_lgc_exp=") {
		t.Fatalf("set result=%+v err=%v", set, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestRenewAPIFirstHavanaSendsOneSilentRequest(t *testing.T) {
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "native-browser-ua", SecChUA: `"Chromium";v="999"`, Platform: "macOS", Mobile: "?0"})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Query().Get("appEntrance") != "xianyu_sdkSilent" || r.URL.Query().Get("ltl") != "true" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("skipSessionFilter") != "" || r.URL.Query().Get("c2r") != "" {
			t.Fatalf("havana mode included cookie3 flags: %s", r.URL.RawQuery)
		}
		if got := r.URL.Query().Get("documentReferer"); got != "https://www.goofish.com/im" {
			t.Fatalf("documentReferer=%q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "native-browser-ua" {
			t.Fatalf("User-Agent=%q", got)
		}
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "unb=1") {
			t.Fatalf("Cookie=%q", got)
		}
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: futureMillis(time.Hour)})
		_, _ = w.Write([]byte(`{"data":{"content":{"data":{"processFinished":true,"resultCode":100}}}}`))
	}))
	defer srv.Close()

	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, DocumentReferer: "https://www.goofish.com/im", RetryDelay: -1}
	input := "unb=1; havana_lgc_exp=" + futureMillis(time.Hour)
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

func TestRenewAPIFirstCookie3UsesBackupFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("skipSessionFilter") != "true" || r.URL.Query().Get("c2r") != "true" || r.URL.Query().Get("ltl") != "" {
			t.Fatalf("cookie3 query=%s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":"100"}}}`))
	}))
	defer srv.Close()
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	res, err := svc.RenewAPIFirst(context.Background(), "cookie3_bak_exp="+futureMillis(time.Hour))
	if err != nil || !res.Success || res.RequestCount != 1 {
		t.Fatalf("result=%#v err=%v", res, err)
	}
}

func TestRenewAPIFirstSkipsFatigueAndExpiredCookies(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}

	for _, input := range []string{
		"sdkSilent=" + futureMillis(time.Hour) + "; havana_lgc_exp=" + futureMillis(2*time.Hour),
		"havana_lgc_exp=1; cookie3_bak_exp=1",
	} {
		res, err := svc.RenewAPIFirst(context.Background(), input)
		if err != nil || !res.Skipped || res.Success || res.RequestCount != 0 {
			t.Fatalf("input=%q result=%#v err=%v", input, res, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("skipped renewal made %d requests", calls.Load())
	}
}

func TestRenewAPIFirstDoesNotRetryOrEscalateFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"content":{"success":false}}`))
	}))
	defer srv.Close()
	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
	res, err := svc.RenewAPIFirst(context.Background(), "havana_lgc_exp="+futureMillis(time.Hour))
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if res.Success || res.Skipped || res.RequestCount != 1 || calls.Load() != 1 {
		t.Fatalf("result=%#v calls=%d", res, calls.Load())
	}
}

func TestMergeSetCookies(t *testing.T) {
	got := MergeSetCookies("unb=1; old=a", []string{"old=b; Path=/; HttpOnly", "new=c; Domain=.goofish.com; Path=/", "bad-cookie"})
	if !strings.Contains(got, "old=b") || !strings.Contains(got, "new=c") || !strings.Contains(got, "unb=1") {
		t.Fatalf("MergeSetCookies=%q", got)
	}
	if changed := strings.Join(ChangedCookieNames("unb=1; old=a", got), ","); changed != "new,old" {
		t.Fatalf("ChangedCookieNames=%s", changed)
	}
}

func TestMergeSetCookiesAppliesServerDeletion(t *testing.T) {
	got := MergeSetCookies("unb=1; stale=a; expired=b", []string{
		"stale=; Max-Age=0; Path=/",
		"expired=; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	})
	if strings.Contains(got, "stale=") || strings.Contains(got, "expired=") {
		t.Fatalf("deleted cookies survived: %q", got)
	}
	if changed := strings.Join(ChangedCookieNames("unb=1; stale=a; expired=b", got), ","); changed != "expired,stale" {
		t.Fatalf("ChangedCookieNames=%s", changed)
	}
}

func TestRenewBusinessOKMatchesOfficialPlugin(t *testing.T) {
	if renewBusinessOK([]byte(`{"content":{"success":true}}`)) {
		t.Fatal("official plugin requires processFinished=true and resultCode=100")
	}
	if !renewBusinessOK([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`)) {
		t.Fatal("official success payload was rejected")
	}
}

func TestRenewBodyLimit(t *testing.T) {
	_, err := readRenewBody(strings.NewReader(strings.Repeat("x", maxRenewBodyBytes+1)))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxRenewBodyBytes>>20)) {
		t.Fatalf("err=%v", err)
	}
}
