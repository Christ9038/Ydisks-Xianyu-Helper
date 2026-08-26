package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"xianyu-go/internal/db"
)

// newAuth 封装newAuth业务协调。
func newAuth(t *testing.T) (*Service, func()) {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, db.DialectSQLite)
	// 初始化 admin。
	if ok, _ := store.Users.Create(context.Background(), "admin", "a@e.com", "pw"); !ok {
		t.Fatal("create admin")
	}
	store.Users.SetAdmin(context.Background(), "admin")
	// svc 用于本次流程后续判断的svc
	svc := &Service{Store: store, Logger: testLogger()}
	return svc, func() { d.Close() }
}

// testLogger 封装testLogger业务协调。
func testLogger() *slog.Logger { return slog.Default() }

// TestLoginAndMiddleware 封装Test登录AndMiddleware业务协调。
func TestLoginAndMiddleware(t *testing.T) {
	// svc、cleanup 用于本次流程后续判断的svc、cleanup
	svc, cleanup := newAuth(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()

	// 登录。
	sid, user, err := svc.Login(ctx, "admin", "pw")
	if err != nil || user == nil || sid == "" {
		t.Fatalf("Login: err=%v user=%v sid=%q", err, user, sid)
	}
	// 错误密码。
	_, noUser, _ := svc.Login(ctx, "admin", "wrong")
	if noUser != nil {
		t.Fatal("错误密码不应登录成功")
	}
	// missingUser、missingUserErr 保存不存在用户的认证结果，覆盖 VerifyAndUpgrade 的 ok=false 保护。
	_, missingUser, missingUserErr := svc.Login(ctx, "missing-user", "pw")
	if missingUserErr != nil || missingUser != nil {
		t.Fatalf("不存在用户不应登录成功: user=%v err=%v", missingUser, missingUserErr)
	}

	// 中间件解析会话。
	chain := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sess 用于本次流程后续判断的sess
		sess := SessionFromContext(r.Context())
		if sess == nil || !sess.IsAdmin {
			t.Errorf("中间件应解析出管理员会话，got=%v", sess)
		}
		w.WriteHeader(200)
	}))

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
}

// TestIdentityFromContextReturnsMinimalUserView 验证通用 HTTP 辅助只接收用户标识而不暴露完整数据库会话。
func TestIdentityFromContextReturnsMinimalUserView(t *testing.T) {
	// ctx 保存包含完整会话的测试上下文，模拟认证中间件已经完成会话解析。
	ctx := WithSession(context.Background(), &db.Session{UserID: 42, Username: "admin", IsAdmin: true})
	// identity 保存认证包裁剪后的最小用户身份视图。
	identity := IdentityFromContext(ctx)
	if identity == nil || identity.UserID != 42 {
		t.Fatalf("身份视图异常: %+v", identity)
	}
}

// TestIdentityFromContextReturnsNilWithoutSession 验证未认证请求不会伪造用户身份。
func TestIdentityFromContextReturnsNilWithoutSession(t *testing.T) {
	// identity 保存无会话上下文经过认证包裁剪后的结果，应保持为空。
	identity := IdentityFromContext(context.Background())
	if identity != nil {
		t.Fatalf("无会话时应返回 nil，得到 %+v", identity)
	}
}

// TestRequireAuthAndAdmin 封装TestRequireAuthAndAdmin业务协调。
func TestRequireAuthAndAdmin(t *testing.T) {
	// svc、cleanup 用于本次流程后续判断的svc、cleanup
	svc, cleanup := newAuth(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// sid 用于本次流程后续判断的sid
	sid, _, _ := svc.Login(ctx, "admin", "pw")

	// protected 用于本次流程后续判断的protected
	protected := svc.Middleware(RequireAuth(RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))))

	// 无 cookie → 401。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}

	// 有 admin cookie → 200。
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	protected.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("管理员应 200，got %d", rec2.Code)
	}
}

