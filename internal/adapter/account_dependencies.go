package adapter

import (
	"errors"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// AccountDependencies 封装账号登录、认证和扫码用例所需的窄适配器构造能力。
// 它只在 adapter 内部持有 Store，避免 Server 通过通用设施容器隐式获取账号仓储。
type AccountDependencies struct {
	// store 是账号适配器共享的数据库入口，不向 Server 或应用层暴露。
	store *db.Store
}

// NewAccountDependencies 构造账号专用依赖，并在缺少数据库入口时立即返回错误。
func NewAccountDependencies(store *db.Store) (*AccountDependencies, error) {
	if store == nil {
		return nil, errors.New("账号依赖 Store 不能为空")
	}
	return &AccountDependencies{store: store}, nil
}

// NewAccountLoginRepository 创建账号凭证和资料仓储。
func (d *AccountDependencies) NewAccountLoginRepository() *AccountLoginRepository {
	if d == nil {
		return nil
	}
	return NewAccountLoginRepository(d.store)
}

// NewAccountSettingsRepository 创建账号设置仓储。
func (d *AccountDependencies) NewAccountSettingsRepository() *AccountSettingsRepository {
	if d == nil {
		return nil
	}
	return NewAccountSettingsRepository(d.store)
}

// NewAccountSummaryRepository 创建不读取敏感凭证的账号摘要仓储。
func (d *AccountDependencies) NewAccountSummaryRepository() *AccountSummaryRepository {
	if d == nil {
		return nil
	}
	return NewAccountSummaryRepository(d.store)
}

// NewQRLoginRepository 创建扫码登录成功写入所需的凭证端口。
func (d *AccountDependencies) NewQRLoginRepository() accountapp.QRLoginRepository {
	if d == nil {
		return nil
	}
	return NewQRLoginRepository(d.store)
}

// NewAuthenticationRepository 创建用户认证仓储。
func (d *AccountDependencies) NewAuthenticationRepository() *AuthenticationRepository {
	if d == nil {
		return nil
	}
	return NewAuthenticationRepository(d.store)
}

// NewAccountLoginAuditRepository 创建账号登录审计仓储。
func (d *AccountDependencies) NewAccountLoginAuditRepository() *AccountLoginAuditRepository {
	if d == nil {
		return nil
	}
	return NewAccountLoginAuditRepository(d.store)
}
