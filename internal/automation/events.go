// Package automation 实现自动化处理中心。
//
// 重要边界：
//   - engine 只负责 WS 消息连接和分流，不在分流层判断业务规则。
//   - 用户消息进入关键词/AI 回复链；系统卡片和平台通知只进入自动化中心。
//   - 自动化中心把 WS 事件、计划任务、后台手动任务统一转换为 Task，再匹配规则和执行动作。
package automation

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"xianyu-go/internal/db"
)

const (
	TriggerOrderPaid            = "order_paid"
	TriggerBuyerReviewed        = "buyer_reviewed"
	TriggerReviewMissingTimeout = "review_missing_timeout"

	ActionConfirmShipment = "confirm_shipment"
	ActionSendCard        = "send_card"
	ActionSendText        = "send_text"
)

// Task 是自动化中心的统一输入。它可以来自 WS 系统事件、计划任务或手动触发。
type Task struct {
	Source      string // ws/scheduler/manual
	AccountID   string
	CookieStr   string
	TriggerType string
	ChatID      string
	OrderID     string
	ItemID      string
	BuyerID     string
	SpecName    string
	SpecValue   string
	Quantity    string
	Amount      string
	OrderStatus string
	Text        string
	UpdateKey   string
	// ForceConfirmShipment 仅供明确的人工“完整发货”使用；自动事件仍遵循账号自动确认开关。
	ForceConfirmShipment bool
	// ActionPlan 是运行创建时冻结的动作计划。延迟恢复和失败重试必须使用该快照，
	// 不能把数字游标应用到管理员后来修改过的规则上。
	ActionPlan []db.AutomationAction
	Raw        map[string]any
}

// OrderDetail 是自动化中心执行交易类任务前需要补齐的订单事实。
// 规格和数量来自闲鱼订单，不由自动化规则修改商品属性。
type OrderDetail struct {
	Quantity    string
	SpecName    string
	SpecValue   string
	Amount      string
	OrderStatus string
}

// ExtractTaskFromWS 从一条解密后的 WS 消息中提取系统事件。
// 这里只做事实解析：识别平台告诉了我们什么；是否执行自动化由 Center 根据规则决定。
func ExtractTaskFromWS(accountID, cookieStr string, raw map[string]any) *Task {
	if raw == nil {
		return nil
	}
	f := fieldsFromRaw(raw)
	if f.text == "" && f.redReminder == "" && f.updateKey == "" {
		return nil
	}
	task := &Task{
		Source:    "ws",
		AccountID: accountID,
		CookieStr: cookieStr,
		ChatID:    f.chatID,
		OrderID:   f.orderID,
		ItemID:    f.itemID,
		BuyerID:   f.buyerID,
		Text:      firstNonEmpty(f.text, f.redReminder),
		UpdateKey: f.updateKey,
		Raw:       raw,
	}
	switch {
	case isOrderPaidEvent(f):
		task.TriggerType = TriggerOrderPaid
	case isBuyerReviewedEvent(f):
		task.TriggerType = TriggerBuyerReviewed
	default:
		return nil
	}
	return task
}

type rawFields struct {
	text        string
	redReminder string
	title       string
	detail      string
	orderRole   string
	updateKey   string
	contentType string
	chatID      string
	orderID     string
	itemID      string
	buyerID     string
	reminderURL string
}

func fieldsFromRaw(raw map[string]any) rawFields {
	var f rawFields
	if m1 := mapAt(raw, "1"); m1 != nil {
		if s := strAny(m1["2"]); s != "" {
			f.chatID = trimGoofishSID(s)
		}
		if m10 := mapAt(m1, "10"); m10 != nil {
			f.text = strAny(m10["reminderContent"])
			f.redReminder = strAny(m10["redReminder"])
			f.title = strAny(m10["reminderTitle"])
			f.detail = strAny(m10["detailNotice"])
			f.reminderURL = strAny(m10["reminderUrl"])
			f.buyerID = strAny(m10["senderUserId"])
			f.updateKey, f.contentType = extFields(strAny(m10["extJson"]))
			f.orderRole = orderRoleFromTaskName(bizTaskName(strAny(m10["bizTag"])))
		}
		if contentJSON := nestedString(raw, "1", "6", "3", "5"); contentJSON != "" {
			if role := extractOrderRoleFromContent(contentJSON); role != "" {
				f.orderRole = role
			}
			if id := extractOrderIDFromContent(contentJSON); id != "" {
				f.orderID = id
			}
		}
	}
	if m3 := mapAt(raw, "3"); m3 != nil {
		if f.redReminder == "" {
			f.redReminder = strAny(m3["redReminder"])
		}
	}
	if m4 := mapAt(raw, "4"); m4 != nil {
		if f.text == "" {
			f.text = strAny(m4["reminderContent"])
		}
		if f.redReminder == "" {
			f.redReminder = strAny(m4["redReminder"])
		}
		if f.title == "" {
			f.title = strAny(m4["reminderTitle"])
		}
		if f.detail == "" {
			f.detail = strAny(m4["detailNotice"])
		}
		if f.reminderURL == "" {
			f.reminderURL = strAny(m4["reminderUrl"])
		}
		if f.updateKey == "" {
			f.updateKey, f.contentType = extFields(strAny(m4["extJson"]))
		}
	}
	if f.updateKey != "" {
		chatID, orderID := parseUpdateKey(f.updateKey)
		if f.chatID == "" {
			f.chatID = chatID
		}
		if f.orderID == "" {
			f.orderID = orderID
		}
	}
	if f.reminderURL != "" {
		if f.itemID == "" {
			f.itemID = queryValue(f.reminderURL, "itemId")
		}
		if f.buyerID == "" {
			f.buyerID = queryValue(f.reminderURL, "peerUserId")
		}
		if f.chatID == "" {
			f.chatID = queryValue(f.reminderURL, "sid")
		}
		if f.orderID == "" {
			f.orderID = matchOrderID(f.reminderURL)
		}
	}
	return f
}

