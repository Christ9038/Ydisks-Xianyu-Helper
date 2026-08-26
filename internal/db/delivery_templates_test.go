package db

import (
	"context"
	"errors"
	"testing"
)

// TestDeliveryTemplateStoreRejectsUninitializedStore 验证模板仓储在未绑定数据库时拒绝所有写读操作。
func TestDeliveryTemplateStoreRejectsUninitializedStore(t *testing.T) {
	// ctx 是本测试所有仓储调用共用的非取消上下文。
	ctx := context.Background()
	// store 是故意未初始化数据库连接的仓储。
	var store *DeliveryTemplateStore
	// input 是用于触发创建和更新校验的最小输入。
	input := DeliveryTemplateInput{Name: "模板", Messages: []string{"正文"}}
	// list, listErr 保存列表调用的初始化错误。
	list, listErr := store.ListForUser(ctx, 1)
	// item, getErr 保存单模板调用的初始化错误。
	item, getErr := store.GetForUser(ctx, 1, 1)
	// createID, createErr 保存创建调用的初始化错误。
	createID, createErr := store.Create(ctx, input)
	// updateErr 保存更新调用的初始化错误。
	updateErr := store.Update(ctx, 1, 1, input)
	// deleteErr 保存删除调用的初始化错误。
	deleteErr := store.Delete(ctx, 1, 1)
	if list != nil || item != nil || createID != 0 || listErr == nil || getErr == nil || createErr == nil || updateErr == nil || deleteErr == nil {
		t.Fatalf("uninitialized store results: list=%v get=%v id=%d errors=%v/%v/%v/%v/%v", list, item, createID, listErr, getErr, createErr, updateErr, deleteErr)
	}
}

// TestDeliveryTemplateStoreCRUD 验证模板创建、查询、更新和逻辑删除的完整 SQLite 业务链路。
func TestDeliveryTemplateStoreCRUD(t *testing.T) {
	// store, cleanup 保存本测试使用的临时数据库及释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试所有仓储调用共用的非取消上下文。
	ctx := context.Background()
	// userID, cookieID 保存外键初始化结果；cookieID 在本测试中用于自动化规则归属。
	userID, cookieID := seedAccount(t, store)

	// emptyID, emptyErr 保存空消息创建的校验结果。
	emptyID, emptyErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "空模板"})
	if emptyID != 0 || emptyErr == nil {
		t.Fatalf("empty messages should fail: id=%d err=%v", emptyID, emptyErr)
	}
	// blankID, blankErr 保存空名称创建的校验结果。
	blankID, blankErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "  ", Messages: []string{"正文"}})
	if blankID != 0 || blankErr == nil {
		t.Fatalf("blank name should fail: id=%d err=%v", blankID, blankErr)
	}
	// templateID, createErr 保存合法模板创建结果。
	templateID, createErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "  发货模板  ", Enabled: true, Messages: []string{"订单 {{order_id}}", "卡密 {{cards.main}} {{custom.remark}}"}})
	if createErr != nil {
		t.Fatalf("Create: %v", createErr)
	}
	// got, getErr 保存单模板读取结果。
	got, getErr := store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if getErr != nil || got == nil || got.Name != "发货模板" || !got.Enabled || len(got.Messages) != 2 || len(got.Keys) != 1 || got.Keys[0] != "main" || len(got.CustomKeys) != 1 || got.CustomKeys[0] != "remark" {
		t.Fatalf("Get result=%+v err=%v", got, getErr)
	}
	// missing, missingErr 保存其他用户读取结果，确认所有权不会泄露模板。
	missing, missingErr := store.DeliveryTemplates.GetForUser(ctx, userID+1, templateID)
	if missing != nil || !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("cross-user Get result=%v err=%v", missing, missingErr)
	}
	// templates, listErr 保存列表查询结果。
	templates, listErr := store.DeliveryTemplates.ListForUser(ctx, userID)
	if listErr != nil || len(templates) != 1 || templates[0].ID != templateID {
		t.Fatalf("List result=%+v err=%v", templates, listErr)
	}
	// updateErr 保存变量集合相同但消息顺序改变的兼容更新结果。
	updateErr := store.DeliveryTemplates.Update(ctx, userID, templateID, DeliveryTemplateInput{UserID: userID, Name: "更新模板", Enabled: false, Messages: []string{"自定义 {{custom.remark}}", "卡密 {{cards.main}}"}})
	if updateErr != nil {
		t.Fatalf("compatible Update: %v", updateErr)
	}
	// updated, updatedErr 保存更新后的模板状态。
	updated, updatedErr := store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if updatedErr != nil || updated.Name != "更新模板" || updated.Enabled || updated.Messages[0].Content != "自定义 {{custom.remark}}" {
		t.Fatalf("updated template=%+v err=%v", updated, updatedErr)
	}
	// deleteErr 保存无引用模板的逻辑删除结果。
	deleteErr := store.DeliveryTemplates.Delete(ctx, userID, templateID)
	if deleteErr != nil {
		t.Fatalf("Delete: %v", deleteErr)
	}
	// deleted, deletedErr 保存逻辑删除后的读取结果。
	deleted, deletedErr := store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if deleted != nil || !errors.Is(deletedErr, ErrNotFound) {
		t.Fatalf("deleted template result=%v err=%v", deleted, deletedErr)
	}
	// listAfterDelete, listAfterDeleteErr 保存逻辑删除后的列表结果。
	listAfterDelete, listAfterDeleteErr := store.DeliveryTemplates.ListForUser(ctx, userID)
	if listAfterDeleteErr != nil || len(listAfterDelete) != 0 {
		t.Fatalf("deleted template should be hidden: list=%v err=%v", listAfterDelete, listAfterDeleteErr)
	}
	// _ 确认此前创建的账号仍可作为自动化归属数据使用，避免测试只覆盖孤立模板表。
	_ = cookieID
}

