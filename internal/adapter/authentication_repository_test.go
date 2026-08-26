package adapter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
	// renameErr 保存有效用户名更新的成功结果。
	if renameErr := repository.UpdateCredentials(ctx, admin.ID, "renamed", ""); renameErr != nil {
		t.Fatalf("有效用户名更新失败: %v", renameErr)
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
	// initializedErr、initErr、usernameErr、verifyErr、passwordErr、credentialsErr 保存各认证入口的缺失依赖错误。
	if _, initializedErr := repository.IsSystemInitialized(ctx); initializedErr == nil {
		t.Fatal("缺少用户仓储时系统初始化查询不应成功")
	}
	// initErr 保存缺少用户仓储时管理员初始化的错误。
	if _, initErr := repository.InitializeAdmin(ctx, "admin@example.com", "pw"); initErr == nil {
		t.Fatal("缺少用户仓储时管理员初始化不应成功")
	}
	// usernameErr 保存缺少用户仓储时邮箱查询的错误。
	if _, usernameErr := repository.UsernameByEmail(ctx, "admin@example.com"); usernameErr == nil {
		t.Fatal("缺少用户仓储时邮箱查询不应成功")
	}
	// verifyErr 保存缺少用户仓储时密码校验的错误。
	if _, _, verifyErr := repository.VerifyPassword(ctx, "admin", "pw"); verifyErr == nil {
		t.Fatal("缺少用户仓储时密码校验不应成功")
	}
	// passwordErr 保存缺少用户仓储时密码更新的错误。
	if _, passwordErr := repository.UpdatePassword(ctx, "admin", "pw"); passwordErr == nil {
		t.Fatal("缺少用户仓储时密码更新不应成功")
	}
	// credentialsErr 保存缺少用户仓储时凭据更新的错误。
	if credentialsErr := repository.UpdateCredentials(ctx, 1, "admin", "pw"); credentialsErr == nil {
		t.Fatal("缺少用户仓储时凭据更新不应成功")
	}
}

// TestAuthenticationRepositoryCoversInactiveUserAndClosedDatabase 验证未激活用户和数据库故障的认证语义。
func TestAuthenticationRepositoryCoversInactiveUserAndClosedDatabase(t *testing.T) {
	// store、cleanup 保存认证边界测试数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 保存绑定测试数据库的认证适配器。
	repository := NewAuthenticationRepository(store)
	// ctx 保存数据库操作上下文。
	ctx := context.Background()
	// disableErr 保存将夹具用户设为未激活状态的 SQL 结果。
	if _, disableErr := store.DB.ExecContext(ctx, `UPDATE users SET is_active=0 WHERE username='admin'`); disableErr != nil {
		t.Fatal(disableErr)
	}
	// user、matched、verifyErr 保存未激活用户的密码校验结果。
	user, matched, verifyErr := repository.VerifyPassword(ctx, "admin", "pw")
	if matched || verifyErr != nil || user != (accountapp.AuthUser{}) {
		t.Fatalf("未激活用户认证结果异常 user=%+v matched=%v err=%v", user, matched, verifyErr)
	}
	// closedStore、closedCleanup 保存主动关闭后的数据库资源。
	closedStore, closedCleanup := newAdapterTestStore(t)
	defer closedCleanup()
	// closedRepository 保存底层连接已经关闭的认证适配器。
	closedRepository := NewAuthenticationRepository(closedStore)
	// closeErr 保存关闭数据库连接的结果。
	if closeErr := closedStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// initErr 保存数据库关闭后管理员初始化的查询错误。
	if _, initErr := closedRepository.InitializeAdmin(ctx, "admin@example.com", "pw"); initErr == nil {
		t.Fatal("数据库关闭后管理员初始化不应成功")
	}
	// usernameErr 保存数据库关闭后邮箱查询的错误。
	if _, usernameErr := closedRepository.UsernameByEmail(ctx, "admin@example.com"); usernameErr == nil {
		t.Fatal("数据库关闭后邮箱查询不应成功")
	}
	// verifyErr 保存数据库关闭后密码校验的错误。
	if _, _, verifyErr := closedRepository.VerifyPassword(ctx, "admin", "pw"); verifyErr == nil {
		t.Fatal("数据库关闭后密码校验不应成功")
	}
	// credentialsErr 保存数据库关闭后凭据更新的底层错误。
	if credentialsErr := closedRepository.UpdateCredentials(ctx, 1, "admin", "pw"); credentialsErr == nil {
		t.Fatal("数据库关闭后凭据更新不应成功")
	}
}

