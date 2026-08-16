package adapter

import (
	"context"
	"testing"
)

// TestAccountTaskRepositoryRejectsMissingStore 验证账号任务数据库适配器不会因缺少 Store 而 panic。
func TestAccountTaskRepositoryRejectsMissingStore(t *testing.T) {
	// repository 是未装配数据库的账号任务适配器。
	repository := NewAccountTaskRepository(nil)
	// _, err 保存缺少数据库时的明确错误。
	if _, err := repository.GetSettings(context.Background(), "account-1"); err == nil {
		t.Fatal("缺少 Store 时应返回错误")
	}
}
