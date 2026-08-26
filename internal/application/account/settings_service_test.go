package account

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
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
	// statusWriteErr 是账号启停状态持久化错误。
	statusWriteErr error
	// statusWriteErrs 保存按调用顺序返回的账号启停状态持久化错误。
	statusWriteErrs []error
	// lockCalls 是凭证锁获取次数。
	lockCalls int
	// unlockCalls 是凭证锁释放次数。
	unlockCalls int
	// statusWrites 是状态写入次数。
	statusWrites int
	// clearTokensErr 是清理旧连接凭证时返回的预置错误。
	clearTokensErr error
	// loginErr、pauseErr 是登录信息和暂停写入时的预置错误。
	loginErr, pauseErr error
	// pauseState 是暂停查询返回的非敏感状态。
	pauseState PauseState
	// getPauseErr 是暂停查询需要返回的预置错误。
	getPauseErr error
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
	return f.loginErr
}

// SetStatusOwned 记录状态写入并返回预置错误。
func (f *fakeSettingsRepository) SetStatusOwned(context.Context, int64, string, bool, string) error {
	f.statusWrites++
	if len(f.statusWriteErrs) > 0 {
		// statusWriteErr 保存当前调用按顺序取出的状态写入错误。
		statusWriteErr := f.statusWriteErrs[0]
		f.statusWriteErrs = f.statusWriteErrs[1:]
		return statusWriteErr
	}
	return f.statusWriteErr
}

// StatusOwned 返回预置的账号启用状态。
func (f *fakeSettingsRepository) StatusOwned(context.Context, int64, string) (bool, error) {
	return f.status, f.statusErr
}

// SetPauseOwned 返回预置的暂停截止时间。
func (f *fakeSettingsRepository) SetPauseOwned(context.Context, int64, string, int) (int64, error) {
	return f.updateResult, f.pauseErr
}

// GetPauseOwned 返回未暂停的默认状态。
func (f *fakeSettingsRepository) GetPauseOwned(context.Context, int64, string) (PauseState, error) {
	return f.pauseState, f.getPauseErr
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
	// stopErr 是预置的运行实例停止错误。
	stopErr error
	// stopping 表示测试替身是否已建立停止 fencing。
	stopping bool
}

// credentialInterleavingRepository 用真实互斥锁模拟 Cookie 替换和平台新 Token 保存的竞争时序。
type credentialInterleavingRepository struct {
	// mu 串行化同一账号 Cookie、metadata 与 Token 的敏感持久化转换。
	mu sync.Mutex
	// token 保存测试观察到的当前连接 Token，不包含真实凭证。
	token string
	// clearStarted 通知旧 Token 清理已经持有凭证锁。
	clearStarted chan struct{}
	// allowClear 控制旧 Token 清理何时完成，用于确认并发新 Token 写入会被锁阻塞。
	allowClear chan struct{}
}

// LockCredentials 获取测试账号唯一的敏感凭证锁。
func (repository *credentialInterleavingRepository) LockCredentials(string) func() {
	repository.mu.Lock()
	return repository.mu.Unlock
}

// UpdateSettings 模拟 Cookie 主写入成功，不改变测试中的新 Token。
func (repository *credentialInterleavingRepository) UpdateSettings(context.Context, SettingsUpdateInput) (int64, error) {
	return 0, nil
}

// UpdateLoginInfo 满足设置仓储接口；本竞争用例不写入登录资料。
func (repository *credentialInterleavingRepository) UpdateLoginInfo(context.Context, LoginInfoUpdateInput) error {
	return nil
}

// SetStatusOwned 满足设置仓储接口；本竞争用例不改变账号状态。
func (repository *credentialInterleavingRepository) SetStatusOwned(context.Context, int64, string, bool, string) error {
	return nil
}

// StatusOwned 返回停用，避免本凭证锁测试触发运行时重启。
func (repository *credentialInterleavingRepository) StatusOwned(context.Context, int64, string) (bool, error) {
	return false, nil
}

// SetPauseOwned 满足设置仓储接口；本竞争用例不涉及暂停状态。
func (repository *credentialInterleavingRepository) SetPauseOwned(context.Context, int64, string, int) (int64, error) {
	return 0, nil
}

