package composition

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// unsupportedCredentialVerificationStarter 在二维码服务不具备续期安全验证能力时占位。
// 生产使用的 qrlogin.Manager 总是具备该能力；该占位只让缺少能力的测试替身仍能完成装配，
// 并在用户真正发起验证时返回明确错误，而不是在请求期出现空指针。
type unsupportedCredentialVerificationStarter struct{}

// StartCredentialVerification 总是返回“不支持”错误。
func (unsupportedCredentialVerificationStarter) StartCredentialVerification(context.Context, string, []cookierefresh.BrowserCookie, string) (string, error) {
	return "", errors.New("当前二维码服务不支持续期安全验证，请重新扫码授权")
}

// credentialVerificationStarterFrom 返回 qr 提供的验证会话创建能力；qr 不具备时返回占位实现。
func credentialVerificationStarterFrom(qr adapter.QRLoginService) adapter.CredentialVerificationStarter {
	// ok 标记 qr. 取得的条目是否存在或调用是否成功；starter 保存 qr. 的结果，供后续分支和断言使用。
	if starter, ok := qr.(adapter.CredentialVerificationStarter); ok {
		return starter
	}
	return unsupportedCredentialVerificationStarter{}
}

// buildCredentialVerification 装配“续期需人工验证”用例：归属校验用 accountRepository，凭证读取与会话创建经适配器完成。
// registry 由组合根与续期调度器共享；为空时创建仅本服务可见的默认登记表（只发生在不装配调度器的测试里）。
func buildCredentialVerification(accountRepository interface {
	accountapp.ProfileSummaryRepository
	accountapp.CredentialRepository
}, registry *accountapp.PendingVerificationRegistry, qr adapter.QRLoginService) (*accountapp.CredentialVerificationService, error) {
	if registry == nil {
		registry = accountapp.NewPendingVerificationRegistry()
	}
	// platform 是读取账号最新凭证并创建验证会话的适配器。
	platform, platformErr := adapter.NewCredentialVerificationAdapter(accountRepository, credentialVerificationStarterFrom(qr))
	if platformErr != nil {
		return nil, fmt.Errorf("构造凭证验证适配器失败: %w", platformErr)
	}
	// service 是账号归属校验、一次性取走验证地址和创建会话的应用服务。
	service, serviceErr := accountapp.NewCredentialVerificationService(accountRepository, registry, platform)
	if serviceErr != nil {
		return nil, fmt.Errorf("构造凭证验证服务失败: %w", serviceErr)
	}
	return service, nil
}

// buildAccountLoginServices 装配长登录用例与续期安全验证用例，二者共享同一个账号 repository 与凭证写回通道。
func buildAccountLoginServices(accountRepository interface {
	accountapp.ProfileSummaryRepository
	accountapp.CredentialRepository
}, dependencies Dependencies) (*accountapp.LongLoginService, *accountapp.CredentialVerificationService, error) {
	// accountLongLogin 由适配器承载平台调用、Cookie 快照合并和凭证写回。
	accountLongLogin, longLoginErr := accountapp.NewLongLoginService(
		accountRepository,
		adapter.NewLongLoginAdapter(accountRepository, dependencies.LongLoginClient, dependencies.UpdateRunningCookie, dependencies.Logger),
	)
	if longLoginErr != nil {
		return nil, nil, fmt.Errorf("构造账号长登录服务失败: %w", longLoginErr)
	}
	// credentialVerification 承接续期被要求安全验证后的人工验证通道，复用账号归属与凭证读取端口。
	credentialVerification, verificationErr := buildCredentialVerification(accountRepository, dependencies.PendingVerifications, dependencies.QRLogin)
	if verificationErr != nil {
		return nil, nil, verificationErr
	}
	return accountLongLogin, credentialVerification, nil
}
