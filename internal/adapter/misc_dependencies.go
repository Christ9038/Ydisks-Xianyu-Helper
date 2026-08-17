package adapter

import (
	"errors"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// MiscDependencies 封装通知、订单分析和卡券应用服务所需的数据库适配器构造能力。
// 该依赖组只在适配器内部持有 Store，Server 通过领域方法获得窄接口，避免继续扩大通用设施容器的职责。
type MiscDependencies struct {
	// store 保存通知、分析和卡券适配器共享的数据库入口，不向 HTTP 层或应用服务暴露。
	store *db.Store
}

// NewMiscDependencies 构造通知、分析和卡券领域专用依赖，并拒绝缺少数据库入口的半初始化实例。
func NewMiscDependencies(store *db.Store) (*MiscDependencies, error) {
	if store == nil {
		return nil, errors.New("通知、分析和卡券依赖 Store 不能为空")
	}
	return &MiscDependencies{store: store}, nil
}

// NewNotificationUncertainRepository 创建通知不确定状态查询适配器。
func (d *MiscDependencies) NewNotificationUncertainRepository() notificationsapp.Repository {
	if d == nil {
		return nil
	}
	return NewNotificationUncertainRepository(d.store)
}

// NewNotificationChannelRepository 创建通知渠道及账号绑定适配器。
func (d *MiscDependencies) NewNotificationChannelRepository() notificationsapp.ChannelRepository {
	if d == nil {
		return nil
	}
	return NewNotificationChannelRepository(d.store)
}

// NewAnalyticsRepository 创建订单分析只读适配器。
func (d *MiscDependencies) NewAnalyticsRepository() *AnalyticsRepository {
	if d == nil {
		return nil
	}
	return NewAnalyticsRepository(d.store)
}

// NewCardsRepository 创建卡券库存适配器。
func (d *MiscDependencies) NewCardsRepository() *CardsRepository {
	if d == nil {
		return nil
	}
	return NewCardsRepository(d.store)
}