// GetPauseOwned 满足设置仓储接口；本竞争用例不读取暂停状态。
func (repository *credentialInterleavingRepository) GetPauseOwned(context.Context, int64, string) (PauseState, error) {
	return PauseState{}, nil
}

// ClearTokens 在持有凭证锁时等待测试放行，然后清除 Cookie 变更前的旧 Token。
func (repository *credentialInterleavingRepository) ClearTokens(context.Context, string) error {
	close(repository.clearStarted)
	<-repository.allowClear
	repository.token = ""
	return nil
}

// Restart 记录重启并返回预置错误。
func (f *fakeSettingsRuntime) Restart(context.Context, string) error {
	f.restartCalls++
	return f.restartErr
}

// BeginStopping 记录当前测试账号的停止 fencing；重复进入时返回 false。
func (f *fakeSettingsRuntime) BeginStopping(string) bool {
	if f.stopping {
		return false
	}
	f.stopping = true
	return true
}

// EndStopping 释放当前测试账号的停止 fencing。
func (f *fakeSettingsRuntime) EndStopping(string) { f.stopping = false }

// StopContext 记录带关闭预算的停止调用并返回预置错误。
func (f *fakeSettingsRuntime) StopContext(context.Context, string) error {
	f.stopCalls++
	return f.stopErr
}

// TestSettingsServiceKeepsCredentialLockUntilOldTokenCleared 验证并发保存的新 Token 不会被旧 Token 清理覆盖。
func TestSettingsServiceKeepsCredentialLockUntilOldTokenCleared(t *testing.T) {
	// repository 保存带真实互斥锁的敏感凭证替身。
	repository := &credentialInterleavingRepository{token: "old-token", clearStarted: make(chan struct{}), allowClear: make(chan struct{})}
	// service 保存待验证 Cookie 更新顺序的账号设置服务。
	service, serviceErr := NewSettingsService(repository, nil)
	if serviceErr != nil {
		t.Fatalf("构造设置服务失败: %v", serviceErr)
	}
	// cookie 保存本次替换的 Cookie 明文，仅在测试输入范围存在。
	cookie := "updated-cookie"
	// updateDone 保存 Cookie 更新和旧 Token 清理完成后的结果。
	updateDone := make(chan error, 1)
	go func() {
		// _, updateErr 保存设置写入结果与错误；当前测试只关注凭证锁释放时机。
		_, updateErr := service.UpdateSettings(context.Background(), SettingsUpdateInput{UserID: 7, AccountID: "acc-1", Cookie: &cookie})
		updateDone <- updateErr
	}()
	select {
	case <-repository.clearStarted:
	case <-time.After(time.Second):
		t.Fatal("旧 Token 清理未开始")
	}
	// newTokenWritten 通知并发平台流程何时真正拿到凭证锁并写入新 Token。
	newTokenWritten := make(chan struct{})
	go func() {
		// unlock 是并发平台流程持有的同账号凭证锁释放函数。
		unlock := repository.LockCredentials("acc-1")
		repository.token = "new-token"
		unlock()
		close(newTokenWritten)
	}()
	select {
	case <-newTokenWritten:
		t.Fatal("旧 Token 清理完成前不应写入新 Token")
	case <-time.After(25 * time.Millisecond):
	}
	close(repository.allowClear)
	// updateErr 保存 Cookie 更新协程完成后返回的错误。
	if updateErr := <-updateDone; updateErr != nil {
		t.Fatalf("Cookie 更新失败: %v", updateErr)
	}
	select {
	case <-newTokenWritten:
	case <-time.After(time.Second):
		t.Fatal("凭证锁释放后未写入新 Token")
	}
	if repository.token != "new-token" {
		t.Fatalf("旧 Token 清理覆盖了新 Token: %q", repository.token)
	}
}

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

