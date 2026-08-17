package adapter

import (
	"context"
	"testing"
)

// TestAdminRepositoryMapsUsersStatsAndDelete 验证管理员适配器只返回摘要并保留删除/统计语义。
func TestAdminRepositoryMapsUsersStatsAndDelete(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库操作上下文。
	ctx := context.Background()
	// created、createErr 保存普通用户创建结果。
	created, createErr := store.Users.Create(ctx, "admin-repository-user", "admin-repository@example.com", "password")
	if createErr != nil || !created {
		t.Fatalf("创建管理员适配器测试用户失败 created=%v err=%v", created, createErr)
	}
	// repository 保存绑定 SQLite Store 的管理员适配器。
	repository := NewAdminRepository(store)
	// users、usersErr 保存脱敏用户列表及错误。
	users, usersErr := repository.ListUsers(ctx)
	if usersErr != nil || len(users) < 2 {
		t.Fatalf("用户列表查询异常 len=%d err=%v", len(users), usersErr)
	}
	// stats、statsErr 保存管理员统计及错误。
	stats, statsErr := repository.Stats(ctx)
	if statsErr != nil || stats.TotalUsers < 2 {
		t.Fatalf("管理员统计异常 stats=%+v err=%v", stats, statsErr)
	}
	// targetID 保存待删除用户的数据库标识。
	target, targetErr := store.Users.GetByUsername(ctx, "admin-repository-user")
	if targetErr != nil || target == nil {
		t.Fatalf("查询待删除用户失败 target=%v err=%v", target, targetErr)
	}
	// ownedIDs、ownedErr 保存管理员删除前读取的非敏感账号标识。
	ownedIDs, ownedErr := repository.ListOwnedAccountIDs(ctx, target.ID)
	if ownedErr != nil || ownedIDs == nil {
		t.Fatalf("读取用户账号标识异常 ids=%v err=%v", ownedIDs, ownedErr)
	}
	// deleteErr 保存管理员删除操作的错误。
	if deleteErr := repository.DeleteUser(ctx, target.ID); deleteErr != nil {
		t.Fatalf("删除用户失败: %v", deleteErr)
	}
	// missingErr 保存缺失适配器依赖的错误。
	missingErr := (*AdminRepository)(nil).DeleteUser(ctx, target.ID)
	if missingErr == nil {
		t.Fatal("空管理员适配器应拒绝删除")
	}
}
