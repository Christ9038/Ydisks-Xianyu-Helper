package adapter

import (
	"context"
	"errors"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// TestAutomationRepositoryCRUDAndQueryPaths 验证自动化规则适配器的创建、查询、更新、归属和模板摘要路径。
func TestAutomationRepositoryCRUDAndQueryPaths(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试所有数据库调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的自动化规则应用适配器。
	repository := NewAutomationRepository(store)
	// itemErr 保存商品摘要写入错误，用于验证规则列表能够返回商品标题。
	itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "item-1", ItemTitle: "测试商品"})
	if itemErr != nil {
		t.Fatal(itemErr)
	}
	// input 是包含文本动作和自定义变量配置的规则输入。
	input := automationapp.RuleInput{
		UserID: 1, CookieID: "cid", ItemID: "item-1", Name: "测试规则",
		TriggerType: automationapp.TriggerOrderPaid, Enabled: true, Priority: 10,
		ConfigJSON: "{}", Actions: []automationapp.ActionInput{{
			ActionType: automationapp.ActionSendText, MessageTemplate: "已付款",
			ConfigJSON: `{"custom_variables":{"color":"red"}}`, Enabled: true, SortOrder: 1,
		}},
	}
	// ruleID、createErr 保存创建规则的持久化结果。
	ruleID, createErr := repository.Create(ctx, input)
	if createErr != nil || ruleID <= 0 {
		t.Fatalf("Create id=%d err=%v", ruleID, createErr)
	}
	// rules、listErr 保存用户规则列表及转换错误。
	rules, listErr := repository.ListForUser(ctx, 1)
	if listErr != nil || len(rules) != 1 || rules[0].ItemTitle != "测试商品" || len(rules[0].Actions) != 1 || rules[0].Actions[0].CustomVariables["color"] != "red" {
		t.Fatalf("ListForUser rules=%+v err=%v", rules, listErr)
	}
	// enabledFilter 是分页查询只筛选启用规则的条件。
	enabledFilter := true
	// page, total, pageErr 保存分页规则、总数及查询错误。
	page, total, pageErr := repository.ListPageForUser(ctx, automationapp.RuleFilter{UserID: 1, TriggerType: automationapp.TriggerOrderPaid, Enabled: &enabledFilter, Search: " 测试 ", Limit: 10, Offset: 0})
	if pageErr != nil || total != 1 || len(page) != 1 || page[0].ID != ruleID {
		t.Fatalf("ListPage page=%+v total=%d err=%v", page, total, pageErr)
	}
	// counts、countErr 保存触发类型统计及查询错误。
	counts, countErr := repository.CountByTriggerForUser(ctx, automationapp.RuleFilter{UserID: 1})
	if countErr != nil || counts[automationapp.TriggerOrderPaid] != 1 {
		t.Fatalf("CountByTrigger counts=%v err=%v", counts, countErr)
	}
	// updateInput 是替换原规则动作后的输入。
	updateInput := input
	updateInput.Name = "更新规则"
	updateInput.TriggerType = automationapp.TriggerBuyerReviewed
	updateInput.Actions = []automationapp.ActionInput{{ActionType: automationapp.ActionSendText, MessageTemplate: "已评价", Enabled: true, SortOrder: 2}}
	// updateErr 保存用户范围内更新规则的错误。
	updateErr := repository.Update(ctx, 1, ruleID, updateInput)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// updatedRules、updatedErr 保存更新后的规则列表。
	updatedRules, updatedErr := repository.ListForUser(ctx, 1)
	if updatedErr != nil || len(updatedRules) != 1 || updatedRules[0].Name != "更新规则" || updatedRules[0].TriggerType != automationapp.TriggerBuyerReviewed || updatedRules[0].Actions[0].SortOrder != 2 {
		t.Fatalf("updated rules=%+v err=%v", updatedRules, updatedErr)
	}
	// ownsAccount、accountErr 保存账号归属查询结果。
	ownsAccount, accountErr := repository.OwnsAccount(ctx, 1, "cid")
	if accountErr != nil || !ownsAccount {
		t.Fatalf("owned account=%v err=%v", ownsAccount, accountErr)
	}
	// missingAccount、missingAccountErr 保存不存在账号的归属结果。
	missingAccount, missingAccountErr := repository.OwnsAccount(ctx, 1, "missing")
	if missingAccountErr != nil || missingAccount {
		t.Fatalf("missing account=%v err=%v", missingAccount, missingAccountErr)
	}
	// ownsItem、itemOwnershipErr 保存商品归属查询结果。
	ownsItem, itemOwnershipErr := repository.OwnsItem(ctx, 1, "cid", "item-1")
	if itemOwnershipErr != nil || !ownsItem {
		t.Fatalf("owned item=%v err=%v", ownsItem, itemOwnershipErr)
	}
	// templateID、templateErr 保存用于规则校验的模板创建结果。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{UserID: 1, Name: "测试模板", Enabled: true, Messages: []string{"正文"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// templateInfo、templateInfoErr 保存用户模板非敏感摘要。
	templateInfo, templateInfoErr := repository.GetDeliveryTemplate(ctx, 1, templateID)
	if templateInfoErr != nil || templateInfo.Enabled != true || len(templateInfo.Keys) != 0 {
		t.Fatalf("template info=%+v err=%v", templateInfo, templateInfoErr)
	}
	// missingTemplateErr 保存跨用户模板读取的业务错误。
	_, missingTemplateErr := repository.GetDeliveryTemplate(ctx, 2, templateID)
	if !errors.Is(missingTemplateErr, automationapp.ErrRuleNotFound) {
		t.Fatalf("cross-user template err=%v", missingTemplateErr)
	}
	// deleteErr 保存用户范围内删除规则的错误。
	deleteErr := repository.Delete(ctx, 1, ruleID)
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// deletedRules、deletedListErr 保存逻辑删除后的规则列表。
	deletedRules, deletedListErr := repository.ListForUser(ctx, 1)
	if deletedListErr != nil || len(deletedRules) != 0 {
		t.Fatalf("deleted rules=%+v err=%v", deletedRules, deletedListErr)
	}
}

