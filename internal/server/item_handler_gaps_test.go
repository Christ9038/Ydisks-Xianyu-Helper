package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	itemapp "xianyu-go/internal/application/items"
)

// itemGapPublishPort 是单商品发布端口桩，直接返回预设结果与错误。
type itemGapPublishPort struct {
	// outcome 是 PublishSingle 返回的发布结果。
	outcome itemapp.PublishOutcome
	// err 是 PublishSingle 返回的错误。
	err error
}

// PublishSingle 返回预设结果。
func (p itemGapPublishPort) PublishSingle(context.Context, itemapp.PublishInput) (itemapp.PublishOutcome, error) {
	return p.outcome, p.err
}

// itemGapCatalog 是商品目录读取端口桩，所有读取都返回同一个错误。
type itemGapCatalog struct {
	// ItemCatalogPort 提供未覆盖方法的默认（空）实现。
	ItemCatalogPort
	// err 是读取方法返回的错误。
	err error
}

// ListForUser 返回预设错误。
func (c itemGapCatalog) ListForUser(context.Context, int64, string) ([]itemapp.CatalogItem, error) {
	return nil, c.err
}

// ListByCookie 返回预设错误。
func (c itemGapCatalog) ListByCookie(context.Context, string) ([]itemapp.CatalogItem, error) {
	return nil, c.err
}

// Get 返回预设错误。
func (c itemGapCatalog) Get(context.Context, string, string) (itemapp.CatalogItem, error) {
	return itemapp.CatalogItem{}, c.err
}

// itemGapMutation 是商品目录写入端口桩，所有写入都返回同一个错误。
type itemGapMutation struct {
	// err 是写入方法返回的错误。
	err error
}

// Create 返回预设错误。
func (m itemGapMutation) Create(context.Context, string, itemapp.CatalogWriteInput) error {
	return m.err
}

// Update 返回预设错误。
func (m itemGapMutation) Update(context.Context, string, string, itemapp.CatalogPatchInput) error {
	return m.err
}

// Delete 返回预设错误。
func (m itemGapMutation) Delete(context.Context, string, string) error { return m.err }

// SetMultiSpec 返回预设错误。
func (m itemGapMutation) SetMultiSpec(context.Context, string, string, bool) error { return m.err }

// SetMultiQuantity 返回预设错误。
func (m itemGapMutation) SetMultiQuantity(context.Context, string, string, bool) error {
	return m.err
}

// itemGapCredentials 是平台凭证端口桩，LoadOwnedValue 返回预设错误。
type itemGapCredentials struct {
	// PlatformCredentialPort 提供未覆盖方法的默认（空）实现。
	PlatformCredentialPort
	// value 是 LoadOwnedValue 成功时返回的 Cookie 占位值。
	value string
	// err 是 LoadOwnedValue 返回的错误。
	err error
}

// LoadOwnedValue 返回预设结果。
func (c itemGapCredentials) LoadOwnedValue(context.Context, int64, string) (string, error) {
	return c.value, c.err
}

// itemGapSync 是商品同步端口桩，返回预设错误。
type itemGapSync struct {
	// err 是同步方法返回的错误。
	err error
}

// SyncAll 返回预设错误。
func (s itemGapSync) SyncAll(context.Context, itemapp.SyncQuery) (itemapp.SyncAllResult, error) {
	return itemapp.SyncAllResult{}, s.err
}

// SyncPage 返回预设错误。
func (s itemGapSync) SyncPage(context.Context, itemapp.SyncQuery) (itemapp.SyncPageResult, error) {
	return itemapp.SyncPageResult{}, s.err
}

