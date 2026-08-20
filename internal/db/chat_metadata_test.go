package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestChatMetadataStoresRepliesAndNotesByAccount 验证快捷回复上限及买家备注的账号隔离、清除语义。
func TestChatMetadataStoresRepliesAndNotesByAccount(t *testing.T) {
	// store、cleanup 分别保存隔离 SQLite 存储和测试结束后的资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试数据库调用共用的上下文。
	ctx := context.Background()
	// userID 保存创建两个账号所需的本地用户主键。
	var userID int64
	// createUserErr 保存创建测试所有者并读取主键时的数据库错误。
	if createUserErr := store.DB.QueryRowContext(ctx, `INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`, "chat-metadata-owner", "chat-metadata-owner@example.com", "test-hash").Scan(&userID); createUserErr != nil {
		t.Fatalf("创建测试用户失败: %v", createUserErr)
	}
	// accountIDs 保存同一用户下两个需要验证数据隔离的账号标识。
	accountIDs := []string{"chat-metadata-a", "chat-metadata-b"}
	// accountID 表示当前待创建的测试账号。
	for _, accountID := range accountIDs {
		// createAccountErr 保存创建当前测试账号时的数据库错误。
		if createAccountErr := store.Cookies.CreateOwned(ctx, accountID, "test-cookie", userID); createAccountErr != nil {
			t.Fatalf("创建测试账号 %s 失败: %v", accountID, createAccountErr)
		}
	}
	// reply 保存第一个账号创建成功的快捷回复。
	reply, createReplyErr := store.Chats.CreateQuickReply(ctx, accountIDs[0], "您好，现货可拍")
	if createReplyErr != nil || reply.Content != "您好，现货可拍" {
		t.Fatalf("创建快捷回复 reply=%+v err=%v", reply, createReplyErr)
	}
	// firstAccountReplies 和 firstListErr 保存第一个账号的快捷回复列表及读取错误。
	firstAccountReplies, firstListErr := store.Chats.ListQuickReplies(ctx, accountIDs[0])
	if firstListErr != nil || len(firstAccountReplies) != 1 || firstAccountReplies[0].ID != reply.ID {
		t.Fatalf("第一个账号快捷回复 rows=%+v err=%v", firstAccountReplies, firstListErr)
	}
	// secondAccountReplies 和 secondListErr 保存第二个账号的快捷回复列表及读取错误。
	secondAccountReplies, secondListErr := store.Chats.ListQuickReplies(ctx, accountIDs[1])
	if secondListErr != nil || len(secondAccountReplies) != 0 {
		t.Fatalf("第二个账号不应读取到快捷回复 rows=%+v err=%v", secondAccountReplies, secondListErr)
	}
	// deleted 和 deleteErr 保存跨账号删除尝试的实际命中状态及错误。
	deleted, deleteErr := store.Chats.DeleteQuickReply(ctx, accountIDs[1], reply.ID)
	if deleteErr != nil || deleted {
		t.Fatalf("跨账号不应删除快捷回复 deleted=%v err=%v", deleted, deleteErr)
	}
	// index 表示当前正在填充账号上限的额外快捷回复编号。
	for index := 1; index < chatQuickReplyLimit; index++ {
		// limitCreateErr 保存填充快捷回复上限时的单条写入错误。
		if _, limitCreateErr := store.Chats.CreateQuickReply(ctx, accountIDs[0], fmt.Sprintf("快捷回复 %d", index)); limitCreateErr != nil {
			t.Fatalf("创建上限内快捷回复 index=%d err=%v", index, limitCreateErr)
		}
	}
	// overflowErr 保存第 51 条快捷回复被数据库上限拒绝时的错误。
	if _, overflowErr := store.Chats.CreateQuickReply(ctx, accountIDs[0], "超过上限"); !errors.Is(overflowErr, ErrChatQuickReplyLimitReached) {
		t.Fatalf("超过快捷回复上限 error=%v", overflowErr)
	}
	// savedNote 和 saveErr 保存第一个账号写入的买家备注及错误。
	savedNote, saveErr := store.Chats.SaveBuyerNote(ctx, ChatBuyerNote{CookieID: accountIDs[0], BuyerID: "buyer-1", Content: "偏好顺丰"})
	if saveErr != nil || savedNote.Content != "偏好顺丰" || savedNote.UpdatedAt == 0 {
		t.Fatalf("保存买家备注 note=%+v err=%v", savedNote, saveErr)
	}
	// foundNote、found 和 readErr 保存同账号同买家备注的读取结果。
	foundNote, found, readErr := store.Chats.GetBuyerNote(ctx, accountIDs[0], "buyer-1")
	if readErr != nil || !found || foundNote.Content != "偏好顺丰" {
		t.Fatalf("读取买家备注 note=%+v found=%v err=%v", foundNote, found, readErr)
	}
	// isolatedNote、isolated 和 isolatedErr 保存另一账号读取同买家 ID 的隔离结果。
	isolatedNote, isolated, isolatedErr := store.Chats.GetBuyerNote(ctx, accountIDs[1], "buyer-1")
	if isolatedErr != nil || isolated || isolatedNote.Content != "" {
		t.Fatalf("买家备注应按账号隔离 note=%+v found=%v err=%v", isolatedNote, isolated, isolatedErr)
	}
	// clearErr 保存以空正文清除买家备注时的数据库错误。
	if _, clearErr := store.Chats.SaveBuyerNote(ctx, ChatBuyerNote{CookieID: accountIDs[0], BuyerID: "buyer-1"}); clearErr != nil {
		t.Fatalf("清除买家备注失败: %v", clearErr)
	}
	// clearedNote、cleared 和 clearedErr 保存清除备注后的逻辑空结果。
	clearedNote, cleared, clearedErr := store.Chats.GetBuyerNote(ctx, accountIDs[0], "buyer-1")
	if clearedErr != nil || cleared || clearedNote.Content != "" {
		t.Fatalf("清除后备注应不存在 note=%+v found=%v err=%v", clearedNote, cleared, clearedErr)
	}
}
