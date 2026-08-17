package adapter

import (
	"testing"

	"xianyu-go/internal/db"
)

// TestNewOrderDependenciesRejectsNilStore 确保订单专用依赖不会在缺少数据库入口时构造成功。
func TestNewOrderDependenciesRejectsNilStore(t *testing.T) {
	// dependencies、err 分别保存构造结果和缺少 Store 时的错误。
	dependencies, err := NewOrderDependencies(nil)
	if dependencies != nil {
		t.Fatal("缺少 Store 时不应返回订单依赖")
	}
	if err == nil {
		t.Fatal("缺少 Store 时应返回构造错误")
	}
}

// TestNewOrderDependenciesCreatesTypedFactory 确保有效 Store 能创建订单专用工厂并生成窄仓储。
func TestNewOrderDependenciesCreatesTypedFactory(t *testing.T) {
	// store 使用已初始化的仓储聚合仅验证装配类型，不执行数据库读写。
	store := db.NewStore(nil, db.DialectSQLite)
	// dependencies、err 分别保存订单工厂和构造错误。
	dependencies, err := NewOrderDependencies(store)
	if err != nil {
		t.Fatalf("NewOrderDependencies: %v", err)
	}
	if dependencies == nil {
		t.Fatal("有效 Store 应返回订单依赖")
	}
	if dependencies.NewOrderRepository() == nil {
		t.Fatal("订单工厂应返回订单仓储")
	}
	if dependencies.NewOrderReconciliationRepository() == nil {
		t.Fatal("订单工厂应返回补偿仓储")
	}
	if dependencies.NewOrderRefreshJobRepository() == nil {
		t.Fatal("订单工厂应返回刷新任务仓储")
	}
}
