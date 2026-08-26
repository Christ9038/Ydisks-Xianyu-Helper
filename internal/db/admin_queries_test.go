package db

import (
	"context"
	"testing"
)

// TestAdminQueriesListsSummariesAndStats 验证管理员用户、账号摘要及仪表盘计数只返回非敏感聚合信息。
func TestAdminQueriesListsSummariesAndStats(t *testing.T) {
	// ctx 是管理员查询测试使用的数据库上下文。
	ctx := context.Background()
	// database、dialect、err 保存临时 SQLite 数据库及打开结果。
	database, dialect, err := Open(ctx, t.TempDir()+"/admin-queries.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是绑定最新迁移的数据库访问聚合。
	store := NewStore(database, dialect)
	// created、err 保存管理员测试用户创建结果及错误。
	created, err := store.Users.Create(ctx, "admin-summary", "admin-summary@example.com", "pw")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if !created {
		t.Fatal("admin user was not created")
	}
	// admin、err 保存管理员测试用户及查询错误。
	admin, err := store.Users.GetByUsername(ctx, "admin-summary")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	// err 表示把测试用户设为管理员摘要所需的数据库更新错误。
	if _, err := database.ExecContext(ctx, `UPDATE users SET is_admin=1 WHERE id=?`, admin.ID); err != nil {
		t.Fatalf("set admin flag: %v", err)
	}
	// _, err 表示创建第二个用户的错误。
	if _, err := store.Users.Create(ctx, "user-summary", "user-summary@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// err 表示创建管理员账号摘要所需 Cookie 的错误。
	if err := store.Cookies.Save(ctx, "admin-cookie", "cookie=test", admin.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	// users、err 保存管理员用户摘要列表及查询错误。
	users, err := store.Admin.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("users=%+v err=%v", users, err)
	}
	// foundAdmin 表示已找到管理员摘要。
	var foundAdmin AdminUserSummary
	// user 表示当前遍历到的管理员用户摘要。
	for _, user := range users {
		if user.Username == "admin-summary" {
			foundAdmin = user
		}
	}
	if foundAdmin.CookieCount != 1 || !foundAdmin.IsAdmin || !foundAdmin.IsActive {
		t.Fatalf("admin summary=%+v", foundAdmin)
	}
	// cookies、err 保存管理员账号摘要列表及查询错误。
	cookies, err := store.Admin.ListCookies(ctx)
	if err != nil || len(cookies) != 1 || cookies[0].ID != "admin-cookie" || cookies[0].Owner != "admin-summary" {
		t.Fatalf("cookies=%+v err=%v", cookies, err)
	}
	// stats、err 保存管理员仪表盘聚合计数及查询错误。
	stats, err := store.Admin.Stats(ctx)
	if err != nil || stats.TotalUsers != 2 || stats.TotalCookies != 1 || stats.ActiveCookies != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	// closedDatabase、closeErr 保存关闭连接后的管理员查询错误场景。
	closedDatabase, _, closeErr := Open(ctx, t.TempDir()+"/admin-queries-closed.db")
	if closeErr != nil {
		t.Fatalf("Open closed database: %v", closeErr)
	}
	// closedQueries 是底层数据库已关闭的管理员查询对象。
	closedQueries := &AdminQueries{DB: closedDatabase}
	closedDatabase.Close()
	// listUsersErr 保存关闭数据库读取用户列表时的错误。
	if _, listUsersErr := closedQueries.ListUsers(ctx); listUsersErr == nil {
		t.Fatal("closed ListUsers should fail")
	}
	// listCookiesErr 保存关闭数据库读取账号列表时的错误。
	if _, listCookiesErr := closedQueries.ListCookies(ctx); listCookiesErr == nil {
		t.Fatal("closed ListCookies should fail")
	}
	// statsErr 保存关闭数据库读取统计时的错误。
	if _, statsErr := closedQueries.Stats(ctx); statsErr == nil {
		t.Fatal("closed Stats should fail")
	}
}
