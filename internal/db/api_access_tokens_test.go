package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestAPIAccessTokenLifecycle 验证令牌摘要保存、有效身份读取、最近使用时间和撤销失效。
func TestAPIAccessTokenLifecycle(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 仓储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试共用的数据库操作上下文。
	ctx := context.Background()
	// created 表示测试用户是否成功创建。
	created, err := store.Users.Create(ctx, "api-token-owner", "api-token@example.com", "password")
	if err != nil || !created {
		t.Fatalf("create user: created=%v err=%v", created, err)
	}
	// owner 是当前访问令牌的本地所有者。
	owner, err := store.Users.GetByUsername(ctx, "api-token-owner")
	if err != nil {
		t.Fatal(err)
	}
	// now 是本次令牌签发和认证使用的 Unix 秒数。
	now := time.Now().Unix()
	// record 是只含摘要和非敏感元数据的令牌记录。
	record := APIAccessToken{
		ID: "key-1", UserID: owner.ID, Name: "外部客服", TokenHash: "aabbcc", Prefix: "ydk_live_12345678",
		Scopes: []string{"chat:read", "chat:send"}, CreatedAt: now, ExpiresAt: now + 3600,
	}
	// err 是保存测试令牌摘要与权限记录时的数据库错误。
	if err := store.APIAccessTokens.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	// principal 是摘要匹配后返回的非秘密认证身份。
	principal, err := store.APIAccessTokens.Authenticate(ctx, record.TokenHash, now)
	if err != nil || principal.ID != record.ID || principal.UserID != owner.ID || principal.Username != owner.Username || len(principal.Scopes) != 2 {
		t.Fatalf("authenticate principal=%+v err=%v", principal, err)
	}
	// err 表示最近使用时间写入错误。
	// err 是更新令牌最近使用时间时的数据库错误。
	if err := store.APIAccessTokens.TouchLastUsed(ctx, record.ID, now+120); err != nil {
		t.Fatal(err)
	}
	// records 是管理界面可见的令牌元数据；列表查询不读取摘要字段。
	records, err := store.APIAccessTokens.List(ctx, owner.ID)
	if err != nil || len(records) != 1 || records[0].TokenHash != "" || records[0].LastUsedAt != now+120 {
		t.Fatalf("list records=%+v err=%v", records, err)
	}
	// err 表示撤销令牌时的数据库错误。
	// err 是首次撤销用户令牌时的数据库错误。
	if err := store.APIAccessTokens.Revoke(ctx, owner.ID, record.ID, now+180); err != nil {
		t.Fatal(err)
	}
	// err 是已撤销令牌被认证仓储拒绝时返回的结果。
	if _, err := store.APIAccessTokens.Authenticate(ctx, record.TokenHash, now+181); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token should be invalid, err=%v", err)
	}
	// err 是重复撤销已撤销令牌时返回的未找到错误。
	if err := store.APIAccessTokens.Revoke(ctx, owner.ID, record.ID, now+182); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated revoke should report not found, err=%v", err)
	}
}

