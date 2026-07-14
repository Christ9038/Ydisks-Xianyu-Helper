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

func (s *Server) mountAutomation(r chi.Router) {
	r.Get("/automation-rules", s.listAutomationRules)
	r.Post("/automation-rules", s.createAutomationRule)
	r.Put("/automation-rules/{rule_id}", s.updateAutomationRule)
	r.Delete("/automation-rules/{rule_id}", s.deleteAutomationRule)
	r.Get("/automation-issues", s.listAutomationIssues)
	r.Post("/automation-runs/{run_id}/resolve", s.resolveAutomationRun)
	r.Post("/automation-pending-tasks/{task_id}/resolve", s.resolveDeferredAutomationTask)
}

func (s *Server) listAutomationIssues(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	runs, tasks, err := s.Store.Automation.ListIssues(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化异常任务失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "pending_tasks": tasks})
}

func (s *Server) resolveAutomationRun(w http.ResponseWriter, r *http.Request) {
	runID, err := strconv.ParseInt(chi.URLParam(r, "run_id"), 10, 64)
	if err != nil || runID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效运行ID")
		return
	}
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if err := s.Store.Automation.ResolveRunIssue(r.Context(), sess.UserID, runID, strings.TrimSpace(req.Resolution)); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "异常运行不存在或已处理")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) resolveDeferredAutomationTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效任务ID")
		return
	}
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Resolution != "retry" && req.Resolution != "dismiss") {
		writeErr(w, http.StatusBadRequest, "处理方式必须是 retry 或 dismiss")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if err := s.Store.Automation.ResolveDeferredIssue(r.Context(), sess.UserID, taskID, req.Resolution == "retry"); err != nil {
		writeErr(w, http.StatusNotFound, "异常任务不存在或已处理")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

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

func (s *Server) listAutomationRules(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	rules, err := s.Store.Automation.ListForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化规则失败")
		return
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		actions := make([]map[string]any, 0, len(rule.Actions))
		for _, action := range rule.Actions {
			actions = append(actions, map[string]any{
				"id": action.ID, "action_type": action.ActionType, "card_id": action.CardID,
				"card_name": action.CardName, "delivery_count": action.DeliveryCount,
				"message_template": action.MessageTemplate, "delay_seconds": action.DelaySeconds,
				"config_json": action.ConfigJSON, "enabled": action.Enabled, "sort_order": action.SortOrder,
			})
		}
		out = append(out, map[string]any{
			"id": rule.ID, "cookie_id": rule.CookieID, "item_id": rule.ItemID, "item_title": rule.ItemTitle,
			"name": rule.Name, "trigger_type": rule.TriggerType, "enabled": rule.Enabled,
			"priority": rule.Priority, "config_json": rule.ConfigJSON, "actions": actions,
			"created_at": rule.CreatedAt, "updated_at": rule.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createAutomationRule(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	var req automationRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	in, err := s.normalizeAutomationRuleRequest(r, sess.UserID, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.Store.Automation.Create(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	var req automationRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	in, err := s.normalizeAutomationRuleRequest(r, sess.UserID, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.Automation.Update(r.Context(), sess.UserID, ruleID, in); err != nil {
		if err == db.ErrNotFound {
			writeErr(w, http.StatusNotFound, "自动化规则不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新自动化规则失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if err := s.Store.Automation.Delete(r.Context(), sess.UserID, ruleID); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

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
	actions := make([]db.AutomationActionInput, 0, len(req.Actions))
	hasSendCard, hasSendText, hasConfirmShipment := false, false, false
	for i, act := range req.Actions {
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
		if act.DelaySeconds < 0 || act.DelaySeconds > 86400 {
			return db.AutomationRuleInput{}, errStr("动作延时必须在 0 到 86400 秒之间")
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

func defaultAutomationRuleName(triggerType, itemID string) string {
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

func isJSONObject(s string) bool {
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}

func firstNonZero(v, fallback int) int {
	if v != 0 {
		return v
	}
	return fallback
}
