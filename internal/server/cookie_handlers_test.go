package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestListCookies 列表 cookie_id。
func TestListCookies(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookies", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ids []string
	json.Unmarshal(rec.Body.Bytes(), &ids)
	if len(ids) != 1 || ids[0] != "acc1" {
		t.Fatalf("cookies 列表异常: %+v", ids)
	}
}

// TestRefreshCookieProfile 主动刷新账号资料。
func TestRefreshCookieProfile(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/cookies/acc1/refresh-profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("刷新资料应成功: %+v", res)
	}
	if res["nickname"] != "测试账号" {
		t.Fatalf("昵称异常: %v", res["nickname"])
	}
}

// TestRefreshCookieProfileBadCookie 无权账号 403。
func TestRefreshCookieProfileBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/cookies/other/refresh-profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestGetCookieDetails 单账号详情。
func TestGetCookieDetails(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookie/acc1/details", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var d map[string]any
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d["id"] != "acc1" || d["has_cookie"] != true {
		t.Fatalf("详情异常: %+v", d)
	}
}

func TestListCookieDetailsIncludesShowBrowser(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Cookies.UpdateLoginInfo(ctx, "acc1", "login-user", "secret", true); err != nil {
		t.Fatalf("UpdateLoginInfo: %v", err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var details []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if len(details) != 1 || details[0]["show_browser"] != true {
		t.Fatalf("账号列表应返回 show_browser=true: %+v", details)
	}
	if _, ok := details[0]["login_password"]; ok {
		t.Fatalf("账号列表不应返回登录密码: %+v", details[0])
	}
}

func TestUpdateCookieSettingsAtomically(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	channelID, err := store.Notifications.CreateChannel(context.Background(), &db.NotificationChannelRow{
		Name: "owned", Type: "webhook", Config: `{}`, Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"remark":"atomic","auto_confirm":false,"pause_duration":3,"username":"login-user","show_browser":true,"channel_ids":[` + jsonInt(channelID) + `]}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	detail, _ := store.Cookies.GetDetails(context.Background(), "acc1")
	bindings, _ := store.Notifications.AccountBindings(context.Background(), "acc1")
	if detail.Remark != "atomic" || detail.AutoConfirm || detail.PauseDuration != 3 || detail.Username != "login-user" || !detail.ShowBrowser {
		t.Fatalf("detail=%+v", detail)
	}
	if len(bindings) != 1 || bindings[0] != channelID {
		t.Fatalf("bindings=%v", bindings)
	}
}

func TestUpdateCookieSettingsClearsTokenButKeepsDeviceID(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Tokens.Save(ctx, "acc1", "permanent-device", "old-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(`{"cookie":"unb=123; _m_h5_tk=new_1;"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	token, err := store.Tokens.Get(ctx, "acc1")
	if err != nil || token.DeviceID != "permanent-device" || token.AccessToken != "" || token.ExpireAt != 0 {
		t.Fatalf("token=%+v err=%v", token, err)
	}
}

func jsonInt(value int64) string { return strconv.FormatInt(value, 10) }

// TestGetCookieDetailsBadCookie 无权账号 403。
func TestGetCookieDetailsBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookie/other/details", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestUpdateCookie 更新 cookie 值。
func TestUpdateCookie(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	h := srv.Router()
	cookie := loginHelper(t, h)
	if err := store.Tokens.Save(ctx, "acc1", "permanent-device", "old-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}

	body := `{"value":"unb=123; _m_h5_tk=newtoken_2;"}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "" || d.LastLoginAt != 0 {
		t.Fatalf("普通 Cookie 更新不应刷新登录审计字段: method=%q last=%d", d.LoginMethod, d.LastLoginAt)
	}
	token, err := store.Tokens.Get(ctx, "acc1")
	if err != nil || token.DeviceID != "permanent-device" || token.AccessToken != "" || token.ExpireAt != 0 {
		t.Fatalf("token=%+v err=%v", token, err)
	}
}

func TestUpdateCookieQRLoginEnablesAccount(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Cookies.SetStatusWithReason(ctx, "acc1", false, "token 失效"); err != nil {
		t.Fatalf("SetStatusWithReason: %v", err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"value":"unb=123; _m_h5_tk=qr_2;","login_method":"qr_scan"}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("扫码登录成功后应重新启用账号")
	}
	var reason string
	if err := store.DB.QueryRowContext(ctx, `SELECT disable_reason FROM cookie_status WHERE cookie_id='acc1'`).Scan(&reason); err != nil {
		t.Fatalf("query disable_reason: %v", err)
	}
	if reason != "" {
		t.Fatalf("扫码登录成功后应清空禁用原因，got %q", reason)
	}
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "qr_scan" || d.LastLoginAt == 0 {
		t.Fatalf("扫码登录应刷新登录审计字段: %+v", d)
	}
}

func TestSetCookieStatusRecordsManualDisableReason(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(`{"enabled":false}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var enabled int
	var reason string
	if err := store.DB.QueryRow(`SELECT enabled,disable_reason FROM cookie_status WHERE cookie_id='acc1'`).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || reason != db.DisableReasonManual {
		t.Fatalf("enabled=%d reason=%q", enabled, reason)
	}
}

// TestUpdateCookieBadJSON 非法 JSON 400。
func TestUpdateCookieBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestUpdateCookieLoginInfo 更新登录信息。
func TestUpdateCookieLoginInfo(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"username":"u1","password":"p1","show_browser":true}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.Username != "u1" || d.Password != "p1" || !d.ShowBrowser {
		t.Fatalf("登录信息未正确保存: %+v", d)
	}

	body = `{"username":"u2","login_password":"","show_browser":false}`
	req = httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err = store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails after empty password: %v", err)
	}
	if d.Username != "u2" || d.Password != "p1" || d.ShowBrowser {
		t.Fatalf("空密码更新应保留原密码并更新其他字段: %+v", d)
	}

	body = `{"username":"u2","clear_password":true,"show_browser":false}`
	req = httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear password status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err = store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails after clear password: %v", err)
	}
	if d.Username != "u2" || d.Password != "" || d.ShowBrowser {
		t.Fatalf("clear_password 应清空密码并保留其他更新: %+v", d)
	}
}

// TestUpdateCookieLoginInfoBadJSON 非法 JSON 400。
func TestUpdateCookieLoginInfoBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieStatus 启停账号。
func TestSetCookieStatus(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 先设置账号为停用，便于测试启用路径。
	store.Cookies.SetStatus(ctx, "acc1", false)
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 启用。
	body := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("enable status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("应已启用")
	}

	// 停用。
	body2 := `{"enabled":false}`
	req2 := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(body2))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("disable status=%d", rec2.Code)
	}
	if store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("应已停用")
	}
}

