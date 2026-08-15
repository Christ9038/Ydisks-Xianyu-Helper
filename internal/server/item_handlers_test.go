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
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// stubPublishMTop 保存stub发布MTop，供当前处理流程使用
type stubPublishMTop struct {
	mtop.Client
	publish func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error)
}

// TestRecommendItemPublishCategory 负责TestRecommend商品发布分类相关处理。
func TestRecommendItemPublishCategory(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if // err 保存err，供当前处理流程使用
	err := srv.Manager.Start(ctx, "acc1", "unb=123; _m_h5_tk=tk1_1;"); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(context.Background(), "acc1", "device", "stale-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// client 保存client，供当前处理流程使用
	client := mtop.NewClient()
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("api") != "mtop.taobao.idle.kgraph.property.recommend" {
			t.Fatalf("api=%q", req.URL.Query().Get("api"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": []string{
				"_m_h5_tk=recommended_2; Domain=.goofish.com; Path=/; Secure",
			}},
			Body: io.NopCloser(strings.NewReader(
				`{"ret":["SUCCESS::调用成功"],"data":{"cardList":[{"cardData":{"propertyId":"-10000","propertyName":"分类","valuesList":[{"catId":"50023914","catName":"电子资料","channelCatId":"202036301","isClicked":"1"}]}}]}}`,
			)),
			Request: req,
		}, nil
	})}
	srv.MTop = client
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-categories/recommend", strings.NewReader(`{"cookie_id":"acc1","keyword":"课程资料"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 保存响应，供当前处理流程使用
	var response struct {
		Category mtop.PublishCategory `json:"category"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Category.CatID != "50023914" || response.Category.CatName != "电子资料" || response.Category.ChannelCatID != "202036301" {
		t.Fatalf("category=%+v", response.Category)
	}
	// saved、err 保存saved、err，供当前处理流程使用
	saved, err := store.Cookies.GetValue(context.Background(), "acc1")
	if err != nil || !strings.Contains(saved, "_m_h5_tk=recommended_2") {
		t.Fatalf("类目推荐响应 Cookie 未保存: value=%q err=%v", saved, err)
	}
	// token、err 保存token、err，供当前处理流程使用
	token, err := store.Tokens.Get(context.Background(), "acc1")
	if err != nil || token.AccessToken != "" {
		t.Fatalf("类目推荐 Cookie 更新后未同步运行时并清理旧 token: token=%+v err=%v", token, err)
	}
}

// PublishItem 负责发布商品相关处理。
func (s *stubPublishMTop) PublishItem(ctx context.Context, cookies string, req mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return s.publish(ctx, cookies, req)
}

// buildPublishMultipart 构造一个 multipart/form-data 请求体，包含一个 1x1 PNG 图片字段。
func buildPublishMultipart(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// mw 保存mw，供当前处理流程使用
	mw := multipart.NewWriter(&buf)
	// 1x1 PNG.
	// png 保存png，供当前处理流程使用
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	// w、err 保存w、err，供当前处理流程使用
	w, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="images"; filename="test.png"`},
		"Content-Type":        []string{"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(png)
	// k、v 表示当前遍历过程中的k、v
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if // err 保存err，供当前处理流程使用
	err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

// TestPublishItemMissingCookieID 缺 cookie_id 应 400。
func TestPublishItemMissingCookieID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{
		"title": "测试商品", "price": "12.50", "quantity": "5",
	})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 cookie_id 应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemBadPrice 价格非法 400。
func TestPublishItemBadPrice(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "0", "quantity": "5",
	})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("价格非法应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemBadQuantity 库存非法 400。
func TestPublishItemBadQuantity(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "0",
	})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("库存非法应 400，got %d", rec.Code)
	}
}

// TestPublishItemBadCookie 无权操作账号 403。
func TestPublishItemBadCookie(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "other-account", "title": "测试商品", "price": "12.50", "quantity": "5",
	})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemBadMultipart 非 multipart 请求 400。
