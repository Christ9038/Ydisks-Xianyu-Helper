package automation

import (
	"encoding/json"
	"fmt"

	"xianyu-go/internal/db"
)

// deliveryReplayPlan 校验 run 的加密内容数量与原始任务计划；返回可安全补齐的单个直接卡密动作。
// 不读取现行规则，防止规则编辑改变历史订单的发货义务；缺少计划或存在未执行动作时返回人工核对错误。
func deliveryReplayPlan(run *db.AutomationRun) (db.AutomationAction, error) {
	if run == nil {
		return db.AutomationAction{}, fmt.Errorf("订单发货运行不存在")
	}
	if run.DeliveryProof.RefillPending {
		return db.AutomationAction{}, fmt.Errorf("上次补取卡密结果未可靠保存，已禁止再次取卡，请先人工核对")
	}
	if run.DeliveryProof.ExpectedUnits <= 0 || run.DeliveryProof.PreparedUnits < 0 || run.DeliveryProof.UnknownUnits < 0 || run.DeliveryProof.PreparedUnits > run.DeliveryProof.ExpectedUnits-run.DeliveryProof.UnknownUnits {
		return db.AutomationAction{}, fmt.Errorf("订单发货快照缺少有效逐单位记录，请先人工核对")
	}
	// original 保存运行创建时固定的订单数量、规格和完整动作计划，不含可用于发送的明文凭证。
	var original Task
	if err := json.Unmarshal([]byte(run.RawEventJSON), &original); err != nil { // err 表示历史任务快照损坏，不能猜测缺失的动作。
		return db.AutomationAction{}, fmt.Errorf("订单缺少有效的原始发货计划，请先人工核对")
	}
	if original.AccountID != run.CookieID || original.OrderID != run.OrderID || len(original.ActionPlan) == 0 {
		return db.AutomationAction{}, fmt.Errorf("订单原始发货计划缺失或归属不符，请先人工核对")
	}
	// plannedUnits 累计完整计划的单位数；deliveryActions 统计发货动作数，用于判定剩余内容是否可唯一定位。
	plannedUnits, deliveryActions := 0, 0
	// templateActions 保存原始模板动作及其动作下标，用于验证合法跳过凭证。
	templateActions := make(map[int]db.AutomationAction)
	// refillAction 保存唯一直接卡密动作的原始配置，禁止从已编辑规则获取替代卡组。
	var refillAction db.AutomationAction
	for actionIndex, action := range original.ActionPlan { // actionIndex、action 分别表示原始计划动作下标和当前动作，确认发货等无内容动作不计入单位数。
		if !action.Enabled || !isDeliveryAction(action) || !actionMatchesOrderSpec(original, action) {
			continue
		}
		deliveryActions++
		if action.ActionType == ActionSendCard {
			plannedUnits += deliverySendCount(original, action)
			refillAction = action
		} else {
			plannedUnits += len(templateMessageItems(action))
			templateActions[actionIndex] = action
		}
	}
	// seenSkips 拒绝重复位置；跳过凭证必须指向原始启用模板动作和有效消息下标。
	seenSkips := make(map[string]struct{}, len(run.DeliveryProof.SkippedTemplateMessages))
	// skippedUnits 记录凭证中已经确认合法跳过的模板消息数量。
	skippedUnits := 0
	for _, skipped := range run.DeliveryProof.SkippedTemplateMessages { // skipped 表示一个已经确认渲染为空的模板消息位置。
		// action、ok 分别表示跳过位置对应的原始模板动作和查找是否成功。
		action, ok := templateActions[skipped.ActionIndex]
		// messages 使用原始有序图文计划，图片没有“渲染为空”的合法跳过语义。
		messages := templateMessageItems(action)
		if !ok || skipped.MessageIndex < 0 || skipped.MessageIndex >= len(messages) {
			return db.AutomationAction{}, fmt.Errorf("订单模板跳过凭证位置无效，请先人工核对")
		}
		if messages[skipped.MessageIndex].Type == "image" {
			return db.AutomationAction{}, fmt.Errorf("订单图片模板不能跳过发送，请先人工核对")
		}
		// key 是动作下标与消息下标组成的唯一位置键。
		key := fmt.Sprintf("%d:%d", skipped.ActionIndex, skipped.MessageIndex)
		// exists 表示该模板消息位置是否已经在凭证中出现。
		if _, exists := seenSkips[key]; exists {
			return db.AutomationAction{}, fmt.Errorf("订单模板跳过凭证重复，请先人工核对")
		}
		seenSkips[key] = struct{}{}
		skippedUnits++
	}
	if plannedUnits <= 0 || plannedUnits != run.DeliveryProof.ExpectedUnits {
		return db.AutomationAction{}, fmt.Errorf("订单仍有未完成的发货动作或快照与原始计划不符，不能确认整单发货，请先人工核对")
	}
	if run.DeliveryProof.PreparedUnits+run.DeliveryProof.UnknownUnits+skippedUnits > plannedUnits {
		return db.AutomationAction{}, fmt.Errorf("订单发货快照单位数量超过原始计划，请先人工核对")
	}
	// remaining 是已有快照尚未覆盖的单位数；模板与多动作快照缺少逐动作归属，不能把它们归给单个卡组。
	remaining := plannedUnits - run.DeliveryProof.PreparedUnits - run.DeliveryProof.UnknownUnits - skippedUnits
	if remaining > 0 && (deliveryActions != 1 || refillAction.ActionType != ActionSendCard) {
		return db.AutomationAction{}, fmt.Errorf("订单剩余内容无法唯一定位到直接卡密动作，请先人工核对")
	}
	return refillAction, nil
}
