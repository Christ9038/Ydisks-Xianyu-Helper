package account

import (
	"context"
	"errors"
	"testing"
)

// TestAccountServicesCoverEmptyAndUninitializedBoundaries 验证账号应用服务对空输入、空接收者和缺失端口的稳定保护。
func TestAccountServicesCoverEmptyAndUninitializedBoundaries(t *testing.T) {
	// auditRepository 保存登录审计测试端口。
	auditRepository := &fakeLoginAuditRepository{}
	// auditService 保存完整依赖的登录审计服务。
	auditService := NewLoginAuditService(auditRepository)
	// emptyAuditErr 保存空账号或空登录方式的兼容结果。
	emptyAuditErr := auditService.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: " ", Method: LoginMethodManual})
	if emptyAuditErr != nil || auditRepository.markCalls != 0 {
		t.Fatalf("空审计输入错误=%v calls=%d", emptyAuditErr, auditRepository.markCalls)
	}
	// emptyMethodErr 保存缺少登录方式时的兼容结果。
	emptyMethodErr := auditService.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: "account"})
	if emptyMethodErr != nil || auditRepository.markCalls != 0 {
		t.Fatalf("空登录方式错误=%v calls=%d", emptyMethodErr, auditRepository.markCalls)
	}

	// lifecycle 保存登录成功后的生命周期测试端口。
	lifecycle := &fakeLoginLifecycle{}
	// loginService 保存完整依赖的登录服务。
	loginService, loginServiceErr := NewLoginService(lifecycle)
	if loginServiceErr != nil {
		t.Fatal(loginServiceErr)
	}
	// missingWriterErr 保存缺少 Cookie 更新端口的错误。
	missingWriterErr := loginService.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "account"}, nil)
	if missingWriterErr == nil {
		t.Fatal("缺少 Cookie 更新端口不应成功")
	}
	// missingAccountErr 保存缺少账号标识的错误。
	missingAccountErr := loginService.UpdateCookie(context.Background(), UpdateCookieInput{}, &fakeCookieUpdater{})
	if missingAccountErr == nil {
		t.Fatal("缺少账号标识不应成功")
	}

	// profilePort 保存资料刷新测试端口。
	profilePort := &fakeProfilePort{}
	// profileService 保存完整依赖的资料服务。
	profileService, profileServiceErr := NewProfileService(fakeSummaryRepository{}, profilePort)
	if profileServiceErr != nil {
		t.Fatal(profileServiceErr)
	}
	// nilProfileService 保存空接收者资料服务。
	var nilProfileService *ProfileService
	// nilProfileErr 保存空接收者资料刷新错误。
	_, nilProfileErr := nilProfileService.RefreshProfile(context.Background(), 1, "account")
	if nilProfileErr == nil {
		t.Fatal("空资料服务不应成功")
	}
	// missingProfileErr 保存资料服务完整依赖的成功结果。
	_, missingProfileErr := profileService.RefreshProfile(context.Background(), 1, "account")
	if missingProfileErr != nil {
		t.Fatalf("资料服务边界测试失败=%v", missingProfileErr)
	}

	// nilSuccessService 保存空接收者登录成功服务。
	var nilSuccessService *LoginSuccessService
	nilSuccessService.AfterSuccessfulLogin(context.Background(), 1, "account")

	// registry 保存扫码会话幂等状态注册表。
	registry := NewQRLoginSessionRegistry()
	// persistedResult 保存已完成会话的非敏感结果。
	persistedResult, persistedErr := registry.PersistOnce("session", 7, func() (QRLoginSessionPersistence, error) {
		return QRLoginSessionPersistence{AccountID: "account"}, nil
	})
	if persistedErr != nil || persistedResult.AccountID != "account" {
		t.Fatalf("首次幂等保存失败=%+v err=%v", persistedResult, persistedErr)
	}
	// forbiddenResult、forbiddenErr 保存其他用户读取已完成会话的结果。
	forbiddenResult, forbiddenErr := registry.PersistOnce("session", 8, func() (QRLoginSessionPersistence, error) {
		t.Fatal("越权会话不应执行工作函数")
		return QRLoginSessionPersistence{}, nil
	})
	if forbiddenResult.AccountID != "" || !errors.Is(forbiddenErr, ErrQRLoginSessionForbidden) {
		t.Fatalf("幂等会话越权结果=%+v err=%v", forbiddenResult, forbiddenErr)
	}
}

// TestAccountRuntimeAndSettingsCoverFailureBoundaries 验证运行时同步失败和设置服务输入保护。
func TestAccountRuntimeAndSettingsCoverFailureBoundaries(t *testing.T) {
	// runtimeError 是运行时 Cookie 同步失败的底层错误。
	runtimeError := errors.New("runtime update failed")
	// runtime 保存注入同步错误的运行时端口。
	runtime := &runtimePortFake{updateErr: runtimeError}
	// service 保存运行时同步应用服务。
	service := NewRuntimeService(runtime, nil)
	// updateErr 保存运行时同步失败结果。
	updateErr := service.UpdateCookie(context.Background(), "account", "value")
	if !errors.Is(updateErr, runtimeError) {
		t.Fatalf("运行时同步错误=%v", updateErr)
	}
	// settingsService 保存具备持久化端口的账号设置服务。
	settingsService, settingsErr := NewSettingsService(&fakeSettingsRepository{}, nil)
	if settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// emptySettingsErr 保存缺少账号标识的设置错误。
	_, emptySettingsErr := settingsService.UpdateSettings(context.Background(), SettingsUpdateInput{})
	if emptySettingsErr == nil {
		t.Fatal("缺少账号标识的设置不应成功")
	}
	// nilSettingsService 保存空接收者设置服务。
	var nilSettingsService *SettingsService
	// nilSettingsErr 保存空接收者设置错误。
	_, nilSettingsErr := nilSettingsService.UpdateSettings(context.Background(), SettingsUpdateInput{AccountID: "account"})
	if nilSettingsErr == nil {
		t.Fatal("空设置服务不应成功")
	}
	// restartError 是运行时重启失败的底层错误。
	restartError := errors.New("runtime restart failed")
	// restartRuntime 保存注入重启错误的运行时端口。
	restartRuntime := &runtimePortFake{restartErr: restartError}
	// wake 保存不应在重启失败后被调用的唤醒端口。
	wake := &wakePortFake{}
	// restartService 保存重启失败测试服务。
	restartService := NewRuntimeService(restartRuntime, wake)
	// restartErr 保存重启失败结果。
	restartErr := restartService.Restart(context.Background(), "account")
	if !errors.Is(restartErr, restartError) || len(wake.accounts) != 0 {
		t.Fatalf("重启失败错误=%v 唤醒记录=%v", restartErr, wake.accounts)
	}
	// noWakeService 保存重启成功但未装配唤醒端口的服务。
	noWakeService := NewRuntimeService(&runtimePortFake{}, nil)
	// noWakeErr 保存无唤醒端口时的成功结果。
	noWakeErr := noWakeService.Restart(context.Background(), "account")
	if noWakeErr != nil {
		t.Fatalf("无唤醒端口重启错误=%v", noWakeErr)
	}
}
