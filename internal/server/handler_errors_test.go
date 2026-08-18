package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOrderDetailNotFound 查询不存在的订单应 404。
func TestOrderDetailNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/api/orders/no-such-order", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在订单应 404，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCardEndpointsErrorBranches 卡券相关错误分支：
// 无效 ID → 400；不存在 → 404；请求体非法 → 400。
// TestCardEndpointsErrorBranches 封装Test卡密Endpoints错误Branches业务协调。
func TestCardEndpointsErrorBranches(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 无效 card_id（非数字）→ 400。
	req := httptest.NewRequest(http.MethodGet, "/cards/abc", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}

	// 合法但不存在的 ID → 404。
	req2 := httptest.NewRequest(http.MethodGet, "/cards/999999", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("不存在卡券应 404，got %d", rec2.Code)
	}

	// 创建卡券请求体非法 → 400。
	req3 := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader("not-json"))
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("非法请求体应 400，got %d", rec3.Code)
	}

	// 创建卡券缺类型 → 400（decodeCard 校验）。
	req4 := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(`{"name":"无类型卡"}`))
	req4.AddCookie(cookie)
	// rec4 用于本次流程后续判断的rec4
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusBadRequest {
		t.Fatalf("缺类型应 400，got %d body=%s", rec4.Code, rec4.Body.String())
	}
}

// TestCreateCardRejectsUnsupportedAPITypeAndInvalidDelay 封装TestCreate卡密RejectsUnsupportedAPI类型AndInvalid延迟业务协调。
func TestCreateCardRejectsUnsupportedAPITypeAndInvalidDelay(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 表示当前遍历过程中的请求体
	for _, body := range []string{
		`{"name":"API 卡密","type":"api","enabled":true}`,
		`{"name":"非法类型","type":"unknown","enabled":true}`,
		`{"name":"延时错误","type":"text","text_content":"x","delay_seconds":3601,"enabled":true}`,
		`{"name":"空文本","type":"text","text_content":"  ","enabled":true}`,
		`{"name":"空库存","type":"data","data_content":"\n","enabled":true}`,
		`{"name":"空图片","type":"image","image_url":"","enabled":true}`,
	} {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d want 400", body, rec.Code)
		}
	}
}

// TestProtectedEndpointsRequireAuth 受保护端点未登录应 401。
func TestProtectedEndpointsRequireAuth(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// path 表示当前遍历过程中的路径
	for _, path := range []string{
		"/cards",
		"/api/orders",
		"/cookies/details",
		"/keywords/acc1",
		"/default-replies/acc1",
		"/admin/stats",
		"/ai-reply-settings",
	} {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodGet, path, nil)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s 未登录应 401，got %d", path, rec.Code)
		}
	}
}

// TestAdminEndpointRejectsNonAdmin 非 admin 用户访问管理端点应 403。
func TestAdminEndpointRejectsNonAdmin(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// 创建普通用户并登录。
	store.Users.Create(context.Background(), "user2", "u2@e.com", "pw")
	// body 用于本次流程后续判断的请求体
	body := `{"username":"user2","password":"pw"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("普通用户登录 status=%d", rec.Code)
	}
	// cookie 用于本次流程后续判断的登录凭证
	cookie := rec.Result().Cookies()[0]

	// 访问 admin 端点 → 403。
	req2 := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("普通用户访问 admin 应 403，got %d", rec2.Code)
	}
}

// TestOrderImportEmptyBody 空导入内容应 400。
func TestOrderImportEmptyBody(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空导入应 400，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestLoginMalformedJSON 登录请求体非法 JSON 应不致命（400 或失败响应）。
func TestLoginMalformedJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("not-json"))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法登录请求应 400，got %d", rec.Code)
	}
}
