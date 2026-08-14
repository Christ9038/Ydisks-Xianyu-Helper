import { useCallback, useRef, useState } from 'react';
import {
  getAccountDetails,
  getAutomationIssues,
  getCards,
  getDefaultReplies,
  getItems,
  getReplyRules,
  getShippingRulesPage,
} from './api';
import type {
  AccountDetail,
  AutomationTriggerType,
  Card,
  DefaultReply,
  Item,
  ReplyRule,
  ShippingRule,
} from '../../../types';
import type { AutomationIssueState, RulesDataOptions, RulesDataResult, RulesTab } from './types';
import { isCurrentRequest, nextRequestGeneration } from './interactionState';

// useRulesData 集中管理 Rules 页的服务端数据、请求代次和刷新动作。
export const useRulesData = (options: RulesDataOptions): RulesDataResult => {
  // automationRules 保存当前分页返回的自动化规则。
  const [automationRules, setAutomationRules] = useState<ShippingRule[]>([]);
  // automationIssues 保存自动化规则列表旁的人工处理异常。
  const [automationIssues, setAutomationIssues] = useState<AutomationIssueState>({ runs: [], pending_tasks: [] });
  // replyRules 保存当前账号的关键词回复规则。
  const [replyRules, setReplyRules] = useState<ReplyRule[]>([]);
  // defaultReplies 保存所有账号的默认回复配置。
  const [defaultReplies, setDefaultReplies] = useState<Record<string, DefaultReply>>({});
  // accounts 保存规则筛选和编辑器使用的账号摘要。
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  // cards 保存规则动作可选的卡密库存。
  const [cards, setCards] = useState<Card[]>([]);
  // items 保存规则编辑器可绑定的商品。
  const [items, setItems] = useState<Item[]>([]);
  // loading 表示当前规则页是否有刷新请求正在执行。
  const [loading, setLoading] = useState(false);
  // automationRulesRequest 保存自动化列表请求的最新代次。
  const automationRulesRequest = useRef(0);
  // replyRulesRequest 保存关键词规则请求的最新代次。
  const replyRulesRequest = useRef(0);

  // automationTotal 保存服务端返回的自动化规则总数。
  const [automationTotal, setAutomationTotal] = useState(0);
  // automationTotalPages 保存服务端返回的自动化规则总页数。
  const [automationTotalPages, setAutomationTotalPages] = useState(0);
  // automationTriggerCounts 保存服务端按触发类型聚合的规则数量。
  const [automationTriggerCounts, setAutomationTriggerCounts] = useState<Record<string, number>>({});

  // loadReferenceData 并行加载规则编辑器所需的全部参考数据。
  const loadReferenceData = useCallback(
    // 参考数据加载器把共享结果写入 Hook 状态。
    async () => {
    // referenceDataPromise 用于确保互不依赖的参考请求同时发出。
    const referenceDataPromise = Promise.all([
      getAccountDetails(),
      getCards(),
      getItems(),
      getDefaultReplies(),
    ]);
    // referenceData 是并行请求完成后的四类参考数据。
    const [accountList, cardList, itemList, defaultReplyMap] = await referenceDataPromise;
    setAccounts(accountList);
    setCards(cardList);
    setItems(itemList);
    setDefaultReplies(defaultReplyMap);
    options.setSelectedAccountId(
      // 账号选择器保留用户已有选择，否则回填首个账号。
      current => current || accountList[0]?.id || '',
    );
    },
    [options.setSelectedAccountId],
  );

  // loadAutomationRules 加载自动化规则和异常，并丢弃过期列表响应。
  const loadAutomationRules = useCallback(
    // 自动化列表加载器隔离旧筛选请求的响应。
    async () => {
    // requestID 标记本次自动化规则请求，防止旧响应覆盖新筛选结果。
    const requestID = nextRequestGeneration(automationRulesRequest.current);
    automationRulesRequest.current = requestID;
    // issuesPromise 与主规则请求并行，异常接口失败不阻断规则展示。
    const issuesPromise = getAutomationIssues().catch(
      // 异常请求失败记录日志并转换为空结果，避免阻断主列表。
      error => {
      console.warn('加载自动化异常列表失败，不阻断规则展示', error);
      return null;
      },
    );
    // result 是当前筛选条件对应的分页响应。
    const result = await getShippingRulesPage({
      cookieId: options.selectedAccountId || undefined,
      triggerType: options.automationTriggerFilter,
      enabled: options.automationStatusFilter === 'all' ? undefined : options.automationStatusFilter === 'enabled',
      search: options.debouncedAutomationSearch,
      page: options.automationPage,
      pageSize: options.automationPageSize,
    });
    if (!isCurrentRequest(requestID, automationRulesRequest.current, options.selectedAccountId, options.selectedAccountId)) return;
    setAutomationRules(result.data);
    setAutomationTotal(result.total);
    setAutomationTotalPages(result.total_pages);
    setAutomationTriggerCounts(result.trigger_counts || {});
    if (result.page !== options.automationPage) {
      // 分页响应可能被后端修正到有效页码，使用函数式更新避免读取过期闭包。
      options.onAutomationPageChange?.(result.page);
    }
    // issues 是与规则列表并行加载的异常结果。
    const issues = await issuesPromise;
    if (!isCurrentRequest(requestID, automationRulesRequest.current, options.selectedAccountId, options.selectedAccountId)) return;
    if (issues) setAutomationIssues(issues);
    },
    [
    options.automationPage,
    options.automationPageSize,
    options.automationStatusFilter,
    options.automationTriggerFilter,
    options.debouncedAutomationSearch,
    options.onAutomationPageChange,
    options.selectedAccountId,
    ],
  );

  // loadReplyRules 加载关键词规则，并用请求代次隔离账号切换产生的旧响应。
  const loadReplyRules = useCallback(
    // 关键词规则加载器只提交最新账号对应的响应。
    async () => {
    // requestID 标记本次关键词规则请求。
    const requestID = nextRequestGeneration(replyRulesRequest.current);
    replyRulesRequest.current = requestID;
    // cookieID 是本次关键词规则请求绑定的账号。
    const cookieID = options.selectedAccountId;
    setReplyRules([]);
    if (!cookieID) return;
    // rules 是当前账号的关键词回复规则。
    const rules = await getReplyRules(cookieID);
    if (!isCurrentRequest(requestID, replyRulesRequest.current, cookieID, options.selectedAccountId)) return;
    setReplyRules(rules);
    },
    [options.selectedAccountId],
  );

  // loadDefaultReplies 刷新默认回复配置，供保存后和切换页签时复用。
  const loadDefaultReplies = useCallback(
    // 默认回复加载器刷新全部账号的默认配置。
    async () => {
    setDefaultReplies(await getDefaultReplies());
    },
    [],
  );

  // refresh 按当前页签只刷新需要的数据，避免无关接口请求。
  const refresh = useCallback(
    // 页面刷新动作根据页签选择唯一的数据请求。
    async () => {
    setLoading(true);
    try {
      if (options.activeTab === 'automation') {
        await loadAutomationRules();
      } else if (options.activeTab === 'reply') {
        await loadReplyRules();
      } else {
        await loadDefaultReplies();
      }
    } finally {
      setLoading(false);
    }
    },
    [loadAutomationRules, loadDefaultReplies, loadReplyRules, options.activeTab],
  );

  // result 汇总 Hook 的状态与动作，保持页面只消费 feature 边界。
  const result: RulesDataResult = {
    automationRules,
    automationIssues,
    replyRules,
    defaultReplies,
    accounts,
    cards,
    items,
    automationTotal,
    automationTotalPages,
    automationTriggerCounts,
    loading,
    setLoading,
    setAutomationRules,
    setCards,
    setItems,
    loadReferenceData,
    loadAutomationRules,
    loadReplyRules,
    loadDefaultReplies,
    refresh,
  };
  return result;
};

// RulesDataOptionsWithPageChange 为分页修正提供可选的页面状态回调。
export interface RulesDataOptionsWithPageChange extends RulesDataOptions {
  // onAutomationPageChange 接收服务端修正后的有效页码。
  onAutomationPageChange?: (page: number) => void;
}

// useRulesDataWithPageChange 是带分页回调的 Rules 数据 Hook 入口。
export const useRulesDataWithPageChange = (options: RulesDataOptionsWithPageChange): RulesDataResult =>
  useRulesData(options);

// RulesHookTab 用于在测试中表达页签切换场景，避免重复声明字符串联合类型。
export type RulesHookTab = RulesTab;

// RulesHookTrigger 用于在测试中表达自动化触发类型筛选场景。
export type RulesHookTrigger = AutomationTriggerType;
