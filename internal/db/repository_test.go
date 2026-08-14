package db

import (
	"context"
	"path/filepath"
	"testing"
)

// newTestDB 负责newTestDB相关处理。
func newTestDB(t *testing.T) (*Store, func()) {
	t.Helper()
	// dbPath 保存db路径，供当前处理流程使用
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// db、err 保存db、err，供当前处理流程使用
	db, _, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// s 保存s，供当前处理流程使用
	s := NewStore(db, DialectSQLite)
	return s, func() { db.Close() }
}

// TestPassword_LegacySHA256Compat 老 SHA-26 哈希能校验、且标记需升级。
func TestPassword_LegacySHA256Compat(t *testing.T) {
	// legacy 保存legacy，供当前处理流程使用
	legacy := legacySHA256("hunter2")
	// matched、needsUpgrade、err 保存matched、needsUpgrade、err，供当前处理流程使用
	matched, needsUpgrade, err := VerifyPassword(legacy, "hunter2")
	if err != nil || !matched || !needsUpgrade {
		t.Fatalf("老哈希校验失败: matched=%v needsUpgrade=%v err=%v", matched, needsUpgrade, err)
	}
	// matched2 保存matched2，供当前处理流程使用
	matched2, _, _ := VerifyPassword(legacy, "wrong")
	if matched2 {
		t.Fatal("错误密码不应通过")
	}
}

// TestPassword_Bcrypt 新 bcrypt 哈希正常工作且不标记升级。
func TestPassword_Bcrypt(t *testing.T) {
	// h、err 保存h、err，供当前处理流程使用
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	// matched、needsUpgrade、err 保存matched、needsUpgrade、err，供当前处理流程使用
	matched, needsUpgrade, err := VerifyPassword(h, "s3cret")
	if err != nil || !matched || needsUpgrade {
		t.Fatalf("bcrypt 校验: matched=%v needsUpgrade=%v err=%v", matched, needsUpgrade, err)
	}
}

// TestUserLifecycle 创建→验证→升级→改密 全链路。
func TestUserLifecycle(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// 系统未初始化。
	if init, _ := s.Users.IsSystemInitialized(ctx); init {
		t.Fatal("新库不应已初始化")
	}

	// ok、err 保存ok、err，供当前处理流程使用
	ok, err := s.Users.Create(ctx, "admin", "admin@example.com", "pw123")
	if err != nil || !ok {
		t.Fatalf("Create: ok=%v err=%v", ok, err)
	}
	if // err 保存err，供当前处理流程使用
	err := s.Users.SetAdmin(ctx, "admin"); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	if // init 保存init，供当前处理流程使用
	init, _ := s.Users.IsSystemInitialized(ctx); !init {
		t.Fatal("创建 admin 后应已初始化")
	}

	// 重复创建应失败。
	ok2, _ := s.Users.Create(ctx, "admin", "other@example.com", "x")
	if ok2 {
		t.Fatal("重复用户名应创建失败")
	}

	// 验证密码 + bcrypt 升级路径（新用户直接 bcrypt，无需升级）。
	user, ok, err := s.Users.VerifyAndUpgrade(ctx, "admin", "pw123")
	if err != nil || !ok || user == nil {
		t.Fatalf("VerifyAndUpgrade: ok=%v err=%v", ok, err)
	}
	if !user.IsAdmin {
		t.Fatal("应为管理员")
	}

	// 改密后旧密码失效。
	_, _ = s.Users.UpdatePassword(ctx, "admin", "newpw")
	// okOld 保存okOld，供当前处理流程使用
	_, okOld, _ := s.Users.VerifyAndUpgrade(ctx, "admin", "pw123")
	if okOld {
		t.Fatal("旧密码应失效")
	}
	// okNew 保存okNew，供当前处理流程使用
	_, okNew, _ := s.Users.VerifyAndUpgrade(ctx, "admin", "newpw")
	if !okNew {
		t.Fatal("新密码应可用")
	}
}

