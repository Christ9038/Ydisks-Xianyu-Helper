package adapter

import (
	"context"
	"testing"
)

// TestAutomationCredentialWakeRepositoryRejectsMissingStore 验证缺失数据库时返回明确错误。
func TestAutomationCredentialWakeRepositoryRejectsMissingStore(t *testing.T) {
	// repository 是未装配数据库的自动化凭证唤醒适配器。
	repository := NewAutomationCredentialWakeRepository(nil)
	// err 保存缺失数据库依赖时的适配器错误。
	if err := repository.WakeCredentialBlocked(context.Background(), "acc1"); err == nil {
		t.Fatal("缺少数据库时不应伪装唤醒成功")
	}
}
