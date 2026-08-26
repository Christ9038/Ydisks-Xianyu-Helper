package account

import (
	"context"
	"errors"
	"testing"
)

// fakeCookieWriter 是登录应用服务测试用的凭证写入端口替身。
type fakeCookieWriter struct {
	// err 是写入凭证时需要模拟的失败。
	err error
	// accountID 和 userID 保存最近一次写入请求，验证敏感值未进入应用输入。
	accountID string
	userID    int64
	called    bool
}

// fakeCookieUpdater 是更新凭证应用服务测试用的专用端口替身，不接收明文 Cookie。
type fakeCookieUpdater struct {
	// err 是更新端口需要模拟的失败，包括归属失败、版本冲突和持久化失败。
	err error
	// accountID 和 userID 保存最近一次更新请求，用于验证应用层只传递非敏感身份。
	accountID string
	userID    int64
	called    bool
}

// UpdateOwnedCookie 记录更新请求并返回预设结果。
func (u *fakeCookieUpdater) UpdateOwnedCookie(_ context.Context, accountID string, userID, _ int64) error {
	u.called = true
	u.accountID = accountID
	u.userID = userID
	return u.err
}

// CreateOwnedCookie 记录写入请求但不接收明文 Cookie。
func (w *fakeCookieWriter) CreateOwnedCookie(_ context.Context, accountID string, userID int64) error {
	w.called = true
	w.accountID = accountID
	w.userID = userID
	return w.err
}

// fakeLoginLifecycle 是登录后续编排测试用的生命周期端口替身。
type fakeLoginLifecycle struct {
	// calls 保存登录后续编排的调用次数。
	calls int
	// userID、accountID 和 method 保存最近一次登录后续输入。
	userID    int64
	accountID string
	method    string
}

// AfterSuccessfulLogin 记录登录成功后的编排输入。
func (l *fakeLoginLifecycle) AfterSuccessfulLogin(_ context.Context, userID int64, accountID, method string) {
	l.calls++
	l.userID = userID
	l.accountID = accountID
	l.method = method
}

// TestLoginServiceRequiresLifecycleAndWriter 验证必需端口缺失时快速失败。
func TestLoginServiceRequiresLifecycleAndWriter(t *testing.T) {
	// lifecycle 是有效的生命周期端口替身。
	lifecycle := &fakeLoginLifecycle{}
	// missingLifecycleErr 保存缺少生命周期端口时的构造错误。
	_, missingLifecycleErr := NewLoginService(nil)
	if missingLifecycleErr == nil {
		t.Fatal("缺少生命周期端口时应构造失败")
	}
	// service 保存使用有效生命周期端口构造的登录服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// missingWriterErr 保存缺少凭证写入端口时的执行错误。
	missingWriterErr := service.CreateCookie(context.Background(), CreateCookieInput{AccountID: "acc1"}, nil)
	if missingWriterErr == nil {
		t.Fatal("缺少 Cookie 写入端口时应失败")
	}
	// missingAccountErr 保存空账号标识的创建错误。
	missingAccountErr := service.CreateCookie(context.Background(), CreateCookieInput{}, &fakeCookieWriter{})
	if missingAccountErr == nil {
		t.Fatal("空账号标识时创建 Cookie 不应成功")
	}
	// nilService 表示未初始化的登录服务指针。
	var nilService *LoginService
	// nilCreateErr、nilUpdateErr 保存空服务指针的创建和更新错误。
	nilCreateErr := nilService.CreateCookie(context.Background(), CreateCookieInput{AccountID: "acc1"}, &fakeCookieWriter{})
	// nilUpdateErr 保存空服务指针的凭证更新错误。
	nilUpdateErr := nilService.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "acc1"}, &fakeCookieUpdater{})
	if nilCreateErr == nil || nilUpdateErr == nil {
		t.Fatal("空登录服务不应执行凭证操作")
	}
}

