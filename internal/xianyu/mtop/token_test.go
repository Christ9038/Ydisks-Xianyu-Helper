package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

// testCookiesWithUnb 用于本次流程后续判断的testCookiesWithUnb
const testCookiesWithUnb = "unb=123; _m_h5_tk=oldtoken_1;"

// TestRefreshTokenHTTPUsesCookieSessionScopes 封装TestRefresh令牌HTTPUses登录凭证会话Scopes业务协调。
func TestRefreshTokenHTTPUsesCookieSessionScopes(t *testing.T) {
	// initial 用于本次流程后续判断的initial
	initial := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "document-old_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "document_only", Value: "visible", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "api_http_only", Value: "secret", Domain: "h5api.m.goofish.com", Path: "/h5", Secure: true, HTTPOnly: true},
	}
	// ctx、session 用于本次流程后续判断的ctx、session
	ctx, session := WithCookieSnapshot(context.Background(), initial)
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		// cookieHeader 用于本次流程后续判断的登录凭证Header
		cookieHeader := req.Header.Get("Cookie")
		// want 表示当前遍历过程中的want
		for _, want := range []string{"unb=123", "_m_h5_tk=document-old_1", "api_http_only=secret"} {
			if !strings.Contains(cookieHeader, want) {
				t.Errorf("request Cookie %q missing %q", cookieHeader, want)
			}
		}
		// unwanted 表示当前遍历过程中的unwanted
		for _, unwanted := range []string{"document_only=", "fallback_only="} {
			if strings.Contains(cookieHeader, unwanted) {
				t.Errorf("request Cookie %q unexpectedly contains %q", cookieHeader, unwanted)
			}
		}
		// timestamp 用于本次流程后续判断的timestamp
		timestamp := req.URL.Query().Get("t")
		// dataVal 用于本次流程后续判断的数据Val
		dataVal := `{"appKey":"` + RegAppKey + `","deviceId":"did"}`
		// wantSign 用于本次流程后续判断的wantSign
		wantSign := protocol.GenerateSign(timestamp, "document-old", dataVal)
		if // got 用于本次流程后续判断的got
		got := req.URL.Query().Get("sign"); got != wantSign {
			t.Errorf("sign=%q want %q", got, wantSign)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": {
				"_m_h5_tk=document-new_9; Domain=.goofish.com; Path=/; Secure",
			}},
			Body:    io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"direct-context"}}`)),
			Request: req,
		}, nil
	})}}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.RefreshTokenWithDeviceIDContext(ctx, "unb=fallback; _m_h5_tk=fallback_1; fallback_only=leak", "did")
	if err != nil || result.AccessToken != "direct-context" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// canonical、changed 用于本次流程后续判断的canonical、changed
	canonical, _, changed := session.State()
	if !changed || !strings.Contains(canonical, "_m_h5_tk=document-new_9") || !strings.Contains(result.UpdatedCookies, "_m_h5_tk=document-new_9") {
		t.Fatalf("canonical=%q result=%+v changed=%v", canonical, result, changed)
	}
}

// TestRequestFreshCaptchaURLContextReturnsDirectToken 覆盖新鲜验证码请求入口在风控已解除时直接返回令牌。
func TestRequestFreshCaptchaURLContextReturnsDirectToken(t *testing.T) {
	// server 保存 token 接口直接返回 accessToken 的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"fresh-access","accessTokenExpireAt":12345}}`))
	}))
	defer server.Close()
	// client 保存验证码链接请求入口使用的本地 token 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// result、requestErr 保存新鲜验证码请求结果和错误。
	result, requestErr := client.RequestFreshCaptchaURLContext(context.Background(), testCookiesWithUnb, "device-fresh")
	if requestErr != nil || result == nil || !result.TokenOK || result.AccessToken != "fresh-access" {
		t.Fatalf("新鲜验证码请求异常 result=%+v err=%v", result, requestErr)
	}
}

// TestRefreshTokenWithDeviceIDSuccessOnRetry: 首次返回 token 过期 + Set-Cookie，二次成功。
func TestRefreshTokenWithDeviceIDSuccessOnRetry(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 用于本次流程后续判断的尝试次数
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_999", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"access-with-device"}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// result、err 用于本次流程后续判断的result、err
	result, err := client.RefreshTokenWithDeviceIDContext(ctx, testCookiesWithUnb, "device-xyz")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.AccessToken != "access-with-device" {
		t.Fatalf("AccessToken=%q", result.AccessToken)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d want 2", requests.Load())
	}
}

