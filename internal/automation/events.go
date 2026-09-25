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
	TriggerOrderCreated = "order_created"
	TriggerOrderPaid    = "order_paid"
	// TriggerOrderCompleted 表示买家确认收货后平台发出的完成交易系统提示；仅用于更新本地订单事实。
	TriggerOrderCompleted = "order_completed"
	// TriggerBargainPending 表示砍价“我已小刀，待刀成”系统阶段；它不是用户可配置规则触发器。
	TriggerBargainPending       = "bargain_pending"
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

// OrderRole 表示 WebSocket 交易卡片声明的接收方角色；空值代表旧协议没有提供角色事实。
type OrderRole string

const (
	// OrderRoleUnknown 表示当前事件没有足够协议字段证明买卖方向。
	OrderRoleUnknown OrderRole = ""
	// OrderRoleSeller 表示当前账号是交易卡片对应商品的卖家。
	OrderRoleSeller OrderRole = "seller"
	// OrderRoleBuyer 表示当前账号是交易卡片对应订单的买家。
	OrderRoleBuyer OrderRole = "buyer"
)

// Task 是自动化中心的统一输入。它可以来自 WS 系统事件、计划任务或手动触发。
type Task struct {
	Source      string // ws/scheduler/manual
	AccountID   string
	CookieStr   string
	TriggerType string
	// OrderRole 是 WebSocket 交易卡片携带的接收方角色；未知时必须由本地订单和商品事实补证，不能直接发货。
	OrderRole OrderRole
	ChatID    string
	OrderID   string
	ItemID    string
	BuyerID   string
	// BuyerNickname 是购买用户昵称，来自本地聊天会话的非敏感摘要。
	BuyerNickname string
	SpecName      string
	SpecValue     string
	Quantity      string
	Amount        string
	OrderStatus   string
	// IsBargain 标记订单是否为订单同步或砍价 WS 已确认的砍价活动订单。
	IsBargain bool
	Text      string
	UpdateKey string
	// ForceConfirmShipment 仅供明确的人工“完整发货”使用；自动事件仍遵循账号自动确认开关。
	ForceConfirmShipment bool
	// AllowAllItems 表示当前冻结规则是否明确授权账号级付款发货动作忽略订单规格；该事实随运行快照保存，避免恢复时重新猜测规则范围。
	AllowAllItems bool
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

// ExtractTaskFromWS 从 raw 单条解密 WS 消息提取卖家交易事件；accountID 是接收账号的本地标识。
// cookieStr 是仅供任务执行使用的明文凭证，不得输出到日志；返回任务沿用该凭证，无法识别或明确为买家副本时返回 nil。
// 本入口不持久化事实或执行动作；无角色的旧版合法卖家事件继续保留，规则和防重由 Center 决定。
func ExtractTaskFromWS(accountID, cookieStr string, raw map[string]any) *Task {
	if raw == nil {
		return nil
	}
	// f 汇总已有协议路径解析出的交易事实和接收方角色，供所有 WS 交易种类共用入口防御。
	f := fieldsFromRaw(raw)
	if f.orderRoleConflict {
		// 角色字段互相矛盾时无法证明当前账号方向，拒绝进入任何自动化分支。
		return nil
	}
	// 接收账号明确为买家时，必须在创建任务和记录订单事实之前拒绝，避免无匹配规则时仍写入错误卖家归属。
	if f.orderRole == "buyer" {
		return nil
	}
	// 明确的普通用户消息不能仅凭“待发货”等词语进入卖家自动化；缺少方向字段的历史协议仍保留兼容入口。
	if f.messageDirection == "2" && !isSystemEvent(f) {
		return nil
	}
	if f.text == "" && f.redReminder == "" && f.reminderNotice == "" && f.taskName == "" && f.cardTitle == "" && f.buttonText == "" && f.updateKey == "" {
		return nil
	}
	// task 保存待交给中心的系统事件；缺少角色只保留为待核验任务，不能根据文案或发送者推断买卖身份。
	task := &Task{
		Source:    "ws",
		AccountID: accountID,
		CookieStr: cookieStr,
		OrderRole: OrderRole(f.orderRole),
		ChatID:    f.chatID,
		OrderID:   f.orderID,
		ItemID:    f.itemID,
		BuyerID:   f.buyerID,
		Text:      firstNonEmpty(f.text, f.redReminder, f.reminderNotice, f.taskName, f.cardTitle, f.buttonText, f.title, f.detail),
		UpdateKey: f.updateKey,
		Raw:       raw,
	}
	switch {
	case isBargainPendingCard(f):
		task.TriggerType = TriggerBargainPending
		task.IsBargain = true
	case isBargainReadyCard(f):
		task.TriggerType = TriggerOrderPaid
		task.IsBargain = true
	case isOrderPaidEvent(f):
		task.TriggerType = TriggerOrderPaid
	case isOrderCreatedEvent(f):
		task.TriggerType = TriggerOrderCreated
	case isOrderCompletedEvent(f):
		task.TriggerType = TriggerOrderCompleted
		task.OrderStatus = "completed"
	case isBuyerReviewedEvent(f):
		task.TriggerType = TriggerBuyerReviewed
	default:
		return nil
	}
	return task
}

// rawFields 用于本次流程后续判断的原始字段列表
type rawFields struct {
	text string
	// reminderContent 保存参考项目直接自动发货判断使用的 message["1"]["10"] 原始文案。
	reminderContent string
	redReminder     string
	// reminderNotice 保存平台交易通知摘要，部分版本只在该字段提供“买家已付款”等发货信号。
	reminderNotice string
	title          string
	detail         string
	// taskName 保存 bizTag.taskName，兼容付款完成业务键缺失时的交易状态识别。
	taskName string
	// cardTitle 保存交易卡片内层标题，避免外层通用标题覆盖付款业务文案。
	cardTitle string
	// buttonText 保存交易卡片内层操作按钮文案，兼容只有“去发货”提示的系统卡片。
	buttonText string
	orderRole  string
	// orderRoleConflict 表示同一报文内出现互相矛盾的买卖角色；冲突时必须失败关闭。
	orderRoleConflict bool
	updateKey         string
	contentType       string
	// messageDirection 保存平台消息方向；2 表示普通接收聊天，1 通常表示系统事件。
	messageDirection string
	// systemBiz 表示 bizTag 明确携带系统任务标识，兼容没有稳定方向字段的系统卡片。
	systemBiz bool
	// simplified 表示 message["1"] 是会话字符串的简化消息，订单事实需要从本地订单回填。
	simplified  bool
	chatID      string
	orderID     string
	itemID      string
	buyerID     string
	reminderURL string
}

// fieldsFromRaw 按 raw 的协议结构提取系统交易事实，兼容完整信封、新版卡片与旧简化提醒；无持久化副作用。
func fieldsFromRaw(raw map[string]any) rawFields {
	// f 保存经固定路径优先及兼容回填取得的会话、订单、状态与角色事实。
	var f rawFields
	if // m1 用于本次流程后续判断的m1
	m1 := mapAt(raw, "1"); m1 != nil {
		f.messageDirection = strAny(m1["7"])
		if // s 用于本次流程后续判断的s
		s := strAny(m1["2"]); s != "" {
			f.chatID = trimGoofishSID(s)
		}
		if // m10 用于本次流程后续判断的m10
		m10 := mapAt(m1, "10"); m10 != nil {
			mergeNoticeFields(&f, m10)
			f.updateKey, f.contentType = extFields(strAny(m10["extJson"]))
			f.taskName = firstNonEmpty(f.taskName, bizTaskName(strAny(m10["bizTag"])))
			setOrderRole(&f, orderRoleFromTaskName(f.taskName))
			f.systemBiz = hasSystemBizTag(strAny(m10["bizTag"]))
		}
		if f.text == "" {
			f.text = nestedString(raw, "1", "6", "3", "2")
		}
		if f.contentType == "" {
			f.contentType = nestedString(raw, "1", "6", "3", "4")
		}
		if // contentJSON 用于本次流程后续判断的内容JSON
		contentJSON := nestedString(raw, "1", "6", "3", "5"); contentJSON != "" {
			// cardTitle、buttonText 分别保存交易卡片内层标题和按钮文案，兼容外层仅显示“卡片消息”的新版协议。
			cardTitle, buttonText := extractCardSignals(contentJSON)
			if f.cardTitle == "" {
				f.cardTitle = cardTitle
			}
			if f.buttonText == "" {
				f.buttonText = buttonText
			}
			if // role 用于本次流程后续判断的role
			role := extractOrderRoleFromContent(contentJSON); role != "" {
				setOrderRole(&f, role)
			}
			if // id 用于本次流程后续判断的标识
			id := extractOrderIDFromContent(contentJSON); id != "" {
				f.orderID = id
			}
		}
	} else if mapAt(raw, "4") != nil && mapAt(raw, "3") == nil {
		// 新版卡片的字段 1 是消息 ID；会话和方向分别来自字段 2、3，不能进入简化提醒分支。
		f.chatID = trimGoofishSID(strAny(raw["2"]))
		f.messageDirection = strAny(raw["3"])
	} else if // compactSession 保存简化消息中的会话标识
	compactSession, ok := raw["1"].(string); ok && strings.TrimSpace(compactSession) != "" {
		f.simplified = true
		f.chatID = trimGoofishSID(compactSession)
		if f.chatID == compactSession {
			f.chatID = trimGoofishSID(strAny(raw["2"]))
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
		mergeNoticeFields(&f, m4)
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

// mergeNoticeFields 合并一份平台交易通知字段；已有固定路径值优先，避免备用层覆盖可信事实。
func mergeNoticeFields(fields *rawFields, notice map[string]any) {
	if fields == nil || notice == nil {
		return
	}
	if fields.reminderContent == "" {
		fields.reminderContent = strAny(notice["reminderContent"])
	}
	if fields.text == "" {
		fields.text = fields.reminderContent
	}
	if fields.redReminder == "" {
		fields.redReminder = strAny(notice["redReminder"])
	}
	if fields.reminderNotice == "" {
		fields.reminderNotice = strAny(notice["reminderNotice"])
	}
	if fields.title == "" {
		fields.title = strAny(notice["reminderTitle"])
	}
	if fields.detail == "" {
		fields.detail = strAny(notice["detailNotice"])
	}
	if fields.reminderURL == "" {
		fields.reminderURL = strAny(notice["reminderUrl"])
	}
	if fields.buyerID == "" {
		fields.buyerID = strAny(notice["senderUserId"])
	}
	if fields.taskName == "" {
		fields.taskName = bizTaskName(strAny(notice["bizTag"]))
	}
	setOrderRole(fields, orderRoleFromTaskName(fields.taskName))
	if hasSystemBizTag(strAny(notice["bizTag"])) {
		fields.systemBiz = true
	}
}

// hasSystemBizTag 判断 bizTag 是否带有参考项目用于识别系统消息的稳定标识。
func hasSystemBizTag(raw string) bool {
	// trimmed 是去除空白后的 bizTag 原文，供稳定系统字段匹配。
	trimmed := strings.TrimSpace(raw)
	return trimmed != "" && (strings.Contains(trimmed, "SECURITY") || strings.Contains(trimmed, "taskName") || strings.Contains(trimmed, "taskId"))
}

// isSystemEvent 判断当前交易事件是否具备系统消息语义；未知方向的旧报文保留兼容，不在此处拒绝。
func isSystemEvent(fields rawFields) bool {
	return fields.simplified || fields.messageDirection == "1" || fields.contentType == "6" || fields.systemBiz
}

// extractCardSignals 从交易卡片 JSON 中提取内层标题和按钮文案；返回值仅用于系统事件识别，不读取普通聊天正文。
func extractCardSignals(contentJSON string) (cardTitle, buttonText string) {
	// content 保存已解码的卡片对象；解析失败时调用方继续使用外层交易字段。
	var content map[string]any
	if json.Unmarshal([]byte(contentJSON), &content) != nil {
		return "", ""
	}
	cardTitle = firstNonEmpty(
		nestedString(content, "dxCard", "item", "main", "exContent", "title"),
		nestedString(content, "dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "title"),
	)
	buttonText = firstNonEmpty(
		nestedString(content, "dxCard", "item", "main", "exContent", "button", "text"),
		nestedString(content, "dynamicOperation", "changeContent", "dxCard", "item", "main", "exContent", "button", "text"),
	)
	return cardTitle, buttonText
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
		setOrderRole(fields, normalizedOrderRole(text))
	case "taskname":
		if fields.taskName == "" {
			fields.taskName = text
		}
		setOrderRole(fields, orderRoleFromTaskName(text))
	case "biztag":
		if fields.taskName == "" {
			fields.taskName = bizTaskName(text)
		}
		setOrderRole(fields, orderRoleFromTaskName(bizTaskName(text)))
		if hasSystemBizTag(text) {
			fields.systemBiz = true
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
	setOrderRole(fields, orderRoleFromURL(rawURL))
}

// setOrderRole 合并报文中的买卖角色；同一事件出现冲突角色时保留冲突标记，调用方必须拒绝执行。
func setOrderRole(fields *rawFields, role string) {
	if fields == nil || role == "" {
		return
	}
	if fields.orderRole != "" && fields.orderRole != role {
		fields.orderRoleConflict = true
		return
	}
	fields.orderRole = role
}

// isOrderPaidEvent 封装is订单PaidEvent业务协调。
func isOrderPaidEvent(f rawFields) bool {
	if f.orderRole == "buyer" {
		return false
	}
	// 简化消息只接受参考项目的精确红色提醒，订单号和商品事实随后由会话订单回填。
	if f.simplified {
		return strings.TrimSpace(f.redReminder) == "等待卖家发货"
	}
	// 成功小刀只能由系统卡片标题触发，不能由普通文本、通知摘要或业务键触发。
	if isBargainReadyCard(f) {
		return isSystemEvent(f)
	}
	// 普通付款自动发货严格采用参考项目 _is_auto_delivery_trigger 的四个文案，并要求系统消息门禁。
	if !isSystemEvent(f) {
		return false
	}
	return strings.Contains(f.reminderContent, "[我已付款，等待你发货]") ||
		strings.Contains(f.reminderContent, "[已付款，待发货]") ||
		strings.Contains(f.reminderContent, "我已付款，等待你发货") ||
		strings.Contains(f.reminderContent, "[记得及时发货]")
}

// isBargainReadyCard 判断是否为参考项目第二阶段的成功小刀系统卡片标题。
func isBargainReadyCard(fields rawFields) bool {
	return fields.cardTitle == "我已成功小刀，待发货" || fields.cardTitle == "我已成功小刀,待发货"
}

// isBargainPendingCard 判断是否为砍价第一阶段的“待刀成”系统卡片。
func isBargainPendingCard(fields rawFields) bool {
	return fields.cardTitle == "我已小刀，待刀成" || fields.cardTitle == "我已小刀,待刀成"
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

// isOrderCompletedEvent 判定卖家收到的买家确认收货系统提醒；contentType=25 是该类平台卡片的稳定协议标识。
func isOrderCompletedEvent(f rawFields) bool {
	if f.orderRole == "buyer" || !isSystemEvent(f) || strings.TrimSpace(f.contentType) != "25" {
		return false
	}
	// message 汇总平台卡片展示字段，只接受确认收货后的固定评价提醒，避免把普通评价文案误记为订单完成。
	message := strings.Join([]string{f.text, f.redReminder, f.reminderNotice, f.title, f.detail, f.taskName, f.cardTitle}, "\n")
	return strings.Contains(message, "快给ta一个评价吧") || strings.Contains(message, "买家已确认收货")
}

// isPriceModifiedEvent 判断卖家改价后的确认卡片，避免它沿用“等待买家付款”文案时被重复识别为拍下事件。
func isPriceModifiedEvent(f rawFields) bool {
	// displayText 汇总卡片的业务键和展示文本；这些字段都不包含用户聊天正文。
	displayText := strings.ToUpper(strings.Join([]string{f.updateKey, f.text, f.redReminder, f.title, f.detail}, "\n"))
	return strings.Contains(displayText, "TRADE_MODIFY_FEE") || strings.Contains(displayText, "我已修改价格")
}

// isBuyerReviewedEvent 根据 f 的接收方角色和评价业务键返回是否为卖家收到的买家评价，不产生副作用。
// 保留函数级 buyer 防御，避免内部调用绕过 WS 统一入口；无角色的合法旧版卖家评价继续兼容。
func isBuyerReviewedEvent(f rawFields) bool {
	if f.orderRole == "buyer" {
		return false
	}
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

// parseUpdateKey 从 updateKey 提取会话号和订单号；确认收货四段键没有会话号，第二段是状态码。
// 无法证明为有效订单号的确认收货键返回空身份，避免把状态码写入订单或用于自动评价。
func parseUpdateKey(updateKey string) (chatID, orderID string) {
	// parts 保留平台业务键的字段顺序，用明确的事件标记区分两种协议布局。
	parts := strings.Split(updateKey, ":")
	if len(parts) == 4 && parts[2] == "BUYER_CONFIRM_RATE_SELLER" && parts[3] == "74" {
		return "", directOrderID(parts[0])
	}
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
