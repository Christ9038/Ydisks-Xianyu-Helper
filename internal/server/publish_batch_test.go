package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestParsePublishIntervalSeconds 验证批量发布间隔的默认值与边界校验。
func TestParsePublishIntervalSeconds(t *testing.T) {
	// cases 保存空值、有效值和越界值的确定性解析样例。
	cases := []struct {
		name string
		raw  string
		want int
		bad  bool
	}{
		{name: "default", raw: "", want: 5},
		{name: "custom", raw: "12", want: 12},
		{name: "not-number", raw: "abc", bad: true},
		{name: "zero", raw: "0", bad: true},
		{name: "too-large", raw: "3601", bad: true},
	}
	for _, testCase := range cases { // testCase 表示当前发布间隔解析样例。
		t.Run(testCase.name, func(t *testing.T) {
			// got、err 保存当前样例的解析结果。
			got, err := parsePublishIntervalSeconds(testCase.raw)
			if testCase.bad {
				if err == nil {
					t.Fatalf("非法间隔应返回错误，got=%d", got)
				}
				return
			}
			if err != nil || got != testCase.want {
				t.Fatalf("解析结果=%d err=%v want=%d", got, err, testCase.want)
			}
		})
	}
}

// TestParsePublishSheetBytes 表格解析各格式。
func TestParsePublishSheetBytes(t *testing.T) {
	// CSV。
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := parsePublishSheetBytes([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,a.png\n"), "products.csv")
	if err != nil || len(rows) != 1 {
		t.Fatalf("csv parse = %#v, %v", rows, err)
	}
	if rows[0]["cookie_id"] != "acc1" || rows[0]["title"] != "商品A" {
		t.Fatalf("row = %#v", rows[0])
	}

	// TSV。
	rows, err = parsePublishSheetBytes([]byte("账号ID\t标题\nacc1\t商品B\n"), "p.tsv")
	if err != nil || len(rows) != 1 || rows[0]["title"] != "商品B" {
		t.Fatalf("tsv parse = %#v, %v", rows, err)
	}

	// 空内容。
	if _, err := parsePublishSheetBytes([]byte("  \n"), "x.csv"); err == nil {
		t.Fatal("空内容应报错")
	}

	// .xls 拒绝。
	if _, err := parsePublishSheetBytes([]byte("x"), "old.xls"); err == nil {
		t.Fatal(".xls 应被拒绝")
	}

	// 仅表头。
	if _, err := parsePublishSheetBytes([]byte("账号ID,标题\n"), "x.csv"); err == nil {
		t.Fatal("仅表头应报错")
	}
}

// TestParsePublishSheetBytesWithLimitRejectsTooManyRows 封装TestParse发布SheetBytesWith上限RejectsTooManyRows业务协调。
func TestParsePublishSheetBytesWithLimitRejectsTooManyRows(t *testing.T) {
	// b 用于本次流程后续判断的b
	var b strings.Builder
	b.WriteString("账号ID,标题\n")
	for // i 用于本次流程后续判断的i
	i := 0; i < 3; i++ {
		b.WriteString("acc1,商品\n")
	}
	if // err 用于本次流程后续判断的err
	_, err := parsePublishSheetBytesWithLimit([]byte(b.String()), "products.csv", 2); err == nil {
		t.Fatal("too many publish rows should fail")
	}
}

// TestPreviewItemPublishBatchCSV 预检 CSV 批量发布（含图片 zip）。
func TestPreviewItemPublishBatchCSV(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 构造一个最小图片 zip（含一张 1x1 PNG）。
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	// zipBuf 用于本次流程后续判断的zipBuf
	var zipBuf bytes.Buffer
	// zw 用于本次流程后续判断的zw
	zw := zip.NewWriter(&zipBuf)
	// f 用于本次流程后续判断的f
	f, _ := zw.Create("img/a.png")
	f.Write(png)
	_ = zw.Close()

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	_ = mw.WriteField("publish_interval_seconds", "12")
	// csvField 用于本次流程后续判断的csv字段
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,img/a.png\n"))
	// zipField 用于本次流程后续判断的zip字段
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(zipBuf.Bytes())
	_ = mw.Close()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true || res["valid"] != float64(1) {
		t.Fatalf("预检异常: %+v", res)
	}
	// previewRow 用于本次流程后续判断的previewRow
	previewRow := res["rows"].([]any)[0].(map[string]any)
	// category 用于本次流程后续判断的分类
	category := previewRow["category"].(map[string]any)
	if category["cat_id"] != "5001" || category["cat_name"] != "虚拟商品" {
		t.Fatalf("预检未保存兜底类目: %+v", category)
	}
	// storedBatch 验证页面设置的发布间隔已经进入批次持久化模型。
	storedBatch, storedErr := store.PublishBatches.Get(context.Background(), 1, res["preview_id"].(string))
	if storedErr != nil || storedBatch.PublishIntervalSeconds != 12 {
		t.Fatalf("批量发布间隔未持久化: batch=%+v err=%v", storedBatch, storedErr)
	}
}

// TestPreviewItemPublishBatchAllowsEmptyDefaultCategory 封装TestPreview商品发布批次AllowsEmptyDefault分类业务协调。
func TestPreviewItemPublishBatchAllowsEmptyDefaultCategory(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	// file 用于本次流程后续判断的文件
	file, _ := mw.CreateFormFile("file", "products.csv")
	file.Write([]byte("标题,价格,图片\n商品A,12.50,https://example.com/a.png\n"))
	_ = mw.Close()
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	// row 用于本次流程后续判断的row
	row := res["rows"].([]any)[0].(map[string]any)
	// category 用于本次流程后续判断的分类
	category := row["category"].(map[string]any)
	if category["cat_id"] != "" || category["cat_name"] != "" {
		t.Fatalf("category should be empty: %+v", category)
	}
}

// TestPreviewItemPublishBatchRowCategoryOverridesFallback 封装TestPreview商品发布批次Row分类OverridesFallback业务协调。
func TestPreviewItemPublishBatchRowCategoryOverridesFallback(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "批次类目")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// file 用于本次流程后续判断的文件
	file, _ := mw.CreateFormFile("file", "products.csv")
	file.Write([]byte("标题,价格,图片,类目ID,类目名称,频道类目ID\n商品A,12.50,https://example.com/a.png,7001,行指定类目,8001\n"))
	_ = mw.Close()
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	// row 用于本次流程后续判断的row
	row := res["rows"].([]any)[0].(map[string]any)
	// category 用于本次流程后续判断的分类
	category := row["category"].(map[string]any)
	if category["cat_id"] != "7001" || category["cat_name"] != "行指定类目" {
		t.Fatalf("row category=%+v", category)
	}
}

// TestPreviewItemPublishBatchNoFile 缺表格文件 400。
func TestPreviewItemPublishBatchNoFile(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	_ = mw.Close()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺表格应 400，got %d", rec.Code)
	}
}

