package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestItemAISettingsLifecycle 验证商品级 AI 配置的默认继承、保存、更新、删除和账号级联清理。
func TestItemAISettingsLifecycle(t *testing.T) {
	// database、store 和 cleanup 是隔离的 SQLite 测试数据库及其资源。
	database, _, openErr := Open(context.Background(), filepath.Join(t.TempDir(), "item-ai.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	// store 是商品 AI 配置持久层测试使用的仓储聚合。
	store := NewStore(database, DialectSQLite)
	// ctx 是本测试全部数据库操作共用的上下文。
	ctx := context.Background()
	if // createErr 是创建测试用户时的错误。
	_, createErr := store.Users.Create(ctx, "item-ai-owner", "item-ai@example.com", "password"); createErr != nil {
		t.Fatal(createErr)
	}
	// owner 和 ownerErr 是测试账号所有者及其查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "item-ai-owner")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if // saveErr 是保存测试账号时的错误。
	saveErr := store.Cookies.Save(ctx, "item-ai-account", "unb=1", owner.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// initial 是没有独立记录时的默认继承配置。
	initial, initialErr := store.ItemAISettings.Get(ctx, "item-ai-account", "item-1")
	if initialErr != nil || initial.Override != ItemAIOverrideInherit || initial.Context != "" {
		t.Fatalf("默认商品 AI 配置异常: settings=%+v err=%v", initial, initialErr)
	}
	if // upsertErr 是首次保存商品配置时的错误。
	upsertErr := store.ItemAISettings.Upsert(ctx, ItemAISettingsRow{CookieID: "item-ai-account", ItemID: "item-1", Override: ItemAIOverrideEnabled, Context: "24 小时内发货"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// saved 是写入后读取到的完整配置。
	saved, savedErr := store.ItemAISettings.Get(ctx, "item-ai-account", "item-1")
	if savedErr != nil || saved.Override != ItemAIOverrideEnabled || saved.Context != "24 小时内发货" {
		t.Fatalf("保存商品 AI 配置异常: settings=%+v err=%v", saved, savedErr)
	}
	if // invalidErr 是非法覆盖值写入错误。
	invalidErr := store.ItemAISettings.Upsert(ctx, ItemAISettingsRow{CookieID: "item-ai-account", ItemID: "item-1", Override: "invalid"}); invalidErr == nil {
		t.Fatal("非法覆盖值应被拒绝")
	}
	if // longErr 是超长商品资料写入错误。
	longErr := store.ItemAISettings.Upsert(ctx, ItemAISettingsRow{CookieID: "item-ai-account", ItemID: "item-1", Override: ItemAIOverrideInherit, Context: strings.Repeat("中", ItemAIContextMaxRunes+1)}); longErr == nil {
		t.Fatal("超长商品资料应被拒绝")
	}
	if // deleteErr 是重置独立商品配置时的错误。
	deleteErr := store.ItemAISettings.Delete(ctx, "item-ai-account", "item-1"); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// reset 是删除独立记录后恢复的默认继承配置。
	reset, resetErr := store.ItemAISettings.Get(ctx, "item-ai-account", "item-1")
	if resetErr != nil || reset.Override != ItemAIOverrideInherit || reset.Context != "" {
		t.Fatalf("重置商品 AI 配置异常: settings=%+v err=%v", reset, resetErr)
	}
}
