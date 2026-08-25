package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// AutomationRepository 将 Store 的自动化异常查询与 resolve 能力适配为应用 Port。
type AutomationRepository struct {
	// store 保存数据库聚合入口，仅由该基础设施适配器访问。
	store *db.Store
}

// NewAutomationRepository 构造自动化异常应用 Port 的数据库适配器。
func NewAutomationRepository(store *db.Store) *AutomationRepository {
	return &AutomationRepository{store: store}
}

// ListIssues 查询并转换当前用户可见的自动化异常摘要。
func (r *AutomationRepository) ListIssues(ctx context.Context, userID int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error) {
	// runIssues、deferredIssues、err 保存数据库异常摘要和查询错误。
	runIssues, deferredIssues, err := r.store.Automation.ListIssues(ctx, userID)
	if err != nil {
		return nil, nil, mapAutomationIssueError(err)
	}
	// runs 是不携带数据库模型的运行异常摘要列表。
	runs := make([]automationapp.RunIssue, 0, len(runIssues))
	// runIssue 是当前待转换的数据库运行异常记录。
	for _, runIssue := range runIssues {
		runs = append(runs, automationapp.RunIssue{
			ID: runIssue.ID, CookieID: runIssue.CookieID, OrderID: runIssue.OrderID,
			TriggerType: runIssue.TriggerType, ErrorMessage: runIssue.ErrorMessage,
			IssueKind: runIssue.IssueKind, AllowedResolutions: runIssue.AllowedResolutions,
			ActionCursor: runIssue.ActionCursor, SentCount: runIssue.SentCount, UpdatedAt: runIssue.UpdatedAt,
		})
	}
	// tasks 是不携带数据库模型的延期异常摘要列表。
	tasks := make([]automationapp.DeferredIssue, 0, len(deferredIssues))
	// deferredIssue 是当前待转换的数据库延期异常记录。
	for _, deferredIssue := range deferredIssues {
		tasks = append(tasks, automationapp.DeferredIssue{
			ID: deferredIssue.ID, CookieID: deferredIssue.CookieID, TriggerType: deferredIssue.TriggerType,
			ErrorMessage: deferredIssue.ErrorMessage, AttemptCount: deferredIssue.AttemptCount, UpdatedAt: deferredIssue.UpdatedAt,
		})
	}
	return runs, tasks, nil
}

// ResolveRunIssue 按用户归属执行异常运行人工处理，并归一化未找到错误。
func (r *AutomationRepository) ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error {
	return mapAutomationIssueError(r.store.Automation.ResolveRunIssue(ctx, userID, runID, resolution))
}

// ResolveDeferredIssue 按用户归属重试或删除死信延期任务，并归一化未找到错误。
func (r *AutomationRepository) ResolveDeferredIssue(ctx context.Context, userID, taskID int64, retry bool) error {
	return mapAutomationIssueError(r.store.Automation.ResolveDeferredIssue(ctx, userID, taskID, retry))
}

// mapAutomationIssueError 将数据库未找到错误转换为应用层错误，避免 Port 暴露数据库包。
func mapAutomationIssueError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return automationapp.ErrNotFound
	}
	return err
}

// ListForUser 返回用户全部自动化规则的应用模型。
func (r *AutomationRepository) ListForUser(ctx context.Context, userID int64) ([]automationapp.Rule, error) {
	// rules、err 保存数据库规则列表及查询失败原因。
	rules, err := r.store.Automation.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return automationRulesModel(rules), nil
}

