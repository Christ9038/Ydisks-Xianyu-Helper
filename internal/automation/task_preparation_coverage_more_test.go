package automation

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestPrepareBuyerNicknameBoundaries 覆盖买家昵称已知、会话缺失和数据库错误边界。
func TestPrepareBuyerNicknameBoundaries(t *testing.T) {
	// store、cleanup 提供自动化中心所需的隔离数据库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 控制昵称准备过程的数据库生命周期。
	ctx := context.Background()
	// center 是只使用本地数据库的自动化中心。
	center := New(store, nil, nil)
	// knownTask 保存已具备昵称的任务，调用不应访问聊天存储。
	knownTask := Task{AccountID: "cid", ChatID: "chat", BuyerNickname: "已知买家"}
	// knownResult、knownErr 保存已知昵称任务的原样结果和错误。
	knownResult, knownErr := center.prepareBuyerNickname(ctx, knownTask)
	if knownErr != nil || knownResult.BuyerNickname != "已知买家" {
		t.Fatalf("known nickname task=%+v err=%v", knownResult, knownErr)
	}
	// emptyChatTask 保存没有会话标识的任务，昵称应保持空值。
	emptyChatTask, emptyChatErr := center.prepareBuyerNickname(ctx, Task{AccountID: "cid"})
	if emptyChatErr != nil || emptyChatTask.BuyerNickname != "" {
		t.Fatalf("empty chat task=%+v err=%v", emptyChatTask, emptyChatErr)
	}
	// upsertErr 保存会话昵称写入错误。
	upsertErr := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "cid", ChatID: "chat-nickname", BuyerID: "buyer", BuyerName: "买家甲"})
	if upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// loadedTask、loadedErr 保存从聊天会话补齐昵称后的任务。
	loadedTask, loadedErr := center.prepareBuyerNickname(ctx, Task{AccountID: "cid", ChatID: "chat-nickname"})
	if loadedErr != nil || loadedTask.BuyerNickname != "买家甲" {
		t.Fatalf("loaded nickname task=%+v err=%v", loadedTask, loadedErr)
	}
	// closedDB、closedCleanup 提供一个确定返回数据库错误的中心。
	closedDB, closedCleanup := newAutomationTestStore(t)
	// closedCenter 使用已关闭数据库验证昵称查询错误包装。
	closedCenter := New(closedDB, nil, nil)
	// err 保存关闭测试数据库连接时的错误。
	if err := closedDB.DB.Close(); err != nil {
		t.Fatal(err)
	}
	// _, readErr 接收聊天数据库关闭后的昵称查询错误。
	_, readErr := closedCenter.prepareBuyerNickname(ctx, Task{AccountID: "cid", ChatID: "chat"})
	if readErr == nil || !strings.Contains(readErr.Error(), "读取买家昵称") {
		t.Fatalf("closed database error=%v", readErr)
	}
	closedCleanup()
}
