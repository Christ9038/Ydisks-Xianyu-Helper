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
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/admin/cookies", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var arr []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["id"] != "acc1" {
		t.Fatalf("账号列表异常: %+v", arr)
	}
}

// TestAdminDeleteUser 删除其他用户。
func TestAdminDeleteUser(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.Users.Create(ctx, "user-del", "u@e.com", "pw")
	u, _ := store.Users.GetByUsername(ctx, "user-del")
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+itoa(u.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminDeleteUserRevokesSessions(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "session-del", "session-del@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	u, err := store.Users.GetByUsername(ctx, "session-del")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	h := srv.Router()

	userLogin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"session-del","password":"pw"}`))
	userLoginRec := httptest.NewRecorder()
	h.ServeHTTP(userLoginRec, userLogin)
	if userLoginRec.Code != http.StatusOK || len(userLoginRec.Result().Cookies()) == 0 {
		t.Fatalf("user login status=%d body=%s", userLoginRec.Code, userLoginRec.Body.String())
	}
	userCookie := userLoginRec.Result().Cookies()[0]
	adminCookie := loginHelper(t, h)

	delReq := httptest.NewRequest(http.MethodDelete, "/admin/users/"+itoa(u.ID), nil)
	delReq.AddCookie(adminCookie)
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(userCookie)
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	var verify map[string]any
	if err := json.Unmarshal(verifyRec.Body.Bytes(), &verify); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if verify["authenticated"] == true {
		t.Fatalf("deleted user's old session should be invalid: %+v", verify)
	}
}

// TestAdminDeleteUserSelf 不能删除自己 400。
func TestAdminDeleteUserSelf(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+itoa(admin.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不能删自己应 400，got %d", rec.Code)
	}
}

// TestAdminDeleteUserBadID 无效 ID 400。
func TestAdminDeleteUserBadID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/abc", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestAdminStatsWithOrders 带订单的统计。
func TestAdminStatsWithOrders(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id, order_status) VALUES ('s1','i1','acc1','2')`)
	store.DB.ExecContext(ctx, `INSERT INTO cards (name, type, user_id) VALUES ('卡1','text',1)`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var stats map[string]any
	json.Unmarshal(rec.Body.Bytes(), &stats)
	if stats["total_orders"] != float64(1) || stats["total_cards"] != float64(1) {
		t.Fatalf("stats 异常: %+v", stats)
	}
}

// TestChangeAdminPassword 修改管理员密码成功。
func TestChangeAdminPassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"pw","new_password":"newpw123"}`
	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("应成功: %+v", res)
	}
}

// TestChangeAdminPasswordWrongCurrent 当前密码错误。
func TestChangeAdminPasswordWrongCurrent(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"wrong","new_password":"newpw123"}`
	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] == true {
		t.Fatal("错误密码不应成功")
	}
}

// TestChangeAdminPasswordBadJSON 非法 JSON 400。
func TestChangeAdminPasswordBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

func TestChangeAdminPasswordShortNewPassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(`{"current_password":"pw","new_password":"123"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("管理员新密码过短应 400，got %d", rec.Code)
	}
}

// TestChangeAdminPasswordNonAdmin 非 admin 403。
func TestChangeAdminPasswordNonAdmin(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Store.Users.Create(context.Background(), "user2", "u2@e.com", "pw")
	h := srv.Router()
	body := `{"username":"user2","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	req2 := httptest.NewRequest(http.MethodPost, "/change-admin-password", strings.NewReader(`{"current_password":"pw","new_password":"newpw123"}`))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("非 admin 应 403，got %d", rec2.Code)
	}
}

// TestChangePassword 修改当前用户密码。
func TestChangePassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"pw","new_password":"newpw123"}`
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("应成功: %+v", res)
	}
}

func TestChangePasswordRevokesCurrentSession(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(`{"current_password":"pw","new_password":"newpw123"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("change status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode change response: %v", err)
	}
	if res["success"] != true || res["requires_relogin"] != true {
		t.Fatalf("change response should require relogin: %+v", res)
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(cookie)
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	var verify map[string]any
	if err := json.Unmarshal(verifyRec.Body.Bytes(), &verify); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if verify["authenticated"] == true {
		t.Fatalf("old session should be revoked after password change: %+v", verify)
	}
}

// TestChangePasswordWrongCurrent 当前密码错误。
func TestChangePasswordWrongCurrent(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"wrong","new_password":"newpw123"}`
	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] == true {
		t.Fatal("错误密码不应成功")
	}
}

// TestChangePasswordBadJSON 非法 JSON 400。
func TestChangePasswordBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

func TestChangePasswordShortNewPassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(`{"current_password":"pw","new_password":"123"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("新密码过短应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsBadUsername 用户名长度不足 400。
func TestUpdateCredentialsBadUsername(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"pw","new_username":"ab"}`
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("用户名过短应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsMissingCurrentPassword 缺当前密码 400。
func TestUpdateCredentialsMissingCurrentPassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"new_username":"newname"}`
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺当前密码应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsShortNewPassword 新密码过短 400。
func TestUpdateCredentialsShortNewPassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"pw","new_password":"123"}`
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("新密码过短应 400，got %d", rec.Code)
	}
}

// TestUpdateCredentialsNothingChanged 用户名密码均未修改 400。
func TestUpdateCredentialsNothingChanged(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"current_password":"pw","new_username":"admin"}`
	req := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未修改应 400，got %d", rec.Code)
	}
}
