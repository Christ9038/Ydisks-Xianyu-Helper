import {
AccountDetail,
AutomationAction,
AutomationIssuesEnvelope,
AutomationRulePageResponse,
AutomationRuleResponse,
AutomationTriggerType,
Card,
DefaultReply,
DefaultReplyResponse,
Item,
KeywordTypedResponse,
MutationIDResponse,OperationResponse,
PaginatedResponse,
ReplyRule,
ShippingRule
} from '../../../shared/api-contract/automation';
import { type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom,objectFrom } from '../../../shared/http/contract';
export type * from '../../../shared/api-contract/automation';

/** 自动化规则筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => runContractRequest(/* signal 控制规则页账号摘要读取的取消和超时。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);

/** 自动化动作编辑器读取可选卡券组。 */
export const getCards = async (options?: RequestControlOptions): Promise<Card[]> => runContractRequest(/* signal 控制规则页卡券读取的取消和超时。 */ signal => contractClient.GET('/api/v1/cards', { signal }), options) as unknown as Promise<Card[]>;

/** 自动化规则商品选择器读取商品索引。 */
export const getItems = async (accountID?: string, options?: RequestControlOptions): Promise<Item[]> => runContractRequest(/* signal 控制规则页商品读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items', { params: { query: { cookie_id: accountID } }, signal }), options) as unknown as Promise<Item[]>;
// Rules - 自动化规则
const normalizeShippingRules = (rules: any[]): ShippingRule[] => rules.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
        id: String(item.id),
        name: item.name || '',
        trigger_type: item.trigger_type || 'order_paid',
        item_keyword: item.item_title || item.item_id || '',
        cookie_id: item.cookie_id || '',
        item_id: item.item_id || '',
        item_title: item.item_title || '',
        card_group_id: Number((item.actions || []).find(/* 当前回调用于处理集合元素或接口响应。 */ (a: any) => a.action_type === 'send_card')?.card_id || 0),
        card_group_name: (item.actions || []).find(/* 当前回调用于处理集合元素或接口响应。 */ (a: any) => a.action_type === 'send_card')?.card_name || '',
        priority: item.priority || 100,
        enabled: item.enabled || false,
        config_json: item.config_json || '{}',
        actions: (item.actions || []).map(/* 当前回调用于处理集合元素或接口响应。 */ (action: any) => ({
          id: action.id ? String(action.id) : undefined,
          action_type: action.action_type,
          card_id: Number(action.card_id || 0),
          card_name: action.card_name || '',
          delivery_count: Number(action.delivery_count || 1),
          message_template: action.message_template || '',
          delay_seconds: Number(action.delay_seconds || 0),
          config_json: action.config_json || '{}',
          enabled: action.enabled !== false,
          sort_order: Number(action.sort_order || 0),
        })),
        variants: (item.actions || [])
          .filter(/* 当前回调用于处理集合元素或接口响应。 */ (action: any) => action.action_type === 'send_card')
          .map(/* 当前回调用于处理集合元素或接口响应。 */ (action: any) => {
            // cfg 动作配置，用于当前 API 处理流程。
            let cfg: any = {};
            try { cfg = JSON.parse(action.config_json || '{}'); } catch {}
            return {
              id: action.id ? String(action.id) : undefined,
              spec_name: cfg.spec_name || '',
              spec_value: cfg.spec_value || '',
              card_id: Number(action.card_id || 0),
              card_name: action.card_name || '',
              delivery_count: Number(action.delivery_count || 1),
              enabled: action.enabled !== false,
              delay_override: cfg.delay_override === true,
              delay_seconds: Number(action.delay_seconds || 0),
              config_json: action.config_json || '{}',
            };
          }),
    }));

// getShippingRules 读取发货规则列表。
export const getShippingRules = async (): Promise<ShippingRule[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制自动化规则列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/automation-rules', { signal })) as unknown;
    // rules 规则列表，用于当前 API 处理流程。
    const rules = collectionFrom<AutomationRuleResponse>(res, ['data', 'rules', 'items']);
    return normalizeShippingRules(rules);
}