func TestPublishItemBadMultipart(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", strings.NewReader("plain text"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非 multipart 应 400，got %d", rec.Code)
	}
}

// TestPublishItemNoImages 缺图片 400。
func TestPublishItemNoImages(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// mw 保存mw，供当前处理流程使用
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("cookie_id", "acc1")
	_ = mw.WriteField("title", "测试商品")
	_ = mw.WriteField("price", "12.50")
	_ = mw.WriteField("quantity", "5")
	_ = mw.Close()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺图片应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemSuccess mtop PublishItem 成功路径。
// 由于 PublishItem 内部串行调用上传图片/类目/定位/发布多个端点，mock 按 URL 分发。
// TestPublishItemSuccess 负责Test发布商品Success相关处理。
func TestPublishItemSuccess(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 替换为按 URL 分发的 mock。
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// u 保存u，供当前处理流程使用
		u := req.URL.String()
		// respBody 保存resp请求体，供当前处理流程使用
		var respBody string
		switch {
		case strings.Contains(u, "stream-upload.goofish.com"):
			respBody = `{"object":{"url":"https://img.alicdn.com/published.png","pix":"800_800"}}`
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

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "5",
	})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true || res["item_id"] != "pub-item-1" {
		t.Fatalf("发布成功响应异常: %+v", res)
	}
}

// TestPublishItemRejectsMissingRemoteItemID 负责Test发布商品RejectsMissingRemote商品ID相关处理。
func TestPublishItemRejectsMissingRemoteItemID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.MTop = &stubPublishMTop{publish: func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		return &mtop.PublishItemResult{Title: "测试商品"}, nil
	}}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "1"})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "publish_result_missing_item_id") {
		t.Fatalf("missing item id status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemReportsRemoteSuccessLocalSaveFailure 负责Test发布商品ReportsRemoteSuccessLocalSaveFailure相关处理。
func TestPublishItemReportsRemoteSuccessLocalSaveFailure(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.MTop = &stubPublishMTop{publish: func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		_, _ = store.DB.ExecContext(context.Background(), `DELETE FROM cookies WHERE id='acc1'`)
		return &mtop.PublishItemResult{ItemID: "remote-only", ItemURL: "https://example/item/remote-only", Title: "测试商品"}, nil
	}}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "1"})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "remote_published_local_save_failed") || !strings.Contains(rec.Body.String(), "remote-only") {
		t.Fatalf("partial publish status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublishItemStockPermissionMissing 库存权限缺失应 403。
func TestPublishItemStockPermissionMissing(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// prev 保存prev，供当前处理流程使用
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// u 保存u，供当前处理流程使用
		u := req.URL.String()
		// respBody 保存resp请求体，供当前处理流程使用
		var respBody string
		switch {
		case strings.Contains(u, "stream-upload.goofish.com"):
			respBody = `{"object":{"url":"https://img.alicdn.com/published.png","pix":"800_800"}}`
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

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body、ct 保存body、ct，供当前处理流程使用
	body, ct := buildPublishMultipart(t, map[string]string{
		"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "5",
	})
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("库存权限缺失应 403，got %d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["code"] != "stock_permission_missing" {
		t.Fatalf("code 异常: %+v", res)
	}
}

// TestSyncItemsFromAccountSuccess mtop FetchAllItems 成功，保存商品。
func TestSyncItemsFromAccountSuccess(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: "acc1", ItemID: "it-sync-1", ItemTitle: "旧标题", ItemDescription: "本地描述", ItemPrice: "¥9.90",
	}); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Items.SetMultiSpec(ctx, "acc1", "it-sync-1", true); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Items.SetMultiQuantity(ctx, "acc1", "it-sync-1", true); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "acc1", ItemID: "it-removed", ItemTitle: "已删除商品"}); err != nil {
		t.Fatal(err)
	}
	// prev 保存prev，供当前处理流程使用
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// body 保存请求体，供当前处理流程使用
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

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","page_size":10}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("sync status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["saved_count"] != float64(1) {
		t.Fatalf("应保存1件商品: %+v", res)
	}
	if res["deleted_count"] != float64(1) {
		t.Fatalf("应删除1件已下架商品: %+v", res)
	}
	// 验证 DB 已写入。
	items, _ := store.Items.AllForCookie(ctx, "acc1")
	if len(items) != 1 {
		t.Fatalf("同步后本地应只保留1件商品: %+v", items)
	}
	// item 保存商品，供当前处理流程使用
	item := items[0]
	if item.ItemID != "it-sync-1" || item.ItemTitle != "同步商品A" || item.ItemPrice != "¥12.50" || item.ItemDescription != "本地描述" ||
		!item.IsMultiSpec || !item.MultiQuantityDelivery {
		t.Fatalf("线上商品更新或本地配置保留异常: %+v", item)
	}
}

