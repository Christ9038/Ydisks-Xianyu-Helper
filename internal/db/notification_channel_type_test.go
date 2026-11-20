package db

import (
	"context"
	"testing"
)

// TestNotificationChannelsAllowFeishuAppType 验证迁移后可以保存扫码创建的飞书应用渠道，避免 CHECK 约束阻止扫码绑定。
func TestNotificationChannelsAllowFeishuAppType(t *testing.T) {
	// store、cleanup 是使用临时 SQLite 数据库的测试存储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存当前数据库操作上下文。
	ctx := context.Background()
	// userID、accountID 是种子账号所属用户与账号标识，用于满足渠道外键。
	userID, _ := seedAccount(t, store)
	// row 是待写入的飞书应用渠道；Config 只有测试占位值，不包含真实密钥。
	row := NotificationChannelRow{
		Name:       "飞书应用测试",
		Type:       "feishu_app",
		Config:     `{"app_id":"cli_test","app_secret":"secret_test","open_id":"ou_test","domain":"feishu"}`,
		EventTypes: `["chat_message_received"]`,
		Enabled:    true,
		UserID:     userID,
	}
	// channelID、createErr 保存渠道创建结果与数据库错误。
	channelID, createErr := store.Notifications.CreateChannel(ctx, &row)
	if createErr != nil || channelID <= 0 {
		t.Fatalf("创建 feishu_app 渠道: id=%d err=%v", channelID, createErr)
	}
}
