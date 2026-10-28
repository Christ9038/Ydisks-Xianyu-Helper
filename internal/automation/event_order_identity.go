package automation

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// orderIDPatterns 用于本次流程后续判断的订单IDPatterns
var orderIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`orderId[=:](\d{10,})`),
	regexp.MustCompile(`order_detail\?id=(\d{10,})`),
	regexp.MustCompile(`bizOrderId[=:](\d{10,})`),
	// 独立 id 参数必须位于查询参数边界，不能把 sid= 中的后缀误判为订单号。
	regexp.MustCompile(`(?:^|[?&])id=(\d{10,})(?:[&#]|$)`),
}

// extractOrderRoleFromContent 封装extract订单RoleFrom内容业务协调。
func extractOrderRoleFromContent(contentJSON string) string {
	// c 用于本次流程后续判断的c
	var c map[string]any
	if json.Unmarshal([]byte(contentJSON), &c) != nil {
		return ""
	}
	// path 表示当前遍历过程中的路径
	for _, path := range [][]string{
		{"dxCard", "item", "main", "exContent", "button", "targetUrl"},
		{"dxCard", "item", "main", "targetUrl"},
		{"dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "button", "targetUrl"},
	} {
		if // role 用于本次流程后续判断的role
		role := orderRoleFromURL(nestedString(c, path...)); role != "" {
			return role
		}
	}
	return ""
}

// orderRoleFromURL 封装订单RoleFromURL业务协调。
func orderRoleFromURL(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "fleamarket://") {
		rawURL = "https://local.invalid/" + strings.TrimPrefix(rawURL, "fleamarket://")
	}
	// u、err 用于本次流程后续判断的u、err
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return normalizedOrderRole(u.Query().Get("role"))
}

// normalizedOrderRole 只接受平台协议中可识别的买卖双方角色，未知值不得影响事件归属判断。
func normalizedOrderRole(value string) string {
	// role 是去除大小写和空白差异后的平台角色字段。
	role := strings.ToLower(strings.TrimSpace(value))
	switch role {
	case "seller", "buyer":
		return role
	default:
		return ""
	}
}

// bizTaskName 封装biz任务名称业务协调。
func bizTaskName(raw string) string {
	// tag 用于本次流程后续判断的tag
	var tag map[string]any
	if json.Unmarshal([]byte(raw), &tag) != nil {
		return ""
	}
	return strAny(tag["taskName"])
}

// orderRoleFromTaskName 封装订单RoleFrom任务名称业务协调。
func orderRoleFromTaskName(taskName string) string {
	switch {
	case strings.Contains(taskName, "买家"):
		return "buyer"
	case strings.Contains(taskName, "卖家"):
		return "seller"
	default:
		return ""
	}
}

// matchOrderID 封装match订单ID业务协调。
func matchOrderID(s string) string {
	// re 表示当前遍历过程中的re
	for _, re := range orderIDPatterns {
		if // m 用于本次流程后续判断的m
		m := re.FindStringSubmatch(s); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// extractOrderIDFromContent 封装extract订单IDFrom内容业务协调。
func extractOrderIDFromContent(contentJSON string) string {
	// c 用于本次流程后续判断的c
	var c map[string]any
	if json.Unmarshal([]byte(contentJSON), &c) != nil {
		return ""
	}
	// path 表示当前遍历过程中的路径
	for _, path := range [][]string{
		{"dxCard", "item", "main", "exContent", "button", "targetUrl"},
		{"dxCard", "item", "main", "targetUrl"},
		{"dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "button", "targetUrl"},
	} {
		if // id 用于本次流程后续判断的标识
		id := matchOrderID(nestedString(c, path...)); id != "" {
			return id
		}
	}
	return ""
}
