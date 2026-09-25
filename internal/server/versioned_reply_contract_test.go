package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// replyRouteCase 描述一个关键词回复或默认回复兼容路由测试样例。
type replyRouteCase struct {
	// name 是测试样例名称。
	name string
	// method 是请求使用的 HTTP 方法。
	method string
	// versionedPath 是版本化入口路径。
	versionedPath string
	// legacyPath 是旧兼容入口路径。
	legacyPath string
	// body 是两条入口共用的请求体。
	body string
	// wantStatus 是两条入口应返回的状态码。
	wantStatus int
}

// requestReplyRoute 发送带认证会话的回复规则请求并返回状态码。
func requestReplyRoute(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method, path, body string) int {
	t.Helper()
	// request 是当前关键词或默认回复兼容入口请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 是捕获兼容入口响应的记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if strings.HasPrefix(path, "/api/v1/") && recorder.Code >= http.StatusOK && recorder.Code < http.StatusMultipleChoices {
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	return recorder.Code
}

// TestVersionedReplyRoutesPreserveLegacyContracts 验证关键词和默认回复入口复用旧 handler 与权限边界。
func TestVersionedReplyRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证关键词和默认回复路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// cases 是覆盖版本化与旧路径状态码兼容性的测试样例集合。
	cases := []replyRouteCase{
		{name: "keywords-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/acc1", legacyPath: "/keywords/acc1", wantStatus: http.StatusOK},
		{name: "keywords-create-invalid", method: http.MethodPost, versionedPath: "/api/v1/reply-rules/acc1", legacyPath: "/keywords/acc1", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "keywords-item-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/acc1/items", legacyPath: "/keywords-with-item-id/acc1", wantStatus: http.StatusOK},
		{name: "keywords-item-create-invalid", method: http.MethodPost, versionedPath: "/api/v1/reply-rules/acc1/items", legacyPath: "/keywords-with-item-id/acc1", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "keywords-typed-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/acc1/typed", legacyPath: "/keywords-with-type/acc1", wantStatus: http.StatusOK},
		{name: "keywords-typed-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/reply-rules/acc1/typed/invalid", legacyPath: "/keywords-with-type/acc1/invalid", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "keywords-typed-delete-invalid", method: http.MethodDelete, versionedPath: "/api/v1/reply-rules/acc1/typed/invalid", legacyPath: "/keywords-with-type/acc1/invalid", wantStatus: http.StatusBadRequest},
		{name: "keywords-index-delete-missing", method: http.MethodDelete, versionedPath: "/api/v1/reply-rules/missing/index/0", legacyPath: "/keywords/missing/0", wantStatus: http.StatusNotFound},
		{name: "item-replies-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/items", legacyPath: "/itemReplays", wantStatus: http.StatusOK},
		{name: "item-reply-get-missing", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/items/acc1/missing", legacyPath: "/item-reply/acc1/missing", wantStatus: http.StatusOK},
		{name: "item-reply-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/reply-rules/items/acc1/item-1", legacyPath: "/item-reply/acc1/item-1", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "item-reply-delete", method: http.MethodDelete, versionedPath: "/api/v1/reply-rules/items/acc1/missing", legacyPath: "/item-reply/acc1/missing", wantStatus: http.StatusOK},
		{name: "default-replies-map", method: http.MethodGet, versionedPath: "/api/v1/default-replies", legacyPath: "/api/default-replies", wantStatus: http.StatusOK},
		{name: "default-replies-list", method: http.MethodGet, versionedPath: "/api/v1/default-replies/list", legacyPath: "/default-replies", wantStatus: http.StatusOK},
		{name: "default-reply-get", method: http.MethodGet, versionedPath: "/api/v1/default-replies/acc1", legacyPath: "/api/default-reply/acc1", wantStatus: http.StatusOK},
		{name: "default-reply-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/default-replies/acc1", legacyPath: "/api/default-reply/acc1", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "default-reply-delete-missing", method: http.MethodDelete, versionedPath: "/api/v1/default-replies/missing", legacyPath: "/default-replies/missing", wantStatus: http.StatusNotFound},
		{name: "default-reply-clear-missing", method: http.MethodPost, versionedPath: "/api/v1/default-replies/missing/clear-records", legacyPath: "/api/default-reply/missing/clear-records", wantStatus: http.StatusNotFound},
	}
	for _, routeCase := range cases { // routeCase 是当前正在执行的回复规则路由样例。
		// versionedStatus 是版本化入口实际返回的状态码。
		versionedStatus := requestReplyRoute(t, handler, sessionCookie, routeCase.method, routeCase.versionedPath, routeCase.body)
		// legacyStatus 是旧兼容入口实际返回的状态码。
		legacyStatus := requestReplyRoute(t, handler, sessionCookie, routeCase.method, routeCase.legacyPath, routeCase.body)
		if versionedStatus != routeCase.wantStatus || legacyStatus != routeCase.wantStatus {
			t.Errorf("%s status versioned=%d legacy=%d want=%d", routeCase.name, versionedStatus, legacyStatus, routeCase.wantStatus)
		}
	}
}

