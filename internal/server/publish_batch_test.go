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

// TestParsePublishSheetBytes 表格解析各格式。
func TestParsePublishSheetBytes(t *testing.T) {
	// CSV。
	// rows、err 保存rows、err，供当前处理流程使用
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

// TestParsePublishSheetBytesWithLimitRejectsTooManyRows 负责TestParse发布SheetBytesWith上限RejectsTooManyRows相关处理。
func TestParsePublishSheetBytesWithLimitRejectsTooManyRows(t *testing.T) {
	// b 保存b，供当前处理流程使用
	var b strings.Builder
	b.WriteString("账号ID,标题\n")
	for // i 保存i，供当前处理流程使用
	i := 0; i < 3; i++ {
		b.WriteString("acc1,商品\n")
	}
	if // err 保存err，供当前处理流程使用
	_, err := parsePublishSheetBytesWithLimit([]byte(b.String()), "products.csv", 2); err == nil {
		t.Fatal("too many publish rows should fail")
	}
}

// TestPreviewItemPublishBatchCSV 预检 CSV 批量发布（含图片 zip）。
func TestPreviewItemPublishBatchCSV(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 构造一个最小图片 zip（含一张 1x1 PNG）。
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	// zipBuf 保存zipBuf，供当前处理流程使用
	var zipBuf bytes.Buffer
	// zw 保存zw，供当前处理流程使用
	zw := zip.NewWriter(&zipBuf)
	// f 保存f，供当前处理流程使用
	f, _ := zw.Create("img/a.png")
	f.Write(png)
	_ = zw.Close()

	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// mw 保存mw，供当前处理流程使用
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// csvField 保存csv字段，供当前处理流程使用
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,img/a.png\n"))
	// zipField 保存zip字段，供当前处理流程使用
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(zipBuf.Bytes())
	_ = mw.Close()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true || res["valid"] != float64(1) {
		t.Fatalf("预检异常: %+v", res)
	}
	// previewRow 保存previewRow，供当前处理流程使用
	previewRow := res["rows"].([]any)[0].(map[string]any)
	// category 保存分类，供当前处理流程使用
	category := previewRow["category"].(map[string]any)
	if category["cat_id"] != "5001" || category["cat_name"] != "虚拟商品" {
		t.Fatalf("预检未保存兜底类目: %+v", category)
	}
}

// TestPreviewItemPublishBatchAllowsEmptyDefaultCategory 负责TestPreview商品发布批次AllowsEmptyDefault分类相关处理。
func TestPreviewItemPublishBatchAllowsEmptyDefaultCategory(t *testing.T) {
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
	_ = mw.WriteField("default_cookie_id", "acc1")
	// file 保存文件，供当前处理流程使用
	file, _ := mw.CreateFormFile("file", "products.csv")
	file.Write([]byte("标题,价格,图片\n商品A,12.50,https://example.com/a.png\n"))
	_ = mw.Close()
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	// row 保存row，供当前处理流程使用
	row := res["rows"].([]any)[0].(map[string]any)
	// category 保存分类，供当前处理流程使用
	category := row["category"].(map[string]any)
	if category["cat_id"] != "" || category["cat_name"] != "" {
		t.Fatalf("category should be empty: %+v", category)
	}
}

// TestPreviewItemPublishBatchRowCategoryOverridesFallback 负责TestPreview商品发布批次Row分类OverridesFallback相关处理。
func TestPreviewItemPublishBatchRowCategoryOverridesFallback(t *testing.T) {
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
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "批次类目")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// file 保存文件，供当前处理流程使用
	file, _ := mw.CreateFormFile("file", "products.csv")
	file.Write([]byte("标题,价格,图片,类目ID,类目名称,频道类目ID\n商品A,12.50,https://example.com/a.png,7001,行指定类目,8001\n"))
	_ = mw.Close()
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	// row 保存row，供当前处理流程使用
	row := res["rows"].([]any)[0].(map[string]any)
	// category 保存分类，供当前处理流程使用
	category := row["category"].(map[string]any)
	if category["cat_id"] != "7001" || category["cat_name"] != "行指定类目" {
		t.Fatalf("row category=%+v", category)
	}
}

// TestPreviewItemPublishBatchNoFile 缺表格文件 400。
func TestPreviewItemPublishBatchNoFile(t *testing.T) {
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
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	_ = mw.Close()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺表格应 400，got %d", rec.Code)
	}
}

