package db

import (
	"context"
	"testing"
)

// TestMultiDB_SaveMessageInheritsReadReceipt 为 t 验证平台 PNM 出站消息入库时继承本地临时消息的已读回执，
// 且该流程在 SQLite、MySQL、PostgreSQL 上都不使用目标表自引用的 UPDATE 子查询（MySQL 错误 1093）。
func TestMultiDB_SaveMessageInheritsReadReceipt(t *testing.T) {
	// target 是当前可用的隔离数据库，资源由子测试释放。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言的夹具与断言。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx、store 为当前方言提供同步上下文与仓储。
			ctx, store := context.Background(), target.store
			// _、cookieID 只需要合法账号归属，不涉及凭据。
			_, cookieID := seedAccount(t, store)
			// session 是两条消息共用的虚构会话。
			session := ChatSession{CookieID: cookieID, ChatID: "chat-1", BuyerID: "buyer-1"}
			// local 是本程序发送后已被买家读取的临时出站消息。
			local := ChatMessage{MessageKey: "local-1", Direction: "outgoing", MessageType: "text", Content: "你好",
				Status: "sent", ReadStatus: 2, ReadAt: 123456, SentAt: 1_700_000_000_000, ObservedAt: 1}
			// err 保存本地临时消息的落库错误。
			if _, _, err := store.Chats.SaveMessage(ctx, session, local, false); err != nil {
				t.Fatalf("保存本地消息: %v", err)
			}
			// platform 是同内容、时间相近的平台回显消息。
			platform := ChatMessage{MessageKey: "abc.PNM", Direction: "outgoing", MessageType: "text", Content: "你好",
				Status: "sent", SentAt: 1_700_000_003_000, ObservedAt: 1}
			// stored、inserted、err 保存平台消息落库结果。
			stored, inserted, err := store.Chats.SaveMessage(ctx, session, platform, false)
			if err != nil || !inserted {
				t.Fatalf("保存平台消息 inserted=%v err=%v", inserted, err)
			}
			if stored.ReadStatus != 2 || stored.ReadAt != 123456 {
				t.Fatalf("未继承已读回执: status=%d read_at=%d", stored.ReadStatus, stored.ReadAt)
			}
		})
	}
}