export interface ShippingRuleListParams {
  /** cookieId 表示登录凭证标识。 */ cookieId?: string;
  /** triggerType 表示触发类型。 */ triggerType?: AutomationTriggerType | '';
  /** enabled 表示启用状态。 */ enabled?: boolean;
  /** search 表示搜索条件。 */ search?: string;
  /** page 表示页码。 */ page?: number;
  /** pageSize 表示每页数量。 */ pageSize?: number;
}

// getShippingRulesPage 分页读取发货规则。
export const getShippingRulesPage = async ({
  cookieId,
  triggerType,
  enabled,
  search,
  page: requestedPage = 1,
  pageSize = 10,
}: ShippingRuleListParams = {}): Promise<PaginatedResponse<ShippingRule>> => {
  // res 接口响应结果，用于当前 API 处理流程。
  const response = await runContractRequest(/* signal 控制自动化规则分页读取的取消和超时。 */ signal => contractClient.GET('/api/v1/automation-rules', { params: { query: { page: requestedPage, page_size: pageSize, cookie_id: cookieId, trigger_type: triggerType, enabled, search: search?.trim() } }, signal })) as unknown;
  // rules 规则列表，用于当前 API 处理流程。
  // page 是兼容直接分页对象和 data 包裹后的分页元数据。
  const pageMeta = objectFrom<Partial<AutomationRulePageResponse>>(response, ['data', 'result']) || {};
  // rules 是归一化后的自动化规则列表。
  const rules = normalizeShippingRules(collectionFrom<AutomationRuleResponse>(response, ['data', 'rules', 'items']));
  return {
    success: true,
    data: rules,
    total: Number(pageMeta.total ?? rules.length),
    page: Number(pageMeta.page ?? requestedPage),
    page_size: Number(pageMeta.page_size ?? pageSize),
    total_pages: Number(pageMeta.total_pages ?? (rules.length ? 1 : 0)),
    trigger_counts: Object.fromEntries(
      Object.entries(pageMeta.trigger_counts || {}).map(/* 当前回调用于处理集合元素或接口响应。 */ ([key, value]) => [key, Number(value)]),
    ),
  };
}

// orderAutomationActions 构建订单自动化动作。
const orderAutomationActions = (triggerType: string, actions: AutomationAction[]) => {
    if (triggerType !== 'order_paid') {
      return actions.map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({ ...action, sort_order: action.sort_order || index + 1 }));
    }
    // sendCards 发送Cards。
    const sendCards = actions
      .filter(/* 当前回调用于处理集合元素或接口响应。 */ action => action.action_type === 'send_card')
      .map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({ ...action, sort_order: index + 1 }));
    // others 其他动作列表，用于当前 API 处理流程。
    const others = actions.filter(/* 当前回调用于处理集合元素或接口响应。 */ action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    return [
      ...sendCards,
      ...others.map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({ ...action, sort_order: sendCards.length + index + 1 })),
      { action_type: 'confirm_shipment' as const, enabled: true, sort_order: sendCards.length + others.length + 1 },
    ];
};

