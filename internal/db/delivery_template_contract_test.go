package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestDeliveryTemplateReferenceIDsAreUniqueAndSorted 验证模板引用 ID 去重并按升序固定锁顺序。
func TestDeliveryTemplateReferenceIDsAreUniqueAndSorted(t *testing.T) {
	// got 保存动作引用模板 ID 的归一化结果。
	got := deliveryTemplateIDsFromActions([]AutomationActionInput{
		{DeliveryTemplateID: 9}, {DeliveryTemplateID: 3}, {DeliveryTemplateID: 9}, {DeliveryTemplateID: 0}, {DeliveryTemplateID: -1},
	})
	// want 保存所有写事务必须采用的唯一升序模板 ID。
	want := []int64{3, 9}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("模板引用 ID 顺序错误：got=%v want=%v", got, want)
	}
	// emptyStore、cleanup 保存空引用集合测试所需的数据库。
	emptyStore, cleanup := newTestDB(t)
	defer cleanup()
	// tx、txErr 保存空集合锁校验事务。
	tx, txErr := emptyStore.DB.BeginTx(context.Background(), nil)
	if txErr != nil {
		t.Fatal(txErr)
	}
	// lockErr 保存空集合直接成功的结果。
	lockErr := lockLiveDeliveryTemplatesTx(context.Background(), tx, DialectSQLite, 1, nil)
	if lockErr != nil {
		t.Fatalf("空模板集合不应失败：%v", lockErr)
	}
	// rollbackErr 保存空集合事务回滚错误。
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
}

