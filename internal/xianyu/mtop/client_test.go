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

// TestNewClientUsesGoHTTPByDefault 负责TestNewClientUsesGoHTTPByDefault相关处理。
func TestNewClientUsesGoHTTPByDefault(t *testing.T) {
	if // client 保存client，供当前处理流程使用
	client := NewClient(); client == nil {
		t.Fatal("默认 MTOP 客户端为空")
	}
}

// TestRefreshTokenRetriesOnceWithUpdatedCookie 负责TestRefresh令牌RetriesOnceWithUpdated登录凭证相关处理。
func TestRefreshTokenRetriesOnceWithUpdatedCookie(t *testing.T) {
	// requests 保存请求列表，供当前处理流程使用
	var requests atomic.Int32
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 保存尝试次数，供当前处理流程使用
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

	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// result、err 保存result、err，供当前处理流程使用
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

// TestReadMTopBodyRejectsOversizedResponse 负责TestReadMTop请求体RejectsOversized响应相关处理。
func TestReadMTopBodyRejectsOversizedResponse(t *testing.T) {
	// resp 保存resp，供当前处理流程使用
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxMTopResponseBytes+1)))}
	defer resp.Body.Close()
	if // err 保存err，供当前处理流程使用
	_, err := readMTopBody(resp); err == nil {
		t.Fatal("oversized mtop response should fail")
	}
}

// TestRefreshTokenUsesOfficialAttemptLimitWithoutUpdatedCookie 负责TestRefresh令牌UsesOfficial尝试次数上限WithoutUpdated登录凭证相关处理。
func TestRefreshTokenUsesOfficialAttemptLimitWithoutUpdatedCookie(t *testing.T) {
	// requests 保存请求列表，供当前处理流程使用
	var requests atomic.Int32
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
	}))
	defer server.Close()

	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	// err 保存err，供当前处理流程使用
	_, err := client.RefreshTokenContext(context.Background(), "unb=123; _m_h5_tk=oldtoken_1;")
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != officialMTopMaxAttempts {
		t.Fatalf("请求次数=%d want %d", requests.Load(), officialMTopMaxAttempts)
	}
}

// TestConsignRetriesWithUpdatedTokenCookie 负责TestConsignRetriesWithUpdated令牌登录凭证相关处理。
func TestConsignRetriesWithUpdatedTokenCookie(t *testing.T) {
	// requests 保存请求列表，供当前处理流程使用
	var requests atomic.Int32
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 保存尝试次数，供当前处理流程使用
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

	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// ok、ret、updated、err 保存ok、ret、updated、err，供当前处理流程使用
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

// TestConsignDoesNotRetryNonTokenFailure 负责TestConsignDoesNot重试Non令牌Failure相关处理。
func TestConsignDoesNotRetryNonTokenFailure(t *testing.T) {
	// requests 保存请求列表，供当前处理流程使用
	var requests atomic.Int32
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"]}`)
	}))
	defer server.Close()

	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	// ok、ret、err 保存ok、ret、err，供当前处理流程使用
	ok, ret, _, err := client.ConsignContext(context.Background(), "unb=123; _m_h5_tk=token_1;", "order-1")
	if err != nil || ok || len(ret) == 0 {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("请求次数=%d want 1", requests.Load())
	}
}

