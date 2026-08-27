package automation

import (
	"encoding/json"
	"fmt"
	"strings"
)

// isJSONObject 判断配置是否为 JSON 对象。
func isJSONObject(raw string) bool {
	// value 是 JSON 对象解析结果，仅用于确认配置顶层类型，不保存业务状态。
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil
}

// defaultRuleName 根据触发类型和商品标识生成默认规则名称。
func defaultRuleName(triggerType, itemID string) string {
	// name 是按触发类型选择的默认显示名称，必要时再附加商品标识。
	name := map[string]string{TriggerOrderCreated: "拍下未付款自动改价", TriggerOrderPaid: "付款后自动发货", TriggerBuyerReviewed: "评价后发送赠品", TriggerReviewMissingTimeout: "超时未评价求评价"}[triggerType]
	if name == "" {
		name = "自动化规则"
	}
	if strings.TrimSpace(itemID) != "" {
		return fmt.Sprintf("%s - %s", name, strings.TrimSpace(itemID))
	}
	return name
}

// firstRuleNonZero 返回动作顺序或其默认下标。
func firstRuleNonZero(value, fallback int) int {
	if value != 0 {
		return value
	}
	return fallback
}
