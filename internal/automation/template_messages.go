package automation

import (
	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// templateMessageItems 返回 action 冻结的有序消息；旧运行只有字符串数组时逐条转换为文本。
// 不重新读取当前模板，也不重新解析已冻结消息，避免编辑模板改变延迟执行或补发计划。
func templateMessageItems(action db.AutomationAction) []deliverytemplate.Message {
	if action.TemplateMessageItems != nil {
		return action.TemplateMessageItems
	}
	// messages 保留旧动作中的消息数和顺序，包括可能渲染为空的文本。
	messages := make([]deliverytemplate.Message, 0, len(action.TemplateMessages))
	for /* content 是旧运行冻结的一条模板正文。 */ _, content := range action.TemplateMessages {
		messages = append(messages, deliverytemplate.Message{Type: "text", Content: content})
	}
	return messages
}
