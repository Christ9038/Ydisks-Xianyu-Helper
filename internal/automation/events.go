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

// TriggerOrderPaid 用于本次流程后续判断的Trigger订单Paid
const (
	TriggerOrderCreated         = "order_created"
	TriggerOrderPaid            = "order_paid"
	TriggerBuyerReviewed        = "buyer_reviewed"
	TriggerReviewMissingTimeout = "review_missing_timeout"

	ActionConfirmShipment = "confirm_shipment"
	ActionSendCard        = "send_card"
	// ActionSendTemplate 表示按发货模板渲染并发送多条消息。
	ActionSendTemplate = "send_template"
	ActionSendText     = "send_text"
	// ActionAdjustPrice 表示把待付款订单价格修改为动作配置中的目标价格。
	ActionAdjustPrice = "adjust_price"
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
	// BuyerNickname 是购买用户昵称，来自本地聊天会话的非敏感摘要。
	BuyerNickname string
	SpecName      string
	SpecValue     string
	Quantity      string
	Amount        string
	OrderStatus   string
	Text          string
	UpdateKey     string
	// ForceConfirmShipment 仅供明确的人工“完整发货”使用；自动事件仍遵循账号自动确认开关。
	ForceConfirmShipment bool
	// ActionPlan 是运行创建时冻结的动作计划。延迟恢复和失败重试必须使用该快照，
	// 不能把数字游标应用到管理员后来修改过的规则上。
	ActionPlan []db.AutomationAction
	Raw        map[string]any
}

// OrderDetail 是自动化中心执行交易类任务前需要补齐的订单事实。
// 规格和数量来自闲鱼订单，不由自动化规则修改商品属性。
// OrderDetail 用于本次流程后续判断的订单Detail
type OrderDetail struct {
	Quantity    string
	SpecName    string
	SpecValue   string
	Amount      string
	OrderStatus string
}

// ExtractTaskFromWS 从一条解密后的 WS 消息中提取系统事件。
// 这里只做事实解析：识别平台告诉了我们什么；是否执行自动化由 Center 根据规则和可用的订单或 updateKey 防重键决定。
// ExtractTaskFromWS 封装Extract任务FromWS业务协调。
func ExtractTaskFromWS(accountID, cookieStr string, raw map[string]any) *Task {
	if raw == nil {
		return nil
	}
	// f 用于本次流程后续判断的f
	f := fieldsFromRaw(raw)
	if f.text == "" && f.redReminder == "" && f.updateKey == "" {
		return nil
	}
	// task 用于本次流程后续判断的任务
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
	case isOrderCreatedEvent(f):
		task.TriggerType = TriggerOrderCreated
	case isBuyerReviewedEvent(f):
		task.TriggerType = TriggerBuyerReviewed
	default:
		return nil
	}
	return task
}

// rawFields 用于本次流程后续判断的原始字段列表
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

