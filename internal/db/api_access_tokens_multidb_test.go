package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// multiDBTokenHash 计算与应用层一致的 SHA-256 十六进制摘要（64 字符），用于验证 CHAR(64) 列在各方言下的行为。
func multiDBTokenHash(seed string) string {
	// digest 是种子字符串的 SHA-256 摘要。
	digest := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(digest[:])
}

// TestMultiDB_APIAccessTokens 在 SQLite/MySQL/PostgreSQL 上验证访问令牌的写入、唯一摘要、认证（含用户停用与过期）、
// 归属撤销、最近使用时间节流和用户删除级联，覆盖迁移 00064 的最终 schema 与 SQL 方言差异。
func TestMultiDB_APIAccessTokens(t *testing.T) {
	// target 是当前执行回归的数据库方言目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的目标副本，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 是当前方言内全部操作共用的上下文。
			ctx := context.Background()
			// suffix 保证外部数据库重复执行时业务标识唯一。
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// users 保存两个互相隔离的测试用户。
			var users [2]*User
			// index 是用户序号。
			for index := range users {
				// name 是当前测试用户名。
				name := fmt.Sprintf("%s_tok_%d_%s", target.name, index, suffix)
				// created、err 是创建当前方言测试用户的结果；created 表示新建成功。
				if created, err := target.store.Users.Create(ctx, name, name+"@e.com", "pw"); err != nil || !created {
					t.Fatalf("create user %s: created=%v err=%v", name, created, err)
				}
				// user、err 是按用户名读回刚创建用户的结果，用于取得其主键。
				user, err := target.store.Users.GetByUsername(ctx, name)
				if err != nil {
					t.Fatal(err)
				}
				users[index] = user
			}
			// now 是基准 Unix 秒数。
			now := time.Now().Unix()
			// mk 构造一条令牌记录，摘要取自真实 64 字符 SHA-256。
			mk := func(id string, userID, created, expires int64) APIAccessToken {
				return APIAccessToken{ID: id + "_" + suffix, UserID: userID, Name: id, TokenHash: multiDBTokenHash(id + suffix), Prefix: "ydk_live_" + id, Scopes: []string{"items:read", "chat:read"}, CreatedAt: created, ExpiresAt: expires}
			}
			// older、newer 是用户 A 的两条令牌；other 是用户 B 的令牌；expired 是已过期令牌。
			older, newer := mk("older", users[0].ID, now-20, now+3600), mk("newer", users[0].ID, now-10, now+3600)
			// other、expired 分别是用户 B 的有效令牌和用户 A 已过期但未撤销的令牌。
			other, expired := mk("other", users[1].ID, now-5, now+3600), mk("expired", users[0].ID, now-100, now-1)
			// record 是当前待写入的令牌记录。
			for _, record := range []APIAccessToken{older, newer, other, expired} {
				// err 是写入当前令牌记录时的数据库错误，应为空。
				if err := target.store.APIAccessTokens.Create(ctx, record); err != nil {
					t.Fatalf("create %s: %v", record.ID, err)
				}
			}
			// 相同摘要必须被唯一约束拒绝。
			duplicate := mk("dup", users[1].ID, now, now+3600)
			duplicate.TokenHash = older.TokenHash
			// err 是写入重复摘要令牌时的错误，必须非空以证明唯一约束在当前方言生效。
			if err := target.store.APIAccessTokens.Create(ctx, duplicate); err == nil {
				t.Fatal("重复摘要必须被唯一约束拒绝")
			}
			// listed 是用户 A 的令牌列表，必须按创建时间倒序且包含 NULL 时间的 COALESCE 结果。
			listed, err := target.store.APIAccessTokens.List(ctx, users[0].ID)
			if err != nil || len(listed) != 3 || listed[0].ID != newer.ID || listed[0].LastUsedAt != 0 || listed[0].RevokedAt != 0 || len(listed[0].Scopes) != 2 {
				t.Fatalf("listed=%+v err=%v", listed, err)
			}
			// principal 是按摘要认证得到的身份，必须携带正确用户与权限。
			principal, err := target.store.APIAccessTokens.Authenticate(ctx, newer.TokenHash, now)
			if err != nil || principal.UserID != users[0].ID || principal.ID != newer.ID || len(principal.Scopes) != 2 {
				t.Fatalf("principal=%+v err=%v", principal, err)
			}
			// err 是认证已过期令牌的结果，必须是 ErrNotFound。
			if _, err := target.store.APIAccessTokens.Authenticate(ctx, expired.TokenHash, now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("过期令牌必须失效: %v", err)
			}
			// err 是认证不存在摘要的结果，必须是 ErrNotFound。
			if _, err := target.store.APIAccessTokens.Authenticate(ctx, multiDBTokenHash("unknown"), now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("未知摘要必须失效: %v", err)
			}
			// 跨用户撤销被拒；本人撤销成功；重复撤销与不存在均为 NotFound。
			if err := target.store.APIAccessTokens.Revoke(ctx, users[1].ID, older.ID, now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("跨用户撤销: %v", err)
			}
			// err 是用户 A 撤销自己令牌的结果，应为空。
			if err := target.store.APIAccessTokens.Revoke(ctx, users[0].ID, older.ID, now); err != nil {
				t.Fatalf("本人撤销: %v", err)
			}
			// err 是对已撤销令牌再次撤销的结果，必须是 ErrNotFound 且不覆盖原撤销时间。
			if err := target.store.APIAccessTokens.Revoke(ctx, users[0].ID, older.ID, now+9); !errors.Is(err, ErrNotFound) {
				t.Fatalf("重复撤销: %v", err)
			}
			// err 是认证已撤销令牌的结果，必须是 ErrNotFound。
			if _, err := target.store.APIAccessTokens.Authenticate(ctx, older.TokenHash, now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("已撤销令牌必须失效: %v", err)
			}
			// 最近使用时间节流：首次写入，30 秒内不更新，61 秒后更新；已撤销令牌不会被更新。
			for _, step := range []struct {
				at   int64
				want int64
			}{{now, now}, {now + 30, now}, {now + 61, now + 61}} {
				// err 是在当前时间点刷新最近使用时间的数据库错误，应为空。
				if err := target.store.APIAccessTokens.TouchLastUsed(ctx, newer.ID, step.at); err != nil {
					t.Fatal(err)
				}
				// touched、err 是读回节流后的令牌列表；列表首项是最新创建的令牌。
				touched, err := target.store.APIAccessTokens.List(ctx, users[0].ID)
				if err != nil || touched[0].LastUsedAt != step.want {
					t.Fatalf("at=%d last_used=%d want %d err=%v", step.at, touched[0].LastUsedAt, step.want, err)
				}
			}
			// err 是尝试刷新已撤销令牌最近使用时间的错误；语句条件会令其不更新但不报错。
			if err := target.store.APIAccessTokens.TouchLastUsed(ctx, older.ID, now+200); err != nil {
				t.Fatal(err)
			}
			// users 停用后其令牌不能认证（is_active 在各方言的真实列类型下都必须生效）。
			if _, err := target.store.DB.ExecContext(ctx, `UPDATE users SET is_active=0 WHERE id=?`, users[1].ID); err != nil {
				t.Fatalf("停用用户: %v", err)
			}
			// err 是认证已停用用户令牌的结果，必须是 ErrNotFound。
			if _, err := target.store.APIAccessTokens.Authenticate(ctx, other.TokenHash, now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("停用用户的令牌必须失效: %v", err)
			}
			// 用户 A 仍然有效，证明停用判断没有误伤他人。
			if _, err := target.store.APIAccessTokens.Authenticate(ctx, newer.TokenHash, now); err != nil {
				t.Fatalf("其他用户令牌不应受影响: %v", err)
			}
			// 删除用户后，其令牌通过外键级联清除。
			if _, err := target.store.DB.ExecContext(ctx, `DELETE FROM users WHERE id=?`, users[0].ID); err != nil {
				t.Fatalf("删除用户: %v", err)
			}
			// remaining、err 是用户删除后读回其令牌列表的结果，外键级联应使其为空。
			if remaining, err := target.store.APIAccessTokens.List(ctx, users[0].ID); err != nil || len(remaining) != 0 {
				t.Fatalf("用户删除后令牌应级联清除: remaining=%+v err=%v", remaining, err)
			}
		})
	}
}