// TestDeliveryTemplateStoreReferenceProtection 验证被规则引用的模板禁止不兼容更新和删除。
func TestDeliveryTemplateStoreReferenceProtection(t *testing.T) {
	// store, cleanup 保存本测试使用的临时数据库及释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试所有仓储调用共用的非取消上下文。
	ctx := context.Background()
	// userID, cookieID 保存自动化规则外键初始化结果。
	userID, cookieID := seedAccount(t, store)
	// templateID, createErr 保存带卡密和自定义变量模板的创建结果。
	templateID, createErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "受保护模板", Enabled: true, Messages: []string{"{{cards.main}} {{custom.remark}}"}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// cardID 保存模板变量绑定所需的文本卡密组。
	cardID, cardErr := store.Cards.Create(ctx, &CardFull{UserID: userID, Name: "绑定库存", Type: "text", TextContent: "内容", Enabled: true})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleID, ruleErr 保存引用模板的自动化规则创建结果。
	ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "item-1", Name: "引用规则", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 1}}, CustomVariables: map[string]string{"remark": "备注"}, ConfigJSON: `{}`, Enabled: true, SortOrder: 1}}})
	if ruleErr != nil {
		t.Fatalf("create referencing rule: %v", ruleErr)
	}
	if ruleID == 0 {
		t.Fatal("referencing rule id must be assigned")
	}
	// conflictErr 保存被引用模板修改变量集合后的保护错误。
	conflictErr := store.DeliveryTemplates.Update(ctx, userID, templateID, DeliveryTemplateInput{UserID: userID, Name: "不兼容", Enabled: true, Messages: []string{"{{cards.other}} {{custom.remark}}"}})
	if !errors.Is(conflictErr, ErrDeliveryTemplateVariableConflict) {
		t.Fatalf("variable conflict=%v", conflictErr)
	}
	// referencedErr 保存被规则引用模板删除时的保护错误。
	referencedErr := store.DeliveryTemplates.Delete(ctx, userID, templateID)
	if !errors.Is(referencedErr, ErrDeliveryTemplateReferenced) {
		t.Fatalf("reference delete error=%v", referencedErr)
	}
	// disableErr 保存禁用引用规则的数据库更新错误；禁用规则仍可被重新启用，必须继续保护模板契约。
	if _, disableErr := store.DB.ExecContext(ctx, `UPDATE automation_rules SET enabled=0 WHERE id=?`, ruleID); disableErr != nil {
		t.Fatal(disableErr)
	}
	// disabledConflictErr 保存禁用但未删除规则继续阻止变量破坏的结果。
	disabledConflictErr := store.DeliveryTemplates.Update(ctx, userID, templateID, DeliveryTemplateInput{UserID: userID, Name: "禁用后仍保护", Enabled: true, Messages: []string{"{{cards.other}} {{custom.remark}}"}})
	if !errors.Is(disabledConflictErr, ErrDeliveryTemplateVariableConflict) {
		t.Fatalf("disabled rule variable conflict=%v", disabledConflictErr)
	}
	// deleteRuleErr 保存软删除引用规则的结果。
	if deleteRuleErr := store.Automation.Delete(ctx, userID, ruleID); deleteRuleErr != nil {
		t.Fatalf("delete referencing rule: %v", deleteRuleErr)
	}
	// releasedUpdateErr 保存规则删除后允许修改模板变量契约的结果。
	releasedUpdateErr := store.DeliveryTemplates.Update(ctx, userID, templateID, DeliveryTemplateInput{UserID: userID, Name: "已释放", Enabled: true, Messages: []string{"{{cards.other}} {{custom.remark}}"}})
	if releasedUpdateErr != nil {
		t.Fatalf("deleted rule should release template: %v", releasedUpdateErr)
	}
	// releasedDeleteErr 保存规则删除后允许逻辑删除模板的结果。
	if releasedDeleteErr := store.DeliveryTemplates.Delete(ctx, userID, templateID); releasedDeleteErr != nil {
		t.Fatalf("deleted rule should release template deletion: %v", releasedDeleteErr)
	}
	// notFoundErr 保存其他用户更新模板的所有权错误。
	notFoundErr := store.DeliveryTemplates.Update(ctx, userID+1, templateID, DeliveryTemplateInput{UserID: userID + 1, Name: "越权", Messages: []string{"正文"}})
	if !errors.Is(notFoundErr, ErrNotFound) {
		t.Fatalf("cross-user Update error=%v", notFoundErr)
	}
}

