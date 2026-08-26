package mtop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestFetchSoldOrdersPageRequestAndParse 封装TestFetchSold订单列表页码请求AndParse业务协调。
func TestFetchSoldOrdersPageRequestAndParse(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.trade.merchant.sold.get" || r.URL.Query().Get("sign") == "" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("Origin") != "https://seller.goofish.com" || r.Header.Get("idle_site_biz_code") != "COMMONPRO" {
			t.Errorf("headers=%v", r.Header)
		}
		// rawBody 用于本次流程后续判断的原始请求体
		rawBody, _ := io.ReadAll(r.Body)
		// form 用于本次流程后续判断的表单
		form, _ := url.ParseQuery(string(rawBody))
		// payload 用于本次流程后续判断的请求载荷
		var payload map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(form.Get("data")), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["pageNumber"] != float64(2) || payload["rowsPerPage"] != float64(30) || payload["queryCode"] != "ALL" {
			t.Errorf("payload=%+v", payload)
		}
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{"module":{"nextPage":"true","totalCount":"31","items":[{`+
			`"commonData":{"orderId":"order-1","itemId":"item-1","orderStatus":"待发货","inRefund":"false","orderCreateTime":"1700000000000"},`+
			`"buyerInfoVO":{"buyerId":"buyer-1","name":"李四","phone":"13900000000","address":"杭州市"},`+
			`"priceVO":{"totalPrice":"29.90","buyNum":"3"},"rightVO":{"btnList":[{"tradeAction":"SKIP_PIN"}]}}]}}}`)
	}))
	defer server.Close()

	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
	// page、err 用于本次流程后续判断的page、err
	page, err := client.FetchSoldOrdersPage(context.Background(), "unb=1; _m_h5_tk=token_1;", 2, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !page.NextPage || page.TotalCount != 31 || len(page.Items) != 1 {
		t.Fatalf("page=%+v", page)
	}
	// item 用于本次流程后续判断的商品
	item := page.Items[0]
	if item.OrderID != "order-1" || item.ItemID != "item-1" || item.OrderStatus != "pending_ship" ||
		item.Quantity != "3" || item.Amount != "29.90" || item.CreatedAt != "2023-11-14 22:13:20" || !item.IsBargain || item.ReceiverName != "李四" {
		t.Fatalf("item=%+v", item)
	}
}

// TestNormalizeSoldOrderTimeSupportsPlatformFormats 验证平台秒级、毫秒级和文本时间都能转换为稳定时间格式。
func TestNormalizeSoldOrderTimeSupportsPlatformFormats(t *testing.T) {
	// cases 保存平台时间输入及其规范化结果。
	cases := map[string]string{
		"1700000000":                "2023-11-14 22:13:20",
		"1700000000000":             "2023-11-14 22:13:20",
		"2026-08-25 10:20:30":       "2026-08-25 02:20:30",
		"2026-08-25T10:20:30+08:00": "2026-08-25 02:20:30",
	}
	// input、want 分别表示平台时间输入和期望的规范化结果。
	for input, want := range cases {
		// got 保存当前平台时间输入的规范化结果。
		got := normalizeSoldOrderTime(input)
		if got != want {
			t.Errorf("normalizeSoldOrderTime(%q)=%q, want %q", input, got, want)
		}
	}
}

// TestFetchSoldOrdersPageRejectsMissingTokenAndFailure 封装TestFetchSold订单列表页码RejectsMissing令牌AndFailure业务协调。
func TestFetchSoldOrdersPageRejectsMissingTokenAndFailure(t *testing.T) {
	// client 用于本次流程后续判断的client
	client := &ClientImpl{}
	if // err 用于本次流程后续判断的err
	_, err := client.FetchSoldOrdersPage(context.Background(), "unb=1", 1, 30); err == nil || !strings.Contains(err.Error(), "_m_h5_tk") {
		t.Fatalf("err=%v", err)
	}
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ret":["FAIL_BIZ_ERROR::失败"]}`)
	}))
	defer server.Close()
	client = &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
	if // err 用于本次流程后续判断的err
	_, err := client.FetchSoldOrdersPage(context.Background(), "_m_h5_tk=token_1", 1, 30); err == nil || !strings.Contains(err.Error(), "非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestParseSoldOrderRejectsInvalidAndDefaultsQuantity 验证订单列表元素的类型、订单号和数量防御逻辑。
func TestParseSoldOrderRejectsInvalidAndDefaultsQuantity(t *testing.T) {
	// _, invalidTypeOK 保存非对象元素的解析结果。
	_, invalidTypeOK := parseSoldOrder("invalid")
	if invalidTypeOK {
		t.Fatal("non-object order should be rejected")
	}
	// _, missingOrderOK 保存缺少订单号元素的解析结果。
	_, missingOrderOK := parseSoldOrder(map[string]any{"commonData": map[string]any{}})
	if missingOrderOK {
		t.Fatal("order without id should be rejected")
	}
	// got、parsedOK 保存数量为空且状态未知的订单解析结果。
	got, parsedOK := parseSoldOrder(map[string]any{"commonData": map[string]any{"orderId": "o-1", "orderStatus": "未知"}, "priceVO": map[string]any{"buyNum": "0"}})
	if !parsedOK || got.Quantity != "1" || got.OrderStatus != "unknown" {
		t.Fatalf("parsed order=%+v ok=%v", got, parsedOK)
	}
}

// TestNormalizeSoldOrderStatus 封装TestNormalizeSold订单状态业务协调。
func TestNormalizeSoldOrderStatus(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"待付款": "processing", "待发货": "pending_ship", "已发货": "shipped",
		"交易成功": "completed", "退款成功": "cancelled", "退款中": "refunding",
	}
	// input、want 表示当前遍历过程中的input、want
	for input, want := range cases {
		if // got 用于本次流程后续判断的got
		got := normalizeSoldOrderStatus(input, false); got != want {
			t.Fatalf("input=%s got=%s want=%s", input, got, want)
		}
	}
	if // got 用于本次流程后续判断的got
	got := normalizeSoldOrderStatus("待发货", true); got != "refunding" {
		t.Fatalf("inRefund got=%s", got)
	}
}

