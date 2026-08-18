package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/auth"
)

// automationActionRequest 用于本次流程后续判断的自动化动作请求
type automationActionRequest struct {
	ActionType      string `json:"action_type"`
	CardID          int64  `json:"card_id"`
	DeliveryCount   int    `json:"delivery_count"`
	MessageTemplate string `json:"message_template"`
	DelaySeconds    int    `json:"delay_seconds"`
	ConfigJSON      string `json:"config_json"`
	Enabled         *bool  `json:"enabled"`
	SortOrder       int    `json:"sort_order"`
}

// automationRuleRequest 用于本次流程后续判断的自动化规则请求
type automationRuleRequest struct {
	CookieID    string                    `json:"cookie_id"`
	ItemID      string                    `json:"item_id"`
	Name        string                    `json:"name"`
	TriggerType string                    `json:"trigger_type"`
	Enabled     bool                      `json:"enabled"`
	Priority    int                       `json:"priority"`
	ConfigJSON  string                    `json:"config_json"`
	Actions     []automationActionRequest `json:"actions"`
}

// listAutomationRules 封装list自动化规则列表业务协调。
func (s *Server) listAutomationRules(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// query 用于本次流程后续判断的查询
	query := r.URL.Query()
	// paginated 用于本次流程后续判断的paginated
	_, paginated := query["page"]
	if !paginated {
		// rules、err 用于本次流程后续判断的rules、err
		rules, err := s.automationRulesApplication().ListForUser(r.Context(), sess.UserID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "查询自动化规则失败")
			return
		}
		writeJSON(w, http.StatusOK, automationRulesJSON(rules))
		return
	}

	// page 用于本次流程后续判断的页码
	page := atoiDefault(query.Get("page"), 1)
	// pageSize 用于本次流程后续判断的每页数量
	pageSize := atoiDefault(query.Get("page_size"), 10)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	// cookieID 用于本次流程后续判断的登录凭证ID
	cookieID := strings.TrimSpace(query.Get("cookie_id"))
	if cookieID != "" {
		if !s.cookieOwnedByUser(r.Context(), sess.UserID, cookieID) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
	}
	// triggerType 用于本次流程后续判断的trigger类型
	triggerType := strings.TrimSpace(query.Get("trigger_type"))
	if triggerType != "" {
		switch triggerType {
		case automationapp.TriggerOrderPaid, automationapp.TriggerBuyerReviewed, automationapp.TriggerReviewMissingTimeout:
		default:
			writeErr(w, http.StatusBadRequest, "不支持的触发类型")
			return
		}
	}
	// enabled 用于本次流程后续判断的启用状态
	var enabled *bool
	if // rawEnabled 用于本次流程后续判断的原始启用状态
	rawEnabled := strings.TrimSpace(query.Get("enabled")); rawEnabled != "" {
		// value、parseErr 用于本次流程后续判断的value、parseErr
		value, parseErr := strconv.ParseBool(rawEnabled)
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, "启用状态必须是 true 或 false")
			return
		}
		enabled = &value
	}

	// rules、total、err 用于本次流程后续判断的rules、total、err
	rules, total, err := s.automationRulesApplication().ListPageForUser(r.Context(), automationapp.RuleFilter{
		UserID: sess.UserID, CookieID: cookieID, TriggerType: triggerType, Enabled: enabled,
		Search: query.Get("search"), Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化规则失败")
		return
	}
	// filter 用于本次流程后续判断的filter
	filter := automationapp.RuleFilter{
		UserID: sess.UserID, CookieID: cookieID, TriggerType: triggerType, Enabled: enabled,
		Search: query.Get("search"),
	}
	// triggerCounts、err 用于本次流程后续判断的triggerCounts、err
	triggerCounts, err := s.automationRulesApplication().CountByTriggerForUser(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计自动化规则失败")
		return
	}
	// totalPages 用于本次流程后续判断的总数Pages
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages > 0 && page > totalPages {
		page = totalPages
		rules, _, err = s.automationRulesApplication().ListPageForUser(r.Context(), automationapp.RuleFilter{
			UserID: sess.UserID, CookieID: cookieID, TriggerType: triggerType, Enabled: enabled,
			Search: query.Get("search"), Limit: pageSize, Offset: (page - 1) * pageSize,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "查询自动化规则失败")
			return
		}
	}
	writeJSON(w, http.StatusOK, automationRulePageResponse{
		Success: true, Data: automationRulesJSON(rules), Total: total, Page: page,
		PageSize: pageSize, TotalPages: totalPages, TriggerCounts: triggerCounts,
	})
}