// TestPreviewItemPublishBatchRequiresDefaultAccount 负责TestPreview商品发布批次RequiresDefault账号相关处理。
func TestPreviewItemPublishBatchRequiresDefaultAccount(t *testing.T) {
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
	// file 保存文件，供当前处理流程使用
	file, _ := mw.CreateFormFile("file", "products.csv")
	file.Write([]byte("标题,价格,图片\n商品A,12.50,https://example.com/a.png\n"))
	_ = mw.Close()
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "请选择默认发布账号") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPreviewItemPublishBatchBadDefaultCookie 默认账号不属于当前用户 403。
func TestPreviewItemPublishBatchBadDefaultCookie(t *testing.T) {
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
	_ = mw.WriteField("default_cookie_id", "other-account")
	_ = mw.Close()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权默认账号应 403，got %d", rec.Code)
	}
}

// TestPreviewItemPublishBatchTooManyRows 超过最大行数 400。
func TestPreviewItemPublishBatchTooManyRows(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 构造 51 行 CSV。
	var csvBuf bytes.Buffer
	csvBuf.WriteString("账号ID,标题,价格,库存,图片\n")
	for // i 保存i，供当前处理流程使用
	i := 0; i < maxPublishBatchRows+1; i++ {
		csvBuf.WriteString("acc1,商品,12.50,5,a.png\n")
	}

	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// mw 保存mw，供当前处理流程使用
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// csvField 保存csv字段，供当前处理流程使用
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write(csvBuf.Bytes())
	_ = mw.Close()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超行应 400，got %d", rec.Code)
	}
}

// TestPreviewItemPublishBatchZipTraversal zip 路径穿越拒绝 400。
func TestPreviewItemPublishBatchZipTraversal(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// zipBuf 保存zipBuf，供当前处理流程使用
	var zipBuf bytes.Buffer
	// zw 保存zw，供当前处理流程使用
	zw := zip.NewWriter(&zipBuf)
	// f 保存f，供当前处理流程使用
	f, _ := zw.Create("../escape.png")
	f.Write([]byte("x"))
	_ = zw.Close()

	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	// mw 保存mw，供当前处理流程使用
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	_ = mw.WriteField("fallback_category_id", "5001")
	_ = mw.WriteField("fallback_category_name", "虚拟商品")
	_ = mw.WriteField("fallback_channel_category_id", "6001")
	// csvField 保存csv字段，供当前处理流程使用
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,../escape.png\n"))
	// zipField 保存zip字段，供当前处理流程使用
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(zipBuf.Bytes())
	_ = mw.Close()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zip 穿越应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetItemPublishBatchNotFound 不存在批次 404。
func TestGetItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/no-such", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestListItemPublishBatchesRestoresRecentTask 负责TestList商品发布批次列表RestoresRecent任务相关处理。
func TestListItemPublishBatchesRestoresRecentTask(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "listed-batch", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "x.csv", Status: "failed",
	}, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "failed", FailureKind: "publish"}}); err != nil {
		t.Fatal(err)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches?limit=10", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"listed-batch"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCancelItemPublishBatchNotFound 不存在批次 404。
func TestCancelItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/no-such/cancel", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestCancelPreviewBatchRetainsUploadDirectoryForRetry 负责Test取消Preview批次RetainsUploadDirectoryFor重试相关处理。
func TestCancelPreviewBatchRetainsUploadDirectoryForRetry(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// batchID 保存批次ID，供当前处理流程使用
	batchID := previewPublishBatch(t, h, cookie)
	// batch、err 保存batch、err，供当前处理流程使用
	batch, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/"+batchID+"/cancel", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 保存err，供当前处理流程使用
	_, err := os.Stat(batch.UploadDir); err != nil {
		t.Fatalf("取消后应保留上传目录供重试: %v", err)
	}
	// retained、err 保存retained、err，供当前处理流程使用
	retained, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil || retained.UploadDir != batch.UploadDir {
		t.Fatalf("取消后应保留 upload_dir: batch=%+v err=%v", retained, err)
	}
}

// TestDeletePreviewBatchRemovesUploadDirectory 负责TestDeletePreview批次RemovesUploadDirectory相关处理。
func TestDeletePreviewBatchRemovesUploadDirectory(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// batchID 保存批次ID，供当前处理流程使用
	batchID := previewPublishBatch(t, h, cookie)
	// batch、err 保存batch、err，供当前处理流程使用
	batch, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := os.Stat(batch.UploadDir); err != nil {
		t.Fatalf("upload dir missing before delete: %v", err)
	}
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/items/publish-batches/"+batchID, nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 保存err，供当前处理流程使用
	_, err := os.Stat(batch.UploadDir); !os.IsNotExist(err) {
		t.Fatalf("upload dir still exists: %v", err)
	}
}

// TestRetryFailedItemPublishBatchNotFound 不存在批次 404。
func TestRetryFailedItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/no-such/retry-failed", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestRetryFailedItemPublishBatchRejectsActiveWorker 负责Test重试失败商品发布批次RejectsActive工作器相关处理。
func TestRetryFailedItemPublishBatchRejectsActiveWorker(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin、err 保存admin、err，供当前处理流程使用
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "running-batch", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "x.csv", Status: "running",
	}, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "failed", FailureKind: "publish"}}); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `UPDATE item_publish_batches SET worker_token='active',lease_expires_at=? WHERE id='running-batch'`, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/running-batch/retry-failed", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("running retry status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := store.PublishBatches.Rows(ctx, "running-batch")
	if err != nil || len(rows) != 1 || rows[0].Status != "failed" {
		t.Fatalf("active retry must not reset rows: rows=%+v err=%v", rows, err)
	}
}

