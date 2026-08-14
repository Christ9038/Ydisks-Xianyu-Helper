package db

import (
	"context"
	"testing"
)

// TestCookieScopedQueriesExcludeSecrets 验证摘要和所有权查询不会解密敏感字段。
func TestCookieScopedQueriesExcludeSecrets(t *testing.T) {
	// store 是当前测试使用的 SQLite repository 聚合器。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// ownerID 和 otherID 是两个互不相同的账号所有者。
	var ownerID, otherID int64
	// ownerCreateErr 表示创建 owner 测试用户失败的原因。
	if ownerCreateErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"scope-owner", "scope-owner@example.com", "test-hash").Scan(&ownerID); ownerCreateErr != nil {
		t.Fatalf("创建 owner: %v", ownerCreateErr)
	}
	// otherCreateErr 表示创建 other 测试用户失败的原因。
	if otherCreateErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"scope-other", "scope-other@example.com", "test-hash").Scan(&otherID); otherCreateErr != nil {
		t.Fatalf("创建 other: %v", otherCreateErr)
	}
	// insertErr 表示写入带有故意无效密文的测试账号失败原因。
	if _, insertErr := store.DB.ExecContext(ctx, `
		INSERT INTO cookies
			(id,value,user_id,auto_confirm,remark,pause_duration,username,password,show_browser,
			 nickname,avatar_url,metadata_json,last_refresh_at,login_method,last_login_at)
		VALUES ('scope-owned','not-a-ciphertext',?,1,'主账号',15,'login-user','not-a-password',1,
				'昵称','https://avatar.invalid/a', 'not-a-metadata',123,'password',456)`, ownerID); insertErr != nil {
		t.Fatalf("创建 owner cookie: %v", insertErr)
	}
	// otherInsertErr 表示写入 other 测试账号失败的原因。
	if _, otherInsertErr := store.DB.ExecContext(ctx,
		`INSERT INTO cookies (id,value,user_id) VALUES ('scope-other','other-cookie',?)`, otherID); otherInsertErr != nil {
		t.Fatalf("创建 other cookie: %v", otherInsertErr)
	}
	// summaries 是 owner 的非敏感摘要，即使 value/password/metadata 不是合法密文也应可读取。
	summaries, summaryErr := store.Cookies.ListSummaries(ctx, ownerID)
	if summaryErr != nil {
		t.Fatalf("ListSummaries: %v", summaryErr)
	}
	if len(summaries) != 1 || summaries[0].ID != "scope-owned" || summaries[0].UserID != ownerID {
		t.Fatalf("摘要范围错误: %#v", summaries)
	}
	if !summaries[0].AutoConfirm || summaries[0].PauseDuration != 15 || summaries[0].Username != "login-user" {
		t.Fatalf("摘要字段错误: %#v", summaries[0])
	}
	// cookieIDs 是 owner 的所有权 ID 列表，不应包含 other 的账号。
	cookieIDs, idsErr := store.Cookies.ListOwnedIDs(ctx, ownerID)
	if idsErr != nil || len(cookieIDs) != 1 || cookieIDs[0] != "scope-owned" {
		t.Fatalf("ListOwnedIDs: ids=%v err=%v", cookieIDs, idsErr)
	}
	// owned 表示 owner 对自己的账号拥有权限；otherOwned 应明确为 false。
	owned, ownedErr := store.Cookies.ExistsOwned(ctx, ownerID, "scope-owned")
	if ownedErr != nil || !owned {
		t.Fatalf("ExistsOwned owner: owned=%v err=%v", owned, ownedErr)
	}
	// otherOwned 和 otherErr 表示 owner 对 other 账号的所有权结果及查询错误。
	otherOwned, otherErr := store.Cookies.ExistsOwned(ctx, ownerID, "scope-other")
	if otherErr != nil || otherOwned {
		t.Fatalf("ExistsOwned cross-owner: owned=%v err=%v", otherOwned, otherErr)
	}
	// invalidListErr 表示 userID=0 被所有权列表查询拒绝的错误。
	if _, invalidListErr := store.Cookies.ListOwnedIDs(ctx, 0); invalidListErr != ErrInvalidUserID {
		t.Fatalf("userID=0 应拒绝隐式管理员查询，err=%v", invalidListErr)
	}
	// invalidExistsErr 表示 userID=0 被所有权存在性查询拒绝的错误。
	if _, invalidExistsErr := store.Cookies.ExistsOwned(ctx, 0, "scope-owned"); invalidExistsErr != ErrInvalidUserID {
		t.Fatalf("ExistsOwned userID=0 应拒绝，err=%v", invalidExistsErr)
	}
}
