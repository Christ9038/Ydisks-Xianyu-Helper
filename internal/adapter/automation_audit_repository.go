package adapter

import (
	"context"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// ListAuditEvents 按用户归属分页读取规则日志，并把数据库事件转换为不含数据库模型的应用层事件。
func (r *AutomationRepository) ListAuditEvents(ctx context.Context, query automationapp.AuditQuery) (automationapp.AuditPage, error) {
	// page、err 保存数据库返回的一页事件和查询错误。
	page, err := r.store.Automation.ListAuditEvents(ctx, db.AutomationAuditQuery{
		UserID: query.UserID, CookieID: query.CookieID, Status: query.Status, TriggerType: query.TriggerType,
		RunID: query.RunID, Search: query.Search, Limit: query.Limit, Offset: query.Offset, Ascending: query.Ascending,
	})
	if err != nil {
		return automationapp.AuditPage{}, err
	}
	// events 是转换后的应用层事件列表，保证非空切片便于调用方序列化。
	events := make([]automationapp.AuditEvent, 0, len(page.Events))
	// event 是当前待转换的数据库审计事件。
	for _, event := range page.Events {
		events = append(events, automationapp.AuditEvent{
			ID: event.ID, CookieID: event.CookieID, OrderID: event.OrderID, ItemID: event.ItemID,
			RuleID: event.RuleID, RunID: event.RunID, TriggerType: event.TriggerType, Source: event.Source,
			EventType: event.EventType, Status: event.Status, ActionType: event.ActionType,
			Summary: event.Summary, Reason: event.Reason, CreatedAt: event.CreatedAt,
		})
	}
	return automationapp.AuditPage{Events: events, Total: page.Total}, nil
}

// 确保数据库适配器完整实现规则日志应用 Port。
var _ automationapp.AuditRepository = (*AutomationRepository)(nil)