// TestStartItemPublishBatchReclaimsExpiredWorker 负责Test开始商品发布批次ReclaimsExpired工作器相关处理。
func TestStartItemPublishBatchReclaimsExpiredWorker(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: "expired-batch", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "x.csv", Status: "running",
	}, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = store.DB.ExecContext(ctx, `UPDATE item_publish_batches SET worker_token='dead',lease_expires_at=? WHERE id='expired-batch'`, time.Now().Add(-time.Minute).Unix())
	_, _ = store.DB.ExecContext(ctx, `UPDATE item_publish_batch_rows SET worker_token='dead' WHERE batch_id='expired-batch'`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"batch_id":"expired-batch"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expired batch should be reclaimed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDownloadItemPublishBatchResultNotFound 不存在批次 404。
func TestDownloadItemPublishBatchResultNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/no-such/result.csv", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在批次应 404，got %d", rec.Code)
	}
}

// TestSafeCSVCellPreventsSpreadsheetFormulaExecution 负责TestSafeCSVCellPreventsSpreadsheetFormulaExecution相关处理。
func TestSafeCSVCellPreventsSpreadsheetFormulaExecution(t *testing.T) {
	// input 表示当前遍历过程中的input
	for _, input := range []string{"=cmd()", "+SUM(1,2)", " -1+2", "@evil"} {
		if // got 保存got，供当前处理流程使用
		got := safeCSVCell(input); !strings.HasPrefix(got, "'") {
			t.Fatalf("dangerous cell %q was not escaped: %q", input, got)
		}
	}
	// input 表示当前遍历过程中的input
	for _, input := range []string{"normal", "https://example.com", "123"} {
		if // got 保存got，供当前处理流程使用
		got := safeCSVCell(input); got != input {
			t.Fatalf("safe cell %q unexpectedly changed to %q", input, got)
		}
	}
}

// TestStartItemPublishBatchBadJSON 非法 JSON 400。
func TestStartItemPublishBatchBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestStartItemPublishBatchMissingPreviewID 缺 preview_id 400。
func TestStartItemPublishBatchMissingPreviewID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 preview_id 应 400，got %d", rec.Code)
	}
}

// TestStartItemPublishBatchNotFound 不存在 404。
func TestStartItemPublishBatchNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"no-such"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在应 404，got %d", rec.Code)
	}
}

// TestWriteFileWithinRoot 写文件限制在根目录内。
func TestWriteFileWithinRoot(t *testing.T) {
	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	if // err 保存err，供当前处理流程使用
	err := writeFileWithinRoot(dest, "file.txt", []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// data、err 保存data、err，供当前处理流程使用
	data, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("read back: %v %q", err, data)
	}
}

// TestWriteFileWithinRootTraversal 路径穿越拒绝。
func TestWriteFileWithinRootTraversal(t *testing.T) {
	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	if // err 保存err，供当前处理流程使用
	err := writeFileWithinRoot(dest, "../escape.txt", []byte("x")); err == nil {
		t.Fatal("路径穿越应拒绝")
	}
}

// TestReadBatchImageFile 读取本地图片文件。
func TestReadBatchImageFile(t *testing.T) {
	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	// png 保存png，供当前处理流程使用
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	_ = os.MkdirAll(filepath.Join(dest, "imgs"), 0o750)
	_ = os.WriteFile(filepath.Join(dest, "imgs", "a.png"), png, 0o600)

	// data、ct、name、err 保存data、ct、name、err，供当前处理流程使用
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
	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	if // err 保存err，供当前处理流程使用
	_, _, _, err := readBatchImageFile(dest, "no-such.png"); err == nil {
		t.Fatal("不存在应报错")
	}
}

