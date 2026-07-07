package mtop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const consignCookies = "unb=123; _m_h5_tk=token_1;"

// TestConsignSuccess: ret SUCCESS 直接成功，无需重试。
func TestConsignSuccess(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	ok, ret, updated, err := client.ConsignContext(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !ok || len(ret) == 0 || !strings.Contains(ret[0], "SUCCESS") {
		t.Fatalf("ok=%v ret=%v", ok, ret)
	}
	if updated != consignCookies {
		t.Fatalf("无 Set-Cookie 时 updated 应保持原样: %q", updated)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
}

// TestConsignWrapper: Consign（无 Context）等价于 ConsignContext。
func TestConsignWrapper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	ok, ret, _, err := client.Consign(consignCookies, "order-1")
	if err != nil || !ok || len(ret) == 0 {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
}

// TestConsignRetFailure: 非 token 过期的失败 ret，不重试，返回 ok=false。
func TestConsignRetFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	ok, ret, _, err := client.ConsignContext(context.Background(), consignCookies, "order-1")
	if err != nil || ok || len(ret) == 0 {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1（非 token 过期不应重试）", requests.Load())
	}
}

// TestConsignMergesSetCookie: 成功响应里的 Set-Cookie 应合并进 updated。
func TestConsignMergesSetCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "fresh_token", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "extra", Value: "v", Path: "/"})
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	_, _, updated, err := client.ConsignContext(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(updated, "_m_h5_tk=fresh_token") || !strings.Contains(updated, "extra=v") {
		t.Fatalf("updated=%q 应含两个合并后的 Cookie", updated)
	}
}

// TestConsignRequestError: 网络错误直接返回 err。
func TestConsignRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	_, _, _, err := client.ConsignContext(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "consign 请求失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestConsignParseFailure: 响应非 JSON 解析失败。
func TestConsignParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not-json{`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	_, _, _, err := client.ConsignContext(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "解析 consign 响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestConsignTokenExpiredNoCookieRefreshFails: token 过期且响应无 Set-Cookie 时，
// 会调用 RefreshTokenContext 刷新；刷新失败（凭证失效）应返回 err。
func TestConsignTokenExpiredNoCookieRefreshFails(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// token 过期，但不 Set-Cookie
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/", TokenURL: server.URL + "/"}
	// 使用 token 过期但无新 Cookie 的场景：RefreshTokenContext 会返回"登录凭证已失效"
	_, _, _, err := client.ConsignContext(context.Background(), consignCookies, "order-1")
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	// consign 的 RefreshToken 失败包装 或 token 重试失败
	if !strings.Contains(err.Error(), "consign token 过期且刷新失败") &&
		!strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("err=%v", err)
	}
}

// TestConsignTokenExpiredThenRefreshSucceeds: token 过期无 Set-Cookie，
// 但 RefreshToken 刷新成功（返回新 Cookie），第二次 consign 成功。
func TestConsignTokenExpiredThenRefreshSucceeds(t *testing.T) {
	var consignReqs atomic.Int32
	var tokenReqs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 通过 URL 路径区分 consign / token 请求
		if strings.Contains(r.URL.Path, "token") || r.URL.Query().Get("api") == "mtop.taobao.idlemessage.pc.login.token" {
			tokenReqs.Add(1)
			// token 接口返回成功并下发新 Cookie
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "refreshtoken_42", Path: "/"})
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"access-1"}}`)
			return
		}
		// consign
		attempt := consignReqs.Add(1)
		if attempt == 1 {
			// token 过期，不 Set-Cookie（强制走 RefreshToken）
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/c", TokenURL: server.URL + "/t"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, _, updated, err := client.ConsignContext(ctx, consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !ok {
		t.Fatalf("ok=false want true")
	}
	if !strings.Contains(updated, "refreshtoken_42") {
		t.Fatalf("updated=%q 应含刷新后的 token", updated)
	}
}

// TestConsignContextCanceled: ctx 取消应中止重试。
func TestConsignContextCanceled(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// 不 Set-Cookie，触发 RefreshToken 重试路径
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/", TokenURL: server.URL + "/"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 已取消
	_, _, _, err := client.ConsignContext(ctx, consignCookies, "order-1")
	if err == nil {
		t.Fatalf("expected err")
	}
	// ctx 取消可能从 sleepCtx 或 RefreshToken 路径返回
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

// TestConsignRetryExhausted: token 过期但每次下发不同 Set-Cookie，4 次重试耗尽返回 ok=false。
func TestConsignRetryExhausted(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: fmt.Sprintf("tok_%d", n), Path: "/"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, ret, _, err := client.ConsignContext(ctx, consignCookies, "order-1")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v want ok=false err=nil", ok, err)
	}
	if len(ret) == 0 || !strings.Contains(ret[0], "FAIL_SYS_TOKEN_EXOIRED") {
		t.Fatalf("ret=%v", ret)
	}
	if requests.Load() != 4 {
		t.Fatalf("requests=%d want 4", requests.Load())
	}
}

// TestBuildConsignQuery: 验证 query 拼接顺序与字段。
func TestBuildConsignQuery(t *testing.T) {
	q := buildConsignQuery("123", "SIGN")
	if !strings.Contains(q, "t=123") || !strings.Contains(q, "sign=SIGN") ||
		!strings.Contains(q, "api=mtop.taobao.idle.logistic.consign.dummy") ||
		!strings.Contains(q, "v=1.0") {
		t.Fatalf("query=%q 缺字段", q)
	}
}