// TestSessionRoundTrip 创建→读取→删除。
func TestSessionRoundTrip(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	// user 保存用户，供当前处理流程使用
	user, _, _ := s.Users.VerifyAndUpgrade(ctx, "u1", "pw")

	// sid、err 保存sid、err，供当前处理流程使用
	sid, err := s.Sessions.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	// got、err 保存got、err，供当前处理流程使用
	got, err := s.Sessions.Get(ctx, sid)
	if err != nil || got == nil || got.UserID != user.ID {
		t.Fatalf("Get session: got=%+v err=%v", got, err)
	}
	if // err 保存err，供当前处理流程使用
	err := s.Sessions.Delete(ctx, sid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := s.Sessions.Get(ctx, sid); err != ErrNotFound {
		t.Fatalf("删除后应 ErrNotFound，got err=%v", err)
	}
}

// TestCookieSaveRequiresInit 未初始化时 Save 应报错（不兜底 user_id=1）。
func TestCookieSaveRequiresInit(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// err 保存err，供当前处理流程使用
	err := s.Cookies.Save(ctx, "cid", "cookievalue", 0)
	if err == nil {
		t.Fatal("系统未初始化时 Save cookie 应报错")
	}

	// 初始化 admin 后，用其 user_id 保存。
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := s.Cookies.Save(ctx, "cid", "cookievalue", admin.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// d、err 保存d、err，供当前处理流程使用
	d, err := s.Cookies.GetDetails(ctx, "cid")
	if err != nil || d.Value != "cookievalue" || d.PauseDuration != 10 {
		t.Fatalf("GetDetails: %+v err=%v", d, err)
	}
	// all 保存all，供当前处理流程使用
	all, _ := s.Cookies.AllForUser(ctx, admin.ID)
	if all["cid"] != "cookievalue" {
		t.Fatalf("AllForUser: %v", all)
	}
	if // pd 保存pd，供当前处理流程使用
	pd := s.Cookies.GetPauseDuration(ctx, "cid"); pd != 10 {
		t.Fatalf("GetPauseDuration=%d want 10", pd)
	}
	if // enabled、err 保存enabled、err，供当前处理流程使用
	enabled, err := s.Cookies.GetAutoConfirm(ctx, "cid"); err != nil || !enabled {
		t.Fatalf("GetAutoConfirm=%v want true, err=%v", enabled, err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id=?`, "cid"); err != nil {
		t.Fatalf("关闭 auto_confirm: %v", err)
	}
	if // enabled、err 保存enabled、err，供当前处理流程使用
	enabled, err := s.Cookies.GetAutoConfirm(ctx, "cid"); err != nil || enabled {
		t.Fatalf("GetAutoConfirm=%v want false, err=%v", enabled, err)
	}
}

// TestCookieLoginAudit 负责Test登录凭证登录Audit相关处理。
func TestCookieLoginAudit(t *testing.T) {
	// s、cleanup 保存s、cleanup，供当前处理流程使用
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	if // err 保存err，供当前处理流程使用
	_, err := s.Users.Create(ctx, "admin", "a@e.com", "pw"); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	// admin 保存admin，供当前处理流程使用
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := s.Cookies.Save(ctx, "cid", "cookievalue", admin.ID); err != nil {
		t.Fatalf("Save cookie: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := s.Cookies.UpdateLoginInfo(ctx, "cid", "login-user", "secret", true); err != nil {
		t.Fatalf("UpdateLoginInfo: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := s.Cookies.MarkLogin(ctx, "cid", "password", 12345); err != nil {
		t.Fatalf("MarkLogin: %v", err)
	}
	// d、err 保存d、err，供当前处理流程使用
	d, err := s.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.Username != "login-user" || d.Password != "secret" || !d.ShowBrowser {
		t.Fatalf("登录资料未保存: %+v", d)
	}
	if d.LoginMethod != "password" || d.LastLoginAt != 12345 {
		t.Fatalf("登录审计字段异常: method=%q at=%d", d.LoginMethod, d.LastLoginAt)
	}

	if // err 保存err，供当前处理流程使用
	err := s.LoginLogs.Add(ctx, AccountLoginLog{CookieID: "cid", UserID: admin.ID, Method: "password", Status: "failed", Message: "wrong", CreatedAt: 10}); err != nil {
		t.Fatalf("Add log failed: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := s.LoginLogs.Add(ctx, AccountLoginLog{CookieID: "cid", UserID: admin.ID, Method: "password", Status: "success", Message: "ok", CreatedAt: 20}); err != nil {
		t.Fatalf("Add log success: %v", err)
	}
	// logs、err 保存logs、err，供当前处理流程使用
	logs, err := s.LoginLogs.ListByCookie(ctx, "cid", 10)
	if err != nil {
		t.Fatalf("ListByCookie: %v", err)
	}
	if len(logs) != 2 || logs[0].Status != "success" || logs[1].Status != "failed" {
		t.Fatalf("登录日志排序/内容异常: %#v", logs)
	}
}
