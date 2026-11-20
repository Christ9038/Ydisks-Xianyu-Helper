package notify

import (
	"net/url"
	"strings"
)

// levelLabel 封装levelLabel业务协调。
func levelLabel(level string) string {
	switch level {
	case "critical":
		return "严重"
	case "warn":
		return "警告"
	case "info":
		return "提示"
	default:
		return level
	}
}

// eventLabel 封装eventLabel业务协调。
func eventLabel(eventType string) string {
	switch eventType {
	case EventAccountOffline:
		return "掉线通知"
	case EventAccountRecovered:
		return "恢复通知"
	case EventAccountDisabled:
		return "禁用通知"
	case EventSecurityVerification:
		return "风控验证"
	case EventTokenRenewal:
		return "续期通知"
	case EventDeliveryResult:
		return "交易通知"
	case EventAutomationOrderCreated:
		return "拍下改价"
	case EventAutomationOrderPaid:
		return "付款发货"
	case EventAutomationBuyerReviewed:
		return "评价赠品"
	case EventAutomationReviewMissingTimeout:
		return "求评价"
	case EventManualDeliveryResult:
		return "手动发货"
	case EventManualInterventionRequired:
		return "需要人工处理"
	case EventSystemError:
		return "系统错误"
	case EventChatManualRequested:
		return "人工客服"
	case EventChatMessageReceived:
		return "新消息"
	default:
		if eventType == "" {
			return "通知"
		}
		return eventType
	}
}

// classifyAccountAlertEvent 封装classify账号AlertEvent业务协调。
func classifyAccountAlertEvent(title, body string) string {
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(title + " " + body)
	switch {
	case strings.Contains(msg, "风控"), strings.Contains(msg, "验证"),
		strings.Contains(msg, "滑块"), strings.Contains(msg, "captcha"),
		strings.Contains(msg, "risk"), strings.Contains(msg, "x5sec"):
		return EventSecurityVerification
	case strings.Contains(msg, "禁用"), strings.Contains(msg, "disabled"):
		return EventAccountDisabled
	case strings.Contains(msg, "掉线"), strings.Contains(msg, "离线"),
		strings.Contains(msg, "offline"), strings.Contains(msg, "session"),
		strings.Contains(msg, "登录凭证已失效"):
		return EventAccountOffline
	case strings.Contains(msg, "token"), strings.Contains(msg, "续期"), strings.Contains(msg, "renew"):
		return EventTokenRenewal
	default:
		return EventSystemError
	}
}

// statusOutcome 把自动化运行终态转换为标题结果后缀；人工处理等无成败语义的状态返回空串。
func statusOutcome(status string) string {
	switch strings.TrimSpace(status) {
	case "success", "succeeded":
		return "成功"
	case "failed":
		return "失败"
	default:
		return ""
	}
}

// messageOutcome 根据手动发货结果文案推断成败；含“成功”视为成功，含“失败”或“异常”视为失败，其余无后缀。
func messageOutcome(message string) string {
	switch {
	case strings.Contains(message, "失败"), strings.Contains(message, "异常"):
		return "失败"
	case strings.Contains(message, "成功"):
		return "成功"
	default:
		return ""
	}
}

