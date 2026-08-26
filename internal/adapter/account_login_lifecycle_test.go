package adapter

import (
	"context"
	"log/slog"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// lifecycleAuditRepositoryFake 是生命周期适配器测试使用的登录审计仓储替身。
type lifecycleAuditRepositoryFake struct {
	// methods 记录适配器提交的归一化登录方式。
	methods []string
}

// MarkLogin 记录最近成功登录方式。
func (f *lifecycleAuditRepositoryFake) MarkLogin(_ context.Context, _ string, method string, _ int64) error {
	f.methods = append(f.methods, method)
	return nil
}

// SetStatusWithReason 满足审计仓储接口；生命周期测试不关心状态写入细节。
func (f *lifecycleAuditRepositoryFake) SetStatusWithReason(context.Context, string, bool, string) error {
	return nil
}

// AddLoginLog 满足审计仓储接口；登录方式已由 MarkLogin 记录。
func (f *lifecycleAuditRepositoryFake) AddLoginLog(context.Context, accountapp.LoginAuditLog) error {
	return nil
}

// TestAccountLoginLifecycleRoutesManualAndQRSuccess 验证手动登录与扫码登录共用审计和后续编排端口。
func TestAccountLoginLifecycleRoutesManualAndQRSuccess(t *testing.T) {
	// repository 保存测试登录审计调用记录。
	repository := &lifecycleAuditRepositoryFake{}
	// audit 保存使用测试仓储的应用审计服务。
	audit := accountapp.NewLoginAuditService(repository)
	// lifecycle 是不持有 Server 的登录生命周期适配器。
	lifecycle := NewAccountLoginLifecycle(audit, nil, nil)
	// manualErr 保存手动登录生命周期调用结果；该方法本身不返回审计错误。
	lifecycle.AfterSuccessfulLogin(context.Background(), 1, "manual-account", accountapp.LoginMethodManual)
	// lifecycle.AfterSuccessfulQRLogin 记录扫码登录方式并保持同一审计端口。
	lifecycle.AfterSuccessfulQRLogin(context.Background(), 1, "qr-account")
	if len(repository.methods) != 2 || repository.methods[0] != accountapp.LoginMethodManual || repository.methods[1] != accountapp.LoginMethodQRScan {
		t.Fatalf("登录生命周期未正确路由审计方式: %+v", repository.methods)
	}
}

// TestAccountLoginLifecycleAllowsMissingOptionalPorts 验证缺少可选应用服务和日志端口时仍安全返回。
func TestAccountLoginLifecycleAllowsMissingOptionalPorts(t *testing.T) {
	// lifecycle 是仅用于验证 nil 安全性的零依赖适配器。
	lifecycle := NewAccountLoginLifecycle(nil, nil, nil)
	lifecycle.AfterSuccessfulLogin(context.Background(), 1, "account", accountapp.LoginMethodManual)
	lifecycle.AfterSuccessfulQRLogin(context.Background(), 1, "account")
	lifecycle.ReportQRLoginCleanupFailure(context.Background(), "account", nil)
	// successService 保存不含可选依赖的非空登录成功编排服务。
	successService := accountapp.NewLoginSuccessService(nil, nil, nil, nil, nil)
	// lifecycleWithSuccess 保存用于覆盖后续编排分支的生命周期适配器。
	lifecycleWithSuccess := NewAccountLoginLifecycle(nil, successService, slog.Default())
	lifecycleWithSuccess.AfterSuccessfulLogin(context.Background(), 1, "account", accountapp.LoginMethodManual)
	lifecycleWithSuccess.AfterSuccessfulQRLogin(context.Background(), 1, "account")
	lifecycleWithSuccess.ReportQRLoginCleanupFailure(context.Background(), "account", context.Canceled)
	// nilLifecycle 验证生命周期适配器 nil 接收者保持安全返回。
	var nilLifecycle *AccountLoginLifecycle
	nilLifecycle.AfterSuccessfulLogin(context.Background(), 1, "account", accountapp.LoginMethodManual)
	nilLifecycle.AfterSuccessfulQRLogin(context.Background(), 1, "account")
	nilLifecycle.ReportQRLoginCleanupFailure(context.Background(), "account", context.Canceled)
}