// TestLockLiveDeliveryTemplatesRejectsMissingTemplate 验证模板缺失、越权和逻辑删除都会阻止规则写入。
func TestLockLiveDeliveryTemplatesRejectsMissingTemplate(t *testing.T) {
	// store、cleanup 保存模板引用锁测试使用的数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存模板创建和事务校验共用的上下文。
	ctx := context.Background()
	// userID 保存模板所有者标识。
	userID, _ := seedAccount(t, store)
	// templateID、templateErr 保存有效模板主键。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "锁测试模板", Enabled: true, Messages: []string{"正文"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// cases 保存有效、缺失和越权模板引用的预期错误。
	cases := []struct {
		name string
		user int64
		ids  []int64
		want error
	}{
		{name: "valid", user: userID, ids: []int64{templateID}},
		{name: "missing", user: userID, ids: []int64{templateID + 99999}, want: ErrDeliveryTemplateUnavailable},
		{name: "foreign", user: userID + 1, ids: []int64{templateID}, want: ErrDeliveryTemplateUnavailable},
	}
	// testCase 表示当前模板锁校验场景。
	for _, testCase := range cases {
		// tx、txErr 保存当前场景独立事务。
		tx, txErr := store.DB.BeginTx(ctx, nil)
		if txErr != nil {
			t.Fatal(txErr)
		}
		// gotErr 保存当前场景的模板锁校验错误。
		gotErr := lockLiveDeliveryTemplatesTx(ctx, tx, DialectSQLite, testCase.user, testCase.ids)
		_ = tx.Rollback()
		if testCase.want == nil {
			if gotErr != nil {
				t.Errorf("%s 不应失败：%v", testCase.name, gotErr)
			}
		} else if !errors.Is(gotErr, testCase.want) {
			t.Errorf("%s 错误=%v want=%v", testCase.name, gotErr, testCase.want)
		}
	}
	// deleteErr 保存模板逻辑删除结果，用于验证删除后不能再被新规则引用。
	if deleteErr := store.DeliveryTemplates.Delete(ctx, userID, templateID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// tx、txErr 保存逻辑删除后的再次引用检查事务。
	tx, txErr := store.DB.BeginTx(ctx, nil)
	if txErr != nil {
		t.Fatal(txErr)
	}
	// deletedErr 保存逻辑删除模板的锁校验错误。
	deletedErr := lockLiveDeliveryTemplatesTx(ctx, tx, DialectSQLite, userID, []int64{templateID})
	_ = tx.Rollback()
	if !errors.Is(deletedErr, ErrDeliveryTemplateUnavailable) {
		t.Fatalf("已删除模板应不可用：%v", deletedErr)
	}
}

// TestDeliveryTemplateDeleteAndRuleCreateSerialize 验证删除先持锁提交时，规则创建会重新校验并原子回滚。
func TestDeliveryTemplateDeleteAndRuleCreateSerialize(t *testing.T) {
	// store、cleanup 保存 SQLite 并发事务测试使用的数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存并发事务共用的上下文。
	ctx := context.Background()
	// userID、cookieID 保存规则创建所需的账号归属。
	userID, cookieID := seedAccount(t, store)
	// templateID、templateErr 保存被并发删除的模板主键。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "并发模板", Enabled: true, Messages: []string{"正文"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// deleteTx、txErr 保存先锁定模板并暂不提交的删除事务。
	deleteTx, txErr := store.DB.BeginTx(ctx, nil)
	if txErr != nil {
		t.Fatal(txErr)
	}
	// lockErr 保存删除事务取得模板锁时的错误。
	if lockErr := lockLiveDeliveryTemplatesTx(ctx, deleteTx, DialectSQLite, userID, []int64{templateID}); lockErr != nil {
		deleteTx.Rollback()
		t.Fatal(lockErr)
	}
	// deleteResult、deleteErr 保存删除事务中软删除模板的结果。
	deleteResult, deleteErr := deleteTx.ExecContext(ctx, `UPDATE delivery_templates SET deleted_at=CURRENT_TIMESTAMP,enabled=0 WHERE id=? AND user_id=? AND deleted_at IS NULL`, templateID, userID)
	if deleteErr != nil {
		deleteTx.Rollback()
		t.Fatal(deleteErr)
	}
	// affected、affectedErr 保存删除事务实际命中的行数及读取行数错误。
	if affected, affectedErr := deleteResult.RowsAffected(); affectedErr != nil || affected != 1 {
		deleteTx.Rollback()
		t.Fatalf("删除事务未命中模板：affected=%d err=%v", affected, affectedErr)
	}
	// createResult 保存等待删除事务提交后执行的规则创建结果。
	createResult := make(chan error, 1)
	go func() {
		// createErr 保存并发规则创建的最终错误；模板删除后必须是不可用哨兵。
		_, createErr := store.Automation.Create(ctx, AutomationRuleInput{
			UserID: userID, CookieID: cookieID, ItemID: "concurrent-item", Name: "并发规则", TriggerType: "order_paid", Enabled: true,
			Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, Enabled: true, SortOrder: 1}},
		})
		createResult <- createErr
	}()
	// waitErr 保存短暂等待结果；未提交删除时规则创建不得越过模板行锁。
	select {
	// earlyErr 保存不应在删除事务提交前出现的规则创建结果。
	case earlyErr := <-createResult:
		deleteTx.Rollback()
		t.Fatalf("删除事务未提交前规则创建提前完成：%v", earlyErr)
	case <-time.After(100 * time.Millisecond):
	}
	// commitErr 保存删除事务提交错误。
	if commitErr := deleteTx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	// timeout 保存并发规则创建的最长等待时间，避免驱动异常导致测试永久阻塞。
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	select {
	// createErr 保存删除事务提交后规则创建返回的模板状态错误。
	case createErr := <-createResult:
		if !errors.Is(createErr, ErrDeliveryTemplateUnavailable) {
			t.Fatalf("模板删除后规则创建错误=%v want=%v", createErr, ErrDeliveryTemplateUnavailable)
		}
	case <-timeout.C:
		t.Fatal("规则创建未在删除提交后结束")
	}
	// ruleCount、actionCount 保存并发失败后规则及动作数量，确保没有部分提交。
	var ruleCount, actionCount int
	// scanErr 保存失败规则数量读取错误。
	if scanErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE name=?`, "并发规则").Scan(&ruleCount); scanErr != nil {
		t.Fatal(scanErr)
	}
	// scanErr 保存失败动作数量读取错误。
	if scanErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rule_actions WHERE delivery_template_id=?`, templateID).Scan(&actionCount); scanErr != nil {
		t.Fatal(scanErr)
	}
	if ruleCount != 0 || actionCount != 0 {
		t.Fatalf("失败规则创建留下部分数据：rules=%d actions=%d", ruleCount, actionCount)
	}
}