// TestRefreshTokenMissingUnbCookie: cookie 缺 unb 报错。
func TestRefreshTokenMissingUnbCookie(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("不应发请求")
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), "_m_h5_tk=token_1;")
	if err == nil || !strings.Contains(err.Error(), "cookie 缺少 unb") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenSuccessButAccessTokenEmpty: ret SUCCESS 但 accessToken 为空。
func TestRefreshTokenSuccessButAccessTokenEmpty(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "accessToken 为空") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1 (非 token 过期不应重试)", requests.Load())
	}
}

// TestRefreshTokenNonSuccessRet: 非 token 过期的失败 ret，不重试直接报错。
func TestRefreshTokenNonSuccessRet(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_VALIDATE_FAIL::参数错误"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "token API 返回非成功") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
}

// TestRefreshTokenHTTPError: 5xx 视为请求失败，直接返回 err（不进入 ret 解析路径）。
func TestRefreshTokenHTTPError(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 5xx 但 body 是有效 JSON — refreshTokenOnce 只看业务 ret，不看 HTTP 状态码；
		// 由于 ret 解析为非 token 过期失败，应走"返回非成功"分支。
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_INTERNAL_ERROR::内部错误"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "token API 返回非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenParseFailure: 响应非 JSON 解析失败。
func TestRefreshTokenParseFailure(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not-json{{{`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "解析 token 响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenRequestError: 网络层错误（服务器关闭）。
func TestRefreshTokenRequestError(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // 立即关闭，使请求失败

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "token API 请求失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenExpiredRetNoCookieUsesOfficialAttemptLimit: 官网 lib-mtop 即使
// 响应未下发新 Cookie，也会最多执行五次请求（含首次）。
// TestRefreshTokenExpiredRetNoCookieUsesOfficialAttemptLimit 封装TestRefresh令牌ExpiredRetNo登录凭证UsesOfficial尝试次数上限业务协调。
func TestRefreshTokenExpiredRetNoCookieUsesOfficialAttemptLimit(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// 不 Set-Cookie，ret 为 token 过期
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != officialMTopMaxAttempts {
		t.Fatalf("requests=%d want %d", requests.Load(), officialMTopMaxAttempts)
	}
}

// TestRefreshTokenExhaustionClearsOfficialMTopCookies 封装TestRefresh令牌ExhaustionClearsOfficialMTopCookies业务协调。
func TestRefreshTokenExhaustionClearsOfficialMTopCookies(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/"},
		{Name: "keep", Value: "yes", Domain: ".goofish.com", Path: "/"},
		{Name: "_m_h5_c", Value: "c", Domain: ".goofish.com", Path: "/"},
		{Name: "_m_h5_tk", Value: "tk", Domain: ".goofish.com", Path: "/"},
		{Name: "_m_h5_tk_enc", Value: "enc", Domain: ".m.goofish.com", Path: "/"},
		{Name: "_m_h5_tk", Value: "scoped", Domain: ".goofish.com", Path: "/im"},
	}
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()
	// ctx 用于本次流程后续判断的ctx
	ctx, _ := WithCookieSnapshot(context.Background(), snapshot)
	// result、err 用于本次流程后续判断的result、err
	result, err := (&ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}).RefreshTokenWithCredentialContext(
		ctx, testCookiesWithUnb, "did", snapshot,
	)
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if requests.Load() != officialMTopMaxAttempts {
		t.Fatalf("requests=%d want %d", requests.Load(), officialMTopMaxAttempts)
	}
	// name 表示当前遍历过程中的名称
	for _, name := range []string{"_m_h5_c=", "_m_h5_tk_enc="} {
		if strings.Contains(result.UpdatedCookies, name) {
			t.Fatalf("官网清理后仍包含 %s: %q", name, result.UpdatedCookies)
		}
	}
	if !strings.Contains(result.UpdatedCookies, "_m_h5_tk=scoped") {
		t.Fatalf("Path=/im 的同名 token 不应被根路径清理误删: %q", result.UpdatedCookies)
	}
	if !strings.Contains(result.UpdatedCookies, "keep=yes") {
		t.Fatalf("无关 Cookie 被误删: %q", result.UpdatedCookies)
	}
	// keptScoped 用于本次流程后续判断的keptScoped
	var keptScoped bool
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range result.CookieSnapshot {
		if cookie.Name == "_m_h5_tk" && cookie.Path == "/im" {
			keptScoped = true
		}
		if cookie.Path == "/" && (cookie.Name == "_m_h5_c" || cookie.Name == "_m_h5_tk" || cookie.Name == "_m_h5_tk_enc") {
			t.Fatalf("官网根路径凭证 Cookie 未清理: %+v", cookie)
		}
	}
	if !keptScoped {
		t.Fatal("不属于官网清理目标的同名作用域 Cookie 被误删")
	}
}

// TestRefreshTokenFlatSessionExhaustionPersistsOfficialCookieClear 封装TestRefresh令牌Flat会话ExhaustionPersistsOfficial登录凭证Clear业务协调。
func TestRefreshTokenFlatSessionExhaustionPersistsOfficialCookieClear(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
	}))
	defer server.Close()
	// initial 用于本次流程后续判断的initial
	initial := "unb=123; _m_h5_c=c; _m_h5_tk=oldtoken_1; _m_h5_tk_enc=enc; keep=yes"
	// ctx、session 用于本次流程后续判断的ctx、session
	ctx, session := WithFlatCookieSession(context.Background(), initial)
	// result、err 用于本次流程后续判断的result、err
	result, err := (&ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}).RefreshTokenContext(ctx, initial)
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// removed 表示当前遍历过程中的removed
	for _, removed := range []string{"_m_h5_c=", "_m_h5_tk=", "_m_h5_tk_enc="} {
		if strings.Contains(result.UpdatedCookies, removed) {
			t.Fatalf("legacy flat 清理结果仍包含 %s: %q", removed, result.UpdatedCookies)
		}
	}
	if !strings.Contains(result.UpdatedCookies, "unb=123") || !strings.Contains(result.UpdatedCookies, "keep=yes") || !result.CookieStateChanged {
		t.Fatalf("result=%+v", result)
	}
	// value、snapshot、changed 用于本次流程后续判断的value、snapshot、changed
	value, snapshot, changed := session.State()
	if !changed || snapshot != nil || value != result.UpdatedCookies {
		t.Fatalf("session value=%q snapshot=%+v changed=%v result=%+v", value, snapshot, changed, result)
	}
}

// TestRefreshTokenKeepsAccumulatedSnapshotWhenLaterAttemptFails 封装TestRefresh令牌KeepsAccumulatedSnapshotWhenLater尝试次数Fails业务协调。
func TestRefreshTokenKeepsAccumulatedSnapshotWhenLaterAttemptFails(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls atomic.Int32
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: &http.Client{Transport: cookieSessionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{"Set-Cookie": {
					"off_im=rotated; Domain=.goofish.com; Path=/account; Secure; HttpOnly",
				}},
				Body:    io.NopCloser(strings.NewReader(`{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)),
				Request: req,
			}, nil
		}
		return nil, fmt.Errorf("second attempt network failure")
	})}}
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "oldtoken_1", Domain: ".goofish.com", Path: "/", Secure: true},
	}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.RefreshTokenWithCredentialContext(context.Background(), testCookiesWithUnb, "did", snapshot)
	if err == nil || !strings.Contains(err.Error(), "network failure") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !result.CookieSnapshotComplete || !result.CookieStateChanged {
		t.Fatalf("累计 Jar 状态丢失: %+v", result)
	}
	// found 用于本次流程后续判断的found
	var found bool
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range result.CookieSnapshot {
		if cookie.Name == "off_im" && cookie.Value == "rotated" && cookie.Path == "/account" {
			found = true
		}
	}
	if !found {
		t.Fatalf("首轮 off-/im Set-Cookie 在后续网络失败后丢失: %+v", result.CookieSnapshot)
	}
}

