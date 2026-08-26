package deliverytemplate

import "testing"

// TestParseExtractsKeysAndRejectsUnknownVariables 验证变量提取顺序和非法语法拒绝。
func TestParseExtractsKeysAndRejectsUnknownVariables(t *testing.T) {
	// emptyErr 保存空模板解析的业务错误。
	if _, emptyErr := Parse(nil); emptyErr == nil {
		t.Fatal("empty messages should be rejected")
	}
	// blankErr 保存空白消息解析的业务错误。
	if _, blankErr := Parse([]string{"  "}); blankErr == nil {
		t.Fatal("blank message should be rejected")
	}
	// parsed 保存合法模板的解析结果。
	// err 保存模板解析测试失败原因。
	parsed, err := Parse([]string{"A {{cards.main}} {{custom.vip}}", "B {{cards.bonus}} {{cards.main}} {{custom.region}}"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.Keys) != 2 || parsed.Keys[0] != "main" || parsed.Keys[1] != "bonus" {
		t.Fatalf("keys=%v", parsed.Keys)
	}
	if len(parsed.CustomKeys) != 2 || parsed.CustomKeys[0] != "vip" || parsed.CustomKeys[1] != "region" {
		t.Fatalf("custom keys=%v", parsed.CustomKeys)
	}
	// parsed、err 保存固定变量语法校验结果。
	parsed, err = Parse([]string{"{{buyer_nickname}} {{order_id}} {{buyer_id}} {{card_name}}"})
	if err != nil || len(parsed.Messages) != 1 {
		t.Fatalf("fixed variables parse: parsed=%+v err=%v", parsed, err)
	}
	// err 保存非法变量解析结果，测试期望该错误存在。
	if _, err := Parse([]string{"{{other.value}}"}); err == nil {
		t.Fatal("expected unsupported variable error")
	}
	// unclosedErr 保存未闭合变量解析的业务错误。
	if _, unclosedErr := Parse([]string{"{{cards.main"}); unclosedErr == nil {
		t.Fatal("unclosed variable should be rejected")
	}
	// legacyParsed 保存旧版 delivery 前缀模板的兼容解析结果。
	legacyParsed, err := Parse([]string{"{{delivery.cards.main}} {{delivery.custom.0}}"})
	if err != nil || len(legacyParsed.Keys) != 1 || len(legacyParsed.CustomKeys) != 1 || legacyParsed.CustomKeys[0] != "0" {
		t.Fatalf("legacy variables parse: parsed=%+v err=%v", legacyParsed, err)
	}
}

// TestReplaceRendersAllDeliveryVariables 验证订单、卡密和规则数组变量可以共同渲染。
func TestReplaceRendersAllDeliveryVariables(t *testing.T) {
	// got 保存完整模板渲染后的消息。
	got := Replace("{{buyer_nickname}}/{{order_id}}/{{buyer_id}}/{{card_name}}/{{cards.main}}/{{custom.region}}", VariableValues{
		BuyerNickname: "小鱼",
		OrderID:       "order-1",
		BuyerID:       "buyer-1",
		CardName:      "主库存",
		CardValues:    map[string]string{"main": "ABC"},
		CustomValues:  map[string]string{"region": "华东"},
	})
	if got != "小鱼/order-1/buyer-1/主库存/ABC/华东" {
		t.Fatalf("Replace=%q", got)
	}
}

// TestReplaceCardsPreservesUnknownKey 验证替换只影响已有绑定值的变量。
func TestReplaceCardsPreservesUnknownKey(t *testing.T) {
	// got 保存卡密变量替换后的文本。
	got := ReplaceCards("主卡 {{cards.main}} / 未绑定 {{cards.other}}", map[string]string{"main": "ABC"})
	if got != "主卡 ABC / 未绑定 {{cards.other}}" {
		t.Fatalf("ReplaceCards=%q", got)
	}
}

// TestReplacePreservesUnboundVariables 验证未提供绑定值的卡密和自定义变量保持原始文本。
func TestReplacePreservesUnboundVariables(t *testing.T) {
	// got 保存缺少绑定值时的模板渲染结果。
	got := Replace("{{cards.main}}/{{custom.region}}", VariableValues{})
	if got != "{{cards.main}}/{{custom.region}}" {
		t.Fatalf("Replace=%q", got)
	}
}
