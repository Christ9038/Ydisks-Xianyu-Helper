package items

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// itemAISettingsRepositoryFake 是商品级 AI 应用服务测试使用的仓储替身。
type itemAISettingsRepositoryFake struct {
	owned    bool
	exists   bool
	settings ItemAISettings
	saved    ItemAISettings
	deleted  bool
	err      error
}

// AccountOwned 返回预设账号归属结果。
func (repository *itemAISettingsRepositoryFake) AccountOwned(context.Context, int64, string) (bool, error) {
	return repository.owned, repository.err
}

// ItemExists 返回预设商品存在性结果。
func (repository *itemAISettingsRepositoryFake) ItemExists(context.Context, string, string) (bool, error) {
	return repository.exists, repository.err
}

// Get 返回预设商品配置。
func (repository *itemAISettingsRepositoryFake) Get(context.Context, string, string) (ItemAISettings, error) {
	return repository.settings, repository.err
}

// Upsert 记录最后一次保存配置。
func (repository *itemAISettingsRepositoryFake) Upsert(_ context.Context, settings ItemAISettings) error {
	repository.saved = settings
	return repository.err
}

// Delete 记录默认配置删除动作。
func (repository *itemAISettingsRepositoryFake) Delete(context.Context, string, string) error {
	repository.deleted = true
	return repository.err
}

// TestItemAISettingsServiceValidation 验证归属、商品存在性、三态和资料长度边界。
func TestItemAISettingsServiceValidation(t *testing.T) {
	// repository 和 service 是默认允许访问的应用服务测试依赖。
	repository := &itemAISettingsRepositoryFake{owned: true, exists: true, settings: ItemAISettings{CookieID: "account", ItemID: "item", Override: ItemAIOverrideInherit}}
	// service 和 serviceErr 是待测应用服务及其构造错误。
	service, serviceErr := NewItemAISettingsService(repository)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	if // saveErr 是规范化保存配置时的错误。
	saveErr := service.Save(context.Background(), 1, ItemAISettings{CookieID: " account ", ItemID: " item ", Override: ItemAIOverrideEnabled, Context: "资料"}); saveErr != nil {
		t.Fatal(saveErr)
	}
	if repository.saved.CookieID != "account" || repository.saved.ItemID != "item" || repository.saved.Override != ItemAIOverrideEnabled {
		t.Fatalf("规范化保存异常: %+v", repository.saved)
	}
	if // resetErr 是默认配置重置操作的错误。
	resetErr := service.Save(context.Background(), 1, ItemAISettings{CookieID: "account", ItemID: "item", Override: ItemAIOverrideInherit}); resetErr != nil || !repository.deleted {
		t.Fatalf("默认配置重置异常: deleted=%v err=%v", repository.deleted, resetErr)
	}
	if // invalidErr 是非法覆盖值保存错误。
	invalidErr := service.Save(context.Background(), 1, ItemAISettings{CookieID: "account", ItemID: "item", Override: "invalid"}); !errors.Is(invalidErr, ErrItemAISettingsInvalidOverride) {
		t.Fatalf("非法覆盖值错误异常: %v", invalidErr)
	}
	if // longErr 是超长商品资料保存错误。
	longErr := service.Save(context.Background(), 1, ItemAISettings{CookieID: "account", ItemID: "item", Override: ItemAIOverrideInherit, Context: strings.Repeat("中", ItemAIContextMaxRunes+1)}); !errors.Is(longErr, ErrItemAISettingsContextTooLong) {
		t.Fatalf("超长资料错误异常: %v", longErr)
	}
	repository.owned = false
	if // forbiddenErr 是跨用户读取配置错误。
	_, forbiddenErr := service.Get(context.Background(), 1, "account", "item"); !errors.Is(forbiddenErr, ErrItemAISettingsForbidden) {
		t.Fatalf("跨用户访问错误异常: %v", forbiddenErr)
	}
	repository.owned, repository.exists = true, false
	if // missingErr 是商品不存在时的读取错误。
	_, missingErr := service.Get(context.Background(), 1, "account", "item"); !errors.Is(missingErr, ErrItemAISettingsItemNotFound) {
		t.Fatalf("商品缺失错误异常: %v", missingErr)
	}
}
