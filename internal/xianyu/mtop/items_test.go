package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// items.go 的 fetchItemsPageOnce 硬编码 ItemListAPI 常量，用 rewriteTransport 改写到本地 server。

const itemsPage1JSON = `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[
  {"cardType":1,"cardData":{"id":"item-1","title":"商品A","categoryId":"cat-1","auctionType":"b","itemStatus":0,
    "priceInfo":{"price":"12.50","preText":"￥"},
    "picInfo":{"picUrl":"https://cdn/a.jpg"},
    "detailParams":{"itemId":"item-1"},
    "detailUrl":"https://www.goofish.com/item?id=item-1"}},
  {"cardType":2,"cardData":{"id":"item-2","title":"商品B","categoryId":"cat-2","auctionType":"b","itemStatus":1,
    "priceInfo":{"price":"88.00","preText":"￥"},
    "picInfo":{"picUrl":"https://cdn/b.jpg"},
    "detailParams":{"itemId":"item-2"}}}
]}}`

// TestFetchItemsPageSuccess: 解析多商品，含 pageCount/字段映射。
func TestFetchItemsPageSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, itemsPage1JSON)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchItemsPage(context.Background(), consignCookies, 1, 20)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items=%d want 2", len(res.Items))
	}
	it := res.Items[0]
	if it.ID != "item-1" || it.Title != "商品A" || it.Price != "12.50" ||
		it.PriceText != "￥12.50" || it.CategoryID != "cat-1" ||
		it.PicURL != "https://cdn/a.jpg" ||
		it.WebURL != "https://www.goofish.com/item?id=item-1" ||
		it.AuctionType != "b" || it.ItemStatus != 0 {
		t.Fatalf("item0=%+v", it)
	}
	if res.PageNumber != 1 || res.PageSize != 20 || res.CurrentCount != 2 {
		t.Fatalf("res=%+v", res)
	}
	// ItemDetail 应为合法 JSON
	var detail map[string]any
	if err := json.Unmarshal([]byte(it.ItemDetail), &detail); err != nil {
		t.Fatalf("ItemDetail 非 JSON: %v (%s)", err, it.ItemDetail)
	}
	if detail["card_type"].(float64) != 1 {
		t.Fatalf("card_type in detail=%v", detail["card_type"])
	}
}

// TestFetchItemsPageMissingUnbCookie: cookie 缺 unb 报错。
func TestFetchItemsPageMissingUnbCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("不应发请求")
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchItemsPage(context.Background(), "_m_h5_tk=t_1;", 1, 20)
	if err == nil || !strings.Contains(err.Error(), "cookie 缺少 unb") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchItemsPageEmptyCardList: SUCCESS 但 cardList 为空。
func TestFetchItemsPageEmptyCardList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchItemsPage(context.Background(), consignCookies, 1, 20)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("items=%d want 0", len(res.Items))
	}
}

// TestFetchItemsPageAutoPrefixFiltered: auto_ 前缀商品被过滤。
func TestFetchItemsPageAutoPrefixFiltered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[
		  {"cardData":{"title":"auto商品","detailParams":{"itemId":"auto_123"}}},
		  {"cardData":{"title":"正常商品","detailParams":{"itemId":"real_456"}}}
		]}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchItemsPage(context.Background(), consignCookies, 1, 20)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != "real_456" {
		t.Fatalf("items=%+v", res.Items)
	}
}

// TestFetchItemsPageNonSuccessRet: 非 token 过期失败 ret。
func TestFetchItemsPageNonSuccessRet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_LIST_ERROR::列表错误"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchItemsPage(context.Background(), consignCookies, 1, 20)
	if err == nil || !strings.Contains(err.Error(), "商品列表接口返回非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchItemsPageParseFailure: 响应非 JSON。
func TestFetchItemsPageParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `bad{`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchItemsPage(context.Background(), consignCookies, 1, 20)
	if err == nil || !strings.Contains(err.Error(), "解析商品列表响应失败") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchItemsPageDefaultsInvalidPage: pageNumber<1 / pageSize<1 兜底。
func TestFetchItemsPageDefaultsInvalidPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchItemsPage(context.Background(), consignCookies, 0, 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.PageNumber != 1 || res.PageSize != 20 {
		t.Fatalf("PageNumber=%d PageSize=%d want 1/20", res.PageNumber, res.PageSize)
	}
}

// TestFetchItemsPageTokenExpiredRetriesWithSetCookie: token 过期 + Set-Cookie，二次成功。
func TestFetchItemsPageTokenExpiredRetriesWithSetCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if attempt == 1 {
			http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtoken_3", Path: "/"})
			fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[]}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.FetchItemsPage(ctx, consignCookies, 1, 20)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d want 2", requests.Load())
	}
}

