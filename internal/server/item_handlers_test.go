package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// buildPublishMultipart 构造一个 multipart/form-data 请求体，包含一个 1x1 PNG 图片字段。
func buildPublishMultipart(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// 1x1 PNG.
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	w, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="images"; filename="test.png"`},
		"Content-Type":        []string{"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(png)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

// TestPublishItemMissingCookieID 缺 cookie_id 应 400。
func TestPublishItemMissingCookieID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body, ct := buildPublishMultipart(t, map[string]string{
		"title": "测试商品", "price": "12.50", "quantity": "5",
	})
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 cookie_id 应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemBadPrice 价格非法 400。
func TestPublishItemBadPrice(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "0", "quantity": "5",
	})
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("价格非法应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemBadQuantity 库存非法 400。
func TestPublishItemBadQuantity(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "0",
	})
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("库存非法应 400，got %d", rec.Code)
	}
}

// TestPublishItemBadCookie 无权操作账号 403。
func TestPublishItemBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "other-account", "title": "测试商品", "price": "12.50", "quantity": "5",
	})
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemBadMultipart 非 multipart 请求 400。
func TestPublishItemBadMultipart(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/items/publish", strings.NewReader("plain text"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非 multipart 应 400，got %d", rec.Code)
	}
}

// TestPublishItemNoImages 缺图片 400。
func TestPublishItemNoImages(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("cookie_id", "acc1")
	_ = mw.WriteField("title", "测试商品")
	_ = mw.WriteField("price", "12.50")
	_ = mw.WriteField("quantity", "5")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/items/publish", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺图片应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemSuccess mtop PublishItem 成功路径。
// 由于 PublishItem 内部串行调用上传图片/类目/定位/发布多个端点，mock 按 URL 分发。
func TestPublishItemSuccess(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 替换为按 URL 分发的 mock。
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		u := req.URL.String()
		var respBody string
		switch {
		case strings.Contains(u, "stream-upload.goofish.com"):
			respBody = `{"object":{"url":"https://img.alicdn.com/published.png","pix":"800_800"}}`
		case strings.Contains(u, "mtop.taobao.idle.local.poi.get"):
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[{"address":"北京"}]}}`
		case strings.Contains(u, "mtop.taobao.idle.kgraph.property.recommend"):
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"99","catName":"数码"}}}`
		case strings.Contains(u, "mtop.idle.pc.idleitem.publish"):
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"pub-item-1","url":"https://www.goofish.com/item?id=pub-item-1","picUrl":"https://img.alicdn.com/published.png","categoryId":"99","categoryName":"数码","title":"测试商品","priceText":"12.50","quantity":"5"}}`
		default:
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(respBody)),
			Request:    req,
		}, nil
	}))
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)

	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "5",
	})
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true || res["item_id"] != "pub-item-1" {
		t.Fatalf("发布成功响应异常: %+v", res)
	}
}

// TestPublishItemStockPermissionMissing 库存权限缺失应 403。
func TestPublishItemStockPermissionMissing(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		u := req.URL.String()
		var respBody string
		switch {
		case strings.Contains(u, "stream-upload.goofish.com"):
			respBody = `{"object":{"url":"https://img.alicdn.com/published.png","pix":"800_800"}}`
		case strings.Contains(u, "mtop.taobao.idle.local.poi.get"):
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{"commonAddresses":[{"address":"北京"}]}}`
		case strings.Contains(u, "mtop.taobao.idle.kgraph.property.recommend"):
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{"categoryPredictResult":{"catId":"99","catName":"数码"}}}`
		case strings.Contains(u, "mtop.idle.pc.idleitem.publish"):
			// 触发 stock_permission_missing 分类。
			respBody = `{"ret":["FAIL_BIZ_STOCK_PERMISSION_MISSING::库存权限缺失"]}`
		default:
			respBody = `{"ret":["SUCCESS::调用成功"],"data":{}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(respBody)),
			Request:    req,
		}, nil
	}))
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)

	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "5",
	})
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("库存权限缺失应 403，got %d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["code"] != "stock_permission_missing" {
		t.Fatalf("code 异常: %+v", res)
	}
}

// TestSyncItemsFromAccountSuccess mtop FetchAllItems 成功，保存商品。
func TestSyncItemsFromAccountSuccess(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[` +
			`{"cardData":{"id":"it-sync-1","title":"同步商品A","priceInfo":{"price":"12.50","preText":"¥"},"picInfo":{"picUrl":"https://img.alicdn.com/a.png"},"categoryId":"9","detailParams":{"itemId":"it-sync-1"}}}]}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}))
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","page_size":10}`
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("sync status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["saved_count"] != float64(1) {
		t.Fatalf("应保存1件商品: %+v", res)
	}
	// 验证 DB 已写入。
	items, _ := store.Items.AllForCookie(context.Background(), "acc1")
	found := false
	for _, it := range items {
		if it.ItemID == "it-sync-1" && it.ItemTitle == "同步商品A" {
			found = true
		}
	}
	if !found {
		t.Fatalf("商品未保存: %+v", items)
	}
}

