package notify

import "strings"

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
