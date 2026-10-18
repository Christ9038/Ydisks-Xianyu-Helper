package db

import (
	"context"
	"database/sql"

	"xianyu-go/internal/deliverytemplate"
)

// parseDeliveryTemplateInput 优先校验 input 的结构化消息；nil 才转换历史文本输入。
func parseDeliveryTemplateInput(input DeliveryTemplateInput) (deliverytemplate.Parsed, error) {
	if input.MessageItems != nil {
		return deliverytemplate.ParseMessages(input.MessageItems)
	}
	return deliverytemplate.Parse(input.Messages)
}

// deliveryTemplateMessageQueryer 限定模板读取对普通连接与事务的共同需求。
type deliveryTemplateMessageQueryer interface {
	// QueryContext 使用 ctx 控制查询取消，query 和 args 是仓储内部的参数化 SQL，返回需关闭的游标。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readDeliveryTemplateMessages 在 queryer 的连接或事务内读取 templateID 的完整消息，保证图片与文本使用同一快照。
func readDeliveryTemplateMessages(ctx context.Context, queryer deliveryTemplateMessageQueryer, templateID int64) ([]DeliveryTemplateMessage, error) {
	// rows、err 保存有序模板消息游标及查询错误。
	rows, err := queryer.QueryContext(ctx, `SELECT id,template_id,sort_order,content,type,COALESCE(image_url,''),COALESCE(image_path,'') FROM delivery_template_messages WHERE template_id=? ORDER BY sort_order ASC,id ASC`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// messages 保留消息标识和全部来源，供展示、契约校验及运行计划共同使用。
	messages := make([]DeliveryTemplateMessage, 0)
	for rows.Next() {
		// message 保存当前数据库消息，不与调用方共享状态。
		var message DeliveryTemplateMessage
		// err 保存当前消息完整字段扫描错误。
		if err := rows.Scan(&message.ID, &message.TemplateID, &message.SortOrder, &message.Content, &message.Type, &message.ImageURL, &message.ImagePath); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	// err 保存游标遍历错误，不能降级为空模板。
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 遍历结束时 database/sql 已关闭游标，关闭错误由上面的 rows.Err 统一返回。
	return messages, nil
}

// deliveryTemplateMessageItems 将 messages 的持久化标识剥离，返回领域消息副本供纯校验使用。
func deliveryTemplateMessageItems(messages []DeliveryTemplateMessage) []deliverytemplate.Message {
	// items 保持图片和文本的原始发送顺序。
	items := make([]deliverytemplate.Message, 0, len(messages))
	// message 是当前数据库消息，图片来源不执行插值。
	for _, message := range messages {
		items = append(items, deliverytemplate.Message{Type: message.Type, Content: message.Content, ImageURL: message.ImageURL, ImagePath: message.ImagePath})
	}
	return items
}
