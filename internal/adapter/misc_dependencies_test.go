package adapter

import (
	"testing"

	"xianyu-go/internal/db"
)

// TestNewMiscDependenciesRejectsNilStore 确保通知、分析和卡券依赖不会以缺失数据库入口的状态构造。
func TestNewMiscDependenciesRejectsNilStore(t *testing.T) {
	// dependencies、err 分别保存构造结果和缺少 Store 时的错误。
	dependencies, err := NewMiscDependencies(nil)
	if dependencies != nil {
		t.Fatal("缺少 Store 时不应返回杂项依赖")
	}
	if err == nil {
		t.Fatal("缺少 Store 时应返回构造错误")
	}
}

// TestNewMiscDependenciesCreatesTypedFactories 确保有效 Store 能创建三个领域的窄适配器。
func TestNewMiscDependenciesCreatesTypedFactories(t *testing.T) {
	// store 仅用于验证工厂构造，不执行数据库读写。
	store := db.NewStore(nil, db.DialectSQLite)
	// dependencies、err 分别保存杂项领域工厂和构造错误。
	dependencies, err := NewMiscDependencies(store)
	if err != nil {
		t.Fatalf("NewMiscDependencies: %v", err)
	}
	if dependencies == nil {
		t.Fatal("有效 Store 应返回杂项领域依赖")
	}
	if dependencies.NewNotificationUncertainRepository() == nil || dependencies.NewNotificationChannelRepository() == nil {
		t.Fatal("通知工厂应返回两个通知应用仓储")
	}
	if dependencies.NewAnalyticsRepository() == nil {
		t.Fatal("分析工厂应返回分析仓储")
	}
	if dependencies.NewCardsRepository() == nil {
		t.Fatal("卡券工厂应返回卡券仓储")
	}
}

// TestMiscDependenciesNilReceiverReturnsNil 确保测试或关闭流程中的空依赖不会伪造适配器。
func TestMiscDependenciesNilReceiverReturnsNil(t *testing.T) {
	// dependencies 表示未装配的杂项依赖接收者。
	var dependencies *MiscDependencies
	if dependencies.NewNotificationUncertainRepository() != nil || dependencies.NewNotificationChannelRepository() != nil || dependencies.NewAnalyticsRepository() != nil || dependencies.NewCardsRepository() != nil {
		t.Fatal("空杂项依赖不应创建任何适配器")
	}
}
