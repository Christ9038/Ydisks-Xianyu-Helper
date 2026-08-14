package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPasswordLoginAPIsArePermanentlyDisabled 负责Test密码登录APIsArePermanentlyDisabled相关处理。
func TestPasswordLoginAPIsArePermanentlyDisabled(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// authCookie 保存auth登录凭证，供当前处理流程使用
	authCookie := loginHelper(t, h)

	// requests 保存请求列表，供当前处理流程使用
	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/password-login", strings.NewReader(`{"account_id":"acc1","account":"u","password":"p"}`)),
		httptest.NewRequest(http.MethodGet, "/password-login/check/legacy", nil),
		httptest.NewRequest(http.MethodDelete, "/password-login/cancel/legacy", nil),
	}
	// req 表示当前遍历过程中的req
	for _, req := range requests {
		req.AddCookie(authCookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL.Path, rec.Code, rec.Body.String())
		}
		// result 保存结果，供当前处理流程使用
		var result map[string]any
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["code"] != "password_login_disabled" || result["message"] == "" {
			t.Fatalf("%s %s 应永久禁用: %+v", req.Method, req.URL.Path, result)
		}
	}
}
