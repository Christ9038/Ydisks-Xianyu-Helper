// Package deliverytemplate 负责解析和渲染可复用的发货消息模板。
package deliverytemplate

import (
	"fmt"
	"regexp"
	"strings"
)

// cardVariablePattern 匹配模板中按名称绑定卡密库存的变量。
var cardVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?cards\.([A-Za-z0-9_-]+)\}\}`)

// builtinVariablePattern 匹配由订单和买家事实直接提供的模板变量。
var builtinVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?(buyer_nickname|order_id|buyer_id|card_name)\}\}`)

// customVariablePattern 匹配发货规则传入的字符串键值变量。
var customVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?custom\.([A-Za-z0-9_-]+)\}\}`)

// anyDoubleBracePattern 检测消息中可能误写的其他双大括号标记。
var anyDoubleBracePattern = regexp.MustCompile(`\{\{|\}\}`)

// Parsed 保存校验后的消息副本、卡密变量键和自定义变量键。
type Parsed struct {
	// Messages 是模板保存时应保留的有序消息内容。
	Messages []string
	// Keys 是模板需要外部绑定卡密组的变量键。
	Keys []string
	// CustomKeys 是模板需要规则提供的自定义变量键，按消息首次出现顺序排列。
	CustomKeys []string
}

// Parse 校验消息非空并提取所有受支持的模板变量。
func Parse(messages []string) (Parsed, error) {
	if len(messages) == 0 {
		return Parsed{}, fmt.Errorf("发货模板至少需要一条消息")
	}
	// parsed 保存不共享调用方切片的模板内容，避免后续修改影响已校验结果。
	parsed := Parsed{Messages: make([]string, len(messages))}
	// seen 记录已经提取的变量键，保证 Keys 只按首次使用顺序出现一次。
	seen := map[string]bool{}
	// customSeen 记录已经出现的自定义变量键，避免重复展示规则输入。
	customSeen := map[string]bool{}
	for /* index 表示消息顺序；message 表示当前消息正文。 */ index, message := range messages {
		if strings.TrimSpace(message) == "" {
			return Parsed{}, fmt.Errorf("发货模板第 %d 条消息不能为空", index+1)
		}
		parsed.Messages[index] = message
		for /* match 表示当前卡密变量的正则匹配结果。 */ _, match := range cardVariablePattern.FindAllStringSubmatch(message, -1) {
			// key 是当前消息中识别出的卡密变量键。
			key := match[1]
			if !seen[key] {
				seen[key] = true
				parsed.Keys = append(parsed.Keys, key)
			}
		}
		for /* match 表示当前自定义变量的正则匹配结果。 */ _, match := range customVariablePattern.FindAllStringSubmatch(message, -1) {
			// key 保存自定义变量在规则键值表中的名称。
			key := match[1]
			if !customSeen[key] {
				customSeen[key] = true
				parsed.CustomKeys = append(parsed.CustomKeys, key)
			}
		}
		for /* match 表示当前双大括号标记的位置。 */ _, match := range anyDoubleBracePattern.FindAllStringIndex(message, -1) {
			// markerStart 是当前双大括号标记在消息中的字节位置。
			markerStart := match[0]
			if markerStart+2 <= len(message) && message[markerStart:markerStart+2] == "{{" {
				// closingOffset 是从变量起点之后搜索到的闭合标记位置。
				closingOffset := strings.Index(message[markerStart+2:], "}}")
				if closingOffset < 0 {
					return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含未闭合变量", index+1)
				}
				// token 是需要按完整语法再次校验的变量文本。
				token := message[markerStart : markerStart+2+closingOffset+2]
				if !cardVariablePattern.MatchString(token) && !builtinVariablePattern.MatchString(token) && !customVariablePattern.MatchString(token) {
					return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含非法变量", index+1)
				}
			}
		}
	}
	return parsed, nil
}

// VariableValues 是渲染一条发货模板消息所需的非敏感业务值。
type VariableValues struct {
	// BuyerNickname 是购买用户昵称，缺失时按空字符串渲染。
	BuyerNickname string
	// OrderID 是订单号。
	OrderID string
	// BuyerID 是买家 ID。
	BuyerID string
	// CardName 是模板绑定的卡密库存名称。
	CardName string
	// CardValues 是模板卡密变量键对应的已取货内容。
	CardValues map[string]string
	// CustomValues 是发货规则传入的自定义字符串键值表。
	CustomValues map[string]string
}

// Replace 将模板支持的变量替换为当前订单和规则上下文中的值。
func Replace(message string, values VariableValues) string {
	// fixedValues 保存固定变量令牌和当前任务字段的对应关系。
	fixedValues := map[string]string{
		"buyer_nickname": values.BuyerNickname,
		"order_id":       values.OrderID,
		"buyer_id":       values.BuyerID,
		"card_name":      values.CardName,
	}
	// out 保存逐类替换后的消息正文。
	out := builtinVariablePattern.ReplaceAllStringFunc(message, func(token string) string {
		// match 保存当前固定变量的结构化匹配结果。
		match := builtinVariablePattern.FindStringSubmatch(token)
		return fixedValues[match[1]]
	})
	out = cardVariablePattern.ReplaceAllStringFunc(out, func(token string) string {
		// match 保存当前卡密变量的结构化匹配结果。
		match := cardVariablePattern.FindStringSubmatch(token)
		// value、ok 分别保存卡密正文和变量是否存在绑定值。
		if value, ok := values.CardValues[match[1]]; ok {
			return value
		}
		return token
	})
	return customVariablePattern.ReplaceAllStringFunc(out, func(token string) string {
		// match 保存当前自定义变量的结构化匹配结果。
		match := customVariablePattern.FindStringSubmatch(token)
		// value、ok 分别保存自定义字符串和变量是否存在绑定值。
		if value, ok := values.CustomValues[match[1]]; ok {
			return value
		}
		return token
	})
}

// ReplaceCards 兼容旧调用方，只替换已经提供绑定值的卡密变量。
func ReplaceCards(message string, values map[string]string) string {
	return Replace(message, VariableValues{CardValues: values})
}
