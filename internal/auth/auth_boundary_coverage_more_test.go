package auth

import (
	"context"
	"strings"
	"testing"
)

// TestAuthCoversSessionCreationAndAdminFlagPersistenceFailures 验证登录会话写入失败以及管理员标记写入失败不会伪造成功结果。
func TestAuthCoversSessionCreationAndAdminFlagPersistenceFailures(t *testing.T) {
	// service、cleanup 保存登录会话写入失败测试服务及清理责任。
	service, cleanup := newAuth(t)
	defer cleanup()
	// ctx 是认证数据库操作共用的上下文。
	ctx := context.Background()
	// triggerErr 保存阻止会话插入的 SQLite 触发器创建错误。
	if _, triggerErr := service.Store.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_session_insert
		BEFORE INSERT ON sessions
		BEGIN SELECT RAISE(ABORT, 'forced session insert failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// loginID、loginUser、loginErr 保存会话写入失败后的登录结果。
	loginID, loginUser, loginErr := service.Login(ctx, "admin", "pw")
	if loginID != "" || loginUser != nil || loginErr == nil {
		t.Fatalf("会话写入失败结果 id=%q user=%v err=%v", loginID, loginUser, loginErr)
	}
	// newStore、newCleanup 保存管理员新建路径使用的空数据库。
	newStore, newCleanup := newEmptyStore(t)
	defer newCleanup()
	// flagTriggerErr 保存阻止管理员标记写入的 SQLite 触发器创建错误。
	if _, flagTriggerErr := newStore.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_admin_flag
		BEFORE UPDATE OF is_admin ON users
		BEGIN SELECT RAISE(ABORT, 'forced admin flag failure'); END`); flagTriggerErr != nil {
		t.Fatal(flagTriggerErr)
	}
	// created、createErr 保存新管理员已创建但标记管理员失败的结果。
	created, createErr := InitAdmin(ctx, newStore, "admin@example.com", "pw")
	if !created || createErr == nil {
		t.Fatalf("管理员标记失败结果 created=%v err=%v", created, createErr)
	}
	// existingService、existingCleanup 保存已有管理员重置路径的数据库。
	existingService, existingCleanup := newAuth(t)
	defer existingCleanup()
	// existingFlagTriggerErr 保存阻止已有管理员标记写入的触发器错误。
	if _, existingFlagTriggerErr := existingService.Store.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_existing_admin_flag
		BEFORE UPDATE OF is_admin ON users
		BEGIN SELECT RAISE(ABORT, 'forced existing admin flag failure'); END`); existingFlagTriggerErr != nil {
		t.Fatal(existingFlagTriggerErr)
	}
	// resetCreated、resetErr 保存已有管理员密码重置后标记失败的结果。
	resetCreated, resetErr := InitAdmin(ctx, existingService.Store, "ignored@example.com", "new-pw")
	if resetCreated || resetErr == nil {
		t.Fatalf("已有管理员标记失败结果 created=%v err=%v", resetCreated, resetErr)
	}

	// updateErrorService、updateErrorCleanup 保存已有管理员密码更新失败场景的数据库。
	updateErrorService, updateErrorCleanup := newAuth(t)
	defer updateErrorCleanup()
	// updateTriggerErr 保存阻止密码更新的触发器创建错误。
	if _, updateTriggerErr := updateErrorService.Store.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_password_update
		BEFORE UPDATE OF password_hash ON users
		BEGIN SELECT RAISE(ABORT, 'forced password update failure'); END`); updateTriggerErr != nil {
		t.Fatal(updateTriggerErr)
	}
	// updateCreated、updateErr 保存已有管理员密码更新失败结果。
	updateCreated, updateErr := InitAdmin(ctx, updateErrorService.Store, "ignored@example.com", "new-pw")
	if updateCreated || updateErr == nil {
		t.Fatalf("密码更新失败结果 created=%v err=%v", updateCreated, updateErr)
	}

	// ignoreUpdateService、ignoreUpdateCleanup 保存密码更新被 SQLite 忽略的数据库。
	ignoreUpdateService, ignoreUpdateCleanup := newAuth(t)
	defer ignoreUpdateCleanup()
	// ignoreTriggerErr 保存让密码 UPDATE 返回零影响行的触发器创建错误。
	if _, ignoreTriggerErr := ignoreUpdateService.Store.DB.ExecContext(ctx, `CREATE TRIGGER ignore_auth_password_update
		BEFORE UPDATE OF password_hash ON users
		BEGIN SELECT RAISE(IGNORE); END`); ignoreTriggerErr != nil {
		t.Fatal(ignoreTriggerErr)
	}
	// ignoreCreated、ignoreErr 保存密码未更新的保护性结果。
	ignoreCreated, ignoreErr := InitAdmin(ctx, ignoreUpdateService.Store, "ignored@example.com", "new-pw")
	if ignoreCreated || ignoreErr == nil {
		t.Fatalf("密码零影响结果 created=%v err=%v", ignoreCreated, ignoreErr)
	}

	// createErrorStore、createErrorCleanup 保存管理员创建 SQL 失败场景的数据库。
	createErrorStore, createErrorCleanup := newEmptyStore(t)
	defer createErrorCleanup()
	// createTriggerErr 保存阻止用户创建的触发器创建错误。
	if _, createTriggerErr := createErrorStore.DB.ExecContext(ctx, `CREATE TRIGGER reject_auth_user_insert
		BEFORE INSERT ON users
		BEGIN SELECT RAISE(ABORT, 'forced user insert failure'); END`); createTriggerErr != nil {
		t.Fatal(createTriggerErr)
	}
	// createFailureCreated、createFailureErr 保存管理员创建失败结果。
	createFailureCreated, createFailureErr := InitAdmin(ctx, createErrorStore, "admin@example.com", "pw")
	if createFailureCreated || createFailureErr == nil {
		t.Fatalf("管理员创建失败结果 created=%v err=%v", createFailureCreated, createFailureErr)
	}
	// hashFailureCreated、hashFailureErr 保存密码哈希阶段失败的管理员创建结果。
	hashFailureCreated, hashFailureErr := InitAdmin(ctx, createErrorStore, "hash-failure@example.com", strings.Repeat("x", 73))
	if hashFailureCreated || hashFailureErr == nil {
		t.Fatalf("超长密码哈希失败结果 created=%v err=%v", hashFailureCreated, hashFailureErr)
	}
}
