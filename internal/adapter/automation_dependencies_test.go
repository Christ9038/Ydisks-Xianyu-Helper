package adapter

import (
	"testing"

	"xianyu-go/internal/db"
)

// TestNewAutomationDependenciesRejectsNilStore 确保自动化依赖组不会接受缺失数据库入口的半初始化状态。
func TestNewAutomationDependenciesRejectsNilStore(t *testing.T) {
	// dependencies、err 分别保存构造结果和缺少 Store 时的错误。
	dependencies, err := NewAutomationDependencies(nil)
	if dependencies != nil {
		t.Fatal("缺少 Store 时不应返回自动化依赖")
	}
	if err == nil {
		t.Fatal("缺少 Store 时应返回构造错误")
	}
}

// TestNewAutomationDependenciesCreatesTypedFactory 确保有效 Store 可创建自动化与回复领域的全部窄适配器。
func TestNewAutomationDependenciesCreatesTypedFactory(t *testing.T) {
	// store 仅用于验证构造类型，不执行数据库读写。
	store := db.NewStore(nil, db.DialectSQLite)
	// dependencies、err 分别保存自动化工厂和构造错误。
	dependencies, err := NewAutomationDependencies(store)
	if err != nil {
		t.Fatalf("NewAutomationDependencies: %v", err)
	}
	if dependencies == nil {
		t.Fatal("有效 Store 应返回自动化依赖")
	}
	if dependencies.NewAutomationCredentialWakeRepository() == nil || dependencies.NewAutomationRepository() == nil || dependencies.NewAccountTaskRepository() == nil {
		t.Fatal("自动化工厂应返回自动化仓储")
	}
	if dependencies.NewDefaultReplyRepository() == nil || dependencies.NewKeywordRepository() == nil {
		t.Fatal("自动化工厂应返回回复规则仓储")
	}
}