// TestSetAndClearCookie 封装TestSetAndClear登录凭证业务协调。
func TestSetAndClearCookie(t *testing.T) {
	// svc、cleanup 用于本次流程后续判断的svc、cleanup
	svc, cleanup := newAuth(t)
	defer cleanup()

	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	svc.SetSessionCookie(rec, "sid123")
	// c 用于本次流程后续判断的c
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Name != CookieName || c[0].Value != "sid123" || !c[0].HttpOnly {
		t.Fatalf("SetSessionCookie 异常: %+v", c)
	}
	if c[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite 应为 Lax，got %v", c[0].SameSite)
	}

	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	svc.ClearSessionCookie(rec2)
	// cc 用于本次流程后续判断的cc
	cc := rec2.Result().Cookies()
	if len(cc) != 1 || cc[0].MaxAge != -1 {
		t.Fatalf("ClearSessionCookie 应 MaxAge=-1: %+v", cc)
	}
}

// TestAuthSessionAndLogoutBoundaries 验证无 Cookie、无效会话、登出和非管理员请求的安全边界。
func TestAuthSessionAndLogoutBoundaries(t *testing.T) {
	// svc、cleanup 保存认证测试服务及资源清理函数。
	svc, cleanup := newAuth(t)
	defer cleanup()
	// ctx 是认证边界测试使用的上下文。
	ctx := context.Background()
	// request 是不携带会话 Cookie 的请求。
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	// session、sessionErr 保存无 Cookie 请求的会话解析结果。
	session, sessionErr := svc.SessionFromRequest(ctx, request)
	if sessionErr != nil || session != nil {
		t.Fatalf("empty request session=%v err=%v", session, sessionErr)
	}
	// invalidRequest 是携带不存在会话标识的请求。
	invalidRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	invalidRequest.AddCookie(&http.Cookie{Name: CookieName, Value: "missing-session"})
	// missingSession、missingErr 保存不存在会话的解析结果。
	missingSession, missingErr := svc.SessionFromRequest(ctx, invalidRequest)
	if missingErr != nil || missingSession != nil {
		t.Fatalf("missing session=%v err=%v", missingSession, missingErr)
	}
	// sid、user、loginErr 保存有效登录会话及登录错误。
	sid, user, loginErr := svc.Login(ctx, "admin", "pw")
	if loginErr != nil || user == nil || sid == "" {
		t.Fatalf("login sid=%q user=%v err=%v", sid, user, loginErr)
	}
	svc.Logout(ctx, sid)
	// loggedOutRequest 是登出后再次查询会话的请求。
	loggedOutRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	loggedOutRequest.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	// loggedOut、logoutErr 保存登出后会话查询结果和错误。
	if loggedOut, logoutErr := svc.SessionFromRequest(ctx, loggedOutRequest); logoutErr != nil || loggedOut != nil {
		t.Fatalf("logged out session=%v err=%v", loggedOut, logoutErr)
	}
	// adminOnly 是需要管理员权限的受保护处理器。
	adminOnly := RequireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("non-admin should not reach handler") }))
	// recorder 是捕获非管理员响应的测试记录器。
	recorder := httptest.NewRecorder()
	adminOnly.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(WithSession(ctx, &db.Session{UserID: 7, IsAdmin: false})))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d", recorder.Code)
	}
}

// TestAuthClosedStoreAndMiddlewareErrors 验证数据库故障在登录、会话读取和中间件链路中保持可诊断且不伪造身份。
func TestAuthClosedStoreAndMiddlewareErrors(t *testing.T) {
	// svc、cleanup 保存认证测试服务及资源清理函数。
	svc, cleanup := newAuth(t)
	defer cleanup()
	// ctx 是认证故障测试使用的上下文。
	ctx := context.Background()
	// request 是携带非空会话标识的请求，确保会进入数据库读取分支。
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: CookieName, Value: "closed-session"})
	// closeErr 保存关闭数据库连接的结果。
	if closeErr := svc.Store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// session、sessionErr 保存关闭数据库后的会话读取结果。
	session, sessionErr := svc.SessionFromRequest(ctx, request)
	if session != nil || sessionErr == nil {
		t.Fatalf("关闭数据库后的会话结果异常: session=%v err=%v", session, sessionErr)
	}
	// loginID、loginUser、loginErr 保存关闭数据库后的登录结果。
	loginID, loginUser, loginErr := svc.Login(ctx, "admin", "pw")
	if loginID != "" || loginUser != nil || loginErr == nil {
		t.Fatalf("关闭数据库后的登录结果异常: id=%q user=%v err=%v", loginID, loginUser, loginErr)
	}
	// middleware 是会话读取失败后仍继续传递请求的认证中间件。
	middleware := svc.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	// response 保存中间件处理数据库错误后的响应。
	response := httptest.NewRecorder()
	middleware.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("中间件故障后不应阻断公开请求: status=%d", response.Code)
	}
}

