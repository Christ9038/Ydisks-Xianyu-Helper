package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// greetingRequest 发送带认证会话的商品首次回复请求，成功响应同时校验 OpenAPI 契约。
func greetingRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	// request、recorder 保存本次请求及其响应。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(cookie)
	// recorder 捕获本次请求的响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code >= http.StatusOK && recorder.Code < http.StatusMultipleChoices {
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	return recorder
}

// TestItemGreetingHandlersContract 覆盖商品首次回复的创建、列表、更新、冲突、校验、清理和删除真实契约。
func TestItemGreetingHandlersContract(t *testing.T) {
	// srv、store、cleanup 是被测 HTTP 服务、测试数据库聚合和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是 srv 的完整路由树。
	handler := srv.Router()
	// session 是管理员登录后的认证会话 Cookie。
	session := loginHelper(t, handler)
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// err 保存 ExecContext 的返回错误，非空时按该分支处理。
	if _, err := store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES ('acc1','cookie-value',1)`); err != nil {
		t.Fatal(err)
	}
	// 其他用户的账号用于越权断言。
	if _, err := store.Users.Create(ctx, "greeting-other", "greeting-other@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// otherUser 是另一个本地用户，持有越权断言使用的账号。
	otherUser, err := store.Users.GetByUsername(ctx, "greeting-other")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存 ExecContext 的返回错误，非空时按该分支处理。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id) VALUES ('other','cookie-value',?)`, otherUser.ID); err != nil {
		t.Fatal(err)
	}

	// 创建：返回 201 与完整规则，商品去重并裁剪空白。
	created := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings",
		`{"cookie_id":"acc1","name":"欢迎","enabled":true,"reply_content":"你好 {item_id}","item_ids":["i1"," i2 ","i1"]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	// rule 保存响应中的新规则。
	var rule itemGreetingResponse
	// err 保存 Unmarshal 的返回错误，非空时按该分支处理。
	if err := json.Unmarshal(created.Body.Bytes(), &rule); err != nil || rule.ID == 0 || len(rule.ItemIDs) != 2 || rule.ItemIDs[1] != "i2" {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}

	// 列表：按账号过滤与不过滤都能读到。
	for _, path := range []string{"/api/v1/reply-rules/greetings", "/api/v1/reply-rules/greetings?cookie_id=acc1"} {
		// listed 保存 greetingRequest 的返回值。
		listed := greetingRequest(t, handler, session, http.MethodGet, path, "")
		if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"item_ids":["i1","i2"]`) {
			t.Fatalf("list %s status=%d body=%s", path, listed.Code, listed.Body.String())
		}
	}

	// 商品冲突：409 且 details 携带冲突商品。
	conflict := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings",
		`{"cookie_id":"acc1","enabled":true,"reply_content":"x","item_ids":["i2","i3"]}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"item_ids":["i2"]`) {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	// 参数错误：缺商品、缺内容、坏 JSON、非法 id、账号为空。
	for name, c := range map[string]struct{ method, path, body string }{
		"no-items":   {http.MethodPost, "/api/v1/reply-rules/greetings", `{"cookie_id":"acc1","reply_content":"x","item_ids":[]}`},
		"no-content": {http.MethodPost, "/api/v1/reply-rules/greetings", `{"cookie_id":"acc1","item_ids":["z"]}`},
		"bad-json":   {http.MethodPost, "/api/v1/reply-rules/greetings", `not-json`},
		"bad-id":     {http.MethodPut, "/api/v1/reply-rules/greetings/abc", `{}`},
		"no-account": {http.MethodPost, "/api/v1/reply-rules/greetings", `{"reply_content":"x","item_ids":["z"]}`},
	} {
		// got 保存 greetingRequest 的返回值。
		if got := greetingRequest(t, handler, session, c.method, c.path, c.body); got.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", name, got.Code, got.Body.String())
		}
	}

	// 越权与不存在。
	if got := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings", `{"cookie_id":"other","reply_content":"x","item_ids":["z"]}`); got.Code != http.StatusForbidden {
		t.Errorf("forbidden status=%d", got.Code)
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings", `{"cookie_id":"missing","reply_content":"x","item_ids":["z"]}`); got.Code != http.StatusNotFound {
		t.Errorf("missing account status=%d", got.Code)
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/reply-rules/greetings?cookie_id=other", ""); got.Code != http.StatusForbidden {
		t.Errorf("list forbidden status=%d", got.Code)
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/reply-rules/greetings/9999", `{"reply_content":"x","item_ids":["z"]}`); got.Code != http.StatusNotFound {
		t.Errorf("update missing status=%d", got.Code)
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodDelete, "/api/v1/reply-rules/greetings/9999", ""); got.Code != http.StatusNotFound {
		t.Errorf("delete missing status=%d", got.Code)
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings/clear-records/other", ""); got.Code != http.StatusForbidden {
		t.Errorf("clear forbidden status=%d", got.Code)
	}

	// 更新：改内容、释放 i2、占用 i3；不允许迁移账号。
	path := "/api/v1/reply-rules/greetings/" + jsonID(rule.ID)
	// updated 保存 greetingRequest 的返回值。
	updated := greetingRequest(t, handler, session, http.MethodPut, path, `{"name":"改","enabled":false,"reply_content":"新","item_ids":["i1","i3"]}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"enabled":false`) || !strings.Contains(updated.Body.String(), `"item_ids":["i1","i3"]`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodPut, path, `{"cookie_id":"other","reply_content":"x","item_ids":["i1"]}`); got.Code != http.StatusBadRequest {
		t.Errorf("move account status=%d", got.Code)
	}

	// 清理记录与删除。
	if got := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings/clear-records/acc1", ""); got.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", got.Code, got.Body.String())
	}
	// got 保存 greetingRequest 的返回值。
	if got := greetingRequest(t, handler, session, http.MethodDelete, path, ""); got.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", got.Code, got.Body.String())
	}
	// listed 保存 greetingRequest 的返回值。
	if listed := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/reply-rules/greetings", ""); strings.TrimSpace(listed.Body.String()) != "[]" {
		t.Fatalf("列表应为空数组: %s", listed.Body.String())
	}
}

// jsonID 把规则主键格式化为路径片段。
func jsonID(id int64) string {
	// encoded 保存十进制主键文本。
	encoded, _ := json.Marshal(id)
	return string(encoded)
}

// TestItemGreetingHandlersRequireAuth 验证未登录请求被统一拒绝。
func TestItemGreetingHandlersRequireAuth(t *testing.T) {
	// srv、cleanup 是被测 HTTP 服务、测试数据库聚合和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是 srv 的完整路由树。
	handler := srv.Router()
	// c 是当前请求样例。
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/reply-rules/greetings"},
		{http.MethodPost, "/api/v1/reply-rules/greetings"},
		{http.MethodPut, "/api/v1/reply-rules/greetings/1"},
		{http.MethodDelete, "/api/v1/reply-rules/greetings/1"},
		{http.MethodPost, "/api/v1/reply-rules/greetings/clear-records/acc1"},
	} {
		// recorder 捕获本次请求的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(c.method, c.path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status=%d", c.method, c.path, recorder.Code)
		}
	}
}
