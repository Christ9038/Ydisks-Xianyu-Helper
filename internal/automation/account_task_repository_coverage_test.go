package automation

import (
	"context"
	"testing"
)

// TestStoreAccountTaskRepositoryUpdatesExistingCookie 覆盖账号任务仓储的 Cookie 更新委托路径。
func TestStoreAccountTaskRepositoryUpdatesExistingCookie(t *testing.T) {
	// store、cleanup 保存自动化测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制本测试数据库操作的生命周期。
	ctx := context.Background()
	// repository 保存完整 Store 到账号任务窄接口的适配器。
	repository := newStoreAccountTaskRepository(store)
	if repository == nil {
		t.Fatal("valid store should create account task repository")
	}
	// updateErr 保存更新已有账号 Cookie 的数据库错误。
	if updateErr := repository.UpdateValueExisting(ctx, "cid", "unb=1; _m_h5_tk=updated"); updateErr != nil {
		t.Fatal(updateErr)
	}
	// value、valueErr 保存更新后的 Cookie 读取结果和数据库错误。
	value, valueErr := store.Cookies.GetValue(ctx, "cid")
	if valueErr != nil || value != "unb=1; _m_h5_tk=updated" {
		t.Fatalf("cookie value=%q err=%v", value, valueErr)
	}
	// nilRepository 验证缺失数据库依赖时不创建可用仓储。
	nilRepository := newStoreAccountTaskRepository(nil)
	if nilRepository != nil {
		t.Fatal("nil store should not create account task repository")
	}
}
