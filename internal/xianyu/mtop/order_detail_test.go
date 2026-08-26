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
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"4"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"3","specName":"颜色","specValue":"红色"},"priceInfo":{"amount":{"value":"88.00"}}}}]}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// res、err 用于本次流程后续判断的res、err
	res, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Quantity != "3" || res.SpecName != "颜色" || res.SpecValue != "红色" ||
		res.OrderStatus != "4" || res.Amount != "88.00" {
		t.Fatalf("res=%+v", res)
	}
}

// TestFetchOrderDetailSuccessWithCombinedSKUText 验证多规格订单以 skuText 返回时仍能提取规则匹配字段。
func TestFetchOrderDetailSuccessWithCombinedSKUText(t *testing.T) {
	// server 返回闲鱼多规格订单详情的组合 SKU 文本形状。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// w、r 分别用于写回本地订单详情响应和读取测试请求。
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"1","skuInfo":{"skuText":"总量：月卡"}},"priceInfo":{"amount":{"value":"9.90"}}}}]}}`)
	}))
	defer server.Close()

	// client 使用本地服务替代闲鱼接口，验证解析逻辑而不触发真实平台请求。
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// result、err 保存订单详情解析结果及错误。
	result, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result.SpecName != "总量" || result.SpecValue != "月卡" {
		t.Fatalf("spec=(%q,%q) want (总量,月卡)", result.SpecName, result.SpecValue)
	}
}

// TestOrderSpecFromItemInfoSupportsPlatformShapes 验证订单规格解析兼容平台字段、嵌套对象和空格格式。
func TestOrderSpecFromItemInfoSupportsPlatformShapes(t *testing.T) {
	// cases 覆盖当前订单详情接口已知的规格字段形状及无效文本。
	cases := []struct {
		name     string
		itemInfo map[string]any
		wantName string
		wantVal  string
	}{
		{name: "explicit fields", itemInfo: map[string]any{"specName": "总量", "specValue": "年卡"}, wantName: "总量", wantVal: "年卡"},
		{name: "snake case fields", itemInfo: map[string]any{"spec_name": "总量", "spec_value": "永久"}, wantName: "总量", wantVal: "永久"},
		{name: "sku text", itemInfo: map[string]any{"skuText": "总量:月卡"}, wantName: "总量", wantVal: "月卡"},
		{name: "nested sku", itemInfo: map[string]any{"skuInfo": map[string]any{"skuText": "总量 月卡"}}, wantName: "总量", wantVal: "月卡"},
		{name: "partial top nested complete", itemInfo: map[string]any{"specName": "颜色", "skuInfo": map[string]any{"skuText": "尺码：M"}}, wantName: "尺码", wantVal: "M"},
		{name: "multiple specs first pair", itemInfo: map[string]any{"skuText": "颜色：红色；尺码：M"}, wantName: "颜色", wantVal: "红色"},
		{name: "slash remains value", itemInfo: map[string]any{"skuText": "套餐:S/M"}, wantName: "套餐", wantVal: "S/M"},
		{name: "partial values do not match", itemInfo: map[string]any{"specName": "颜色", "skuText": "尺码："}, wantName: "颜色", wantVal: ""},
		{name: "invalid text", itemInfo: map[string]any{"skuText": "月卡"}},
	}
	// tc 表示当前规格解析测试场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// gotName、gotValue 保存当前响应形状解析出的规格名称和值。
			gotName, gotValue := orderSpecFromItemInfo(tc.itemInfo)
			if gotName != tc.wantName || gotValue != tc.wantVal {
				t.Fatalf("spec=(%q,%q) want (%q,%q)", gotName, gotValue, tc.wantName, tc.wantVal)
			}
		})
	}
}

// TestOrderSpecFromItemInfoHandlesNilInput 验证缺少商品信息时不产生虚假规格。
func TestOrderSpecFromItemInfoHandlesNilInput(t *testing.T) {
	// gotName、gotValue 保存空商品信息的规格解析结果。
	gotName, gotValue := orderSpecFromItemInfo(nil)
	if gotName != "" || gotValue != "" {
		t.Fatalf("nil item info=(%q,%q)", gotName, gotValue)
	}
}

// TestFetchOrderDetailSessionExpired 验证平台明确要求重新登录时不进入 token 重试。
func TestFetchOrderDetailSessionExpired(t *testing.T) {
	// server 返回平台会话过期响应。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_SYS_SESSION_EXPIRED::会话过期"]}`)
	}))
	defer server.Close()
	// client 使用本地服务替代闲鱼详情接口。
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL}
	// _, err 保存会话过期处理结果。
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "订单详情接口") {
		t.Fatalf("session expired error=%v", err)
	}
}

