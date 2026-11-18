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