func isOrderPaidEvent(f rawFields) bool {
	if f.orderRole == "buyer" {
		return false
	}
	return strings.Contains(f.text, "我已付款，等待你发货") ||
		strings.Contains(f.text, "已付款，待发货") ||
		strings.Contains(f.text, "记得及时发货") ||
		strings.Contains(f.redReminder, "等待卖家发货")
}

func isBuyerReviewedEvent(f rawFields) bool {
	// 闲鱼评价样本：
	//   redReminder=有新交易评价
	//   reminderContent=[我完成了评价]
	//   updateKey=chat_id:order_id:10:BUYER_RATE_SELLER:26
	// 仅“服务评价邀请”不含 BUYER_RATE_SELLER，不能误触发赠品。
	// BUYER_RATE_SELLER 是交易评价的稳定业务标识。展示文案会因客户端版本、
	// 同一买家重复购买等场景变化，不能再把两段中文文案同时存在作为必要条件。
	return strings.Contains(strings.ToUpper(f.updateKey), "BUYER_RATE_SELLER")
}

func extFields(ext string) (updateKey, contentType string) {
	if strings.TrimSpace(ext) == "" {
		return "", ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(ext), &m) != nil {
		return "", ""
	}
	return strAny(m["updateKey"]), strAny(m["contentType"])
}

func parseUpdateKey(updateKey string) (chatID, orderID string) {
	parts := strings.Split(updateKey, ":")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func queryValue(rawURL, key string) string {
	if strings.HasPrefix(rawURL, "fleamarket://") {
		rawURL = "https://local.invalid/" + strings.TrimPrefix(rawURL, "fleamarket://")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func mapAt(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func nestedString(m map[string]any, path ...string) string {
	var cur any = m
	for _, p := range path {
		cm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = cm[p]
	}
	return strAny(cur)
}

func strAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func trimGoofishSID(s string) string {
	if i := strings.Index(s, "@"); i >= 0 {
		return s[:i]
	}
	return s
}

var orderIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`orderId[=:](\d{10,})`),
	regexp.MustCompile(`order_detail\?id=(\d{10,})`),
	regexp.MustCompile(`bizOrderId[=:](\d{10,})`),
	regexp.MustCompile(`id=(\d{10,})`),
}

func extractOrderRoleFromContent(contentJSON string) string {
	var c map[string]any
	if json.Unmarshal([]byte(contentJSON), &c) != nil {
		return ""
	}
	for _, path := range [][]string{
		{"dxCard", "item", "main", "exContent", "button", "targetUrl"},
		{"dxCard", "item", "main", "targetUrl"},
		{"dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "button", "targetUrl"},
	} {
		if role := orderRoleFromURL(nestedString(c, path...)); role != "" {
			return role
		}
	}
	return ""
}

func orderRoleFromURL(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "fleamarket://") {
		rawURL = "https://local.invalid/" + strings.TrimPrefix(rawURL, "fleamarket://")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(u.Query().Get("role"))) {
	case "seller", "buyer":
		return strings.ToLower(strings.TrimSpace(u.Query().Get("role")))
	default:
		return ""
	}
}

func bizTaskName(raw string) string {
	var tag map[string]any
	if json.Unmarshal([]byte(raw), &tag) != nil {
		return ""
	}
	return strAny(tag["taskName"])
}

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

func matchOrderID(s string) string {
	for _, re := range orderIDPatterns {
		if m := re.FindStringSubmatch(s); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

func extractOrderIDFromContent(contentJSON string) string {
	var c map[string]any
	if json.Unmarshal([]byte(contentJSON), &c) != nil {
		return ""
	}
	for _, path := range [][]string{
		{"dxCard", "item", "main", "exContent", "button", "targetUrl"},
		{"dxCard", "item", "main", "targetUrl"},
		{"dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "button", "targetUrl"},
	} {
		if id := matchOrderID(nestedString(c, path...)); id != "" {
			return id
		}
	}
	return ""
}