// TestDeliveryTemplateStoreKeepsLegacyInvalidMessages 验证历史脏消息仍可展示，但不会伪造变量键摘要。
func TestDeliveryTemplateStoreKeepsLegacyInvalidMessages(t *testing.T) {
	// store, cleanup 保存本测试使用的临时数据库及释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试所有仓储调用共用的非取消上下文。
	ctx := context.Background()
	// userID 保存模板所有者标识。
	userID, _ := seedAccount(t, store)
	// templateID, createErr 保存合法模板创建结果。
	templateID, createErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "历史模板", Messages: []string{"正文"}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// err 保存把历史非法正文写入消息表的数据库错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE delivery_template_messages SET content=? WHERE template_id=?`, "{{invalid", templateID); err != nil {
		t.Fatal(err)
	}
	// got, getErr 保存脏消息展示结果。
	got, getErr := store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if getErr != nil || got == nil || len(got.Messages) != 1 || got.Messages[0].Content != "{{invalid" || len(got.Keys) != 0 || len(got.CustomKeys) != 0 {
		t.Fatalf("legacy invalid message=%+v err=%v", got, getErr)
	}
}

// TestAutomationLoadsTemplateActionContract 验证自动化动作会加载模板消息、卡密绑定和新旧自定义变量格式。
func TestAutomationLoadsTemplateActionContract(t *testing.T) {
	// store, cleanup 保存本测试使用的临时数据库及释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试所有仓储调用共用的非取消上下文。
	ctx := context.Background()
	// userID, cookieID 保存自动化规则外键初始化结果。
	userID, cookieID := seedAccount(t, store)
	// cardID, cardErr 保存模板绑定需要的卡密组。
	cardID, cardErr := store.Cards.Create(ctx, &CardFull{UserID: userID, Name: "绑定库存", Type: "text", TextContent: "内容", Enabled: true})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// templateID, templateErr 保存包含卡密和自定义变量的模板。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "动作模板", Enabled: true, Messages: []string{"{{cards.main}} {{custom.remark}}"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// ruleID, ruleErr 保存带新格式自定义变量和绑定的规则。
	ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "item", Name: "完整动作", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, ConfigJSON: `{}`, CustomVariables: map[string]string{"remark": "备注"}, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 1}}, Enabled: true, SortOrder: 1}}})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// actions, actionsErr 保存完整动作读取结果。
	actions, actionsErr := store.Automation.Actions(ctx, ruleID)
	if actionsErr != nil || len(actions) != 1 || len(actions[0].TemplateMessages) != 1 || actions[0].TemplateMessages[0] != "{{cards.main}} {{custom.remark}}" || actions[0].CustomVariables["remark"] != "备注" || len(actions[0].TemplateBindings) != 1 || actions[0].TemplateBindings[0].CardName != "绑定库存" {
		t.Fatalf("loaded actions=%+v err=%v", actions, actionsErr)
	}

	// legacyTemplateID, legacyTemplateErr 保存使用历史数字自定义键的模板。
	legacyTemplateID, legacyTemplateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "历史模板", Enabled: true, Messages: []string{"{{cards.main}} {{delivery.custom.0}}"}})
	if legacyTemplateErr != nil {
		t.Fatal(legacyTemplateErr)
	}
	// legacyRuleID, legacyRuleErr 保存历史数组格式自定义变量的规则。
	legacyRuleID, legacyRuleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "legacy", Name: "历史动作", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: legacyTemplateID, ConfigJSON: `{"custom_variables":["历史备注"]}`, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 1}}, Enabled: true, SortOrder: 1}}})
	if legacyRuleErr != nil {
		t.Fatal(legacyRuleErr)
	}
	// legacyActions, legacyActionsErr 保存历史动作的兼容读取结果。
	legacyActions, legacyActionsErr := store.Automation.Actions(ctx, legacyRuleID)
	if legacyActionsErr != nil || legacyActions[0].CustomVariables["0"] != "历史备注" {
		t.Fatalf("legacy actions=%+v err=%v", legacyActions, legacyActionsErr)
	}

	// missingBindingRuleID, missingBindingRuleErr 保存缺少卡密绑定的规则。
	missingBindingRuleID, missingBindingRuleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "missing-binding", Name: "缺少绑定", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, ConfigJSON: `{"custom_variables":{"remark":"备注"}}`, Enabled: true, SortOrder: 1}}})
	if missingBindingRuleID != 0 || !errors.Is(missingBindingRuleErr, ErrDeliveryTemplateUnavailable) {
		t.Fatalf("missing card binding should fail at write, id=%d err=%v", missingBindingRuleID, missingBindingRuleErr)
	}

	// missingCustomRuleID, missingCustomRuleErr 保存缺少自定义变量值的规则。
	missingCustomRuleID, missingCustomRuleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "missing-custom", Name: "缺少自定义值", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, ConfigJSON: `{"custom_variables":{"remark":"  "}}`, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 1}}, Enabled: true, SortOrder: 1}}})
	if missingCustomRuleID != 0 || !errors.Is(missingCustomRuleErr, ErrDeliveryTemplateUnavailable) {
		t.Fatalf("blank custom variable should fail at write, id=%d err=%v", missingCustomRuleID, missingCustomRuleErr)
	}
}

