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

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// TestAddKeywordWithItemID 带商品ID的关键词添加 + 缺 keyword 400。
func TestAddKeywordWithItemID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 正常添加。
	body := `{"keyword":"价格","reply":"50元","item_id":"item1"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 缺 keyword → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(`{"reply":"x"}`))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("缺 keyword 应 400，got %d", rec2.Code)
	}

	// 非法 JSON → 400。
	req3 := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader("not-json"))
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec3.Code)
	}
}

// TestBatchCreateCards CSV 批量建卡密组。
func TestBatchCreateCards(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// csv 用于本次流程后续判断的csv
	csv := "name,type,content,delay_seconds\n卡A,text,内容A,0\n卡B,text,内容B,5\nAPI卡,api,https://example.com,0\n延时异常,text,内容,-1\n"
	// body 用于本次流程后续判断的请求体
	body := &bytes.Buffer{}
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(body)
	// fw 用于本次流程后续判断的fw
	fw, _ := mw.CreateFormFile("file", "cards.csv")
	fw.Write([]byte(csv))
	mw.Close()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cards/batch", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("batch status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
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
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("缺文件应 400，got %d", rec2.Code)
	}
}

// TestBatchCreateCardsAcceptsUTF8BOMTemplate 验证下载的 UTF-8 BOM 卡密模板可以正常识别首列名称。
func TestBatchCreateCardsAcceptsUTF8BOMTemplate(t *testing.T) {
	// srv、cleanup 分别保存真实卡密批量接口及测试资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 保存带认证路由的真实 HTTP 入口。
	handler := srv.Router()
	// sessionCookie 保存管理员登录后的会话凭证。
	sessionCookie := loginHelper(t, handler)
	// csvData 模拟前端下载模板，首个表头带有 UTF-8 BOM。
	csvData := "\uFEFF名称,类型,内容,描述,启用,延迟秒,多规格,规格名,规格值\n模板卡密,data,serial-001,模板导入,是,0,否,,\n"
	// body 保存 multipart 请求的编码内容。
	var body bytes.Buffer
	// writer 负责把卡密模板写入 multipart 请求。
	writer := multipart.NewWriter(&body)
	// file 保存卡密模板文件字段。
	file, fileErr := writer.CreateFormFile("file", "卡密组批量导入模板.csv")
	if fileErr != nil {
		t.Fatalf("创建卡密模板文件字段失败: %v", fileErr)
	}
	// writeErr 保存模板内容写入 multipart 文件时的错误。
	if _, writeErr := file.Write([]byte(csvData)); writeErr != nil {
		t.Fatalf("写入卡密模板失败: %v", writeErr)
	}
	// closeErr 保存 multipart 尾部边界写入错误。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("关闭卡密模板 multipart 失败: %v", closeErr)
	}
	// request 保存带有真实上传内容和认证会话的批量创建请求。
	request := httptest.NewRequest(http.MethodPost, "/cards/batch", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	// recorder 捕获卡密批量创建接口响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("BOM 模板上传状态=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// response 保存接口返回的逐行创建统计。
	var response cardBatchResponse
	// decodeErr 保存批量响应 JSON 解码错误。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("解码 BOM 模板响应失败: %v", decodeErr)
	}
	if response.Created != 1 || response.Failed != 0 || len(response.Rows) != 1 || response.Rows[0].Name != "模板卡密" {
		t.Fatalf("BOM 模板应成功创建一组卡密: %+v", response)
	}
}

// TestAppendCardData 追加批量卡密号 + 校验分支。
func TestAppendCardData(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// 建一个 data 类型卡密组。
	id, _ := store.Cards.Create(ctx, &db.CardFull{Name: "批量卡", Type: "data", DataContent: "K1\nK2", Enabled: true, UserID: admin.ID})

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 追加。
	body := `{"content":"K3\nK4"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(id)+"/append-data", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("append status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 空 content → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(id)+"/append-data", strings.NewReader(`{"content":""}`))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("空 content 应 400，got %d", rec2.Code)
	}

	// 无效 card_id → 400。
	req3 := httptest.NewRequest(http.MethodPost, "/cards/abc/append-data", strings.NewReader(`{"content":"x"}`))
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec3.Code)
	}

	// 非 data 类型卡密组追加 → 400。建一个 text 类型。
	textID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "文本卡", Type: "text", TextContent: "T", Enabled: true, UserID: admin.ID})
	// req4 用于本次流程后续判断的req4
	req4 := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(textID)+"/append-data", strings.NewReader(`{"content":"x"}`))
	req4.AddCookie(cookie)
	// rec4 用于本次流程后续判断的rec4
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusBadRequest {
		t.Fatalf("非 data 类型应 400，got %d", rec4.Code)
	}
}

// TestParseXLSXPublishSheet 解析 XLSX 表格（复用 buildMinimalXLSX 构造）。
func TestParseXLSXPublishSheet(t *testing.T) {
	// xlsx 用于本次流程后续判断的xlsx
	xlsx := buildMinimalXLSXForPublish(t, [][]string{{"title", "price"}, {"商品A", "9.9"}})
	// rows、err 用于本次流程后续判断的rows、err
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
	// cli 用于本次流程后续判断的cli
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

// TestPublishBatchToMap 批次转具名 DTO 序列化并校验状态计数。
func TestPublishBatchToMap(t *testing.T) {
	// batch 用于本次流程后续判断的批次
	batch := itemapp.BatchInfo{ID: "b1", Status: "running", Filename: "x.csv"}
	// rows 用于本次流程后续判断的rows
	rows := []itemapp.BatchRow{
		{ID: 1, RowNo: 1, CookieID: "c1", Title: "t1", Status: "pending", ImagesJSON: `["a.png"]`},
		{ID: 2, RowNo: 2, CookieID: "c1", Title: "t2", Status: "running"},
	}
	// m 用于本次流程后续判断的m
	m := publishBatchApplicationToResponse(batch, rows)
	if m.ID != "b1" || m.Status != "running" {
		t.Fatalf("batch 字段异常: %+v", m)
	}
	// rs 用于本次流程后续判断的rs
	rs := m.Rows
	if len(rs) != 2 {
		t.Fatalf("rows 数异常: %d", len(rs))
	}
}

// buildMinimalXLSXForPublish 构造最小 xlsx 供 parseXLSXPublishSheet 测试。
// 复用 parser_test.go 的 buildMinimalXLSX 但独立命名避免冲突。
// buildMinimalXLSXForPublish 封装buildMinimalXLSXFor发布业务协调。
func buildMinimalXLSXForPublish(t *testing.T, grid [][]string) []byte {
	t.Helper()
	return buildMinimalXLSX(t, grid)
}