// TestRefreshTokenPreservesExplicitFlatDeletionOnParseError 封装TestRefresh令牌PreservesExplicitFlatDeletionOnParse错误业务协调。
func TestRefreshTokenPreservesExplicitFlatDeletionOnParseError(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "unb=; Path=/; Max-Age=0")
		w.Header().Add("Set-Cookie", "_m_h5_tk=; Path=/; Max-Age=0")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()
	// result、err 用于本次流程后续判断的result、err
	result, err := (&ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}).RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "解析 token 响应失败") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.UpdatedCookies != "" || !result.CookieStateChanged || result.CookieSnapshotComplete {
		t.Fatalf("明确删除到空的 flat Cookie 被恢复: %+v", result)
	}
}

// TestRefreshTokenSessionExpiredDoesNotUseTokenRetry 封装TestRefresh令牌会话ExpiredDoesNotUse令牌重试业务协调。
func TestRefreshTokenSessionExpiredDoesNotUseTokenRetry(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_SESSION_EXPIRED::会话过期"]}`)
	}))
	defer server.Close()
	// err 用于本次流程后续判断的err
	_, err := (&ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}).RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || requests.Load() != 1 {
		t.Fatalf("err=%v requests=%d want 1", err, requests.Load())
	}
}

// TestBuildTokenQueryMatchesCurrentMessagePageContext 封装TestBuild令牌查询MatchesCurrent消息页码上下文业务协调。
func TestBuildTokenQueryMatchesCurrentMessagePageContext(t *testing.T) {
	// query、err 用于本次流程后续判断的query、err
	query, err := url.ParseQuery(buildTokenQuery("123", "sig"))
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("spm_cnt") != "a21ybx.im.0.0" || query.Get("spm_pre") != "" || query.Get("log_id") != "" {
		t.Fatalf("spm query=%v", query)
	}
	// stale 表示当前遍历过程中的stale
	for _, stale := range []string{"smToken", "queryToken", "sm"} {
		if // ok 用于本次流程后续判断的ok
		_, ok := query[stale]; ok {
			t.Fatalf("官网当前 token 请求不应包含 %s: %v", stale, query)
		}
	}
}

