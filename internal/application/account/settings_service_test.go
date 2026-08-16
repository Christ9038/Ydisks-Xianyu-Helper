package account

import (
	"context"
	"errors"
	"testing"
)

// fakeSettingsRepository 记录账号设置应用服务对持久化端口的调用。
type fakeSettingsRepository struct {
	// updateResult 是账号设置写入后返回的暂停截止时间。
	updateResult int64
	// updateErr 是账号设置写入错误。
	updateErr error
	// status 是账号当前是否启用。
	status bool
	// statusErr 是账号状态查询错误。
	statusErr error
	// lockCalls 是凭证锁获取次数。
	lockCalls int
	// unlockCalls 是凭证锁释放次数。
	unlockCalls int
	// statusWrites 是状态写入次数。
	statusWrites int
	// clearTokensErr 是清理旧连接凭证时返回的预置错误。
	clearTokensErr error
}

// LockCredentials 记录凭证锁的获取和释放。
func (f *fakeSettingsRepository) LockCredentials(string) func() {
	f.lockCalls++
	return func() { f.unlockCalls++ }
}

// UpdateSettings 返回预置的账号设置写入结果。
func (f *fakeSettingsRepository) UpdateSettings(context.Context, SettingsUpdateInput) (int64, error) {
	return f.updateResult, f.updateErr
}

// UpdateLoginInfo 模拟登录信息保存成功。
func (f *fakeSettingsRepository) UpdateLoginInfo(context.Context, LoginInfoUpdateInput) error {
	return nil
}

// SetStatusOwned 记录状态写入并返回预置错误。
func (f *fakeSettingsRepository) SetStatusOwned(context.Context, int64, string, bool, string) error {
	f.statusWrites++
	return nil
}

// StatusOwned 返回预置的账号启用状态。
func (f *fakeSettingsRepository) StatusOwned(context.Context, int64, string) (bool, error) {
	return f.status, f.statusErr
}

// SetPauseOwned 返回预置的暂停截止时间。
func (f *fakeSettingsRepository) SetPauseOwned(context.Context, int64, string, int) (int64, error) {
	return f.updateResult, nil
}

// GetPauseOwned 返回未暂停的默认状态。
func (f *fakeSettingsRepository) GetPauseOwned(context.Context, int64, string) (PauseState, error) {
	return PauseState{}, nil
}

// ClearTokens 模拟 Cookie 更新后清理旧连接凭证。
func (f *fakeSettingsRepository) ClearTokens(context.Context, string) error { return f.clearTokensErr }

// fakeSettingsRuntime 记录账号运行实例控制调用。
type fakeSettingsRuntime struct {
	// restartCalls 是运行实例重启次数。
	restartCalls int
	// stopCalls 是运行实例停止次数。
	stopCalls int
	// restartErr 是预置的重启错误。
	restartErr error
}

// Restart 记录重启并返回预置错误。
func (f *fakeSettingsRuntime) Restart(context.Context, string) error {
	f.restartCalls++
	return f.restartErr
}

// Stop 记录运行实例停止。
func (f *fakeSettingsRuntime) Stop(string) { f.stopCalls++ }

// TestSettingsServiceRestartsAfterCookieWrite 验证 Cookie 写入释放凭证锁后才重启运行实例。
func TestSettingsServiceRestartsAfterCookieWrite(t *testing.T) {
	// repository 保存当前测试的伪持久化端口。
	repository := &fakeSettingsRepository{updateResult: 123, status: true}
	// runtime 保存当前测试的伪运行时端口。
	runtime := &fakeSettingsRuntime{}
	// service 保存待验证的账号设置应用服务。
	// service、err 保存账号设置应用服务及其装配错误。
	service, err := NewSettingsService(repository, runtime)
	if err != nil {
		t.Fatalf("NewSettingsService: %v", err)
	}
	// cookie 保存本次模拟写入的明文 Cookie，仅存在测试输入作用域。
	cookie := "cookie"
	// result、err 保存 Cookie 设置后的运行时结果和用例错误。
	result, err := service.UpdateSettings(context.Background(), SettingsUpdateInput{UserID: 7, AccountID: "a1", Cookie: &cookie})
	if err != nil || result.PausedUntil != 123 || runtime.restartCalls != 1 {
		t.Fatalf("UpdateSettings result=%+v err=%v restart=%d", result, err, runtime.restartCalls)
	}
	if repository.lockCalls != 1 || repository.unlockCalls != 1 {
		t.Fatalf("credential lock calls=%d/%d", repository.lockCalls, repository.unlockCalls)
	}
}

