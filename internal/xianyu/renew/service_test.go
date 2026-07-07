package renew

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	}
	_, err := svc.RenewAPIFirst(context.Background(), "cookie2=c2")
	if err != nil {
		t.Fatalf("RenewAPIFirst: %v", err)
	}
	if calls != 2 {
		t.Fatalf("缺少 unb 时 hasLogin 应跳过，只调用后两步，calls=%d", calls)
	}
}
