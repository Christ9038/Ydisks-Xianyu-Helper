package account

import (
	"context"
	"errors"
	"testing"
)

// TestPlatformCredentialServiceLoadOwnedValue 验证已通过归属检查的 Cookie 才能以窄范围明文返回。
func TestPlatformCredentialServiceLoadOwnedValue(t *testing.T) {
	// service 是返回有效平台凭证视图的应用服务。
	service, err := NewPlatformCredentialService(platformCredentialPortFake{detail: &CredentialDetail{ID: "acc-1", UserID: 7, Value: "sid=masked"}})
	if err != nil {
		t.Fatal(err)
	}
	// value、loadErr 保存所有权复核后的明文结果和错误。
	value, loadErr := service.LoadOwnedValue(context.Background(), 7, "acc-1")
	if loadErr != nil || value != "sid=masked" {
		t.Fatalf("已归属凭证读取异常: value=%q err=%v", value, loadErr)
	}
	// foreignService 是返回其他用户凭证视图的应用服务。
	foreignService, err := NewPlatformCredentialService(platformCredentialPortFake{detail: &CredentialDetail{ID: "acc-1", UserID: 8, Value: "sid=masked"}})
	if err != nil {
		t.Fatal(err)
	}
	// _, foreignErr 保存越权读取的稳定错误。
	_, foreignErr := foreignService.LoadOwnedValue(context.Background(), 7, "acc-1")
	if !errors.Is(foreignErr, ErrCredentialNotFound) {
		t.Fatalf("越权凭证读取应隐藏存在性: %v", foreignErr)
	}
	// emptyService 是返回空 Cookie 的应用服务。
	emptyService, err := NewPlatformCredentialService(platformCredentialPortFake{detail: &CredentialDetail{ID: "acc-1", UserID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	// _, emptyErr 保存空 Cookie 的稳定错误。
	_, emptyErr := emptyService.LoadOwnedValue(context.Background(), 7, "acc-1")
	if !errors.Is(emptyErr, ErrCredentialEmpty) {
		t.Fatalf("空 Cookie 应被拒绝: %v", emptyErr)
	}
	// missingDetailService 是返回空凭证视图的应用服务。
	missingDetailService, err := NewPlatformCredentialService(platformCredentialPortFake{})
	if err != nil {
		t.Fatal(err)
	}
	// missingDetailOwnedErr 保存 ValidateOwned 遇到空视图时的错误。
	if _, missingDetailOwnedErr := missingDetailService.ValidateOwned(context.Background(), 7, "acc-1"); !errors.Is(missingDetailOwnedErr, ErrCredentialNotFound) {
		t.Fatalf("空凭证视图归属错误=%v", missingDetailOwnedErr)
	}
	// missingDetailValueErr 保存 LoadOwnedValue 遇到空视图时的错误。
	if _, missingDetailValueErr := missingDetailService.LoadOwnedValue(context.Background(), 7, "acc-1"); !errors.Is(missingDetailValueErr, ErrCredentialNotFound) {
		t.Fatalf("空凭证视图读取错误=%v", missingDetailValueErr)
	}
}

// TestLongLoginServiceConstructsAndSets 验证长登录设置成功路径和必需端口校验。
func TestLongLoginServiceConstructsAndSets(t *testing.T) {
	// port 保存长登录平台设置端口及结果。
	port := &longLoginPortFake{result: LongLoginResult{CanOpenLongLogin: true, Enabled: true}}
	// missingRepositoryErr 保存缺少账号摘要仓储时的构造错误。
	_, missingRepositoryErr := NewLongLoginService(nil, port)
	if missingRepositoryErr == nil {
		t.Fatal("缺少摘要仓储应构造失败")
	}
	// missingPortErr 保存缺少平台端口时的构造错误。
	_, missingPortErr := NewLongLoginService(longLoginSummaryRepositoryFake{}, nil)
	if missingPortErr == nil {
		t.Fatal("缺少长登录平台端口应构造失败")
	}
	// service 保存有效长登录应用服务。
	service, err := NewLongLoginService(longLoginSummaryRepositoryFake{summary: Summary{ID: "acc-1"}}, port)
	if err != nil {
		t.Fatal(err)
	}
	// result、setErr 保存平台设置结果和错误。
	result, setErr := service.Set(context.Background(), 7, "acc-1", true)
	if setErr != nil || result.Enabled != true || port.calls != 1 {
		t.Fatalf("长登录设置异常: result=%+v err=%v calls=%d", result, setErr, port.calls)
	}
}

// TestQRLoginSessionRegistryDelete 验证删除会话会同时清理所有权和幂等结果。
func TestQRLoginSessionRegistryDelete(t *testing.T) {
	// registry 保存待删除的扫码会话状态。
	registry := NewQRLoginSessionRegistry()
	registry.Register("session-delete", 7, registry.currentTime())
	// persisted 保存一次成功持久化的非敏感幂等结果。
	if _, err := registry.PersistOnce("session-delete", 7, func() (QRLoginSessionPersistence, error) {
		return QRLoginSessionPersistence{AccountID: "acc-1"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	registry.Delete("session-delete")
	// err 保存删除后访问会话得到的不存在错误。
	if err := registry.Authorize("session-delete", 7); !errors.Is(err, ErrQRLoginSessionNotFound) {
		t.Fatalf("删除后会话仍可访问: %v", err)
	}
	// _, persistErr 验证删除后的幂等缓存不会继续返回旧结果。
	_, persistErr := registry.PersistOnce("session-delete", 7, func() (QRLoginSessionPersistence, error) {
		return QRLoginSessionPersistence{}, errors.New("work should not be reused")
	})
	if persistErr == nil {
		t.Fatal("删除后的会话不应复用旧持久化结果")
	}
	registry.Delete("")
}

// TestRuntimeServiceRestartBranches 验证重启、唤醒和未装配运行时的生命周期分支。
func TestRuntimeServiceRestartBranches(t *testing.T) {
	// runtime 保存成功重启的运行时端口。
	runtime := &runtimePortFake{}
	// wake 保存成功唤醒的自动化端口。
	wake := &wakePortFake{}
	// service 保存完整装配的运行时应用服务。
	service := NewRuntimeService(runtime, wake)
	// err 保存完整运行时重启成功路径的结果。
	if err := service.Restart(context.Background(), "acc-1"); err != nil || len(runtime.restarts) != 1 || len(wake.accounts) != 1 {
		t.Fatalf("重启成功路径异常: err=%v runtime=%v wake=%v", err, runtime.restarts, wake.accounts)
	}
	// runtimeErrService 保存运行时重启失败的服务。
	runtimeErrService := NewRuntimeService(&runtimePortFake{restartErr: errors.New("restart failed")}, wake)
	// err 保存运行时端口返回的重启错误。
	if err := runtimeErrService.Restart(context.Background(), "acc-1"); err == nil || err.Error() != "restart failed" {
		t.Fatalf("运行时重启错误未透传: %v", err)
	}
	// missingRuntimeService 保存没有运行时但需要唤醒任务的服务。
	missingRuntimeService := NewRuntimeService(nil, &wakePortFake{})
	// err 保存缺少运行时但仍执行唤醒的结果。
	if err := missingRuntimeService.Restart(context.Background(), "acc-1"); err != nil {
		t.Fatalf("未装配运行时应允许唤醒: %v", err)
	}
	// canceled、cancel 是用于验证重启取消保护的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保存取消上下文阻止重启的错误。
	if err := service.Restart(canceled, "acc-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文未阻止重启: %v", err)
	}
	// emptyService 保存空指针接收者分支的调用对象。
	var emptyService *RuntimeService
	// err 保存空运行时服务的初始化错误。
	if err := emptyService.Restart(context.Background(), "acc-1"); err == nil {
		t.Fatal("空运行时服务应返回初始化错误")
	}
}

// TestRuntimeServiceReturnsEmptyStatusesWithoutRuntime 验证未装配运行时返回空快照且不伪造在线状态。
func TestRuntimeServiceReturnsEmptyStatusesWithoutRuntime(t *testing.T) {
	// statuses、err 保存未装配运行时的状态快照结果。
	statuses, err := NewRuntimeService(nil, nil).RuntimeStatuses(context.Background())
	if err != nil || len(statuses) != 0 {
		t.Fatalf("空运行时状态快照异常: statuses=%v err=%v", statuses, err)
	}
	// canceled、cancel 是用于验证状态读取取消保护的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// _, canceledErr 保存取消上下文的状态读取错误。
	_, canceledErr := NewRuntimeService(nil, nil).RuntimeStatuses(canceled)
	if !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("取消上下文未阻止状态读取: %v", canceledErr)
	}
}
