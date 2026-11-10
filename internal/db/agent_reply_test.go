package db

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newAgentReplyTestStore 创建带一个账号的隔离 SQLite 仓储，返回仓储与账号标识。
func newAgentReplyTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	// database、openErr 是隔离的 SQLite 测试数据库及打开错误。
	database, _, openErr := Open(context.Background(), filepath.Join(t.TempDir(), "agent.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() { _ = database.Close() })
	// store 是测试使用的仓储聚合；ctx 是全部数据库操作共用的上下文。
	store := NewStore(database, DialectSQLite)
	// ctx 是本测试全部数据库操作共用的上下文。
	ctx := context.Background()
	// err 保存创建测试用户时的错误。
	if _, err := store.Users.Create(ctx, "agent-owner", "agent@example.com", "password"); err != nil {
		t.Fatal(err)
	}
	// owner 是测试账号所有者。
	owner, err := store.Users.GetByUsername(ctx, "agent-owner")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存保存测试账号时的错误。
	if err := store.Cookies.Save(ctx, "agent-account", "unb=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	return store, "agent-account"
}

// TestAgentReplyLifecycle 验证默认缺省、保存、令牌三态、摘要不含令牌、运行时解密和账号级联清理。
func TestAgentReplyLifecycle(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "agent-reply-test-key")
	// store、account 是带一个账号的隔离仓储及账号标识。
	store, account := newAgentReplyTestStore(t)
	// ctx 是本测试全部数据库操作共用的上下文。
	ctx := context.Background()
	// err 保存未配置时读取摘要的错误，期望为 ErrNotFound。
	if _, err := store.AgentReply.Get(ctx, account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未配置应返回 ErrNotFound: %v", err)
	}
	// err 保存未配置时读取运行时配置的错误，期望为 ErrNotFound。
	if _, err := store.AgentReply.RuntimeConfig(ctx, account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未配置运行时配置应返回 ErrNotFound: %v", err)
	}
	// 首次保存并替换令牌。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{Enabled: true, EndpointURL: " https://agent.example.com/v1 ",
		RequestTimeoutSeconds: 10, MaxSessionSeconds: 60, TokenAction: AgentTokenReplace, Token: " secret-token "}); err != nil {
		t.Fatal(err)
	}
	// summary 是不含令牌的摘要。
	summary, err := store.AgentReply.Get(ctx, account)
	if err != nil || !summary.Enabled || summary.EndpointURL != "https://agent.example.com/v1" || !summary.TokenConfigured ||
		summary.RequestTimeoutSeconds != 10 || summary.MaxSessionSeconds != 60 {
		t.Fatalf("摘要异常: %+v err=%v", summary, err)
	}
	// 数据库中令牌必须是密文。
	var raw string
	// err 保存读取库中令牌原文的查询错误。
	if err := store.DB.QueryRow(`SELECT auth_token FROM agent_reply_settings WHERE cookie_id=?`, account).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "secret-token" || !strings.HasPrefix(raw, encryptedValuePrefix) {
		t.Fatalf("令牌未加密: %q", raw)
	}
	// runtime 是解密后的运行时配置。
	runtime, err := store.AgentReply.RuntimeConfig(ctx, account)
	if err != nil || runtime.Token != "secret-token" || runtime.RequestTimeout != 10*time.Second || runtime.MaxSession != time.Minute {
		t.Fatalf("运行时配置异常: %+v err=%v", runtime, err)
	}
	// retain 不改令牌。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{Enabled: false, EndpointURL: "https://agent.example.com/v2",
		RequestTimeoutSeconds: 15, MaxSessionSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	if runtime, err = store.AgentReply.RuntimeConfig(ctx, account); err != nil || runtime.Token != "secret-token" || runtime.Enabled || runtime.EndpointURL != "https://agent.example.com/v2" {
		t.Fatalf("retain 应保留令牌: %+v err=%v", runtime, err)
	}
	// clear 清空令牌。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{EndpointURL: "https://agent.example.com/v2",
		RequestTimeoutSeconds: 15, MaxSessionSeconds: 120, TokenAction: AgentTokenClear}); err != nil {
		t.Fatal(err)
	}
	if summary, err = store.AgentReply.Get(ctx, account); err != nil || summary.TokenConfigured {
		t.Fatalf("clear 后不应有令牌: %+v err=%v", summary, err)
	}
	// 未知命令与超出范围的值被拒绝。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{TokenAction: "bogus", RequestTimeoutSeconds: 15, MaxSessionSeconds: 120}); err == nil {
		t.Fatal("未知令牌命令应失败")
	}
	// err 保存越界超时被 CHECK 约束拒绝的错误，期望非空。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{RequestTimeoutSeconds: 1, MaxSessionSeconds: 120}); err == nil {
		t.Fatal("越界超时应被 CHECK 拒绝")
	}
	// 删除账号后配置级联清理。
	if _, err := store.DB.Exec(`DELETE FROM cookies WHERE id=?`, account); err != nil {
		t.Fatal(err)
	}
	// err 保存账号删除后读取摘要的错误，期望为 ErrNotFound。
	if _, err := store.AgentReply.Get(ctx, account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("账号删除后配置应级联清理: %v", err)
	}
}

