package account

import (
	"context"
	"errors"
	"testing"
)

// TestLoginAuditPureMappingsCoversAliasesAndMessageLimits 覆盖登录方式别名、触发原因和审计摘要截断边界。
func TestLoginAuditPureMappingsCoversAliasesAndMessageLimits(t *testing.T) {
	// cases 保存登录方式别名及其稳定归一化结果。
	cases := map[string]string{"": LoginMethodManual, "cookie": LoginMethodManual, "manual_cookie": LoginMethodManual, "password_login": LoginMethodPassword, "qr_login": LoginMethodQRScan, "CUSTOM": "custom"}
	// method、want 表示当前待验证的登录方式映射。
	for method, want := range cases {
		// got 保存当前别名归一化后的稳定方式。
		if got := NormalizeLoginMethod(method); got != want {
			t.Fatalf("登录方式归一化异常 method=%q got=%q want=%q", method, got, want)
		}
	}
	// got 保存手动登录触发原因。
	if got := loginTriggerReason("manual"); got == "" {
		t.Fatal("手动登录触发原因为空")
	}
	// got 保存密码登录触发原因。
	if got := loginTriggerReason("password"); got == "" {
		t.Fatal("密码登录触发原因为空")
	}
	// got 保存扫码登录触发原因。
	if got := loginTriggerReason("qr"); got == "" {
		t.Fatal("扫码登录触发原因为空")
	}
	// got 保存未知登录方式触发原因。
	if got := loginTriggerReason("unknown"); got != "" {
		t.Fatalf("未知登录方式不应生成触发原因: %q", got)
	}
	// got 保存零长度限制下的审计摘要。
	if got := truncateLoginMessage("message", 0); got != "" {
		t.Fatalf("零长度限制未清空摘要: %q", got)
	}
	// got 保存截断后的审计摘要。
	if got := truncateLoginMessage("message", 3); got != "mes" {
		t.Fatalf("摘要截断异常: %q", got)
	}
	// got 保存未超限的审计摘要。
	if got := truncateLoginMessage("message", 20); got != "message" {
		t.Fatalf("未超限摘要不应改变: %q", got)
	}
}

// TestCredentialRefreshCoordinatorCoversNilAndMissingWork 覆盖协调器 nil 接收者、懒初始化和空工作函数分支。
func TestCredentialRefreshCoordinatorCoversNilAndMissingWork(t *testing.T) {
	// nilCoordinator 保存 nil 接收者。
	var nilCoordinator *CredentialRefreshCoordinator
	if nilCoordinator.TryBegin("account") {
		t.Fatal("nil 协调器不应接受恢复任务")
	}
	nilCoordinator.Finish("account")
	// _, _, nilErr 保存 nil 协调器 Run 结果。
	_, _, nilErr := nilCoordinator.Run(context.Background(), "account", func(context.Context) (bool, error) { return true, nil })
	if nilErr == nil {
		t.Fatal("nil 协调器 Run 未返回错误")
	}
	// coordinator 保存零值协调器，验证 TryBegin 能懒初始化集合。
	coordinator := &CredentialRefreshCoordinator{}
	if !coordinator.TryBegin("account") || coordinator.TryBegin("account") {
		t.Fatal("零值协调器的同账号互斥异常")
	}
	coordinator.Finish("account")
	// accepted、renewed、workErr 保存空工作函数结果。
	accepted, renewed, workErr := coordinator.Run(context.Background(), "account", nil)
	if !accepted || renewed || workErr == nil {
		t.Fatalf("空恢复工作结果异常: accepted=%v renewed=%v err=%v", accepted, renewed, workErr)
	}
}

// TestLongLoginServiceCoversQuerySetAndNilGuards 覆盖长登录查询、设置错误传递和 nil 服务保护。
func TestLongLoginServiceCoversQuerySetAndNilGuards(t *testing.T) {
	// portErr 保存平台长登录端口错误。
	portErr := errors.New("长登录请求失败")
	// port 保存返回平台错误的长登录端口替身。
	port := &longLoginPortFake{queryErr: portErr, setErr: portErr}
	// service、serviceErr 保存长登录服务构造结果。
	service, serviceErr := NewLongLoginService(longLoginSummaryRepositoryFake{summary: Summary{ID: "account-1"}}, port)
	if serviceErr != nil {
		t.Fatalf("构造长登录服务失败: %v", serviceErr)
	}
	// err 保存长登录查询平台错误。
	if _, err := service.Query(context.Background(), 7, "account-1"); !errors.Is(err, portErr) {
		t.Fatalf("长登录查询错误异常: %v", err)
	}
	// err 保存长登录设置平台错误。
	if _, err := service.Set(context.Background(), 7, "account-1", true); !errors.Is(err, portErr) {
		t.Fatalf("长登录设置错误异常: %v", err)
	}
	// nilService 保存 nil 接收者。
	var nilService *LongLoginService
	// err 保存 nil 长登录服务查询错误。
	if _, err := nilService.Query(context.Background(), 7, "account-1"); err == nil {
		t.Fatal("nil 长登录服务查询未返回错误")
	}
	// err 保存 nil 长登录服务设置错误。
	if _, err := nilService.Set(context.Background(), 7, "account-1", true); err == nil {
		t.Fatal("nil 长登录服务设置未返回错误")
	}
}

// TestPlatformCredentialServiceCoversNilReceiverGuard 覆盖平台凭证服务 nil 接收者保护。
func TestPlatformCredentialServiceCoversNilReceiverGuard(t *testing.T) {
	// service 保存 nil 接收者。
	var service *PlatformCredentialService
	// err 保存 nil 平台凭证读取错误。
	if _, err := service.LoadPlatformDetail(context.Background(), "account-1"); err == nil {
		t.Fatal("nil 平台凭证服务未拒绝读取")
	}
	// err 保存 nil 平台凭证归属错误。
	if _, err := service.ValidateOwned(context.Background(), 7, "account-1"); err == nil {
		t.Fatal("nil 平台凭证服务未拒绝归属校验")
	}
	// err 保存 nil 平台 Cookie 读取错误。
	if _, err := service.LoadOwnedValue(context.Background(), 7, "account-1"); err == nil {
		t.Fatal("nil 平台凭证服务未拒绝 Cookie 读取")
	}
}
