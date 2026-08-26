package notify

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestStoreRepositoryGetSettingCoversSensitiveAndPublicPaths 验证通知仓储对普通设置和敏感设置使用不同读取边界。
func TestStoreRepositoryGetSettingCoversSensitiveAndPublicPaths(t *testing.T) {
	// store、cleanup 保存通知仓储测试数据库和关闭责任。
	store, cleanup := newNotifyStore(t)
	defer cleanup()
	// ctx 是设置读取使用的非取消上下文。
	ctx := context.Background()
	// publicErr 表示写入公开设置的数据库错误。
	if publicErr := store.Settings.Set(ctx, "notify.public", "public-value"); publicErr != nil {
		t.Fatal(publicErr)
	}
	// repository 是绑定账号上下文的通知窄仓储。
	repository := newStoreRepository("cid", store).(storeRepository)
	// publicValue、publicReadErr 保存普通系统设置读取结果。
	publicValue, publicReadErr := repository.GetSetting(ctx, "notify.public")
	if publicReadErr != nil || publicValue != "public-value" {
		t.Fatalf("普通设置读取异常 value=%q err=%v", publicValue, publicReadErr)
	}
	// secretErr 表示写入敏感设置的数据库错误。
	if secretErr := store.Settings.Set(ctx, "ai_api_key", "secret-value"); secretErr != nil {
		t.Fatal(secretErr)
	}
	// secretValue、secretReadErr 保存敏感系统设置读取结果。
	secretValue, secretReadErr := repository.GetSetting(ctx, "ai_api_key")
	if secretReadErr != nil || secretValue != "secret-value" {
		t.Fatalf("敏感设置读取异常 value=%q err=%v", secretValue, secretReadErr)
	}
	// incompleteStore 是基于同一数据库但缺少系统设置仓储的装配边界替身。
	incompleteStore := db.NewStore(store.DB, store.Dialect)
	incompleteStore.Settings = nil
	// incompleteErr 表示缺少系统设置仓储时的稳定错误。
	if _, incompleteErr := (storeRepository{store: incompleteStore}).GetSetting(ctx, "key"); incompleteErr == nil {
		t.Fatal("缺少系统设置仓储时应返回错误")
	}
}

// TestNotifierWaitContextRejectsNilContext 验证已启动通知 worker 不接受缺少生命周期上下文的等待请求。
func TestNotifierWaitContextRejectsNilContext(t *testing.T) {
	// notifier 保存已启动但尚未完成的最小通知器。
	notifier := &Notifier{done: make(chan struct{})}
	notifier.started.Store(true)
	// missingContext 模拟调用方遗漏生命周期 Context 的零值接口。
	var missingContext context.Context
	// waitErr 表示缺少 Context 时的稳定等待错误。
	waitErr := notifier.WaitContext(missingContext)
	if waitErr == nil {
		t.Fatal("缺少生命周期 Context 时应返回等待错误")
	}
	close(notifier.done)
}
