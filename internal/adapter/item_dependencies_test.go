package adapter

import (
	"testing"

	"xianyu-go/internal/db"
)

// TestNewItemDependenciesRejectsNilStore 确保商品专用依赖不会在缺少数据库入口时构造成功。
func TestNewItemDependenciesRejectsNilStore(t *testing.T) {
	// dependencies、err 分别保存构造结果和缺少 Store 时的错误。
	dependencies, err := NewItemDependencies(nil)
	if dependencies != nil {
		t.Fatal("缺少 Store 时不应返回商品依赖")
	}
	if err == nil {
		t.Fatal("缺少 Store 时应返回构造错误")
	}
}

// TestNewItemDependenciesCreatesTypedFactory 确保有效 Store 可创建商品领域全部窄适配器。
func TestNewItemDependenciesCreatesTypedFactory(t *testing.T) {
	// store 仅用于验证构造类型，不执行数据库读写。
	store := db.NewStore(nil, db.DialectSQLite)
	// dependencies、err 分别保存商品工厂和构造错误。
	dependencies, err := NewItemDependencies(store)
	if err != nil {
		t.Fatalf("NewItemDependencies: %v", err)
	}
	if dependencies == nil {
		t.Fatal("有效 Store 应返回商品依赖")
	}
	if dependencies.NewItemBatchRepository() == nil || dependencies.NewItemBatchPreviewPort() == nil || dependencies.NewItemPublishRepository() == nil || dependencies.NewItemCatalogRepository() == nil {
		t.Fatal("商品工厂应返回本地仓储和预检端口")
	}
}
