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

func newAuth(t *testing.T) (*Service, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	// 初始化 admin。
	if ok, _ := store.Users.Create(context.Background(), "admin", "a@e.com", "pw"); !ok {
		t.Fatal("create admin")
	}
	store.Users.SetAdmin(context.Background(), "admin")
	svc := &Service{Store: store, Logger: testLogger()}
	return svc, func() { d.Close() }
}

func testLogger() *slog.Logger { return slog.Default() }

func TestLoginAndMiddleware(t *testing.T) {
	svc, cleanup := newAuth(t)
	defer cleanup()
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

	// 中间件解析会话。
	chain := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFromContext(r.Context())
		if sess == nil || !sess.IsAdmin {
			t.Errorf("中间件应解析出管理员会话，got=%v", sess)
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestRequireAuthAndAdmin(t *testing.T) {
	svc, cleanup := newAuth(t)
	defer cleanup()
	ctx := context.Background()
	sid, _, _ := svc.Login(ctx, "admin", "pw")

	protected := svc.Middleware(RequireAuth(RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))))

	// 无 cookie → 401。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}

	// 有 admin cookie → 200。
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	rec2 := httptest.NewRecorder()
	protected.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("管理员应 200，got %d", rec2.Code)
	}
}

func TestSetAndClearCookie(t *testing.T) {
	svc, cleanup := newAuth(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	svc.SetSessionCookie(rec, "sid123")
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Name != CookieName || c[0].Value != "sid123" || !c[0].HttpOnly {
		t.Fatalf("SetSessionCookie 异常: %+v", c)
	}
	if c[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite 应为 Lax，got %v", c[0].SameSite)
	}

	rec2 := httptest.NewRecorder()
	svc.ClearSessionCookie(rec2)
	cc := rec2.Result().Cookies()
	if len(cc) != 1 || cc[0].MaxAge != -1 {
		t.Fatalf("ClearSessionCookie 应 MaxAge=-1: %+v", cc)
	}
}

// newEmptyStore 构造未初始化 admin 的测试 store，供 InitAdmin 测试。
func newEmptyStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "init.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return db.NewStore(d, db.DialectSQLite), func() { d.Close() }
}

// TestInitAdmin_CreateThenReset 全新库创建 admin，二次调用走重置路径。
func TestInitAdmin_CreateThenReset(t *testing.T) {
	store, cleanup := newEmptyStore(t)
	defer cleanup()
	ctx := context.Background()

	// 全新库 → 创建。
	created, err := InitAdmin(ctx, store, "admin@example.com", "pw1")
	if err != nil || !created {
		t.Fatalf("首次 InitAdmin 应创建：created=%v err=%v", created, err)
	}
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
	if _, ok, _ := store.Users.VerifyAndUpgrade(ctx, "admin", "pw1"); ok {
		t.Fatal("旧密码 pw1 应已失效")
	}
	if _, ok, _ := store.Users.VerifyAndUpgrade(ctx, "admin", "pw2"); !ok {
		t.Fatal("新密码 pw2 应可用")
	}
}

// TestInitAdmin_DoesNotValidatePassword 密码强度校验由调用方负责（ensureAdmin），
// InitAdmin 本身不拒绝空密码，仅保证不 panic 且能完成创建。
func TestInitAdmin_DoesNotValidatePassword(t *testing.T) {
	store, cleanup := newEmptyStore(t)
	defer cleanup()
	if _, err := InitAdmin(context.Background(), store, "a@e.com", ""); err != nil {
		t.Fatalf("InitAdmin 不应自行拒绝空密码: %v", err)
	}
	if admin, _ := store.Users.GetByUsername(context.Background(), "admin"); admin == nil {
		t.Fatal("空密码也应创建 admin")
	}
}
