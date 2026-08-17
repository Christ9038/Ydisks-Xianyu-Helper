package account

import (
	"context"
	"testing"
)

// loginSuccessStatusFake 保存登录成功编排测试中的启用状态。
type loginSuccessStatusFake struct{ enabled bool }

// GetStatus 返回测试账号启用状态。
func (f loginSuccessStatusFake) GetStatus(context.Context, string) bool { return f.enabled }

// loginSuccessRuntimeFake 记录登录成功后的重启请求。
type loginSuccessRuntimeFake struct{ restarts int }

// Restart 记录一次账号运行时重启。
func (f *loginSuccessRuntimeFake) Restart(context.Context, string) error {
	f.restarts++
	return nil
}

// TestLoginSuccessServiceSkipsMissingOptionalPorts 验证缺少可选后续端口时不产生副作用。
func TestLoginSuccessServiceSkipsMissingOptionalPorts(t *testing.T) {
	// service 保存未装配后续端口的登录成功服务。
	service := NewLoginSuccessService(nil, nil, nil, nil, nil)
	service.AfterSuccessfulLogin(context.Background(), 1, "account")
}

// TestLoginSuccessServiceRestartsEnabledAccount 验证登录成功后仅对启用账号触发运行时重启。
func TestLoginSuccessServiceRestartsEnabledAccount(t *testing.T) {
	// runtime 保存登录成功后重启调用的测试端口。
	runtime := &loginSuccessRuntimeFake{}
	// service 保存启用账号的登录成功编排服务。
	service := NewLoginSuccessService(nil, nil, loginSuccessStatusFake{enabled: true}, runtime, nil)
	service.AfterSuccessfulLogin(context.Background(), 1, "account")
	if runtime.restarts != 1 {
		t.Fatalf("restarts=%d", runtime.restarts)
	}
	// disabledRuntime 保存停用账号使用的独立运行时端口。
	disabledRuntime := &loginSuccessRuntimeFake{}
	// disabledService 保存停用账号的登录成功编排服务。
	disabledService := NewLoginSuccessService(nil, nil, loginSuccessStatusFake{}, disabledRuntime, nil)
	disabledService.AfterSuccessfulLogin(context.Background(), 1, "account")
	if disabledRuntime.restarts != 0 {
		t.Fatalf("disabled account restarts=%d", disabledRuntime.restarts)
	}
}