// fieldsFromRaw 封装字段列表From原始业务协调。
func fieldsFromRaw(raw map[string]any) rawFields {
	// f 用于本次流程后续判断的f
	var f rawFields
	if // m1 用于本次流程后续判断的m1
	m1 := mapAt(raw, "1"); m1 != nil {
		if // s 用于本次流程后续判断的s
		s := strAny(m1["2"]); s != "" {
			f.chatID = trimGoofishSID(s)
		}
		if // m10 用于本次流程后续判断的m10
		m10 := mapAt(m1, "10"); m10 != nil {
			f.text = strAny(m10["reminderContent"])
			f.redReminder = strAny(m10["redReminder"])
			f.title = strAny(m10["reminderTitle"])
			f.detail = strAny(m10["detailNotice"])
			f.reminderURL = strAny(m10["reminderUrl"])
			f.buyerID = strAny(m10["senderUserId"])
			f.updateKey, f.contentType = extFields(strAny(m10["extJson"]))
			f.orderRole = orderRoleFromTaskName(bizTaskName(strAny(m10["bizTag"])))
		}
		if // contentJSON 用于本次流程后续判断的内容JSON
		contentJSON := nestedString(raw, "1", "6", "3", "5"); contentJSON != "" {
			if // role 用于本次流程后续判断的role
			role := extractOrderRoleFromContent(contentJSON); role != "" {
				f.orderRole = role
			}
			if // id 用于本次流程后续判断的标识
			id := extractOrderIDFromContent(contentJSON); id != "" {
				f.orderID = id
			}
		}
	}
	if // m3 用于本次流程后续判断的m3
	m3 := mapAt(raw, "3"); m3 != nil {
		if f.redReminder == "" {
			f.redReminder = strAny(m3["redReminder"])
		}
	}
	if // m4 用于本次流程后续判断的m4
	m4 := mapAt(raw, "4"); m4 != nil {
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
		// chatID、orderID 用于本次流程后续判断的聊天ID、orderID
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
	// 平台交易卡片会随客户端版本把订单事实或买卖角色移动到不同嵌套层级或 JSON 字符串中；
	// 固定路径已取到全部事实时仍需识别角色，避免买家侧卡片被误投递为卖家自动化。
	supplementEventFacts(&f, raw, 0)
	return f
}

// fallbackEventFactsMaxDepth 限制平台原始报文递归解析深度，避免异常报文占用无限栈空间。
const fallbackEventFactsMaxDepth = 16

// supplementEventFacts 从非固定层级的对象、数组和内嵌 JSON 中补齐交易事实。
// 它只接受具有明确字段名或交易链接语义的值，不会从普通聊天正文猜测订单标识。
func supplementEventFacts(fields *rawFields, value any, depth int) {
	if fields == nil || value == nil || depth > fallbackEventFactsMaxDepth {
		return
	}
	switch // typedValue 保存当前原始节点按运行时类型断言后的值。
	typedValue := value.(type) {
	case map[string]any:
		// key、nestedValue 分别是当前对象字段名和待继续解析的字段值。
		for key, nestedValue := range typedValue {
			supplementEventFactByKey(fields, key, nestedValue)
			supplementEventFacts(fields, nestedValue, depth+1)
		}
	case []any:
		// nestedValue 是当前数组中的报文节点。
		for _, nestedValue := range typedValue {
			supplementEventFacts(fields, nestedValue, depth+1)
		}
	case string:
		// text 是去除两端空白后的原始字符串，可能是交易链接或内嵌 JSON。
		text := strings.TrimSpace(typedValue)
		if fields.orderID == "" {
			fields.orderID = matchOrderID(text)
		}
		if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
			return
		}
		// nestedValue 是内嵌 JSON 反序列化后的节点；解析失败时该字符串不携带可安全识别的结构化事实。
		var nestedValue any
		if json.Unmarshal([]byte(text), &nestedValue) == nil {
			supplementEventFacts(fields, nestedValue, depth+1)
		}
	}
}

// supplementEventFactByKey 按平台字段名补齐一项交易事实，并保留固定路径优先的已有值。
func supplementEventFactByKey(fields *rawFields, key string, value any) {
	if fields == nil {
		return
	}
	// normalizedKey 是移除大小写、下划线和短横线差异后的平台字段名。
	normalizedKey := normalizeEventFactKey(key)
	// text 是当前字段的字符串形式；数字订单号会由 JSON 解码后的数值统一转换。
	text := strings.TrimSpace(strAny(value))
	switch normalizedKey {
	case "orderid", "bizorderid", "tradeid", "orderno", "tradeno":
		if fields.orderID == "" {
			fields.orderID = directOrderID(text)
		}
	case "updatekey":
		if fields.updateKey == "" {
			fields.updateKey = text
		}
		// chatID、orderID 是 updateKey 中稳定编码的会话和订单标识。
		chatID, orderID := parseUpdateKey(text)
		if fields.chatID == "" {
			fields.chatID = chatID
		}
		if fields.orderID == "" {
			fields.orderID = orderID
		}
	case "chatid", "sessionid", "sid":
		if fields.chatID == "" {
			fields.chatID = trimGoofishSID(text)
		}
	case "itemid", "auctionid":
		if fields.itemID == "" {
			fields.itemID = text
		}
	case "buyerid", "peeruserid", "senderuserid":
		if fields.buyerID == "" {
			fields.buyerID = text
		}
	case "role", "orderrole":
		if fields.orderRole == "" {
			fields.orderRole = normalizedOrderRole(text)
		}
	case "taskname":
		if fields.orderRole == "" {
			fields.orderRole = orderRoleFromTaskName(text)
		}
	case "biztag":
		if fields.orderRole == "" {
			fields.orderRole = orderRoleFromTaskName(bizTaskName(text))
		}
	case "reminderurl", "targeturl", "url", "deeplink", "link":
		supplementEventFactsFromURL(fields, text)
	}
}

// normalizeEventFactKey 统一平台字段名的大小写和分隔符，兼容同一字段的不同协议命名。
func normalizeEventFactKey(key string) string {
	// normalized 是去除字段名分隔符并转为小写后的比较值。
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "_", ""), "-", "")
	return strings.ToLower(normalized)
}

// directOrderID 校验直接字段中的闲鱼订单标识，避免把普通文本或短数字误用为可执行订单号。
func directOrderID(value string) string {
	if len(value) < 10 {
		return ""
	}
	// character 是订单标识中的当前字符；闲鱼交易订单号只允许十进制数字。
	for _, character := range value {
		if character < '0' || character > '9' {
			return ""
		}
	}
	return value
}

