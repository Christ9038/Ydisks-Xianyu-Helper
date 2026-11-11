package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	libraryapp "xianyu-go/internal/application/library"
)

// libraryImportBody 构造导入预检的 multipart 请求体；zipData 为空时不附带图片压缩包。
func libraryImportBody(t *testing.T, sheetName, sheet string, zipData []byte) ([]byte, string) {
	t.Helper()
	// buffer、writer 保存 multipart 编码结果。
	var buffer bytes.Buffer
	// writer 是 multipart 写入器。
	writer := multipart.NewWriter(&buffer)
	if sheetName != "" {
		// part、err 保存表格字段写入器。
		part, err := writer.CreateFormFile("file", sheetName)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(sheet))
	}
	if len(zipData) > 0 {
		// part、err 保存压缩包字段写入器。
		part, err := writer.CreateFormFile("images_zip", "images.zip")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(zipData)
	}
	// err 保存 Close 调用的失败原因。
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), writer.FormDataContentType()
}

// libraryImportZip 构造只含 images/a.png 的压缩包，内容为有效 PNG。
func libraryImportZip(t *testing.T) []byte {
	t.Helper()
	// buffer 保存压缩包字节。
	var buffer bytes.Buffer
	// writer 写入压缩包条目。
	writer := zip.NewWriter(&buffer)
	// entry、err 保存条目写入器。
	entry, err := writer.Create("images/a.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write(libraryPNG)
	// closeErr 保存压缩包收尾错误。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return buffer.Bytes()
}

// TestLibraryTemplateImportHandlersContract 覆盖 Excel 导入商品模板的预检、分段提交、丢弃与错误路径的真实契约。
func TestLibraryTemplateImportHandlersContract(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", t.TempDir())
	// srv、cleanup 是被测 HTTP 服务与释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树；session 是管理员会话。
	handler := srv.Router()
	// session 保存 loginHelper 的返回结果。
	session := loginHelper(t, handler)
	// sheet 包含一行可导入（压缩包图片）与一行价格非法。
	sheet := "模板名称,标题,价格,库存,邮费模式,图片\n网盘A,资源A,9.90,5,none,images/a.png\n,价格错,abc,1,free,images/a.png\n"
	// body、contentType 是预检请求体。
	body, contentType := libraryImportBody(t, "t.csv", sheet, libraryImportZip(t))
	// previewRec 保存预检响应。
	previewRec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports", contentType, body)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("预检失败: %d %s", previewRec.Code, previewRec.Body.String())
	}
	// preview 保存解码后的预检响应。
	var preview libraryImportPreviewResponse
	// err 保存预检响应体的 JSON 解码错误。
	if err := json.Unmarshal(previewRec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ImportID == "" || preview.Valid != 1 || preview.Invalid != 1 || !preview.Rows[0].Valid || preview.Rows[1].Valid || len(preview.Rows[1].Errors) == 0 || preview.Rows[0].Warnings == nil {
		t.Fatalf("预检结果异常: %+v", preview)
	}
	// commitRec 保存提交响应。
	commitRec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports/"+preview.ImportID+"/commit", "", nil)
	if commitRec.Code != http.StatusOK {
		t.Fatalf("提交失败: %d %s", commitRec.Code, commitRec.Body.String())
	}
	// progress 保存解码后的进度。
	var progress libraryImportProgressResponse
	// err 保存提交进度响应体的 JSON 解码错误。
	if err := json.Unmarshal(commitRec.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if !progress.Done || progress.Created != 1 || progress.Invalid != 1 || len(progress.CreatedTemplateIDs) != 1 {
		t.Fatalf("提交进度异常: %+v", progress)
	}
	// listRec 保存模板列表，新模板应已存在。
	listRec := libraryRequest(t, handler, session, http.MethodGet, "/api/v1/library/product-templates", "", nil)
	if !strings.Contains(listRec.Body.String(), `"name":"网盘A"`) || !strings.Contains(listRec.Body.String(), `"postage_mode":"none"`) {
		t.Fatalf("导入的模板未出现在列表中: %s", listRec.Body.String())
	}
	// 完成后任务已删除：再次提交返回 404。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports/"+preview.ImportID+"/commit", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("完成后的任务应返回 404: %d", rec.Code)
	}
	// 新建一个任务后丢弃。
	body, contentType = libraryImportBody(t, "t.csv", sheet, libraryImportZip(t))
	// second 保存第二个任务的预检结果。
	var second libraryImportPreviewResponse
	_ = json.Unmarshal(libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports", contentType, body).Body.Bytes(), &second)
	// rec 保存本次调用的返回值，用于断言：丢弃失败。
	if rec := libraryRequest(t, handler, session, http.MethodDelete, "/api/v1/library/product-template-imports/"+second.ImportID, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("丢弃失败: %d %s", rec.Code, rec.Body.String())
	}
	// rec 保存本次调用的返回值，用于断言：丢弃后提交应返回 404。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports/"+second.ImportID+"/commit", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("丢弃后提交应返回 404: %d", rec.Code)
	}
	// 错误路径：缺少表格、非 multipart、表格格式错误、全部无效时返回空任务标识。
	body, contentType = libraryImportBody(t, "", "", libraryImportZip(t))
	// rec 保存本次调用的返回值，用于断言：缺少表格应返回 400。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports", contentType, body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "缺少商品表格文件") {
		t.Fatalf("缺少表格应返回 400: %d %s", rec.Code, rec.Body.String())
	}
	// rec 保存本次调用的返回值，用于断言：非 multipart 应返回 400。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports", "application/json", []byte("{}")); rec.Code != http.StatusBadRequest {
		t.Fatalf("非 multipart 应返回 400: %d", rec.Code)
	}
	body, contentType = libraryImportBody(t, "t.csv", "标题\n", nil)
	// rec 保存本次调用的返回值，用于断言：只有表头应返回 400。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports", contentType, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("只有表头应返回 400: %d %s", rec.Code, rec.Body.String())
	}
	body, contentType = libraryImportBody(t, "t.csv", "标题,价格,图片\n缺图,1,images/none.png\n", nil)
	// invalidRec 保存全部无效时的预检响应。
	invalidRec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-template-imports", contentType, body)
	if invalidRec.Code != http.StatusOK || !strings.Contains(invalidRec.Body.String(), `"import_id":""`) {
		t.Fatalf("全部无效时应返回空任务标识: %d %s", invalidRec.Code, invalidRec.Body.String())
	}
}