// TestAutomationRepositoryPricingModeAndSensitiveSummary 验证改价互斥校验、AI 开关读取和卡密脱敏摘要。
func TestAutomationRepositoryPricingModeAndSensitiveSummary(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的自动化规则应用适配器。
	repository := NewAutomationRepository(store)
	// textCardID、textCardErr 保存普通文本卡密组的创建结果。
	textCardID, textCardErr := store.Cards.Create(ctx, &db.CardFull{UserID: 1, Name: "文本卡", Type: "text", TextContent: "secret", Enabled: true})
	if textCardErr != nil {
		t.Fatal(textCardErr)
	}
	// textCard、textCardLookupErr 保存普通卡密脱敏摘要。
	textCard, textCardLookupErr := repository.GetCard(ctx, 1, textCardID)
	if textCardLookupErr != nil || !textCard.Enabled || textCard.APIReady != true {
		t.Fatalf("text card=%+v err=%v", textCard, textCardLookupErr)
	}
	// foreignCard、foreignCardErr 保存跨用户卡密读取结果。
	foreignCard, foreignCardErr := repository.GetCard(ctx, 2, textCardID)
	if foreignCard != (automationapp.CardInfo{}) || !errors.Is(foreignCardErr, automationapp.ErrRuleNotFound) {
		t.Fatalf("foreign card=%+v err=%v", foreignCard, foreignCardErr)
	}
	// aiBefore、aiBeforeErr 保存尚未配置 AI 开关时的默认状态。
	aiBefore, aiBeforeErr := repository.AIReplyEnabled(ctx, "cid")
	if aiBeforeErr != nil || aiBefore {
		t.Fatalf("default AI enabled=%v err=%v", aiBefore, aiBeforeErr)
	}
	// settingsErr 保存开启 AI 议价设置的错误。
	settingsErr := store.AIReply.UpsertSettings(ctx, "cid", db.AIReplySettings{AIEnabled: true, MaxBargainRounds: 2})
	if settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// aiAfter、aiAfterErr 保存开启后的 AI 开关状态。
	aiAfter, aiAfterErr := repository.AIReplyEnabled(ctx, "cid")
	if aiAfterErr != nil || !aiAfter {
		t.Fatalf("enabled AI=%v err=%v", aiAfter, aiAfterErr)
	}
	// adjustInput 是会启用固定改价动作的规则输入。
	adjustInput := automationapp.RuleInput{UserID: 1, CookieID: "cid", ItemID: "item", Name: "改价", TriggerType: automationapp.TriggerOrderCreated, Enabled: true, Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionAdjustPrice, Enabled: true}}}
	// createErr 保存 AI 议价与固定改价冲突时的创建错误。
	_, createErr := repository.Create(ctx, adjustInput)
	if !errors.Is(createErr, automationapp.ErrPricingModeConflict) {
		t.Fatalf("pricing conflict create err=%v", createErr)
	}
	// _, unavailableErr 保存引用缺失模板时适配器应返回的应用层冲突错误。
	_, unavailableErr := repository.Create(ctx, automationapp.RuleInput{
		UserID: 1, CookieID: "cid", Name: "缺失模板", TriggerType: automationapp.TriggerOrderPaid, Enabled: true,
		Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionSendTemplate, DeliveryTemplateID: 999999, Enabled: true}},
	})
	if !errors.Is(unavailableErr, automationapp.ErrDeliveryTemplateUnavailable) {
		t.Fatalf("missing delivery template err=%v", unavailableErr)
	}
	// nilRepository 表示未装配数据库存储的适配器。
	var nilRepository *AutomationRepository
	// nilAIEnabled、nilAIError 保存未初始化适配器的 AI 查询结果和稳定错误。
	nilAIEnabled, nilAIError := nilRepository.AIReplyEnabled(ctx, "cid")
	// nilTemplateError 保存未初始化适配器的模板查询错误。
	_, nilTemplateError := nilRepository.GetDeliveryTemplate(ctx, 1, 1)
	if nilAIEnabled || nilAIError == nil || nilTemplateError == nil {
		t.Fatalf("nil repository result=%v errors=%v/%v", nilAIEnabled, nilAIError, nilTemplateError)
	}
}