// TestPreviewItemPublishBatchRequiresDefaultAccount 封装TestPreview商品发布批次RequiresDefault账号业务协调。
func TestPreviewItemPublishBatchRequiresDefaultAccount(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	// file 用于本次流程后续判断的文件
	file, _ := mw.CreateFormFile("file", "products.csv")
	file.Write([]byte("标题,价格,图片\n商品A,12.50,https://example.com/a.png\n"))
	_ = mw.Close()
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "请选择默认发布账号") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPreviewItemPublishBatchBadDefaultCookie 默认账号不属于当前用户 403。
func TestPreviewItemPublishBatchBadDefaultCookie(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "other-account")
	_ = mw.Close()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权默认账号应 403，got %d", rec.Code)
	}
}

// TestPreviewItemPublishBatchTooManyRows 超过最大行数 400。
func TestPreviewItemPublishBatchTooManyRows(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 构造 51 行 CSV。
	var csvBuf bytes.Buffer
	csvBuf.WriteString("账号ID,标题,价格,库存,图片\n")
	for // i 用于本次流程后续判断的i
	i := 0; i < maxPublishBatchRows+1; i++ {
		csvBuf.WriteString("acc1,商品,12.50,5,a.png\n")
	}

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// csvField 用于本次流程后续判断的csv字段
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write(csvBuf.Bytes())
	_ = mw.Close()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超行应 400，got %d", rec.Code)
	}
}

