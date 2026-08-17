package adapter

import "testing"

// TestNewAdminSettingsDependenciesRejectsNilStore 确保管理员设置依赖不会接受缺少数据库入口的构造请求。
func TestNewAdminSettingsDependenciesRejectsNilStore(t *testing.T) {
	// dependencies 保存缺少 Store 时的管理员设置依赖结果。
	if dependencies := NewAdminSettingsDependencies(nil); dependencies != nil {
		t.Fatal("缺少 Store 时不应返回管理员设置依赖")
	}
}
