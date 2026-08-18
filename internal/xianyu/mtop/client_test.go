package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewClientUsesGoHTTPByDefault 封装TestNewClientUsesGoHTTPByDefault业务协调。
func TestNewClientUsesGoHTTPByDefault(t *testing.T) {
	if // client 用于本次流程后续判断的client
	client := NewClient(); client == nil {
		t.Fatal("默认 MTOP 客户端为空")
	}
}

// TestRefreshTokenRetriesOnceWithUpdatedCookie 封装TestRefresh令牌RetriesOnceWithUpdated登录凭证业务协调。
func TestRefreshTokenRetriesOnceWithUpdatedCookie(t *testing.T) {
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
		if !strings.Contains(r.Header.Get("Cookie"), "_m_h5_tk=newtoken_999") {
			t.Errorf("第二次请求未携带更新后的 Cookie: %s", r.Header.Get("Cookie"))
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"access-1"}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// result、err 用于本次流程后续判断的result、err
	result, err := client.RefreshTokenContext(ctx, "unb=123; _m_h5_tk=oldtoken_1;")
	if err != nil {
		t.Fatalf("RefreshTokenContext: %v", err)
	}
	if result.AccessToken != "access-1" {
		t.Fatalf("AccessToken=%q", result.AccessToken)
	}
	if requests.Load() != 2 {
		t.Fatalf("请求次数=%d want 2", requests.Load())
	}
}

// TestReadMTopBodyRejectsOversizedResponse 封装TestReadMTop请求体RejectsOversized响应业务协调。
func TestReadMTopBodyRejectsOversizedResponse(t *testing.T) {
	// resp 用于本次流程后续判断的resp
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxMTopResponseBytes+1)))}
	defer resp.Body.Close()
	if // err 用于本次流程后续判断的err
	_, err := readMTopBody(resp); err == nil {
		t.Fatal("oversized mtop response should fail")
	}
}

// TestRefreshTokenUsesOfficialAttemptLimitWithoutUpdatedCookie 封装TestRefresh令牌UsesOfficial尝试次数上限WithoutUpdated登录凭证业务协调。
func TestRefreshTokenUsesOfficialAttemptLimitWithoutUpdatedCookie(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.RefreshTokenContext(context.Background(), "unb=123; _m_h5_tk=oldtoken_1;")
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != officialMTopMaxAttempts {
		t.Fatalf("请求次数=%d want %d", requests.Load(), officialMTopMaxAttempts)
	}
}

// TestConsignRetriesWithUpdatedTokenCookie 封装TestConsignRetriesWithUpdated令牌登录凭证业务协调。
func TestConsignRetriesWithUpdatedTokenCookie(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 用于本次流程后续判断的尝试次数
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_999", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		if !strings.Contains(r.Header.Get("Cookie"), "_m_h5_tk=newtoken_999") {
			t.Errorf("重试未携带更新后的 Cookie: %s", r.Header.Get("Cookie"))
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// ok、ret、updated、err 用于本次流程后续判断的ok、ret、updated、err
	ok, ret, updated, err := client.ConsignContext(ctx, "unb=123; _m_h5_tk=oldtoken_1;", "order-1")
	if err != nil {
		t.Fatalf("ConsignContext: %v", err)
	}
	if !ok || len(ret) == 0 || !strings.Contains(ret[0], "SUCCESS") {
		t.Fatalf("ok=%v ret=%v", ok, ret)
	}
	if !strings.Contains(updated, "_m_h5_tk=newtoken_999") {
		t.Fatalf("updatedCookies=%q", updated)
	}
	if requests.Load() != 2 {
		t.Fatalf("请求次数=%d want 2", requests.Load())
	}
}

// TestConsignDoesNotRetryNonTokenFailure 封装TestConsignDoesNot重试Non令牌Failure业务协调。
func TestConsignDoesNotRetryNonTokenFailure(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	// ok、ret、err 用于本次流程后续判断的ok、ret、err
	ok, ret, _, err := client.ConsignContext(context.Background(), "unb=123; _m_h5_tk=token_1;", "order-1")
	if err != nil || ok || len(ret) == 0 {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("请求次数=%d want 1", requests.Load())
	}
}

// TestFetchOrderDetailParsesPaidAmountAndQuantity 封装TestFetch订单DetailParsesPaidAmountAndQuantity业务协调。
func TestFetchOrderDetailParsesPaidAmountAndQuantity(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"2"},"priceInfo":{"amount":{"value":"12.50"}}}}]}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.FetchOrderDetail(context.Background(), "unb=123; _m_h5_tk=token_1;", "order-1")
	if err != nil {
		t.Fatalf("FetchOrderDetail: %v", err)
	}
	if result.Amount != "12.50" || result.Quantity != "2" || result.OrderStatus != "3" {
		t.Fatalf("result=%+v", result)
	}
}

