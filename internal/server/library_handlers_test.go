package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// libraryPNG 是 1×1 透明 PNG，用于图片库上传测试。
var libraryPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

// libraryRequest 以登录会话执行一次资源库请求；2xx 响应同时校验 OpenAPI 契约并记录覆盖。
func libraryRequest(t *testing.T, handler http.Handler, session *http.Cookie, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	// request 是本次资源库 HTTP 请求。
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.AddCookie(session)
	// recorder 捕获响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code >= http.StatusOK && recorder.Code < http.StatusMultipleChoices {
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	return recorder
}

// libraryUploadBody 构造含若干文件的 multipart 上传体，返回请求体和 Content-Type。
func libraryUploadBody(t *testing.T, files map[string][]byte) ([]byte, string) {
	t.Helper()
	// buffer、writer 保存 multipart 编码结果。
	var buffer bytes.Buffer
	// writer 保存 NewWriter 的返回结果。
	writer := multipart.NewWriter(&buffer)
	// name、data 是当前文件名与内容。
	for name, data := range files {
		// part 保存 CreateFormFile 的返回结果，err 保存其失败原因。
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
	}
	// err 保存 Close 调用的失败原因。
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), writer.FormDataContentType()
}

// TestLibraryHandlersContract 覆盖图片库与商品模板的上传、读取、引用保护、模板 CRUD 和发布预检真实契约。
func TestLibraryHandlersContract(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", t.TempDir())
	// srv、store、cleanup 是被测 HTTP 服务、测试数据库和释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树；session 是管理员会话。
	handler := srv.Router()
	// session 保存 loginHelper 的返回结果。
	session := loginHelper(t, handler)
	// err 保存 ExecContext 调用的失败原因。
	if _, err := store.DB.ExecContext(t.Context(), `INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES ('acc1','cookie-value',1)`); err != nil {
		t.Fatal(err)
	}

	// 上传：一张有效图片与一个非图片文件，部分成功时返回 200 并逐文件给出结果。
	body, contentType := libraryUploadBody(t, map[string][]byte{"a.png": libraryPNG, "bad.txt": []byte("not image")})
	// uploaded 保存 libraryRequest 的返回结果。
	uploaded := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/images", contentType, body)
	// upload 保存上传响应。
	var upload libraryImageUploadResponse
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal(uploaded.Body.Bytes(), &upload); err != nil || uploaded.Code != http.StatusOK || len(upload.Results) != 2 {
		t.Fatalf("upload status=%d body=%s", uploaded.Code, uploaded.Body.String())
	}
	// imageID 是成功入库图片的主键。
	var imageID int64
	// result 是遍历 upload.Results 时的当前元素。
	for _, result := range upload.Results {
		if result.Image != nil {
			imageID = result.Image.ID
		} else if result.Error == "" {
			t.Fatalf("失败项缺少原因: %+v", result)
		}
	}
	if imageID == 0 {
		t.Fatalf("没有图片入库: %s", uploaded.Body.String())
	}
	// 全部失败：400。
	badBody, badType := libraryUploadBody(t, map[string][]byte{"bad.txt": []byte("x")})
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/images", badType, badBody); rec.Code != http.StatusBadRequest {
		t.Fatalf("全失败上传 status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列表与内容。
	if rec := libraryRequest(t, handler, session, http.MethodGet, "/api/v1/library/images", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"a.png"`) {
		t.Fatalf("list images status=%d body=%s", rec.Code, rec.Body.String())
	}
	// imagePath 是目标图片内容路径。
	imagePath := "/api/v1/library/images/" + itoa(imageID)
	// content 保存 libraryRequest 的返回结果。
	content := libraryRequest(t, handler, session, http.MethodGet, imagePath+"/content", "", nil)
	if content.Code != http.StatusOK || !bytes.Equal(content.Body.Bytes(), libraryPNG) || content.Header().Get("X-Content-Type-Options") != "nosniff" || content.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content status=%d headers=%v", content.Code, content.Header())
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodGet, "/api/v1/library/images/999999/content", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing content status=%d", rec.Code)
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodGet, "/api/v1/library/images/abc/content", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status=%d", rec.Code)
	}

	// 模板 CRUD：创建返回 201。
	templateBody := `{"name":"模板A","title":"标题A","description":"描述","price":"9.90","quantity":3,"postage_mode":"free","postage":"0",` +
		`"category":{"cat_id":"1","cat_name":"测试"},"image_ids":[` + itoa(imageID) + `],` +
		`"specs":{"specs":[],"skus":[],"spec_image_ids":[]},"automation":{"paid_delivery":{"enabled":false,"actions":[]},"review_gift":{"enabled":false,"actions":[]},"review_request":{"enabled":false,"after_shipped_hours":0,"message":"","max_attempts":0,"delay_seconds":0}}}`
	// created 保存 libraryRequest 的返回结果。
	created := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-templates", "application/json", []byte(templateBody))
	// template 保存新建模板响应。
	var template libraryTemplateResponse
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal(created.Body.Bytes(), &template); err != nil || created.Code != http.StatusCreated || template.ID == 0 {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	// templatePath 是目标模板路径。
	templatePath := "/api/v1/library/product-templates/" + itoa(template.ID)
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodGet, "/api/v1/library/product-templates", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "模板A") {
		t.Fatalf("list templates status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodGet, templatePath, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("get template status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodPut, templatePath, "application/json", []byte(strings.Replace(templateBody, "模板A", "模板B", 1))); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "模板B") {
		t.Fatalf("update template status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 校验失败与不存在。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-templates", "application/json", []byte(`{"name":""}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid template status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-templates", "application/json", []byte(`{`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status=%d", rec.Code)
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodGet, "/api/v1/library/product-templates/999999", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing template status=%d", rec.Code)
	}

	// 被模板引用的图片不可删除：409 且 details 携带模板名。
	if rec := libraryRequest(t, handler, session, http.MethodDelete, imagePath, "", nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "模板B") {
		t.Fatalf("referenced delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 发布预检：生成 preview 批次；非法间隔与空选择返回 400。
	previewBody := `{"template_ids":[` + itoa(template.ID) + `],"cookie_ids":["acc1"],"location":{"province":"浙江","city":"杭州","area":"西湖","division_id":"330106"},"publish_interval_seconds":5}`
	// preview 保存 libraryRequest 的返回结果。
	preview := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-templates/publish-preview", "application/json", []byte(previewBody))
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-templates/publish-preview", "application/json", []byte(`{"template_ids":[1],"cookie_ids":["acc1"],"publish_interval_seconds":99999}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad interval status=%d", rec.Code)
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodPost, "/api/v1/library/product-templates/publish-preview", "application/json", []byte(`{"template_ids":[],"cookie_ids":[]}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty preview status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 删除模板后图片可删除；再次删除返回 404。
	if rec := libraryRequest(t, handler, session, http.MethodDelete, templatePath, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete template status=%d", rec.Code)
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodDelete, imagePath, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete image status=%d body=%s", rec.Code, rec.Body.String())
	}
	// rec 保存 libraryRequest 的返回结果。
	if rec := libraryRequest(t, handler, session, http.MethodDelete, imagePath, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing image status=%d", rec.Code)
	}
}