// TestDeliveryTemplateWriteOrderingProtectsReferences 验证规则先提交、模板更新竞争和删除竞争都不会绕过模板引用保护。
func TestDeliveryTemplateWriteOrderingProtectsReferences(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存事务写入共用的上下文。
	ctx := context.Background()
	// userID、cookieID 保存规则和模板的归属信息。
	userID, cookieID := seedAccount(t, store)
	// templateID、templateErr 保存初始模板主键。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "顺序模板", Enabled: true, Messages: []string{"{{cards.old}}"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// cardID、cardErr 保存规则绑定所需的合法卡密组主键。
	cardID, cardErr := store.Cards.Create(ctx, &CardFull{UserID: userID, Name: "顺序测试卡密", Type: "text", TextContent: "测试内容", Enabled: true})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// createTx、createTxErr 保存先持有模板锁并创建规则的事务。
	createTx, createTxErr := store.DB.BeginTx(ctx, nil)
	if createTxErr != nil {
		t.Fatal(createTxErr)
	}
	// createInput 保存先提交规则所引用的当前模板契约。
	createInput := AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "ordered-item", Name: "先提交规则", TriggerType: "order_paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "old", CardID: cardID}}, Enabled: true, SortOrder: 1}}}
	// lockErr 保存规则事务取得模板锁的结果。
	if lockErr := validateAutomationTemplateContractsTx(ctx, createTx, DialectSQLite, userID, createInput.Actions); lockErr != nil {
		createTx.Rollback()
		t.Fatal(lockErr)
	}
	// _, insertErr 保存规则及动作在未提交事务中的写入结果。
	if _, insertErr := createAutomationRuleTx(ctx, createTx, DialectSQLite, createInput); insertErr != nil {
		createTx.Rollback()
		t.Fatal(insertErr)
	}
	// deleteResult 保存等待规则事务提交的模板删除调用结果。
	deleteResult := make(chan error, 1)
	go func() {
		// deleteErr 保存规则提交后模板删除的引用保护结果。
		deleteResult <- store.DeliveryTemplates.Delete(ctx, userID, templateID)
	}()
	// waitErr 保存规则未提交时删除调用的等待状态。
	select {
	// waitErr 保存规则提交前模板删除调用不应提前完成的错误结果。
	case waitErr := <-deleteResult:
		// waitErr 表示删除事务等待规则引用写入时返回的异常结果。
		createTx.Rollback()
		t.Fatalf("模板删除在规则提交前完成：%v", waitErr)
	case <-time.After(100 * time.Millisecond):
	}
	// commitErr 保存规则事务提交结果。
	if commitErr := createTx.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	// timeout 保存删除竞争的最长等待时间。
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	select {
	// deleteErr 保存规则提交后模板删除的最终引用保护错误。
	case deleteErr := <-deleteResult:
		if !errors.Is(deleteErr, ErrDeliveryTemplateReferenced) {
			t.Fatalf("规则提交后模板删除错误=%v want=%v", deleteErr, ErrDeliveryTemplateReferenced)
		}
	case <-timeout.C:
		t.Fatal("模板删除未在规则提交后结束")
	}

	// updateTemplateID、updateTemplateErr 保存另一组用于变量契约竞争的模板。
	updateTemplateID, updateTemplateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "更新顺序模板", Enabled: true, Messages: []string{"{{cards.old}}"}})
	if updateTemplateErr != nil {
		t.Fatal(updateTemplateErr)
	}
	// updateTx、updateTxErr 保存先更新模板契约且暂不提交的事务。
	updateTx, updateTxErr := store.DB.BeginTx(ctx, nil)
	if updateTxErr != nil {
		t.Fatal(updateTxErr)
	}
	// updateLockErr 保存模板更新事务取得模板锁的结果。
	if updateLockErr := lockLiveDeliveryTemplatesTx(ctx, updateTx, DialectSQLite, userID, []int64{updateTemplateID}); updateLockErr != nil {
		updateTx.Rollback()
		t.Fatal(updateLockErr)
	}
	// updateExecErr 保存模板消息契约替换结果。
	if _, updateExecErr := updateTx.ExecContext(ctx, `UPDATE delivery_template_messages SET content=? WHERE template_id=?`, "{{cards.new}}", updateTemplateID); updateExecErr != nil {
		updateTx.Rollback()
		t.Fatal(updateExecErr)
	}
	// staleResult 保存等待模板更新提交的旧变量规则创建结果。
	staleResult := make(chan error, 1)
	go func() {
		// staleErr 保存模板更新提交后旧变量规则的契约复核结果。
		_, staleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "stale-order", Name: "旧契约规则", TriggerType: "order_paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: updateTemplateID, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "old", CardID: cardID}}, Enabled: true, SortOrder: 1}}})
		staleResult <- staleErr
	}()
	// waitErr 保存模板更新未提交时规则创建的等待状态。
	select {
	// waitErr 保存模板更新提交前旧变量规则不应提前完成的错误结果。
	case waitErr := <-staleResult:
		// waitErr 表示规则事务等待模板契约更新时返回的异常结果。
		updateTx.Rollback()
		t.Fatalf("旧变量规则在模板更新提交前完成：%v", waitErr)
	case <-time.After(100 * time.Millisecond):
	}
	// updateCommitErr 保存模板更新提交结果。
	if updateCommitErr := updateTx.Commit(); updateCommitErr != nil {
		t.Fatal(updateCommitErr)
	}
	// timeout 保存旧变量规则竞争的最长等待时间。
	timeout = time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	select {
	// staleErr 保存模板更新提交后的旧变量规则错误结果。
	case staleErr := <-staleResult:
		if !errors.Is(staleErr, ErrDeliveryTemplateUnavailable) {
			t.Fatalf("模板更新后旧变量规则错误=%v want=%v", staleErr, ErrDeliveryTemplateUnavailable)
		}
	case <-timeout.C:
		t.Fatal("旧变量规则未在模板更新后结束")
	}
}