// TestHasMTopSuccess 封装TestHasMTopSuccess业务协调。
func TestHasMTopSuccess(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		ret  []string
		want bool
	}{
		{[]string{"SUCCESS::调用成功"}, true},
		{[]string{"FAIL_SYS_TOKEN_EXOIRED::令牌过期", "SUCCESS::调用成功"}, true},
		{[]string{"FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"}, false},
		{nil, false},
		{[]string{}, false},
		{[]string{"SUCCESS_OTHER::其他成功"}, false},
	}
	// i、c 表示当前遍历过程中的i、c
	for i, c := range cases {
		if // got 用于本次流程后续判断的got
		got := hasMTopSuccess(c.ret); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

// TestIsTokenExpiredRet 封装TestIs令牌ExpiredRet业务协调。
func TestIsTokenExpiredRet(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		ret  []string
		want bool
	}{
		{[]string{"FAIL_SYS_TOKEN_EXOIRED::令牌过期"}, true},
		{[]string{"FAIL_SYS_TOKEN_EXPIRED::令牌过期"}, true},
		{[]string{"FAIL_SYS_SESSION_EXPIRED::会话过期"}, false},
		{[]string{"FAIL_SYS_USER_VALIDATE::非法请求TOKEN"}, false},
		{[]string{"SUCCESS::调用成功"}, false},
		{[]string{"FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"}, false},
		{nil, false},
		{[]string{}, false},
	}
	// i、c 表示当前遍历过程中的i、c
	for i, c := range cases {
		if // got 用于本次流程后续判断的got
		got := isTokenExpiredRet(c.ret); got != c.want {
			t.Errorf("case %d: got %v want %v (ret=%v)", i, got, c.want, c.ret)
		}
	}
}

// TestSessionExpiredRetIsSeparateFromTokenExpiry 封装Test会话ExpiredRetIsSeparateFrom令牌Expiry业务协调。
func TestSessionExpiredRetIsSeparateFromTokenExpiry(t *testing.T) {
	// ret 用于本次流程后续判断的ret
	ret := []string{"FAIL_SYS_SESSION_EXPIRED::会话过期"}
	if !isSessionExpiredRet(ret) {
		t.Fatal("session expiry must be recognized")
	}
	if isTokenExpiredRet(ret) {
		t.Fatal("session expiry must not enter token retry path")
	}
	// err 用于本次流程后续判断的err
	err := sessionExpiredError("test API", ret)
	if !IsSessionExpiredErr(fmt.Errorf("wrapped: %w", err)) {
		t.Fatalf("typed wrapped error not recognized: %v", err)
	}
}

// TestIsSessionExpiredErr 封装TestIs会话ExpiredErr业务协调。
func TestIsSessionExpiredErr(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("fail_sys_session_expired"), true},
		{fmt.Errorf("Session过期"), true},
		{fmt.Errorf("token API 登录凭证已失效: ret=[]"), true},
		{fmt.Errorf("订单详情接口返回非成功"), false},
		{nil, false},
		{fmt.Errorf("consign 请求失败: connection refused"), false},
	}
	// i、c 表示当前遍历过程中的i、c
	for i, c := range cases {
		if // got 用于本次流程后续判断的got
		got := IsSessionExpiredErr(c.err); got != c.want {
			t.Errorf("case %d: got %v want %v (err=%v)", i, got, c.want, c.err)
		}
	}
}

// TestMtopString 封装TestMtopString业务协调。
func TestMtopString(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{float64(123), "123"},
		{float64(12.99), "12"},
		{int(456), "456"},
		{json.Number("789"), "789"},
		{nil, ""},
		{true, ""},
		{[]string{"a"}, ""},
	}
	// i、c 表示当前遍历过程中的i、c
	for i, c := range cases {
		if // got 用于本次流程后续判断的got
		got := mtopString(c.in); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

// TestMtopInt 封装TestMtopInt业务协调。
func TestMtopInt(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		in   any
		want int
	}{
		{float64(123), 123},
		{float64(12.99), 12},
		{int(456), 456},
		{"789", 789},
		{"abc", 0},
		{json.Number("42"), 42},
		{nil, 0},
		{true, 0},
	}
	// i、c 表示当前遍历过程中的i、c
	for i, c := range cases {
		if // got 用于本次流程后续判断的got
		got := mtopInt(c.in); got != c.want {
			t.Errorf("case %d: got %d want %d", i, got, c.want)
		}
	}
}

