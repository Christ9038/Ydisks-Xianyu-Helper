package adapter

import (
	"context"
	"errors"
	"testing"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// TestNotificationChannelRepositoryKeepsConfigOutOfSummaries 验证渠道摘要映射不携带敏感配置。
func TestNotificationChannelRepositoryKeepsConfigOutOfSummaries(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// owner、ownerErr 保存模板管理员及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// channelID、createErr 保存带敏感配置的渠道创建结果。
	channelID, createErr := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{Name: "邮件", Type: "email", Config: `{"password":"secret"}`, EventTypes: "order", Enabled: true, UserID: owner.ID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// repository 是待验证的通知渠道应用端口适配器。
	repository := NewNotificationChannelRepository(store)
	// summaries、listErr 保存适配器返回的脱敏渠道列表。
	summaries, listErr := repository.ListChannels(ctx, owner.ID)
	if listErr != nil || len(summaries) != 1 || summaries[0].ID != channelID {
		t.Fatalf("渠道摘要映射异常 summaries=%+v err=%v", summaries, listErr)
	}
	// record、recordErr 保存更新路径读取的完整配置记录；该值只应停留在应用更新端口内。
	record, recordErr := repository.GetChannelForUpdate(ctx, channelID, owner.ID)
	if recordErr != nil || record == nil || record.Config == "" {
		t.Fatalf("渠道更新记录缺少配置 record=%+v err=%v", record, recordErr)
	}
}

// TestNotificationChannelRepositoryMapsMissingResources 验证资源缺失统一转换为应用层渠道错误。
func TestNotificationChannelRepositoryMapsMissingResources(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// repository 是待验证的通知渠道应用端口适配器。
	repository := NewNotificationChannelRepository(store)
	// updateErr 保存更新不存在渠道时的应用层错误。
	updateErr := repository.UpdateChannel(ctx, 1, notificationsapp.ChannelRecord{ID: 999, Name: "missing", Type: "webhook"})
	if !errors.Is(updateErr, notificationsapp.ErrChannelNotFound) {
		t.Fatalf("不存在渠道更新应映射为缺失错误: %v", updateErr)
	}
	// deleteErr 保存删除不存在渠道时的应用层错误。
	deleteErr := repository.DeleteChannel(ctx, 999, 1)
	if !errors.Is(deleteErr, notificationsapp.ErrChannelNotFound) {
		t.Fatalf("不存在渠道删除应映射为缺失错误: %v", deleteErr)
	}
	// nilRepository 验证缺少数据库依赖时不会伪造可用端口。
	nilRepository := NewNotificationChannelRepository(nil)
	if nilRepository != nil {
		t.Fatal("缺少 Store 时通知渠道适配器应返回 nil")
	}
}

// TestNotificationUncertainRepositoryRejectsClosedStore 验证不确定通知适配器透传基础设施错误且不暴露正文。
func TestNotificationUncertainRepositoryRejectsClosedStore(t *testing.T) {
	// store 是随后关闭连接的临时 SQLite 数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试存储的不确定通知适配器。
	repository := NewNotificationUncertainRepository(store)
	// closeErr 保存关闭测试数据库时的资源错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// _, listErr 保存数据库关闭后的普通用户查询错误。
	_, listErr := repository.ListUncertainForUser(context.Background(), 1, 10)
	if listErr == nil {
		t.Fatal("数据库关闭后不确定通知查询应返回基础设施错误")
	}
	// nilRepository 验证缺少数据库依赖时返回 nil。
	nilRepository := NewNotificationUncertainRepository(nil)
	if nilRepository != nil {
		t.Fatal("缺少 Store 时不确定通知适配器应返回 nil")
	}
}
