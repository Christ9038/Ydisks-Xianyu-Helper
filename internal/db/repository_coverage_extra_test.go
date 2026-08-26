package db

import (
	"context"
	"errors"
	"testing"
)

// TestKeywordAndItemRepositoryBranches 覆盖关键字替换/删除和商品详情读取路径。
func TestKeywordAndItemRepositoryBranches(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存测试账号的归属信息。
	userID, cookieID := seedAccount(t, store)
	// firstKeywordID、firstErr 保存首条关键字创建结果和数据库错误。
	firstKeywordID, firstErr := store.Keywords.Add(ctx, cookieID, "旧关键词", "旧回复", "item-1", "", "")
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	// secondKeywordID、secondErr 保存第二条关键字创建结果和数据库错误。
	secondKeywordID, secondErr := store.Keywords.Add(ctx, cookieID, "图片关键词", "图片回复", "", "image", "https://example.invalid/image.png")
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	// updateErr 保存关键字更新结果。
	if updateErr := store.Keywords.UpdateByID(ctx, KeywordRow{ID: firstKeywordID, CookieID: cookieID, Keyword: "新关键词", Reply: "新回复", ItemID: "item-2", Type: "text"}); updateErr != nil {
		t.Fatal(updateErr)
	}
	// rows、rowsErr 保存关键字读取结果及数据库错误。
	rows, rowsErr := store.Keywords.AllRows(ctx, cookieID)
	if rowsErr != nil || len(rows) != 2 || rows[0].Keyword != "新关键词" {
		t.Fatalf("keyword rows=%#v err=%v", rows, rowsErr)
	}
	// replaceErr 保存覆盖账号关键字的事务结果。
	if replaceErr := store.Keywords.ReplaceForCookie(ctx, cookieID, []KeywordRow{{CookieID: cookieID, Keyword: "覆盖关键词", Reply: "覆盖回复"}}); replaceErr != nil {
		t.Fatal(replaceErr)
	}
	// replacedRows、replacedErr 保存覆盖后的关键字列表及数据库错误。
	replacedRows, replacedErr := store.Keywords.AllRows(ctx, cookieID)
	if replacedErr != nil || len(replacedRows) != 1 || replacedRows[0].Keyword != "覆盖关键词" || replacedRows[0].Type != "text" {
		t.Fatalf("replaced keyword rows=%#v err=%v", replacedRows, replacedErr)
	}
	// deleteMissingErr 保存删除不存在关键字时的业务错误。
	deleteMissingErr := store.Keywords.DeleteByID(ctx, cookieID, secondKeywordID)
	if !errors.Is(deleteMissingErr, ErrNotFound) {
		t.Fatalf("missing keyword delete error=%v", deleteMissingErr)
	}
	// deleteErr 保存删除当前账号关键字时的数据库错误。
	if deleteErr := store.Keywords.DeleteByID(ctx, cookieID, replacedRows[0].ID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// itemErr 保存商品详情写入结果。
	if itemErr := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: cookieID, ItemID: "item-detail", ItemTitle: "详情商品", ItemDescription: "描述", IsMultiSpec: true, MultiQuantityDelivery: true}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// item、itemReadErr 保存商品详情读取结果及数据库错误。
	item, itemReadErr := store.Items.GetByCookieItem(ctx, cookieID, "item-detail")
	if itemReadErr != nil || item.ItemTitle != "详情商品" || !item.IsMultiSpec || !item.MultiQuantityDelivery {
		t.Fatalf("item=%#v err=%v", item, itemReadErr)
	}
	// missingItem、missingItemErr 保存不存在商品读取结果及错误。
	missingItem, missingItemErr := store.Items.GetByCookieItem(ctx, cookieID, "missing-item")
	if !errors.Is(missingItemErr, ErrNotFound) || missingItem.ItemID != "" {
		t.Fatalf("missing item=%#v err=%v", missingItem, missingItemErr)
	}
	// cardID、cardErr 保存卡券创建结果及数据库错误。
	cardID, cardErr := store.Cards.Create(ctx, &CardFull{Name: "归属卡券", Type: "text", TextContent: "内容", Enabled: true, UserID: userID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ownsCard、ownsCardErr 保存卡券归属检查结果及数据库错误。
	ownsCard, ownsCardErr := store.Cards.ExistsOwned(ctx, cardID, userID)
	if ownsCardErr != nil || !ownsCard {
		t.Fatalf("owned card: owns=%v err=%v", ownsCard, ownsCardErr)
	}
	// foreignCard、foreignCardErr 保存跨用户卡券归属检查结果及数据库错误。
	foreignCard, foreignCardErr := store.Cards.ExistsOwned(ctx, cardID, userID+1)
	if foreignCardErr != nil || foreignCard {
		t.Fatalf("foreign card: owns=%v err=%v", foreignCard, foreignCardErr)
	}
	// dataCardID、dataCardErr 保存批量卡密卡券创建结果及数据库错误。
	dataCardID, dataCardErr := store.Cards.Create(ctx, &CardFull{Name: "批量卡券", Type: "data", DataContent: "第一条\n第二条", Enabled: true, UserID: userID})
	if dataCardErr != nil {
		t.Fatal(dataCardErr)
	}
	// firstContent、snapshot、firstErr 保存首条卡密内容、库存快照和读取错误。
	firstContent, snapshot, firstErr := store.Cards.FirstBatchData(ctx, dataCardID)
	if firstErr != nil || firstContent != "第一条" || snapshot != "第一条\n第二条" {
		t.Fatalf("first batch data: content=%q snapshot=%q err=%v", firstContent, snapshot, firstErr)
	}
	// commitErr 保存按快照提交首条卡密的结果。
	if commitErr := store.Cards.CommitFirstBatchData(ctx, dataCardID, snapshot); commitErr != nil {
		t.Fatal(commitErr)
	}
	// remainingContent、remainingErr 保存提交后剩余卡密及数据库错误。
	remainingContent, _, remainingErr := store.Cards.FirstBatchData(ctx, dataCardID)
	if remainingErr != nil || remainingContent != "第二条" {
		t.Fatalf("remaining batch data: content=%q err=%v", remainingContent, remainingErr)
	}
}

// TestNotificationChannelUserScopedBranches 覆盖通知渠道按用户读取、更新和删除的权限分支。
func TestNotificationChannelUserScopedBranches(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存当前用户及账号标识；cookieID 用于完成账号外键准备。
	userID, cookieID := seedAccount(t, store)
	_ = cookieID
	// channelID、createErr 保存通知渠道创建结果及数据库错误。
	channelID, createErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "用户渠道", Type: "webhook", Config: `{}`, EventTypes: "order", Enabled: true, UserID: userID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// channel、getErr 保存用户范围渠道读取结果及数据库错误。
	channel, getErr := store.Notifications.GetChannelRowForUser(ctx, channelID, userID)
	if getErr != nil || channel == nil || channel.Name != "用户渠道" || channel.Config != `{}` {
		t.Fatalf("channel=%#v err=%v", channel, getErr)
	}
	// foreignChannel、foreignErr 保存错误用户读取结果及数据库错误。
	foreignChannel, foreignErr := store.Notifications.GetChannelRowForUser(ctx, channelID, userID+1)
	if foreignErr != nil || foreignChannel != nil {
		t.Fatalf("foreign channel=%#v err=%v", foreignChannel, foreignErr)
	}
	// updateErr 保存错误用户更新渠道时的归属错误。
	updateErr := store.Notifications.UpdateChannelForUser(ctx, &NotificationChannelRow{ID: channelID, Name: "越权", Type: "webhook", Config: `{}`, Enabled: true}, userID+1)
	if !errors.Is(updateErr, ErrNotFound) {
		t.Fatalf("foreign update error=%v", updateErr)
	}
	// ownUpdateErr 保存当前用户更新渠道的数据库错误。
	if ownUpdateErr := store.Notifications.UpdateChannelForUser(ctx, &NotificationChannelRow{ID: channelID, Name: "已更新", Type: "webhook", Config: `{"changed":true}`, Enabled: false}, userID); ownUpdateErr != nil {
		t.Fatal(ownUpdateErr)
	}
	// updatedChannel、updatedErr 保存更新后渠道读取结果及数据库错误。
	updatedChannel, updatedErr := store.Notifications.GetChannelRowForUser(ctx, channelID, userID)
	if updatedErr != nil || updatedChannel == nil || updatedChannel.Name != "已更新" || updatedChannel.Enabled {
		t.Fatalf("updated channel=%#v err=%v", updatedChannel, updatedErr)
	}
	// foreignDeleteErr 保存错误用户删除渠道时的归属错误。
	foreignDeleteErr := store.Notifications.DeleteChannelForUser(ctx, channelID, userID+1)
	if !errors.Is(foreignDeleteErr, ErrNotFound) {
		t.Fatalf("foreign delete error=%v", foreignDeleteErr)
	}
	// ownDeleteErr 保存当前用户删除渠道的数据库错误。
	if ownDeleteErr := store.Notifications.DeleteChannelForUser(ctx, channelID, userID); ownDeleteErr != nil {
		t.Fatal(ownDeleteErr)
	}
	// deletedChannel、deletedErr 保存删除后渠道读取结果及数据库错误。
	deletedChannel, deletedErr := store.Notifications.GetChannelRowForUser(ctx, channelID, userID)
	if deletedErr != nil || deletedChannel != nil {
		t.Fatalf("deleted channel=%#v err=%v", deletedChannel, deletedErr)
	}
}

// TestChatReconciliationAndSoftDeleteBranches 覆盖空会话清理、订单补偿重试和订单逻辑删除路径。
func TestChatReconciliationAndSoftDeleteBranches(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// cookieID 保存清理目标账号标识。
	_, cookieID := seedAccount(t, store)
	// emptyErr 保存空会话写入结果。
	if emptyErr := store.Chats.UpsertSession(ctx, ChatSession{CookieID: cookieID, ChatID: "empty", LastMessage: "暂无消息"}); emptyErr != nil {
		t.Fatal(emptyErr)
	}
	// fullErr 保存有消息会话写入结果。
	if fullErr := store.Chats.UpsertSession(ctx, ChatSession{CookieID: cookieID, ChatID: "full", LastMessage: "有消息", LastMessageAt: 1}); fullErr != nil {
		t.Fatal(fullErr)
	}
	// cleanupErr 保存空会话清理结果。
	if cleanupErr := store.Chats.DeleteEmptySessions(ctx, cookieID); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	// emptyCount、fullCount 保存清理后两个会话的数量。
	var emptyCount, fullCount int
	// countErr 保存清理后会话计数查询错误。
	countErr := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_sessions WHERE cookie_id=? AND chat_id='empty'", cookieID).Scan(&emptyCount)
	if countErr != nil {
		t.Fatal(countErr)
	}
	// fullCountErr 保存有消息会话计数查询错误。
	fullCountErr := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_sessions WHERE cookie_id=? AND chat_id='full'", cookieID).Scan(&fullCount)
	if fullCountErr != nil || emptyCount != 0 || fullCount != 1 {
		t.Fatalf("session counts: empty=%d full=%d err=%v", emptyCount, fullCount, fullCountErr)
	}
	// reconciliationID、createErr 保存待补偿记录创建结果和数据库错误。
	reconciliationID, createErr := store.Reconciliations.CreatePending(ctx, "order-retry", cookieID, "manual_status_ship", "first failure")
	if createErr != nil {
		t.Fatal(createErr)
	}
	// recordErr 保存补偿失败重试记录结果。
	if recordErr := store.Reconciliations.RecordAttempt(ctx, reconciliationID, "second failure"); recordErr != nil {
		t.Fatal(recordErr)
	}
	// attempts、message 保存补偿记录更新后的尝试次数和错误说明。
	var attempts int
	// message 保存补偿记录最近一次失败说明。
	var message string
	// queryErr 保存补偿记录读取错误。
	if queryErr := store.DB.QueryRowContext(ctx, "SELECT attempts,error_message FROM order_reconciliations WHERE id=?", reconciliationID).Scan(&attempts, &message); queryErr != nil {
		t.Fatal(queryErr)
	}
	if attempts != 1 || message != "second failure" {
		t.Fatalf("reconciliation attempts=%d message=%q", attempts, message)
	}
	// orderErr 保存订单创建结果。
	if orderErr := store.Orders.Upsert(ctx, "soft-delete-order", OrderUpsertOpts{CookieID: cookieID, OrderStatus: "paid"}); orderErr != nil {
		t.Fatal(orderErr)
	}
	// deleted、deleteErr 保存订单首次逻辑删除结果和数据库错误。
	deleted, deleteErr := store.Orders.SoftDelete(ctx, "soft-delete-order")
	if deleteErr != nil || !deleted {
		t.Fatalf("first soft delete: deleted=%v err=%v", deleted, deleteErr)
	}
	// deletedAgain、deleteAgainErr 保存重复逻辑删除结果和数据库错误。
	deletedAgain, deleteAgainErr := store.Orders.SoftDelete(ctx, "soft-delete-order")
	if deleteAgainErr != nil || deletedAgain {
		t.Fatalf("second soft delete: deleted=%v err=%v", deletedAgain, deleteAgainErr)
	}
}

// TestOrderWriteUnitOfWorkAdditionalMethods 覆盖事务内订单补丁和批量订单写入方法。
func TestOrderWriteUnitOfWorkAdditionalMethods(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存订单写入事务所需的账号归属。
	_, cookieID := seedAccount(t, store)
	// status 保存事务内要写入的订单状态。
	status := "shipped"
	// transactionErr 保存订单补丁和批量写入事务结果。
	transactionErr := store.OrderWrites.WithTransaction(ctx, func(writer *OrderWriteTransaction) error {
		// orderErr 保存事务内初始订单写入错误。
		if orderErr := writer.UpsertOrder(ctx, "uow-patch", OrderUpsertOpts{CookieID: cookieID, OrderStatus: "paid"}); orderErr != nil {
			return orderErr
		}
		// patchErr 保存事务内订单状态补丁错误。
		if patchErr := writer.PatchOrder(ctx, "uow-patch", OrderPatch{OrderStatus: &status}); patchErr != nil {
			return patchErr
		}
		// batchErr 保存事务内多值订单写入错误。
		return writer.UpsertOrders(ctx, []BatchOrderUpsert{{OrderID: "uow-batch", Options: OrderUpsertOpts{CookieID: cookieID, ItemID: "batch-item", OrderStatus: "pending_ship", Amount: "3.20"}}})
	})
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	// patchedOrder、patchedErr 保存事务提交后的订单补丁结果及数据库错误。
	patchedOrder, patchedErr := store.Orders.Get(ctx, "uow-patch")
	if patchedErr != nil || patchedOrder.OrderStatus != "shipped" {
		t.Fatalf("patched order=%#v err=%v", patchedOrder, patchedErr)
	}
	// batchOrder、batchErr 保存事务提交后的批量订单结果及数据库错误。
	batchOrder, batchErr := store.Orders.Get(ctx, "uow-batch")
	if batchErr != nil || batchOrder.OrderStatus != "pending_ship" || batchOrder.Amount != "3.20" {
		t.Fatalf("batch order=%#v err=%v", batchOrder, batchErr)
	}
}
