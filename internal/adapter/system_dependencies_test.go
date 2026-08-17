package adapter

import "testing"

// TestNewSystemDependenciesRejectsNilStore 确保系统级适配器不会在缺少数据库入口时构造成功。
func TestNewSystemDependenciesRejectsNilStore(t *testing.T) {
	// dependencies 保存缺少 Store 时的系统依赖构造结果。
	dependencies := NewSystemDependencies(nil)
	if dependencies != nil {
		t.Fatal("缺少 Store 时不应返回系统依赖")
	}
}