// TestPreviewItemPublishBatchZipTraversal zip 路径穿越拒绝 400。
func TestPreviewItemPublishBatchZipTraversal(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// zipBuf 用于本次流程后续判断的zipBuf
	var zipBuf bytes.Buffer
	// zw 用于本次流程后续判断的zw
	zw := zip.NewWriter(&zipBuf)
	// f 用于本次流程后续判断的f
	f, _ := zw.Create("../escape.png")
	f.Write([]byte("x"))
	_ = zw.Close()

	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// csvField 用于本次流程后续判断的csv字段
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,../escape.png\n"))
	// zipField 用于本次流程后续判断的zip字段
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(zipBuf.Bytes())
	_ = mw.Close()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zip 穿越应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetItemPublishBatchNotFound 不存在批次 404。
func TestGetItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/no-such", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestListItemPublishBatchesRestoresRecentTask 封装TestList商品发布批次列表RestoresRecent任务业务协调。
func TestListItemPublishBatchesRestoresRecentTask(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "listed-batch", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "x.csv", Status: "failed",
	}, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "failed", FailureKind: "publish"}}); err != nil {
		t.Fatal(err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches?limit=10", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"listed-batch"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCancelItemPublishBatchNotFound 不存在批次 404。
func TestCancelItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/no-such/cancel", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestCancelPreviewBatchRetainsUploadDirectoryForRetry 封装Test取消Preview批次RetainsUploadDirectoryFor重试业务协调。
func TestCancelPreviewBatchRetainsUploadDirectoryForRetry(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// batchID 用于本次流程后续判断的批次ID
	batchID := previewPublishBatch(t, h, cookie)
	// batch、err 用于本次流程后续判断的batch、err
	batch, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/"+batchID+"/cancel", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 用于本次流程后续判断的err
	_, err := os.Stat(batch.UploadDir); err != nil {
		t.Fatalf("取消后应保留上传目录供重试: %v", err)
	}
	// retained、err 用于本次流程后续判断的retained、err
	retained, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil || retained.UploadDir != batch.UploadDir {
		t.Fatalf("取消后应保留 upload_dir: batch=%+v err=%v", retained, err)
	}
}

// TestDeletePreviewBatchRemovesUploadDirectory 封装TestDeletePreview批次RemovesUploadDirectory业务协调。
func TestDeletePreviewBatchRemovesUploadDirectory(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// batchID 用于本次流程后续判断的批次ID
	batchID := previewPublishBatch(t, h, cookie)
	// batch、err 用于本次流程后续判断的batch、err
	batch, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := os.Stat(batch.UploadDir); err != nil {
		t.Fatalf("upload dir missing before delete: %v", err)
	}
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/items/publish-batches/"+batchID, nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 用于本次流程后续判断的err
	_, err := os.Stat(batch.UploadDir); !os.IsNotExist(err) {
		t.Fatalf("upload dir still exists: %v", err)
	}
}

// TestRetryFailedItemPublishBatchNotFound 不存在批次 404。
func TestRetryFailedItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/no-such/retry-failed", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestRetryFailedItemPublishBatchRejectsActiveWorker 封装Test重试失败商品发布批次RejectsActive工作器业务协调。
func TestRetryFailedItemPublishBatchRejectsActiveWorker(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin、err 用于本次流程后续判断的admin、err
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "running-batch", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "x.csv", Status: "running",
	}, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "failed", FailureKind: "publish"}}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `UPDATE item_publish_batches SET worker_token='active',lease_expires_at=? WHERE id='running-batch'`, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/running-batch/retry-failed", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("running retry status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := store.PublishBatches.Rows(ctx, "running-batch")
	if err != nil || len(rows) != 1 || rows[0].Status != "failed" {
		t.Fatalf("active retry must not reset rows: rows=%+v err=%v", rows, err)
	}
}

// TestStartItemPublishBatchReclaimsExpiredWorker 封装Test开始商品发布批次ReclaimsExpired工作器业务协调。
func TestStartItemPublishBatchReclaimsExpiredWorker(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "expired-batch", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "x.csv", Status: "running",
	}, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE item_publish_batches SET worker_token='dead',lease_expires_at=? WHERE id='expired-batch'`, time.Now().Add(-time.Minute).Unix())
	_, _ = store.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows SET worker_token='dead' WHERE batch_id='expired-batch'`)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"batch_id":"expired-batch"}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expired batch should be reclaimed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDownloadItemPublishBatchResultNotFound 不存在批次 404。
func TestDownloadItemPublishBatchResultNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/no-such/result.csv", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestDownloadItemPublishBatchResultExportsRows 验证批次结果下载通过应用服务读取并保持 CSV 兼容格式。
func TestDownloadItemPublishBatchResultExportsRows(t *testing.T) {
	// srv、store、cleanup 保存 HTTP 测试服务器、数据库和清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存本测试共用的非取消上下文。
	ctx := context.Background()
	// admin 保存下载批次的当前用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("读取测试管理员失败: %v", adminErr)
	}
	// batchID 保存本次结果下载批次标识。
	batchID := "download-success"
	// createErr 保存测试批次及明细写入结果。
	createErr := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: batchID, UserID: admin.ID, Filename: "items.csv", Status: "completed",
	}, []db.ItemPublishBatchRow{{
		RowNo: 2, CookieID: "acc1", Title: "商品A", Price: "12.50", Quantity: 3,
		CategoryJSON: `{"cat_id":"5001","cat_name":"虚拟商品"}`, Status: "completed",
		ItemID: "item-1", ItemURL: "https://example/item-1",
	}})
	if createErr != nil {
		t.Fatalf("创建下载测试批次失败: %v", createErr)
	}
	// h 保存带应用服务装配的 HTTP 路由。
	h := srv.Router()
	// cookie 保存管理员登录会话。
	cookie := loginHelper(t, h)
	// req 保存结果下载请求。
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID+"/result.csv", nil)
	req.AddCookie(cookie)
	// rec 保存结果下载响应。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("下载结果状态=%d body=%s", rec.Code, rec.Body.String())
	}
	// body 保存导出的 CSV 内容，包含 UTF-8 BOM、表头和业务明细。
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") || !strings.Contains(body, "行号") || !strings.Contains(body, "商品A") || !strings.Contains(body, "item-1") {
		t.Fatalf("导出 CSV 内容异常: %q", body)
	}
}

// TestDownloadItemPublishBatchResultHidesOtherUserBatch 验证结果下载不会泄露其他用户的批次存在性。
func TestDownloadItemPublishBatchResultHidesOtherUserBatch(t *testing.T) {
	// srv、store、cleanup 保存 HTTP 测试服务器、数据库和清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存本测试共用的非取消上下文。
	ctx := context.Background()
	// created 保存其他用户创建结果。
	created, createUserErr := store.Users.Create(ctx, "download-other", "download-other@example.com", "pw")
	if createUserErr != nil || !created {
		t.Fatalf("创建其他用户失败: created=%v err=%v", created, createUserErr)
	}
	// other 保存其他用户的数据库身份。
	other, otherErr := store.Users.GetByUsername(ctx, "download-other")
	if otherErr != nil {
		t.Fatalf("读取其他用户失败: %v", otherErr)
	}
	// createBatchErr 保存其他用户批次写入结果。
	createBatchErr := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "download-other-batch", UserID: other.ID, Filename: "items.csv", Status: "completed",
	}, []db.ItemPublishBatchRow{{RowNo: 2, Title: "不应泄露", Status: "completed"}})
	if createBatchErr != nil {
		t.Fatalf("创建其他用户批次失败: %v", createBatchErr)
	}
	// h 保存结果下载路由。
	h := srv.Router()
	// cookie 保存当前管理员的登录会话。
	cookie := loginHelper(t, h)
	// req 保存越权结果下载请求。
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/download-other-batch/result.csv", nil)
	req.AddCookie(cookie)
	// rec 保存越权响应。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("越权下载应隐藏批次并返回 404，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSafeCSVCellPreventsSpreadsheetFormulaExecution 封装TestSafeCSVCellPreventsSpreadsheetFormulaExecution业务协调。
func TestSafeCSVCellPreventsSpreadsheetFormulaExecution(t *testing.T) {
	// input 表示当前遍历过程中的input
	for _, input := range []string{"=cmd()", "+SUM(1,2)", " -1+2", "@evil"} {
		if // got 用于本次流程后续判断的got
		got := safeCSVCell(input); !strings.HasPrefix(got, "'") {
			t.Fatalf("dangerous cell %q was not escaped: %q", input, got)
		}
	}
	// input 表示当前遍历过程中的input
	for _, input := range []string{"normal", "https://example.com", "123"} {
		if // got 用于本次流程后续判断的got
		got := safeCSVCell(input); got != input {
			t.Fatalf("safe cell %q unexpectedly changed to %q", input, got)
		}
	}
}

