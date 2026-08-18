package automation

import (
	"context"
	"encoding/json"
	"fmt"

	"xianyu-go/internal/db"
)

// eventFactRecorder 只负责把已经解析出的事件事实写入持久化层。
// 它不读取规则、不创建运行记录，也不执行任何外部动作。
// eventFactRecorder 用于本次流程后续判断的eventFactRecorder
type eventFactRecorder struct {
	// store 提供订单事实和事件时间的持久化能力。
	store *db.Store
}

// newEventFactRecorder 构造事件事实记录组件。
func newEventFactRecorder(store *db.Store) eventFactRecorder {
	return eventFactRecorder{store: store}
}

// record 将自动化任务中的订单字段和触发时间写入事实表。
func (r eventFactRecorder) record(ctx context.Context, task Task) error {
	if r.store == nil || r.store.Orders == nil || r.store.Automation == nil || task.OrderID == "" {
		return nil
	}
	if // err 用于本次流程后续判断的err
	err := r.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:    task.AccountID,
		ItemID:      task.ItemID,
		BuyerID:     task.BuyerID,
		ChatID:      task.ChatID,
		OrderStatus: task.OrderStatus,
		SpecName:    task.SpecName,
		SpecValue:   task.SpecValue,
		Quantity:    task.Quantity,
		Amount:      task.Amount,
	}); err != nil {
		return fmt.Errorf("记录自动化事件订单事实: %w", err)
	}
	switch task.TriggerType {
	case TriggerOrderPaid:
		if // err 用于本次流程后续判断的err
		err := r.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "paid_at"); err != nil {
			return fmt.Errorf("记录订单付款时间: %w", err)
		}
	case TriggerBuyerReviewed:
		if // err 用于本次流程后续判断的err
		err := r.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "buyer_reviewed_at"); err != nil {
			return fmt.Errorf("记录买家评价时间: %w", err)
		}
	}
	return nil
}

// ruleMatcher 只查询适用于任务的规则，不执行规则动作或修改运行状态。
type ruleMatcher struct {
	// store 提供规则查询和恢复运行读取能力。
	store *db.Store
}

// newRuleMatcher 构造无动作副作用的规则匹配组件。
func newRuleMatcher(store *db.Store) ruleMatcher {
	return ruleMatcher{store: store}
}

// match 查询普通事件或恢复运行对应的规则快照。
func (m ruleMatcher) match(ctx context.Context, task Task) ([]db.AutomationRule, error) {
	if m.store == nil || m.store.Automation == nil {
		return nil, nil
	}
	if // runID 用于本次流程后续判断的运行ID
	runID := taskAutomationRunID(task); runID > 0 {
		// run、err 用于本次流程后续判断的run、err
		run, err := m.store.Automation.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if run.Status != "running" {
			return nil, nil
		}
		// rule、err 用于本次流程后续判断的rule、err
		rule, err := m.store.Automation.Get(ctx, run.RuleID)
		if err != nil {
			return nil, err
		}
		if rule == nil {
			return nil, nil
		}
		return []db.AutomationRule{*rule}, nil
	}
	return m.store.Automation.Match(ctx, task.AccountID, task.ItemID, task.TriggerType)
}

// actionPlanner 只根据任务事实和规则动作生成不可变的动作计划。
// 计划过程不得访问数据库、发送网络请求或修改规则。
// actionPlanner 用于本次流程后续判断的动作Planner
type actionPlanner struct{}

// plan 根据触发类型筛选可执行动作，并保留付款事件的发卡优先顺序。
func (actionPlanner) plan(task Task, actions []db.AutomationAction) []db.AutomationAction {
	// out 用于本次流程后续判断的out
	out := make([]db.AutomationAction, 0, len(actions))
	if task.TriggerType == TriggerOrderPaid {
		// action 表示当前遍历过程中的动作
		for _, action := range actions {
			if action.Enabled && action.ActionType == ActionSendCard && actionMatchesOrderSpec(task, action) {
				out = append(out, action)
			}
		}
		// action 表示当前遍历过程中的动作
		for _, action := range actions {
			if action.Enabled && action.ActionType == ActionConfirmShipment {
				out = append(out, action)
			}
		}
		return out
	}
	// action 表示当前遍历过程中的动作
	for _, action := range actions {
		if action.Enabled {
			out = append(out, action)
		}
	}
	return out
}

// hasMatchingSendCard 判断付款事件是否存在匹配当前规格的发卡动作。
func (actionPlanner) hasMatchingSendCard(task Task, actions []db.AutomationAction) bool {
	// action 表示当前遍历过程中的动作
	for _, action := range actions {
		if action.Enabled && action.ActionType == ActionSendCard && actionMatchesOrderSpec(task, action) {
			return true
		}
	}
	return false
}

// immediateManualActions 复制动作并清除延迟，供明确的人工完整发货使用。
func (actionPlanner) immediateManualActions(actions []db.AutomationAction) []db.AutomationAction {
	// out 用于本次流程后续判断的out
	out := make([]db.AutomationAction, len(actions))
	copy(out, actions)
	// i 表示当前遍历过程中的i
	for i := range out {
		out[i].DelaySeconds = 0
		if out[i].ActionType != ActionSendCard {
			continue
		}
		// config 用于本次流程后续判断的配置
		config := map[string]any{}
		_ = json.Unmarshal([]byte(out[i].ConfigJSON), &config)
		config["delay_override"] = true
		// raw 用于本次流程后续判断的原始
		raw, _ := json.Marshal(config)
		out[i].ConfigJSON = string(raw)
	}
	return out
}
