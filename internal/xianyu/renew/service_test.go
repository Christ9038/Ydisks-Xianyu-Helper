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
		{name: "expired", cookies: map[string]string{"havana_lgc_exp": "bad", "cookie3_bak_exp": strconv.FormatInt(now.Add(-time.Hour).UnixMilli(), 10)}, wantReason: "long_login_expired"},
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

	svc := Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}
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

func TestRenewBodyLimit(t *testing.T) {
	_, err := readRenewBody(strings.NewReader(strings.Repeat("x", maxRenewBodyBytes+1)))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxRenewBodyBytes>>20)) {
		t.Fatalf("err=%v", err)
	}
}