// TestSetCookieStatusBadJSON 非法 JSON 400。
func TestSetCookieStatusBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieAutoConfirmBadJSON 非法 JSON 400。
func TestSetCookieAutoConfirmBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/auto-confirm", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieRemarkBadJSON 非法 JSON 400。
func TestSetCookieRemarkBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/remark", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestDeleteCookie 删除账号。
func TestDeleteCookie(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.Cookies.Save(ctx, "acc-del", "unb=1; _m_h5_tk=t_1;", 1)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/cookies/acc-del", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d", rec.Code)
	}
}

// TestAddCookieBad 缺 id 或 value 400。
func TestAddCookieBad(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"id":"acc2"}`
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 value 应 400，got %d", rec.Code)
	}
}

func TestAddCookieDefaultsManualLoginAudit(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	ctx := context.Background()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"id":"acc-manual","value":"unb=456; _m_h5_tk=manual_1;"}`
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := store.Cookies.GetDetails(ctx, "acc-manual")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "manual" || d.LastLoginAt == 0 {
		t.Fatalf("手动新增 Cookie 应记录 manual 登录审计字段: %+v", d)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "acc-manual", 10)
	if err != nil || len(logs) != 1 || logs[0].Method != "manual" || logs[0].TriggerReason != "手动Cookie录入" {
		t.Fatalf("手动新增 Cookie 应记录登录日志: logs=%#v err=%v", logs, err)
	}
}

// TestAddCookieBadJSON 非法 JSON 400。
func TestAddCookieBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetCookieAutoConfirmNotFound 不存在账号 404。
func TestGetCookieAutoConfirmNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookies/no-such/auto-confirm", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d", rec.Code)
	}
}

// TestCachedAccountNickname 备注优先于昵称。
func TestCachedAccountNickname(t *testing.T) {
	cases := []struct {
		remark, nickname, id, want string
	}{
		{"我的备注", "昵称", "acc1", "我的备注"},
		{"", "昵称", "acc1", "昵称"},
		{"", "", "acc1234567890", "账号 acc123"},
	}
	for _, c := range cases {
		got := cachedAccountNickname(&db.CookieDetail{ID: c.id, Nickname: c.nickname, Remark: c.remark})
		if got != c.want {
			t.Errorf("cachedAccountNickname(remark=%q,nick=%q,id=%q)=%q want %q", c.remark, c.nickname, c.id, got, c.want)
		}
	}
}

// TestNormalizeProfileAvatarURL 头像 URL 归一。
func TestNormalizeProfileAvatarURL(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"//img.alicdn.com/x.jpg":           "https://img.alicdn.com/x.jpg",
		"http://img.alicdn.com/x.jpg":      "https://img.alicdn.com/x.jpg",
		"https://img.alicdn.com/x.jpg":     "https://img.alicdn.com/x.jpg",
		"  https://img.alicdn.com/x.jpg  ": "https://img.alicdn.com/x.jpg",
	}
	for in, want := range cases {
		if got := normalizeProfileAvatarURL(in); got != want {
			t.Errorf("normalizeProfileAvatarURL(%q)=%q want %q", in, got, want)
		}
	}
}

// TestTruncate truncate 不超长则原样返回。
func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
