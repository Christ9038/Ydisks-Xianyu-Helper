package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/account"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
)

func newTestServer(t *testing.T) (*Server, *db.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	store.Users.SetAdmin(context.Background(), "admin")
	// 一个账号。
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "acc1", "unb=123; _m_h5_tk=tk1_1;", admin.ID)

	mgr := account.NewManager(store, noopHandler{}, nil)
	srv := New(store, mgr, false, "", ":0", nil, nil, nil)
	mtopClient := mtop.NewClient()
	mtopClient.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"ret":["SUCCESS::调用成功"],"data":{"module":{"base":{"displayName":"测试账号","avatar":"https://img.alicdn.com/test-avatar.jpg"}}}}`,
			)),
			Request: req,
		}, nil
	})}
	srv.MTop = mtopClient
	return srv, store, func() {
		mgr.StopAll()
		_ = d.Close()
	}
}

func newUninitializedTestServer(t *testing.T) (*Server, *db.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	mgr := account.NewManager(store, noopHandler{}, nil)
	srv := New(store, mgr, false, "", ":0", nil, nil, nil)
	return srv, store, func() {
		mgr.StopAll()
		_ = d.Close()
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type noopHandler struct{}

func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error    { return nil }
func (noopHandler) HandleSystemEvent(context.Context, automation.Task) error       { return nil }
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool            { return false }
func (noopHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestLoginVerifyLogout 登录→verify→登出 全链路。
func TestLoginVerifyLogout(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	// 1) 登录（用户名密码）。
	body := `{"username":"admin","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if !lr.Success || !lr.IsAdmin {
		t.Fatalf("登录响应异常: %+v", lr)
	}
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "session" || cookie.Value == "" || !cookie.HttpOnly {
		t.Fatalf("Cookie 异常: %+v", cookie)
	}

	// 2) verify 带 cookie 应 authenticated。
	req2 := httptest.NewRequest(http.MethodGet, "/verify", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	var v map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &v)
	if v["authenticated"] != true || v["initialized"] != true {
		t.Fatalf("verify 异常: %+v", v)
	}

	// 3) 无 cookie verify 应未认证。
	req3 := httptest.NewRequest(http.MethodGet, "/verify", nil)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	json.Unmarshal(rec3.Body.Bytes(), &v)
	if v["authenticated"] == true {
		t.Fatal("无 cookie 应未认证")
	}

	// 4) 登出。
	req4 := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// 登出后 cookie 应被清除（MaxAge=-1）。
	dc := rec4.Result().Cookies()
	cleared := false
	for _, c := range dc {
		if c.Name == "session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("登出应清除 cookie")
	}
}

// TestLoginWrongPassword 错误密码。
func TestLoginWrongPassword(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	body := `{"username":"admin","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var lr loginResponse
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if lr.Success {
		t.Fatal("错误密码不应成功")
	}
}

func TestInitializeFromWebUI(t *testing.T) {
	srv, store, cleanup := newUninitializedTestServer(t)
	defer cleanup()
	h := srv.Router()

	shortReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"short"}`))
	shortRec := httptest.NewRecorder()
	h.ServeHTTP(shortRec, shortReq)
	if shortRec.Code != http.StatusBadRequest {
		t.Fatalf("短密码应返回 400，got %d body=%s", shortRec.Code, shortRec.Body.String())
	}

	initReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"newpassword"}`))
	initRec := httptest.NewRecorder()
	h.ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("初始化 status=%d body=%s", initRec.Code, initRec.Body.String())
	}
	var initResult loginResponse
	if err := json.Unmarshal(initRec.Body.Bytes(), &initResult); err != nil {
		t.Fatalf("解析初始化响应: %v", err)
	}
	if !initResult.Success || !initResult.IsAdmin || initResult.Username != "admin" {
		t.Fatalf("初始化响应异常: %+v", initResult)
	}

	admin, err := store.Users.GetAdmin(context.Background())
	if err != nil || admin == nil {
		t.Fatalf("初始化后 admin 不存在: admin=%+v err=%v", admin, err)
	}
	if _, ok, err := store.Users.VerifyAndUpgrade(context.Background(), "admin", "newpassword"); err != nil || !ok {
		t.Fatalf("初始化密码不可用: ok=%v err=%v", ok, err)
	}

	cookies := initRec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "session" || cookies[0].Value == "" {
		t.Fatalf("初始化后应自动建立会话: %+v", cookies)
	}
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(cookies[0])
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	var verifyResult map[string]any
	if err := json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult); err != nil {
		t.Fatalf("解析 verify 响应: %v", err)
	}
	if verifyResult["authenticated"] != true || verifyResult["initialized"] != true {
		t.Fatalf("初始化后 verify 异常: %+v", verifyResult)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"anotherpw"}`))
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("重复初始化应返回 409，got %d body=%s", secondRec.Code, secondRec.Body.String())
	}
}

