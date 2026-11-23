package mtop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestMTopResponseFailureClassifiesAndRedacts 验证统一错误分类覆盖各类 MTOP 失败，并且日志与用户错误均不泄露凭证。
func TestMTopResponseFailureClassifiesAndRedacts(t *testing.T) {
	// logs 收集结构化日志，验证平台错误码和分类可供排障检索。
	var logs bytes.Buffer
	// client 使用测试日志器，避免把合成失败写入全局日志。
	client := &ClientImpl{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	// cases 覆盖风控、会话、Token、HTTP、解析和普通业务失败分类。
	cases := []struct {
		name   string
		kind   MTopErrorKind
		code   string
		status int
		detail string
	}{
		{name: "risk", kind: MTopErrorRiskVerification, code: "FAIL_SYS_USER_VALIDATE::安全校验", status: http.StatusOK},
		{name: "session", kind: MTopErrorSessionExpired, code: "FAIL_SYS_SESSION_EXPIRED::会话过期", status: http.StatusOK},
		{name: "token", kind: MTopErrorTokenExpired, code: "FAIL_SYS_TOKEN_EXPIRED::令牌过期", status: http.StatusOK},
		{name: "system", kind: MTopErrorSystem, code: "FAIL_SYS_INTERNAL_ERROR::内部错误", status: http.StatusOK},
		{name: "http", kind: MTopErrorHTTP, code: "FAIL_SYS_GATEWAY::网关错误", status: http.StatusBadGateway},
		{name: "decode", kind: MTopErrorDecode, status: http.StatusOK, detail: "JSON 解析失败"},
		{name: "business", kind: MTopErrorBusiness, code: "FAIL_BIZ_ORDER::订单错误", status: http.StatusOK},
	}
	// testCase 验证当前失败类型的错误链、诊断内容和日志字段。
	for _, testCase := range cases {
		// ret 只模拟平台错误标记，不包含真实账号信息。
		ret := []string(nil)
		if testCase.code != "" {
			ret = []string{testCase.code + " cookie=_m_h5_tk=secret access_token=secret"}
		}
		// err 保存统一失败分类结果。
		err := client.mtopResponseFailure("订单列表接口", testCase.status, ret, testCase.detail+" private-marker")
		// kind、ok 保存错误链中解析出的失败分类。
		kind, ok := MTopErrorKindOf(err)
		if !ok || kind != testCase.kind {
			t.Fatalf("%s kind=%q ok=%v want %q", testCase.name, kind, ok, testCase.kind)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-marker") {
			t.Fatalf("%s error leaked sensitive text: %s", testCase.name, err)
		}
	}
	// output 保存结构化日志文本，验证日志包含可检索类别但不包含敏感值。
	output := logs.String()
	if !strings.Contains(output, `"category":"token_expired"`) || !strings.Contains(output, "FAIL_SYS_TOKEN_EXPIRED") {
		t.Fatalf("structured MTOP failure log incomplete: %s", output)
	}
	if strings.Contains(output, "secret") || strings.Contains(output, "private-marker") {
		t.Fatalf("structured MTOP failure log leaked sensitive text: %s", output)
	}
}

// TestMTopResponseFailureWithCausePreservesErrorChain 验证统一 MTOP 错误保留底层取消原因但不泄露原因文本。
func TestMTopResponseFailureWithCausePreservesErrorChain(t *testing.T) {
	// cause 保存模拟 HTTP 响应读取阶段的取消错误及敏感诊断文本。
	cause := fmt.Errorf("响应读取被取消，cookie=_m_h5_tk=secret")
	// client 使用默认安全日志器完成错误构造。
	client := &ClientImpl{}
	// err 保存带底层原因的统一 MTOP 错误。
	err := client.mtopResponseFailureWithCause("订单列表接口", http.StatusOK, nil, "读取响应失败", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("底层错误未保留: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("错误文本泄露底层敏感原因: %v", err)
	}
}

// TestNewClientUsesGoHTTPByDefault 封装TestNewClientUsesGoHTTPByDefault业务协调。
func TestNewClientUsesGoHTTPByDefault(t *testing.T) {
	if // client 用于本次流程后续判断的client
	client := NewClient(); client == nil {
		t.Fatal("默认 MTOP 客户端为空")
	}
}

// TestMTopTokenCookieChangedIgnoresUnrelatedCookieChanges 验证签名令牌判断只关注非空 _m_h5_tk 轮换。
func TestMTopTokenCookieChangedIgnoresUnrelatedCookieChanges(t *testing.T) {
	// cases 覆盖普通 Cookie 变化、签名令牌轮换、令牌缺失和相同值重复写回。
	cases := []struct {
		name     string
		previous string
		current  string
		changed  bool
	}{
		{name: "unrelated", previous: "_m_h5_tk=token-a; sdkSilent=1", current: "_m_h5_tk=token-a; sdkSilent=2", changed: false},
		{name: "expiry-only", previous: "_m_h5_tk=token-a_100", current: "_m_h5_tk=token-a_200", changed: false},
		{name: "rotated", previous: "_m_h5_tk=token-a", current: "_m_h5_tk=token-b", changed: true},
		{name: "missing", previous: "_m_h5_tk=token-a", current: "sdkSilent=2", changed: false},
		{name: "same", previous: "_m_h5_tk=token-a", current: "_m_h5_tk=token-a", changed: false},
	}
	// testCase 逐项承载签名令牌变化样例，避免把普通 Cookie 更新误判为恢复。
	for _, testCase := range cases {
		// changed 保存当前样例是否应被识别为有效签名轮换。
		changed := MTopTokenCookieChanged(testCase.previous, testCase.current)
		if changed != testCase.changed {
			t.Errorf("%s changed=%v want %v", testCase.name, changed, testCase.changed)
		}
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
	if !IsMTopTokenExpiredErr(err) || IsSessionExpiredErr(err) || !strings.Contains(err.Error(), "重试次数已耗尽") {
		t.Fatalf("Token 刷新耗尽必须保留 Token 分类: %v", err)
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
	if err != nil || ok || len(ret) == 0 || !strings.Contains(ret[0], "订单状态错误") {
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
		{[]string{"SID_INVALID::会话无效"}, false},
		{[]string{"AUTH_REJECT::认证拒绝"}, false},
		{[]string{"NEED_LOGIN::需要登录"}, false},
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

// TestOfficialSessionExpiredRetCodes 验证官网 lib-mtop 统一识别的四类 Session 失效码。
func TestOfficialSessionExpiredRetCodes(t *testing.T) {
	// cases 保存官网中会被归入 SESSION_EXPIRED 的平台返回码。
	cases := []string{
		"SESSION_EXPIRED",
		"SID_INVALID",
		"AUTH_REJECT",
		"NEED_LOGIN",
		"SESSION_EXPIRED::会话过期",
		"SID_INVALID::会话无效",
		"AUTH_REJECT::认证拒绝",
		"NEED_LOGIN::需要登录",
	}
	// ret 表示当前待分类的平台返回码。
	for _, ret := range cases {
		if !IsSessionExpiredErr(errors.New(ret)) {
			t.Errorf("非类型化官方 Session 错误未识别: %q", ret)
		}
		if !isSessionExpiredRet([]string{ret}) {
			t.Errorf("官方 Session 失效码未识别: %q", ret)
		}
		// failure 保存统一错误分类，确认这些返回码不会误入 Token 过期路径。
		failure := (&ClientImpl{}).mtopResponseFailure("test", http.StatusOK, []string{ret}, "平台返回失败")
		if !IsSessionExpiredErr(failure) || IsMTopTokenExpiredErr(failure) {
			t.Errorf("官方 Session 失效码分类错误: ret=%q err=%v", ret, failure)
		}
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

// TestIsCredentialRefreshableErr 验证仅明确 Session 失效允许账号级恢复；t 检查兼容分类入口。
func TestIsCredentialRefreshableErr(t *testing.T) {
	// tokenErr 是只应在 MTOP 内部刷新的签名 Token 失效错误。
	tokenErr := &MTopResponseError{Kind: MTopErrorTokenExpired, API: "token", HTTPStatus: http.StatusOK}
	// sessionErr 是需要协议续期或重新登录的 Session 失效错误。
	sessionErr := &SessionExpiredError{API: "session", Ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}}
	if IsCredentialRefreshableErr(tokenErr) || !IsCredentialRefreshableErr(sessionErr) {
		t.Fatal("仅 Session 失效可以进入账号级凭证恢复入口")
	}
	if IsCredentialRefreshableErr(errors.New("普通业务失败")) {
		t.Fatal("普通业务失败不应触发凭证恢复")
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
		{float64(12.99), "12.99"},
		{float64(92.5), "92.5"},
		{float64(0.1), "0.1"},
		{float64(-3), "-3"},
		{float64(1e19), "10000000000000000000"},
		{math.NaN(), ""},
		{math.Inf(1), ""},
		{math.Inf(-1), ""},
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
