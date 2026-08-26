package db

import (
	"context"
	"testing"
)

// TestClosedDatabaseCoversCoreRepositoryErrors 覆盖核心数据库仓储在连接关闭后的错误传播。
func TestClosedDatabaseCoversCoreRepositoryErrors(t *testing.T) {
	// store、cleanup 保存随后主动关闭数据库连接的测试存储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// closeErr 保存关闭测试数据库连接的资源释放结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// operations 保存关闭连接后各仓储入口的错误结果。
	operations := []error{
		store.AIReply.ReplacePendingQuote(ctx, AIBargainQuote{CookieID: "cid", BuyerID: "buyer", ItemID: "item", PriceCents: 100}, 1),
		func() error {
			// err 保存聊天买家昵称查询在关闭连接后的错误。
			_, err := store.Chats.BuyerNicknameForAutomation(ctx, "cid", "chat")
			return err
		}(),
		func() error {
			// err 保存消息状态更新在关闭连接后的错误。
			_, err := store.Chats.UpdateMessageStatus(ctx, "cid", "key", "read")
			return err
		}(),
		func() error {
			// err 保存最近出站消息已读查询在关闭连接后的错误。
			_, err := store.Chats.MarkLatestOutgoingRead(ctx, "cid", "chat", 1)
			return err
		}(),
		func() error {
			// err 保存账号设置事务在关闭连接后的错误。
			_, err := store.Cookies.UpdateSettings(ctx, "cid", AccountSettingsUpdate{UserID: 1})
			return err
		}(),
		store.Cards.RestoreBatchData(ctx, 1, "card-content"),
		store.Items.Delete(ctx, "cid", "item"),
		store.ItemReps.Set(ctx, "cid", "item", "reply"),
		store.Keywords.UpdateByID(ctx, KeywordRow{ID: 1, CookieID: "cid", Keyword: "keyword", Reply: "reply"}),
		store.Notifications.SetBindings(ctx, "cid", []int64{1}),
		func() error {
			// err 保存批量取消请求在关闭连接后的错误。
			_, _, err := store.PublishBatches.RequestCancel(ctx, "batch")
			return err
		}(),
		func() error {
			// err 保存过期取消收口事务在关闭连接后的错误。
			_, err := store.PublishBatches.FinalizeExpiredCancellation(ctx, "batch", 1)
			return err
		}(),
		store.EncryptLegacySecrets(ctx),
		Migrate(ctx, store.DB, DialectSQLite),
	}
	// operation 表示当前待验证的关闭连接错误结果。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后核心仓储操作不应成功")
		}
	}
}