// TestSplitOrderSpecTextHandlesSegments 验证组合规格文本按平台分隔符提取首个完整键值对。
func TestSplitOrderSpecTextHandlesSegments(t *testing.T) {
	// cases 保存组合规格文本及预期拆分结果。
	cases := []struct {
		// name 是子测试名称。
		name string
		// raw 是平台返回的组合规格文本。
		raw string
		// wantName 是预期的规格名称。
		wantName string
		// wantValue 是预期的规格值。
		wantValue string
	}{
		{name: "semicolon", raw: "颜色；尺码：M", wantName: "尺码", wantValue: "M"},
		{name: "comma", raw: "颜色,尺码=42", wantName: "尺码", wantValue: "42"},
		{name: "empty", raw: "  ", wantName: "", wantValue: ""},
		{name: "invalid", raw: "没有分隔符", wantName: "", wantValue: ""},
		{name: "blank sides", raw: ":值", wantName: "", wantValue: ""},
	}
	for /* item 表示当前组合规格解析场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// gotName、gotValue 保存当前组合规格拆分结果。
			gotName, gotValue := splitOrderSpecText(item.raw)
			if gotName != item.wantName || gotValue != item.wantValue {
				t.Fatalf("splitOrderSpecText(%q)=(%q,%q), want (%q,%q)", item.raw, gotName, gotValue, item.wantName, item.wantValue)
			}
		})
	}
}

// TestFetchOrderDetailMissingBuyAmountDefaultsTo1: components 无 buyAmount 时 Quantity 默认 "1"。
func TestFetchOrderDetailMissingBuyAmountDefaultsTo1(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"components":[{"render":"orderInfoVO","data":{"itemInfo":{}}}]}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// res、err 用于本次流程后续判断的res、err
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
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ORDER_NOT_FOUND::订单不存在"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
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
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `broken{`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "解析订单详情响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchOrderDetailRequestError: 网络层错误。
func TestFetchOrderDetailRequestError(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
	_, err := client.FetchOrderDetail(context.Background(), consignCookies, "order-1")
	if err == nil || !strings.Contains(err.Error(), "订单详情请求失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchOrderDetailTokenExpiredRetriesWithSetCookie: token 过期 + Set-Cookie，二次成功。
func TestFetchOrderDetailTokenExpiredRetriesWithSetCookie(t *testing.T) {
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// attempt 用于本次流程后续判断的尝试次数
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_5", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"1"},"priceInfo":{"amount":{"value":"9.90"}}}}]}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// res、err 用于本次流程后续判断的res、err
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
// TestFetchOrderDetailTokenExpiredNoCookieRefreshes 封装TestFetch订单Detail令牌ExpiredNo登录凭证Refreshes业务协调。
func TestFetchOrderDetailTokenExpiredNoCookieRefreshes(t *testing.T) {
	// orderReqs 用于本次流程后续判断的订单Reqs
	var orderReqs atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") == "mtop.taobao.idlemessage.pc.login.token" {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "refreshed_7", Path: "/"})
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"accessToken":"a"}}`)
			return
		}
		// attempt 用于本次流程后续判断的尝试次数
		attempt := orderReqs.Add(1)
		if attempt == 1 {
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"3"},"components":[{"render":"orderInfoVO","data":{"priceInfo":{"amount":{"value":"5.00"}}}}]}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/o", TokenURL: server.URL + "/t"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// res、err 用于本次流程后续判断的res、err
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
// TestFetchOrderDetailNoOrderInfoComponent 封装TestFetch订单DetailNo订单InfoComponent业务协调。
func TestFetchOrderDetailNoOrderInfoComponent(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"components":[{"render":"otherVO","data":{}}]}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// res、err 用于本次流程后续判断的res、err
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
	// longBody 用于本次流程后续判断的long请求体
	longBody := strings.Repeat("a", 500)
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, longBody)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// err 用于本次流程后续判断的err
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
	// requests 用于本次流程后续判断的请求列表
	var requests atomic.Int32
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// n 用于本次流程后续判断的n
		n := requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: fmt.Sprintf("tok_%d", n), Path: "/"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), OrderDetailURL: server.URL + "/"}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// err 用于本次流程后续判断的err
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
	// q 用于本次流程后续判断的q
	q := buildOrderDetailQuery("T", "SIGN")
	if !strings.Contains(q, "t=T") || !strings.Contains(q, "sign=SIGN") ||
		!strings.Contains(q, "api=mtop.idle.web.trade.order.detail") ||
		!strings.Contains(q, "valueType=string") {
		t.Fatalf("query=%q 缺字段", q)
	}
}