// TestItemCatalogHandlersMapStorageErrors 覆盖商品目录读写失败时的 500 与 404 映射。
func TestItemCatalogHandlersMapStorageErrors(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)
	// boom 是注入的通用存储错误。
	boom := errors.New("boom")
	srv.applications.itemCatalog = itemGapCatalog{err: boom}
	srv.applications.itemCatalogMutation = itemGapMutation{err: boom}

	// cases 描述每个请求及期望状态码。
	cases := []struct {
		// method、path、body 描述请求。
		method, path, body string
		// want 是期望状态码。
		want int
	}{
		{"GET", "/items", ``, 500},
		{"GET", "/items?cookie_id=acc1", ``, 500},
		{"GET", "/items?cookie_id=missing", ``, 403},
		{"GET", "/items/cookie/acc1", ``, 500},
		{"GET", "/items/acc1/i1", ``, 404},
		{"POST", "/items/acc1", `{"item_id":"i1","multi_quantity_delivery":true}`, 500},
		{"POST", "/items/acc1", `{"item_id":"i1","is_multi_qty_ship":true}`, 500},
		{"PUT", "/items/acc1/i1", `{"is_multi_qty_ship":true}`, 500},
		{"DELETE", "/items/acc1/i1", ``, 500},
		{"PUT", "/items/acc1/i1/multi-spec", `{"is_multi_spec":true}`, 500},
		{"PUT", "/items/acc1/i1/multi-quantity-delivery", `{"multi_quantity_delivery":true}`, 500},
	}
	// tc 表示当前请求场景。
	for _, tc := range cases {
		// recorder 保存响应。
		recorder := serveChatCoverageRequest(handler, cookie, tc.method, tc.path, tc.body)
		if recorder.Code != tc.want {
			t.Errorf("%s %s status=%d want %d body=%s", tc.method, tc.path, recorder.Code, tc.want, recorder.Body.String())
		}
	}
	srv.applications.itemCatalogMutation = itemGapMutation{err: itemapp.ErrCatalogNotFound}
	// recorder 本次请求返回的 HTTP 响应记录，用于断言状态码
	if recorder := serveChatCoverageRequest(handler, cookie, "PUT", "/items/acc1/i1", `{}`); recorder.Code != http.StatusNotFound {
		t.Errorf("update not found status=%d", recorder.Code)
	}
}

// TestWriteItemSyncErrorMapping 覆盖商品同步各类应用错误到 HTTP 状态的映射。
func TestWriteItemSyncErrorMapping(t *testing.T) {
	// srv 是只用于调用映射方法的零值 Server。
	srv := &Server{}
	// cases 描述应用错误与期望状态码。
	cases := []struct {
		// err 是注入的同步错误。
		err error
		// want 是期望状态码。
		want int
	}{
		{itemapp.ErrSyncNotOwned, 403},
		{itemapp.ErrSyncInvalidUser, 400},
		{itemapp.ErrSyncInvalidCookie, 400},
		{&itemapp.SyncError{Kind: itemapp.SyncErrorCredential, Err: errors.New("x")}, 409},
		{&itemapp.SyncError{Kind: itemapp.SyncErrorPersistence, Err: errors.New("x")}, 500},
		{&itemapp.SyncError{Kind: itemapp.SyncErrorPlatform, Err: errors.New("x")}, 502},
		{errors.New("other"), 500},
	}
	// tc 表示当前错误映射场景。
	for _, tc := range cases {
		// recorder 保存响应。
		recorder := httptest.NewRecorder()
		srv.writeItemSyncError(recorder, httptest.NewRequest(http.MethodGet, "/x", nil), tc.err)
		if recorder.Code != tc.want {
			t.Errorf("%v => %d want %d", tc.err, recorder.Code, tc.want)
		}
	}
}

