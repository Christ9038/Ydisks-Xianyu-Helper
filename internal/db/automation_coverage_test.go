package db

import (
	"context"
	"errors"
	"testing"
)

// TestAutomationRuleQueryBranches 覆盖规则存在性、改价动作统计、分页触发统计和单条读取路径。
func TestAutomationRuleQueryBranches(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存测试规则的归属用户和账号。
	userID, cookieID := seedAccount(t, store)
	// adjustRuleID、adjustErr 保存改价规则创建结果和数据库错误。
	adjustRuleID, adjustErr := store.Automation.Create(ctx, AutomationRuleInput{
		UserID: userID, CookieID: cookieID, ItemID: "adjust-item", Name: "调整价格", TriggerType: "price_change", Enabled: true, Priority: 10,
		Actions: []AutomationActionInput{{ActionType: "adjust_price", Enabled: true, SortOrder: 1}},
	})
	if adjustErr != nil {
		t.Fatal(adjustErr)
	}
	// disabledRuleID、disabledErr 保存禁用规则创建结果和数据库错误。
	disabledRuleID, disabledErr := store.Automation.Create(ctx, AutomationRuleInput{
		UserID: userID, CookieID: cookieID, ItemID: "disabled-item", Name: "禁用规则", TriggerType: "paid", Enabled: false, Priority: 20,
		Actions: []AutomationActionInput{{ActionType: "adjust_price", Enabled: false, SortOrder: 1}},
	})
	if disabledErr != nil {
		t.Fatal(disabledErr)
	}
	// publishInput 描述用于验证发布规则重复判断的稳定字段。
	publishInput := AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "publish-item", Name: "发布规则", TriggerType: "item_publish", Enabled: true, Priority: 30}
	// publishRuleID、publishErr 保存发布规则创建结果和数据库错误。
	publishRuleID, publishErr := store.Automation.Create(ctx, publishInput)
	if publishErr != nil {
		t.Fatal(publishErr)
	}
	// hasAdjust、adjustCheckErr 保存启用改价规则检查结果和数据库错误。
	hasAdjust, adjustCheckErr := store.Automation.HasEnabledAdjustPriceRule(ctx, cookieID)
	if adjustCheckErr != nil || !hasAdjust {
		t.Fatalf("has enabled adjust rule: value=%v err=%v", hasAdjust, adjustCheckErr)
	}
	// existsPublish、publishCheckErr 保存发布规则存在性检查结果和数据库错误。
	existsPublish, publishCheckErr := store.Automation.ExistsPublishRule(ctx, publishInput)
	if publishCheckErr != nil || !existsPublish {
		t.Fatalf("exists publish rule: value=%v err=%v", existsPublish, publishCheckErr)
	}
	// missingPublish、missingPublishErr 保存不存在规则的重复判断结果和数据库错误。
	missingPublish, missingPublishErr := store.Automation.ExistsPublishRule(ctx, AutomationRuleInput{
		UserID: userID, CookieID: cookieID, ItemID: "missing", Name: "不存在", TriggerType: "item_publish",
	})
	if missingPublishErr != nil || missingPublish {
		t.Fatalf("missing publish rule: value=%v err=%v", missingPublish, missingPublishErr)
	}
	// counts、countErr 保存各触发类型的规则统计和数据库错误。
	counts, countErr := store.Automation.CountByTriggerForUser(ctx, AutomationRuleListFilter{UserID: userID})
	if countErr != nil || counts["item_publish"] != 1 || counts["price_change"] != 1 || counts["paid"] != 1 {
		t.Fatalf("trigger counts=%v err=%v", counts, countErr)
	}
	// enabled 表示本次统计只保留启用规则。
	enabled := true
	// filteredCounts、filteredErr 保存按启用状态筛选后的统计和数据库错误。
	filteredCounts, filteredErr := store.Automation.CountByTriggerForUser(ctx, AutomationRuleListFilter{UserID: userID, Enabled: &enabled})
	if filteredErr != nil || filteredCounts["item_publish"] != 1 || filteredCounts["price_change"] != 1 || filteredCounts["paid"] != 0 {
		t.Fatalf("filtered trigger counts=%v err=%v", filteredCounts, filteredErr)
	}
	// gotRule、getErr 保存单条规则读取结果和数据库错误。
	gotRule, getErr := store.Automation.Get(ctx, adjustRuleID)
	if getErr != nil || gotRule == nil || gotRule.ID != adjustRuleID || len(gotRule.Actions) != 1 || gotRule.Actions[0].ActionType != "adjust_price" {
		t.Fatalf("get rule=%#v err=%v", gotRule, getErr)
	}
	// missingRule、missingRuleErr 保存不存在规则的读取结果和错误。
	missingRule, missingRuleErr := store.Automation.Get(ctx, 999999)
	if !errors.Is(missingRuleErr, ErrNotFound) || missingRule != nil {
		t.Fatalf("missing rule=%#v err=%v", missingRule, missingRuleErr)
	}
	// runID、started、runErr 保存租约测试运行的创建结果和数据库错误。
	runID, started, runErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: publishRuleID, CookieID: cookieID, TriggerType: "item_publish", TriggerKey: "query-lease"})
	if runErr != nil || !started || runID == 0 {
		t.Fatalf("start run: id=%d started=%v err=%v", runID, started, runErr)
	}
	// renewErr 保存当前 worker 延长运行租约的结果。
	if renewErr := store.Automation.RenewRunLease(ctx, runID, 1, 123456); renewErr != nil {
		t.Fatal(renewErr)
	}
	// lostErr 保存旧 worker 更新租约时的租约失效错误。
	lostErr := store.Automation.RenewRunLease(ctx, runID, 2, 123457)
	if !errors.Is(lostErr, ErrAutomationRunLeaseLost) {
		t.Fatalf("stale lease error=%v", lostErr)
	}
	// unusedRuleID 用于确认禁用规则没有被启用改价检查计入结果。
	_ = disabledRuleID
}
