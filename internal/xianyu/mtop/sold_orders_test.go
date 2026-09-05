package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestFetchSoldOrdersPageCompleteness 用 t 验证唯一已售格式的完整性、合法空页及错误脱敏，失败页不能成为成功快照。
func TestFetchSoldOrdersPageCompleteness(t *testing.T) {
	// validOrder 是只含必要订单身份的合成平台记录，不引入另一种响应格式。
	const validOrder = `{"commonData":{"orderId":"order-safe"}}`
	// cases 覆盖必需结构、逐条解析和分页结束标志；wantError 为空表示合法成功。
	cases := []struct {
		// name 标识被验证的响应边界。
		name string
		// body 是本地服务返回的合成响应，不含真实账号数据。
		body string
		// status 是本地响应 HTTP 状态，零值使用 200。
		status int
		// wantError 指定稳定错误分类，禁止回显输入内容。
		wantError string
		// wantCount 是成功页应完整保留的订单数。
		wantCount int
		// expired 要求会话失效响应继续保留可识别的错误类型。
		expired bool
	}{
		{name: "empty array", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":false}}}`},
		{name: "null empty list", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":null,"nextPage":"false"}}}`},
		{name: "twelve orders", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[` + strings.TrimSuffix(strings.Repeat(validOrder+",", 12), ",") + `],"nextPage":false}}}`, wantCount: 12},
		{name: "missing module", body: `{"ret":["SUCCESS::调用成功"],"data":{}}`, wantError: "module"},
		{name: "null module", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":null}}`, wantError: "module"},
		{name: "invalid module", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":"private-marker"}}`, wantError: "解析"},
		{name: "missing items", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"nextPage":false}}}`, wantError: "items"},
		{name: "invalid items", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":{},"nextPage":false}}}`, wantError: "items"},
		{name: "dropped order", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[` + validOrder + `,{"commonData":{"orderId":" "},"private":"private-marker"}],"nextPage":false}}}`, wantError: "第 2 条"},
		{name: "non object order", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":["private-marker"],"nextPage":false}}}`, wantError: "第 1 条"},
		{name: "empty has next", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":true}}}`, wantError: "空页"},
		{name: "null has next", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":null,"nextPage":true}}}`, wantError: "空页"},
		{name: "missing next", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[]}}}`, wantError: "nextPage"},
		{name: "invalid next", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":"private-marker"}}}`, wantError: "nextPage"},
		{name: "invalid numeric next", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":2}}}`, wantError: "nextPage"},
		{name: "invalid nested next", body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":[true]}}}`, wantError: "nextPage"},
		{name: "malformed json", body: `{"private":"private-marker"`, wantError: "解析"},
		{name: "failed ret", body: `{"ret":["FAIL::private-marker"]}`, wantError: "非成功"},
		{name: "expired ret", body: `{"ret":["FAIL_SYS_SESSION_EXPIRED::private-marker"]}`, wantError: "Session", expired: true},
		{name: "http failure with success body", status: http.StatusBadGateway, body: `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":false}}}`, wantError: "HTTP 502"},
	}
	// testCase 是当前响应边界；每个子测试独占本地服务和 Cookie 会话。
	for _, testCase := range cases {
		// t 接收当前响应边界的断言结果。
		t.Run(testCase.name, func(t *testing.T) {
			// server 的回调通过 w 写入合成响应，忽略请求内容并附带 Cookie 更新。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Add("Set-Cookie", "sid=rotated; Path=/")
				if testCase.status != 0 {
					w.WriteHeader(testCase.status)
				}
				_, _ = io.WriteString(w, testCase.body)
			}))
			defer server.Close()
			// client 仅请求本地 HTTP 服务，避免依赖真实平台。
			client := &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
			// ctx、session 记录失败和成功响应同样应吸收的 Cookie，不输出凭证值。
			ctx, session := WithFlatCookieSession(context.Background(), "_m_h5_tk=fixture_1; sid=initial")
			// page、err 保存本次单页获取结果和完整性错误。
			page, err := client.FetchSoldOrdersPage(ctx, "_m_h5_tk=fixture_1; sid=initial", 3, 30)
			if testCase.wantError == "" {
				if err != nil || page == nil || len(page.Items) != testCase.wantCount || page.NextPage {
					t.Fatal("合法响应未完整解析")
				}
			} else if err == nil || !strings.Contains(err.Error(), testCase.wantError) || strings.Contains(err.Error(), "private-marker") {
				t.Fatal("未返回预期的脱敏完整性错误")
			}
			if IsSessionExpiredErr(err) != testCase.expired {
				t.Fatal("会话失效分类发生变化")
			}
			// value、changed 只用于检查 Cookie 收集，快照内容不参与本测试。
			value, _, changed := session.State()
			if !changed || !strings.Contains(value, "sid=rotated") {
				t.Fatal("响应 Cookie 未被收集")
			}
		})
	}
}

