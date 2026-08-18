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
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/adapter"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// minimalPNG 是一张 1x1 PNG，供发布批次图片 zip 复用。
var minimalPNG = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}

// TestFinalPublishBatchStatus 封装TestFinal发布批次状态业务协调。
func TestFinalPublishBatchStatus(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		in   itemapp.BatchInfo
		want string
	}{
		{name: "completed", in: itemapp.BatchInfo{SuccessCount: 2}, want: "completed"},
		{name: "failed", in: itemapp.BatchInfo{FailedCount: 2}, want: "failed"},
		{name: "partially failed", in: itemapp.BatchInfo{SuccessCount: 1, FailedCount: 1}, want: "partially_failed"},
	}
	// tt 表示当前遍历过程中的tt
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if // got 用于本次流程后续判断的got
			got := finalPublishBatchStatus(&tt.in); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

// TestPublishBatchFailurePreservesUncertainRemoteWarningWhenCanceled 封装Test发布批次FailurePreservesUncertainRemoteWarningWhenCanceled业务协调。
func TestPublishBatchFailurePreservesUncertainRemoteWarningWhenCanceled(t *testing.T) {
	// message、kind 用于本次流程后续判断的message、kind
	message, kind := publishBatchFailure(&uncertainRemotePublishError{err: context.Canceled}, "canceling")
	if kind != "uncertain_remote" || !strings.Contains(message, "任务已取消") ||
		!strings.Contains(message, "禁止自动重试") || !strings.Contains(message, "人工核对") {
		t.Fatalf("message=%q kind=%q", message, kind)
	}
	message, kind = publishBatchFailure(context.Canceled, "canceling")
	if kind != "publish" || message != "任务已取消" {
		t.Fatalf("ordinary cancel message=%q kind=%q", message, kind)
	}
}

// buildImageZip 构造含一张图片的 zip 字节流。
func buildImageZip(t *testing.T) []byte {
	t.Helper()
	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// zw 用于本次流程后续判断的zw
	zw := zip.NewWriter(&buf)
	// f 用于本次流程后续判断的f
	f, _ := zw.Create("img/a.png")
	f.Write(minimalPNG)
	_ = zw.Close()
	return buf.Bytes()
}

// previewPublishBatch 构造一个预检批次（单行商品 + 1 张图片），返回 preview_id。
func previewPublishBatch(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
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
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,img/a.png\n"))
	// zipField 用于本次流程后续判断的zip字段
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(buildImageZip(t))
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
	if res["valid"].(float64) != 1 {
		t.Fatalf("预检应 1 行有效，got %+v", res)
	}
	return res["preview_id"].(string)
}