// TestItemSyncHandlersPropagateErrors 覆盖同步接口的参数缺失与应用错误映射。
func TestItemSyncHandlersPropagateErrors(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.applications.itemSync = itemGapSync{err: itemapp.ErrSyncNotOwned}
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)
	// path 表示全量与分页同步接口。
	for _, path := range []string{"/items/get-all-from-account", "/items/get-by-page"} {
		// recorder 本次请求返回的 HTTP 响应记录，用于断言状态码
		if recorder := serveChatCoverageRequest(handler, cookie, "POST", path, `{}`); recorder.Code != http.StatusBadRequest {
			t.Errorf("%s missing cookie status=%d", path, recorder.Code)
		}
		// recorder 本次请求返回的 HTTP 响应记录，用于断言状态码
		if recorder := serveChatCoverageRequest(handler, cookie, "POST", path, `{"cookie_id":"acc1"}`); recorder.Code != http.StatusForbidden {
			t.Errorf("%s not owned status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

// TestCookieForCurrentUserErrorBranches 覆盖凭证读取返回空 Cookie 与基础设施错误的响应。
func TestCookieForCurrentUserErrorBranches(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)
	// cases 描述凭证端口错误与期望状态码。
	cases := []struct {
		// err 是凭证端口返回的错误。
		err error
		// want 是期望状态码。
		want int
	}{
		{accountapp.ErrCredentialEmpty, 400},
		{accountapp.ErrForbidden, 403},
		{errors.New("db down"), 500},
	}
	// tc 表示当前凭证错误场景。
	for _, tc := range cases {
		srv.applications.platformCredentials = itemGapCredentials{err: tc.err}
		// body 是满足 cookie_id 的最小 multipart。
		var body bytes.Buffer
		// writer 生成表单。
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("cookie_id", "acc1")
		_ = writer.Close()
		// request 是发布请求。
		request := httptest.NewRequest(http.MethodPost, "/items/publish", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.AddCookie(cookie)
		// recorder 保存响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("%v => %d want %d", tc.err, recorder.Code, tc.want)
		}
	}
}

// publishFormField 描述发布表单中的一个文本字段。
type publishFormField struct {
	// name 是字段名。
	name string
	// value 是字段值。
	value string
}

// postPublishForm 以 multipart 向发布接口提交字段和图片，返回状态码与正文。
func postPublishForm(handler http.Handler, sessionCookie *http.Cookie, fields []publishFormField, images []publishImagePart) (int, string) {
	// body 保存请求体。
	var body bytes.Buffer
	// writer 生成表单。
	writer := multipart.NewWriter(&body)
	// field 表示当前文本字段。
	for _, field := range fields {
		_ = writer.WriteField(field.name, field.value)
	}
	// image 表示当前图片部件。
	for _, image := range images {
		// header 是带可选 Content-Type 的部件头。
		header := textproto.MIMEHeader{"Content-Disposition": []string{`form-data; name="` + image.field + `"; filename="a.png"`}}
		if image.contentType != "" {
			header.Set("Content-Type", image.contentType)
		}
		// part 是图片部件写入器。
		part, _ := writer.CreatePart(header)
		_, _ = part.Write(image.data)
	}
	_ = writer.Close()
	// request 是发布请求。
	request := httptest.NewRequest(http.MethodPost, "/items/publish", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	// recorder 保存响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

// publishImagePart 描述发布表单中的一个图片部件。
type publishImagePart struct {
	// field 是表单字段名（images/image/spec_images）。
	field string
	// contentType 是部件声明的类型；为空时由服务端探测。
	contentType string
	// data 是图片字节。
	data []byte
}

// TestPublishItemValidationAndOutcomeMapping 覆盖单商品发布的字段校验、图片校验与发布结果映射。
func TestPublishItemValidationAndOutcomeMapping(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	// port 是可切换结果的发布端口桩。
	port := &itemGapPublishPort{}
	srv.applications.itemSinglePublish = itemGapPublishDelegate{port}
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)
	// png 是最小 PNG 图片字节。
	png := []byte("\x89PNG\r\n\x1a\n")
	// okImages 是合法的单张主图。
	okImages := []publishImagePart{{field: "images", contentType: "image/png", data: png}}
	// base 返回通过全部基础校验的字段集合。
	base := func(extra ...publishFormField) []publishFormField {
		return append([]publishFormField{{"cookie_id", "acc1"}, {"title", "t"}, {"price", "10"}, {"quantity", "1"}}, extra...)
	}
	// replace 返回把同名字段替换为新值的字段集合。
	replace := func(name, value string) []publishFormField {
		// out 保存替换后的字段。
		var out []publishFormField
		// field 表示基础字段。
		for _, field := range base() {
			if field.name == name {
				field.value = value
			}
			out = append(out, field)
		}
		return out
	}

	// manyImages 是 10 张主图，超过单商品最多 9 张的限制。
	var manyImages []publishImagePart
	// i 循环序号，用于生成不同的测试数据
	for i := 0; i < 10; i++ {
		manyImages = append(manyImages, okImages...)
	}
	// failures 描述各类 400 校验失败。
	failures := []struct {
		// name 是子场景名称。
		name string
		// fields、images 是提交内容。
		fields []publishFormField
		images []publishImagePart
	}{
		{"bad price", replace("price", "abc"), okImages},
		{"bad original price", base(publishFormField{"original_price", "x"}), okImages},
		{"bad postage", base(publishFormField{"postage_mode", "fixed"}), okImages},
		{"no image", base(), nil},
		{"image alias", base(), []publishImagePart{{field: "image", contentType: "text/plain", data: png}}},
		{"empty image", base(), []publishImagePart{{field: "images", contentType: "image/png", data: nil}}},
		{"detected non image", base(), []publishImagePart{{field: "images", data: []byte("plain text")}}},
		{"too many images", base(), manyImages},
		{"bad specs", base(publishFormField{"item_properties", "{"}, publishFormField{"item_sku_list", "[]"}), okImages},
		{"no price without specs", replace("price", "0"), okImages},
		{"bad location", base(publishFormField{"location", "{"}), okImages},
		{"partial category", base(publishFormField{"category_id", "1"}), okImages},
	}
	// tc 表示当前校验失败场景。
	for _, tc := range failures {
		// body、code 分别保存本步骤的返回值：body为本次请求返回的响应正文，用于诊断失败；code为本次请求返回的 HTTP 状态码
		if code, body := postPublishForm(handler, cookie, tc.fields, tc.images); code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", tc.name, code, body)
		}
	}

	// outcomes 描述应用端口返回结果与期望的 HTTP 状态码。
	outcomes := []struct {
		// name 是子场景名称。
		name string
		// outcome、err 是端口返回。
		outcome itemapp.PublishOutcome
		err     error
		// want 是期望状态码。
		want int
	}{
		{"stock permission", itemapp.PublishOutcome{}, &itemapp.PublishError{Code: itemapp.PublishErrorStockPermissionMissing}, 403},
		{"publish error", itemapp.PublishOutcome{}, &itemapp.PublishError{Code: itemapp.PublishErrorUnknown, Ret: []string{"x"}}, 502},
		{"credential changed", itemapp.PublishOutcome{}, errors.New("账号凭证已变化"), 409},
		{"generic error", itemapp.PublishOutcome{}, errors.New("boom"), 502},
		{"blank error", itemapp.PublishOutcome{}, errors.New(" "), 502},
		{"missing item id", itemapp.PublishOutcome{Result: &itemapp.PublishResult{}}, nil, 502},
		{"local save failed", itemapp.PublishOutcome{Result: &itemapp.PublishResult{ItemID: "1"}, LocalSaveErr: errors.New("x")}, nil, 500},
		{"cookie save failed", itemapp.PublishOutcome{Result: &itemapp.PublishResult{ItemID: "1"}, ResponseCookieErr: errors.New("x")}, nil, 500},
		{"success with location", itemapp.PublishOutcome{Result: &itemapp.PublishResult{ItemID: "1"}}, nil, 200},
	}
	// tc 表示当前发布结果场景。
	for _, tc := range outcomes {
		port.outcome, port.err = tc.outcome, tc.err
		// extra 为成功场景附带发货地和规格图片，覆盖 DTO 转换分支。
		extra := []publishFormField{}
		// images 是成功场景附带的主图与规格图片。
		images := okImages
		if tc.want == 200 {
			extra = append(extra, publishFormField{"location", `{"city":"杭州"}`})
			images = append(append([]publishImagePart{}, okImages...), publishImagePart{field: "spec_images", contentType: "image/png", data: png})
		}
		// body、code 分别保存本步骤的返回值：body为本次请求返回的响应正文，用于诊断失败；code为本次请求返回的 HTTP 状态码
		if code, body := postPublishForm(handler, cookie, base(extra...), images); code != tc.want {
			t.Errorf("%s: status=%d want %d body=%s", tc.name, code, tc.want, body)
		}
	}
	// 非 multipart 的请求体直接被拒绝。
	if recorder := serveChatCoverageRequest(handler, cookie, "POST", "/items/publish", `{}`); recorder.Code != http.StatusBadRequest {
		t.Errorf("non multipart status=%d", recorder.Code)
	}
	// 缺少账号字段。
	if code, _ := postPublishForm(handler, cookie, []publishFormField{{"title", strings.Repeat("a", 3)}}, okImages); code != http.StatusBadRequest {
		t.Errorf("missing cookie status=%d", code)
	}
}

// itemGapPublishDelegate 把端口调用转发给可在测试中修改的 itemGapPublishPort。
type itemGapPublishDelegate struct {
	// target 是被转发的可变桩。
	target *itemGapPublishPort
}

// PublishSingle 转发给目标桩。
func (d itemGapPublishDelegate) PublishSingle(ctx context.Context, input itemapp.PublishInput) (itemapp.PublishOutcome, error) {
	return d.target.PublishSingle(ctx, input)
}

// TestItemImageFromDetailFallback 覆盖详情中 item_image 兜底字段和无图详情。
func TestItemImageFromDetailFallback(t *testing.T) {
	// got 被测函数的实际返回值，用于与期望值比较
	if got := itemImageFromDetail(`{"item_image":"https://x/a.png"}`); got != "https://x/a.png" {
		t.Errorf("fallback image = %q", got)
	}
	// got 被测函数的实际返回值，用于与期望值比较
	if got := itemImageFromDetail(`{"pic_info":{"picUrl":"https://x/b.png"}}`); got != "https://x/b.png" {
		t.Errorf("pic image = %q", got)
	}
	// got 被测函数的实际返回值，用于与期望值比较
	if got := itemImageFromDetail(`{"other":1}`); got != "" {
		t.Errorf("no image = %q", got)
	}
	// err 本步骤返回的错误，用于断言失败或成功路径
	if _, err := parseMoneyCents("1.234"); err == nil {
		t.Error("three decimals should fail")
	}
	// err 本步骤返回的错误，用于断言失败或成功路径
	if _, err := parseMoneyCents("1.x"); err == nil {
		t.Error("non numeric fraction should fail")
	}
	// err 本步骤返回的错误，用于断言失败或成功路径
	if _, err := parseMoneyCents("1.2.3"); err == nil {
		t.Error("two dots should fail")
	}
	// err 本步骤返回的错误，用于断言失败或成功路径
	if _, err := parseMoneyCents("x"); err == nil {
		t.Error("non numeric should fail")
	}
}