// TestFetchSoldOrdersPagePaginationFlags 用 t 验证现有布尔兼容值不会因完整性检查改变含义。
func TestFetchSoldOrdersPagePaginationFlags(t *testing.T) {
	// flag 是当前 JSON 布尔兼容值，wantNext 是应保留的是否继续分页语义。
	for flag, wantNext := range map[string]bool{`false`: false, `0`: false, `"false"`: false, `"0"`: false, `"NO"`: false, `true`: true, `1`: true, `"true"`: true, `"1"`: true, `"YES"`: true} {
		// server 的回调通过 w 返回相同格式的合法订单列表，忽略请求。
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[{"commonData":{"orderId":"order-safe"}}],"nextPage":%s}}}`, flag)
		}))
		// client 是本次布尔兼容值对应的本地客户端。
		client := &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
		// page、err 保存合法订单页及错误。
		page, err := client.FetchSoldOrdersPage(context.Background(), "_m_h5_tk=fixture_1", 1, 30)
		server.Close()
		if err != nil || page == nil || page.NextPage != wantNext {
			t.Fatalf("合法分页标志未被接受: %s", flag)
		}
	}
}

// TestFetchSoldOrdersPageTransportErrors 用 t 验证请求构造、传输和读体失败不会回显 URL 或正文，且取消身份保留。
func TestFetchSoldOrdersPageTransportErrors(t *testing.T) {
	// scenario 标识当前本地故障，全部避免发起外部请求。
	for _, scenario := range []string{"address", "transport", "body", "cancel"} {
		// t 接收当前传输故障的断言结果。
		t.Run(scenario, func(t *testing.T) {
			// ctx、cancel 管理本次合成请求的取消生命周期，测试退出时释放资源。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// transport 的回调忽略请求，直接注入带合成私密标记的传输或读体错误。
			transport := cookieSessionRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
				if scenario == "cancel" {
					cancel()
					return nil, context.Canceled
				}
				if scenario == "body" {
					// reader、writer 组成已失败的本地管道，使读取响应正文确定性报错。
					reader, writer := io.Pipe()
					_ = writer.CloseWithError(errors.New("private-marker"))
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Set-Cookie": {"sid=rotated; Path=/"}}, Body: reader}, nil
				}
				return nil, errors.New("private-marker")
			})
			// client 只使用本地传输替身，日志丢弃以避免测试把合成错误写入输出。
			client := &ClientImpl{HTTPClient: &http.Client{Transport: transport}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			if scenario == "address" {
				client.SoldOrdersURL = "://private-marker"
			}
			// requestCtx、session 保存失败响应仍需收集的 Cookie 会话。
			requestCtx, session := WithFlatCookieSession(ctx, "_m_h5_tk=fixture_1")
			// page、err 保存当前失败分支的结果，错误不得包含合成私密标记。
			page, err := client.FetchSoldOrdersPage(requestCtx, "_m_h5_tk=fixture_1", 0, 101)
			if page != nil || err == nil || strings.Contains(err.Error(), "private-marker") {
				t.Fatal("传输错误没有按预期脱敏")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("取消错误身份丢失")
			}
			// changed 只验证读体失败前已经收集 Cookie，不输出会话内容。
			_, _, changed := session.State()
			if scenario == "body" && !changed {
				t.Fatal("读体失败丢失响应 Cookie")
			}
		})
	}
}

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