// formatEvent 封装formatEvent业务协调。
func formatEvent(ev NotificationEvent) string {
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// label 用于本次流程后续判断的label
	label := eventLabel(ev.Type)
	// level 用于本次流程后续判断的level
	level := levelLabel(ev.Level)
	if level == "" {
		level = "提示"
	}
	// title 用于本次流程后续判断的标题
	title := strings.TrimSpace(ev.Title)
	if title == "" {
		title = label
	}
	// 首行必须直接给出事件类型与结果，手机推送预览只显示首行，用户无需进入程序即可判断。
	b.WriteString("【")
	b.WriteString(label)
	b.WriteString("】")
	b.WriteString(ev.Outcome)
	// 自动化与发货类事件的原标题只是泛称，已被类型标签取代；账号告警等保留原标题作为具体事件说明。
	if title != label && !isAutomationEventType(ev.Type) && ev.Type != EventManualDeliveryResult && ev.Type != EventManualInterventionRequired && ev.Type != EventDeliveryResult {
		b.WriteString(" ")
		b.WriteString(title)
	}
	b.WriteString("\n\n级别: ")
	b.WriteString(level)
	if ev.AccountID != "" {
		b.WriteString("\n账号: ")
		b.WriteString(ev.AccountID)
	}
	b.WriteString("\n时间: ")
	b.WriteString(ev.Time.Format("2006-01-02 15:04:05"))
	if len(ev.Fields) > 0 {
		// keys 用于本次流程后续判断的keys
		keys := make([]string, 0, len(ev.Fields))
		// k 表示当前遍历过程中的k
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sortStrings(keys)
		// k 表示当前遍历过程中的k
		for _, k := range keys {
			// v 用于本次流程后续判断的v
			v := strings.TrimSpace(ev.Fields[k])
			if v == "" {
				continue
			}
			b.WriteByte('\n')
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
		}
	}
	// body 用于本次流程后续判断的请求体
	body := strings.TrimSpace(ev.Body)
	if body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	return b.String()
}

// chatMessageURL 生成管理后台消息页深链接；账号与会话都经过 URL 编码，避免特殊字符改变查询参数结构。
// accountID 是账号标识，chatID 是会话标识；两者只用于定位页面，不包含凭证。
func (n *Notifier) chatMessageURL(accountID, chatID string) string {
	return n.publicBaseURL + "/app/chat?account=" + url.QueryEscape(accountID) + "&chat=" + url.QueryEscape(chatID)
}

// chatPeerLabel 组合买家名称与平台用户 ID；任意一侧缺失时只展示可用部分，全部缺失时显示“未知”。
func chatPeerLabel(peerName, peerID string) string {
	// name、id 是去除首尾空白后的买家展示名称与平台用户标识。
	name, id := strings.TrimSpace(peerName), strings.TrimSpace(peerID)
	switch {
	case name != "" && id != "":
		return name + " (ID: " + id + ")"
	case name != "":
		return name
	case id != "":
		return "ID: " + id
	default:
		return "未知"
	}
}

// chatMessageTypeLabel 把平台消息类型转换成中文展示标签；未知类型保留原始编码，空值显示“未知”。
func chatMessageTypeLabel(messageType string) string {
	// normalized 是去除空白并转为小写的消息类型编码，用于稳定匹配平台取值。
	normalized := strings.TrimSpace(strings.ToLower(messageType))
	switch normalized {
	case "text":
		return "文本"
	case "image":
		return "图片"
	case "item":
		return "商品卡片"
	case "video":
		return "视频"
	case "audio":
		return "语音"
	case "system":
		return "系统消息"
	default:
		if normalized != "" {
			return normalized
		}
		return "未知"
	}
}

// chatMessagePreview 生成非敏感消息摘要；媒体与商品卡片只显示类型说明，文本按字符数安全截断。
func chatMessagePreview(messageType, preview string) string {
	// normalized 是去除空白并转为小写的消息类型编码，用于决定摘要展示方式。
	normalized := strings.TrimSpace(strings.ToLower(messageType))
	switch normalized {
	case "image":
		return "[图片]"
	case "item":
		return "[商品卡片]"
	case "video":
		return "[视频]"
	case "audio":
		return "[语音]"
	}
	// text 是去除首尾空白后的文本消息内容，空内容用通用提示替代。
	text := strings.TrimSpace(preview)
	if text == "" {
		return "[新消息]"
	}
	return truncateRunes(text, 160)
}

// truncateRunes 按 Unicode 字符而不是字节截断文本，避免中文或表情被截成非法 UTF-8；超长时追加省略号。
func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	// runes 保存按字符切分的文本，长度以字符数而非字节数计算。
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
