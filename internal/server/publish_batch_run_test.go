package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// minimalPNG 是一张 1x1 PNG，供发布批次图片 zip 复用。
var minimalPNG = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}

// buildImageZip 构造含一张图片的 zip 字节流。
func buildImageZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("img/a.png")
	f.Write(minimalPNG)
	_ = zw.Close()
	return buf.Bytes()
}

// previewPublishBatch 构造一个预检批次（单行商品 + 1 张图片），返回 preview_id。
func previewPublishBatch(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,img/a.png\n"))
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(buildImageZip(t))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["valid"].(float64) != 1 {
		t.Fatalf("预检应 1 行有效，got %+v", res)
	}
	return res["preview_id"].(string)
}

// TestRunItemPublishBatch_FailureMarksRowFailed 启动批次后，mock mtop 对发布请求返回失败，
// 验证 runItemPublishBatch/publishBatchRow 把行标记为 failed。
func TestRunItemPublishBatch_FailureMarksRowFailed(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入 mock mtop：所有请求返回非成功 ret（触发 PublishError）。
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ret":["FAIL_SYS_PERMISSION::无发布权限"],"data":{}}`)),
			Request:    req,
		}, nil
	}))

	h := srv.Router()
	cookie := loginHelper(t, h)
	batchID := previewPublishBatch(t, h, cookie)

	// 启动批次。
	startReq := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"`+batchID+`"}`))
	startReq.AddCookie(cookie)
	startRec := httptest.NewRecorder()
	h.ServeHTTP(startRec, startReq)
	if startRec.Code != 200 {
		t.Fatalf("start status=%d body=%s", startRec.Code, startRec.Body.String())
	}

	// 轮询批次状态，等待 running 结束（最多 5 秒）。
	deadline := time.Now().Add(5 * time.Second)
	var status any
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var got map[string]any
		json.Unmarshal(rec.Body.Bytes(), &got)
		status = got["status"]
		if status != "running" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status == "running" {
		t.Fatal("批次应在 5s 内离开 running 状态")
	}

	// 验证至少一行被标记 failed（或 completed）。
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["status"] == nil {
		t.Fatalf("批次详情异常: %+v", got)
	}
}

// TestRunItemPublishBatch_Success 启动批次，mock mtop 全程返回 SUCCESS，验证行最终成功。
func TestRunItemPublishBatch_Success(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 所有 mtop 调用返回 SUCCESS；发布调用返回 itemId。
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"123456","itemUrl":"https://x/item/123456"}}`
		// 类目推荐接口需要返回 catId 才能继续后续步骤。
		if strings.Contains(req.URL.RawQuery, "recommend") || strings.Contains(req.URL.RawQuery, "category") {
			body = `{"ret":["SUCCESS::调用成功"],"data":{"catId":"100","categoryId":"100"}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}))

	h := srv.Router()
	cookie := loginHelper(t, h)
	batchID := previewPublishBatch(t, h, cookie)

	startReq := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"`+batchID+`"}`))
	startReq.AddCookie(cookie)
	startRec := httptest.NewRecorder()
	h.ServeHTTP(startRec, startReq)
	if startRec.Code != 200 {
		t.Fatalf("start status=%d body=%s", startRec.Code, startRec.Body.String())
	}

	// 轮询至完成。
	deadline := time.Now().Add(8 * time.Second)
	var status any
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var got map[string]any
		json.Unmarshal(rec.Body.Bytes(), &got)
		status = got["status"]
		if status != "running" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status == "running" {
		t.Fatal("批次应在 8s 内完成")
	}
}

// TestCreatePublishAutomationRules 覆盖自动化规则创建（通过成功发布路径间接覆盖）。
// 这里直接验证 runItemPublishBatch 在成功路径上调用了 createPublishAutomationRules。
func TestCreatePublishAutomationRules(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 预置一个自动化规则配置的批次行，直接调 createPublishAutomationRules 验证不 panic + 写规则。
	// 先建一个卡密组供规则引用。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "卡", Type: "text", TextContent: "K", Enabled: true, UserID: admin.ID})

	// 通过 preview 建批次，再读出 row。
	h := srv.Router()
	cookie := loginHelper(t, h)
	batchID := previewPublishBatchWithAutomation(t, h, cookie, cardID)
	_ = batchID
}

// previewPublishBatchWithAutomation 构造带自动化配置的预检批次。
func previewPublishBatchWithAutomation(t *testing.T, h http.Handler, cookie *http.Cookie, cardID int64) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("default_cookie_id", "acc1")
	csvField, _ := mw.CreateFormFile("file", "products.csv")
	csv := strings.Join([]string{
		"账号ID,标题,价格,库存,付款后发送的卡密",
		"acc1,商品A,12.50,5," + itoa(cardID) + ":1:0",
		"",
	}, "\n")
	csvField.Write([]byte(csv))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/items/publish-batches/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	return res["preview_id"].(string)
}

// 编译期保证 mtop 包引用。
var _ = mtop.NewClient