func TestUpdateCredentialsRenamesUserAndRevokesSessions(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	sessionCookie := loginRec.Result().Cookies()[0]

	updateReq := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(
		`{"current_password":"pw","new_username":"operator","new_password":"newpassword"}`,
	))
	updateReq.AddCookie(sessionCookie)
	updateRec := httptest.NewRecorder()
	h.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResult map[string]any
	json.Unmarshal(updateRec.Body.Bytes(), &updateResult)
	if updateResult["success"] != true || updateResult["requires_relogin"] != true {
		t.Fatalf("update result=%+v", updateResult)
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(sessionCookie)
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	var verifyResult map[string]any
	json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult)
	if verifyResult["authenticated"] == true || verifyResult["initialized"] != true {
		t.Fatalf("旧会话应失效且系统仍已初始化: %+v", verifyResult)
	}

	oldLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	oldLoginRec := httptest.NewRecorder()
	h.ServeHTTP(oldLoginRec, oldLoginReq)
	var oldLogin loginResponse
	json.Unmarshal(oldLoginRec.Body.Bytes(), &oldLogin)
	if oldLogin.Success {
		t.Fatal("旧用户名不应继续登录")
	}

	newLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"operator","password":"newpassword"}`))
	newLoginRec := httptest.NewRecorder()
	h.ServeHTTP(newLoginRec, newLoginReq)
	var newLogin loginResponse
	json.Unmarshal(newLoginRec.Body.Bytes(), &newLogin)
	if !newLogin.Success {
		t.Fatalf("新凭据登录失败: %s", newLoginRec.Body.String())
	}
}

// TestCookiesDetailsRequiresAuth 未登录访问受保护端点应 401。
func TestCookiesDetailsRequiresAuth(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}
}

// TestCookiesDetailsWithAuth 登录后能取账号详情。
func TestCookiesDetailsWithAuth(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	// 登录拿 cookie。
	body := `{"username":"admin","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	// 取详情。
	req2 := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["id"] != "acc1" {
		t.Fatalf("账号详情异常: %+v", arr)
	}
	// 不应返回 cookie 明文（安全基线）。
	if _, has := arr[0]["value"]; has {
		t.Fatal("不应返回 cookie 明文")
	}
	if arr[0]["has_cookie"] != true {
		t.Fatal("应 has_cookie=true")
	}
}

func TestCookieRuntimeStatusWithAuth(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/cookies/runtime-status", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var statuses map[string]engine.RuntimeStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if statuses["acc1"].State != engine.RuntimeError {
		t.Fatalf("未启动账号应返回 error，got %+v", statuses["acc1"])
	}
}

// TestAddAndDeleteCookie 添加/删除账号。
func TestAddAndDeleteCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	body := `{"username":"admin","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	// 添加。
	addBody := `{"id":"acc2","value":"unb=456; _m_h5_tk=tk2_2;"}`
	req2 := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(addBody))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 删除。
	req3 := httptest.NewRequest(http.MethodDelete, "/cookies/acc2", nil)
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec3.Code, rec3.Body.String())
	}
}

// TestHealth 健康检查无需认证。
func TestHealth(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("health status=%d", rec.Code)
	}
}

func TestHealthReportsUnavailableDatabase(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	if err := store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["status"] != "degraded" || response["database"] != "unavailable" {
		t.Fatalf("health response=%+v", response)
	}
}
