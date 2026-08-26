package adapter

import (
	"context"
	"testing"

	analyticsapp "xianyu-go/internal/application/analytics"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	settingsapp "xianyu-go/internal/application/settings"
)

// TestDefaultReplyRepositoryCoversClosedDatabaseOperations 验证默认回复适配器各端点传播数据库故障。
func TestDefaultReplyRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的默认回复适配器。
	repository := NewDefaultReplyRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// operations 保存需要统一验证底层错误传播的默认回复操作结果。
	operations := []error{
		func() error {
			// err 表示默认回复归属查询在关闭数据库后的底层错误。
			_, err := repository.CheckOwnership(ctx, 1, "cid")
			return err
		}(),
		func() error {
			// err 表示默认回复读取在关闭数据库后的底层错误。
			_, err := repository.Get(ctx, "cid")
			return err
		}(),
		repository.Upsert(ctx, "cid", defaultreplyapp.Reply{}),
		func() error {
			// err 表示默认回复列表在关闭数据库后的底层错误。
			_, err := repository.ListForUser(ctx, 1)
			return err
		}(),
		repository.Delete(ctx, "cid"),
		repository.ClearRecords(ctx, "cid"),
	}
	// operation 表示当前待验证的默认回复操作错误。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后默认回复操作不应成功")
		}
	}
}

// TestAnalyticsRepositoryCoversClosedDatabaseOperations 验证订单分析适配器各查询端点传播数据库故障。
func TestAnalyticsRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的订单分析适配器。
	repository := NewAnalyticsRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// filter 是覆盖默认订单范围条件的分析查询输入。
	filter := analyticsapp.Filter{}
	// operations 保存需要统一验证底层错误传播的订单分析操作结果。
	operations := []error{
		func() error {
			// err 表示仪表盘统计在关闭数据库后的底层错误。
			_, err := repository.DashboardStats(ctx, 1)
			return err
		}(),
		func() error {
			// err 表示卡券库存统计在关闭数据库后的底层错误。
			_, err := repository.AvailableCardStock(ctx, 1)
			return err
		}(),
		func() error {
			// err 表示收益统计在关闭数据库后的底层错误。
			_, err := repository.QueryRevenue(ctx, filter)
			return err
		}(),
		func() error {
			// err 表示日期统计在关闭数据库后的底层错误。
			_, err := repository.QueryDaily(ctx, filter)
			return err
		}(),
		func() error {
			// err 表示状态统计在关闭数据库后的底层错误。
			_, err := repository.QueryStatus(ctx, filter)
			return err
		}(),
		func() error {
			// err 表示城市统计在关闭数据库后的底层错误。
			_, err := repository.QueryCity(ctx, filter)
			return err
		}(),
		func() error {
			// err 表示商品统计在关闭数据库后的底层错误。
			_, err := repository.QueryItem(ctx, filter)
			return err
		}(),
		func() error {
			// err 表示有效订单数量统计在关闭数据库后的底层错误。
			_, err := repository.CountValidOrders(ctx, filter)
			return err
		}(),
		func() error {
			// err 表示有效订单列表在关闭数据库后的底层错误。
			_, err := repository.ListValidOrders(ctx, filter, 10, 0)
			return err
		}(),
	}
	// operation 表示当前待验证的订单分析操作错误。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后订单分析操作不应成功")
		}
	}
}

// TestSettingsRepositoryCoversClosedDatabaseOperations 验证设置适配器各读写端点传播数据库故障。
func TestSettingsRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的设置适配器。
	repository := NewSettingsRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// operations 保存需要统一验证底层错误传播的设置操作结果。
	operations := []error{
		func() error {
			// err 表示公开系统设置读取在关闭数据库后的底层错误。
			_, err := repository.PublicSystem(ctx)
			return err
		}(),
		func() error {
			// err 表示脱敏系统设置读取在关闭数据库后的底层错误。
			_, err := repository.RedactedSystem(ctx)
			return err
		}(),
		func() error {
			// err 表示单项系统设置读取在关闭数据库后的底层错误。
			_, err := repository.GetSystem(ctx, "key")
			return err
		}(),
		func() error {
			// err 表示敏感系统设置读取在关闭数据库后的底层错误。
			_, err := repository.ReadSensitiveSystem(ctx, 1, "key", "read", "resource")
			return err
		}(),
		repository.ApplySystemChanges(ctx, nil, map[string]settingsapp.SecretChange{"key": {Action: "retain"}}),
		repository.SetSystem(ctx, "key", "value"),
		repository.AddAudit(ctx, settingsapp.AuditRecord{UserID: 1, Action: "read", Resource: "key", Outcome: "accepted"}),
		func() error {
			// err 表示用户设置列表在关闭数据库后的底层错误。
			_, err := repository.ListUser(ctx, 1)
			return err
		}(),
		func() error {
			// err 表示用户设置读取在关闭数据库后的底层错误。
			_, err := repository.GetUser(ctx, 1, "key")
			return err
		}(),
		repository.SetUser(ctx, 1, "key", "value"),
		func() error {
			// err 表示账号归属查询在关闭数据库后的底层错误。
			_, err := repository.CheckOwnership(ctx, 1, "cid")
			return err
		}(),
		func() error {
			// err 表示 AI 设置列表在关闭数据库后的底层错误。
			_, err := repository.ListAIReply(ctx, 1)
			return err
		}(),
		func() error {
			// err 表示 AI 设置读取在关闭数据库后的底层错误。
			_, err := repository.GetAIReply(ctx, 1, "cid")
			return err
		}(),
		repository.UpsertAIReply(ctx, "cid", settingsapp.AIReplySettings{}),
		func() error {
			// err 表示固定改价规则查询在关闭数据库后的底层错误。
			_, err := repository.HasEnabledAdjustPriceRule(ctx, "cid")
			return err
		}(),
	}
	// operation 表示当前待验证的设置操作错误。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后设置操作不应成功")
		}
	}
}
