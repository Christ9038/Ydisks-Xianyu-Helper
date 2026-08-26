package db

import (
	"context"
	"reflect"
	"testing"
)

// TestPureBusinessHelpers 覆盖状态归一、批次终态和敏感键白名单等纯业务判断。
func TestPureBusinessHelpers(t *testing.T) {
	// statusCases 保存订单状态输入及其兼容查询候选值。
	statusCases := map[string][]string{
		"":             nil,
		"all":          nil,
		"1":            []string{"processing", "1"},
		"paid":         []string{"pending_ship", "paid", "2"},
		"3":            []string{"shipped", "3"},
		"completed":    []string{"completed", "4", "11"},
		"5":            []string{"refunding", "5", "7", "9"},
		"cancelled":    []string{"cancelled", "6", "8", "10", "12"},
		"unrecognized": []string{"unrecognized"},
	}
	// status、want 分别表示当前订单状态输入和期望的查询候选值。
	for status, want := range statusCases {
		// got 保存当前订单状态生成的 SQL 查询候选值。
		got := normalizedStatusCandidates(status)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("normalizedStatusCandidates(%q)=%v want %v", status, got, want)
		}
	}
	// ruleFilter 保存自动化规则查询的组合筛选条件。
	ruleFilter := AutomationRuleListFilter{UserID: 7, CookieID: "cookie", TriggerType: "paid", Search: "  Test  "}
	// whereSQL、args 保存筛选条件归一后的 SQL 片段和绑定参数。
	whereSQL, args := automationRuleWhere(ruleFilter)
	if whereSQL == "" || len(args) != 6 || args[0] != int64(7) || args[1] != "cookie" || args[2] != "paid" || args[3] != "%test%" {
		t.Fatalf("automation where=%q args=%v", whereSQL, args)
	}
	// allWhere、allArgs 保存仅按用户筛选时的 SQL 片段和绑定参数。
	allWhere, allArgs := automationRuleWhere(AutomationRuleListFilter{UserID: 7})
	if allWhere == "" || len(allArgs) != 1 || allArgs[0] != int64(7) {
		t.Fatalf("automation base where=%q args=%v", allWhere, allArgs)
	}
	// sensitiveKeys 保存业务允许进入敏感设置审计流程的完整键名集合。
	sensitiveKeys := SensitiveSettingKeys()
	if len(sensitiveKeys) != 4 || !IsSensitiveSettingKey(" AI_API_KEY ") || IsSensitiveSettingKey("normal_key") {
		t.Fatalf("sensitive keys=%v", sensitiveKeys)
	}
	// statusCasesForBatch 保存批次成功/失败计数与最终状态的映射。
	statusCasesForBatch := []struct {
		success int
		failed  int
		want    string
	}{
		{success: 2, failed: 0, want: "completed"},
		{success: 1, failed: 1, want: "partially_failed"},
		{success: 0, failed: 2, want: "failed"},
	}
	// statusCase 表示当前批次成功/失败计数与期望终态。
	for _, statusCase := range statusCasesForBatch {
		// got 保存当前批次计数计算出的终态。
		got := finalBatchStatus(statusCase.success, statusCase.failed)
		if got != statusCase.want {
			t.Fatalf("finalBatchStatus(%d,%d)=%q want %q", statusCase.success, statusCase.failed, got, statusCase.want)
		}
	}
}

// TestWSMessageFindInboundParsedJSONContaining 覆盖入站解密诊断帧的内容查询和默认限制。
func TestWSMessageFindInboundParsedJSONContaining(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// cookieID 保存诊断帧所属账号标识。
	_, cookieID := seedAccount(t, store)
	// addErr 保存批量写入诊断帧结果。
	addErr := store.WSMessages.AddBatch(ctx, []WSMessage{
		{CookieID: cookieID, Direction: "in", ParseStatus: "decrypted", ParsedJSON: `{"chat_id":"target"}`},
		{CookieID: cookieID, Direction: "in", ParseStatus: "decrypted", ParsedJSON: `{"chat_id":"other"}`},
		{CookieID: cookieID, Direction: "out", ParseStatus: "decrypted", ParsedJSON: `{"chat_id":"target"}`},
	})
	if addErr != nil {
		t.Fatal(addErr)
	}
	// matches、findErr 保存匹配入站诊断 JSON 和查询错误。
	matches, findErr := store.WSMessages.FindInboundParsedJSONContaining(ctx, cookieID, "target", 0)
	if findErr != nil || len(matches) != 1 || matches[0] != `{"chat_id":"target"}` {
		t.Fatalf("matches=%v err=%v", matches, findErr)
	}
	// limitedMatches、limitedErr 保存指定小分页查询结果和数据库错误。
	limitedMatches, limitedErr := store.WSMessages.FindInboundParsedJSONContaining(ctx, cookieID, "chat_id", 1)
	if limitedErr != nil || len(limitedMatches) != 1 {
		t.Fatalf("limited matches=%v err=%v", limitedMatches, limitedErr)
	}
}