// automationRulesJSON 封装自动化规则列表JSON业务协调。
func automationRulesJSON(rules []automationapp.Rule) []automationRuleResponse {
	// out 用于本次流程后续判断的out
	out := make([]automationRuleResponse, 0, len(rules))
	// rule 表示当前遍历过程中的规则
	for _, rule := range rules {
		// actions 用于本次流程后续判断的动作列表
		actions := make([]automationActionResponse, 0, len(rule.Actions))
		// action 表示当前遍历过程中的动作
		for _, action := range rule.Actions {
			actions = append(actions, automationActionResponse{
				ID: action.ID, ActionType: action.ActionType, CardID: action.CardID, CardName: action.CardName,
				DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate,
				DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled,
				SortOrder: action.SortOrder,
			})
		}
		out = append(out, automationRuleResponse{
			ID: rule.ID, CookieID: rule.CookieID, ItemID: rule.ItemID, ItemTitle: rule.ItemTitle,
			Name: rule.Name, TriggerType: rule.TriggerType, Enabled: rule.Enabled, Priority: rule.Priority,
			ConfigJSON: rule.ConfigJSON, Actions: actions, CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
		})
	}
	return out
}

// createAutomationRule 创建自动化规则并返回数值主键 DTO。
func (s *Server) createAutomationRule(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// req 用于本次流程后续判断的req
	var req automationRuleRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// in、err 用于本次流程后续判断的in、err
	in, err := s.automationRulesApplication().Normalize(r.Context(), sess.UserID, automationRuleDraft(req))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// id、err 用于本次流程后续判断的id、err
	id, err := s.automationRulesApplication().Create(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateAutomationRule 封装update自动化规则业务协调。
func (s *Server) updateAutomationRule(w http.ResponseWriter, r *http.Request) {
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// req 用于本次流程后续判断的req
	var req automationRuleRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// in、err 用于本次流程后续判断的in、err
	in, err := s.automationRulesApplication().Normalize(r.Context(), sess.UserID, automationRuleDraft(req))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if // err 用于本次流程后续判断的err
	err := s.automationRulesApplication().Update(r.Context(), sess.UserID, ruleID, in); err != nil {
		if errors.Is(err, automationapp.ErrRuleNotFound) {
			writeErr(w, http.StatusNotFound, "自动化规则不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteAutomationRule 封装delete自动化规则业务协调。
func (s *Server) deleteAutomationRule(w http.ResponseWriter, r *http.Request) {
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
	err := s.automationRulesApplication().Delete(r.Context(), sess.UserID, ruleID); err != nil {
		if errors.Is(err, automationapp.ErrRuleNotFound) {
			writeErr(w, http.StatusNotFound, "自动化规则不存在")
			return
		}
		if errors.Is(err, automationapp.ErrRuleActive) {
			writeErr(w, http.StatusConflict, "规则仍有运行中或待人工处理的任务，处理完成后才能删除")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// automationRuleDraft 将 HTTP 请求转换为应用层规则草稿。
func automationRuleDraft(req automationRuleRequest) automationapp.RuleDraft {
	// actions 保存转换后的应用层动作草稿，不携带数据库模型。
	actions := make([]automationapp.ActionDraft, 0, len(req.Actions))
	// action 是当前待转换的 HTTP 动作请求。
	for _, action := range req.Actions {
		actions = append(actions, automationapp.ActionDraft{ActionType: action.ActionType, CardID: action.CardID,
			DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate, DelaySeconds: action.DelaySeconds,
			ConfigJSON: action.ConfigJSON, Enabled: action.Enabled, SortOrder: action.SortOrder})
	}
	return automationapp.RuleDraft{CookieID: req.CookieID, ItemID: req.ItemID, Name: req.Name, TriggerType: req.TriggerType,
		Enabled: req.Enabled, Priority: req.Priority, ConfigJSON: req.ConfigJSON, Actions: actions}
}

// defaultAutomationRuleName 保留旧测试和兼容调用所需的默认名称函数。
func defaultAutomationRuleName(triggerType, itemID string) string {
	// name 保存按触发类型选择的默认规则名称，必要时附加商品标识。
	name := map[string]string{automationapp.TriggerOrderPaid: "付款后自动发货", automationapp.TriggerBuyerReviewed: "评价后发送赠品", automationapp.TriggerReviewMissingTimeout: "超时未评价求评价"}[triggerType]
	if name == "" {
		name = "自动化规则"
	}
	if strings.TrimSpace(itemID) != "" {
		return name + " - " + strings.TrimSpace(itemID)
	}
	return name
}

// isJSONObject 保留规则配置测试需要的 JSON 对象判断。
func isJSONObject(raw string) bool {
	// value 保存 JSON 对象解析结果，仅用于判断配置顶层类型。
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil
}

// firstNonZero 保留旧兼容调用的非零回退行为。
func firstNonZero(value, fallback int) int {
	if value != 0 {
		return value
	}
	return fallback
}