// TestAutomationRuleWriteRechecksTemplateVariableContract 验证模板先更新后，携带旧变量键的规则创建和更新都会被拒绝。
func TestAutomationRuleWriteRechecksTemplateVariableContract(t *testing.T) {
	// store、cleanup 保存变量契约并发后置条件测试使用的数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存模板和规则写入共用的上下文。
	ctx := context.Background()
	// userID、cookieID 保存规则归属与账号外键。
	userID, cookieID := seedAccount(t, store)
	// templateID、templateErr 保存初始变量契约为 old 的模板。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "变量契约模板", Enabled: true, Messages: []string{"{{cards.old}}"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// updateErr 保存模板先更新为 new 契约的结果。
	if updateErr := store.DeliveryTemplates.Update(ctx, userID, templateID, DeliveryTemplateInput{UserID: userID, Name: "变量契约模板", Enabled: true, Messages: []string{"{{cards.new}}"}}); updateErr != nil {
		t.Fatal(updateErr)
	}
	// staleInput 保存应用层在模板更新前完成校验、但尚未落库的旧规则输入。
	staleInput := AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "stale-create", Name: "旧变量创建", TriggerType: "order_paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, TemplateBindings: []DeliveryTemplateBinding{{VariableKey: "old", CardID: 1}}, Enabled: true, SortOrder: 1}}}
	// _, createErr 保存旧变量创建被事务内契约复核拒绝的结果。
	_, createErr := store.Automation.Create(ctx, staleInput)
	if !errors.Is(createErr, ErrDeliveryTemplateUnavailable) {
		t.Fatalf("旧变量创建错误=%v want=%v", createErr, ErrDeliveryTemplateUnavailable)
	}
	// ruleID、ruleErr 保存一个无模板动作的基础规则，供更新路径复核旧变量。
	ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "stale-update", Name: "旧变量更新", TriggerType: "order_paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_text", MessageTemplate: "占位", Enabled: true, SortOrder: 1}}})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// updateRuleErr 保存规则更新携带旧变量绑定时的事务内契约错误。
	updateRuleErr := store.Automation.Update(ctx, userID, ruleID, staleInput)
	if !errors.Is(updateRuleErr, ErrDeliveryTemplateUnavailable) {
		t.Fatalf("旧变量更新错误=%v want=%v", updateRuleErr, ErrDeliveryTemplateUnavailable)
	}
	// actionCount 保存失败更新后动作数量，验证旧的 send_text 没有被删除。
	var actionCount int
	// scanErr 保存失败规则动作数量读取错误。
	if scanErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rule_actions WHERE rule_id=?`, ruleID).Scan(&actionCount); scanErr != nil {
		t.Fatal(scanErr)
	}
	if actionCount != 1 {
		t.Fatalf("失败规则更新不应删除旧动作：count=%d", actionCount)
	}
}

// TestMultiDB_DeliveryTemplateReferenceContract 验证模板引用保护在所有已配置数据库方言中保持一致。
func TestMultiDB_DeliveryTemplateReferenceContract(t *testing.T) {
	// target 表示当前执行的数据库方言及其独立测试存储。
	for _, target := range allTestTargets(t) {
		// target 保存当前子测试闭包独占的数据库目标副本，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 保存当前方言模板引用测试共用的上下文。
			ctx := context.Background()
			// userID、cookieID 保存当前方言规则归属。
			userID, cookieID := seedAccount(t, target.store)
			// templateID、templateErr 保存当前方言有效模板。
			templateID, templateErr := target.store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "方言模板", Enabled: true, Messages: []string{"正文"}})
			if templateErr != nil {
				t.Fatal(templateErr)
			}
			// ruleID、ruleErr 保存有效模板引用规则。
			ruleID, ruleErr := target.store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "dialect-item", Name: "方言规则", TriggerType: "order_paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, Enabled: true, SortOrder: 1}}})
			if ruleErr != nil || ruleID <= 0 {
				t.Fatalf("创建有效规则失败：id=%d err=%v", ruleID, ruleErr)
			}
			// referencedErr 保存有效规则存在时的删除保护错误。
			if referencedErr := target.store.DeliveryTemplates.Delete(ctx, userID, templateID); !errors.Is(referencedErr, ErrDeliveryTemplateReferenced) {
				t.Fatalf("方言=%s 删除被引用模板错误=%v", target.name, referencedErr)
			}
			// _, missingErr 保存缺失模板引用的原子规则写入结果。
			_, missingErr := target.store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "missing-item", Name: "缺失模板规则", TriggerType: "order_paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID + 99999, Enabled: true, SortOrder: 1}}})
			if !errors.Is(missingErr, ErrDeliveryTemplateUnavailable) {
				t.Fatalf("方言=%s 缺失模板错误=%v", target.name, missingErr)
			}
		})
	}
}