// TestFetchOrderDetailParsesPaidAmountAndQuantity 负责TestFetch订单DetailParsesPaidAmountAndQuantity相关处理。
func TestFetchOrderDetailParsesPaidAmountAndQuantity(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"2"},"priceInfo":{"amount":{"value":"12.50"}}}}]}}`)
	}))
	defer server.Close()

	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// result、err 保存result、err，供当前处理流程使用
	result, err := client.FetchOrderDetail(context.Background(), "unb=123; _m_h5_tk=token_1;", "order-1")
	if err != nil {
		t.Fatalf("FetchOrderDetail: %v", err)
	}
	if result.Amount != "12.50" || result.Quantity != "2" || result.OrderStatus != "3" {
		t.Fatalf("result=%+v", result)
	}
}

// TestHasMTopSuccess 负责TestHasMTopSuccess相关处理。
func TestHasMTopSuccess(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := hasMTopSuccess(c.ret); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

// TestIsTokenExpiredRet 负责TestIs令牌ExpiredRet相关处理。
func TestIsTokenExpiredRet(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := isTokenExpiredRet(c.ret); got != c.want {
			t.Errorf("case %d: got %v want %v (ret=%v)", i, got, c.want, c.ret)
		}
	}
}

// TestSessionExpiredRetIsSeparateFromTokenExpiry 负责Test会话ExpiredRetIsSeparateFrom令牌Expiry相关处理。
func TestSessionExpiredRetIsSeparateFromTokenExpiry(t *testing.T) {
	// ret 保存ret，供当前处理流程使用
	ret := []string{"FAIL_SYS_SESSION_EXPIRED::会话过期"}
	if !isSessionExpiredRet(ret) {
		t.Fatal("session expiry must be recognized")
	}
	if isTokenExpiredRet(ret) {
		t.Fatal("session expiry must not enter token retry path")
	}
	// err 保存err，供当前处理流程使用
	err := sessionExpiredError("test API", ret)
	if !IsSessionExpiredErr(fmt.Errorf("wrapped: %w", err)) {
		t.Fatalf("typed wrapped error not recognized: %v", err)
	}
}

// TestIsSessionExpiredErr 负责TestIs会话ExpiredErr相关处理。
func TestIsSessionExpiredErr(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := IsSessionExpiredErr(c.err); got != c.want {
			t.Errorf("case %d: got %v want %v (err=%v)", i, got, c.want, c.err)
		}
	}
}

// TestMtopString 负责TestMtopString相关处理。
func TestMtopString(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := mtopString(c.in); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

// TestMtopInt 负责TestMtopInt相关处理。
func TestMtopInt(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := mtopInt(c.in); got != c.want {
			t.Errorf("case %d: got %d want %d", i, got, c.want)
		}
	}
}

// TestTruncate 负责TestTruncate相关处理。
func TestTruncate(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := truncate(c.s, c.n); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

// TestMergeSetCookieMultiple 负责TestMergeSet登录凭证Multiple相关处理。
func TestMergeSetCookieMultiple(t *testing.T) {
	// orig 保存orig，供当前处理流程使用
	orig := "unb=123; _m_h5_tk=oldtoken_1; foo=bar"
	// current 保存current，供当前处理流程使用
	current := map[string]string{
		"unb":      "123",
		"_m_h5_tk": "oldtoken_1",
		"foo":      "bar",
	}
	// resp 保存resp，供当前处理流程使用
	resp := &http.Response{Header: http.Header{}}
	resp.Header["Set-Cookie"] = []string{
		"_m_h5_tk=newtoken_999; Path=/; Domain=.goofish.com",
		"newkey=newval; Path=/",
		"empty=; Path=/",
	}
	// got 保存got，供当前处理流程使用
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

// TestMergeSetCookieNoSetCookie 负责TestMergeSet登录凭证NoSet登录凭证相关处理。
func TestMergeSetCookieNoSetCookie(t *testing.T) {
	// orig 保存orig，供当前处理流程使用
	orig := "unb=123; _m_h5_tk=token_1"
	// current 保存current，供当前处理流程使用
	current := map[string]string{"unb": "123", "_m_h5_tk": "token_1"}
	// resp 保存resp，供当前处理流程使用
	resp := &http.Response{Header: http.Header{}}
	if // got 保存got，供当前处理流程使用
	got := mergeSetCookie(orig, current, resp); got != orig {
		t.Errorf("no Set-Cookie should return orig, got %q", got)
	}
}

// TestMergeSetCookieMalformedIgnored 负责TestMergeSet登录凭证MalformedIgnored相关处理。
func TestMergeSetCookieMalformedIgnored(t *testing.T) {
	// orig 保存orig，供当前处理流程使用
	orig := "unb=123"
	// current 保存current，供当前处理流程使用
	current := map[string]string{"unb": "123"}
	// resp 保存resp，供当前处理流程使用
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

// TestMergeSetCookieMaxAgeOverridesPastExpires 负责TestMergeSet登录凭证MaxAgeOverridesPastExpires相关处理。
func TestMergeSetCookieMaxAgeOverridesPastExpires(t *testing.T) {
	// orig 保存orig，供当前处理流程使用
	orig := "session=old"
	// current 保存current，供当前处理流程使用
	current := map[string]string{"session": "old"}
	// resp 保存resp，供当前处理流程使用
	resp := &http.Response{Header: http.Header{
		"Set-Cookie": {"session=fresh; Max-Age=3600; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/"},
	}}
	if // got 保存got，供当前处理流程使用
	got := mergeSetCookie(orig, current, resp); !strings.Contains(got, "session=fresh") {
		t.Fatalf("positive Max-Age must override past Expires: %q", got)
	}
}

// TestSleepCtx 负责TestSleepCtx相关处理。
func TestSleepCtx(t *testing.T) {
	// d <= 0 直接返回
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Errorf("sleepCtx(0)=%v", err)
	}
	if // err 保存err，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := sleepCtx(ctx, time.Second); err != context.Canceled {
		t.Errorf("sleepCtx(canceled)=%v want %v", err, context.Canceled)
	}
}

// TestNewClient 负责TestNewClient相关处理。
func TestNewClient(t *testing.T) {
	// c 保存c，供当前处理流程使用
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	// 零值 ClientImpl 应实现 Client 接口
	var _ Client = c
}
