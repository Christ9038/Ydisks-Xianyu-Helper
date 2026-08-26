package automation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"xianyu-go/internal/db"
)

// TestSendTemplateHandlesEmptyAndUnavailableActions 验证模板动作的规格门禁、空消息和卡券可用性校验。
func TestSendTemplateHandlesEmptyAndUnavailableActions(t *testing.T) {
	// store、cleanup 保存模板动作测试使用的数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// sender 是不会产生外部副作用的模拟发送器。
	sender := &testSender{}
	// executor 是绑定测试存储和模拟发送器的模板执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// task 是携带规格信息的订单任务。
	task := Task{AccountID: "cid", SpecName: "套餐", SpecValue: "标准"}
	// mismatchResult、mismatchErr 保存规格不匹配时的执行结果。
	mismatchResult, mismatchErr := executor.sendTemplate(ctx, task, db.AutomationAction{ConfigJSON: `{"spec_name":"套餐","spec_value":"高级"}`, TemplateMessages: []string{"不会发送"}})
	if mismatchErr != nil || mismatchResult.sent != 0 || len(sender.texts) != 0 {
		t.Fatalf("规格不匹配不应发送：result=%+v err=%v texts=%v", mismatchResult, mismatchErr, sender.texts)
	}
	// emptyResult、emptyErr 保存缺少模板消息时的执行结果。
	emptyResult, emptyErr := executor.sendTemplate(ctx, task, db.AutomationAction{ConfigJSON: "{}"})
	if emptyErr == nil || emptyResult.sent != 0 {
		t.Fatalf("缺少模板消息应失败：result=%+v err=%v", emptyResult, emptyErr)
	}
	// blankResult、blankErr 保存模板消息全部为空白时的执行结果。
	blankResult, blankErr := executor.sendTemplate(ctx, task, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{" ", "\n"}})
	if blankErr != nil || blankResult.sent != 0 {
		t.Fatalf("空白模板消息应跳过：result=%+v err=%v", blankResult, blankErr)
	}
	// admin、adminErr 保存创建测试卡券所需的管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("GetByUsername error: %v", adminErr)
	}
	// disabledID、disabledErr 保存禁用卡券组的创建结果。
	disabledID, disabledErr := store.Cards.Create(ctx, &db.CardFull{Name: "disabled", Type: "text", TextContent: "disabled", UserID: admin.ID})
	if disabledErr != nil {
		t.Fatalf("create disabled card error: %v", disabledErr)
	}
	// disabledResult、disabledRunErr 保存禁用卡券绑定的执行结果。
	disabledResult, disabledRunErr := executor.sendTemplate(ctx, task, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{"x"}, TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "code", CardID: disabledID}}})
	if disabledRunErr == nil || disabledResult.sent != 0 {
		t.Fatalf("禁用卡券绑定应失败：result=%+v err=%v", disabledResult, disabledRunErr)
	}
	// wrongTypeID、wrongTypeErr 保存不支持模板发货类型的卡券组创建结果。
	wrongTypeID, wrongTypeErr := store.Cards.Create(ctx, &db.CardFull{Name: "wrong-type", Type: "api", APIConfig: "{}", Enabled: true, UserID: admin.ID})
	if wrongTypeErr != nil {
		t.Fatalf("create wrong type card error: %v", wrongTypeErr)
	}
	// wrongTypeResult、wrongTypeRunErr 保存不支持类型卡券绑定的执行结果。
	wrongTypeResult, wrongTypeRunErr := executor.sendTemplate(ctx, task, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{"x"}, TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "code", CardID: wrongTypeID}}})
	if wrongTypeRunErr == nil || wrongTypeResult.sent != 0 {
		t.Fatalf("不支持类型卡券绑定应失败：result=%+v err=%v", wrongTypeResult, wrongTypeRunErr)
	}
	// missingResult、missingErr 保存不存在卡券绑定的执行结果。
	missingResult, missingErr := executor.sendTemplate(ctx, task, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{"x"}, TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "code", CardID: 999999}}})
	if missingErr == nil || missingResult.sent != 0 {
		t.Fatalf("不存在卡券绑定应失败：result=%+v err=%v", missingResult, missingErr)
	}
}