// TestTruncate 封装TestTruncate业务协调。
func TestTruncate(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 0, "..."},
	}
	// i、c 表示当前遍历过程中的i、c
	for i, c := range cases {
		if // got 用于本次流程后续判断的got
		got := truncate(c.s, c.n); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

// TestMergeSetCookieMultiple 封装TestMergeSet登录凭证Multiple业务协调。
func TestMergeSetCookieMultiple(t *testing.T) {
	// orig 用于本次流程后续判断的orig
	orig := "unb=123; _m_h5_tk=oldtoken_1; foo=bar"
	// current 用于本次流程后续判断的current
	current := map[string]string{
		"unb":      "123",
		"_m_h5_tk": "oldtoken_1",
		"foo":      "bar",
	}
	// resp 用于本次流程后续判断的resp
	resp := &http.Response{Header: http.Header{}}
	resp.Header["Set-Cookie"] = []string{
		"_m_h5_tk=newtoken_999; Path=/; Domain=.goofish.com",
		"newkey=newval; Path=/",
		"empty=; Path=/",
	}
	// got 用于本次流程后续判断的got
	got := mergeSetCookie(orig, current, resp)
	// 必须含更新后的两个已知字段与新增字段
	if !strings.Contains(got, "_m_h5_tk=newtoken_999") {
		t.Errorf("missing updated token: %q", got)
	}
	if !strings.Contains(got, "newkey=newval") {
		t.Errorf("missing new cookie: %q", got)
	}
	if !strings.Contains(got, "unb=123") {
		t.Errorf("missing preserved cookie: %q", got)
	}
	if !strings.Contains(got, "empty=") {
		t.Errorf("missing empty-value cookie: %q", got)
	}
}

// TestMergeSetCookieNoSetCookie 封装TestMergeSet登录凭证NoSet登录凭证业务协调。
func TestMergeSetCookieNoSetCookie(t *testing.T) {
	// orig 用于本次流程后续判断的orig
	orig := "unb=123; _m_h5_tk=token_1"
	// current 用于本次流程后续判断的current
	current := map[string]string{"unb": "123", "_m_h5_tk": "token_1"}
	// resp 用于本次流程后续判断的resp
	resp := &http.Response{Header: http.Header{}}
	if // got 用于本次流程后续判断的got
	got := mergeSetCookie(orig, current, resp); got != orig {
		t.Errorf("no Set-Cookie should return orig, got %q", got)
	}
}

// TestMergeSetCookieMalformedIgnored 封装TestMergeSet登录凭证MalformedIgnored业务协调。
func TestMergeSetCookieMalformedIgnored(t *testing.T) {
	// orig 用于本次流程后续判断的orig
	orig := "unb=123"
	// current 用于本次流程后续判断的current
	current := map[string]string{"unb": "123"}
	// resp 用于本次流程后续判断的resp
	resp := &http.Response{Header: http.Header{}}
	resp.Header["Set-Cookie"] = []string{
		"; Path=/",       // 无 name=value
		"=noval; Path=/", // 空 name
	}
	// 仅有无效 Set-Cookie，应视为未变化返回 orig
	if got := mergeSetCookie(orig, current, resp); got != orig {
		t.Errorf("malformed only should return orig, got %q", got)
	}
}

// TestMergeSetCookieMaxAgeOverridesPastExpires 封装TestMergeSet登录凭证MaxAgeOverridesPastExpires业务协调。
func TestMergeSetCookieMaxAgeOverridesPastExpires(t *testing.T) {
	// orig 用于本次流程后续判断的orig
	orig := "session=old"
	// current 用于本次流程后续判断的current
	current := map[string]string{"session": "old"}
	// resp 用于本次流程后续判断的resp
	resp := &http.Response{Header: http.Header{
		"Set-Cookie": {"session=fresh; Max-Age=3600; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/"},
	}}
	if // got 用于本次流程后续判断的got
	got := mergeSetCookie(orig, current, resp); !strings.Contains(got, "session=fresh") {
		t.Fatalf("positive Max-Age must override past Expires: %q", got)
	}
}

// TestSleepCtx 封装TestSleepCtx业务协调。
func TestSleepCtx(t *testing.T) {
	// d <= 0 直接返回
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Errorf("sleepCtx(0)=%v", err)
	}
	if // err 用于本次流程后续判断的err
	err := sleepCtx(context.Background(), -1); err != nil {
		t.Errorf("sleepCtx(-1)=%v", err)
	}
	// 正常 sleep
	if err := sleepCtx(context.Background(), 5*time.Millisecond); err != nil {
		t.Errorf("sleepCtx(5ms)=%v", err)
	}
	// ctx 已取消立即返回 ctx.Err()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if // err 用于本次流程后续判断的err
	err := sleepCtx(ctx, time.Second); err != context.Canceled {
		t.Errorf("sleepCtx(canceled)=%v want %v", err, context.Canceled)
	}
}

// TestNewClient 封装TestNewClient业务协调。
func TestNewClient(t *testing.T) {
	// c 用于本次流程后续判断的c
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	// 零值 ClientImpl 应实现 Client 接口
	var _ Client = c
}