// supplementEventFactsFromURL 从交易跳转链接补齐订单、商品、买家、会话和买卖角色。
func supplementEventFactsFromURL(fields *rawFields, rawURL string) {
	if fields == nil || strings.TrimSpace(rawURL) == "" {
		return
	}
	if fields.itemID == "" {
		fields.itemID = queryValue(rawURL, "itemId")
	}
	if fields.buyerID == "" {
		fields.buyerID = queryValue(rawURL, "peerUserId")
	}
	if fields.chatID == "" {
		fields.chatID = queryValue(rawURL, "sid")
	}
	if fields.orderID == "" {
		fields.orderID = matchOrderID(rawURL)
	}
	if fields.orderRole == "" {
		fields.orderRole = orderRoleFromURL(rawURL)
	}
}

// isOrderPaidEvent 封装is订单PaidEvent业务协调。
func isOrderPaidEvent(f rawFields) bool {
	if f.orderRole == "buyer" {
		return false
	}
	return strings.Contains(f.text, "我已付款，等待你发货") ||
		strings.Contains(f.text, "已付款，待发货") ||
		strings.Contains(f.text, "记得及时发货") ||
		strings.Contains(f.redReminder, "等待卖家发货")
}

// isOrderCreatedEvent 判定买家已拍下但尚未付款的交易卡片。
// 闲鱼拍下样本：reminderContent=[我已拍下，待付款]，或红色提醒“等待买家付款”。
// 买家角色的同类卡片属于当前账号自己下单，不进入卖家自动化。
func isOrderCreatedEvent(f rawFields) bool {
	if f.orderRole == "buyer" {
		return false
	}
	if isPriceModifiedEvent(f) {
		return false
	}
	return strings.Contains(f.text, "我已拍下") ||
		strings.Contains(f.text, "已拍下，待付款") ||
		strings.Contains(f.redReminder, "等待买家付款")
}

// isPriceModifiedEvent 判断卖家改价后的确认卡片，避免它沿用“等待买家付款”文案时被重复识别为拍下事件。
func isPriceModifiedEvent(f rawFields) bool {
	// displayText 汇总卡片的业务键和展示文本；这些字段都不包含用户聊天正文。
	displayText := strings.ToUpper(strings.Join([]string{f.updateKey, f.text, f.redReminder, f.title, f.detail}, "\n"))
	return strings.Contains(displayText, "TRADE_MODIFY_FEE") || strings.Contains(displayText, "我已修改价格")
}

// isBuyerReviewedEvent 封装is买家ReviewedEvent业务协调。
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

// extFields 封装ext字段列表业务协调。
func extFields(ext string) (updateKey, contentType string) {
	if strings.TrimSpace(ext) == "" {
		return "", ""
	}
	// m 用于本次流程后续判断的m
	var m map[string]any
	if json.Unmarshal([]byte(ext), &m) != nil {
		return "", ""
	}
	return strAny(m["updateKey"]), strAny(m["contentType"])
}

// parseUpdateKey 封装parseUpdateKey业务协调。
func parseUpdateKey(updateKey string) (chatID, orderID string) {
	// parts 用于本次流程后续判断的parts
	parts := strings.Split(updateKey, ":")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

// queryValue 封装查询值业务协调。
func queryValue(rawURL, key string) string {
	if strings.HasPrefix(rawURL, "fleamarket://") {
		rawURL = "https://local.invalid/" + strings.TrimPrefix(rawURL, "fleamarket://")
	}
	// u、err 用于本次流程后续判断的u、err
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

// mapAt 封装mapAt业务协调。
func mapAt(m map[string]any, key string) map[string]any {
	// v 用于本次流程后续判断的v
	v, _ := m[key].(map[string]any)
	return v
}

// nestedString 封装nestedString业务协调。
func nestedString(m map[string]any, path ...string) string {
	// cur 用于本次流程后续判断的cur
	var cur any = m
	// p 表示当前遍历过程中的p
	for _, p := range path {
		// cm、ok 用于本次流程后续判断的cm、ok
		cm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = cm[p]
	}
	return strAny(cur)
}

// strAny 封装strAny业务协调。
func strAny(v any) string {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		// b 用于本次流程后续判断的b
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// firstNonEmpty 封装firstNonEmpty业务协调。
func firstNonEmpty(values ...string) string {
	// v 表示当前遍历过程中的v
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// trimGoofishSID 封装trimGoofishSID业务协调。
func trimGoofishSID(s string) string {
	if // i 用于本次流程后续判断的i
	i := strings.Index(s, "@"); i >= 0 {
		return s[:i]
	}
	return s
}

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