// TestStartItemPublishBatchBadJSON 非法 JSON 400。
func TestStartItemPublishBatchBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestStartItemPublishBatchMissingPreviewID 缺 preview_id 400。
func TestStartItemPublishBatchMissingPreviewID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 preview_id 应 400，got %d", rec.Code)
	}
}

// TestStartItemPublishBatchNotFound 不存在 404。
func TestStartItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"no-such"}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在应 404，got %d", rec.Code)
	}
}

// TestWriteFileWithinRoot 写文件限制在根目录内。
func TestWriteFileWithinRoot(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	if // err 用于本次流程后续判断的err
	err := writeFileWithinRoot(dest, "file.txt", []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// data、err 用于本次流程后续判断的data、err
	data, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("read back: %v %q", err, data)
	}
}

// TestWriteFileWithinRootTraversal 路径穿越拒绝。
func TestWriteFileWithinRootTraversal(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	if // err 用于本次流程后续判断的err
	err := writeFileWithinRoot(dest, "../escape.txt", []byte("x")); err == nil {
		t.Fatal("路径穿越应拒绝")
	}
}

// TestReadBatchImageFile 读取本地图片文件。
func TestReadBatchImageFile(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	// png 用于本次流程后续判断的png
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	_ = os.MkdirAll(filepath.Join(dest, "imgs"), 0o750)
	_ = os.WriteFile(filepath.Join(dest, "imgs", "a.png"), png, 0o600)

	// data、ct、name、err 用于本次流程后续判断的data、ct、name、err
	data, ct, name, err := readBatchImageFile(dest, "imgs/a.png")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 || !strings.HasPrefix(ct, "image/") || name != "a.png" {
		t.Fatalf("read result异常: ct=%s name=%s", ct, name)
	}
}

// TestReadBatchImageFileNotFound 不存在图片报错。
func TestReadBatchImageFileNotFound(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	if // err 用于本次流程后续判断的err
	_, _, _, err := readBatchImageFile(dest, "no-such.png"); err == nil {
		t.Fatal("不存在应报错")
	}
}

// TestReadBatchImageFileTraversal 路径穿越拒绝。
func TestReadBatchImageFileTraversal(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	if // err 用于本次流程后续判断的err
	_, _, _, err := readBatchImageFile(dest, "../escape.png"); err == nil {
		t.Fatal("路径穿越应报错")
	}
}

// TestValidateBatchImageRef 校验图片引用。
func TestValidateBatchImageRef(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	// png 用于本次流程后续判断的png
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	_ = os.WriteFile(filepath.Join(dest, "a.png"), png, 0o600)

	if // err 用于本次流程后续判断的err
	err := validateBatchImageRef(dest, "a.png"); err != nil {
		t.Fatalf("存在图片应通过: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := validateBatchImageRef(dest, "no-such.png"); err == nil {
		t.Fatal("不存在应报错")
	}
	if // err 用于本次流程后续判断的err
	err := validateBatchImageRef(dest, "https://example.com/a.png"); err != nil {
		t.Fatalf("HTTP URL 应直接通过: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := validateBatchImageRef(dest, "../escape.png"); err == nil {
		t.Fatal("穿越应报错")
	}
}

// TestIsHTTPURL isHTTPURL 表驱动。
func TestIsHTTPURL(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]bool{
		"http://x.com/a.png":  true,
		"https://x.com/a.png": true,
		"HTTP://X.com/a.png":  true,
		"ftp://x.com/a.png":   false,
		"a.png":               false,
		"":                    false,
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := isHTTPURL(in); got != want {
			t.Errorf("isHTTPURL(%q)=%v want %v", in, got, want)
		}
	}
}

// TestPathBaseFromURL pathBaseFromURL 表驱动。
func TestPathBaseFromURL(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := pathBaseFromURL("https://example.com/path/a.png?x=1"); got != "a.png" {
		t.Fatalf("got %q", got)
	}
	// 仅 host 的 URL：base 为 host 名。
	if got := pathBaseFromURL("https://example.com/"); got != "example.com" {
		t.Fatalf("host-only base 异常: %q", got)
	}
}

// TestSafeBaseName safeBaseName 表驱动。
func TestSafeBaseName(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"normal.png":          "normal.png",
		"  trim.png  ":        "trim.png",
		"path/with/slash.png": "slash.png",
		"":                    "",
		".":                   "",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := safeBaseName(in); got != want {
			t.Errorf("safeBaseName(%q)=%q want %q", in, got, want)
		}
	}
}