// TestInitAdminRejectsDuplicateEmail 验证新建管理员时邮箱冲突会返回失败而不伪造创建成功。
func TestInitAdminRejectsDuplicateEmail(t *testing.T) {
	// store、cleanup 保存未初始化管理员的数据库及资源清理函数。
	store, cleanup := newEmptyStore(t)
	defer cleanup()
	// ctx 是管理员初始化测试使用的上下文。
	ctx := context.Background()
	// created、createErr 保存占用目标邮箱的普通用户创建结果。
	created, createErr := store.Users.Create(ctx, "other-user", "admin@example.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("创建占用邮箱用户失败: created=%v err=%v", created, createErr)
	}
	// adminCreated、initErr 保存管理员初始化结果。
	adminCreated, initErr := InitAdmin(ctx, store, "admin@example.com", "pw-admin")
	if adminCreated || initErr == nil {
		t.Fatalf("邮箱冲突应拒绝管理员创建: created=%v err=%v", adminCreated, initErr)
	}
}

// TestInitAdminReturnsLookupError 验证管理员初始化不会吞掉数据库查询故障。
func TestInitAdminReturnsLookupError(t *testing.T) {
	// store、cleanup 保存可关闭的管理员初始化数据库。
	store, cleanup := newEmptyStore(t)
	defer cleanup()
	// closeErr 保存关闭数据库连接的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// created、initErr 保存关闭数据库后的管理员初始化结果。
	created, initErr := InitAdmin(context.Background(), store, "admin@example.com", "pw")
	if created || initErr == nil {
		t.Fatalf("关闭数据库后不应初始化成功: created=%v err=%v", created, initErr)
	}
}

// newEmptyStore 构造未初始化 admin 的测试 store，供 InitAdmin 测试。
func newEmptyStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "init.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return db.NewStore(d, db.DialectSQLite), func() { d.Close() }
}

// TestInitAdmin_CreateThenReset 全新库创建 admin，二次调用走重置路径。
func TestInitAdmin_CreateThenReset(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newEmptyStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()

	// 全新库 → 创建。
	created, err := InitAdmin(ctx, store, "admin@example.com", "pw1")
	if err != nil || !created {
		t.Fatalf("首次 InitAdmin 应创建：created=%v err=%v", created, err)
	}
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if admin == nil || !admin.IsAdmin {
		t.Fatalf("admin 未正确创建/标记: %+v", admin)
	}
	// 密码可用。
	if _, ok, _ := store.Users.VerifyAndUpgrade(ctx, "admin", "pw1"); !ok {
		t.Fatal("pw1 应可用")
	}

	// 二次调用 → 重置密码。
	created2, err := InitAdmin(ctx, store, "ignored@e.com", "pw2")
	if err != nil || created2 {
		t.Fatalf("二次 InitAdmin 应重置：created=%v err=%v", created2, err)
	}
	if // ok 用于本次流程后续判断的ok
	_, ok, _ := store.Users.VerifyAndUpgrade(ctx, "admin", "pw1"); ok {
		t.Fatal("旧密码 pw1 应已失效")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok, _ := store.Users.VerifyAndUpgrade(ctx, "admin", "pw2"); !ok {
		t.Fatal("新密码 pw2 应可用")
	}
}

// TestInitAdmin_DoesNotValidatePassword 密码强度校验由调用方负责（ensureAdmin），
// InitAdmin 本身不拒绝空密码，仅保证不 panic 且能完成创建。
// TestInitAdmin_DoesNotValidatePassword 封装TestInitAdminDoesNotValidate密码业务协调。
func TestInitAdmin_DoesNotValidatePassword(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newEmptyStore(t)
	defer cleanup()
	if // err 用于本次流程后续判断的err
	_, err := InitAdmin(context.Background(), store, "a@e.com", ""); err != nil {
		t.Fatalf("InitAdmin 不应自行拒绝空密码: %v", err)
	}
	if // admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin"); admin == nil {
		t.Fatal("空密码也应创建 admin")
	}
}