// TestAgentReplyRetainOnFreshRowAndLegacyMigration 验证新建行 retain 令牌为空，以及启用密钥后明文令牌被升级、错误密钥被拒绝。
func TestAgentReplyRetainOnFreshRowAndLegacyMigration(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "")
	// store、account 是未启用数据密钥的隔离仓储及账号标识。
	store, account := newAgentReplyTestStore(t)
	// ctx 是本测试全部数据库操作共用的上下文。
	ctx := context.Background()
	// err 保存首次保存且保留令牌的写入错误。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{EndpointURL: "http://127.0.0.1:1", RequestTimeoutSeconds: 15, MaxSessionSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	// summary、err 保存新建行读回的摘要及错误，期望没有令牌。
	if summary, err := store.AgentReply.Get(ctx, account); err != nil || summary.TokenConfigured {
		t.Fatalf("新建行 retain 不应有令牌: %+v err=%v", summary, err)
	}
	// 未配置密钥时令牌以明文保存（项目约定），随后启用密钥并升级。
	if err := store.AgentReply.Upsert(ctx, account, AgentReplyWrite{EndpointURL: "http://127.0.0.1:1", RequestTimeoutSeconds: 15, MaxSessionSeconds: 120,
		TokenAction: AgentTokenReplace, Token: "legacy-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XIANYU_DATA_KEY", "agent-migration-key")
	// upgraded 是启用密钥后的新仓储，升级函数通过它读取 codec。
	upgraded := NewStore(store.DB, DialectSQLite)
	// err 保存启用密钥后升级旧明文令牌的错误。
	if err := upgraded.EncryptLegacySecrets(ctx); err != nil {
		t.Fatal(err)
	}
	// raw 保存库中令牌原文，用于断言已加密。
	var raw string
	// err 保存读取令牌原文的查询错误。
	if err := store.DB.QueryRow(`SELECT auth_token FROM agent_reply_settings WHERE cookie_id=?`, account).Scan(&raw); err != nil || !strings.HasPrefix(raw, encryptedValuePrefix) {
		t.Fatalf("明文令牌应被升级: raw=%q err=%v", raw, err)
	}
	// runtime、err 保存升级后的解密结果及错误，期望得到原明文。
	if runtime, err := upgraded.AgentReply.RuntimeConfig(ctx, account); err != nil || runtime.Token != "legacy-token" {
		t.Fatalf("升级后应可解密: %+v err=%v", runtime, err)
	}
	// 错误密钥下运行时读取失败且不泄漏密文。
	t.Setenv("XIANYU_DATA_KEY", "another-key")
	// wrong 是使用错误数据密钥的新仓储，用于验证密钥校验。
	wrong := NewStore(store.DB, DialectSQLite)
	// err 保存错误密钥下读取运行时配置的错误，期望非空且不含密文。
	if _, err := wrong.AgentReply.RuntimeConfig(ctx, account); err == nil || strings.Contains(err.Error(), raw) {
		t.Fatalf("错误密钥应失败且不含密文: %v", err)
	}
	// err 保存错误密钥下启动期升级的错误，期望非空。
	if err := wrong.EncryptLegacySecrets(ctx); err == nil {
		t.Fatal("错误密钥应使启动期校验失败")
	}
}
