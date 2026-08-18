package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPasswordLoginAPIsArePermanentlyDisabled 封装Test密码登录APIsArePermanentlyDisabled业务协调。
func TestPasswordLoginAPIsArePermanentlyDisabled(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// authCookie 用于本次流程后续判断的auth登录凭证
	authCookie := loginHelper(t, h)

	// requests 用于本次流程后续判断的请求列表
	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/password-login", strings.NewReader(`{"account_id":"acc1","account":"u","password":"p"}`)),
		httptest.NewRequest(http.MethodGet, "/password-login/check/legacy", nil),
		httptest.NewRequest(http.MethodDelete, "/password-login/cancel/legacy", nil),
	}
	// req 表示当前遍历过程中的req
	for _, req := range requests {
		req.AddCookie(authCookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL.Path, rec.Code, rec.Body.String())
		}
		// result 用于本次流程后续判断的结果
		var result map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["code"] != "password_login_disabled" || result["message"] == "" {
			t.Fatalf("%s %s 应永久禁用: %+v", req.Method, req.URL.Path, result)
		}
	}
}

// TestPasswordLoginRouteDelegatesDisabledPolicy 验证旧路径与版本化路径均通过应用服务返回关闭策略，而不是解析或传播密码。
func TestPasswordLoginRouteDelegatesDisabledPolicy(t *testing.T) {
	// srv、cleanup 保存带完整应用服务装配的 HTTP 测试服务及其释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 保存当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 保存管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// paths 保存需要验证的旧路径和版本化路径。
	paths := []string{"/password-login", "/api/v1/password-login"}
	for _, path := range paths { // path 表示当前待验证的密码登录入口。
		// request 使用故意无法解析的正文，确认关闭策略先于请求体解析执行。
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("not-json-password-body"))
		request.AddCookie(sessionCookie)
		// recorder 捕获当前入口的 HTTP 响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotImplemented {
			t.Fatalf("%s 应由应用策略返回 501，got %d body=%s", path, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "not-json-password-body") {
			t.Fatalf("%s 不得回显密码登录请求体", path)
		}
	}
}
