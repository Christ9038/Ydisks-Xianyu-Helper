package account

import (
	"context"
	"errors"
	"testing"
)

// TestSettingsServiceCoversStatusFailures 验证启停状态写入、停止冲突和停止失败的保护分支。
func TestSettingsServiceCoversStatusFailures(t *testing.T) {
	// constructorErr 保存缺少设置仓储时的构造错误。
	if _, constructorErr := NewSettingsService(nil, nil); constructorErr == nil {
		t.Fatal("缺少设置仓储时构造应失败")
	}
	// wantErr 是状态持久化需要返回的稳定错误。
	wantErr := errors.New("status write failed")
	// repository 保存停用状态写入错误的设置仓储替身。
	repository := &fakeSettingsRepository{statusWriteErr: wantErr}
	// runtime 保存正常建立 fencing 但停止失败的运行时替身。
	runtime := &fakeSettingsRuntime{stopErr: errors.New("stop failed")}
	// service 保存待验证的账号设置应用服务。
	service, serviceErr := NewSettingsService(repository, runtime)
	if serviceErr != nil {
		t.Fatalf("构造设置服务失败: %v", serviceErr)
	}
	// enableErr 保存启用状态持久化错误。
	if _, enableErr := service.SetStatus(context.Background(), 7, "account", true); !errors.Is(enableErr, wantErr) {
		t.Fatalf("启用状态错误=%v", enableErr)
	}
	// stopResult 保存运行时停止失败后的显式结果。
	stopResult, stopErr := service.SetStatus(context.Background(), 7, "account", false)
	if stopErr != nil || !errors.Is(stopResult.RuntimeError, ErrRuntimeStopConflict) {
		t.Fatalf("停止失败结果=%+v err=%v", stopResult, stopErr)
	}
	// conflictRuntime 保存拒绝重复停止 fencing 的运行时替身。
	conflictRuntime := &fakeSettingsRuntime{}
	conflictRuntime.stopping = true
	// conflictService 保存用于验证停止冲突短路的账号设置服务。
	conflictService, conflictServiceErr := NewSettingsService(&fakeSettingsRepository{}, conflictRuntime)
	if conflictServiceErr != nil {
		t.Fatalf("构造冲突服务失败: %v", conflictServiceErr)
	}
	// conflictResult 保存停止 fencing 冲突后的显式结果。
	conflictResult, conflictErr := conflictService.SetStatus(context.Background(), 7, "account", false)
	if conflictErr != nil || !errors.Is(conflictResult.RuntimeError, ErrRuntimeStopConflict) {
		t.Fatalf("停止冲突结果=%+v err=%v", conflictResult, conflictErr)
	}
}

// TestSettingsServiceCoversRestartCompensationFailure 验证启用重启失败且停用补偿也失败时的组合错误。
func TestSettingsServiceCoversRestartCompensationFailure(t *testing.T) {
	// repository 保存启用成功但补偿停用失败的设置仓储替身。
	repository := &fakeSettingsRepository{statusWriteErrs: []error{nil, errors.New("compensation write failed")}}
	// runtime 保存重启失败的运行时替身。
	runtime := &fakeSettingsRuntime{restartErr: errors.New("restart failed")}
	// service 保存待验证的账号设置应用服务。
	service, serviceErr := NewSettingsService(repository, runtime)
	if serviceErr != nil {
		t.Fatalf("构造设置服务失败: %v", serviceErr)
	}
	// result 保存重启和补偿均失败时的运行时错误。
	result, callErr := service.SetStatus(context.Background(), 7, "account", true)
	if callErr != nil || !errors.Is(result.RuntimeError, ErrRuntimeStartUnavailable) {
		t.Fatalf("组合失败结果=%+v err=%v", result, callErr)
	}
}