// TestLibraryImportFileReadAndErrorMapping 覆盖上传文件超限与读取失败，以及导入占用错误映射为 409。
func TestLibraryImportFileReadAndErrorMapping(t *testing.T) {
	// body、contentType 是含 2 字节表格的请求体。
	body, contentType := libraryImportBody(t, "t.csv", "ab", nil)
	// request 是解析后的 multipart 请求。
	request := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	// err 保存测试请求 multipart 表单解析错误。
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	// recorder 捕获超限响应。
	recorder := httptest.NewRecorder()
	// ok 保存本次调用的返回值，用于断言：超限应返回 400。
	if _, _, ok := readLibraryImportFile(recorder, request, "file", 1, true); ok || recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "超过大小限制") {
		t.Fatalf("超限应返回 400: %d %s", recorder.Code, recorder.Body.String())
	}
	// 文件名为空时回退为 products.csv。
	request.MultipartForm.File["file"][0].Filename = ""
	// name、ok 保存本次调用的返回值，用于断言：空文件名应回退为 products.csv。
	if _, name, ok := readLibraryImportFile(httptest.NewRecorder(), request, "file", 10, true); !ok || name != "products.csv" {
		t.Fatalf("空文件名应回退为 products.csv: %q %v", name, ok)
	}
	// busy 捕获导入占用错误映射。
	busy := httptest.NewRecorder()
	writeLibraryError(busy, httptest.NewRequest(http.MethodPost, "/x", nil), libraryapp.ErrImportBusy, "fallback")
	if busy.Code != http.StatusConflict {
		t.Fatalf("导入占用应映射为 409: %d", busy.Code)
	}
}

// libraryImportPortStub 在资源库端口上只替换导入三个方法，返回预置错误，用于覆盖处理器的错误映射。
type libraryImportPortStub struct {
	LibraryPort
	// err 是三个导入方法统一返回的错误。
	err error
}

// PreviewImport 返回预置错误。
func (stub libraryImportPortStub) PreviewImport(context.Context, libraryapp.PreviewImportInput) (libraryapp.ImportPreview, error) {
	return libraryapp.ImportPreview{}, stub.err
}