// TestSettingsServiceDelegatesLoginPauseAndConvenienceUpdates 验证登录资料、暂停状态和快捷设置入口的端口转发。
func TestSettingsServiceDelegatesLoginPauseAndConvenienceUpdates(t *testing.T) {
	// wantErr 是设置仓储返回的确定性错误。
	wantErr := errors.New("settings repository failed")
	// repository 是带暂停状态和错误注入能力的设置仓储替身。
	repository := &fakeSettingsRepository{updateResult: 42, pauseState: PauseState{Duration: 5, PausedUntil: 99, Paused: true}, loginErr: wantErr, pauseErr: wantErr}
	// service、err 保存账号设置服务及构造错误。
	service, err := NewSettingsService(repository, nil)
	if err != nil {
		t.Fatalf("NewSettingsService: %v", err)
	}
	// loginErr 保存登录资料写入返回的错误。
	if loginErr := service.UpdateLoginInfo(context.Background(), LoginInfoUpdateInput{UserID: 7, AccountID: "account", Username: "user"}); !errors.Is(loginErr, wantErr) {
		t.Fatalf("login error=%v", loginErr)
	}
	// pauseResult、pauseErr 保存暂停设置结果及错误。
	pauseResult, pauseErr := service.SetPause(context.Background(), 7, "account", 5)
	if pauseResult.PausedUntil != 42 || !errors.Is(pauseErr, wantErr) {
		t.Fatalf("pause result=%+v err=%v", pauseResult, pauseErr)
	}
	// pauseState、getErr 保存暂停查询结果及错误。
	pauseState, getErr := service.GetPause(context.Background(), 7, "account")
	if getErr != nil || !pauseState.Paused || pauseState.Duration != 5 {
		t.Fatalf("pause state=%+v err=%v", pauseState, getErr)
	}
	// repository.getPauseErr 保存暂停查询端口的错误传播场景。
	repository.getPauseErr = wantErr
	// failedPauseState、failedPauseErr 保存暂停查询失败后的结果。
	failedPauseState, failedPauseErr := service.GetPause(context.Background(), 7, "account")
	if failedPauseState != repository.pauseState || !errors.Is(failedPauseErr, wantErr) {
		t.Fatalf("failed pause state=%+v err=%v", failedPauseState, failedPauseErr)
	}
	// repository.getPauseErr 清除测试替身的暂停查询错误，供后续快捷设置继续使用。
	repository.getPauseErr = nil
	// autoConfirmErr 保存快捷设置自动确认开关的错误。
	if _, autoConfirmErr := service.SetAutoConfirm(context.Background(), 7, "account", true); autoConfirmErr != nil {
		t.Fatalf("SetAutoConfirm error=%v", autoConfirmErr)
	}
	// remarkErr 保存快捷设置备注的错误。
	if _, remarkErr := service.SetRemark(context.Background(), 7, "account", "remark"); remarkErr != nil {
		t.Fatalf("SetRemark error=%v", remarkErr)
	}
	// invalidService 表示未初始化的账号设置服务指针。
	var invalidService *SettingsService
	// invalidPauseErr 保存未初始化服务设置暂停时的错误。
	if _, invalidPauseErr := invalidService.SetPause(context.Background(), 7, "account", 1); invalidPauseErr == nil {
		t.Fatal("nil SetPause should fail")
	}
	// invalidGetPauseErr 保存未初始化服务查询暂停时的错误。
	if _, invalidGetPauseErr := invalidService.GetPause(context.Background(), 7, "account"); invalidGetPauseErr == nil {
		t.Fatal("nil GetPause should fail")
	}
	// uninitializedService 表示字段未装配仓储的设置服务。
	uninitializedService := &SettingsService{}
	// uninitializedPauseErr 保存未装配仓储时查询暂停的错误。
	if _, uninitializedPauseErr := uninitializedService.GetPause(context.Background(), 7, "account"); uninitializedPauseErr == nil {
		t.Fatal("未装配仓储的 GetPause should fail")
	}
	// invalidLoginErr 保存未初始化服务更新登录资料时的错误。
	if invalidLoginErr := invalidService.UpdateLoginInfo(context.Background(), LoginInfoUpdateInput{AccountID: "account"}); invalidLoginErr == nil {
		t.Fatal("nil UpdateLoginInfo should fail")
	}
	// emptyAccountPauseErr 保存空账号设置暂停时的输入错误。
	if _, emptyAccountPauseErr := service.SetPause(context.Background(), 7, "", 1); emptyAccountPauseErr == nil {
		t.Fatal("empty account pause should fail")
	}
	// emptyAccountGetPauseErr 保存空账号查询暂停时的输入错误。
	if _, emptyAccountGetPauseErr := service.GetPause(context.Background(), 7, ""); emptyAccountGetPauseErr == nil {
		t.Fatal("empty account get pause should fail")
	}
}