// TestSyncItemsFromAccountReleasesCredentialLockDuringRemoteCall 验证商品远端同步期间不会占用账号凭证锁。
func TestSyncItemsFromAccountReleasesCredentialLockDuringRemoteCall(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// started 表示远端商品请求已经进入阻塞点。
	started := make(chan struct{})
	// release 允许测试释放阻塞的远端请求。
	release := make(chan struct{})
	// once 保证 started 只关闭一次。
	var once sync.Once
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		once.Do(func() { close(started) })
		<-release
		// body 是空商品列表的成功响应。
		body := `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[]}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// requestDone 表示同步请求已经返回。
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		// req 是触发商品同步的测试请求。
		req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(`{"cookie_id":"acc1","page_size":10}`))
		req.AddCookie(cookie)
		// rec 保存商品同步请求的测试响应。
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("远端商品请求未进入阻塞点")
	}
	// lockAcquired 表示另一个操作已成功取得同账号凭证锁。
	lockAcquired := make(chan struct{})
	go func() {
		// unlock 释放测试 goroutine 取得的账号凭证锁。
		unlock := store.LockAccountCredentials("acc1")
		close(lockAcquired)
		unlock()
	}()
	select {
	case <-lockAcquired:
	case <-time.After(time.Second):
		t.Fatal("远端商品请求期间凭证锁仍被占用")
	}
	close(release)
	select {
	case <-requestDone:
	case <-time.After(3 * time.Second):
		t.Fatal("商品同步请求未收束")
	}
}

// TestSyncItemsFromAccountDetectsMultiSpecFromDetail 负责TestSync商品列表From账号DetectsMultiSpecFromDetail相关处理。
func TestSyncItemsFromAccountDetectsMultiSpecFromDetail(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// body 保存请求体，供当前处理流程使用
		body := `{"ret":["SUCCESS::调用成功"],"data":{}}`
		if strings.Contains(req.URL.String(), "mtop.idle.web.xyh.item.list") {
			body = `{"ret":["SUCCESS::调用成功"],"data":{"cardList":[{"cardData":{"id":"multi-item","title":"多规格商品","detailParams":{"itemId":"multi-item"}}}]}}`
		} else if strings.Contains(req.URL.String(), "mtop.taobao.idle.pc.detail") {
			body = `{"ret":["SUCCESS::调用成功"],"data":{"multiSKU":true,"skuDO":{"skuList":[{"id":"a"},{"id":"b"}]}}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(`{"cookie_id":"acc1","page_size":10}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// item、err 保存item、err，供当前处理流程使用
	item, err := store.Items.Get(context.Background(), "acc1", "multi-item")
	if err != nil || !item.IsMultiSpec {
		t.Fatalf("item=%+v err=%v", item, err)
	}
}

// TestSyncItemsFromAccountFail mtop 返回非成功 → 502。
func TestSyncItemsFromAccountFail(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// prev 保存prev，供当前处理流程使用
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// body 保存请求体，供当前处理流程使用
		body := `{"ret":["FAIL_SYS_USER_VALIDATE::用户校验失败"]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}))
	defer func() { srv.MTop = prev }()

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("mtop 失败应 502，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSyncItemsFromAccountBadCookie 无权账号 403。
func TestSyncItemsFromAccountBadCookie(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"other-account"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestSyncItemsFromAccountMissingCookieID 缺 cookie_id 400。
func TestSyncItemsFromAccountMissingCookieID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-all-from-account", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 cookie_id 应 400，got %d", rec.Code)
	}
}