// updateShippingRule 更新发货规则。
export const updateShippingRule = async (rule: Partial<ShippingRule>): Promise<OperationResponse | MutationIDResponse> => {
    // triggerType 触发类型，用于当前 API 处理流程。
    const triggerType = rule.trigger_type || 'order_paid';
    // triggerName 触发名称，用于当前 API 处理流程。
    const triggerName: Record<string, string> = {
      order_paid: '付款后自动发货',
      buyer_reviewed: '评价后发送赠品',
      review_missing_timeout: '超时未评价求评价',
    };
    // generatedName 生成d名称。
    const generatedName = [
      triggerName[triggerType] || '自动化规则',
      rule.item_title || rule.item_id || rule.cookie_id || '',
    ].filter(Boolean).join(' - ');
    // preservedNonCardActions 保留的非卡密动作，用于当前 API 处理流程。
    const preservedNonCardActions = (rule.actions || []).filter(/* 当前回调用于处理集合元素或接口响应。 */ action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    // baseActions 基础动作列表，用于当前 API 处理流程。
    const baseActions: AutomationAction[] = rule.variants && rule.variants.length > 0
      ? [...rule.variants.map(/* 当前回调用于处理集合元素或接口响应。 */ (variant, index) => ({
            action_type: 'send_card' as const,
            card_id: variant.card_id,
            delivery_count: variant.delivery_count || 1,
            enabled: variant.enabled !== false,
            sort_order: index + 1,
            delay_seconds: variant.delay_seconds || 0,
            config_json: JSON.stringify({
              spec_name: variant.spec_name || '',
              spec_value: variant.spec_value || '',
              delay_override: variant.delay_override === true,
            }),
		  })), ...preservedNonCardActions]
      : (rule.actions && rule.actions.length > 0 ? rule.actions : [{
          action_type: 'send_card' as const,
          card_id: rule.card_group_id || 0,
          delivery_count: 1,
          enabled: true,
          sort_order: 1,
        }]);
    // actions 动作列表，用于当前 API 处理流程。
    const actions = orderAutomationActions(triggerType, baseActions);
    // payload 请求载荷，用于当前 API 处理流程。
    const payload = {
        cookie_id: rule.cookie_id || '',
        item_id: rule.item_id || '',
        name: (rule.name || '').trim() || generatedName || '自动化规则',
        trigger_type: triggerType,
        enabled: rule.enabled ?? true,
        priority: rule.priority || 100,
        config_json: rule.config_json || '{}',
        actions: actions.map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({
          action_type: action.action_type,
          card_id: action.card_id || 0,
          delivery_count: action.delivery_count || 1,
          message_template: action.message_template || '',
          delay_seconds: action.delay_seconds || 0,
          config_json: action.config_json || '{}',
          enabled: action.enabled !== false,
          sort_order: action.sort_order || index + 1,
        })),
    };
    return rule.id
      ? runContractRequest(/* signal 控制自动化规则更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/automation-rules/{rule_id}', { params: { path: { rule_id: String(rule.id) } }, body: payload as never, signal }))
      : runContractRequest(/* signal 控制自动化规则创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/automation-rules', { body: payload as never, signal }));
}

// deleteShippingRule 删除发货规则。
export const deleteShippingRule = async (id: string): Promise<OperationResponse> => runContractRequest(/* signal 控制自动化规则删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/automation-rules/{rule_id}', { params: { path: { rule_id: id } }, signal }));

export interface AutomationRunIssue {
  /** id 表示标识。 */ id: number;
  /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
  /** order_id 表示订单标识。 */ order_id: string;
  /** trigger_type 表示触发条件类型。 */ trigger_type: string;
  /** error_message 保存自动化运行失败的可展示说明，不包含账号凭证。 */ error_message: string;
  /** issue_kind 表示问题类型。 */ issue_kind: 'external_result_unknown' | 'invalid_snapshot' | 'rule_unavailable' | 'partial_failure' | 'execution_failed';
  /** allowed_resolutions 表示允许的解决方式。 */ allowed_resolutions: Array<'continue' | 'retry' | 'cancel'>;
  /** action_cursor 表示动作游标。 */ action_cursor: number;
  /** sent_count 表示已发送数量。 */ sent_count: number;
  /** updated_at 表示最后更新时间。 */ updated_at: string;
}

export interface DeferredAutomationIssue {
  /** id 表示标识。 */ id: number;
  /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
  /** trigger_type 表示触发条件类型。 */ trigger_type: string;
  /** error_message 保存延迟自动化任务的失败说明，不包含账号凭证。 */ error_message: string;
  /** attempt_count 表示尝试次数。 */ attempt_count: number;
  /** updated_at 表示最后更新时间。 */ updated_at: string;
}

// getAutomationIssues 读取自动化问题列表。
export const getAutomationIssues = async (): Promise<{ /** runs 表示运行记录。 */ runs: AutomationRunIssue[]; /** pending_tasks 表示待处理任务列表。 */ pending_tasks: DeferredAutomationIssue[] }> => {
  // response 是兼容直接问题对象、data 包裹和 null 的自动化问题响应。
  const response = await runContractRequest(/* signal 控制自动化问题读取的取消和超时。 */ signal => contractClient.GET('/api/v1/automation-issues', { signal })) as unknown;
  // result 是去除历史包裹后的自动化问题对象。
  const result = objectFrom<Partial<AutomationIssuesEnvelope>>(response, ['data', 'result']) || {};
  return {
    runs: Array.isArray(result?.runs) ? result.runs : [],
    pending_tasks: Array.isArray(result?.pending_tasks) ? result.pending_tasks : [],
  };
};

// resolveAutomationRun 处理自动化运行记录。
export const resolveAutomationRun = async (id: number, resolution: 'continue' | 'retry' | 'cancel'): Promise<OperationResponse> =>
  runContractRequest(/* signal 控制自动化运行处理请求的取消和超时。 */ signal => contractClient.POST('/api/v1/automation-runs/{run_id}/resolve', { params: { path: { run_id: String(id) } }, body: { resolution } as never, signal }));

// resolveDeferredAutomationTask 处理延迟自动化任务。
export const resolveDeferredAutomationTask = async (id: number, resolution: 'retry' | 'dismiss'): Promise<OperationResponse> =>
  runContractRequest(/* signal 控制待处理自动化任务请求的取消和超时。 */ signal => contractClient.POST('/api/v1/automation-pending-tasks/{task_id}/resolve', { params: { path: { task_id: String(id) } }, body: { resolution } as never, signal }));

// Rules - 关键词回复规则 (使用关键词API)
type KeywordRowPayload = {
    /** id 表示标识。 */ id: string;
    /** keyword 表示关键词。 */ keyword: string;
    /** reply 表示回复内容。 */ reply: string;
    /** item_id 表示商品标识。 */ item_id: string;
    /** type 表示规则类型。 */ type: 'text' | 'image';
    /** image_url 表示图片地址。 */ image_url: string;
};

// normalizeKeywordRow 归一化关键词规则。
const normalizeKeywordRow = (item: any): KeywordRowPayload => ({
    id: String(item?.id || ''),
    keyword: item?.keyword || '',
    reply: item?.reply || '',
    item_id: item?.item_id || '',
    type: item?.type === 'image' ? 'image' : 'text',
    image_url: item?.image_url || '',
});

// getKeywordRowsWithType 读取带类型的关键词规则。
const getKeywordRowsWithType = async (cookieId: string): Promise<KeywordRowPayload[]> => {
    // existing 已有规则，用于当前 API 处理流程。
    const existing = await runContractRequest(/* signal 控制关键词规则读取的取消和超时。 */ signal => contractClient.GET('/api/v1/reply-rules/{cid}/typed', { params: { path: { cid: cookieId } }, signal })) as unknown;
    return collectionFrom<KeywordTypedResponse>(existing, ['data', 'items', 'rules']).map(normalizeKeywordRow);
};

// getReplyRules 读取回复规则。
export const getReplyRules = async (cookieId?: string): Promise<ReplyRule[]> => {
    if (!cookieId) return [];
    // keywords keywords，用于当前 API 处理流程。
    const keywords = await getKeywordRowsWithType(cookieId);
	return keywords.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
		id: item.id,
        keyword: item.keyword || '',
        reply_content: item.reply || '',
        match_type: 'fuzzy' as const,
        enabled: true,
        item_id: item.item_id || '',
        type: item.type === 'image' ? 'image' : 'text',
        image_url: item.image_url || ''
    }));
}

