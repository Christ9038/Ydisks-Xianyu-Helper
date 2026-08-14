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

// TestFetchSoldOrdersPageRequestAndParse 负责TestFetchSold订单列表页码请求AndParse相关处理。
func TestFetchSoldOrdersPageRequestAndParse(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.trade.merchant.sold.get" || r.URL.Query().Get("sign") == "" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("Origin") != "https://seller.goofish.com" || r.Header.Get("idle_site_biz_code") != "COMMONPRO" {
			t.Errorf("headers=%v", r.Header)
		}
		// rawBody 保存原始请求体，供当前处理流程使用
		rawBody, _ := io.ReadAll(r.Body)
		// form 保存表单，供当前处理流程使用
		form, _ := url.ParseQuery(string(rawBody))
		// payload 保存请求载荷，供当前处理流程使用
		var payload map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal([]byte(form.Get("data")), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["pageNumber"] != float64(2) || payload["rowsPerPage"] != float64(30) || payload["queryCode"] != "ALL" {
			t.Errorf("payload=%+v", payload)
		}
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{"module":{"nextPage":"true","totalCount":"31","items":[{`+
			`"commonData":{"orderId":"order-1","itemId":"item-1","orderStatus":"待发货","inRefund":"false"},`+
			`"buyerInfoVO":{"buyerId":"buyer-1","name":"李四","phone":"13900000000","address":"杭州市"},`+
			`"priceVO":{"totalPrice":"29.90","buyNum":"3"},"rightVO":{"btnList":[{"tradeAction":"SKIP_PIN"}]}}]}}}`)
	}))
	defer server.Close()

	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
	// page、err 保存page、err，供当前处理流程使用
	page, err := client.FetchSoldOrdersPage(context.Background(), "unb=1; _m_h5_tk=token_1;", 2, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !page.NextPage || page.TotalCount != 31 || len(page.Items) != 1 {
		t.Fatalf("page=%+v", page)
	}
	// item 保存商品，供当前处理流程使用
	item := page.Items[0]
	if item.OrderID != "order-1" || item.ItemID != "item-1" || item.OrderStatus != "pending_ship" ||
		item.Quantity != "3" || item.Amount != "29.90" || !item.IsBargain || item.ReceiverName != "李四" {
		t.Fatalf("item=%+v", item)
	}
}

// TestFetchSoldOrdersPageRejectsMissingTokenAndFailure 负责TestFetchSold订单列表页码RejectsMissing令牌AndFailure相关处理。
func TestFetchSoldOrdersPageRejectsMissingTokenAndFailure(t *testing.T) {
	// client 保存client，供当前处理流程使用
	client := &ClientImpl{}
	if // err 保存err，供当前处理流程使用
	_, err := client.FetchSoldOrdersPage(context.Background(), "unb=1", 1, 30); err == nil || !strings.Contains(err.Error(), "_m_h5_tk") {
		t.Fatalf("err=%v", err)
	}
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ret":["FAIL_BIZ_ERROR::失败"]}`)
	}))
	defer server.Close()
	client = &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
	if // err 保存err，供当前处理流程使用
	_, err := client.FetchSoldOrdersPage(context.Background(), "_m_h5_tk=token_1", 1, 30); err == nil || !strings.Contains(err.Error(), "非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestNormalizeSoldOrderStatus 负责TestNormalizeSold订单状态相关处理。
func TestNormalizeSoldOrderStatus(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]string{
		"待付款": "processing", "待发货": "pending_ship", "已发货": "shipped",
		"交易成功": "completed", "退款成功": "cancelled", "退款中": "refunding",
	}
	// input、want 表示当前遍历过程中的input、want
	for input, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := normalizeSoldOrderStatus(input, false); got != want {
			t.Fatalf("input=%s got=%s want=%s", input, got, want)
		}
	}
	if // got 保存got，供当前处理流程使用
	got := normalizeSoldOrderStatus("待发货", true); got != "refunding" {
		t.Fatalf("inRefund got=%s", got)
	}
}