// TestSyncItemsPageFromAccountSuccess mtop FetchItemsPage 成功。
func TestSyncItemsPageFromAccountSuccess(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// prev 保存prev，供当前处理流程使用
	prev := srv.MTop
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// body 保存请求体，供当前处理流程使用
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

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","page_number":1,"page_size":10}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-by-page", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["saved_count"] != float64(1) {
		t.Fatalf("应保存1件: %+v", res)
	}
}

// TestSyncItemsPageFromAccountFail mtop 失败 502。
func TestSyncItemsPageFromAccountFail(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// prev 保存prev，供当前处理流程使用
	prev := srv.MTop
	srv.MTop = newMockMTop(t, mtopResp{ret: []string{"FAIL_SYS_USER_VALIDATE::失败"}})
	defer func() { srv.MTop = prev }()

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/get-by-page", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("mtop 失败应 502，got %d", rec.Code)
	}
}

// TestItemCRUD 商品增删改查 + 多规格。
func TestItemCRUD(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `INSERT INTO item_info
		(cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery)
		VALUES ('acc1','it-crud','商品C','原描述','原分类','12.00','原详情',1,1)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 按账号列表。
	req := httptest.NewRequest(http.MethodGet, "/items/cookie/acc1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list by cookie status=%d body=%s", rec.Code, rec.Body.String())
	}
	// arr 保存arr，供当前处理流程使用
	var arr []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["item_id"] != "it-crud" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 单品详情。
	req2 := httptest.NewRequest(http.MethodGet, "/items/acc1/it-crud", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get status=%d", rec2.Code)
	}

	// 更新。
	updBody := `{"item_title":"改名商品","item_price":"88.00"}`
	// req3 保存req3，供当前处理流程使用
	req3 := httptest.NewRequest(http.MethodPut, "/items/acc1/it-crud", strings.NewReader(updBody))
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	// updated、err 保存updated、err，供当前处理流程使用
	updated, err := store.Items.Get(ctx, "acc1", "it-crud")
	if err != nil {
		t.Fatalf("get updated item: %v", err)
	}
	if updated.ItemTitle != "改名商品" || updated.ItemPrice != "88.00" {
		t.Fatalf("update text fields failed: %+v", updated)
	}
	if updated.ItemDescription != "原描述" || updated.ItemCategory != "原分类" || updated.ItemDetail != "原详情" {
		t.Fatalf("omitted text fields should be preserved: %+v", updated)
	}
	if !updated.IsMultiSpec || !updated.MultiQuantityDelivery {
		t.Fatalf("omitted multi flags should be preserved: %+v", updated)
	}

	// 显式传空字符串时允许清空文本字段。
	clearBody := `{"item_description":""}`
	// reqClear 保存reqClear，供当前处理流程使用
	reqClear := httptest.NewRequest(http.MethodPut, "/items/acc1/it-crud", strings.NewReader(clearBody))
	reqClear.AddCookie(cookie)
	// recClear 保存recClear，供当前处理流程使用
	recClear := httptest.NewRecorder()
	h.ServeHTTP(recClear, reqClear)
	if recClear.Code != 200 {
		t.Fatalf("clear text status=%d body=%s", recClear.Code, recClear.Body.String())
	}
	updated, err = store.Items.Get(ctx, "acc1", "it-crud")
	if err != nil {
		t.Fatalf("get cleared item: %v", err)
	}
	if updated.ItemDescription != "" || updated.ItemCategory != "原分类" || updated.ItemDetail != "原详情" {
		t.Fatalf("explicit empty text should clear only requested field: %+v", updated)
	}

	// 显式传 false 时仍允许清除布尔配置。
	updFlagsBody := `{"item_title":"改名商品","item_price":"88.00","is_multi_spec":false,"is_multi_qty_ship":false}`
	// reqFlags 保存reqFlags，供当前处理流程使用
	reqFlags := httptest.NewRequest(http.MethodPut, "/items/acc1/it-crud", strings.NewReader(updFlagsBody))
	reqFlags.AddCookie(cookie)
	// recFlags 保存recFlags，供当前处理流程使用
	recFlags := httptest.NewRecorder()
	h.ServeHTTP(recFlags, reqFlags)
	if recFlags.Code != 200 {
		t.Fatalf("update flags status=%d body=%s", recFlags.Code, recFlags.Body.String())
	}
	updated, err = store.Items.Get(ctx, "acc1", "it-crud")
	if err != nil {
		t.Fatalf("get updated flags item: %v", err)
	}
	if updated.IsMultiSpec || updated.MultiQuantityDelivery {
		t.Fatalf("explicit false flags should be persisted: %+v", updated)
	}

	// reqMissing 保存reqMissing，供当前处理流程使用
	reqMissing := httptest.NewRequest(http.MethodPut, "/items/acc1/missing-item", strings.NewReader(`{"item_title":"不会创建"}`))
	reqMissing.AddCookie(cookie)
	// recMissing 保存recMissing，供当前处理流程使用
	recMissing := httptest.NewRecorder()
	h.ServeHTTP(recMissing, reqMissing)
	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("missing item update should be 404, got %d body=%s", recMissing.Code, recMissing.Body.String())
	}

	// 多规格 + 多件发货。
	multiBody := `{"multi_quantity_delivery":true}`
	// req4 保存req4，供当前处理流程使用
	req4 := httptest.NewRequest(http.MethodPut, "/items/acc1/it-crud/multi-quantity-delivery", strings.NewReader(multiBody))
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("multi-qty status=%d", rec4.Code)
	}

	// 删除。
	req5 := httptest.NewRequest(http.MethodDelete, "/items/acc1/it-crud", nil)
	req5.AddCookie(cookie)
	// rec5 保存rec5，供当前处理流程使用
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("delete status=%d", rec5.Code)
	}
}

// TestListItemsFiltersByOwnedAccount 负责TestList商品列表FiltersByOwned账号相关处理。
func TestListItemsFiltersByOwnedAccount(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "acc2", "unb=456; _m_h5_tk=tk2_1;", admin.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','item-a','商品A'),('acc2','item-b','商品B')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/items?cookie_id=acc2", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// rows 保存rows，供当前处理流程使用
	var rows []map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &rows) != nil {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(rows) != 1 || rows[0]["cookie_id"] != "acc2" || rows[0]["item_id"] != "item-b" {
		t.Fatalf("rows=%+v", rows)
	}

	// forbidden 保存forbidden，供当前处理流程使用
	forbidden := httptest.NewRequest(http.MethodGet, "/items?cookie_id=not-owned", nil)
	forbidden.AddCookie(cookie)
	// forbiddenRec 保存forbiddenRec，供当前处理流程使用
	forbiddenRec := httptest.NewRecorder()
	h.ServeHTTP(forbiddenRec, forbidden)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}
}

// TestItemGetNotFound 不存在商品 404。
func TestItemGetNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/items/acc1/no-such", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在商品应 404，got %d", rec.Code)
	}
}

// TestCreateItem 新建商品。
func TestCreateItem(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"item_id":"new-item","item_title":"新商品","item_price":"10.00"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCreateItemMissingID 缺商品 ID 400。
func TestCreateItemMissingID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"item_title":"无ID商品"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 ID 应 400，got %d", rec.Code)
	}
}

// TestUpdateItemBadJSON 非法 JSON 400。
func TestUpdateItemBadJSON(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id) VALUES ('acc1','it-bad')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/items/acc1/it-bad", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetItemMultiSpecBadJSON 非法 JSON 400。
func TestSetItemMultiSpecBadJSON(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id) VALUES ('acc1','it-spec')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/items/acc1/it-spec/multi-spec", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}
