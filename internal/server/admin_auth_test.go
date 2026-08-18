package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAdminListCookies 管理员列出所有账号。
func TestAdminListCookies(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/admin/cookies", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// arr 用于本次流程后续判断的arr
	var arr []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["id"] != "acc1" {
		t.Fatalf("账号列表异常: %+v", arr)
	}
}

// TestAdminDeleteUser 删除其他用户。
func TestAdminDeleteUser(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.Users.Create(ctx, "user-del", "u@e.com", "pw")
	// u 用于本次流程后续判断的u
	u, _ := store.Users.GetByUsername(ctx, "user-del")
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+itoa(u.ID), nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminDeleteUserRevokesSessions 封装TestAdminDelete用户RevokesSessions业务协调。
func TestAdminDeleteUserRevokesSessions(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "session-del", "session-del@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// u、err 用于本次流程后续判断的u、err
	u, err := store.Users.GetByUsername(ctx, "session-del")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// userLogin 用于本次流程后续判断的用户登录
	userLogin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"session-del","password":"pw"}`))
	// userLoginRec 用于本次流程后续判断的用户登录Rec
	userLoginRec := httptest.NewRecorder()
	h.ServeHTTP(userLoginRec, userLogin)
	if userLoginRec.Code != http.StatusOK || len(userLoginRec.Result().Cookies()) == 0 {
		t.Fatalf("user login status=%d body=%s", userLoginRec.Code, userLoginRec.Body.String())
	}
	// userCookie 用于本次流程后续判断的用户登录凭证
	userCookie := userLoginRec.Result().Cookies()[0]
	// adminCookie 用于本次流程后续判断的admin登录凭证
	adminCookie := loginHelper(t, h)

	// delReq 用于本次流程后续判断的delReq
	delReq := httptest.NewRequest(http.MethodDelete, "/admin/users/"+itoa(u.ID), nil)
	delReq.AddCookie(adminCookie)
	// delRec 用于本次流程后续判断的delRec
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}

	// verifyReq 用于本次流程后续判断的verifyReq
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(userCookie)
	// verifyRec 用于本次流程后续判断的verifyRec
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	// verify 用于本次流程后续判断的verify
	var verify map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(verifyRec.Body.Bytes(), &verify); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if verify["authenticated"] == true {
		t.Fatalf("deleted user's old session should be invalid: %+v", verify)
	}
}

// TestAdminDeleteUserSelf 不能删除自己 400。
func TestAdminDeleteUserSelf(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+itoa(admin.ID), nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不能删自己应 400，got %d", rec.Code)
	}
}

// TestAdminDeleteUserBadID 无效 ID 400。
func TestAdminDeleteUserBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/admin/users/abc", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestAdminStatsWithOrders 带订单的统计。
func TestAdminStatsWithOrders(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id, order_status) VALUES ('s1','i1','acc1','2')`)
	store.DB.ExecContext(ctx, `INSERT INTO cards (name, type, user_id) VALUES ('卡1','text',1)`)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	// stats 用于本次流程后续判断的stats
	var stats map[string]any
	json.Unmarshal(rec.Body.Bytes(), &stats)
	if stats["total_orders"] != float64(1) || stats["total_cards"] != float64(1) {
		t.Fatalf("stats 异常: %+v", stats)
	}
}

// TestChangeAdminPassword 修改管理员密码成功。
func TestChangeAdminPassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"pw","new_password":"newpw123"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("应成功: %+v", res)
	}
}

// TestChangeAdminPasswordWrongCurrent 当前密码错误。
func TestChangeAdminPasswordWrongCurrent(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"wrong","new_password":"newpw123"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// res 用于本次流程后续判断的响应
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] == true {
		t.Fatal("错误密码不应成功")
	}
}

// TestChangeAdminPasswordBadJSON 非法 JSON 400。
func TestChangeAdminPasswordBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestChangeAdminPasswordShortNewPassword 封装TestChangeAdmin密码ShortNew密码业务协调。
func TestChangeAdminPasswordShortNewPassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(`{"current_password":"pw","new_password":"123"}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("管理员新密码过短应 400，got %d", rec.Code)
	}
}

// TestChangeAdminPasswordNonAdmin 非 admin 403。
func TestChangeAdminPasswordNonAdmin(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	store.Users.Create(context.Background(), "user2", "u2@e.com", "pw")
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// body 用于本次流程后续判断的请求体
	body := `{"username":"user2","password":"pw"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cookie 用于本次流程后续判断的登录凭证
	cookie := rec.Result().Cookies()[0]

	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(`{"current_password":"pw","new_password":"newpw123"}`))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("非 admin 应 403，got %d", rec2.Code)
	}
}

// TestChangePassword 修改当前用户密码。
func TestChangePassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"pw","new_password":"newpw123"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("应成功: %+v", res)
	}
}

// TestChangePasswordRevokesCurrentSession 封装TestChange密码RevokesCurrent会话业务协调。
func TestChangePasswordRevokesCurrentSession(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(`{"current_password":"pw","new_password":"newpw123"}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("change status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode change response: %v", err)
	}
	if res["success"] != true || res["requires_relogin"] != true {
		t.Fatalf("change response should require relogin: %+v", res)
	}

	// verifyReq 用于本次流程后续判断的verifyReq
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(cookie)
	// verifyRec 用于本次流程后续判断的verifyRec
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	// verify 用于本次流程后续判断的verify
	var verify map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(verifyRec.Body.Bytes(), &verify); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if verify["authenticated"] == true {
		t.Fatalf("old session should be revoked after password change: %+v", verify)
	}
}

// TestChangePasswordWrongCurrent 当前密码错误。
func TestChangePasswordWrongCurrent(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"wrong","new_password":"newpw123"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// res 用于本次流程后续判断的响应
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] == true {
		t.Fatal("错误密码不应成功")
	}
}

// TestChangePasswordBadJSON 非法 JSON 400。
func TestChangePasswordBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestChangePasswordShortNewPassword 封装TestChange密码ShortNew密码业务协调。
func TestChangePasswordShortNewPassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(`{"current_password":"pw","new_password":"123"}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("新密码过短应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsBadUsername 用户名长度不足 400。
func TestUpdateCredentialsBadUsername(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"pw","new_username":"ab"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("用户名过短应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsMissingCurrentPassword 缺当前密码 400。
func TestUpdateCredentialsMissingCurrentPassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"new_username":"newname"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺当前密码应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsShortNewPassword 新密码过短 400。
func TestUpdateCredentialsShortNewPassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"pw","new_password":"123"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("新密码过短应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsNothingChanged 用户名密码均未修改 400。
func TestUpdateCredentialsNothingChanged(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"current_password":"pw","new_username":"admin"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未修改应 400，got %d", rec.Code)
	}
}
