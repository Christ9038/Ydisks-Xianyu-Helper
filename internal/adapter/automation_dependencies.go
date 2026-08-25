package adapter

import (
	"errors"

	"xianyu-go/internal/db"
)

// AutomationDependencies 封装自动化、默认回复和关键词用例所需的窄适配器构造能力。
// 它只在 adapter 内部持有 Store，避免 Server 通过通用设施容器隐藏领域依赖。
type AutomationDependencies struct {
	// store 保存自动化相关适配器共享的数据库入口，不向 Server 或应用层暴露。
	store *db.Store
}

// NewAutomationDependencies 构造自动化领域专用依赖，并在缺少数据库入口时立即返回错误。
func NewAutomationDependencies(store *db.Store) (*AutomationDependencies, error) {
	if store == nil {
		return nil, errors.New("自动化依赖 Store 不能为空")
	}
	return &AutomationDependencies{store: store}, nil
}

// NewAutomationCredentialWakeRepository 创建凭证恢复后的自动化任务唤醒适配器。
func (d *AutomationDependencies) NewAutomationCredentialWakeRepository() *AutomationCredentialWakeRepository {
	if d == nil {
		return nil
	}
	return NewAutomationCredentialWakeRepository(d.store)
}

// NewAutomationRepository 创建自动化规则、异常和发布规则共用的数据库适配器。
func (d *AutomationDependencies) NewAutomationRepository() *AutomationRepository {
	if d == nil {
		return nil
	}
	return NewAutomationRepository(d.store)
}

// NewDeliveryTemplateRepository 创建发货模板数据库适配器。
func (d *AutomationDependencies) NewDeliveryTemplateRepository() *DeliveryTemplateRepository {
	if d == nil {
		return nil
	}
	return NewDeliveryTemplateRepository(d.store)
}

// NewAccountTaskRepository 创建账号自动化任务设置与运行记录适配器。
func (d *AutomationDependencies) NewAccountTaskRepository() *AccountTaskRepository {
	if d == nil {
		return nil
	}
	return NewAccountTaskRepository(d.store)
}

// NewDefaultReplyRepository 创建默认回复配置与投递记录适配器。
func (d *AutomationDependencies) NewDefaultReplyRepository() *DefaultReplyRepository {
	if d == nil {
		return nil
	}
	return NewDefaultReplyRepository(d.store)
}

// NewKeywordRepository 创建关键词和指定商品回复适配器。
func (d *AutomationDependencies) NewKeywordRepository() *KeywordRepository {
	if d == nil {
		return nil
	}
	return NewKeywordRepository(d.store)
}