// TestFetchAllItemsMultiPage: 多页翻页（pageCount 驱动 / len < pageSize 终止）。
func TestFetchAllItemsMultiPage(t *testing.T) {
	var pageReqs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := pageReqs.Add(1)
		// 第 1 页 2 条（pageSize=2，满页），第 2 页 1 条（不满，终止）
		if page == 1 {
			fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[
			  {"cardData":{"title":"A","detailParams":{"itemId":"i1"}}},
			  {"cardData":{"title":"B","detailParams":{"itemId":"i2"}}}
			]}}`)
			return
		}
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[
		  {"cardData":{"title":"C","detailParams":{"itemId":"i3"}}}
		]}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.FetchAllItems(ctx, consignCookies, 2, 10)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("items=%d want 3", len(res.Items))
	}
	if res.TotalCount != 3 || res.TotalPages != 2 {
		t.Fatalf("TotalCount=%d TotalPages=%d", res.TotalCount, res.TotalPages)
	}
}

// TestFetchAllItemsMaxPagesCap: maxPages 限制最大页数。
func TestFetchAllItemsMaxPagesCap(t *testing.T) {
	var pageReqs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageReqs.Add(1)
		// 每页满（pageSize=1），但 maxPages=2 应只取 2 页
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[
		  {"cardData":{"title":"x","detailParams":{"itemId":"i1"}}}
		]}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.FetchAllItems(ctx, consignCookies, 1, 2)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items=%d want 2", len(res.Items))
	}
	if pageReqs.Load() != 2 {
		t.Fatalf("pageReqs=%d want 2", pageReqs.Load())
	}
}

// TestFetchAllItemsEmptyFirstPage: 首页空立即终止。
func TestFetchAllItemsEmptyFirstPage(t *testing.T) {
	var pageReqs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageReqs.Add(1)
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchAllItems(context.Background(), consignCookies, 20, 5)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("items=%d want 0", len(res.Items))
	}
	if pageReqs.Load() != 1 {
		t.Fatalf("pageReqs=%d want 1", pageReqs.Load())
	}
}

// TestFetchAllItemsDefaultPageSize: pageSize<=0 默认 20。
func TestFetchAllItemsDefaultPageSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{}}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	res, err := client.FetchAllItems(context.Background(), consignCookies, 0, 5)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.PageSize != 20 {
		t.Fatalf("PageSize=%d want 20", res.PageSize)
	}
}

// TestFetchAllItemsPropagatesError: 子页失败透传。
func TestFetchAllItemsPropagatesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_LIST_ERROR::错误"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchAllItems(context.Background(), consignCookies, 20, 5)
	if err == nil || !strings.Contains(err.Error(), "商品列表接口返回非成功") {
		t.Fatalf("err=%v", err)
	}
}

// TestFetchItemsPageRetryExhausted: token 过期但每次下发不同 Set-Cookie，4 次重试耗尽。
func TestFetchItemsPageRetryExhausted(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: fmt.Sprintf("tok_%d", n), Path: "/"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 15 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := client.FetchItemsPage(ctx, consignCookies, 1, 20)
	if err == nil || !strings.Contains(err.Error(), "商品列表接口 token 重试失败") {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 4 {
		t.Fatalf("requests=%d want 4", requests.Load())
	}
}

// TestFetchItemsPageRefreshTokenFailure: token 过期无 Set-Cookie，RefreshToken 失败。
func TestFetchItemsPageRefreshTokenFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"]}`)
	}))
	defer server.Close()

	rt := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	client := &ClientImpl{HTTPClient: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
	_, err := client.FetchItemsPage(context.Background(), consignCookies, 1, 20)
	if err == nil || !strings.Contains(err.Error(), "刷新 mtop token 失败") {
		t.Fatalf("err=%v", err)
	}
}

// ---- parseItemList 边缘字段 ----

func TestParseItemListEmpty(t *testing.T) {
	if got := parseItemList(map[string]any{}); len(got) != 0 {
		t.Fatalf("got=%v want empty", got)
	}
}

func TestParseItemListNonMapCardSkipped(t *testing.T) {
	data := map[string]any{"cardList": []any{"not-a-map", 123, nil}}
	if got := parseItemList(data); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseItemListNilCardDataSkipped(t *testing.T) {
	// card 是 map 但 cardData 为 nil，应跳过
	data := map[string]any{"cardList": []any{map[string]any{"cardType": 1}}}
	if got := parseItemList(data); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseItemListIdFallsBackToCardDataId(t *testing.T) {
	// detailParams.itemId 为空，回退到 cardData.id
	data := map[string]any{
		"cardList": []any{
			map[string]any{
				"cardType": 1,
				"cardData": map[string]any{
					"id":    "fallback-id",
					"title": "T",
				},
			},
		},
	}
	items := parseItemList(data)
	if len(items) != 1 || items[0].ID != "fallback-id" {
		t.Fatalf("items=%+v", items)
	}
}

func TestParseItemListEmptyIdSkipped(t *testing.T) {
	// 两个 id 都空，跳过
	data := map[string]any{
		"cardList": []any{
			map[string]any{
				"cardType": 1,
				"cardData": map[string]any{
					"title": "T",
				},
			},
		},
	}
	if got := parseItemList(data); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseItemListNumericFields(t *testing.T) {
	// price / auctionType 为数字，itemStatus 为数字
	data := map[string]any{
		"cardList": []any{
			map[string]any{
				"cardType": float64(2),
				"cardData": map[string]any{
					"detailParams": map[string]any{"itemId": "n1"},
					"title":        "T",
					"priceInfo":    map[string]any{"price": float64(99), "preText": "$"},
					"categoryId":   float64(7),
					"itemStatus":   float64(2),
					"picInfo":      map[string]any{"picUrl": "https://cdn/p.jpg"},
				},
			},
		},
	}
	items := parseItemList(data)
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	it := items[0]
	if it.Price != "99" || it.PriceText != "$99" || it.CategoryID != "7" || it.ItemStatus != 2 {
		t.Fatalf("item=%+v", it)
	}
}

func TestBuildItemListQuery(t *testing.T) {
	q := buildItemListQuery("T", "SIGN")
	if !strings.Contains(q, "t=T") || !strings.Contains(q, "sign=SIGN") ||
		!strings.Contains(q, "api=mtop.idle.web.xyh.item.list") ||
		!strings.Contains(q, "spm_cnt=a21ybx.im.0.0") {
		t.Fatalf("query=%q 缺字段", q)
	}
}
