package renew

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestMergeSetCookies(t *testing.T) {
	got := MergeSetCookies("unb=1; old=a", []string{
		"old=b; Path=/; HttpOnly",
		"new=c; Domain=.goofish.com; Path=/",
		"bad-cookie",
	})
	if !strings.Contains(got, "old=b") || !strings.Contains(got, "new=c") || !strings.Contains(got, "unb=1") {
		t.Fatalf("MergeSetCookies=%q", got)
	}
	changed := ChangedCookieNames("unb=1; old=a", got)
	if strings.Join(changed, ",") != "new,old" {
		t.Fatalf("ChangedCookieNames=%v", changed)
	}
}

func TestRenewAPIFirstSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "sgcookie", Value: "s1"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tk_1"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/setLoginSettings.do":
			// 只有这个接口返回 Set-Cookie 才代表长登录续期真正成功。
			body, _ := io.ReadAll(r.Body)
			if string(body) != "status=0" {
				t.Fatalf("setLoginSettings body=%q, want status=0", string(body))
			}
			if r.URL.Query().Get("bizEntrance") != "web" {
				t.Fatalf("setLoginSettings query=%s", r.URL.RawQuery)
			}
			http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "lgc"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
	res, err := svc.RenewAPIFirst(context.Background(), "unb=1; cookie2=c2; _tb_token_=csrf")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if !res.Success || res.RenewMethod != "api" {
		t.Fatalf("success/method 异常: %#v", res)
	}
	for _, want := range []string{"sgcookie=s1", "_m_h5_tk=tk_1", "havana_lgc2_77=lgc"} {
		if !strings.Contains(res.NewCookies, want) {
			t.Fatalf("NewCookies 缺少 %s: %q", want, res.NewCookies)
		}
	}
}

func TestRenewAPIFirstMatchesReferenceRequestShape(t *testing.T) {
	var hasLoginBody string
	var hasLoginReferer string
	var hasLoginBXV string
	var hasLoginHeaders http.Header
	var silentHeaders http.Header
	var silentQuery url.Values
	var settingBody string
	var settingHeaders http.Header
	var settingQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			body, _ := io.ReadAll(r.Body)
			hasLoginBody = string(body)
			hasLoginReferer = r.Header.Get("Referer")
			hasLoginBXV = r.Header.Get("bx-v")
			hasLoginHeaders = r.Header.Clone()
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			silentHeaders = r.Header.Clone()
			silentQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"content":{"success":true},"marker":"silent"}`))
		case "/setLoginSettings.do":
			body, _ := io.ReadAll(r.Body)
			settingBody = string(body)
			settingHeaders = r.Header.Clone()
			settingQuery = r.URL.Query()
			http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "lgc"})
			_, _ = w.Write([]byte(`{"content":{"success":true},"marker":"settings"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
	res, err := svc.RenewAPIFirst(context.Background(), "unb=1; cookie2=c2; _tb_token_=csrf; _uab_collina=umid; XSRF-TOKEN=xsrf")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if !strings.Contains(res.ResponseText, `"marker":"silent"`) || strings.Contains(res.ResponseText, `"marker":"settings"`) {
		t.Fatalf("ResponseText 应来自 silentHasLogin 响应，got %q", res.ResponseText)
	}
	values, err := url.ParseQuery(hasLoginBody)
	if err != nil {
		t.Fatalf("hasLogin body parse: %v", err)
	}
	for _, key := range []string{"hid", "ltl", "appName", "appEntrance", "_csrf_token", "umidToken", "hsiz", "bizParams", "mainPage", "isMobile", "lang", "returnUrl", "fromSite", "isIframe", "documentReferer", "defaultView", "umidTag", "deviceId", "pageTraceId"} {
		if _, ok := values[key]; !ok {
			t.Fatalf("hasLogin body missing %s: %s", key, hasLoginBody)
		}
	}
	if !regexp.MustCompile(`^21504\d{19}$`).MatchString(values.Get("pageTraceId")) {
		t.Fatalf("pageTraceId=%q", values.Get("pageTraceId"))
	}
	if !strings.Contains(hasLoginReferer, "mini_login.htm") || !strings.Contains(hasLoginReferer, "rnd=") {
		t.Fatalf("hasLogin Referer=%q", hasLoginReferer)
	}
	if hasLoginBXV != "2.5.31" {
		t.Fatalf("hasLogin bx-v=%q", hasLoginBXV)
	}
	if got := hasLoginHeaders.Get("Accept-Language"); got != "zh-CN" {
		t.Fatalf("hasLogin Accept-Language=%q", got)
	}
	if got := hasLoginHeaders.Get("sec-ch-ua"); got != hasLoginSecChUA {
		t.Fatalf("hasLogin sec-ch-ua=%q", got)
	}
	if got := hasLoginHeaders.Get("x-xsrf-token"); got != "xsrf" {
		t.Fatalf("hasLogin x-xsrf-token=%q", got)
	}
	if silentQuery.Get("documentReferer") != "https://www.goofish.com/" ||
		silentQuery.Get("appEntrance") != "xianyu_sdkSilent" ||
		silentQuery.Get("fromSite") != "0" {
		t.Fatalf("silentHasLogin query=%s", silentQuery.Encode())
	}
	if got := silentHeaders.Get("Accept"); got != "*/*" {
		t.Fatalf("silentHasLogin Accept=%q", got)
	}
	if got := silentHeaders.Get("sec-ch-ua-platform"); got != `"Win32"` {
		t.Fatalf("silentHasLogin sec-ch-ua-platform=%q", got)
	}
	if got := silentHeaders.Get("sec-fetch-site"); got != "same-site" {
		t.Fatalf("silentHasLogin sec-fetch-site=%q", got)
	}
	if settingBody != "status=0" {
		t.Fatalf("setLoginSettings body=%q", settingBody)
	}
	if settingQuery.Get("fromSite") != "77" || settingQuery.Get("appName") != "xianyu" || settingQuery.Get("bizEntrance") != "web" {
		t.Fatalf("setLoginSettings query=%s", settingQuery.Encode())
	}
	if got := settingHeaders.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("setLoginSettings Content-Type=%q", got)
	}
	if got := settingHeaders.Get("sec-ch-ua"); got != settingSecChUA {
		t.Fatalf("setLoginSettings sec-ch-ua=%q", got)
	}
	for name, headers := range map[string]http.Header{
		"hasLogin":         hasLoginHeaders,
		"silentHasLogin":   silentHeaders,
		"setLoginSettings": settingHeaders,
	} {
		if got := headers.Get("Origin"); got != "" {
			t.Fatalf("%s Origin should be empty, got %q", name, got)
		}
		if got := headers.Get("Cookie"); strings.ContainsAny(got, "\r\n") {
			t.Fatalf("%s Cookie 未清理换行: %q", name, got)
		}
	}
}

func TestRenewAPIFirstDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			w.Header().Set("Location", "/unexpected")
			w.WriteHeader(http.StatusFound)
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/setLoginSettings.do":
			http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "lgc"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			t.Fatalf("request followed redirect to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
	res, err := svc.RenewAPIFirst(context.Background(), "unb=1; cookie2=c2")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if !res.Success {
		t.Fatalf("RenewAPIFirst should continue after 302 without redirect: %#v", res)
	}
}

func TestMergeSetCookiesIgnoresDeletionCookies(t *testing.T) {
	got := MergeSetCookies("old=a", filterValidSetCookies([]string{
		"old=; Max-Age=0; Path=/",
		"gone=x; Expires=Thu, 01 Jan 1970 00:00:00 GMT",
		"new=b; Path=/",
	}))
	if strings.Contains(got, "gone=") || strings.Contains(got, "old=") && !strings.Contains(got, "old=a") {
		t.Fatalf("deletion cookies should be ignored: %q", got)
	}
	if !strings.Contains(got, "old=a") || !strings.Contains(got, "new=b") {
		t.Fatalf("valid cookies missing: %q", got)
	}
}

func TestRenewAPIFirstKeepsPartialCookiesWhenLongLoginFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "sgcookie", Value: "s1"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/silentHasLogin.do":
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "tk_1"})
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/setLoginSettings.do":
			_, _ = w.Write([]byte(`{"content":{"success":false}}`))
		default:
			http.Error(w, fmt.Sprintf("unexpected path %s", r.URL.Path), http.StatusNotFound)
		}
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
	res, err := svc.RenewAPIFirst(context.Background(), "unb=1; cookie2=c2")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if res.Success {
		t.Fatalf("setLoginSettings 无 Set-Cookie 不应成功: %#v", res)
	}
	if !strings.Contains(res.NewCookies, "sgcookie=s1") || !strings.Contains(res.NewCookies, "_m_h5_tk=tk_1") {
		t.Fatalf("部分 Cookie 更新未保留: %q", res.NewCookies)
	}
}

func TestRenewAPIFirstKeepsOriginalStringWhenNoSetCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":{"success":true}}`))
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
	original := "unb=1; cookie2=c2"
	res, err := svc.RenewAPIFirst(context.Background(), original)
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if res.Success {
		t.Fatalf("setLoginSettings 无 Set-Cookie 不应成功: %#v", res)
	}
	if res.NewCookies != original || len(res.UpdatedCookieNames) != 0 {
		t.Fatalf("无 Set-Cookie 时不应重排/标记更新: %#v", res)
	}
}

func TestRenewAPIFirstSkipsHasLoginWithoutUNB(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"content":{"success":true}}`))
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          -1,
	}
	_, err := svc.RenewAPIFirst(context.Background(), "cookie2=c2")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if calls != 2 {
		t.Fatalf("缺少 unb 时 hasLogin 应跳过，只调用后两步，calls=%d", calls)
	}
}

func TestRenewAPIFirstRetriesLongLoginOnce(t *testing.T) {
	setCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hasLogin.do", "/silentHasLogin.do":
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		case "/setLoginSettings.do":
			setCalls++
			if setCalls == 2 {
				http.SetCookie(w, &http.Cookie{Name: "havana_lgc2_77", Value: "lgc"})
			}
			_, _ = w.Write([]byte(`{"content":{"success":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := Service{
		HTTPClient:          srv.Client(),
		HasLoginURL:         srv.URL + "/hasLogin.do",
		SilentHasLoginURL:   srv.URL + "/silentHasLogin.do",
		SetLoginSettingsURL: srv.URL + "/setLoginSettings.do",
		RetryDelay:          time.Nanosecond,
	}
	res, err := svc.RenewAPIFirst(context.Background(), "unb=1; cookie2=c2")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if !res.Success || setCalls != 2 || !strings.Contains(res.NewCookies, "havana_lgc2_77=lgc") {
		t.Fatalf("retry result=%#v setCalls=%d", res, setCalls)
	}
}