// TestRandomHex randomHex 长度。
func TestRandomHex(t *testing.T) {
	if // s 用于本次流程后续判断的s
	s := randomHex(8); len(s) != 16 {
		t.Fatalf("randomHex(8) 长度=%d want 16", len(s))
	}
}

// TestFirstNonEmpty firstNonEmpty 表驱动。
func TestFirstNonEmpty(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := firstNonEmpty("", "", "x"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := firstNonEmpty("", "", ""); got != "" {
		t.Fatalf("got %q", got)
	}
}

// TestDownloadImageURLInvalid 无效 URL 报错。
func TestDownloadImageURLInvalid(t *testing.T) {
	if // err 用于本次流程后续判断的err
	_, _, err := downloadImageURL(context.Background(), "ftp://x.com/a.png"); err == nil {
		t.Fatal("ftp 应报错")
	}
	if // err 用于本次流程后续判断的err
	_, _, err := downloadImageURL(context.Background(), "not-a-url"); err == nil {
		t.Fatal("非 URL 应报错")
	}
}

// TestLoadBatchPublishImagesBadJSON imagesJSON 非数组。
func TestLoadBatchPublishImagesBadJSON(t *testing.T) {
	// dest 用于本次流程后续判断的dest
	dest := t.TempDir()
	// ImagesJSON 非法 JSON。
	_, err := loadBatchPublishImages(context.Background(), dest, db.ItemPublishBatchRow{ImagesJSON: `not-json`})
	if err == nil {
		t.Fatal("非法 JSON 应报错")
	}
	// 空 refs。
	_, err = loadBatchPublishImages(context.Background(), dest, db.ItemPublishBatchRow{ImagesJSON: `[]`})
	if err == nil {
		t.Fatal("空 refs 应报错")
	}
}

// TestCookieOwnedByUser 归属判定。
func TestCookieOwnedByUser(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if !srv.cookieOwnedByUser(ctx, 1, "acc1") {
		t.Fatal("acc1 应属于 user 1")
	}
	if srv.cookieOwnedByUser(ctx, 1, "no-such") {
		t.Fatal("不存在账号不应属于")
	}
	if srv.cookieOwnedByUser(ctx, 999, "acc1") {
		t.Fatal("不存在用户不应拥有")
	}
}

// TestCardOwnedByUser 卡密归属判定。
func TestCardOwnedByUser(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO cards (name, type, user_id) VALUES ('卡1','text',1)`)
	if !srv.cardOwnedByUser(ctx, 1, 1) {
		t.Fatal("card 1 应属于 user 1")
	}
	if srv.cardOwnedByUser(ctx, 1, 999) {
		t.Fatal("card 999 不应存在")
	}
}

// TestCookieValueForUser 取 cookie 值。
func TestCookieValueForUser(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// v、err 用于本次流程后续判断的v、err
	v, err := srv.cookieValueForUser(ctx, 1, "acc1")
	if err != nil || v == "" {
		t.Fatalf("取 cookie 值异常: v=%q err=%v", v, err)
	}
	if // err 用于本次流程后续判断的err
	_, err := srv.cookieValueForUser(ctx, 1, "no-such"); err == nil {
		t.Fatal("不存在应报错")
	}
}

// TestDefaultPublishUploadRoot 默认上传根目录。
func TestDefaultPublishUploadRoot(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", "")
	if // got 用于本次流程后续判断的got
	got := defaultPublishUploadRoot(); got != filepath.Join("data", "uploads") {
		t.Fatalf("默认根目录异常: %q", got)
	}
	t.Setenv("XIANYU_UPLOAD_DIR", "/tmp/uploads")
	if // got 用于本次流程后续判断的got
	got := defaultPublishUploadRoot(); got != "/tmp/uploads" {
		t.Fatalf("env 根目录异常: %q", got)
	}
}
