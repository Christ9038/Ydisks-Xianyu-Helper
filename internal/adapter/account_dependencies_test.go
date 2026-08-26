package adapter

import (
	"testing"

	"xianyu-go/internal/db"
)

// TestNewAccountDependenciesRejectsNilStore 确保账号专用依赖不会在缺少数据库入口时构造成功。
func TestNewAccountDependenciesRejectsNilStore(t *testing.T) {
	// dependencies、err 分别保存构造结果和缺少 Store 时的错误。
	dependencies, err := NewAccountDependencies(nil)
	if dependencies != nil {
		t.Fatal("缺少 Store 时不应返回账号依赖")
	}
	if err == nil {
		t.Fatal("缺少 Store 时应返回构造错误")
	}
}

// TestNewAccountDependenciesCreatesTypedFactory 确保有效 Store 可创建全部账号窄适配器。
func TestNewAccountDependenciesCreatesTypedFactory(t *testing.T) {
	// store 仅用于验证构造类型，不执行数据库读写。
	store := db.NewStore(nil, db.DialectSQLite)
	// dependencies、err 分别保存账号工厂和构造错误。
	dependencies, err := NewAccountDependencies(store)
	if err != nil {
		t.Fatalf("NewAccountDependencies: %v", err)
	}
	if dependencies == nil {
		t.Fatal("有效 Store 应返回账号依赖")
	}
	if dependencies.NewAccountLoginRepository() == nil || dependencies.NewAccountSettingsRepository() == nil || dependencies.NewAccountSummaryRepository() == nil {
		t.Fatal("账号工厂应返回账号仓储")
	}
	if dependencies.NewQRLoginRepository() == nil || dependencies.NewAuthenticationRepository() == nil || dependencies.NewAccountLoginAuditRepository() == nil {
		t.Fatal("账号工厂应返回认证与扫码端口")
	}
}

// TestAccountDependenciesNilReceiverGuards 验证账号依赖工厂的 nil 接收者不会伪造可用仓储。
func TestAccountDependenciesNilReceiverGuards(t *testing.T) {
	// dependencies 表示缺少装配状态的账号依赖接收者。
	var dependencies *AccountDependencies
	if dependencies.NewAccountLoginRepository() != nil || dependencies.NewAccountSettingsRepository() != nil || dependencies.NewAccountSummaryRepository() != nil {
		t.Fatal("nil 账号依赖不应创建账号仓储")
	}
	if dependencies.NewQRLoginRepository() != nil || dependencies.NewAuthenticationRepository() != nil || dependencies.NewAccountLoginAuditRepository() != nil {
		t.Fatal("nil 账号依赖不应创建认证仓储")
	}
}
