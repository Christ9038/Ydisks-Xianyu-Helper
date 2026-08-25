package server

import (
	"strings"
	"time"
)

// normalizeOrderTimestamp 将数据库保存的 UTC 时间文本转换为带明确时区的 RFC3339 时间。
// 数据库中的无时区文本按 UTC 解释；无法识别的历史值原样返回，避免隐藏或破坏旧数据。
func normalizeOrderTimestamp(raw string) string {
	// value 是去除首尾空白后的订单时间文本。
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	// layouts 是兼容数据库旧格式和已带时区时间的解析模板集合。
	layouts := []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"}
	// layout 是当前尝试解析订单时间的模板。
	for _, layout := range layouts {
		// parsed 是按数据库 UTC 约定或原始时区解析后的时间值。
		var parsed time.Time
		// parseErr 保存当前模板解析失败的原因；失败时继续尝试兼容模板。
		var parseErr error
		if strings.Contains(layout, "Z07:00") {
			parsed, parseErr = time.Parse(layout, value)
		} else {
			parsed, parseErr = time.ParseInLocation(layout, value, time.UTC)
		}
		if parseErr == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return raw
}
