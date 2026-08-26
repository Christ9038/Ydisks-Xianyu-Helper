package account

import (
	"context"
	"errors"
	"testing"
)

// nilRuntimeContext 返回用于覆盖兼容 nil Context 分支的空上下文接口。
func nilRuntimeContext() context.Context { return nil }

// TestRuntimeServiceCoversNilContextAndValidationGuards 验证运行时服务允许 nil Context 并拒绝空账号标识。
func TestRuntimeServiceCoversNilContextAndValidationGuards(t *testing.T) {
	// service 保存可接收 nil Context 的完整运行时服务。
	service := NewRuntimeService(&runtimePortFake{}, &wakePortFake{})
	// err 保存 nil Context Cookie 更新的结果错误。
	if err := service.UpdateCookie(nilRuntimeContext(), "account", "cookie"); err != nil {
		t.Fatalf("nil Context Cookie 更新失败: %v", err)
	}
	// err 保存 nil Context 重启的结果错误。
	if err := service.Restart(nilRuntimeContext(), "account"); err != nil {
		t.Fatalf("nil Context 重启失败: %v", err)
	}
	// emptyAccountErr 保存空账号 Cookie 更新的输入错误。
	if emptyAccountErr := service.UpdateCookie(nilRuntimeContext(), " ", "cookie"); emptyAccountErr == nil {
		t.Fatal("空账号 Cookie 更新应失败")
	}
	// emptyRestartErr 保存空账号重启的输入错误。
	if emptyRestartErr := service.Restart(nilRuntimeContext(), " "); emptyRestartErr == nil {
		t.Fatal("空账号重启应失败")
	}
	// emptyRecoveryResult 保存空账号凭证恢复的拒绝结果。
	if emptyRecoveryResult := service.RecoverExpiredCredential(nilRuntimeContext(), " "); emptyRecoveryResult {
		t.Fatal("空账号凭证恢复不应转发")
	}
	if contextError(nilRuntimeContext()) != nil {
		t.Fatal("nil Context 不应产生取消错误")
	}
}

// TestRuntimeServiceCoversNilReceiversAndStatusErrors 验证空接收者、缺失运行时和状态读取错误边界。
func TestRuntimeServiceCoversNilReceiversAndStatusErrors(t *testing.T) {
	// nilService 表示未初始化的运行时服务。
	var nilService *RuntimeService
	if nilService.UpdateCookie(context.Background(), "account", "cookie") == nil {
		t.Fatal("空服务 Cookie 更新应失败")
	}
	// statusesErr 保存空服务状态读取时的初始化错误。
	if _, statusesErr := nilService.RuntimeStatuses(context.Background()); statusesErr == nil {
		t.Fatal("空服务状态读取应失败")
	}
	if nilService.RecoverExpiredCredential(context.Background(), "account") {
		t.Fatal("空服务凭证恢复不应成功")
	}
	// statusErr 是运行状态读取需要返回的稳定错误。
	statusErr := errors.New("runtime status failed")
	// statusService 保存状态读取错误的运行时服务。
	statusService := NewRuntimeService(&runtimePortFake{statusesErr: statusErr}, nil)
	// actualStatusErr 保存状态读取错误透传结果。
	if _, actualStatusErr := statusService.RuntimeStatuses(context.Background()); !errors.Is(actualStatusErr, statusErr) {
		t.Fatalf("状态读取错误未透传: %v", actualStatusErr)
	}
	// missingRuntimeService 保存没有运行时端口的服务。
	missingRuntimeService := NewRuntimeService(nil, nil)
	if missingRuntimeService.RecoverExpiredCredential(context.Background(), "account") {
		t.Fatal("缺失运行时凭证恢复不应成功")
	}
}