// TestVersionedReplyTypedListExposesItemIDs 验证带类型列表对每条规则都暴露商品范围集合。
func TestVersionedReplyTypedListExposesItemIDs(t *testing.T) {
	// srv、store 与 cleanup 保存带类型列表契约测试使用的服务、数据库聚合和资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// err 表示测试用账号写入失败；测试夹具可能已预置该账号。
	if _, err := store.DB.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES (?,?,?)`, "acc1", "cookie-value", 1); err != nil {
		t.Fatalf("写入测试账号失败: %v", err)
	}
	// seedErr 表示测试用回复规则写入失败。
	if _, seedErr := store.DB.ExecContext(context.Background(),
		`INSERT INTO keywords (cookie_id,keyword,reply,item_id,type) VALUES (?,?,?,?,?)`,
		"acc1", "关键词", "回复", "item-1", "text"); seedErr != nil {
		t.Fatalf("写入测试回复规则失败: %v", seedErr)
	}
	// recorder 保存带类型列表接口的响应。
	recorder := httptest.NewRecorder()
	// request 是带类型列表接口的 GET 请求。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reply-rules/acc1/typed", nil)
	request.AddCookie(sessionCookie)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	// body 是带类型列表的 JSON 响应正文。
	body := recorder.Body.String()
	if !strings.Contains(body, `"item_ids":["item-1"]`) {
		t.Fatalf("响应缺少 item_ids 集合: %s", body)
	}
}

// TestVersionedReplyKeywordExpressionsContract 验证版本化接口保存并回显多表达式正则规则，且拒绝非法正则。
func TestVersionedReplyKeywordExpressionsContract(t *testing.T) {
	// srv、cleanup 保存版本化关键词契约测试使用的服务与资源清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// body 是包含两个 OR 表达式和正则模式的版本化创建请求。
	body := `{"keyword":"^hello","expressions":[" ^hello ","price[0-9]+"],"match_type":"regexp","reply":"已命中","item_id":"","item_ids":[],"type":"text","image_url":""}`
	// request 是创建多表达式规则的版本化 POST 请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reply-rules/acc1/items", strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 捕获创建接口的响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("创建多表达式规则 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)

	// listRequest 是读取带类型规则的版本化 GET 请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reply-rules/acc1/typed", nil)
	listRequest.AddCookie(sessionCookie)
	// listRecorder 捕获规则列表响应。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("读取多表达式规则 status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	assertOpenAPIRecordedSuccessResponse(t, listRequest, listRecorder)
	// rows 保存版本化列表响应中的关键词规则对象。
	var rows []map[string]any
	// decodeErr 表示解析版本化关键词列表响应 JSON 时产生的错误。
	if decodeErr := json.Unmarshal(listRecorder.Body.Bytes(), &rows); decodeErr != nil {
		t.Fatalf("解析规则列表失败: %v", decodeErr)
	}
	if len(rows) != 1 || rows[0]["match_type"] != "regexp" || rows[0]["keyword"] != "^hello" {
		t.Fatalf("多表达式规则兼容字段异常: %+v", rows)
	}
	// expressions、expressionsOK 保存响应中的表达式数组及类型断言结果。
	expressions, expressionsOK := rows[0]["expressions"].([]any)
	if !expressionsOK || len(expressions) != 2 || expressions[0] != "^hello" || expressions[1] != "price[0-9]+" {
		t.Fatalf("多表达式规则回显异常: %+v", rows[0]["expressions"])
	}

	// invalidRequest 是包含非法 RE2 表达式的版本化创建请求。
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/reply-rules/acc1/items", strings.NewReader(`{"keyword":"(","expressions":["("],"match_type":"regexp","reply":"不会保存","item_id":"","item_ids":[],"type":"text","image_url":""}`))
	invalidRequest.AddCookie(sessionCookie)
	// invalidRecorder 捕获非法正则的错误响应。
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法正则应返回 400，实际 status=%d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}