// TestRefreshTokenUsesReferenceFingerprint 封装TestRefresh令牌UsesReferenceFingerprint业务协调。
func TestRefreshTokenUsesReferenceFingerprint(t *testing.T) {
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{UserAgent: "playwright-native-ua", SecChUA: `"Chromium";v="999"`})
	// gotUA、gotSecChUA 用于本次流程后续判断的gotUA、gotSecChUA
	var gotUA, gotSecChUA string
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotSecChUA = r.Header.Get("sec-ch-ua")
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"fingerprint"}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	if // err 用于本次流程后续判断的err
	_, err := client.RefreshTokenWithDeviceIDContext(context.Background(), testCookiesWithUnb, "did"); err != nil {
		t.Fatal(err)
	}
	if gotUA != "playwright-native-ua" || gotSecChUA != `"Chromium";v="999"` {
		t.Fatalf("token fingerprint mismatch: ua=%q sec-ch-ua=%q", gotUA, gotSecChUA)
	}
}

// TestRefreshTokenContextCanceled: 连续重试期间仍遵守调用方 ctx。
func TestRefreshTokenContextCanceled(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 用于本次流程后续判断的尝试次数
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_999", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// 在第二次请求开始后取消，避免依赖固定重试延迟。
	go func() {
		for requests.Load() < 2 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(ctx, testCookiesWithUnb)
	if err == nil {
		t.Fatalf("expected ctx cancel error, got nil")
	}
	// 应是 context.Canceled 而非"登录凭证已失效"
	if strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("不应是凭证失效错误: %v", err)
	}
}

// TestRefreshTokenRefreshWrapper: RefreshToken（无 Context）调用等价于 RefreshTokenContext。
func TestRefreshTokenRefreshWrapper(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"wrapped"}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.RefreshToken(testCookiesWithUnb)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.AccessToken != "wrapped" {
		t.Fatalf("AccessToken=%q", result.AccessToken)
	}
}

// TestParseAccessTokenExpireAtSupportsOfficialTimestampForms 封装TestParseAccess令牌ExpireAtSupportsOfficialTimestampForms业务协调。
func TestParseAccessTokenExpireAtSupportsOfficialTimestampForms(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_700_000_000, 0)
	// tests 用于本次流程后续判断的tests
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "unix milliseconds", raw: `1700007200000`, want: now.Add(2 * time.Hour).Unix()},
		{name: "unix seconds string", raw: `"1700007200"`, want: now.Add(2 * time.Hour).Unix()},
		{name: "relative seconds", raw: `7200`, want: now.Add(2 * time.Hour).Unix()},
		{name: "relative milliseconds", raw: `7200000`, want: now.Add(2 * time.Hour).Unix()},
	}
	// tt 表示当前遍历过程中的tt
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if // got 用于本次流程后续判断的got
			got := parseAccessTokenExpireAt(json.RawMessage(tt.raw), now); got != tt.want {
				t.Fatalf("expireAt=%d want=%d", got, tt.want)
			}
		})
	}
}

// TestParseAccessTokenExpireAtRejectsMissingValue 封装TestParseAccess令牌ExpireAtRejectsMissing值业务协调。
func TestParseAccessTokenExpireAtRejectsMissingValue(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := parseAccessTokenExpireAt(nil, time.Now()); got != 0 {
		t.Fatalf("expireAt=%d want=0", got)
	}
}
