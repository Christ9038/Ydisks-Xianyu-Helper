package adapter

import (
	"context"
	"errors"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// AutomationCredentialWakeRepository 将自动化任务唤醒 Port 适配为数据库仓储。
type AutomationCredentialWakeRepository struct {
	// store 保存自动化任务数据库仓储入口。
	store *db.Store
}

// NewAutomationCredentialWakeRepository 构造自动化凭证唤醒数据库适配器。
func NewAutomationCredentialWakeRepository(store *db.Store) *AutomationCredentialWakeRepository {
	return &AutomationCredentialWakeRepository{store: store}
}

// WakeCredentialBlocked 唤醒指定账号因凭证失效而暂停的自动化任务。
func (r *AutomationCredentialWakeRepository) WakeCredentialBlocked(ctx context.Context, accountID string) error {
	if r == nil || r.store == nil || r.store.Automation == nil {
		return errors.New("自动化任务存储未初始化")
	}
	return r.store.Automation.WakeCredentialBlocked(ctx, accountID)
}

// 确保数据库适配器覆盖凭证恢复唤醒应用 Port。
var _ automationapp.CredentialWakeRepository = (*AutomationCredentialWakeRepository)(nil)
