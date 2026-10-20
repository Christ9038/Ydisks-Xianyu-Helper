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

// TestResolvePaidTaskOrderFromSimplifiedContext 验证缺订单号的简化付款事件不会按会话猜测订单，原有事实原样保留。
func TestResolvePaidTaskOrderFromSimplifiedContext(t *testing.T) {
	// store、cleanup 提供自动化中心所需的隔离数据库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 控制订单事实回填过程的数据库生命周期。
	ctx := context.Background()
	// writeErr 保存待发货订单种子写入错误。
	if writeErr := store.Orders.Upsert(ctx, "simple-order", db.OrderUpsertOpts{
		CookieID: "cid", ChatID: "simple-chat", BuyerID: "buyer-1", ItemID: "item-1", OrderStatus: "pending_ship",
	}); writeErr != nil {
		t.Fatal(writeErr)
	}
	// center 是只使用本地数据库的自动化中心。
	center := New(store, nil, nil)
	// task 保存只有会话、买家和商品事实的简化付款任务。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, ChatID: "simple-chat", BuyerID: "buyer-1", ItemID: "item-1"}
	// resolved、resolveErr 保存回填后的付款任务及错误。
	resolved, resolveErr := center.resolvePaidTaskOrder(ctx, task)
	if resolveErr != nil || resolved.OrderID != "" || resolved.ItemID != "item-1" {
		t.Fatalf("缺订单号的付款事件不应按会话猜测订单: task=%+v err=%v", resolved, resolveErr)
	}
	// untouched、untouchedErr 验证普通评价任务不会触发付款订单回填。
	untouched, untouchedErr := center.resolvePaidTaskOrder(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, ChatID: "simple-chat"})
	if untouchedErr != nil || untouched.OrderID != "" {
		t.Fatalf("非付款任务不应回填订单: task=%+v err=%v", untouched, untouchedErr)
	}
}

// TestHandleTaskSkipsPaidDeliveryWhenAutoConfirmDisabled 验证参考项目的自动确认开关会在付款自动发货入口处整体阻断执行。
func TestHandleTaskSkipsPaidDeliveryWhenAutoConfirmDisabled(t *testing.T) {
	// store、cleanup 提供自动化中心所需的隔离数据库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 控制付款事件处理过程的数据库生命周期。
	ctx := context.Background()
	// updateErr 保存关闭账号自动确认设置的数据库错误。
	if _, updateErr := store.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id='cid'`); updateErr != nil {
		t.Fatal(updateErr)
	}
	// sender 记录本次被门禁阻断时不应发送的自动发货消息。
	sender := &testSender{}
	// center 是带测试发送器的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// handleErr 保存付款任务经过账号开关门禁后的处理错误。
	handleErr := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "paid-order"})
	if handleErr != nil {
		t.Fatal(handleErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("关闭自动确认时不应发送付款自动发货消息: %v", sender.texts)
	}
}
