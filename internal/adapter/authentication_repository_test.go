package adapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// TestAuthenticationRepositoryMapsLoginAndPasswordOperations 验证认证端口在 SQLite 中映射登录、会话和改密行为。
func TestAuthenticationRepositoryMapsLoginAndPasswordOperations(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的认证适配器。
	repository := NewAuthenticationRepository(store)
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// setAdminErr 保存测试管理员标记结果，确保初始化状态与夹具语义一致。
	if setAdminErr := store.Users.SetAdmin(ctx, "admin"); setAdminErr != nil {
		t.Fatalf("标记管理员失败: %v", setAdminErr)
	}
	// initialized、initializedErr 保存系统初始化状态查询结果。
	initialized, initializedErr := repository.IsSystemInitialized(ctx)
	if initializedErr != nil || !initialized {
		t.Fatalf("初始化状态异常 initialized=%v err=%v", initialized, initializedErr)
	}
	// username、usernameErr 保存邮箱映射结果。
	username, usernameErr := repository.UsernameByEmail(ctx, "a@e.com")
	if usernameErr != nil || username != "admin" {
		t.Fatalf("邮箱映射异常 username=%q err=%v", username, usernameErr)
	}
	// user、matched、verifyErr 保存正确密码校验结果。
	user, matched, verifyErr := repository.VerifyPassword(ctx, "admin", "pw")
	if verifyErr != nil || !matched || user.ID == 0 || !user.IsAdmin {
		t.Fatalf("正确密码校验异常 user=%+v matched=%v err=%v", user, matched, verifyErr)
	}
	// sessionID、sessionErr 保存认证成功后的会话写入结果。
	sessionID, sessionErr := repository.CreateSession(ctx, user)
	if sessionErr != nil || sessionID == "" {
		t.Fatalf("会话写入异常 session=%q err=%v", sessionID, sessionErr)
	}
	// _, wrongMatched、wrongErr 保存错误密码的稳定失败语义。
	_, wrongMatched, wrongErr := repository.VerifyPassword(ctx, "admin", "wrong")
	if wrongMatched || !errors.Is(wrongErr, accountapp.ErrPasswordMismatch) {
		t.Fatalf("错误密码映射异常 matched=%v err=%v", wrongMatched, wrongErr)
	}
	// updated、updateErr 保存管理员密码更新结果。
	updated, updateErr := repository.UpdatePassword(ctx, "admin", "new-password")
	if updateErr != nil || !updated {
		t.Fatalf("密码更新异常 updated=%v err=%v", updated, updateErr)
	}
	// _, newMatched、newVerifyErr 验证新密码生效。
	_, newMatched, newVerifyErr := repository.VerifyPassword(ctx, "admin", "new-password")
	if newVerifyErr != nil || !newMatched {
		t.Fatalf("新密码校验异常 matched=%v err=%v", newMatched, newVerifyErr)
	}
}

// TestAuthenticationRepositoryMapsInitializationAndUsernameConflicts 验证管理员初始化和用户名冲突映射。
func TestAuthenticationRepositoryMapsInitializationAndUsernameConflicts(t *testing.T) {
	// database、dialect、openErr 保存全新 SQLite 数据库的打开结果。
	database, dialect, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if openErr != nil {
		t.Fatalf("打开数据库失败: %v", openErr)
	}
	defer database.Close()
	// store 是全新数据库的 repository 聚合入口。
	store := db.NewStore(database, dialect)
	// repository 是绑定全新数据库的认证适配器。
	repository := NewAuthenticationRepository(store)
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// created、createErr 保存首次管理员初始化结果。
	created, createErr := repository.InitializeAdmin(ctx, "admin@example.com", "password")
	if createErr != nil || !created {
		t.Fatalf("首次初始化异常 created=%v err=%v", created, createErr)
	}
	// createdAgain、resetErr 保存已初始化管理员的兼容重置结果。
	createdAgain, resetErr := repository.InitializeAdmin(ctx, "ignored@example.com", "new-password")
	if resetErr != nil || createdAgain {
		t.Fatalf("重复初始化异常 created=%v err=%v", createdAgain, resetErr)
	}
	// _, otherCreateErr 验证重复用户名的数据库结果不会伪装成认证成功。
	if _, otherCreateErr := store.Users.Create(ctx, "other", "other@example.com", "password"); otherCreateErr != nil {
		t.Fatalf("创建冲突测试用户失败: %v", otherCreateErr)
	}
	// admin、adminErr 保存管理员用户查询结果。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("查询管理员失败: %v", adminErr)
	}
	// conflictErr 保存将管理员改成已占用用户名的结果。
	conflictErr := repository.UpdateCredentials(ctx, admin.ID, "other", "")
	if !errors.Is(conflictErr, accountapp.ErrUsernameTaken) {
		t.Fatalf("用户名冲突映射异常: %v", conflictErr)
	}
}

// TestAuthenticationRepositoryRejectsMissingDependencies 验证认证适配器缺少数据库时所有入口均快速失败。
func TestAuthenticationRepositoryRejectsMissingDependencies(t *testing.T) {
	// repository 是未装配数据库的认证适配器。
	repository := NewAuthenticationRepository(nil)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// initializedErr 保存缺少用户仓储时的初始化状态错误。
	if _, initializedErr := repository.IsSystemInitialized(ctx); initializedErr == nil {
		t.Fatal("缺少用户仓储时不应返回初始化成功")
	}
	// sessionErr 保存缺少会话仓储时的会话创建错误。
	if _, sessionErr := repository.CreateSession(ctx, accountapp.AuthUser{ID: 1, Username: "admin"}); sessionErr == nil {
		t.Fatal("缺少会话仓储时不应返回会话成功")
	}
}