// TestMTopBoolParsesPlatformValues 验证订单列表接口返回的多种布尔值形状。
func TestMTopBoolParsesPlatformValues(t *testing.T) {
	// cases 保存平台布尔值及预期解析结果。
	cases := []struct {
		// name 是子测试名称。
		name string
		// value 是平台返回的布尔值形状。
		value any
		// want 是预期的布尔解析结果。
		want bool
	}{
		{name: "bool true", value: true, want: true},
		{name: "float false", value: float64(0), want: false},
		{name: "int true", value: 1, want: true},
		{name: "string yes", value: " YES ", want: true},
		{name: "string false", value: "false", want: false},
		{name: "unknown", value: []string{"true"}, want: false},
	}
	for /* item 表示当前布尔值解析场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// got 保存当前平台布尔值的解析结果。
			if got := mtopBool(item.value); got != item.want {
				t.Fatalf("mtopBool(%#v)=%v want %v", item.value, got, item.want)
			}
		})
	}
}

// TestSoldOrderCreatedAtChecksNestedAndInvalidValues 验证订单创建时间的嵌套候选和无效回退。
func TestSoldOrderCreatedAtChecksNestedAndInvalidValues(t *testing.T) {
	// nested 保存时间位于交易嵌套对象中的订单记录。
	nested := map[string]any{"orderInfo": map[string]any{"gmtCreate": "2026/08/25 10:20:30"}}
	// gotNested 保存嵌套时间解析结果。
	gotNested := soldOrderCreatedAt(nested, map[string]any{})
	if gotNested != "2026-08-25 02:20:30" {
		t.Fatalf("nested created time=%q", gotNested)
	}
	// gotInvalid 保存没有任何可解析时间字段的结果。
	gotInvalid := soldOrderCreatedAt(map[string]any{"orderTime": "invalid"}, map[string]any{})
	if gotInvalid != "" {
		t.Fatalf("invalid created time=%q", gotInvalid)
	}
	// parsedInvalid 保存无效文本时间的规范化结果。
	parsedInvalid := normalizeSoldOrderTime("not-a-time")
	if parsedInvalid != "" {
		t.Fatalf("invalid normalized time=%q", parsedInvalid)
	}
}
