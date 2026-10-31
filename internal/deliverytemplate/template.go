// Package deliverytemplate 负责解析和渲染可复用的发货消息模板。
package deliverytemplate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// cardVariablePattern 匹配模板中按名称绑定卡密库存的变量。
var cardVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?cards\.([A-Za-z0-9_-]+)\}\}`)

// builtinVariablePattern 匹配由订单和买家事实直接提供的模板变量。
var builtinVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?(buyer_nickname|order_id|buyer_id|card_name)\}\}`)

// customVariablePattern 匹配发货规则传入的字符串键值变量。
var customVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?custom\.([A-Za-z0-9_-]+)\}\}`)

// fullVariablePattern 只匹配完整且受支持的模板变量令牌。
var fullVariablePattern = regexp.MustCompile(`^\{\{(?:delivery\.)?(?:cards\.[A-Za-z0-9_-]+|custom\.[A-Za-z0-9_-]+|buyer_nickname|order_id|buyer_id|card_name)\}\}$`)

// imageTokenPattern 匹配整条消息就是一张图片库图片的令牌，例如 {{library_image:12}}。
var imageTokenPattern = regexp.MustCompile(`^\{\{library_image:([1-9][0-9]{0,17})\}\}$`)

// ImageToken 返回引用图片库图片的整条图片消息令牌。
func ImageToken(imageID int64) string {
	return "{{library_image:" + strconv.FormatInt(imageID, 10) + "}}"
}

// ImageID 判断一条模板消息是否是图片库图片消息；只有整条消息恰好是图片令牌时才成立，不能与文字混排。
func ImageID(message string) (int64, bool) {
	// match 保存去除首尾空白后的整条消息与图片令牌的匹配结果。
	match := imageTokenPattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 2 {
		return 0, false
	}
	// imageID、err 保存令牌中的图片主键及解析错误。
	imageID, err := strconv.ParseInt(match[1], 10, 64)
	return imageID, err == nil
}

// Parsed 保存校验后的消息副本、卡密变量键和自定义变量键。
type Parsed struct {
	// Messages 是模板保存时应保留的有序消息内容。
	Messages []string
	// Keys 是模板需要外部绑定卡密组的变量键。
	Keys []string
	// CustomKeys 是模板需要规则提供的自定义变量键，按消息首次出现顺序排列。
	CustomKeys []string
	// ImageIDs 是模板中图片消息引用的图片库图片主键，按消息顺序排列，可含重复项。
	ImageIDs []int64
}

// CardKeys 提取单条消息中按首次出现顺序使用的卡密变量键。
func CardKeys(message string) []string {
	// seen 保存已经提取的变量键，避免同一消息重复消费库存。
	seen := make(map[string]bool)
	// keys 保存当前消息需要的卡密变量键。
	keys := make([]string, 0)
	for /* match 表示当前卡密变量的正则匹配结果。 */ _, match := range cardVariablePattern.FindAllStringSubmatch(message, -1) {
		if len(match) == 2 && !seen[match[1]] {
			seen[match[1]] = true
			keys = append(keys, match[1])
		}
	}
	return keys
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
		// imageID、isImage 表示该消息是否是整条图片库图片消息；图片消息没有任何变量，直接进入下一条。
		if imageID, isImage := ImageID(message); isImage {
			parsed.ImageIDs = append(parsed.ImageIDs, imageID)
			continue
		}
		// offset 表示当前消息从左到右扫描双大括号令牌的字节位置。
		for offset := 0; offset < len(message); {
			// nextOpen、nextClose 分别保存下一个开放和闭合标记的相对位置。
			nextOpen := strings.Index(message[offset:], "{{")
			// nextClose 保存下一个闭合双大括号的相对位置。
			nextClose := strings.Index(message[offset:], "}}")
			if nextClose >= 0 && (nextOpen < 0 || nextClose < nextOpen) {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含未匹配闭合标记", index+1)
			}
			if nextOpen < 0 {
				break
			}
			// markerStart 保存当前开放标记的绝对字节位置。
			markerStart := offset + nextOpen
			// closingOffset 保存从开放标记后找到的闭合标记相对位置。
			closingOffset := strings.Index(message[markerStart+2:], "}}")
			if closingOffset < 0 {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含未闭合变量", index+1)
			}
			// tokenEnd 是当前变量令牌闭合符之后的下一个字节位置。
			tokenEnd := markerStart + 2 + closingOffset + 2
			// token 是需要按完整语法校验并提取变量键的完整令牌。
			token := message[markerStart:tokenEnd]
			if !fullVariablePattern.MatchString(token) {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含非法变量", index+1)
			}
			// cardMatch 保存当前令牌的卡密变量匹配结果。
			if cardMatch := cardVariablePattern.FindStringSubmatch(token); len(cardMatch) == 2 {
				// key 是当前消息中识别出的卡密变量键。
				key := cardMatch[1]
				if !seen[key] {
					seen[key] = true
					parsed.Keys = append(parsed.Keys, key)
				}
			}
			// customMatch 保存当前令牌的自定义变量匹配结果。
			if customMatch := customVariablePattern.FindStringSubmatch(token); len(customMatch) == 2 {
				// key 保存自定义变量在规则键值表中的名称。
				key := customMatch[1]
				if !customSeen[key] {
					customSeen[key] = true
					parsed.CustomKeys = append(parsed.CustomKeys, key)
				}
			}
			offset = tokenEnd
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
