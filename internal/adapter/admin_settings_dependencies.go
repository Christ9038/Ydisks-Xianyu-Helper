package adapter

import (
	adminapp "xianyu-go/internal/application/admin"
	"xianyu-go/internal/db"
)

// AdminSettingsDependencies 封装管理员和系统设置应用服务所需的数据库适配器。
type AdminSettingsDependencies struct {
	// store 保存管理员与设置适配器共享的数据库入口，仅在 adapter 内部使用。
	store *db.Store
}

// NewAdminSettingsDependencies 从数据库 Store 构造管理员设置专用依赖组。
func NewAdminSettingsDependencies(store *db.Store) *AdminSettingsDependencies {
	if store == nil {
		return nil
	}
	return &AdminSettingsDependencies{store: store}
}

// NewAdminRepository 创建管理员用户与统计适配器。
func (d *AdminSettingsDependencies) NewAdminRepository() adminapp.Repository {
	if d == nil {
		return nil
	}
	return NewAdminRepository(d.store)
}

// NewSettingsRepository 创建系统设置适配器。
func (d *AdminSettingsDependencies) NewSettingsRepository() *SettingsRepository {
	if d == nil {
		return nil
	}
	return NewSettingsRepository(d.store)
}
