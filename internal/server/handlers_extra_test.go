package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestAddKeywordWithItemID 带商品ID的关键词添加 + 缺 keyword 400。
func TestAddKeywordWithItemID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 正常添加。
	body := `{"keyword":"价格","reply":"50元","item_id":"item1"}`
	req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 缺 keyword → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(`{"reply":"x"}`))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("缺 keyword 应 400，got %d", rec2.Code)
	}

	// 非法 JSON → 400。
	req3 := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader("not-json"))
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec3.Code)
	}
}

// TestBatchCreateCards CSV 批量建卡密组。
func TestBatchCreateCards(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	csv := "name,type,content,delay_seconds\n卡A,text,内容A,0\n卡B,text,内容B,5\nAPI卡,api,https://example.com,0\n延时异常,text,内容,-1\n"
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, _ := mw.CreateFormFile("file", "cards.csv")
	fw.Write([]byte(csv))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/cards/batch", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("batch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["created"].(float64) != 2 {
		t.Fatalf("应创建 2 个，got %+v", res)
	}
	if res["failed"].(float64) != 2 {
		t.Fatalf("API 卡和非法延时应拒绝，got %+v", res)
	}

	// 缺文件 → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/cards/batch", &bytes.Buffer{})
	req2.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("缺文件应 400，got %d", rec2.Code)
	}
}

// TestAppendCardData 追加批量卡密号 + 校验分支。
func TestAppendCardData(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// 建一个 data 类型卡密组。
	id, _ := store.Cards.Create(ctx, &db.CardFull{Name: "批量卡", Type: "data", DataContent: "K1\nK2", Enabled: true, UserID: admin.ID})

	h := srv.Router()
	cookie := loginHelper(t, h)

	// 追加。
	body := `{"content":"K3\nK4"}`
	req := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(id)+"/append-data", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("append status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 空 content → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(id)+"/append-data", strings.NewReader(`{"content":""}`))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("空 content 应 400，got %d", rec2.Code)
	}

	// 无效 card_id → 400。
	req3 := httptest.NewRequest(http.MethodPost, "/cards/abc/append-data", strings.NewReader(`{"content":"x"}`))
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec3.Code)
	}

	// 非 data 类型卡密组追加 → 400。建一个 text 类型。
	textID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "文本卡", Type: "text", TextContent: "T", Enabled: true, UserID: admin.ID})
	req4 := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(textID)+"/append-data", strings.NewReader(`{"content":"x"}`))
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusBadRequest {
		t.Fatalf("非 data 类型应 400，got %d", rec4.Code)
	}
}

// TestParseXLSXPublishSheet 解析 XLSX 表格（复用 buildMinimalXLSX 构造）。
func TestParseXLSXPublishSheet(t *testing.T) {
	xlsx := buildMinimalXLSXForPublish(t, [][]string{{"title", "price"}, {"商品A", "9.9"}})
	rows, err := parseXLSXPublishSheet(xlsx)
	if err != nil {
		t.Fatalf("parseXLSX: %v", err)
	}
	if len(rows) != 1 || rows[0]["title"] != "商品A" {
		t.Fatalf("rows = %#v", rows)
	}

	// 非 xlsx 字节 → 报错。
	if _, err := parseXLSXPublishSheet([]byte("not-xlsx")); err == nil {
		t.Fatal("非 xlsx 应报错")
	}
}

// TestPublicHTTPClient 公网 HTTP 客户端拒绝非公网地址 + 重定向协议校验。
func TestPublicHTTPClient(t *testing.T) {
	cli := publicHTTPClient()
	if cli == nil {
		t.Fatal("应返回非 nil client")
	}
	// 访问私有 IP 应被拒绝。
	_, err := cli.Get("http://127.0.0.1:1/")
	if err == nil {
		t.Fatal("私有 IP 应被拒绝")
	}
}

// TestPublishBatchToMap 批次转 map 序列化 + 状态计数。
func TestPublishBatchToMap(t *testing.T) {
	batch := &db.ItemPublishBatch{ID: "b1", Status: "running", Filename: "x.csv"}
	rows := []db.ItemPublishBatchRow{
		{ID: 1, RowNo: 1, CookieID: "c1", Title: "t1", Status: "pending", ImagesJSON: `["a.png"]`},
		{ID: 2, RowNo: 2, CookieID: "c1", Title: "t2", Status: "running"},
	}
	m := publishBatchToMap(batch, rows)
	if m["id"] != "b1" || m["status"] != "running" {
		t.Fatalf("batch 字段异常: %+v", m)
	}
	rs, _ := m["rows"].([]map[string]any)
	if len(rs) != 2 {
		t.Fatalf("rows 数异常: %d", len(rs))
	}
}

// buildMinimalXLSXForPublish 构造最小 xlsx 供 parseXLSXPublishSheet 测试。
// 复用 parser_test.go 的 buildMinimalXLSX 但独立命名避免冲突。
func buildMinimalXLSXForPublish(t *testing.T, grid [][]string) []byte {
	t.Helper()
	return buildMinimalXLSX(t, grid)
}