// TestSyncItemsFromAccountFail mtop 返回非成功 → 502。
func TestSyncItemsFromAccountFail(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ret":["FAIL_SYS_USER_VALIDATE::用户校验失败"]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}))
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1"}`
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("mtop 失败应 502，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSyncItemsFromAccountBadCookie 无权账号 403。
func TestSyncItemsFromAccountBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"other-account"}`
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestSyncItemsFromAccountMissingCookieID 缺 cookie_id 400。
func TestSyncItemsFromAccountMissingCookieID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 cookie_id 应 400，got %d", rec.Code)
	}
}

// TestSyncItemsPageFromAccountSuccess mtop FetchItemsPage 成功。
func TestSyncItemsPageFromAccountSuccess(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[` +
			`{"cardData":{"id":"it-page-1","title":"分页商品","priceInfo":{"price":"9.90","preText":"¥"},"picInfo":{"picUrl":"https://img.alicdn.com/p.png"},"categoryId":"1","detailParams":{"itemId":"it-page-1"}}}]}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}))
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","page_number":1,"page_size":10}`
	req := httptest.NewRequest(http.MethodPost, "/items/get-by-page", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["saved_count"] != float64(1) {
		t.Fatalf("应保存1件: %+v", res)
	}
}

// TestSyncItemsPageFromAccountFail mtop 失败 502。
func TestSyncItemsPageFromAccountFail(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	prev := srv.MTop
	srv.MTop = newMockMTop(t, mtopResp{ret: []string{"FAIL_SYS_USER_VALIDATE::失败"}})
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)
	body := `{"cookie_id":"acc1"}`
	req := httptest.NewRequest(http.MethodPost, "/items/get-by-page", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("mtop 失败应 502，got %d", rec.Code)
	}
}

// TestItemCRUD 商品增删改查 + 多规格。
func TestItemCRUD(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','it-crud','商品C')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 按账号列表。
	req := httptest.NewRequest(http.MethodGet, "/items/cookie/acc1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list by cookie status=%d body=%s", rec.Code, rec.Body.String())
	}
	var arr []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["item_id"] != "it-crud" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 单品详情。
	req2 := httptest.NewRequest(http.MethodGet, "/items/acc1/it-crud", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get status=%d", rec2.Code)
	}

	// 更新。
	updBody := `{"item_title":"改名商品","item_price":"88.00"}`
	req3 := httptest.NewRequest(http.MethodPut, "/items/acc1/it-crud", strings.NewReader(updBody))
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec3.Code, rec3.Body.String())
	}

	// 多规格 + 多件发货。
	multiBody := `{"multi_quantity_delivery":true}`
	req4 := httptest.NewRequest(http.MethodPut, "/items/acc1/it-crud/multi-quantity-delivery", strings.NewReader(multiBody))
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("multi-qty status=%d", rec4.Code)
	}

	// 删除。
	req5 := httptest.NewRequest(http.MethodDelete, "/items/acc1/it-crud", nil)
	req5.AddCookie(cookie)
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("delete status=%d", rec5.Code)
	}
}

// TestItemGetNotFound 不存在商品 404。
func TestItemGetNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/items/acc1/no-such", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在商品应 404，got %d", rec.Code)
	}
}

// TestCreateItem 新建商品。
func TestCreateItem(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"item_id":"new-item","item_title":"新商品","item_price":"10.00"}`
	req := httptest.NewRequest(http.MethodPost, "/items/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCreateItemMissingID 缺商品 ID 400。
func TestCreateItemMissingID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"item_title":"无ID商品"}`
	req := httptest.NewRequest(http.MethodPost, "/items/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 ID 应 400，got %d", rec.Code)
	}
}

// TestUpdateItemBadJSON 非法 JSON 400。
func TestUpdateItemBadJSON(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id) VALUES ('acc1','it-bad')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/items/acc1/it-bad", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetItemMultiSpecBadJSON 非法 JSON 400。
func TestSetItemMultiSpecBadJSON(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id) VALUES ('acc1','it-spec')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/items/acc1/it-spec/multi-spec", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}