// previewTwoRowPublishBatch 封装previewTwoRow发布批次业务协调。
func previewTwoRowPublishBatch(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
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
	csvField.Write([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,img/a.png\nacc1,商品B,13.50,5,img/a.png\n"))
	// zipField 用于本次流程后续判断的zip字段
	zipField, _ := mw.CreateFormFile("images_zip", "images.zip")
	zipField.Write(buildImageZip(t))
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
	if res["valid"].(float64) != 2 {
		t.Fatalf("预检应 2 行有效，got %+v", res)
	}
	return res["preview_id"].(string)
}

// TestRunItemPublishBatch_FailureMarksRowFailed 启动批次后，mock mtop 对发布请求返回失败，
// 验证应用层批量 worker 把远端发布失败的行标记为 failed。
// TestRunItemPublishBatch_FailureMarksRowFailed 封装Test运行商品发布批次FailureMarksRow失败业务协调。
func TestRunItemPublishBatch_FailureMarksRowFailed(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入 mock mtop：所有请求返回非成功 ret（触发 PublishError）。
	setTestMTop(srv, withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ret":["FAIL_SYS_PERMISSION::无发布权限"],"data":{}}`)),
			Request:    req,
		}, nil
	})))

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// batchID 用于本次流程后续判断的批次ID
	batchID := previewPublishBatch(t, h, cookie)

	// 启动批次。
	startReq := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"`+batchID+`"}`))
	startReq.AddCookie(cookie)
	// startRec 用于本次流程后续判断的开始Rec
	startRec := httptest.NewRecorder()
	h.ServeHTTP(startRec, startReq)
	if startRec.Code != 200 {
		t.Fatalf("start status=%d body=%s", startRec.Code, startRec.Body.String())
	}

	// 轮询批次状态，等待 running 结束（最多 5 秒）。
	deadline := time.Now().Add(5 * time.Second)
	// status 用于本次流程后续判断的状态
	var status any
	for time.Now().Before(deadline) {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// got 用于本次流程后续判断的got
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
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// got 用于本次流程后续判断的got
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["status"] != "failed" {
		t.Fatalf("status=%v want failed body=%+v", got["status"], got)
	}
	if got["failed"] != float64(1) {
		t.Fatalf("failed=%v want 1 body=%+v", got["failed"], got)
	}
	// rows 用于本次流程后续判断的rows
	rows, _ := got["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows=%+v want 1 row", got["rows"])
	}
	// row 用于本次流程后续判断的row
	row, _ := rows[0].(map[string]any)
	if row["status"] != "failed" || row["error_message"] == "" {
		t.Fatalf("row should be failed with error message: %+v", row)
	}
}

// TestCancelItemPublishBatchMarksUnfinishedRowsFailed 封装Test取消商品发布批次MarksUnfinishedRows失败业务协调。
func TestCancelItemPublishBatchMarksUnfinishedRowsFailed(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	setTestMTop(srv, withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(300 * time.Millisecond):
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ret":["SUCCESS::调用成功"],"data":{"itemId":"late-item"}}`)),
			Request:    req,
		}, nil
	})))

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// batchID 用于本次流程后续判断的批次ID
	batchID := previewTwoRowPublishBatch(t, h, cookie)

	// startReq 用于本次流程后续判断的开始Req
	startReq := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"`+batchID+`"}`))
	startReq.AddCookie(cookie)
	// startRec 用于本次流程后续判断的开始Rec
	startRec := httptest.NewRecorder()
	h.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", startRec.Code, startRec.Body.String())
	}

	// cancelReq 用于本次流程后续判断的取消Req
	cancelReq := httptest.NewRequest(http.MethodPost, "/items/publish-batches/"+batchID+"/cancel", nil)
	cancelReq.AddCookie(cookie)
	// cancelRec 用于本次流程后续判断的取消Rec
	cancelRec := httptest.NewRecorder()
	h.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelRec.Code, cancelRec.Body.String())
	}

	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(2 * time.Second)
	// got 用于本次流程后续判断的got
	var got map[string]any
	for time.Now().Before(deadline) {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", rec.Code, rec.Body.String())
		}
		json.Unmarshal(rec.Body.Bytes(), &got)
		if got["pending"] == float64(0) && got["running"] == float64(0) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got["status"] != "canceled" {
		t.Fatalf("status=%v want canceled body=%+v", got["status"], got)
	}
	if got["pending"] != float64(0) || got["running"] != float64(0) {
		t.Fatalf("取消后不应残留 pending/running: %+v", got)
	}
	if got["failed"] != got["total"] {
		t.Fatalf("取消后未完成行应全部失败: total=%v failed=%v body=%+v", got["total"], got["failed"], got)
	}
	// batch、err 用于本次流程后续判断的batch、err
	batch, err := store.PublishBatches.Get(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(batch.UploadDir) == "" {
		t.Fatal("取消运行中的任务后应保留 upload_dir 供失败项重试")
	}
	if // err 用于本次流程后续判断的err
	_, err := os.Stat(batch.UploadDir); err != nil {
		t.Fatalf("取消运行中的任务后应保留图片目录: %v", err)
	}
}

// TestRunItemPublishBatch_Success 启动批次，mock mtop 全程返回 SUCCESS，验证行最终成功。
func TestRunItemPublishBatch_Success(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// publishedCategory 用于本次流程后续判断的published分类
	var publishedCategory map[string]any
	// 所有 mtop 调用返回 SUCCESS；发布调用返回 itemId。
	setTestMTop(srv, withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// u 用于本次流程后续判断的u
		u := req.URL.String()
		// body 用于本次流程后续判断的请求体
		body := `{"ret":["SUCCESS::调用成功"],"data":{}}`
		switch {
		case strings.Contains(u, "stream-upload.goofish.com"):
			body = `{"object":{"url":"https://img.alicdn.com/published.png","pix":"800_800"}}`
		case strings.Contains(u, "mtop.taobao.idle.kgraph.property.recommend"):
			body = `{"ret":["SUCCESS::调用成功"],"data":{}}`
		case strings.Contains(u, "mtop.idle.pc.idleitem.publish"):
			// rawBody 用于本次流程后续判断的原始请求体
			rawBody, _ := io.ReadAll(req.Body)
			// form 用于本次流程后续判断的表单
			form, _ := url.ParseQuery(string(rawBody))
			// publishData 用于本次流程后续判断的发布数据
			var publishData map[string]any
			_ = json.Unmarshal([]byte(form.Get("data")), &publishData)
			publishedCategory, _ = publishData["itemCatDTO"].(map[string]any)
			body = `{"ret":["SUCCESS::调用成功"],"data":{"itemId":"123456","url":"https://x/item/123456","picUrl":"https://img.alicdn.com/published.png","categoryId":"5001","categoryName":"虚拟商品","title":"商品A","priceText":"12.50","quantity":"5"}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})))

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// batchID 用于本次流程后续判断的批次ID
	batchID := previewPublishBatch(t, h, cookie)

	// startReq 用于本次流程后续判断的开始Req
	startReq := httptest.NewRequest(http.MethodPost, "/items/publish-batches", strings.NewReader(`{"preview_id":"`+batchID+`"}`))
	startReq.AddCookie(cookie)
	// startRec 用于本次流程后续判断的开始Rec
	startRec := httptest.NewRecorder()
	h.ServeHTTP(startRec, startReq)
	if startRec.Code != 200 {
		t.Fatalf("start status=%d body=%s", startRec.Code, startRec.Body.String())
	}

	// 轮询至完成。
	deadline := time.Now().Add(8 * time.Second)
	// status 用于本次流程后续判断的状态
	var status any
	for time.Now().Before(deadline) {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// got 用于本次流程后续判断的got
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
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/items/publish-batches/"+batchID, nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", rec.Code, rec.Body.String())
	}
	// got 用于本次流程后续判断的got
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["status"] != "completed" {
		t.Fatalf("status=%v want completed body=%+v", got["status"], got)
	}
	if got["success"] != float64(1) {
		t.Fatalf("success=%v want 1 body=%+v", got["success"], got)
	}
	// rows 用于本次流程后续判断的rows
	rows, _ := got["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows=%+v want 1 row", got["rows"])
	}
	// row 用于本次流程后续判断的row
	row, _ := rows[0].(map[string]any)
	if row["status"] != "success" || row["item_id"] == "" {
		t.Fatalf("row should be success with item_id: %+v", row)
	}
	if publishedCategory["catId"] != "5001" || publishedCategory["catName"] != "虚拟商品" {
		t.Fatalf("batch did not apply fallback category: %+v", publishedCategory)
	}
}