// CommitImport 返回预置错误。
func (stub libraryImportPortStub) CommitImport(context.Context, int64, string) (libraryapp.ImportProgress, error) {
	return libraryapp.ImportProgress{}, stub.err
}

// DiscardImport 返回预置错误。
func (stub libraryImportPortStub) DiscardImport(context.Context, int64, string) error {
	return stub.err
}

// TestLibraryImportHandlersErrorsAndAuth 覆盖未登录、应用错误映射（占用 409、系统故障 500）与压缩包超限。
func TestLibraryImportHandlersErrorsAndAuth(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", t.TempDir())
	// srv、cleanup 是被测 HTTP 服务与释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树；session 是管理员会话。
	handler := srv.Router()
	// session 保存 loginHelper 的返回结果。
	session := loginHelper(t, handler)
	// routes 是三个导入接口的方法与路径。
	routes := [][2]string{{http.MethodPost, "/api/v1/library/product-template-imports"}, {http.MethodPost, "/api/v1/library/product-template-imports/import_x/commit"}, {http.MethodDelete, "/api/v1/library/product-template-imports/import_x"}}
	// route 是当前接口。
	for _, route := range routes {
		// recorder 捕获未登录请求的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(route[0], route[1], nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 未登录应返回 401: %d", route[0], route[1], recorder.Code)
		}
	}
	// cases 是注入的应用错误与期望状态码。
	cases := []struct {
		err  error
		want int
	}{{libraryapp.ErrImportBusy, http.StatusConflict}, {errors.New("disk down"), http.StatusInternalServerError}}
	// item 是当前错误用例。
	for _, item := range cases {
		srv.applications.library = libraryImportPortStub{err: item.err}
		// body、contentType 是合法的预检请求体。
		body, contentType := libraryImportBody(t, "t.csv", "标题\n商品\n", nil)
		// requests 是三个接口的请求。
		requests := []*http.Request{httptest.NewRequest(http.MethodPost, routes[0][1], bytes.NewReader(body)), httptest.NewRequest(routes[1][0], routes[1][1], nil), httptest.NewRequest(routes[2][0], routes[2][1], nil)}
		requests[0].Header.Set("Content-Type", contentType)
		// request 是当前请求。
		for _, request := range requests {
			request.AddCookie(session)
			// recorder 捕获响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != item.want || strings.Contains(recorder.Body.String(), "disk down") {
				t.Fatalf("%s %s 错误 %v 应返回 %d 且不泄露内部原因: %d %s", request.Method, request.URL.Path, item.err, item.want, recorder.Code, recorder.Body.String())
			}
		}
	}
	// zipBody、zipType 是附带 3 字节压缩包的请求体，用 2 字节上限触发超限。
	zipBody, zipType := libraryImportBody(t, "t.csv", "a", []byte("zip"))
	// request 是解析后的 multipart 请求。
	request := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(zipBody))
	request.Header.Set("Content-Type", zipType)
	// parseErr 保存测试请求 multipart 表单解析错误。
	if parseErr := request.ParseMultipartForm(1 << 20); parseErr != nil {
		t.Fatal(parseErr)
	}
	// recorder 捕获压缩包超限响应。
	recorder := httptest.NewRecorder()
	// ok 表示读取是否成功，压缩包超限应失败。
	if _, ok := readLibraryImportInput(recorder, request, 10, 2); ok || recorder.Code != http.StatusBadRequest {
		t.Fatalf("压缩包超限应返回 400: %d", recorder.Code)
	}
}

// TestLibraryImportHandlersRequireSession 绕过认证中间件直接调用处理器，验证缺少会话时返回 401 而不调用应用服务。
func TestLibraryImportHandlersRequireSession(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", t.TempDir())
	// srv、cleanup 是测试服务及释放函数；注入会报错的替身，任何应用调用都会使状态码不是 401。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.applications.library = libraryImportPortStub{err: errors.New("不应调用")}
	// handlers 是三个导入处理器。
	handlers := []http.HandlerFunc{srv.previewLibraryTemplateImport, srv.commitLibraryTemplateImport, srv.discardLibraryTemplateImport}
	// handler 是当前处理器。
	for _, handler := range handlers {
		// recorder 捕获响应。
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodPost, "/x", nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("缺少会话应返回 401: %d", recorder.Code)
		}
	}
}
