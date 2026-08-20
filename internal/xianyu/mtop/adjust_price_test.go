package mtop

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// adjustPriceCookies 是订单改价单测使用的最小签名 Cookie。
const adjustPriceCookies = "unb=123; _m_h5_tk=token_1;"

// TestAdjustOrderPriceSuccess: ret SUCCESS 且 data.success=true 时业务成功，请求体携带整数分价格。
func TestAdjustOrderPriceSuccess(t *testing.T) {
	// requests 统计服务端收到的请求次数。
	var requests atomic.Int32
	// gotBody 保存服务端收到的请求体，用于校验改价参数。
	var gotBody atomic.Value
	// server 是模拟 MTOP 改价端点的本地 HTTP 服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// body、_ 分别是读取到的请求体和忽略的读取错误。
		body, _ := io.ReadAll(r.Body)
		gotBody.Store(string(body))
		fmt.Fprint(w, `{"api":"mtop.taobao.idle.trade.user.adjust.price","ret":["SUCCESS::调用成功"],"data":{"success":true,"serverTime":"1"},"v":"1.0"}`)
	}))
	defer server.Close()

	// client 是指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), AdjustPriceURL: server.URL + "/"}
	// ok、ret、updated、err 分别是改价结果、业务返回、Cookie 更新和调用错误。
	ok, ret, updated, err := client.AdjustOrderPriceContext(context.Background(), adjustPriceCookies, "order-1", 990)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !ok || len(ret) == 0 || !strings.Contains(ret[0], "SUCCESS") {
		t.Fatalf("ok=%v ret=%v", ok, ret)
	}
	if updated != adjustPriceCookies {
		t.Fatalf("无 Set-Cookie 时 updated 应保持原样: %q", updated)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
	// body 是服务端记录的实际请求体。
	body, _ := gotBody.Load().(string)
	if !strings.Contains(body, "%22modifyFee%22%3A990") || !strings.Contains(body, "order-1") {
		t.Fatalf("请求体缺少改价参数: %q", body)
	}
}

// TestAdjustOrderPriceDataSuccessFalse: ret SUCCESS 但 data.success=false 时视为业务失败且不重试。
func TestAdjustOrderPriceDataSuccessFalse(t *testing.T) {
	// requests 统计服务端收到的请求次数。
	var requests atomic.Int32
	// server 是返回 data.success=false 的本地改价端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"success":false}}`)
	}))
	defer server.Close()

	// client 是指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), AdjustPriceURL: server.URL + "/"}
	// ok、ret、err 分别是改价结果、业务返回和调用错误。
	ok, ret, _, err := client.AdjustOrderPriceContext(context.Background(), adjustPriceCookies, "order-1", 990)
	if err != nil || ok || len(ret) == 0 {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
}

// TestAdjustOrderPriceBizFailure: 非 token 过期的业务失败不重试，返回 ok=false 且无错误。
func TestAdjustOrderPriceBizFailure(t *testing.T) {
	// requests 统计服务端收到的请求次数。
	var requests atomic.Int32
	// server 是返回订单状态错误的本地改价端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_NOT_ALLOW_MODIFY::当前订单不允许修改价格"],"data":{}}`)
	}))
	defer server.Close()

	// client 是指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), AdjustPriceURL: server.URL + "/"}
	// ok、ret、err 分别是改价结果、业务返回和调用错误。
	ok, ret, _, err := client.AdjustOrderPriceContext(context.Background(), adjustPriceCookies, "order-1", 990)
	if err != nil || ok || len(ret) == 0 || !strings.Contains(ret[0], "FAIL_BIZ_ORDER_NOT_ALLOW_MODIFY") {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
}

// TestAdjustOrderPriceSessionExpired: Session 失效返回可识别的 SessionExpiredError。
func TestAdjustOrderPriceSessionExpired(t *testing.T) {
	// server 是返回 Session 失效的本地改价端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_SYS_SESSION_EXPIRED::Session过期"]}`)
	}))
	defer server.Close()

	// client 是指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), AdjustPriceURL: server.URL + "/"}
	// ok、err 分别是改价结果和调用错误。
	ok, _, _, err := client.AdjustOrderPriceContext(context.Background(), adjustPriceCookies, "order-1", 990)
	if ok || !IsSessionExpiredErr(err) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

// TestAdjustOrderPriceTokenRetry: token 过期时使用响应下发的新 Cookie 重签并重试成功。
func TestAdjustOrderPriceTokenRetry(t *testing.T) {
	// requests 统计服务端收到的请求次数。
	var requests atomic.Int32
	// server 是首次返回 token 过期并下发新 token、第二次成功的本地改价端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "token_2"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"success":true}}`)
	}))
	defer server.Close()

	// client 是指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), AdjustPriceURL: server.URL + "/"}
	// ok、updated、err 分别是改价结果、Cookie 更新和调用错误。
	ok, _, updated, err := client.AdjustOrderPriceContext(context.Background(), adjustPriceCookies, "order-1", 990)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.Contains(updated, "token_2") {
		t.Fatalf("updated 应包含新 token: %q", updated)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d want 2", requests.Load())
	}
}