// TestLoginServiceCreateCookieKeepsCredentialOutOfInput 验证成功登录只通过凭证端口写入并触发后续编排。
func TestLoginServiceCreateCookieKeepsCredentialOutOfInput(t *testing.T) {
	// lifecycle 是记录后续审计和运行时编排的测试替身。
	lifecycle := &fakeLoginLifecycle{}
	// writer 是不暴露 Cookie 参数的凭证写入测试替身。
	writer := &fakeCookieWriter{}
	// service 保存使用有效生命周期端口构造的登录服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// createErr 保存成功用例执行时的错误。
	createErr := service.CreateCookie(context.Background(), CreateCookieInput{AccountID: "acc1", UserID: 7, LoginMethod: "cookie"}, writer)
	if createErr != nil {
		t.Fatalf("创建 Cookie 登录失败: %v", createErr)
	}
	if !writer.called || writer.accountID != "acc1" || writer.userID != 7 {
		t.Fatalf("凭证写入端口输入异常: %+v", writer)
	}
	if lifecycle.calls != 1 || lifecycle.accountID != "acc1" || lifecycle.userID != 7 || lifecycle.method != "manual" {
		t.Fatalf("登录后续编排输入异常: %+v", lifecycle)
	}
}

// TestLoginServiceCreateCookieStopsAfterWriterFailure 验证凭证写入失败时不会执行成功后续编排。
func TestLoginServiceCreateCookieStopsAfterWriterFailure(t *testing.T) {
	// expectedErr 是凭证写入端口需要返回的故障。
	expectedErr := errors.New("凭证写入失败")
	// lifecycle 是用于确认未被调用的后续编排替身。
	lifecycle := &fakeLoginLifecycle{}
	// writer 是返回预期错误的凭证写入替身。
	writer := &fakeCookieWriter{err: expectedErr}
	// service 保存使用有效生命周期端口构造的登录服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// callErr 保存凭证写入失败后由应用服务传出的错误。
	callErr := service.CreateCookie(context.Background(), CreateCookieInput{AccountID: "acc1"}, writer)
	if !errors.Is(callErr, expectedErr) {
		t.Fatalf("应保留凭证写入错误，got %v", callErr)
	}
	if lifecycle.calls != 0 {
		t.Fatal("凭证写入失败时不应触发登录成功后续编排")
	}
}

// TestLoginServiceCreateCookiePropagatesOwnershipFailure 验证凭证端口拒绝跨用户账号时不会伪造登录成功。
func TestLoginServiceCreateCookiePropagatesOwnershipFailure(t *testing.T) {
	// ownershipErr 是持久化端口完成账号归属校验后返回的越权错误。
	ownershipErr := errors.New("账号不属于当前用户")
	// lifecycle 是用于确认越权请求未进入成功后续编排的测试替身。
	lifecycle := &fakeLoginLifecycle{}
	// writer 是模拟归属校验失败的凭证写入端口。
	writer := &fakeCookieWriter{err: ownershipErr}
	// service 保存使用有效生命周期端口构造的登录服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// callErr 保存归属校验失败后由应用服务传出的错误。
	callErr := service.CreateCookie(context.Background(), CreateCookieInput{AccountID: "other", UserID: 7}, writer)
	if !errors.Is(callErr, ownershipErr) {
		t.Fatalf("应保留归属校验错误，got %v", callErr)
	}
	if lifecycle.calls != 0 {
		t.Fatal("归属校验失败时不应触发登录成功后续编排")
	}
}

