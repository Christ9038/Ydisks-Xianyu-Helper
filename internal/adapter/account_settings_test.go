package adapter

import (
	"context"
	"errors"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// TestAccountSettingsRepositoryRejectsMissingStore 验证账号设置适配器缺少数据库时所有入口均快速失败。
func TestAccountSettingsRepositoryRejectsMissingStore(t *testing.T) {
	// repository 保存未装配数据库的账号设置适配器。
	repository := NewAccountSettingsRepository(nil)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// err 保存缺少数据库时的账号设置写入错误。
	if _, err := repository.UpdateSettings(ctx, accountapp.SettingsUpdateInput{AccountID: "cid"}); err == nil {
		t.Fatal("UpdateSettings 缺少数据库时不应成功")
	}
	// err 保存缺少数据库时的登录信息写入错误。
	if err := repository.UpdateLoginInfo(ctx, accountapp.LoginInfoUpdateInput{AccountID: "cid"}); err == nil {
		t.Fatal("UpdateLoginInfo 缺少数据库时不应成功")
	}
	// err 保存缺少数据库时的状态写入错误。
	if err := repository.SetStatusOwned(ctx, 1, "cid", true, ""); err == nil {
		t.Fatal("SetStatusOwned 缺少数据库时不应成功")
	}
	// err 保存缺少数据库时的状态读取错误。
	if _, err := repository.StatusOwned(ctx, 1, "cid"); err == nil {
		t.Fatal("StatusOwned 缺少数据库时不应成功")
	}
	// err 保存缺少数据库时的暂停写入错误。
	if _, err := repository.SetPauseOwned(ctx, 1, "cid", 1); err == nil {
		t.Fatal("SetPauseOwned 缺少数据库时不应成功")
	}
	// err 保存缺少数据库时的暂停读取错误。
	if _, err := repository.GetPauseOwned(ctx, 1, "cid"); err == nil {
		t.Fatal("GetPauseOwned 缺少数据库时不应成功")
	}
	// err 保存缺少数据库时的旧 Token 清理错误。
	if err := repository.ClearTokens(ctx, "cid"); err == nil {
		t.Fatal("ClearTokens 缺少数据库时不应成功")
	}
}

// TestAccountSettingsRepositoryWritesAndReadsSettings 验证账号设置、登录秘密、状态和暂停端口的真实 SQLite 行为。
func TestAccountSettingsRepositoryWritesAndReadsSettings(t *testing.T) {
	// XIANYU_DATA_KEY 让本测试验证启用密钥时登录秘密确实加密落盘。
	t.Setenv("XIANYU_DATA_KEY", "account-settings-test-key")
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的账号设置适配器。
	repository := NewAccountSettingsRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// admin、userErr 保存测试账号所属用户和查询错误。
	admin, userErr := store.Users.GetByUsername(ctx, "admin")
	if userErr != nil {
		t.Fatalf("GetByUsername: %v", userErr)
	}
	// remark、password 保存本次设置写入的非敏感备注和敏感密码输入。
	remark := "新备注"
	password := "new-password" // password 保存仅用于适配器加密写入的测试秘密。
	// enabled、showBrowser 保存账号开关设置。
	enabled, showBrowser := true, false
	// updateErr 保存账号设置事务写入错误。
	_, updateErr := repository.UpdateSettings(ctx, accountapp.SettingsUpdateInput{
		UserID: admin.ID, AccountID: "cid", Remark: &remark, AutoConfirm: &enabled,
		Username: ptrString("login-user"), Password: &password, ShowBrowser: &showBrowser,
	})
	if updateErr != nil {
		t.Fatalf("UpdateSettings: %v", updateErr)
	}
	// detail、detailErr 保存数据库返回的兼容详情，用于确认敏感字段由数据库加密保存。
	detail, detailErr := store.Cookies.GetDetails(ctx, "cid")
	if detailErr != nil || detail.Remark != remark || !detail.AutoConfirm || detail.Username != "login-user" || detail.ShowBrowser {
		t.Fatalf("settings detail=%+v err=%v", detail, detailErr)
	}
	// storedPassword 保存数据库中加密后的密码字段，仅用于确认敏感值未以明文落盘。
	var storedPassword string
	// scanErr 保存读取数据库加密字段的错误。
	if scanErr := store.DB.QueryRowContext(ctx, `SELECT password FROM cookies WHERE id=?`, "cid").Scan(&storedPassword); scanErr != nil {
		t.Fatalf("读取加密密码字段: %v", scanErr)
	}
	if storedPassword == password {
		t.Fatal("登录密码不应以明文保存")
	}
	// statusErr 保存状态写入错误。
	statusErr := repository.SetStatusOwned(ctx, admin.ID, "cid", false, "manual")
	if statusErr != nil {
		t.Fatalf("SetStatusOwned: %v", statusErr)
	}
	// status、statusErr 保存状态读取结果和错误。
	status, statusErr := repository.StatusOwned(ctx, admin.ID, "cid")
	if statusErr != nil || status {
		t.Fatalf("StatusOwned status=%v err=%v", status, statusErr)
	}
	// pausedUntil、pauseErr 保存暂停写入结果和错误。
	pausedUntil, pauseErr := repository.SetPauseOwned(ctx, admin.ID, "cid", 5)
	if pauseErr != nil || pausedUntil == 0 {
		t.Fatalf("SetPauseOwned until=%d err=%v", pausedUntil, pauseErr)
	}
	// pauseState、pauseErr 保存暂停查询结果和错误。
	pauseState, pauseErr := repository.GetPauseOwned(ctx, admin.ID, "cid")
	if pauseErr != nil || pauseState.Duration != 5 || !pauseState.Paused {
		t.Fatalf("GetPauseOwned state=%+v err=%v", pauseState, pauseErr)
	}
	// other、otherErr 保存第二个用户，确保跨用户测试命中真实账号但不属于当前用户。
	if _, otherCreateErr := store.Users.Create(ctx, "other", "other@example.com", "pw"); otherCreateErr != nil {
		t.Fatalf("Create other user: %v", otherCreateErr)
	}
	// other、otherErr 保存第二个用户及其查询错误。
	other, otherErr := store.Users.GetByUsername(ctx, "other")
	if otherErr != nil {
		t.Fatalf("GetByUsername other: %v", otherErr)
	}
	// wrongStatusErr 保存跨用户状态更新错误，确认适配器不会越权写入。
	wrongStatusErr := repository.SetStatusOwned(ctx, other.ID, "cid", true, "")
	if !errors.Is(wrongStatusErr, accountapp.ErrForbidden) && !errors.Is(wrongStatusErr, db.ErrForbidden) {
		t.Fatalf("跨用户状态更新错误=%v", wrongStatusErr)
	}
}

// TestAccountSettingsRepositoryClearTokensWithoutTokenStore 验证未装配 Token 仓储时清理动作保持兼容空操作。
func TestAccountSettingsRepositoryClearTokensWithoutTokenStore(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的账号设置适配器。
	repository := NewAccountSettingsRepository(store)
	// tokenStore 保存原始 Token 仓储并暂时清空，模拟最小装配场景。
	tokenStore := store.Tokens
	store.Tokens = nil
	defer func() { store.Tokens = tokenStore }()
	// err 保存无 Token 仓储时的兼容清理结果。
	if err := repository.ClearTokens(context.Background(), "cid"); err != nil {
		t.Fatalf("未装配 Token 仓储时不应失败: %v", err)
	}
}

// ptrString 返回指向给定字符串的输入指针，供设置端口测试构造可选字段。
func ptrString(value string) *string { return &value }