// TestPublishBatchRetryResumesSavedRemoteResultWithoutPublishingAgain 封装Test发布批次重试ResumesSavedRemote结果WithoutPublishingAgain业务协调。
func TestPublishBatchRetryResumesSavedRemoteResultWithoutPublishingAgain(t *testing.T) {
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
	// batch 用于本次流程后续判断的批次
	batch := &db.ItemPublishBatch{
		ID: "resume-remote", UserID: admin.ID, DefaultCookieID: "acc1", Filename: "resume.csv", Status: "pending",
	}
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.Create(ctx, batch, []db.ItemPublishBatchRow{{
		BatchID: batch.ID, RowNo: 1, CookieID: "acc1", Title: "已发布商品", Description: "详情",
		Price: "12.50", Quantity: 1, PostageMode: "free", AutomationJSON: `{}`,
	}}); err != nil {
		t.Fatal(err)
	}
	// lease 用于本次流程后续判断的lease
	lease := time.Now().UTC().Add(time.Minute).Unix()
	if // claimed、err 用于本次流程后续判断的claimed、err
	claimed, err := store.PublishBatches.ClaimBatch(ctx, batch.ID, "worker-1", lease); err != nil || !claimed {
		t.Fatalf("claim first worker: claimed=%v err=%v", claimed, err)
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := store.PublishBatches.Rows(ctx, batch.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	// rowID 用于本次流程后续判断的rowID
	rowID := rows[0].ID
	if // claimed、err 用于本次流程后续判断的claimed、err
	claimed, err := store.PublishBatches.ClaimRow(ctx, rowID, "worker-1"); err != nil || !claimed {
		t.Fatalf("claim row: claimed=%v err=%v", claimed, err)
	}
	if // saved、err 用于本次流程后续判断的saved、err
	saved, err := store.PublishBatches.SaveClaimedRemoteResult(ctx, rowID, "worker-1", "remote-123", "https://x/item/remote-123", `{"remote":true}`); err != nil || !saved {
		t.Fatalf("save remote result: saved=%v err=%v", saved, err)
	}
	if // marked、err 用于本次流程后续判断的marked、err
	marked, err := store.PublishBatches.MarkClaimedRowFailed(ctx, rowID, "worker-1", "local database unavailable", "post_publish"); err != nil || !marked {
		t.Fatalf("mark post publish failure: marked=%v err=%v", marked, err)
	}
	if // finished、err 用于本次流程后续判断的finished、err
	finished, err := store.PublishBatches.FinishBatchStatus(ctx, batch.ID, "worker-1", "failed"); err != nil || !finished {
		t.Fatalf("finish first batch: finished=%v err=%v", finished, err)
	}
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.ResetFailed(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	if // claimed、err 用于本次流程后续判断的claimed、err
	claimed, err := store.PublishBatches.ClaimBatch(ctx, batch.ID, "worker-2", lease); err != nil || !claimed {
		t.Fatalf("claim retry worker: claimed=%v err=%v", claimed, err)
	}
	rows, _ = store.PublishBatches.Rows(ctx, batch.ID)
	if rows[0].ItemID != "remote-123" || rows[0].Status != "pending" {
		t.Fatalf("retry checkpoint lost: %+v", rows[0])
	}
	if // claimed、err 用于本次流程后续判断的claimed、err
	claimed, err := store.PublishBatches.ClaimRow(ctx, rowID, "worker-2"); err != nil || !claimed {
		t.Fatalf("retry claim row: claimed=%v err=%v", claimed, err)
	}
	// remoteCalls 用于本次流程后续判断的remoteCalls
	remoteCalls := 0
	// client 用于本次流程后续判断的client
	client := withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		remoteCalls++
		return nil, context.Canceled
	}))
	// port 将测试平台客户端接入批量远端应用端口；已保存远端结果时不会再次发起平台请求。
	port := adapter.NewItemBatchPublishPort(store, func() mtop.Client { return client }, srv.Logger, srv.updateRunningCookie, func(callCtx context.Context, cookieID string, callErr error) {
		srv.recoverExpiredSession(callCtx, cookieID, callErr)
	}, readBatchImageFile, downloadImageURL)
	// localService 保存本地商品和批次成功检查点收口能力。
	localService, serviceErr := srv.newBatchLocalPublishService()
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// applicationRow 保存恢复测试所需的非敏感批次明细模型。
	applicationRow := itemapp.BatchRow{ID: rows[0].ID, BatchID: rows[0].BatchID, RowNo: rows[0].RowNo, CookieID: rows[0].CookieID, Title: rows[0].Title, Description: rows[0].Description, Price: rows[0].Price, OriginalPrice: rows[0].OriginalPrice, Quantity: rows[0].Quantity, PostageMode: rows[0].PostageMode, Postage: rows[0].Postage, ImagesJSON: rows[0].ImagesJSON, CategoryJSON: rows[0].CategoryJSON, AutomationJSON: rows[0].AutomationJSON, RawJSON: rows[0].RawJSON, ItemID: rows[0].ItemID, ItemURL: rows[0].ItemURL, Status: rows[0].Status, ErrorMessage: rows[0].ErrorMessage, FailureKind: rows[0].FailureKind, WorkerToken: rows[0].WorkerToken, CreatedAt: rows[0].CreatedAt, UpdatedAt: rows[0].UpdatedAt}
	// publisher、publisherErr 分别保存已装配的批量发布适配器及其组合错误。
	publisher, publisherErr := adapter.NewItemBatchPublisher(port, localService)
	if publisherErr != nil {
		t.Fatalf("构造批量发布适配器: %v", publisherErr)
	}
	// err 保存应用层批量发布适配器的恢复结果。
	err = publisher.PublishRow(ctx, admin.ID, applicationRow, "worker-2", nil)
	if err != nil {
		t.Fatalf("resume local persistence: %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("saved remote result must skip PublishItem, remote calls=%d", remoteCalls)
	}
	rows, _ = store.PublishBatches.Rows(ctx, batch.ID)
	if rows[0].Status != "success" || rows[0].ItemID != "remote-123" {
		t.Fatalf("resumed row=%+v", rows[0])
	}
}

// TestPublishBatchRecoveryAutomaticallyResumesInterruptedRow 封装Test发布批次RecoveryAutomaticallyResumesInterruptedRow业务协调。
func TestPublishBatchRecoveryAutomaticallyResumesInterruptedRow(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// batchID 用于本次流程后续判断的批次ID
	batchID := "auto-recover-batch"
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.Create(ctx, &db.ItemPublishBatch{
		ID: batchID, UserID: admin.ID, DefaultCookieID: "acc1", Filename: "recover.csv", Status: "failed",
	}, []db.ItemPublishBatchRow{{
		RowNo: 1, CookieID: "acc1", Title: "Recovered", Price: "9.90", Quantity: 1,
		Status: "failed", FailureKind: "interrupted", ErrorMessage: "server stopped",
		ItemID: "remote-recovered", ItemURL: "https://example.com/remote-recovered", RawJSON: `{"saved":true}`,
	}}); err != nil {
		t.Fatal(err)
	}
	// err 保存应用层批量恢复扫描的错误。
	if err := startTestBatchRecovery(srv, ctx); err != nil {
		t.Fatalf("批量恢复扫描: %v", err)
	}
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// batch、err 用于本次流程后续判断的batch、err
		batch, err := store.PublishBatches.Get(ctx, admin.ID, batchID)
		if err == nil && batch.Status == "completed" {
			// rows 用于本次流程后续判断的rows
			rows, _ := store.PublishBatches.Rows(ctx, batchID)
			if len(rows) != 1 || rows[0].Status != "success" || rows[0].ItemID != "remote-recovered" {
				t.Fatalf("rows=%+v", rows)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	// batch 用于本次流程后续判断的批次
	batch, _ := store.PublishBatches.Get(ctx, admin.ID, batchID)
	// rows 用于本次流程后续判断的rows
	rows, _ := store.PublishBatches.Rows(ctx, batchID)
	t.Fatalf("batch=%+v rows=%+v", batch, rows)
}

// TestCreatePublishAutomationRules 覆盖自动化规则创建（通过成功发布路径间接覆盖）。
// 这里直接验证 runItemPublishBatch 在成功路径上调用了 createPublishAutomationRules。
// TestCreatePublishAutomationRules 封装TestCreate发布自动化规则列表业务协调。
func TestCreatePublishAutomationRules(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// 预置一个自动化规则配置的批次行，直接调 createPublishAutomationRules 验证不 panic + 写规则。
	// 先建一个卡密组供规则引用。
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// cardID 用于本次流程后续判断的卡密ID
	cardID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "卡", Type: "text", TextContent: "K", Enabled: true, UserID: admin.ID})

	// automationJSON 用于本次流程后续判断的自动化JSON
	automationJSON := `{"paid_delivery":{"enabled":true,"actions":[{"card_id":` + itoa(cardID) + `,"delivery_count":1,"delay_seconds":23}]}}`
	if // err 用于本次流程后续判断的err
	err := srv.createPublishAutomationRules(ctx, admin.ID, itemapp.BatchRow{
		CookieID: "acc1", Title: "商品A", AutomationJSON: automationJSON,
	}, &itemapp.BatchPublishResult{ItemID: "published-delay-item", Title: "商品A"}); err != nil {
		t.Fatal(err)
	}
	// rules、err 用于本次流程后续判断的rules、err
	rules, err := store.Automation.Match(ctx, "acc1", "published-delay-item", automation.TriggerOrderPaid)
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	if len(rules[0].Actions) < 1 || rules[0].Actions[0].DelaySeconds != 23 || !strings.Contains(rules[0].Actions[0].ConfigJSON, `"delay_override":true`) {
		t.Fatalf("batch delay override lost: %+v", rules[0].Actions)
	}
}

// 编译期保证 mtop 包引用。
var _ = mtop.NewClient