// TestLoginServiceUpdateCookieSuccess 验证更新成功后才触发统一登录后续编排。
func TestLoginServiceUpdateCookieSuccess(t *testing.T) {
	// lifecycle 记录更新成功后的审计、资料和运行时同步调用。
	lifecycle := &fakeLoginLifecycle{}
	// updater 只接收账号身份，不接收明文 Cookie。
	updater := &fakeCookieUpdater{}
	// service 保存使用有效生命周期端口构造的登录应用服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// updateErr 保存应用服务返回的更新结果。
	updateErr := service.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "acc1", UserID: 7, LoginMethod: "qr_scan"}, updater)
	if updateErr != nil {
		t.Fatalf("更新 Cookie 失败: %v", updateErr)
	}
	if !updater.called || updater.accountID != "acc1" || updater.userID != 7 {
		t.Fatalf("更新端口输入异常: %+v", updater)
	}
	if lifecycle.calls != 1 || lifecycle.method != "qr_scan" {
		t.Fatalf("成功更新后未触发正确后续编排: %+v", lifecycle)
	}
	// plainUpdateErr 保存未声明登录方式时的普通凭证替换结果。
	plainUpdateErr := service.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "acc1", UserID: 7}, updater)
	if plainUpdateErr != nil || lifecycle.calls != 2 || lifecycle.method != "" {
		t.Fatalf("普通凭证替换结果错误：err=%v lifecycle=%+v", plainUpdateErr, lifecycle)
	}
}

// TestLoginServiceUpdateCookiePropagatesOwnershipFailure 验证归属失败不会触发成功后续编排。
func TestLoginServiceUpdateCookiePropagatesOwnershipFailure(t *testing.T) {
	// ownershipErr 是凭证适配器完成账号归属校验后返回的越权错误。
	ownershipErr := errors.New("账号不属于当前用户")
	// lifecycle 用于确认归属失败时不会记录登录成功。
	lifecycle := &fakeLoginLifecycle{}
	// updater 模拟归属校验失败。
	updater := &fakeCookieUpdater{err: ownershipErr}
	// service 保存有效应用服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// updateErr 保存归属失败结果。
	updateErr := service.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "other", UserID: 7}, updater)
	if !errors.Is(updateErr, ownershipErr) {
		t.Fatalf("应保留归属失败错误，got %v", updateErr)
	}
	if lifecycle.calls != 0 {
		t.Fatal("归属失败时不应触发登录成功后续编排")
	}
}

// TestLoginServiceUpdateCookiePropagatesVersionConflict 验证并发版本冲突会原样返回并阻止旧响应生效。
func TestLoginServiceUpdateCookiePropagatesVersionConflict(t *testing.T) {
	// lifecycle 用于确认版本冲突不会触发运行时重启。
	lifecycle := &fakeLoginLifecycle{}
	// updater 模拟凭证快照已被其他请求更新后的冲突结果。
	updater := &fakeCookieUpdater{err: ErrCredentialConflict}
	// service 保存有效应用服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// updateErr 保存并发版本冲突结果。
	updateErr := service.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "acc1", UserID: 7}, updater)
	if !errors.Is(updateErr, ErrCredentialConflict) {
		t.Fatalf("应返回版本冲突，got %v", updateErr)
	}
	if lifecycle.calls != 0 {
		t.Fatal("版本冲突时不应触发登录成功后续编排")
	}
}

// TestLoginServiceUpdateCookiePropagatesWriteFailure 验证凭证持久化失败不会伪造登录成功。
func TestLoginServiceUpdateCookiePropagatesWriteFailure(t *testing.T) {
	// writeErr 是凭证适配器返回的底层写入故障。
	writeErr := errors.New("凭证写入失败")
	// lifecycle 用于确认写入失败时未触发后续编排。
	lifecycle := &fakeLoginLifecycle{}
	// updater 模拟数据库写入失败。
	updater := &fakeCookieUpdater{err: writeErr}
	// service 保存有效应用服务；serviceErr 保存构造错误。
	service, serviceErr := NewLoginService(lifecycle)
	if serviceErr != nil {
		t.Fatalf("构造登录服务失败: %v", serviceErr)
	}
	// updateErr 保存写入失败结果。
	updateErr := service.UpdateCookie(context.Background(), UpdateCookieInput{AccountID: "acc1", UserID: 7}, updater)
	if !errors.Is(updateErr, writeErr) {
		t.Fatalf("应返回凭证写入错误，got %v", updateErr)
	}
	if lifecycle.calls != 0 {
		t.Fatal("写入失败时不应触发登录成功后续编排")
	}
}
