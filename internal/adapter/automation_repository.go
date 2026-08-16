package adapter

import (
	"context"
	"errors"

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
	// err 保存数据库更新失败原因，随后转换为应用层错误。
	err := r.store.Automation.Update(ctx, userID, ruleID, automationRuleInputDB(input))
	return mapAutomationRuleError(err)
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
	card, err := r.store.Cards.Get(ctx, cardID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && (card == nil || card.UserID != userID)) {
		return automationapp.CardInfo{}, automationapp.ErrRuleNotFound
	}
	if err != nil {
		return automationapp.CardInfo{}, err
	}
	return automationapp.CardInfo{Type: card.Type}, nil
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
			actions = append(actions, automationapp.Action{ID: action.ID, ActionType: action.ActionType, CardID: action.CardID,
				CardName: action.CardName, DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate,
				DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled, SortOrder: action.SortOrder})
		}
		result = append(result, automationapp.Rule{ID: rule.ID, CookieID: rule.CookieID, ItemID: rule.ItemID, ItemTitle: rule.ItemTitle,
			Name: rule.Name, TriggerType: rule.TriggerType, Enabled: rule.Enabled, Priority: rule.Priority,
			ConfigJSON: rule.ConfigJSON, Actions: actions, CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt})
	}
	return result
}

// automationRuleInputDB 将应用规则输入转换为数据库写入模型。
func automationRuleInputDB(input automationapp.RuleInput) db.AutomationRuleInput {
	// actions 保存转换后的数据库动作写入模型。
	actions := make([]db.AutomationActionInput, 0, len(input.Actions))
	// action 是当前待转换的应用动作。
	for _, action := range input.Actions {
		actions = append(actions, db.AutomationActionInput{ActionType: action.ActionType, CardID: action.CardID,
			DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate, DelaySeconds: action.DelaySeconds,
			ConfigJSON: action.ConfigJSON, Enabled: action.Enabled, SortOrder: action.SortOrder})
	}
	return db.AutomationRuleInput{UserID: input.UserID, CookieID: input.CookieID, ItemID: input.ItemID, Name: input.Name,
		TriggerType: input.TriggerType, Enabled: input.Enabled, Priority: input.Priority, ConfigJSON: input.ConfigJSON, Actions: actions}
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
