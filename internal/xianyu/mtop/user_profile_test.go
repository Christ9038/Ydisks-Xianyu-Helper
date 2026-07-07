package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// user_profile.go 的 fetchUserProfileOnce 硬编码 UserPageNavAPI 常量（无 URL 字段可注入），
// 故用 rewriteTransport 把所有请求改写到本地 httptest server 来覆盖整链。

// rewriteTransport 把所有请求改写到 target URL（保留原 query 与 body）。
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	target, err := url.Parse(t.target)
	if err != nil {
		return nil, err
	}
	target.RawQuery = req.URL.RawQuery
	req.URL = target
	req.Host = target.Host
	rt := t.base
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(req)
}

// TestFetchUserProfileSuccess: 成功解析 module.base。
func TestFetchUserProfileSuccess(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "fresh_8", Path: "/"})
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"module":{"base":{"displayName":"小明","displayNick":"小明昵称","avatar":"https://cdn/a.jpg"}}}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchUserProfile(context.Background(), consignCookies)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Nickname != "小明" || res.DisplayNick != "小明昵称" || res.AvatarURL != "https://cdn/a.jpg" {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(res.UpdatedCookies, "fresh_8") {
		t.Fatalf("UpdatedCookies=%q", res.UpdatedCookies)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
}

// TestFetchUserProfileNonSuccessRet: 非 token 过期失败 ret 报错。
func TestFetchUserProfileNonSuccessRet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_USER_NOT_FOUND::用户不存在"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchUserProfile(context.Background(), consignCookies)
	if err == nil || !strings.Contains(err.Error(), "账号资料接口返回非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchUserProfileParseFailure: 响应非 JSON。
func TestFetchUserProfileParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not-json{`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchUserProfile(context.Background(), consignCookies)
	if err == nil || !strings.Contains(err.Error(), "解析账号资料响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchUserProfileRequestError: 网络层错误。
func TestFetchUserProfileRequestError(t *testing.T) {
	// 指向一个不可达地址
	rt := &rewriteTransport{base: http.DefaultTransport, target: "http://127.0.0.1:1"}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 200 * time.Millisecond}}
	_, err := client.FetchUserProfile(context.Background(), consignCookies)
	if err == nil {
		t.Fatalf("expected err")
	}
}

// TestFetchUserProfileTokenExpiredRetriesWithSetCookie: token 过期 + Set-Cookie，二次成功。
func TestFetchUserProfileTokenExpiredRetriesWithSetCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_9", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"module":{"base":{"displayName":"小明"}}}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.FetchUserProfile(ctx, consignCookies)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Nickname != "小明" {
		t.Fatalf("Nickname=%q", res.Nickname)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d want 2", requests.Load())
	}
}

// ---- parseUserProfile 直接覆盖解析分支 ----

// TestFetchUserProfileRefreshTokenFailure: token 过期无 Set-Cookie，RefreshToken 失败时报错。
func TestFetchUserProfileRefreshTokenFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// profile 持续返回 token 过期且不 Set-Cookie；token API 也返回失败
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchUserProfile(context.Background(), consignCookies)
	if err == nil || !strings.Contains(err.Error(), "刷新 mtop token 失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchUserProfileRetryExhausted: token 过期但每次都通过新 Set-Cookie 重试，4 次后耗尽。
// 关键：每次下发的 Cookie 值都不同，使 updatedCookies != currentCookies，跳过 RefreshToken 走 continue。
func TestFetchUserProfileRetryExhausted(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		// 每次下发不同的 _m_h5_tk 值，确保 updatedCookies != currentCookies
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: fmt.Sprintf("tok_%d", n), Path: "/"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 15 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := client.FetchUserProfile(ctx, consignCookies)
	if err == nil || !strings.Contains(err.Error(), "账号资料接口 token 重试失败") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 4 {
		t.Fatalf("requests=%d want 4（重试上限）", requests.Load())
	}
}
func TestParseUserProfileFull(t *testing.T) {
	data := map[string]any{
		"module": map[string]any{
			"base": map[string]any{
				"displayName": "小明",
				"displayNick": "小明昵称",
				"avatar":      "https://cdn/avatar.jpg",
			},
		},
	}
	p := parseUserProfile(data)
	if p.Nickname != "小明" || p.DisplayNick != "小明昵称" || p.AvatarURL != "https://cdn/avatar.jpg" {
		t.Fatalf("p=%+v", p)
	}
}

// TestParseUserProfileNicknameFallsBackToDisplayNick: displayName 为空时用 displayNick。
func TestParseUserProfileNicknameFallsBackToDisplayNick(t *testing.T) {
	data := map[string]any{
		"module": map[string]any{
			"base": map[string]any{
				"displayNick": "fallbackNick",
			},
		},
	}
	p := parseUserProfile(data)
	if p.Nickname != "fallbackNick" || p.DisplayNick != "fallbackNick" {
		t.Fatalf("p=%+v", p)
	}
}

// TestParseUserProfileEmptyBase: base 为 nil 时返回空结构。
func TestParseUserProfileEmptyBase(t *testing.T) {
	p := parseUserProfile(map[string]any{"module": map[string]any{}})
	if p == nil || p.Nickname != "" || p.AvatarURL != "" {
		t.Fatalf("p=%+v want empty", p)
	}
}

// TestParseUserProfileNoModule: module 缺失也返回空结构。
func TestParseUserProfileNoModule(t *testing.T) {
	p := parseUserProfile(map[string]any{})
	if p == nil || p.Nickname != "" {
		t.Fatalf("p=%+v want empty", p)
	}
}

// TestParseUserProfileTrimsWhitespace: 字段带空白被 TrimSpace。
func TestParseUserProfileTrimsWhitespace(t *testing.T) {
	data := map[string]any{
		"module": map[string]any{
			"base": map[string]any{
				"displayName": "  小明  ",
				"avatar":      "  https://cdn/a.jpg  ",
			},
		},
	}
	p := parseUserProfile(data)
	if p.Nickname != "小明" || p.AvatarURL != "https://cdn/a.jpg" {
		t.Fatalf("p=%+v", p)
	}
}

// TestParseUserProfileNumericAvatar: 非字符串字段经 mtopString 转换。
func TestParseUserProfileNumericAvatar(t *testing.T) {
	data := map[string]any{
		"module": map[string]any{
			"base": map[string]any{
				"displayName": float64(12345),
			},
		},
	}
	p := parseUserProfile(data)
	if p.Nickname != "12345" {
		t.Fatalf("Nickname=%q want 12345", p.Nickname)
	}
}

// TestBuildUserPageNavQuery: 验证 query 拼接与编码。
func TestBuildUserPageNavQuery(t *testing.T) {
	q := buildUserPageNavQuery("1000", "SIGN")
	if !strings.Contains(q, "t=1000") || !strings.Contains(q, "sign=SIGN") ||
		!strings.Contains(q, "api=mtop.idle.web.user.page.nav") ||
		!strings.Contains(q, "spm_cnt=a21ybx.home.0.0") {
		t.Fatalf("query=%q 缺字段", q)
	}
}
