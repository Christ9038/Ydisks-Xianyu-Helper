package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testCookiesWithUnb = "unb=123; _m_h5_tk=oldtoken_1;"

// TestRefreshTokenWithDeviceIDSuccessOnRetry: 首次返回 token 过期 + Set-Cookie，二次成功。
func TestRefreshTokenWithDeviceIDSuccessOnRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_999", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"access-with-device"}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("不应发请求")
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	_, err := client.RefreshTokenContext(context.Background(), "_m_h5_tk=token_1;")
	if err == nil || !strings.Contains(err.Error(), "cookie 缺少 unb") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenSuccessButAccessTokenEmpty: ret SUCCESS 但 accessToken 为空。
func TestRefreshTokenSuccessButAccessTokenEmpty(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
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
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_VALIDATE_FAIL::参数错误"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 5xx 但 body 是有效 JSON — refreshTokenOnce 只看业务 ret，不看 HTTP 状态码；
		// 由于 ret 解析为非 token 过期失败，应走"返回非成功"分支。
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_INTERNAL_ERROR::内部错误"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "token API 返回非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenParseFailure: 响应非 JSON 解析失败。
func TestRefreshTokenParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not-json{{{`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "解析 token 响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenRequestError: 网络层错误（服务器关闭）。
func TestRefreshTokenRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // 立即关闭，使请求失败

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "token API 请求失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestRefreshTokenExpiredRetNoCookieDoesNotRetry: token 过期但响应未下发新 Cookie，
// 不重试直接报"登录凭证已失效"。
func TestRefreshTokenExpiredRetNoCookieDoesNotRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// 不 Set-Cookie，ret 为 token 过期
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	_, err := client.RefreshTokenContext(context.Background(), testCookiesWithUnb)
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1（无新 Cookie 不应重试）", requests.Load())
	}
}

// TestRefreshTokenContextCanceled: ctx 取消时 sleepCtx 返回 ctx.Err。
func TestRefreshTokenContextCanceled(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_999", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"x"}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	ctx, cancel := context.WithCancel(context.Background())
	// 在第一次请求返回后、sleep 触发前取消
	go func() {
		// 等待第一次请求完成
		for requests.Load() < 1 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"wrapped"}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	result, err := client.RefreshToken(testCookiesWithUnb)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.AccessToken != "wrapped" {
		t.Fatalf("AccessToken=%q", result.AccessToken)
	}
}