// TestReadBatchImageFileTraversal 路径穿越拒绝。
func TestReadBatchImageFileTraversal(t *testing.T) {
	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	if // err 保存err，供当前处理流程使用
	_, _, _, err := readBatchImageFile(dest, "../escape.png"); err == nil {
		t.Fatal("路径穿越应报错")
	}
}

// TestValidateBatchImageRef 校验图片引用。
func TestValidateBatchImageRef(t *testing.T) {
	// dest 保存dest，供当前处理流程使用
	dest := t.TempDir()
	// png 保存png，供当前处理流程使用
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	_ = os.WriteFile(filepath.Join(dest, "a.png"), png, 0o600)

	if // err 保存err，供当前处理流程使用
	err := validateBatchImageRef(dest, "a.png"); err != nil {
		t.Fatalf("存在图片应通过: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := validateBatchImageRef(dest, "no-such.png"); err == nil {
		t.Fatal("不存在应报错")
	}
	if // err 保存err，供当前处理流程使用
	err := validateBatchImageRef(dest, "https://example.com/a.png"); err != nil {
		t.Fatalf("HTTP URL 应直接通过: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := validateBatchImageRef(dest, "../escape.png"); err == nil {
		t.Fatal("穿越应报错")
	}
}

// TestIsHTTPURL isHTTPURL 表驱动。
func TestIsHTTPURL(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
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
		if // got 保存got，供当前处理流程使用
		got := isHTTPURL(in); got != want {
			t.Errorf("isHTTPURL(%q)=%v want %v", in, got, want)
		}
	}
}

// TestPathBaseFromURL pathBaseFromURL 表驱动。
func TestPathBaseFromURL(t *testing.T) {
	if // got 保存got，供当前处理流程使用
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
	// cases 保存cases，供当前处理流程使用
	cases := map[string]string{
		"normal.png":          "normal.png",
		"  trim.png  ":        "trim.png",
		"path/with/slash.png": "slash.png",
		"":                    "",
		".":                   "",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := safeBaseName(in); got != want {
			t.Errorf("safeBaseName(%q)=%q want %q", in, got, want)
		}
	}
}

// TestRandomHex randomHex 长度。
func TestRandomHex(t *testing.T) {
	if // s 保存s，供当前处理流程使用
	s := randomHex(8); len(s) != 16 {
		t.Fatalf("randomHex(8) 长度=%d want 16", len(s))
	}
}

// TestFirstNonEmpty firstNonEmpty 表驱动。
func TestFirstNonEmpty(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := firstNonEmpty("", "", "x"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if // got 保存got，供当前处理流程使用
	got := firstNonEmpty("", "", ""); got != "" {
		t.Fatalf("got %q", got)
	}
}

// TestDownloadImageURLInvalid 无效 URL 报错。
func TestDownloadImageURLInvalid(t *testing.T) {
	if // err 保存err，供当前处理流程使用
	_, _, err := downloadImageURL(context.Background(), "ftp://x.com/a.png"); err == nil {
		t.Fatal("ftp 应报错")
	}
	if // err 保存err，供当前处理流程使用
	_, _, err := downloadImageURL(context.Background(), "not-a-url"); err == nil {
		t.Fatal("非 URL 应报错")
	}
}

// TestLoadBatchPublishImagesBadJSON imagesJSON 非数组。
func TestLoadBatchPublishImagesBadJSON(t *testing.T) {
	// dest 保存dest，供当前处理流程使用
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
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
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
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
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
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// v、err 保存v、err，供当前处理流程使用
	v, err := srv.cookieValueForUser(ctx, 1, "acc1")
	if err != nil || v == "" {
		t.Fatalf("取 cookie 值异常: v=%q err=%v", v, err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := srv.cookieValueForUser(ctx, 1, "no-such"); err == nil {
		t.Fatal("不存在应报错")
	}
}

// TestDefaultPublishUploadRoot 默认上传根目录。
func TestDefaultPublishUploadRoot(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", "")
	if // got 保存got，供当前处理流程使用
	got := defaultPublishUploadRoot(); got != filepath.Join("data", "uploads") {
		t.Fatalf("默认根目录异常: %q", got)
	}
	t.Setenv("XIANYU_UPLOAD_DIR", "/tmp/uploads")
	if // got 保存got，供当前处理流程使用
	got := defaultPublishUploadRoot(); got != "/tmp/uploads" {
		t.Fatalf("env 根目录异常: %q", got)
	}
}