// TestEncryptLegacyAutomationDeliveryProof 验证历史明文发货凭证会在迁移时按运行标识加密。
func TestEncryptLegacyAutomationDeliveryProof(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "legacy-proof-test-key")
	// store, cleanup 保存启用数据密钥的临时数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试所有仓储调用共用的非取消上下文。
	ctx := context.Background()
	// userID, cookieID 保存自动化运行外键初始化结果。
	userID, cookieID := seedAccount(t, store)
	// ruleID, ruleErr 保存最小自动化规则。
	ruleID, ruleErr := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, "legacy-proof", "paid", true, 1))
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// runID, started, startErr 保存自动化运行创建结果。
	runID, started, startErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, TriggerType: "paid", TriggerKey: "legacy-proof-key"})
	if startErr != nil || !started {
		t.Fatalf("TryStartRun started=%v err=%v", started, startErr)
	}
	// legacyProof 是模拟旧版本落库格式的非敏感测试文本。
	legacyProof := `{"trade_text":"legacy-text","pic_list":[]}`
	// err 保存把旧格式凭证写入数据库的错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET delivery_proof=? WHERE id=?`, legacyProof, runID); err != nil {
		t.Fatal(err)
	}
	// encryptErr 保存历史凭证批量迁移结果。
	if encryptErr := store.EncryptLegacySecrets(ctx); encryptErr != nil {
		t.Fatal(encryptErr)
	}
	// rawProof 保存迁移后的数据库原始值，仅用于验证没有继续保留明文。
	var rawProof string
	// scanErr 保存迁移后凭证读取错误。
	if scanErr := store.DB.QueryRowContext(ctx, `SELECT delivery_proof FROM automation_runs WHERE id=?`, runID).Scan(&rawProof); scanErr != nil {
		t.Fatal(scanErr)
	}
	if rawProof == legacyProof || rawProof == "" {
		t.Fatalf("legacy proof was not encrypted: %q", rawProof)
	}
	// restored, restoreErr 保存迁移后通过业务仓储解密的凭证。
	restored, restoreErr := store.Automation.GetRun(ctx, runID)
	if restoreErr != nil || restored.DeliveryProof.TradeText != "legacy-text" {
		t.Fatalf("restored proof=%+v err=%v", restored, restoreErr)
	}
}

// TestSameStringSet 验证模板变量契约比较忽略顺序但拒绝缺失和额外键。
func TestSameStringSet(t *testing.T) {
	// cases 描述变量集合比较的边界场景。
	cases := []struct {
		// name 是子测试名称。
		name string
		// left 是左侧变量键集合。
		left []string
		// right 是右侧变量键集合。
		right []string
		// want 是预期的集合相等结果。
		want bool
	}{
		{name: "same", left: []string{"a", "b"}, right: []string{"b", "a"}, want: true},
		{name: "different length", left: []string{"a"}, right: []string{"a", "b"}, want: false},
		{name: "missing key", left: []string{"a"}, right: []string{"b"}, want: false},
	}
	for /* item 表示当前变量集合比较场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// got 保存当前变量键集合比较结果。
			if got := sameStringSet(item.left, item.right); got != item.want {
				t.Fatalf("sameStringSet(%v,%v)=%v want %v", item.left, item.right, got, item.want)
			}
		})
	}
}