// TestAuthenticationRepositoryCoversAdminInitializationFailures 覆盖管理员重置、创建和管理员标记失败分支。
func TestAuthenticationRepositoryCoversAdminInitializationFailures(t *testing.T) {
	// ctx 保存管理员初始化边界测试共用的数据库上下文。
	ctx := context.Background()
	// updateStore、updateCleanup 保存密码更新 SQL 失败场景的 Store。
	updateStore, updateCleanup := newAdapterTestStore(t)
	defer updateCleanup()
	// setAdminErr 保存将夹具用户标记为管理员的结果，使初始化走已有管理员分支。
	if setAdminErr := updateStore.Users.SetAdmin(ctx, "admin"); setAdminErr != nil {
		t.Fatal(setAdminErr)
	}
	// updateTriggerErr 保存拒绝管理员密码更新的 SQLite 触发器创建结果。
	if _, updateTriggerErr := updateStore.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_repository_password_update
		BEFORE UPDATE OF password_hash ON users
		BEGIN SELECT RAISE(ABORT, 'forced password update failure'); END`); updateTriggerErr != nil {
		t.Fatal(updateTriggerErr)
	}
	// updateRepository 保存密码更新失败场景的认证适配器。
	updateRepository := NewAuthenticationRepository(updateStore)
	// updateErr 保存管理员重置密码失败结果。
	if _, updateErr := updateRepository.InitializeAdmin(ctx, "ignored@example.com", "new-password"); updateErr == nil {
		t.Fatal("密码更新触发器应返回错误")
	}
	// ignoredStore、ignoredCleanup 保存密码更新零影响场景的 Store。
	ignoredStore, ignoredCleanup := newAdapterTestStore(t)
	defer ignoredCleanup()
	// setIgnoredAdminErr 保存将零影响测试夹具用户标记为管理员的结果。
	if setIgnoredAdminErr := ignoredStore.Users.SetAdmin(ctx, "admin"); setIgnoredAdminErr != nil {
		t.Fatal(setIgnoredAdminErr)
	}
	// ignoredTriggerErr 保存忽略管理员密码更新的 SQLite 触发器创建结果。
	if _, ignoredTriggerErr := ignoredStore.DB.ExecContext(ctx, `CREATE TRIGGER ignore_auth_repository_password_update
		BEFORE UPDATE OF password_hash ON users
		BEGIN SELECT RAISE(IGNORE); END`); ignoredTriggerErr != nil {
		t.Fatal(ignoredTriggerErr)
	}
	// ignoredRepository 保存密码更新零影响场景的认证适配器。
	ignoredRepository := NewAuthenticationRepository(ignoredStore)
	// ignoredErr 保存管理员密码未更新的保护性错误。
	if _, ignoredErr := ignoredRepository.InitializeAdmin(ctx, "ignored@example.com", "new-password"); ignoredErr == nil {
		t.Fatal("密码更新零影响应返回保护性错误")
	}
	// flagStore、flagCleanup 保存管理员标记失败场景的 Store。
	flagStore, flagCleanup := newAdapterTestStore(t)
	defer flagCleanup()
	// setFlagAdminErr 保存将标记失败测试夹具用户标记为管理员的结果。
	if setFlagAdminErr := flagStore.Users.SetAdmin(ctx, "admin"); setFlagAdminErr != nil {
		t.Fatal(setFlagAdminErr)
	}
	// flagTriggerErr 保存拒绝管理员标记更新的 SQLite 触发器创建结果。
	if _, flagTriggerErr := flagStore.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_repository_admin_flag
		BEFORE UPDATE OF is_admin ON users
		BEGIN SELECT RAISE(ABORT, 'forced admin flag failure'); END`); flagTriggerErr != nil {
		t.Fatal(flagTriggerErr)
	}
	// flagRepository 保存管理员标记失败场景的认证适配器。
	flagRepository := NewAuthenticationRepository(flagStore)
	// flagErr 保存管理员密码更新成功但标记失败的结果。
	if _, flagErr := flagRepository.InitializeAdmin(ctx, "ignored@example.com", "new-password"); flagErr == nil {
		t.Fatal("管理员标记触发器应返回错误")
	}
	// createStore、createCleanup 保存全新数据库的管理员创建边界。
	createStore, createCleanup := newEmptyAuthenticationStore(t)
	defer createCleanup()
	// duplicateCreateErr 保存占用目标邮箱的普通用户创建结果。
	if _, duplicateCreateErr := createStore.Users.Create(ctx, "other", "admin@example.com", "pw"); duplicateCreateErr != nil {
		t.Fatal(duplicateCreateErr)
	}
	// createRepository 保存管理员创建零影响场景的认证适配器。
	createRepository := NewAuthenticationRepository(createStore)
	// createErr 保存管理员创建因邮箱冲突返回的错误。
	if _, createErr := createRepository.InitializeAdmin(ctx, "admin@example.com", "pw"); createErr == nil {
		t.Fatal("管理员创建零影响应返回错误")
	}
	// hashStore、hashCleanup 保存密码哈希失败场景的全新数据库。
	hashStore, hashCleanup := newEmptyAuthenticationStore(t)
	defer hashCleanup()
	// hashRepository 保存密码哈希失败场景的认证适配器。
	hashRepository := NewAuthenticationRepository(hashStore)
	// hashErr 保存超过 bcrypt 输入限制后的管理员创建错误。
	if _, hashErr := hashRepository.InitializeAdmin(ctx, "admin@example.com", strings.Repeat("x", 73)); hashErr == nil {
		t.Fatal("超长密码应返回哈希错误")
	}
}

// newEmptyAuthenticationStore 创建没有用户记录的 SQLite Store，供管理员创建分支测试使用。
func newEmptyAuthenticationStore(t *testing.T) (*db.Store, func()) {
	// database、dialect、openErr 保存新数据库的打开结果。
	database, dialect, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "empty-auth.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	// store 保存完成迁移但没有用户记录的数据库聚合入口。
	store := db.NewStore(database, dialect)
	return store, func() { _ = database.Close() }
}