// updateReplyRule 更新回复规则。
export const updateReplyRule = async (rule: Partial<ReplyRule>, cookieId: string): Promise<OperationResponse> => {
	// type 规则类型，用于当前 API 处理流程。
	const type = rule.type || 'text';
	// payload 请求载荷，用于当前 API 处理流程。
	const payload = {
		keyword: rule.keyword || '',
		reply: type === 'text' ? (rule.reply_content || '') : '',
		item_id: rule.item_id || '',
		type,
		image_url: type === 'image' ? (rule.image_url || '') : '',
	};
	return rule.id
		? runContractRequest(/* signal 控制关键词规则更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/reply-rules/{cid}/typed/{id}', { params: { path: { cid: cookieId, id: String(rule.id) } }, body: payload as never, signal }))
		: runContractRequest(/* signal 控制关键词规则创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/reply-rules/{cid}/items', { params: { path: { cid: cookieId } }, body: payload as never, signal }));
}

// deleteReplyRule 删除回复规则。
export const deleteReplyRule = async (id: string, cookieId: string): Promise<OperationResponse> => {
	return runContractRequest(/* signal 控制关键词规则删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/reply-rules/{cid}/typed/{id}', { params: { path: { cid: cookieId, id } }, signal }));
}


// Default Reply
// getDefaultReplies 读取默认回复列表。
export const getDefaultReplies = async (): Promise<Record<string, DefaultReplyResponse>> => {
	const response = await runContractRequest(/* signal 控制默认回复列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/default-replies', { signal })) as unknown;
	// replies 是兼容直接映射、data 包裹和 null 的默认回复索引。
	return objectFrom<Record<string, DefaultReplyResponse>>(response, ['data', 'replies', 'items']) || {};
};

// getDefaultReply 读取默认回复。
export const getDefaultReply = async (cookieId: string): Promise<DefaultReply> => {
	// result 接口响应结果，用于当前 API 处理流程。
  const response = await runContractRequest(/* signal 控制默认回复读取的取消和超时。 */ signal => contractClient.GET('/api/v1/default-replies/{cid}', { params: { path: { cid: cookieId } }, signal })) as unknown;
  // result 是兼容直接默认回复和 data 包裹后的对象。
  const result = objectFrom<Partial<DefaultReplyResponse>>(response, ['data', 'result']) || {};
  return {
    cookie_id: cookieId,
    enabled: result.enabled || false,
    reply_content: result.reply_content || '',
    reply_once: result.reply_once || false,
    reply_image_url: result.reply_image_url || ''
  };
};

// updateDefaultReply 更新默认回复。
export const updateDefaultReply = async (cookieId: string, data: Partial<DefaultReply>): Promise<OperationResponse> => {
  return runContractRequest(/* signal 控制默认回复更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/default-replies/{cid}', { params: { path: { cid: cookieId }, }, body: {
    enabled: data.enabled ?? false,
    reply_content: data.reply_content || '',
    reply_once: data.reply_once ?? false,
    reply_image_url: data.reply_image_url || ''
  } as never, signal }));
};

// deleteDefaultReply 删除默认回复。
export const deleteDefaultReply = async (cookieId: string): Promise<OperationResponse> => {
	return runContractRequest(/* signal 控制默认回复删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/default-replies/{cid}', { params: { path: { cid: cookieId } }, signal }));
};

// clearDefaultReplyRecords 清理默认回复记录。
export const clearDefaultReplyRecords = async (cookieId: string): Promise<OperationResponse> => {
	return runContractRequest(/* signal 控制默认回复记录清理请求的取消和超时。 */ signal => contractClient.POST('/api/v1/default-replies/{cid}/clear-records', { params: { path: { cid: cookieId } }, body: {} as never, signal }));
};