// TestAPIAccessTokenRejectsExpiredAndInactiveUsers 验证过期令牌和已停用用户不能通过认证。
func TestAPIAccessTokenRejectsExpiredAndInactiveUsers(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 仓储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试共用的数据库操作上下文。
	ctx := context.Background()
	// created 表示测试用户是否成功创建。
	created, err := store.Users.Create(ctx, "api-token-disabled", "api-token-disabled@example.com", "password")
	if err != nil || !created {
		t.Fatalf("create user: created=%v err=%v", created, err)
	}
	// owner 是过期和停用场景共用的本地用户。
	owner, err := store.Users.GetByUsername(ctx, "api-token-disabled")
	if err != nil {
		t.Fatal(err)
	}
	// now 是本次认证测试的 Unix 秒数。
	now := time.Now().Unix()
	// expired 是已过期但未撤销的令牌记录。
	expired := APIAccessToken{ID: "expired", UserID: owner.ID, Name: "expired", TokenHash: "expired-hash", Prefix: "ydk_live_expired", Scopes: []string{"items:read"}, CreatedAt: now - 100, ExpiresAt: now - 1}
	// err 是保存已过期令牌测试记录时的数据库错误。
	if err := store.APIAccessTokens.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	// err 是已过期令牌被认证仓储拒绝时返回的结果。
	if _, err := store.APIAccessTokens.Authenticate(ctx, expired.TokenHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token should be invalid, err=%v", err)
	}
	// active 是用户停用前创建的有效期令牌。
	active := APIAccessToken{ID: "inactive-user", UserID: owner.ID, Name: "inactive", TokenHash: "inactive-hash", Prefix: "ydk_live_inactive", Scopes: []string{"items:read"}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是保存停用用户令牌测试记录时的数据库错误。
	if err := store.APIAccessTokens.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	// err 是停用测试用户时的数据库错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE users SET is_active=0 WHERE id=?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	// err 是已停用用户的令牌被认证仓储拒绝时返回的结果。
	if _, err := store.APIAccessTokens.Authenticate(ctx, active.TokenHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive user token should be invalid, err=%v", err)
	}
}

// TestAPIAccessTokenListRevokeAndTouchBoundaries 验证列表排序与归属隔离、撤销的归属与幂等、最近使用时间节流和重复摘要约束。
func TestAPIAccessTokenListRevokeAndTouchBoundaries(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 仓储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试共用的数据库操作上下文。
	ctx := context.Background()
	// owners 保存两个互相隔离的用户。
	var owners [2]*User
	// index 是用户序号，用于生成不同的用户名。
	for index := range owners {
		// name 是当前测试用户名。
		name := []string{"token-user-a", "token-user-b"}[index]
		// created、err 是创建当前测试用户的结果；created 表示新建成功。
		if created, err := store.Users.Create(ctx, name, name+"@example.com", "password"); err != nil || !created {
			t.Fatalf("create %s: created=%v err=%v", name, created, err)
		}
		// user、err 是按用户名读回刚创建用户的结果。
		user, err := store.Users.GetByUsername(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		owners[index] = user
	}
	// now 是本测试的基准 Unix 秒数。
	now := time.Now().Unix()
	// mk 构造一条指定用户、创建时间的令牌记录。
	mk := func(id string, userID int64, created int64) APIAccessToken {
		return APIAccessToken{ID: id, UserID: userID, Name: id, TokenHash: "hash-" + id, Prefix: "ydk_live_" + id, Scopes: []string{"items:read", "chat:read"}, CreatedAt: created, ExpiresAt: now + 3600}
	}
	// record 是待写入的令牌记录：同一用户两条不同创建时间的记录，加上另一用户的一条。
	for _, record := range []APIAccessToken{mk("old", owners[0].ID, now-20), mk("new", owners[0].ID, now-10), mk("other", owners[1].ID, now-5)} {
		// err 是写入当前令牌记录时的数据库错误，应为空。
		if err := store.APIAccessTokens.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	// 相同摘要必须被唯一约束拒绝，避免两条记录对应同一明文令牌。
	if err := store.APIAccessTokens.Create(ctx, mk("old", owners[1].ID, now)); err == nil {
		t.Fatal("重复 ID 或摘要必须被拒绝")
	}
	// listed 是用户 A 的令牌，必须按创建时间倒序且不含他人记录。
	listed, err := store.APIAccessTokens.List(ctx, owners[0].ID)
	if err != nil || len(listed) != 2 || listed[0].ID != "new" || listed[1].ID != "old" || len(listed[0].Scopes) != 2 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	// empty 是没有任何令牌的用户，应返回空切片而非错误。
	if empty, err := store.APIAccessTokens.List(ctx, 999999); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	// 用户 B 不能撤销用户 A 的令牌。
	if err := store.APIAccessTokens.Revoke(ctx, owners[1].ID, "old", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户撤销应返回 ErrNotFound: %v", err)
	}
	// err 是用户 A 撤销自己令牌的结果，应成功。
	if err := store.APIAccessTokens.Revoke(ctx, owners[0].ID, "old", now); err != nil {
		t.Fatal(err)
	}
	// 重复撤销不应再次成功，也不能覆盖原撤销时间。
	if err := store.APIAccessTokens.Revoke(ctx, owners[0].ID, "old", now+50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复撤销应返回 ErrNotFound: %v", err)
	}
	// err 是撤销不存在令牌的结果，必须是 ErrNotFound。
	if err := store.APIAccessTokens.Revoke(ctx, owners[0].ID, "missing", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的令牌应返回 ErrNotFound: %v", err)
	}
	// 已撤销令牌不再可认证，也不会被刷新最近使用时间。
	if _, err := store.APIAccessTokens.Authenticate(ctx, "hash-old", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("已撤销令牌应失效: %v", err)
	}
	// 最近使用时间每分钟最多写入一次：首次写入成功，30 秒内不更新，61 秒后更新。
	if err := store.APIAccessTokens.TouchLastUsed(ctx, "new", now); err != nil {
		t.Fatal(err)
	}
	// err 是 30 秒内再次刷新最近使用时间的结果；节流生效时不报错也不更新。
	if err := store.APIAccessTokens.TouchLastUsed(ctx, "new", now+30); err != nil {
		t.Fatal(err)
	}
	// touched 读取节流后的最近使用时间。
	touched, _ := store.APIAccessTokens.List(ctx, owners[0].ID)
	if touched[0].LastUsedAt != now {
		t.Fatalf("30 秒内不应更新最近使用时间: %d", touched[0].LastUsedAt)
	}
	// err 是超过一分钟后刷新最近使用时间的结果，应成功并更新。
	if err := store.APIAccessTokens.TouchLastUsed(ctx, "new", now+61); err != nil {
		t.Fatal(err)
	}
	touched, _ = store.APIAccessTokens.List(ctx, owners[0].ID)
	if touched[0].LastUsedAt != now+61 {
		t.Fatalf("超过一分钟应更新: %d", touched[0].LastUsedAt)
	}
	if touched[1].RevokedAt != now {
		t.Fatalf("撤销时间应保持首次值: %d", touched[1].RevokedAt)
	}
}

// TestAPIAccessTokenCorruptScopesAndClosedDatabase 验证权限 JSON 损坏和数据库不可用时返回错误而不是放行。
func TestAPIAccessTokenCorruptScopesAndClosedDatabase(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 仓储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试共用的数据库操作上下文。
	ctx := context.Background()
	// created、err 是创建当前测试用户的结果；created 表示新建成功。
	if created, err := store.Users.Create(ctx, "corrupt-owner", "corrupt@example.com", "password"); err != nil || !created {
		t.Fatalf("create user: %v %v", created, err)
	}
	// owner 是读回的测试用户，忽略查询错误因为上一步已确认创建成功。
	owner, _ := store.Users.GetByUsername(ctx, "corrupt-owner")
	// now 是基准 Unix 秒数。
	now := time.Now().Unix()
	// err 是写入待损坏令牌记录时的数据库错误，应为空。
	if err := store.APIAccessTokens.Create(ctx, APIAccessToken{ID: "bad", UserID: owner.ID, Name: "bad", TokenHash: "bad-hash", Prefix: "ydk_live_bad", Scopes: []string{"items:read"}, CreatedAt: now, ExpiresAt: now + 3600}); err != nil {
		t.Fatal(err)
	}
	// 直接写入非法 JSON，模拟数据损坏。
	if _, err := store.DB.ExecContext(ctx, `UPDATE api_access_tokens SET scopes_json='not-json' WHERE id='bad'`); err != nil {
		t.Fatal(err)
	}
	// err 是权限 JSON 损坏后列表读取的结果，必须报错。
	if _, err := store.APIAccessTokens.List(ctx, owner.ID); err == nil {
		t.Error("List 遇到损坏的权限 JSON 必须报错")
	}
	// err 是权限 JSON 损坏后按摘要认证的结果，必须是非 NotFound 的错误，防止损坏数据被当作放行。
	if _, err := store.APIAccessTokens.Authenticate(ctx, "bad-hash", now); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Authenticate 遇到损坏权限必须返回非 NotFound 错误: %v", err)
	}
	// 关闭数据库后，所有操作必须返回错误。
	store.DB.Close()
	// err 是数据库关闭后创建令牌的结果，必须报错。
	if err := store.APIAccessTokens.Create(ctx, APIAccessToken{ID: "x", UserID: owner.ID, Scopes: []string{"items:read"}}); err == nil {
		t.Error("Create 在数据库关闭后必须报错")
	}
	// err 是数据库关闭后列表读取的结果，必须报错。
	if _, err := store.APIAccessTokens.List(ctx, owner.ID); err == nil {
		t.Error("List 在数据库关闭后必须报错")
	}
	// err 是数据库关闭后撤销令牌的结果，必须是数据库错误而非 NotFound。
	if err := store.APIAccessTokens.Revoke(ctx, owner.ID, "bad", now); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Revoke 在数据库关闭后必须返回数据库错误: %v", err)
	}
	// err 是数据库关闭后认证令牌的结果，必须是数据库错误而非 NotFound。
	if _, err := store.APIAccessTokens.Authenticate(ctx, "bad-hash", now); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Authenticate 在数据库关闭后必须返回数据库错误: %v", err)
	}
	// err 是数据库关闭后刷新最近使用时间的结果，必须报错。
	if err := store.APIAccessTokens.TouchLastUsed(ctx, "bad", now); err == nil {
		t.Error("TouchLastUsed 在数据库关闭后必须报错")
	}
}
