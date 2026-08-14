package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

// mountAutomation 负责mount自动化相关处理。
func (s *Server) mountAutomation(r chi.Router) {
	r.Get("/automation-rules", s.listAutomationRules)
	r.Post("/automation-rules", s.createAutomationRule)
	r.Put("/automation-rules/{rule_id}", s.updateAutomationRule)
	r.Delete("/automation-rules/{rule_id}", s.deleteAutomationRule)
	r.Get("/automation-issues", s.listAutomationIssues)
	r.Post("/automation-runs/{run_id}/resolve", s.resolveAutomationRun)
	r.Post("/automation-pending-tasks/{task_id}/resolve", s.resolveDeferredAutomationTask)
}

// listAutomationIssues 负责list自动化问题列表相关处理。
func (s *Server) listAutomationIssues(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// runs、tasks、err 保存runs、tasks、err，供当前处理流程使用
	runs, tasks, err := s.Store.Automation.ListIssues(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化异常任务失败")
		return
	}
	writeJSON(w, http.StatusOK, automationIssuesResponse{Runs: runs, PendingTasks: tasks})
}

// resolveAutomationRun 负责resolve自动化运行相关处理。
func (s *Server) resolveAutomationRun(w http.ResponseWriter, r *http.Request) {
	// runID、err 保存运行ID、err，供当前处理流程使用
	runID, err := strconv.ParseInt(chi.URLParam(r, "run_id"), 10, 64)
	if err != nil || runID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效运行ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Resolution string `json:"resolution"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
	err := s.Store.Automation.ResolveRunIssue(r.Context(), sess.UserID, runID, strings.TrimSpace(req.Resolution)); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "异常运行不存在或已处理")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// resolveDeferredAutomationTask 负责resolveDeferred自动化任务相关处理。
func (s *Server) resolveDeferredAutomationTask(w http.ResponseWriter, r *http.Request) {
	// taskID、err 保存任务ID、err，供当前处理流程使用
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效任务ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Resolution string `json:"resolution"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || (req.Resolution != "retry" && req.Resolution != "dismiss") {
		writeErr(w, http.StatusBadRequest, "处理方式必须是 retry 或 dismiss")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
	err := s.Store.Automation.ResolveDeferredIssue(r.Context(), sess.UserID, taskID, req.Resolution == "retry"); err != nil {
		writeErr(w, http.StatusNotFound, "异常任务不存在或已处理")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// automationActionRequest 保存自动化动作请求，供当前处理流程使用
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

// automationRuleRequest 保存自动化规则请求，供当前处理流程使用
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

// listAutomationRules 负责list自动化规则列表相关处理。
func (s *Server) listAutomationRules(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// query 保存查询，供当前处理流程使用
	query := r.URL.Query()
	// paginated 保存paginated，供当前处理流程使用
	_, paginated := query["page"]
	if !paginated {
		// rules、err 保存rules、err，供当前处理流程使用
		rules, err := s.Store.Automation.ListForUser(r.Context(), sess.UserID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "查询自动化规则失败")
			return
		}
		writeJSON(w, http.StatusOK, automationRulesJSON(rules))
		return
	}

	// page 保存页码，供当前处理流程使用
	page := atoiDefault(query.Get("page"), 1)
	// pageSize 保存每页数量，供当前处理流程使用
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
	// cookieID 保存登录凭证ID，供当前处理流程使用
	cookieID := strings.TrimSpace(query.Get("cookie_id"))
	if cookieID != "" {
		if !s.cookieOwnedByUser(r.Context(), sess.UserID, cookieID) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
	}
	// triggerType 保存trigger类型，供当前处理流程使用
	triggerType := strings.TrimSpace(query.Get("trigger_type"))
	if triggerType != "" {
		switch triggerType {
		case automation.TriggerOrderPaid, automation.TriggerBuyerReviewed, automation.TriggerReviewMissingTimeout:
		default:
			writeErr(w, http.StatusBadRequest, "不支持的触发类型")
			return
		}
	}
	// enabled 保存启用状态，供当前处理流程使用
	var enabled *bool
	if // rawEnabled 保存原始启用状态，供当前处理流程使用
	rawEnabled := strings.TrimSpace(query.Get("enabled")); rawEnabled != "" {
		// value、parseErr 保存value、parseErr，供当前处理流程使用
		value, parseErr := strconv.ParseBool(rawEnabled)
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, "启用状态必须是 true 或 false")
			return
		}
		enabled = &value
	}

	// rules、total、err 保存rules、total、err，供当前处理流程使用
	rules, total, err := s.Store.Automation.ListPageForUser(r.Context(), db.AutomationRuleListFilter{
		UserID: sess.UserID, CookieID: cookieID, TriggerType: triggerType, Enabled: enabled,
		Search: query.Get("search"), Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化规则失败")
		return
	}
	// filter 保存filter，供当前处理流程使用
	filter := db.AutomationRuleListFilter{
		UserID: sess.UserID, CookieID: cookieID, TriggerType: triggerType, Enabled: enabled,
		Search: query.Get("search"),
	}
	// triggerCounts、err 保存triggerCounts、err，供当前处理流程使用
	triggerCounts, err := s.Store.Automation.CountByTriggerForUser(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计自动化规则失败")
		return
	}
	// totalPages 保存总数Pages，供当前处理流程使用
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages > 0 && page > totalPages {
		page = totalPages
		rules, _, err = s.Store.Automation.ListPageForUser(r.Context(), db.AutomationRuleListFilter{
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

// automationRulesJSON 负责自动化规则列表JSON相关处理。
func automationRulesJSON(rules []db.AutomationRule) []automationRuleResponse {
	// out 保存out，供当前处理流程使用
	out := make([]automationRuleResponse, 0, len(rules))
	// rule 表示当前遍历过程中的规则
	for _, rule := range rules {
		// actions 保存动作列表，供当前处理流程使用
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
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// req 保存req，供当前处理流程使用
	var req automationRuleRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// in、err 保存in、err，供当前处理流程使用
	in, err := s.normalizeAutomationRuleRequest(r, sess.UserID, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// id、err 保存id、err，供当前处理流程使用
	id, err := s.Store.Automation.Create(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateAutomationRule 负责update自动化规则相关处理。
func (s *Server) updateAutomationRule(w http.ResponseWriter, r *http.Request) {
	// ruleID、err 保存规则ID、err，供当前处理流程使用
	ruleID, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// req 保存req，供当前处理流程使用
	var req automationRuleRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// in、err 保存in、err，供当前处理流程使用
	in, err := s.normalizeAutomationRuleRequest(r, sess.UserID, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Automation.Update(r.Context(), sess.UserID, ruleID, in); err != nil {
		if err == db.ErrNotFound {
			writeErr(w, http.StatusNotFound, "自动化规则不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteAutomationRule 负责delete自动化规则相关处理。
func (s *Server) deleteAutomationRule(w http.ResponseWriter, r *http.Request) {
	// ruleID、err 保存规则ID、err，供当前处理流程使用
	ruleID, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
	err := s.Store.Automation.Delete(r.Context(), sess.UserID, ruleID); err != nil {
		if err == db.ErrNotFound {
			writeErr(w, http.StatusNotFound, "自动化规则不存在")
			return
		}
		if errors.Is(err, db.ErrAutomationRunActive) {
			writeErr(w, http.StatusConflict, "规则仍有运行中或待人工处理的任务，处理完成后才能删除")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// normalizeAutomationRuleRequest 负责normalize自动化规则请求相关处理。
func (s *Server) normalizeAutomationRuleRequest(r *http.Request, userID int64, req automationRuleRequest) (db.AutomationRuleInput, error) {
	req.CookieID = strings.TrimSpace(req.CookieID)
	req.ItemID = strings.TrimSpace(req.ItemID)
	req.Name = strings.TrimSpace(req.Name)
	req.TriggerType = strings.TrimSpace(req.TriggerType)
	switch req.TriggerType {
	case automation.TriggerOrderPaid, automation.TriggerBuyerReviewed, automation.TriggerReviewMissingTimeout:
	default:
		return db.AutomationRuleInput{}, errStr("不支持的触发类型")
	}
	if req.CookieID == "" || !s.cookieOwnedByUser(r.Context(), userID, req.CookieID) {
		return db.AutomationRuleInput{}, errStr("账号不存在或不属于当前用户")
	}
	if req.ItemID != "" && !s.itemOwnedByUser(r, userID, req.CookieID, req.ItemID) {
		return db.AutomationRuleInput{}, errStr("商品不属于当前用户")
	}
	if req.Priority <= 0 {
		req.Priority = 100
	}
	// config 保存配置，供当前处理流程使用
	config := req.ConfigJSON
	if config == "" {
		config = "{}"
	}
	if !isJSONObject(config) {
		return db.AutomationRuleInput{}, errStr("规则配置必须是 JSON 对象")
	}
	if len(req.Actions) == 0 {
		return db.AutomationRuleInput{}, errStr("至少需要一个自动化动作")
	}
	if req.Name == "" {
		req.Name = defaultAutomationRuleName(req.TriggerType, req.ItemID)
	}
	// actions 保存动作列表，供当前处理流程使用
	actions := make([]db.AutomationActionInput, 0, len(req.Actions))
	// hasSendCard、hasSendText、hasConfirmShipment 保存hasSendCard、hasSendText、hasConfirmShipment，供当前处理流程使用
	hasSendCard, hasSendText, hasConfirmShipment := false, false, false
	// i、act 表示当前遍历过程中的i、act
	for i, act := range req.Actions {
		// enabled 保存启用状态，供当前处理流程使用
		enabled := true
		if act.Enabled != nil {
			enabled = *act.Enabled
		}
		act.ActionType = strings.TrimSpace(act.ActionType)
		switch act.ActionType {
		case automation.ActionConfirmShipment:
			hasConfirmShipment = hasConfirmShipment || enabled
		case automation.ActionSendCard:
			if act.CardID <= 0 {
				return db.AutomationRuleInput{}, errStr("发送卡密动作必须选择卡密组")
			}
			// card、cardErr 保存card、cardErr，供当前处理流程使用
			card, cardErr := s.Store.Cards.Get(r.Context(), act.CardID)
			if cardErr != nil || card.UserID != userID {
				return db.AutomationRuleInput{}, errStr("卡密组不存在或不属于当前用户")
			}
			if card.Type == "api" {
				return db.AutomationRuleInput{}, errStr("API 卡密暂不支持自动发货，请选择文本、批量数据或图片卡密")
			}
			hasSendCard = hasSendCard || enabled
		case automation.ActionSendText:
			if strings.TrimSpace(act.MessageTemplate) == "" {
				return db.AutomationRuleInput{}, errStr("发送文本动作必须填写文案")
			}
			hasSendText = hasSendText || enabled
		default:
			return db.AutomationRuleInput{}, errStr("不支持的动作类型")
		}
		if act.DeliveryCount <= 0 {
			act.DeliveryCount = 1
		}
		if act.DelaySeconds < 0 || act.DelaySeconds > 3600 {
			return db.AutomationRuleInput{}, errStr("动作延时必须在 0 到 3600 秒之间")
		}
		if act.ConfigJSON == "" {
			act.ConfigJSON = "{}"
		}
		if !isJSONObject(act.ConfigJSON) {
			return db.AutomationRuleInput{}, errStr("动作配置必须是 JSON 对象")
		}
		actions = append(actions, db.AutomationActionInput{
			ActionType: act.ActionType, CardID: act.CardID, DeliveryCount: act.DeliveryCount,
			MessageTemplate: act.MessageTemplate, DelaySeconds: act.DelaySeconds, ConfigJSON: act.ConfigJSON,
			Enabled: enabled, SortOrder: firstNonZero(act.SortOrder, i+1),
		})
	}
	switch req.TriggerType {
	case automation.TriggerOrderPaid:
		if !hasSendCard {
			return db.AutomationRuleInput{}, errStr("付款后自动发货至少需要一个已启用的发送卡密动作")
		}
	case automation.TriggerBuyerReviewed:
		if hasConfirmShipment {
			return db.AutomationRuleInput{}, errStr("评价后规则不能包含确认发货动作")
		}
		if !hasSendCard && !hasSendText {
			return db.AutomationRuleInput{}, errStr("评价后规则至少需要一个已启用的发送动作")
		}
	case automation.TriggerReviewMissingTimeout:
		if hasConfirmShipment || hasSendCard {
			return db.AutomationRuleInput{}, errStr("求评价规则只能发送文本")
		}
		if !hasSendText {
			return db.AutomationRuleInput{}, errStr("求评价规则至少需要一个已启用的文本动作")
		}
	}
	return db.AutomationRuleInput{
		UserID: userID, CookieID: req.CookieID, ItemID: req.ItemID, Name: req.Name,
		TriggerType: req.TriggerType, Enabled: req.Enabled, Priority: req.Priority,
		ConfigJSON: config, Actions: actions,
	}, nil
}

// defaultAutomationRuleName 负责default自动化规则名称相关处理。
func defaultAutomationRuleName(triggerType, itemID string) string {
	// name 保存名称，供当前处理流程使用
	name := map[string]string{
		automation.TriggerOrderPaid:            "付款后自动发货",
		automation.TriggerBuyerReviewed:        "评价后发送赠品",
		automation.TriggerReviewMissingTimeout: "超时未评价求评价",
	}[triggerType]
	if name == "" {
		name = "自动化规则"
	}
	if strings.TrimSpace(itemID) != "" {
		return name + " - " + strings.TrimSpace(itemID)
	}
	return name
}

// isJSONObject 负责isJSONObject相关处理。
func isJSONObject(s string) bool {
	// m 保存m，供当前处理流程使用
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}

// firstNonZero 负责firstNonZero相关处理。
func firstNonZero(v, fallback int) int {
	if v != 0 {
		return v
	}
	return fallback
}