// TestSendTemplateRestoresReservedBatchData 验证批量卡密消费失败或首条消息确定未发送时会恢复库存。
func TestSendTemplateRestoresReservedBatchData(t *testing.T) {
	// store、cleanup 保存批量库存测试使用的数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// admin、adminErr 保存创建批量卡券所需的管理员用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("GetByUsername error: %v", adminErr)
	}
	// cardID、cardErr 保存只有一条库存数据的批量卡券组。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "batch-template", Type: "data", DataContent: "唯一卡密", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatalf("create batch card error: %v", cardErr)
	}
	// consumeExecutor 是用于覆盖第二次消费失败并自动回滚的执行器。
	consumeExecutor := automationActionExecutor{store: store, senders: testSenderProvider{sender: &testSender{}}}
	// consumeResult、consumeErr 保存请求两份但库存只有一份时的执行结果。
	consumeResult, consumeErr := consumeExecutor.sendTemplate(ctx, Task{AccountID: "cid", Quantity: "1"}, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{"{{cards.code}}"}, TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "code", CardID: cardID, DeliveryCount: 2}}})
	if consumeErr == nil || consumeResult.sent != 0 {
		t.Fatalf("库存不足应失败：result=%+v err=%v", consumeResult, consumeErr)
	}
	// restoredAfterConsume、restoreConsumeErr 保存消费失败回滚后重新取出的卡密。
	restoredAfterConsume, restoreConsumeErr := store.Cards.ConsumeBatchData(ctx, cardID)
	if restoreConsumeErr != nil || restoredAfterConsume != "唯一卡密" {
		t.Fatalf("消费失败后库存未恢复：content=%q err=%v", restoredAfterConsume, restoreConsumeErr)
	}
	// restoreID、restoreIDErr 保存第二组批量卡券的创建结果。
	restoreID, restoreIDErr := store.Cards.Create(ctx, &db.CardFull{Name: "batch-send", Type: "data", DataContent: "待恢复", Enabled: true, UserID: admin.ID})
	if restoreIDErr != nil {
		t.Fatalf("create restore card error: %v", restoreIDErr)
	}
	// notSentExecutor 是首条消息确定未发送时使用的执行器。
	notSentExecutor := automationActionExecutor{store: store, senders: testSenderProvider{sender: &testSender{err: fmt.Errorf("%w: offline", ErrMessageNotSent)}}}
	// notSentResult、notSentErr 保存首条消息确定未发送的执行结果。
	notSentResult, notSentErr := notSentExecutor.sendTemplate(ctx, Task{AccountID: "cid"}, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{"{{cards.code}}"}, TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "code", CardID: restoreID}}})
	if !errors.Is(notSentErr, ErrMessageNotSent) || notSentResult.sent != 0 {
		t.Fatalf("首条消息未发送错误未保留：result=%+v err=%v", notSentResult, notSentErr)
	}
	// restoredAfterSend、restoreSendErr 保存首条消息未发送后的库存内容。
	restoredAfterSend, restoreSendErr := store.Cards.ConsumeBatchData(ctx, restoreID)
	if restoreSendErr != nil || restoredAfterSend != "待恢复" {
		t.Fatalf("首条消息未发送后库存未恢复：content=%q err=%v", restoredAfterSend, restoreSendErr)
	}
}

// TestSendTemplateClassifiesPartialSendFailure 验证模板前序消息成功而后续消息失败时返回结果未知。
func TestSendTemplateClassifiesPartialSendFailure(t *testing.T) {
	// store、cleanup 保存部分发送测试使用的数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// sender 保存第一条成功、第二条失败的模拟发送器。
	sender := &testSender{err: errors.New("网络连接中断"), failAfter: 1}
	// executor 是绑定失败发送器的模板执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// result、runErr 保存部分发送的统计、凭证和错误。
	result, runErr := executor.sendTemplate(ctx, Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}, db.AutomationAction{ConfigJSON: "{}", TemplateMessages: []string{"第一条", "第二条"}})
	// uncertain 保存用于表示远端发送结果未知的错误包装。
	var uncertain *uncertainActionError
	if result.sent != 1 || len(sender.texts) != 1 || !errors.As(runErr, &uncertain) || !errors.Is(runErr, sender.err) || result.proof.tradeText != "第一条" {
		t.Fatalf("部分发送分类错误：result=%+v texts=%v err=%v", result, sender.texts, runErr)
	}
}
