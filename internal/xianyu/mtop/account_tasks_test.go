package mtop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAccountTaskEndpointsAndParsing(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(r.Form.Get("data")), &received); err != nil {
			t.Fatalf("decode data: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("api") {
		case "mtop.taobao.idle.merchant.rate.list":
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[{"tradeInfo":{"tradeId":"order-1"},"item":{"itemId":"item-1"}},{"orderNo":"order-2","itemId":"item-2"}]}}}`))
		case "mtop.taobao.idle.rate.create":
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
		case "mtop.taobao.idle.item.polish":
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
		default:
			t.Fatalf("unexpected api: %s", r.URL.Query().Get("api"))
		}
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), RateListURL: server.URL, RateCreateURL: server.URL, PolishItemURL: server.URL}
	cookies := "unb=123; _m_h5_tk=token_1"
	pending, err := client.FetchPendingRateOrders(context.Background(), cookies, 1, 50)
	if err != nil || len(pending.Orders) != 2 || pending.Orders[0].TradeID != "order-1" || pending.Orders[0].ItemID != "item-1" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	rate, err := client.RateBuyer(context.Background(), cookies, "order-1", "交易愉快")
	if err != nil || !rate.Success || received["tradeId"] != "order-1" || received["feedback"] != "交易愉快" {
		t.Fatalf("rate=%+v received=%+v err=%v", rate, received, err)
	}
	polish, err := client.PolishItem(context.Background(), cookies, "item-1")
	if err != nil || !polish.Success || received["itemId"] != "item-1" {
		t.Fatalf("polish=%+v received=%+v err=%v", polish, received, err)
	}
}

func TestAccountTaskRequestUsesSignedForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Cookie") == "" || r.URL.Query().Get("sign") == "" {
			t.Fatalf("request method=%s cookie=%q query=%v", r.Method, r.Header.Get("Cookie"), r.URL.Query())
		}
		body, _ := url.ParseQuery(func() string { _ = r.ParseForm(); return r.Form.Encode() }())
		if body.Get("data") == "" {
			t.Fatal("missing data form")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["FAIL_BIZ_IDLEITEM_POLISH_AGAIN::宝贝已经擦亮过了"],"data":{}}`))
	}))
	defer server.Close()
	client := &ClientImpl{HTTPClient: server.Client(), PolishItemURL: server.URL}
	result, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err != nil || !result.Success {
		t.Fatalf("duplicate polish should be success: result=%+v err=%v", result, err)
	}
}

func TestPolishItemFallsBackToAlternateAPI(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api := r.URL.Query().Get("api")
		calls = append(calls, api)
		w.Header().Set("Content-Type", "application/json")
		if api == "mtop.taobao.idle.item.polish" {
			_, _ = w.Write([]byte(`{"ret":["FAIL_BIZ_FORBIDDEN::主接口暂不可用"],"data":{}}`))
			return
		}
		if api != "mtop.idle.item.polish" || r.URL.Query().Get("v") != "1.0" {
			t.Fatalf("unexpected backup request: api=%s version=%s", api, r.URL.Query().Get("v"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(r.Form.Get("data")), &data); err != nil || data["itemId"] != "item-1" {
			t.Fatalf("backup data=%v err=%v", data, err)
		}
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
	}))
	defer server.Close()
	client := &ClientImpl{HTTPClient: server.Client(), PolishItemURL: server.URL, PolishItemBackupURL: server.URL}
	result, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err != nil || result == nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := []string{"mtop.taobao.idle.item.polish", "mtop.idle.item.polish"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
}

func TestPolishItemSessionExpiredDoesNotCallBackupOrTokenAPI(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"ret":["FAIL_SYS_SESSION_EXPIRED::Session过期"],"data":{}}`))
	}))
	defer server.Close()
	client := &ClientImpl{
		HTTPClient: server.Client(), PolishItemURL: server.URL, PolishItemBackupURL: server.URL, TokenURL: server.URL,
	}
	_, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err == nil || !IsSessionExpiredErr(err) {
		t.Fatalf("err=%v want session expired", err)
	}
	if calls != 1 {
		t.Fatalf("session expiry must stop all fallback/retry requests, calls=%d", calls)
	}
}