// ListPageForUser 返回用户自动化规则分页及总数。
func (r *AutomationRepository) ListPageForUser(ctx context.Context, filter automationapp.RuleFilter) ([]automationapp.Rule, int, error) {
	// rules、total、err 保存分页规则、总数及数据库查询失败原因。
	rules, total, err := r.store.Automation.ListPageForUser(ctx, db.AutomationRuleListFilter{
		UserID: filter.UserID, CookieID: filter.CookieID, TriggerType: filter.TriggerType,
		Enabled: filter.Enabled, Search: filter.Search, Limit: filter.Limit, Offset: filter.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return automationRulesModel(rules), total, nil
}

// CountByTriggerForUser 返回用户规则触发类型统计。
func (r *AutomationRepository) CountByTriggerForUser(ctx context.Context, filter automationapp.RuleFilter) (map[string]int, error) {
	return r.store.Automation.CountByTriggerForUser(ctx, db.AutomationRuleListFilter{
		UserID: filter.UserID, CookieID: filter.CookieID, TriggerType: filter.TriggerType,
		Enabled: filter.Enabled, Search: filter.Search,
	})
}

// Create 将应用层规则输入转换为数据库模型并创建规则。
func (r *AutomationRepository) Create(ctx context.Context, input automationapp.RuleInput) (int64, error) {
	// unlock 串行化固定改价规则与 AI 议价设置的最终冲突检查和写入。
	unlock := r.store.LockPricingMode()
	defer unlock()
	if automationInputEnablesAdjustPrice(input) {
		// aiEnabled 表示最终写入时账号是否已经开启 AI 议价；aiErr 是开关读取错误。
		aiEnabled, aiErr := r.store.AIReply.IsEnabled(ctx, input.CookieID)
		if aiErr != nil {
			return 0, aiErr
		}
		if aiEnabled {
			return 0, automationapp.ErrPricingModeConflict
		}
	}
	return r.store.Automation.Create(ctx, automationRuleInputDB(input))
}

// EnsurePublishRule 将发布自动化规则输入转换为数据库模型并执行幂等创建。
func (r *AutomationRepository) EnsurePublishRule(ctx context.Context, input automationapp.RuleInput) error {
	// databaseInput 保存发布自动化规则对应的数据库写入模型。
	databaseInput := automationRuleInputDB(input)
	// exists、err 保存同一发布规则的存在状态及查询错误。
	exists, err := r.store.Automation.ExistsPublishRule(ctx, databaseInput)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	// _, err 保存首次创建发布自动化规则的结果。
	_, err = r.store.Automation.Create(ctx, databaseInput)
	return err
}

// Update 将应用层规则输入转换为数据库模型并更新规则。
func (r *AutomationRepository) Update(ctx context.Context, userID, ruleID int64, input automationapp.RuleInput) error {
	// unlock 串行化固定改价规则与 AI 议价设置的最终冲突检查和写入。
	unlock := r.store.LockPricingMode()
	defer unlock()
	if automationInputEnablesAdjustPrice(input) {
		// aiEnabled 表示最终写入时账号是否已经开启 AI 议价；aiErr 是开关读取错误。
		aiEnabled, aiErr := r.store.AIReply.IsEnabled(ctx, input.CookieID)
		if aiErr != nil {
			return aiErr
		}
		if aiEnabled {
			return automationapp.ErrPricingModeConflict
		}
	}
	// err 保存数据库更新失败原因，随后转换为应用层错误。
	err := r.store.Automation.Update(ctx, userID, ruleID, automationRuleInputDB(input))
	return mapAutomationRuleError(err)
}

// automationInputEnablesAdjustPrice 判断规则输入是否会实际启用固定订单改价动作。
func automationInputEnablesAdjustPrice(input automationapp.RuleInput) bool {
	if !input.Enabled {
		return false
	}
	// action 是当前待检查的规则动作。
	for _, action := range input.Actions {
		if action.Enabled && action.ActionType == automationapp.ActionAdjustPrice {
			return true
		}
	}
	return false
}

// Delete 删除用户拥有的规则并转换数据库错误边界。
func (r *AutomationRepository) Delete(ctx context.Context, userID, ruleID int64) error {
	return mapAutomationRuleError(r.store.Automation.Delete(ctx, userID, ruleID))
}

// OwnsAccount 返回账号是否属于指定用户。
func (r *AutomationRepository) OwnsAccount(ctx context.Context, userID int64, accountID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, accountID)
}

// OwnsItem 返回商品是否属于指定用户账号。
func (r *AutomationRepository) OwnsItem(ctx context.Context, userID int64, accountID, itemID string) (bool, error) {
	// owned、err 保存账号归属查询结果及失败原因。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, accountID)
	if err != nil || !owned {
		return false, err
	}
	// _, err 保存按账号和商品双键读取结果；只有未找到才转换为业务上的不归属。
	_, err = r.store.Items.Get(ctx, accountID, itemID)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetCard 返回用户拥有的卡密组类型，不将卡密内容传入应用层。
func (r *AutomationRepository) GetCard(ctx context.Context, userID, cardID int64) (automationapp.CardInfo, error) {
	// card、err 保存卡密组摘要及读取失败原因；卡密正文不会进入应用层。
	card, err := r.store.Cards.GetSummary(ctx, cardID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && (card == nil || card.UserID != userID)) {
		return automationapp.CardInfo{}, automationapp.ErrRuleNotFound
	}
	if err != nil {
		return automationapp.CardInfo{}, err
	}
	return automationapp.CardInfo{Enabled: card.Enabled, Type: card.Type, APIReady: card.Type != "api" || card.APIConfigSummary != nil && card.APIConfigSummary.Ready}, nil
}

// AIReplyEnabled 判断账号是否启用了 AI 议价，不读取任何 API 密钥或平台凭证。
func (r *AutomationRepository) AIReplyEnabled(ctx context.Context, accountID string) (bool, error) {
	if r == nil || r.store == nil || r.store.AIReply == nil {
		return false, errors.New("AI 设置存储未初始化")
	}
	return r.store.AIReply.IsEnabled(ctx, accountID)
}

// GetDeliveryTemplate 返回规则校验所需的模板非敏感摘要，并确认用户归属。
func (r *AutomationRepository) GetDeliveryTemplate(ctx context.Context, userID, templateID int64) (automationapp.TemplateInfo, error) {
	if r == nil || r.store == nil || r.store.DeliveryTemplates == nil {
		return automationapp.TemplateInfo{}, errors.New("发货模板存储未初始化")
	}
	// template、err 保存模板摘要及读取错误。
	template, err := r.store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
	if errors.Is(err, db.ErrNotFound) {
		return automationapp.TemplateInfo{}, automationapp.ErrRuleNotFound
	}
	if err != nil {
		return automationapp.TemplateInfo{}, err
	}
	return automationapp.TemplateInfo{Enabled: template.Enabled, Keys: append([]string(nil), template.Keys...), CustomKeys: append([]string(nil), template.CustomKeys...)}, nil
}

// automationRulesModel 将数据库规则列表转换为应用模型。
func automationRulesModel(rules []db.AutomationRule) []automationapp.Rule {
	// result 保存从数据库模型转换出的应用规则列表。
	result := make([]automationapp.Rule, 0, len(rules))
	// rule 是当前待转换的数据库规则。
	for _, rule := range rules {
		// actions 保存当前规则转换后的应用动作列表。
		actions := make([]automationapp.Action, 0, len(rule.Actions))
		// action 是当前待转换的数据库动作。
		for _, action := range rule.Actions {
			// bindings 保存应用层模板变量绑定列表。
			bindings := make([]automationapp.TemplateBinding, 0, len(action.TemplateBindings))
			for /* binding 表示当前数据库模板变量绑定。 */ _, binding := range action.TemplateBindings {
				bindings = append(bindings, automationapp.TemplateBinding{VariableKey: binding.VariableKey, CardID: binding.CardID, CardName: binding.CardName, DeliveryCount: binding.DeliveryCount})
			}
			actions = append(actions, automationapp.Action{ID: action.ID, ActionType: action.ActionType, CardID: action.CardID,
				CardName: action.CardName, DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate,
				DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled, SortOrder: action.SortOrder,
				DeliveryTemplateID: action.DeliveryTemplateID, DeliveryTemplateName: action.DeliveryTemplateName,
				TemplateMessages: append([]string(nil), action.TemplateMessages...), TemplateKeys: append([]string(nil), action.TemplateKeys...), TemplateBindings: bindings,
				CustomVariables: customVariablesFromConfig(action.ConfigJSON)})
		}
		result = append(result, automationapp.Rule{ID: rule.ID, CookieID: rule.CookieID, ItemID: rule.ItemID, ItemTitle: rule.ItemTitle,
			Name: rule.Name, TriggerType: rule.TriggerType, Enabled: rule.Enabled, Priority: rule.Priority,
			ConfigJSON: rule.ConfigJSON, Actions: actions, CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt})
	}
	return result
}

// customVariablesFromConfig 从动作配置中读取规则页保存的自定义字符串键值表。
func customVariablesFromConfig(raw string) map[string]string {
	// config 保存动作配置对象，仅读取非敏感的模板变量字段原文。
	var config map[string]json.RawMessage
	// err 保存动作配置解析错误。
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil
	}
	// rawValues 保存自定义变量字段的 JSON 原文。
	rawValues := config["custom_variables"]
	// values 保存新格式的自定义变量键值表。
	var values map[string]string
	if json.Unmarshal(rawValues, &values) == nil && values != nil {
		return copyStringMap(values)
	}
	// legacyValues 保存历史数组格式的自定义变量值。
	var legacyValues []string
	if json.Unmarshal(rawValues, &legacyValues) != nil {
		return nil
	}
	// converted 保存按数组下标转换得到的兼容键值表。
	converted := make(map[string]string, len(legacyValues))
	for /* index 表示历史数组下标；value 表示历史自定义字符串。 */ index, value := range legacyValues {
		converted[strconv.Itoa(index)] = value
	}
	return converted
}

// automationRuleInputDB 将应用规则输入转换为数据库写入模型。
func automationRuleInputDB(input automationapp.RuleInput) db.AutomationRuleInput {
	// actions 保存转换后的数据库动作写入模型。
	actions := make([]db.AutomationActionInput, 0, len(input.Actions))
	// action 是当前待转换的应用动作。
	for _, action := range input.Actions {
		// bindings 保存数据库模板变量绑定列表。
		bindings := make([]db.DeliveryTemplateBinding, 0, len(action.TemplateBindings))
		for /* binding 表示当前应用模板变量绑定。 */ _, binding := range action.TemplateBindings {
			bindings = append(bindings, db.DeliveryTemplateBinding{VariableKey: binding.VariableKey, CardID: binding.CardID, CardName: binding.CardName, DeliveryCount: binding.DeliveryCount})
		}
		actions = append(actions, db.AutomationActionInput{ActionType: action.ActionType, CardID: action.CardID,
			DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate, DelaySeconds: action.DelaySeconds,
			ConfigJSON: action.ConfigJSON, Enabled: action.Enabled, SortOrder: action.SortOrder,
			DeliveryTemplateID: action.DeliveryTemplateID, TemplateBindings: bindings,
			CustomVariables: copyStringMap(action.CustomVariables)})
	}
	return db.AutomationRuleInput{UserID: input.UserID, CookieID: input.CookieID, ItemID: input.ItemID, Name: input.Name,
		TriggerType: input.TriggerType, Enabled: input.Enabled, Priority: input.Priority, ConfigJSON: input.ConfigJSON, Actions: actions}
}

// copyStringMap 复制字符串键值表，避免适配层对象共享可变 map。
func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	// copied 保存字符串键值表的独立副本。
	copied := make(map[string]string, len(values))
	for /* key 表示键值表中的键；value 表示对应字符串。 */ key, value := range values {
		copied[key] = value
	}
	return copied
}

// mapAutomationRuleError 将数据库规则错误转换为应用层错误。
func mapAutomationRuleError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return automationapp.ErrRuleNotFound
	}
	if errors.Is(err, db.ErrAutomationRunActive) {
		return automationapp.ErrRuleActive
	}
	return err
}

// automationRepositoryCompileCheck 确保数据库适配器完整实现应用 Port。
var _ automationapp.IssueRepository = (*AutomationRepository)(nil)
var _ automationapp.RuleRepository = (*AutomationRepository)(nil)
var _ automationapp.RuleOwnership = (*AutomationRepository)(nil)
var _ automationapp.PublishRuleRepository = (*AutomationRepository)(nil)