// TestSettingsServiceKeepsPersistenceSuccessOnRuntimeFailure 验证运行时重启失败不会伪装成数据库写入失败。
func TestSettingsServiceKeepsPersistenceSuccessOnRuntimeFailure(t *testing.T) {
	// repository 保存启用账号的伪持久化端口。
	repository := &fakeSettingsRepository{status: true}
	// runtime 保存返回重启错误的伪运行时端口。
	runtime := &fakeSettingsRuntime{restartErr: errors.New("restart failed")}
	// service 保存待验证的账号设置应用服务。
	// service、err 保存账号设置应用服务及其装配错误。
	service, err := NewSettingsService(repository, runtime)
	if err != nil {
		t.Fatalf("NewSettingsService: %v", err)
	}
	// cookie 保存本次模拟写入的明文 Cookie。
	cookie := "cookie"
	// result、err 保存重启失败时的三态结果和用例错误。
	result, err := service.UpdateSettings(context.Background(), SettingsUpdateInput{UserID: 7, AccountID: "a1", Cookie: &cookie})
	if err != nil || result.RuntimeError == nil {
		t.Fatalf("UpdateSettings result=%+v err=%v", result, err)
	}
}

// TestSettingsServiceContinuesRuntimeAfterTokenCleanupFailure 验证旧 Token 清理失败不会阻止 Cookie 写入后的运行时重启。
func TestSettingsServiceContinuesRuntimeAfterTokenCleanupFailure(t *testing.T) {
	// repository 保存 Cookie 写入成功但 Token 清理失败的伪持久化端口。
	repository := &fakeSettingsRepository{status: true, clearTokensErr: errors.New("token cleanup failed")}
	// runtime 保存运行实例重启调用记录。
	runtime := &fakeSettingsRuntime{}
	// service、err 保存账号设置应用服务及其装配错误。
	service, err := NewSettingsService(repository, runtime)
	if err != nil {
		t.Fatalf("NewSettingsService: %v", err)
	}
	// cookie 保存本次模拟写入的 Cookie 输入。
	cookie := "cookie"
	// result、err 保存 Token 清理失败时的三态结果和用例错误。
	result, err := service.UpdateSettings(context.Background(), SettingsUpdateInput{UserID: 7, AccountID: "a1", Cookie: &cookie})
	if err != nil || result.TokenCleanupError == nil || runtime.restartCalls != 1 {
		t.Fatalf("UpdateSettings result=%+v err=%v restart=%d", result, err, runtime.restartCalls)
	}
}

// TestSettingsServiceStatusControlsRuntime 验证启停状态写入后分别控制运行实例。
func TestSettingsServiceStatusControlsRuntime(t *testing.T) {
	// repository 保存状态写入调用记录。
	repository := &fakeSettingsRepository{}
	// runtime 保存运行实例控制调用记录。
	runtime := &fakeSettingsRuntime{}
	// service 保存待验证的账号设置应用服务。
	// service、err 保存账号设置应用服务及其装配错误。
	service, err := NewSettingsService(repository, runtime)
	if err != nil {
		t.Fatalf("NewSettingsService: %v", err)
	}
	// err 保存启用账号并重启运行实例的用例错误。
	if _, err := service.SetStatus(context.Background(), 7, "a1", true); err != nil || runtime.restartCalls != 1 {
		t.Fatalf("enable err=%v restart=%d", err, runtime.restartCalls)
	}
	// err 保存停用账号并停止运行实例的用例错误。
	if _, err := service.SetStatus(context.Background(), 7, "a1", false); err != nil || runtime.stopCalls != 1 {
		t.Fatalf("disable err=%v stop=%d", err, runtime.stopCalls)
	}
	if repository.statusWrites != 2 {
		t.Fatalf("status writes=%d", repository.statusWrites)
	}
}
