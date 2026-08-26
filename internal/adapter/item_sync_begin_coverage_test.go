package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// TestItemSyncBeginCoversValidationAndCredentialStates 覆盖商品同步开始阶段的依赖、归属和凭证校验分支。
func TestItemSyncBeginCoversValidationAndCredentialStates(t *testing.T) {
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// nilRepository 表示未装配商品同步依赖的适配器。
	nilRepository := NewItemSyncRepository(nil, nil, nil, nil, nil)
	// _, _, _, _, _, nilErr 保存缺少存储依赖时的阶段错误。
	_, _, _, _, _, nilErr := nilRepository.begin(ctx, itemapp.SyncQuery{UserID: 1, CookieID: "cid"})
	if nilErr == nil {
		t.Fatal("缺少同步存储时应返回错误")
	}
	// missingStore、missingCleanup 保存不存在账号场景使用的测试存储。
	missingStore, missingCleanup := newAdapterTestStore(t)
	defer missingCleanup()
	// missingRepository 保存不存在账号场景的同步适配器。
	missingRepository := NewItemSyncRepository(missingStore, nil, nil, nil, nil)
	// _, _, _, _, _, missingErr 保存不存在账号的归属错误。
	_, _, _, _, _, missingErr := missingRepository.begin(ctx, itemapp.SyncQuery{UserID: 1, CookieID: "missing"})
	if !errors.Is(missingErr, itemapp.ErrSyncNotOwned) {
		t.Fatalf("不存在账号错误=%v", missingErr)
	}
	// wrongOwnerStore、wrongOwnerCleanup 保存跨用户归属场景使用的测试存储。
	wrongOwnerStore, wrongOwnerCleanup := newAdapterTestStore(t)
	defer wrongOwnerCleanup()
	// wrongOwnerRepository 保存跨用户归属场景的同步适配器。
	wrongOwnerRepository := NewItemSyncRepository(wrongOwnerStore, nil, nil, nil, nil)
	// _, _, _, _, _, wrongOwnerErr 保存跨用户账号的归属错误。
	_, _, _, _, _, wrongOwnerErr := wrongOwnerRepository.begin(ctx, itemapp.SyncQuery{UserID: 2, CookieID: "cid"})
	if !errors.Is(wrongOwnerErr, itemapp.ErrSyncNotOwned) {
		t.Fatalf("跨用户账号错误=%v", wrongOwnerErr)
	}
	// emptyStore、emptyCleanup 保存无可用凭证场景使用的测试存储。
	emptyStore, emptyCleanup := newAdapterTestStore(t)
	defer emptyCleanup()
	// emptyErr 保存清空账号 Cookie 的数据库写回结果。
	if emptyErr := emptyStore.Cookies.UpdateRenewalCookie(ctx, "cid", "", `{}`, 1); emptyErr != nil {
		t.Fatal(emptyErr)
	}
	// emptyRepository 保存无可用凭证场景的同步适配器。
	emptyRepository := NewItemSyncRepository(emptyStore, nil, nil, nil, nil)
	// _, _, _, _, _, emptyCredentialErr 保存无可用凭证的阶段错误。
	_, _, _, _, _, emptyCredentialErr := emptyRepository.begin(ctx, itemapp.SyncQuery{UserID: 1, CookieID: "cid"})
	if emptyCredentialErr == nil {
		t.Fatal("无可用凭证时应返回阶段错误")
	}
	// closedStore、closedCleanup 保存数据库关闭场景使用的测试存储。
	closedStore, closedCleanup := newAdapterTestStore(t)
	defer closedCleanup()
	// closeErr 保存关闭数据库连接的结果。
	if closeErr := closedStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// closedRepository 保存数据库关闭场景的同步适配器。
	closedRepository := NewItemSyncRepository(closedStore, nil, nil, nil, nil)
	// _, _, _, _, _, closedErr 保存数据库关闭后的持久化阶段错误。
	_, _, _, _, _, closedErr := closedRepository.begin(ctx, itemapp.SyncQuery{UserID: 1, CookieID: "cid"})
	if closedErr == nil {
		t.Fatal("数据库关闭时应返回阶段错误")
	}
	// successStore、successCleanup 保存完整凭证成功场景使用的测试存储。
	successStore, successCleanup := newAdapterTestStore(t)
	defer successCleanup()
	// successRepository 保存完整凭证成功场景的同步适配器。
	successRepository := NewItemSyncRepository(successStore, nil, nil, nil, nil)
	// detail、value、requestContext、session、unlock、successErr 保存成功初始化结果。
	detail, value, requestContext, session, unlock, successErr := successRepository.begin(ctx, itemapp.SyncQuery{UserID: 1, CookieID: "cid"})
	if successErr != nil || detail == nil || value == "" || requestContext == nil || session == nil || unlock == nil {
		t.Fatalf("成功初始化 detail=%+v value=%q ctx=%v session=%v unlockNil=%v err=%v", detail, value, requestContext, session, unlock == nil, successErr)
	}
	unlock()
}
