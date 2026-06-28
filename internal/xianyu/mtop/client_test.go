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

func TestRefreshTokenRetriesOnceWithUpdatedCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	client := &Client{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
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

func TestRefreshTokenDoesNotRetryWithoutUpdatedCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), TokenURL: server.URL + "/"}
	_, err := client.RefreshTokenContext(context.Background(), "unb=123; _m_h5_tk=oldtoken_1;")
	if err == nil || !strings.Contains(err.Error(), "登录凭证已失效") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("请求次数=%d want 1", requests.Load())
	}
}

func TestConsignRetriesWithUpdatedTokenCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	client := &Client{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
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

func TestConsignDoesNotRetryNonTokenFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"]}`)
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	ok, ret, _, err := client.ConsignContext(context.Background(), "unb=123; _m_h5_tk=token_1;", "order-1")
	if err != nil || ok || len(ret) == 0 {
		t.Fatalf("ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("请求次数=%d want 1", requests.Load())
	}
}

func TestFetchOrderDetailParsesPaidAmountAndQuantity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"2"},"priceInfo":{"amount":{"value":"12.50"}}}}]}}`)
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	result, err := client.FetchOrderDetail(context.Background(), "unb=123; _m_h5_tk=token_1;", "order-1")
	if err != nil {
		t.Fatalf("FetchOrderDetail: %v", err)
	}
	if result.Amount != "12.50" || result.Quantity != "2" || result.OrderStatus != "3" {
		t.Fatalf("result=%+v", result)
	}
}
