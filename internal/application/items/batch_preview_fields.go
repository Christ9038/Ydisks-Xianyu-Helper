package items

import (
	"fmt"
	"strconv"
	"strings"
)

// normalizeHeaders 将用户表头映射为稳定的应用字段名。
func normalizeHeaders(headers []string) []string {
	// keys 保存归一化后的字段名。
	keys := make([]string, len(headers))
	// index 和 header 表示表头下标及原始文本。
	for index, header := range headers {
		keys[index] = normalizeHeader(header)
	}
	return keys
}

// rowMap 将一行单元格按表头映射为字符串字段。
func rowMap(keys, values []string) (map[string]any, bool) {
	// result 保存表头到单元格值的映射。
	result := map[string]any{}
	// nonEmpty 表示当前行是否包含有效文本。
	nonEmpty := false
	// index 和 key 表示当前列下标及归一化字段名。
	for index, key := range keys {
		if key == "" || index >= len(values) {
			continue
		}
		// value 保存当前单元格的裁剪后文本。
		value := strings.TrimSpace(values[index])
		if value != "" {
			nonEmpty = true
		}
		result[key] = value
	}
	return result, nonEmpty
}

// normalizeHeader 将中英文表头归一化为应用字段名。
func normalizeHeader(header string) string {
	// value 保存去除空白和分隔符后的表头文本。
	value := strings.ToLower(strings.TrimSpace(header))
	value = strings.NewReplacer(" ", "", "_", "", "-", "", "（", "(", "）", ")").Replace(value)
	switch value {
	case "cookieid", "账号id", "账号", "闲鱼账号":
		return "cookie_id"
	case "title", "itemtitle", "标题", "商品标题", "商品名称":
		return "title"
	case "description", "desc", "itemdescription", "描述", "商品描述", "商品详情":
		return "description"
	case "price", "itemprice", "价格", "商品价格":
		return "price"
	case "originalprice", "原价":
		return "original_price"
	case "quantity", "库存", "数量":
		return "quantity"
	case "postagemode", "邮费模式":
		return "postage_mode"
	case "postage", "邮费":
		return "postage"
	case "images", "image", "图片", "商品图片":
		return "images"
	case "categoryid", "catid", "类目id", "商品类目id":
		return "category_id"
	case "categoryname", "catname", "类目名称", "商品类目名称", "类目":
		return "category_name"
	case "channelcategoryid", "channelcatid", "频道类目id":
		return "channel_category_id"
	case "tbcategoryid", "tbcatid", "淘宝类目id":
		return "tb_category_id"
	case "paiddeliveryenabled", "付款发货启用", "付款后自动发货":
		return "paid_delivery_enabled"
	case "paiddeliverycontents", "付款发货内容", "付款后发送的卡密":
		return "paid_delivery_contents"
	case "reviewgiftenabled", "评价赠品启用", "评价后发送赠品":
		return "review_gift_enabled"
	case "reviewgiftcontents", "评价赠品内容", "评价后发送的卡密":
		return "review_gift_contents"
	case "reviewrequestenabled", "求评价启用", "超时未评价时提醒":
		return "review_request_enabled"
	case "reviewrequestafterhours", "求评价等待小时", "发货几小时后提醒":
		return "review_request_after_hours"
	case "reviewrequestmessage", "求评价文案", "提醒内容":
		return "review_request_message"
	case "reviewrequestmaxattempts", "求评价最多次数", "最多提醒几次":
		return "review_request_max_attempts"
	case "reviewrequestdelayseconds", "求评价延迟秒":
		return "review_request_delay_seconds"
	default:
		return strings.TrimSpace(header)
	}
}

// firstString 从字段映射中返回首个非空字符串。
func firstString(fields map[string]any, keys ...string) string {
	// key 是当前按优先级读取的候选字段名。
	for _, key := range keys {
		// value、ok 分别是字段原始值和其是否存在于当前行。
		if value, ok := fields[key]; ok {
			// text 是字段统一转换后的候选非空文本。
			if text := stringValue(value); strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

// stringValue 将常见表格值转换为文本。
func stringValue(value any) string {
	// typed 是当前字段值断言后的具体基础类型。
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}
