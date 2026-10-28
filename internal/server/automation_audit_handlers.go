package server

import (
	"errors"
	"net/http"
	"strconv"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/auth"
)

// automationAuditEventResponse 是规则日志中一条自动化行为事件的具名响应 DTO，只含非敏感摘要。
type automationAuditEventResponse struct {
	// ID 是事件标识。
	ID int64 `json:"id"`
	// CookieID 是触发事件的账号标识。
	CookieID string `json:"cookie_id"`
	// OrderID 与 ItemID 是关联的订单和商品，可为空字符串。
	OrderID string `json:"order_id"`
	ItemID  string `json:"item_id"`
	// RuleID 与 RunID 是关联规则和运行，没有关联时为零。
	RuleID int64 `json:"rule_id"`
	RunID  int64 `json:"run_id"`
	// TriggerType 是触发类型，Source 是事件来源（ws、scheduler、recovery、manual）。
	TriggerType string `json:"trigger_type"`
	Source      string `json:"source"`
	// EventType 是行为分类，Status 是事件状态，ActionType 是动作类型（运行级事件为空）。
	EventType  string `json:"event_type"`
	Status     string `json:"status"`
	ActionType string `json:"action_type"`
	// Summary 是不含卡密正文的发货内容摘要，Reason 是跳过或失败原因。
	Summary string `json:"summary"`
	Reason  string `json:"reason"`
	// CreatedAt 是事件发生时刻（Unix 秒）。
	CreatedAt int64 `json:"created_at"`
}

// automationAuditEventListResponse 是规则日志分页接口的具名响应 DTO。
type automationAuditEventListResponse struct {
	// Success 表示查询成功。
	Success bool `json:"success"`
	// Data 是当前页事件，无记录时为空数组。
	Data []automationAuditEventResponse `json:"data"`
	// Total 是符合条件的事件总数。
	Total int64 `json:"total"`
	// Page 与 PageSize 是规范化后的分页参数，TotalPages 是总页数。
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalPages int `json:"total_pages"`
}

// listAutomationAuditEvents 分页返回当前用户的自动化行为审计事件，支持按账号、状态、触发类型、订单号和运行过滤。
func (s *Server) listAutomationAuditEvents(w http.ResponseWriter, r *http.Request) {
	// sess 保存认证中间件注入的当前用户会话，查询只返回该用户名下账号的事件。
	sess := auth.SessionFromContext(r.Context())
	// query 保存 URL 查询参数；非法页码由应用层规范化。
	query := r.URL.Query()
	// page、pageSize 解析失败按 0 交给应用层取默认值。
	page, _ := strconv.Atoi(query.Get("page"))
	// pageSize 解析失败按 0 交给应用层取默认值。
	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	// runID 解析失败按 0 处理，表示不按运行过滤。
	runID, _ := strconv.ParseInt(query.Get("run_id"), 10, 64)
	// result、err 保存应用服务返回的一页事件及错误。
	result, err := s.automationAuditApplication().ListEvents(r.Context(), sess.UserID, automationapp.AuditListInput{
		CookieID: query.Get("cookie_id"), Status: query.Get("status"), TriggerType: query.Get("trigger_type"),
		RunID: runID, Search: query.Get("search"), Page: page, PageSize: pageSize,
	})
	if errors.Is(err, automationapp.ErrInvalidInput) {
		writeErr(w, http.StatusBadRequest, "规则日志查询参数无效")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询规则日志失败")
		return
	}
	writeJSON(w, http.StatusOK, automationAuditEventListResponse{
		Success: true, Data: newAutomationAuditEventResponses(result.Events), Total: result.Total,
		Page: result.Page, PageSize: result.PageSize,
		TotalPages: int((result.Total + int64(result.PageSize) - 1) / int64(result.PageSize)),
	})
}

// newAutomationAuditEventResponses 把应用层审计事件转换为 HTTP DTO，保证返回非空切片。
func newAutomationAuditEventResponses(events []automationapp.AuditEvent) []automationAuditEventResponse {
	// result 是待写入响应的事件 DTO 列表。
	result := make([]automationAuditEventResponse, 0, len(events))
	// event 是当前待转换的应用层事件。
	for _, event := range events {
		result = append(result, automationAuditEventResponse{
			ID: event.ID, CookieID: event.CookieID, OrderID: event.OrderID, ItemID: event.ItemID,
			RuleID: event.RuleID, RunID: event.RunID, TriggerType: event.TriggerType, Source: event.Source,
			EventType: event.EventType, Status: event.Status, ActionType: event.ActionType,
			Summary: event.Summary, Reason: event.Reason, CreatedAt: event.CreatedAt,
		})
	}
	return result
}
