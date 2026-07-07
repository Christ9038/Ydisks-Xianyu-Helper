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

// TestFetchOrderDetailSuccessWithSpecAndStatus: 完整解析 utArgs/components，含 spec。
func TestFetchOrderDetailSuccessWithSpecAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"4"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"3","specName":"颜色","specValue":"红色"},"priceInfo":{"amount":{"value":"88.00"}}}}]}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	res, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Quantity != "3" || res.SpecName != "颜色" || res.SpecValue != "红色" ||
		res.OrderStatus != "4" || res.Amount != "88.00" {
		t.Fatalf("res=%+v", res)
	}
}

// TestFetchOrderDetailMissingBuyAmountDefaultsTo1: components 无 buyAmount 时 Quantity 默认 "1"。
func TestFetchOrderDetailMissingBuyAmountDefaultsTo1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"components":[{"render":"orderInfoVO","data":{"itemInfo":{}}}]}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	res, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Quantity != "1" {
		t.Fatalf("Quantity=%q want 1", res.Quantity)
	}
}

// TestFetchOrderDetailNonSuccessRet: 非 token 过期的失败 ret。
func TestFetchOrderDetailNonSuccessRet(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_NOT_FOUND::订单不存在"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "订单详情接口返回非成功") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1（非 token 过期不重试）", requests.Load())
	}
}

// TestFetchOrderDetailParseFailure: 响应非 JSON。
func TestFetchOrderDetailParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `broken{`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "解析订单详情响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchOrderDetailRequestError: 网络层错误。
func TestFetchOrderDetailRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "订单详情请求失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchOrderDetailTokenExpiredRetriesWithSetCookie: token 过期 + Set-Cookie，二次成功。
func TestFetchOrderDetailTokenExpiredRetriesWithSetCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_5", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"1"},"priceInfo":{"amount":{"value":"9.90"}}}}]}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.FetchOrderDetail(ctx, consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Amount != "9.90" {
		t.Fatalf("Amount=%q", res.Amount)
	}
	if !strings.Contains(res.UpdatedCookies, "newtoken_5") {
		t.Fatalf("UpdatedCookies=%q", res.UpdatedCookies)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d want 2", requests.Load())
	}
}

// TestFetchOrderDetailTokenExpiredNoCookieRefreshes: token 过期无 Set-Cookie，
// 走 RefreshToken 刷新成功后重试成功。
func TestFetchOrderDetailTokenExpiredNoCookieRefreshes(t *testing.T) {
	var orderReqs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") == "mtop.taobao.idlemessage.pc.login.token" {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "refreshed_7", Path: "/"})
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"a"}}`)
			return
		}
		attempt := orderReqs.Add(1)
		if attempt == 1 {
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"priceInfo":{"amount":{"value":"5.00"}}}}]}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/o", TokenURL: server.URL + "/t"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.FetchOrderDetail(ctx, consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Amount != "5.00" {
		t.Fatalf("Amount=%q", res.Amount)
	}
}

// TestFetchOrderDetailNoOrderInfoComponent: SUCCESS 但无 orderInfoVO component，
// Quantity 默认 1，其他字段空。
func TestFetchOrderDetailNoOrderInfoComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"components":[{"render":"otherVO","data":{}}]}}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	res, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Quantity != "1" || res.Amount != "" || res.OrderStatus != "" {
		t.Fatalf("res=%+v", res)
	}
}

// TestFetchOrderDetailTruncateInParseError: 解析失败时 body 截断 300 字符。
func TestFetchOrderDetailTruncateInParseError(t *testing.T) {
	longBody := strings.Repeat("a", 500)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, longBody)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil {
		t.Fatalf("expected err")
	}
	// 截断后不应含完整 500 字符
	if len(err.Error()) > 600 && strings.Contains(err.Error(), strings.Repeat("a", 400)) {
		t.Fatalf("err 未截断: %d chars", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "解析订单详情响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchOrderDetailRetryExhausted: token 过期但每次下发不同 Set-Cookie，4 次重试耗尽。
func TestFetchOrderDetailRetryExhausted(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: fmt.Sprintf("tok_%d", n), Path: "/"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := client.FetchOrderDetail(ctx, consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "订单详情 token 重试失败") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 4 {
		t.Fatalf("requests=%d want 4", requests.Load())
	}
}

// TestBuildOrderDetailQuery: 验证 query 拼接。
func TestBuildOrderDetailQuery(t *testing.T) {
	q := buildOrderDetailQuery("T", "SIGN")
	if !strings.Contains(q, "t=T") || !strings.Contains(q, "sign=SIGN") ||
		!strings.Contains(q, "api=mtop.idle.web.trade.order.detail") ||
		!strings.Contains(q, "valueType=string") {
		t.Fatalf("query=%q 缺字段", q)
	}
}
