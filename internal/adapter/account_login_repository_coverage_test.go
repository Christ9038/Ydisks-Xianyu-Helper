package adapter

import (
	"context"
	"errors"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// TestAccountLoginRepositoryRemainingOperations 覆盖账号登录适配器的状态、资料、扫码更新和删除路径。
func TestAccountLoginRepositoryRemainingOperations(t *testing.T) {
	// store、cleanup 提供真实 SQLite 账号存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 保存绑定当前数据库的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// status 验证已有账号默认启用状态。
	if !repository.GetStatus(ctx, "cid") {
		t.Fatal("账号默认应启用")
	}
	// err 表示账号资料更新错误。
	if err := repository.UpdateProfile(ctx, "cid", "昵称", "https://example.test/avatar"); err != nil {
		t.Fatal(err)
	}
	// summary、summaryErr 保存资料更新后的非敏感摘要。
	summary, summaryErr := repository.GetOwnedSummary(ctx, 1, "cid")
	if summaryErr != nil || summary.Nickname != "昵称" || summary.AvatarURL == "" {
		t.Fatalf("summary=%+v err=%v", summary, summaryErr)
	}
	// account、accountErr 保存已存在扫码账号的归属查询结果。
	account, accountErr := repository.FindAccount(ctx, "cid")
	if accountErr != nil || account.ID != "cid" || account.UserID != 1 {
		t.Fatalf("account=%+v err=%v", account, accountErr)
	}
	// err 表示扫码登录已有账号的扁平 Cookie 更新错误。
	if err := repository.UpdateFlatCookieOwnedForQR(ctx, "cid", "sid=qr"); err != nil {
		t.Fatal(err)
	}
	// err 表示平台续期 Cookie 和 metadata 更新错误。
	if err := repository.UpdateRenewalCookie(ctx, "cid", "sid=renewed", `{"device":"new"}`, 200); err != nil {
		t.Fatal(err)
	}
	// err 表示清理旧 Token 的兼容入口错误。
	if err := repository.ClearTokens(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	// qrRepository 保存扫码登录专用适配器外观，验证其方法委托到共享实现。
	qrRepository := NewQRLoginRepository(store)
	// unlock 保护扫码登录凭证更新的账号级临界区。
	unlock := qrRepository.LockCredentials("cid")
	unlock()
	// err 表示扫码登录专用入口创建新账号的错误。
	if err := qrRepository.CreateCookieOwned(ctx, "qr-cid", "sid=qr", 1); err != nil {
		t.Fatal(err)
	}
	// qrAccount、qrAccountErr 保存扫码专用账号查询结果。
	qrAccount, qrAccountErr := qrRepository.FindAccount(ctx, "qr-cid")
	if qrAccountErr != nil || qrAccount.ID != "qr-cid" {
		t.Fatalf("qr account=%+v err=%v", qrAccount, qrAccountErr)
	}
	// err 表示扫码专用扁平 Cookie 更新错误。
	if err := qrRepository.UpdateFlatCookieOwned(ctx, "qr-cid", "sid=qr-flat"); err != nil {
		t.Fatal(err)
	}
	// err 表示扫码专用快照 Cookie 更新错误。
	if err := qrRepository.UpdateCookieSnapshotOwned(ctx, "qr-cid", "sid=qr-snapshot", []accountapp.CookieSnapshot{{Name: "sid", Value: "qr-snapshot"}}); err != nil {
		t.Fatal(err)
	}
	// err 表示扫码专用 Token 清理错误。
	if err := qrRepository.ClearTokens(ctx, "qr-cid"); err != nil {
		t.Fatal(err)
	}
	// err 表示删除账号时的归属复核和删除错误。
	if err := repository.DeleteOwned(ctx, 1, "cid"); err != nil {
		t.Fatal(err)
	}
	// deletedErr 验证删除后摘要返回统一不存在错误。
	_, deletedErr := repository.GetOwnedSummary(ctx, 1, "cid")
	if !errors.Is(deletedErr, accountapp.ErrNotFound) {
		t.Fatalf("deleted summary error=%v", deletedErr)
	}
}

// TestAccountLoginRepositoryCoversClosedDatabaseOperations 验证账号登录适配器所有数据库端点传播基础设施故障。
func TestAccountLoginRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// operations 保存需要统一验证底层错误传播的账号操作结果。
	operations := []struct {
		name string
		err  error
	}{
		{name: "创建 Cookie", err: repository.CreateCookieOwned(ctx, "cid", "sid=value", 1)},
		{name: "更新扁平 Cookie", err: repository.UpdateFlatCookieOwned(ctx, &accountapp.CredentialDetail{ID: "cid"}, "sid=value")},
		{name: "更新续期 Cookie", err: repository.UpdateRenewalCookie(ctx, "cid", "sid=value", "{}", 1)},
		{name: "更新资料", err: repository.UpdateProfile(ctx, "cid", "昵称", "avatar")},
		{name: "查询账号", err: func() error {
			// err 表示账号归属查询在关闭数据库后的底层错误。
			_, err := repository.FindAccount(ctx, "cid")
			return err
		}()},
		{name: "扫码扁平更新", err: repository.UpdateFlatCookieOwnedForQR(ctx, "cid", "sid=value")},
		{name: "扫码快照更新", err: repository.UpdateCookieSnapshotOwned(ctx, "cid", "sid=value", nil)},
		{name: "读取摘要", err: func() error {
			// err 表示账号摘要查询在关闭数据库后的底层错误。
			_, err := repository.GetOwnedSummary(ctx, 1, "cid")
			return err
		}()},
		{name: "删除账号", err: repository.DeleteOwned(ctx, 1, "cid")},
		{name: "清理 Token", err: repository.ClearTokens(ctx, "cid")},
	}
	// operation 表示当前待验证的账号操作及其底层结果。
	for _, operation := range operations {
		if operation.err == nil {
			t.Errorf("%s 未传播数据库故障", operation.name)
		}
	}
	// status 表示数据库关闭后账号状态查询应安全降级为停用。
	if repository.GetStatus(ctx, "cid") {
		t.Fatal("数据库关闭后账号状态不应报告为启用")
	}
	// nilDetailErr 表示缺少凭证详情时的快速失败错误。
	nilDetailErr := repository.UpdateFlatCookieOwned(ctx, nil, "sid=value")
	if !errors.Is(nilDetailErr, db.ErrNotFound) {
		t.Fatalf("缺少凭证详情应返回 ErrNotFound，err=%v", nilDetailErr)
	}
	// nilUnlock 是缺少 Store 时仍可安全调用的空解锁函数。
	nilUnlock := NewAccountLoginRepository(nil).LockCredentials("cid")
	nilUnlock()
}
