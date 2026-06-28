package db

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) (*Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := NewStore(db)
	return s, func() { db.Close() }
}

// TestPassword_LegacySHA256Compat 老 SHA-26 哈希能校验、且标记需升级。
func TestPassword_LegacySHA256Compat(t *testing.T) {
	legacy := legacySHA256("hunter2")
	matched, needsUpgrade, err := VerifyPassword(legacy, "hunter2")
	if err != nil || !matched || !needsUpgrade {
		t.Fatalf("老哈希校验失败: matched=%v needsUpgrade=%v err=%v", matched, needsUpgrade, err)
	}
	matched2, _, _ := VerifyPassword(legacy, "wrong")
	if matched2 {
		t.Fatal("错误密码不应通过")
	}
}

// TestPassword_Bcrypt 新 bcrypt 哈希正常工作且不标记升级。
func TestPassword_Bcrypt(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	matched, needsUpgrade, err := VerifyPassword(h, "s3cret")
	if err != nil || !matched || needsUpgrade {
		t.Fatalf("bcrypt 校验: matched=%v needsUpgrade=%v err=%v", matched, needsUpgrade, err)
	}
}

// TestUserLifecycle 创建→验证→升级→改密 全链路。
func TestUserLifecycle(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// 系统未初始化。
	if init, _ := s.Users.IsSystemInitialized(ctx); init {
		t.Fatal("新库不应已初始化")
	}

	ok, err := s.Users.Create(ctx, "admin", "admin@example.com", "pw123")
	if err != nil || !ok {
		t.Fatalf("Create: ok=%v err=%v", ok, err)
	}
	if err := s.Users.SetAdmin(ctx, "admin"); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	if init, _ := s.Users.IsSystemInitialized(ctx); !init {
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
	if _, _ = s.Users.UpdatePassword(ctx, "admin", "newpw"); true {
	}
	_, okOld, _ := s.Users.VerifyAndUpgrade(ctx, "admin", "pw123")
	if okOld {
		t.Fatal("旧密码应失效")
	}
	_, okNew, _ := s.Users.VerifyAndUpgrade(ctx, "admin", "newpw")
	if !okNew {
		t.Fatal("新密码应可用")
	}
}

// TestSessionRoundTrip 创建→读取→删除。
func TestSessionRoundTrip(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	s.Users.Create(ctx, "u1", "u1@e.com", "pw")
	user, _, _ := s.Users.VerifyAndUpgrade(ctx, "u1", "pw")

	sid, err := s.Sessions.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	got, err := s.Sessions.Get(ctx, sid)
	if err != nil || got == nil || got.UserID != user.ID {
		t.Fatalf("Get session: got=%+v err=%v", got, err)
	}
	if err := s.Sessions.Delete(ctx, sid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Sessions.Get(ctx, sid); err != ErrNotFound {
		t.Fatalf("删除后应 ErrNotFound，got err=%v", err)
	}
}

// TestCookieSaveRequiresInit 未初始化时 Save 应报错（不兜底 user_id=1）。
func TestCookieSaveRequiresInit(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := s.Cookies.Save(ctx, "cid", "cookievalue", 0)
	if err == nil {
		t.Fatal("系统未初始化时 Save cookie 应报错")
	}

	// 初始化 admin 后，用其 user_id 保存。
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	if err := s.Cookies.Save(ctx, "cid", "cookievalue", admin.ID); err != nil {
		t.Fatalf("Save: %v", err)
	}
	d, err := s.Cookies.GetDetails(ctx, "cid")
	if err != nil || d.Value != "cookievalue" || d.PauseDuration != 10 {
		t.Fatalf("GetDetails: %+v err=%v", d, err)
	}
	all, _ := s.Cookies.AllForUser(ctx, admin.ID)
	if all["cid"] != "cookievalue" {
		t.Fatalf("AllForUser: %v", all)
	}
	if pd := s.Cookies.GetPauseDuration(ctx, "cid"); pd != 10 {
		t.Fatalf("GetPauseDuration=%d want 10", pd)
	}
	if enabled, err := s.Cookies.GetAutoConfirm(ctx, "cid"); err != nil || !enabled {
		t.Fatalf("GetAutoConfirm=%v want true, err=%v", enabled, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id=?`, "cid"); err != nil {
		t.Fatalf("关闭 auto_confirm: %v", err)
	}
	if enabled, err := s.Cookies.GetAutoConfirm(ctx, "cid"); err != nil || enabled {
		t.Fatalf("GetAutoConfirm=%v want false, err=%v", enabled, err)
	}
}
