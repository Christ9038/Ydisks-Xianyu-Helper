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

func TestFetchSoldOrdersPageRequestAndParse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.trade.merchant.sold.get" || r.URL.Query().Get("sign") == "" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("Origin") != "https://seller.goofish.com" || r.Header.Get("idle_site_biz_code") != "COMMONPRO" {
			t.Errorf("headers=%v", r.Header)
		}
		rawBody, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(rawBody))
		var payload map[string]any
		if err := json.Unmarshal([]byte(form.Get("data")), &payload); err != nil {
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

	client := &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
	page, err := client.FetchSoldOrdersPage(context.Background(), "unb=1; _m_h5_tk=token_1;", 2, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !page.NextPage || page.TotalCount != 31 || len(page.Items) != 1 {
		t.Fatalf("page=%+v", page)
	}
	item := page.Items[0]
	if item.OrderID != "order-1" || item.ItemID != "item-1" || item.OrderStatus != "pending_ship" ||
		item.Quantity != "3" || item.Amount != "29.90" || !item.IsBargain || item.ReceiverName != "李四" {
		t.Fatalf("item=%+v", item)
	}
}

func TestFetchSoldOrdersPageRejectsMissingTokenAndFailure(t *testing.T) {
	client := &ClientImpl{}
	if _, err := client.FetchSoldOrdersPage(context.Background(), "unb=1", 1, 30); err == nil || !strings.Contains(err.Error(), "_m_h5_tk") {
		t.Fatalf("err=%v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ret":["FAIL_BIZ_ERROR::失败"]}`)
	}))
	defer server.Close()
	client = &ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL}
	if _, err := client.FetchSoldOrdersPage(context.Background(), "_m_h5_tk=token_1", 1, 30); err == nil || !strings.Contains(err.Error(), "非成功") {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeSoldOrderStatus(t *testing.T) {
	cases := map[string]string{
		"待付款": "processing", "待发货": "pending_ship", "已发货": "shipped",
		"交易成功": "completed", "退款成功": "cancelled", "退款中": "refunding",
	}
	for input, want := range cases {
		if got := normalizeSoldOrderStatus(input, false); got != want {
			t.Fatalf("input=%s got=%s want=%s", input, got, want)
		}
	}
	if got := normalizeSoldOrderStatus("待发货", true); got != "refunding" {
		t.Fatalf("inRefund got=%s", got)
	}
}
