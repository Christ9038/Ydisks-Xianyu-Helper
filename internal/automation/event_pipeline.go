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
		IsBargain:   confirmedBargainPointer(task.IsBargain),
	}); err != nil {
		return fmt.Errorf("记录自动化事件订单事实: %w", err)
	}
	switch task.TriggerType {
	case TriggerOrderPaid:
		if // err 用于本次流程后续判断的err
		err := r.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "paid_at"); err != nil {
			return fmt.Errorf("记录订单付款时间: %w", err)
		}
		if task.IsBargain {
			// readyErr 保存最终成功小刀阶段事实的写入错误，供最终卡片丢失时的安全兜底使用。
			if readyErr := r.store.Automation.MarkBargainReady(ctx, task.OrderID, task.AccountID); readyErr != nil {
				return fmt.Errorf("记录成功小刀阶段: %w", readyErr)
			}
		}
	case TriggerBuyerReviewed:
		if // err 用于本次流程后续判断的err
		err := r.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "buyer_reviewed_at"); err != nil {
			return fmt.Errorf("记录买家评价时间: %w", err)
		}
	case TriggerOrderCompleted:
		if // err 保存买家确认收货后订单完成时间写入错误。
		err := r.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "completed_at"); err != nil {
			return fmt.Errorf("记录订单完成时间: %w", err)
		}
	}
	return nil
}

// confirmedBargainPointer 只把事件明确识别出的砍价事实写回订单；普通事件不携带“非砍价”的权威结论，
// 因而返回 nil 以保留订单同步或先前砍价事件已经持久化的保护标记。
func confirmedBargainPointer(value bool) *bool {
	if !value {
		return nil
	}
	return &value
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
		// 规则被管理员停用后，不得再按冻结计划继续执行外部动作；恢复链路 runRecoveryTasks 已有同样的停用检查。
		if rule == nil || !rule.Enabled || rule.SKUMigrationStatus != "ready" {
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

// ruleAllowsAllItems 判断规则是否明确授权账号级付款发货动作适用于全部商品。
// 只有账号级 order_paid 规则的 JSON 布尔值为 true 时才返回 true，避免把商品级或旧规则误当通配规则。
func ruleAllowsAllItems(rule db.AutomationRule, triggerType string) bool {
	if triggerType != TriggerOrderPaid || rule.TriggerType != TriggerOrderPaid || rule.ItemID != "" {
		return false
	}
	// scope 保存规则中由用户明确确认的全商品适用范围。
	var scope struct {
		// AllowAllItems 表示账号级发货内容是否经过用户确认可用于全部商品。
		AllowAllItems bool `json:"allow_all_items"`
	}
	if json.Unmarshal([]byte(rule.ConfigJSON), &scope) != nil {
		return false
	}
	return scope.AllowAllItems
}

// isDeliveryAction 判断动作是否会产生订单发货消息。
func isDeliveryAction(action db.AutomationAction) bool {
	return action.ActionType == ActionSendCard || action.ActionType == ActionSendTemplate
}

// plan 根据触发类型筛选可执行动作，并保留付款事件的发卡优先顺序。
func (actionPlanner) plan(task Task, actions []db.AutomationAction) []db.AutomationAction {
	// out 用于本次流程后续判断的out
	out := make([]db.AutomationAction, 0, len(actions))
	if task.TriggerType == TriggerOrderPaid {
		// action 表示当前遍历过程中的动作
		for _, action := range actions {
			if action.Enabled && isDeliveryAction(action) && actionMatchesOrderSpec(task, action) {
				out = append(out, action)
			}
		}
		// hasConfirmAction 表示规则是否显式配置过确认发货动作（无论启用与否）；显式停用代表规则作者的有意选择，必须尊重。
		hasConfirmAction := false
		// action 表示当前遍历过程中的动作
		for _, action := range actions {
			if action.ActionType == ActionConfirmShipment {
				hasConfirmAction = true
			}
			if action.Enabled && action.ActionType == ActionConfirmShipment {
				out = append(out, action)
			}
		}
		if !hasConfirmAction && len(out) > 0 {
			// 规则没有任何确认发货动作（直接调 API 创建或旧版本遗留）时补一步隐式确认发货：是否真正调用平台接口仍由账号“自动确认发货”开关决定，
			// 开关关闭时该动作直接返回，不产生外部副作用，因此账号开关不会被规则缺少动作而静默架空。
			out = append(out, db.AutomationAction{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: len(out) + 1})
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
		if action.Enabled && isDeliveryAction(action) && actionMatchesOrderSpec(task, action) {
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
		if out[i].ActionType != ActionSendCard && out[i].ActionType != ActionSendTemplate {
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
