package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"xianyu-go/internal/deliverytemplate"
)

// TestDeliveryImagesPersistence 验证混合模板、卡券本地图、旧客户端保护、用户隔离及动作完整快照。
func TestDeliveryImagesPersistence(t *testing.T) {
	// store、cleanup 是独立 SQLite 仓储和清理入口。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本地仓储调用，userID 与 cookieID 提供真实外键。
	ctx := context.Background()
	// userID、cookieID 是同一用户的模板与规则所有者。
	userID, cookieID := seedAccount(t, store)
	// input 同时带旧字段以确认结构化消息具有优先权。
	input := DeliveryTemplateInput{UserID: userID, Name: "图文", Enabled: true, Messages: []string{"不应保存"}, MessageItems: []deliverytemplate.Message{{Content: "订单 {{order_id}}"}, {Type: "image", ImagePath: "说明/图.png"}, {Type: "image", ImageURL: "https://example.test/a.png"}}}
	// templateID、err 保存混合模板创建结果。
	templateID, err := store.DeliveryTemplates.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	// template、readErr 保存真实 SQL 查询结果，确保图片未丢失。
	template, readErr := store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if readErr != nil || len(template.Messages) != 3 || template.Messages[0].Type != "text" || template.Messages[1].ImagePath != "说明/图.png" || template.Messages[2].ImageURL != "https://example.test/a.png" {
		t.Fatalf("模板往返失败: %+v err=%v", template, readErr)
	}
	if err := store.DeliveryTemplates.Update(ctx, userID, templateID, DeliveryTemplateInput{Name: "旧客户端", Messages: []string{"只有正文"}}); !errors.Is(err, ErrDeliveryTemplateMessageConflict) { // err 必须在同一事务内阻止旧字段覆盖图片。
		t.Fatalf("旧客户端丢图保护=%v", err)
	}
	if err := store.DeliveryTemplates.Update(ctx, userID+1, templateID, input); !errors.Is(err, ErrNotFound) { // err 验证跨用户更新不能修改数据或泄露内容。
		t.Fatalf("越权更新=%v", err)
	}
	// ruleID、ruleErr 验证独立图片不要求任何卡密绑定，事务契约可接受图片正文为空。
	ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, Name: "图片发货", TriggerType: "order_paid", Enabled: true, ConfigJSON: "{}", Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, Enabled: true, ConfigJSON: "{}"}}})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// actions、actionErr 检查运行计划同时包含新结构及纯文本兼容字段。
	actions, actionErr := store.Automation.Actions(ctx, ruleID)
	if actionErr != nil || len(actions) != 1 || len(actions[0].TemplateMessageItems) != 3 || len(actions[0].TemplateMessages) != 1 || actions[0].TemplateMessageItems[1].ImagePath != "说明/图.png" {
		t.Fatalf("动作消息缺失: %+v err=%v", actions, actionErr)
	}
	if err := store.Cookies.Save(ctx, "second-image-account", "fixture=second", userID); err != nil { // err 是同用户第二账号的夹具写入错误。
		t.Fatal(err)
	}
	// secondRuleID、secondRuleErr 验证同一个用户级模板允许另一账号复用，而不把图片路径绑定到第一个账号。
	secondRuleID, secondRuleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: "second-image-account", Name: "跨账号图片", TriggerType: "order_paid", Enabled: true, ConfigJSON: "{}", Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, Enabled: true, ConfigJSON: "{}"}}})
	if secondRuleErr != nil || secondRuleID == 0 {
		t.Fatalf("同用户跨账号模板复用失败: %v", secondRuleErr)
	}
	// secondActions、secondActionsErr 验证复用模板的完整图片来源保持相对路径。
	secondActions, secondActionsErr := store.Automation.Actions(ctx, secondRuleID)
	if secondActionsErr != nil || len(secondActions) != 1 || secondActions[0].TemplateMessageItems[1].ImagePath != "说明/图.png" {
		t.Fatalf("跨账号模板加载失败: %v", secondActionsErr)
	}
	// encoded、encodeErr 验证快照新字段使用明确 JSON 名称。
	encoded, encodeErr := json.Marshal(actions[0])
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	// restored 是模拟重启后的动作快照，不依赖当前模板内容。
	var restored AutomationAction
	if err := json.Unmarshal(encoded, &restored); err != nil || len(restored.TemplateMessageItems) != 3 { // err 是动作快照往返错误。
		t.Fatalf("快照往返错误=%v", err)
	}
	input.MessageItems = []deliverytemplate.Message{{Type: "image", ImagePath: "更新.png"}}
	if err := store.DeliveryTemplates.Update(ctx, userID, templateID, input); err != nil { // err 验证已引用模板在变量集合不变时允许图片更新。
		t.Fatal(err)
	}
	// card 是同用户可由不同账号运行时加载的本地图片卡，不持久化账号绑定。
	card := &CardFull{Name: "本地图", Type: "image", UserID: userID, ImagePath: "卡券/图.png", Enabled: true}
	// cardID、cardErr 保存图片卡的真实 SQL 创建结果。
	cardID, cardErr := store.Cards.Create(ctx, card)
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// loaded、loadErr 检查发送专用读取透传相对路径。
	loaded, loadErr := store.Cards.GetForDelivery(ctx, cardID)
	if loadErr != nil || loaded.ImagePath != card.ImagePath || loaded.ImageURL != "" {
		t.Fatalf("图片卡往返失败: %+v err=%v", loaded, loadErr)
	}
	loaded.ImagePath, loaded.ImageURL = "", "https://example.test/card.png"
	if err := store.Cards.Update(ctx, loaded); err != nil { // err 验证更新可显式清除本地来源。
		t.Fatal(err)
	}
	// cards、listErr 检查摘要查询和列表也透传完整来源。
	cards, listErr := store.Cards.AllForUserSummary(ctx, userID)
	if listErr != nil || len(cards) != 1 || cards[0].ImagePath != "" || cards[0].ImageURL != loaded.ImageURL {
		t.Fatalf("卡券列表来源错误: %+v err=%v", cards, listErr)
	}
}
