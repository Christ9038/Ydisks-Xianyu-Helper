package account

import (
	"context"
	"errors"
	"testing"
)

// TestQRLoginPersistCoversTargetAndLookupFailures 验证重新授权目标不存在、查询失败及空服务保护分支。
func TestQRLoginPersistCoversTargetAndLookupFailures(t *testing.T) {
	// ctx 是扫码持久化调用使用的基础上下文。
	ctx := context.Background()
	// nilService 表示未初始化的扫码登录服务。
	var nilService *QRLoginService
	// persistErr 保存空服务执行扫码持久化时的初始化错误。
	if _, persistErr := nilService.PersistSuccess(ctx, QRLoginInput{UserID: 7, ScannedAccountID: "account", Cookies: "cookie"}); persistErr == nil {
		t.Fatal("空扫码服务应返回初始化错误")
	}
	// targetRepository 模拟重新授权目标账号不存在。
	targetRepository := &fakeQRLoginRepository{findErr: ErrNotFound}
	// targetService 保存重新授权场景的扫码服务。
	targetService, targetServiceErr := NewQRLoginService(targetRepository, &fakeQRLoginLifecycle{})
	if targetServiceErr != nil {
		t.Fatalf("构造目标服务失败: %v", targetServiceErr)
	}
	// targetErr 保存重新授权目标不存在时的业务错误。
	if _, targetErr := targetService.PersistSuccess(ctx, QRLoginInput{UserID: 7, ScannedAccountID: "account", TargetAccountID: "account", Cookies: "cookie"}); targetErr == nil {
		t.Fatal("重新授权目标不存在应返回错误")
	}
	// lookupErr 是账号查询需要返回的稳定错误。
	lookupErr := errors.New("account lookup failed")
	// lookupService 保存账号查询失败场景的扫码服务。
	lookupService, lookupServiceErr := NewQRLoginService(&fakeQRLoginRepository{findErr: lookupErr}, &fakeQRLoginLifecycle{})
	if lookupServiceErr != nil {
		t.Fatalf("构造查询错误服务失败: %v", lookupServiceErr)
	}
	// actualErr 保存扫码服务向调用方透传的账号查询错误。
	if _, actualErr := lookupService.PersistSuccess(ctx, QRLoginInput{UserID: 7, ScannedAccountID: "account", Cookies: "cookie"}); !errors.Is(actualErr, lookupErr) {
		t.Fatalf("账号查询错误未透传: %v", actualErr)
	}
}

// TestQRLoginPersistCoversSnapshotAndFlatUpdateFailures 验证已有账号的快照、扁平 Cookie 以及新账号快照写入错误。
func TestQRLoginPersistCoversSnapshotAndFlatUpdateFailures(t *testing.T) {
	// ctx 是扫码持久化调用使用的基础上下文。
	ctx := context.Background()
	// snapshotErr 是已有账号完整 Cookie 快照写入错误。
	snapshotErr := errors.New("snapshot update failed")
	// snapshotRepository 保存已有账号快照更新错误的凭证替身。
	snapshotRepository := &fakeQRLoginRepository{account: QRLoginAccount{ID: "account", UserID: 7}, snapshotErr: snapshotErr}
	// snapshotService 保存已有账号快照更新服务。
	snapshotService, snapshotServiceErr := NewQRLoginService(snapshotRepository, &fakeQRLoginLifecycle{})
	if snapshotServiceErr != nil {
		t.Fatalf("构造快照服务失败: %v", snapshotServiceErr)
	}
	// actualSnapshotErr 保存已有账号快照错误透传结果。
	if _, actualSnapshotErr := snapshotService.PersistSuccess(ctx, QRLoginInput{UserID: 7, ScannedAccountID: "account", Cookies: "cookie", Snapshot: []CookieSnapshot{{Name: "sid", Value: "value"}}}); !errors.Is(actualSnapshotErr, snapshotErr) {
		t.Fatalf("已有账号快照错误未透传: %v", actualSnapshotErr)
	}
	// flatErr 是已有账号扁平 Cookie 更新错误。
	flatErr := errors.New("flat cookie update failed")
	// flatRepository 保存扁平 Cookie 更新错误的凭证替身。
	flatRepository := &fakeQRLoginRepository{account: QRLoginAccount{ID: "account", UserID: 7}, flatErr: flatErr}
	// flatService 保存已有账号扁平更新服务。
	flatService, flatServiceErr := NewQRLoginService(flatRepository, &fakeQRLoginLifecycle{})
	if flatServiceErr != nil {
		t.Fatalf("构造扁平服务失败: %v", flatServiceErr)
	}
	// actualFlatErr 保存已有账号扁平 Cookie 错误透传结果。
	if _, actualFlatErr := flatService.PersistSuccess(ctx, QRLoginInput{UserID: 7, ScannedAccountID: "account", Cookies: "cookie"}); !errors.Is(actualFlatErr, flatErr) {
		t.Fatalf("已有账号扁平错误未透传: %v", actualFlatErr)
	}
	// newSnapshotErr 是新账号首次写入完整快照错误。
	newSnapshotErr := errors.New("new snapshot failed")
	// newRepository 保存新账号快照写入错误的凭证替身。
	newRepository := &fakeQRLoginRepository{findErr: ErrNotFound, snapshotErr: newSnapshotErr}
	// newService 保存新账号快照错误服务。
	newService, newServiceErr := NewQRLoginService(newRepository, &fakeQRLoginLifecycle{})
	if newServiceErr != nil {
		t.Fatalf("构造新账号快照服务失败: %v", newServiceErr)
	}
	// actualNewErr 保存新账号快照错误透传结果。
	if _, actualNewErr := newService.PersistSuccess(ctx, QRLoginInput{UserID: 7, ScannedAccountID: "account", Cookies: "cookie", Snapshot: []CookieSnapshot{{Name: "sid", Value: "value"}}}); !errors.Is(actualNewErr, newSnapshotErr) {
		t.Fatalf("新账号快照错误未透传: %v", actualNewErr)
	}
}