// TestAutomationRepositoryIssuesAndErrorMapping 验证异常摘要转换、人工处理和数据库错误边界。
func TestAutomationRepositoryIssuesAndErrorMapping(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的自动化规则应用适配器。
	repository := NewAutomationRepository(store)
	// ruleID、ruleErr 保存问题运行引用的规则创建结果。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: 1, CookieID: "cid", ItemID: "item", Name: "问题规则", TriggerType: automationapp.TriggerBuyerReviewed, Enabled: true, Actions: []db.AutomationActionInput{{ActionType: automationapp.ActionSendText, Enabled: true}}})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// runID、started、startErr 保存待人工处理运行的创建结果。
	runID, started, startErr := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "cid", OrderID: "order", TriggerType: automationapp.TriggerBuyerReviewed, TriggerKey: "adapter-issue", RawEventJSON: `{"account_id":"cid"}`})
	if startErr != nil || !started {
		t.Fatalf("start=%v err=%v", started, startErr)
	}
	// quarantineErr 保存将部分执行结果转入人工处理状态的错误。
	quarantineErr := store.Automation.QuarantineRunResult(ctx, runID, 1, 1, "需要人工确认")
	if quarantineErr != nil {
		t.Fatal(quarantineErr)
	}
	// deferErr 保存死信延期任务的写入错误。
	deferErr := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{TaskKey: "adapter-dead", CookieID: "cid", TriggerType: automationapp.TriggerBuyerReviewed, TaskJSON: `{}`, DueAt: 0, ErrorMessage: "失败"})
	if deferErr != nil {
		t.Fatal(deferErr)
	}
	// updateErr 保存将延期任务设置为死信的测试更新错误。
	_, updateErr := store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='dead_letter',attempt_count=5 WHERE task_key=?`, "adapter-dead")
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// runs、tasks、issuesErr 保存适配器转换后的异常摘要。
	runs, tasks, issuesErr := repository.ListIssues(ctx, 1)
	if issuesErr != nil || len(runs) != 1 || len(tasks) != 1 || runs[0].CookieID != "cid" || tasks[0].AttemptCount != 5 {
		t.Fatalf("issues runs=%+v tasks=%+v err=%v", runs, tasks, issuesErr)
	}
	// continueErr 保存允许的继续处理结果。
	continueErr := repository.ResolveRunIssue(ctx, 1, runID, "continue")
	if continueErr != nil {
		t.Fatal(continueErr)
	}
	// retryErr 保存延期任务重试处理结果。
	retryErr := repository.ResolveDeferredIssue(ctx, 1, tasks[0].ID, true)
	if retryErr != nil {
		t.Fatal(retryErr)
	}
	// missingRunErr、missingTaskErr 保存不存在异常的应用层错误。
	missingRunErr := repository.ResolveRunIssue(ctx, 1, 999999, "cancel")
	// missingTaskErr 保存不存在延期任务的应用层错误。
	missingTaskErr := repository.ResolveDeferredIssue(ctx, 1, 999999, false)
	if !errors.Is(missingRunErr, automationapp.ErrNotFound) || !errors.Is(missingTaskErr, automationapp.ErrNotFound) {
		t.Fatalf("missing errors run=%v task=%v", missingRunErr, missingTaskErr)
	}
	// invalidJSONValues、legacyValues、invalidValues 保存自定义变量配置的兼容解析结果。
	invalidJSONValues := customVariablesFromConfig("not-json")
	// legacyValues 保存历史数组格式转换后的自定义变量。
	legacyValues := customVariablesFromConfig(`{"custom_variables":["first","second"]}`)
	// invalidValues 保存不支持的自定义变量类型解析结果。
	invalidValues := customVariablesFromConfig(`{"custom_variables":1}`)
	if invalidJSONValues != nil || legacyValues["0"] != "first" || legacyValues["1"] != "second" || invalidValues != nil {
		t.Fatalf("custom variables invalid=%v legacy=%v type=%v", invalidJSONValues, legacyValues, invalidValues)
	}
	// copied、copiedSource 保存字符串映射复制结果及其独立源数据。
	copiedSource := map[string]string{"key": "value"}
	// copied 保存不应与源映射共享底层存储的副本。
	copied := copyStringMap(copiedSource)
	copied["key"] = "changed"
	if copiedSource["key"] != "value" || copyStringMap(nil) != nil {
		t.Fatalf("copy map source=%v copied=%v", copiedSource, copied)
	}
	// disabledInput、disabledAction、enabledInput 保存固定改价动作判定的边界输入。
	disabledInput := automationapp.RuleInput{Enabled: false, Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionAdjustPrice, Enabled: true}}}
	// disabledAction 保存规则启用但改价动作禁用的输入。
	disabledAction := automationapp.RuleInput{Enabled: true, Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionAdjustPrice, Enabled: false}}}
	// enabledInput 保存规则和改价动作同时启用的输入。
	enabledInput := automationapp.RuleInput{Enabled: true, Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionAdjustPrice, Enabled: true}}}
	if automationInputEnablesAdjustPrice(disabledInput) || automationInputEnablesAdjustPrice(disabledAction) || !automationInputEnablesAdjustPrice(enabledInput) {
		t.Fatal("adjust price enablement mapping is incorrect")
	}
	// mappedNotFound、mappedActive 保存数据库规则错误的应用层映射。
	mappedNotFound := mapAutomationRuleError(db.ErrNotFound)
	// mappedActive 保存规则仍有运行时的应用层错误映射。
	mappedActive := mapAutomationRuleError(db.ErrAutomationRunActive)
	if !errors.Is(mappedNotFound, automationapp.ErrRuleNotFound) || !errors.Is(mappedActive, automationapp.ErrRuleActive) || mapAutomationRuleError(nil) != nil {
		t.Fatalf("rule errors notFound=%v active=%v", mappedNotFound, mappedActive)
	}
	// mappedIssueNotFound 保存数据库异常错误的应用层映射。
	mappedIssueNotFound := mapAutomationIssueError(db.ErrNotFound)
	if !errors.Is(mappedIssueNotFound, automationapp.ErrNotFound) || mapAutomationIssueError(nil) != nil {
		t.Fatalf("issue errors=%v", mappedIssueNotFound)
	}
}
