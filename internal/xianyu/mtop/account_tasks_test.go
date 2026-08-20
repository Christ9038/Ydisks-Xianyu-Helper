package mtop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestAccountTaskEndpointsAndParsing 封装Test账号任务EndpointsAndParsing业务协调。
func TestAccountTaskEndpointsAndParsing(t *testing.T) {
	// received 用于本次流程后续判断的received
	var received map[string]any
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if // err 用于本次流程后续判断的err
		err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(r.Form.Get("data")), &received); err != nil {
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

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), RateListURL: server.URL, RateCreateURL: server.URL, PolishItemURL: server.URL}
	// cookies 用于本次流程后续判断的cookies
	cookies := "unb=123; _m_h5_tk=token_1"
	// pending、err 用于本次流程后续判断的pending、err
	pending, err := client.FetchPendingRateOrders(context.Background(), cookies, 1, 50)
	if err != nil || len(pending.Orders) != 2 || pending.Orders[0].TradeID != "order-1" || pending.Orders[0].ItemID != "item-1" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	// rate、err 用于本次流程后续判断的rate、err
	rate, err := client.RateBuyer(context.Background(), cookies, "order-1", "交易愉快")
	if err != nil || !rate.Success || received["tradeId"] != "order-1" || received["feedback"] != "交易愉快" {
		t.Fatalf("rate=%+v received=%+v err=%v", rate, received, err)
	}
	// polish、err 用于本次流程后续判断的polish、err
	polish, err := client.PolishItem(context.Background(), cookies, "item-1")
	if err != nil || !polish.Success || received["itemId"] != "item-1" {
		t.Fatalf("polish=%+v received=%+v err=%v", polish, received, err)
	}
}

// TestAccountTaskRequestUsesSignedForm 封装Test账号任务请求UsesSigned表单业务协调。
func TestAccountTaskRequestUsesSignedForm(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Cookie") == "" || r.URL.Query().Get("sign") == "" {
			t.Fatalf("request method=%s cookie=%q query=%v", r.Method, r.Header.Get("Cookie"), r.URL.Query())
		}
		// body 用于本次流程后续判断的请求体
		body, _ := url.ParseQuery(func() string { _ = r.ParseForm(); return r.Form.Encode() }())
		if body.Get("data") == "" {
			t.Fatal("missing data form")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["FAIL_BIZ_IDLEITEM_POLISH_AGAIN::宝贝已经擦亮过了"],"data":{}}`))
	}))
	defer server.Close()
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), PolishItemURL: server.URL}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err != nil || !result.Success {
		t.Fatalf("duplicate polish should be success: result=%+v err=%v", result, err)
	}
}

// TestPolishItemFallsBackToAlternateAPI 封装TestPolish商品FallsBackToAlternateAPI业务协调。
func TestPolishItemFallsBackToAlternateAPI(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	var calls []string
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// api 用于本次流程后续判断的api
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
		if // err 用于本次流程后续判断的err
		err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		// data 用于本次流程后续判断的数据
		var data map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(r.Form.Get("data")), &data); err != nil || data["itemId"] != "item-1" {
			t.Fatalf("backup data=%v err=%v", data, err)
		}
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
	}))
	defer server.Close()
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), PolishItemURL: server.URL, PolishItemBackupURL: server.URL}
	// result、err 用于本次流程后续判断的result、err
	result, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err != nil || result == nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// want 用于本次流程后续判断的want
	want := []string{"mtop.taobao.idle.item.polish", "mtop.idle.item.polish"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
}

// TestPolishItemSessionExpiredDoesNotCallBackupOrTokenAPI 封装TestPolish商品会话ExpiredDoesNotCallBackupOr令牌API业务协调。
func TestPolishItemSessionExpiredDoesNotCallBackupOrTokenAPI(t *testing.T) {
	// calls 用于本次流程后续判断的calls
	calls := 0
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"ret":["FAIL_SYS_SESSION_EXPIRED::Session过期"],"data":{}}`))
	}))
	defer server.Close()
	// client 用于本次流程后续判断的client
	client := &ClientImpl{
		HTTPClient: server.Client(), PolishItemURL: server.URL, PolishItemBackupURL: server.URL, TokenURL: server.URL,
	}
	// err 用于本次流程后续判断的err
	_, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err == nil || !IsSessionExpiredErr(err) {
		t.Fatalf("err=%v want session expired", err)
	}
	if calls != 1 {
		t.Fatalf("session expiry must stop all fallback/retry requests, calls=%d", calls)
	}
}

// TestPolishItemSendsItemPageSpmContext 验证擦亮请求携带商品页埋点上下文，与已验证可用的调用形态一致。
func TestPolishItemSendsItemPageSpmContext(t *testing.T) {
	// gotQuery 保存服务端收到的擦亮请求查询参数。
	var gotQuery url.Values
	// server 是记录查询参数并返回成功的本地擦亮端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
	}))
	defer server.Close()
	// client 是指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), PolishItemURL: server.URL}
	// result、err 分别是擦亮结果和调用错误。
	result, err := client.PolishItem(context.Background(), "unb=123; _m_h5_tk=token_1", "item-1")
	if err != nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if gotQuery.Get("spm_cnt") != "a21ybx.item.0.0" || gotQuery.Get("spm_pre") == "" || gotQuery.Get("log_id") == "" {
		t.Fatalf("擦亮请求缺少商品页埋点上下文: %v", gotQuery)
	}
}
