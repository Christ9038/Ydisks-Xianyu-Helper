import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import type { AutomationTriggerType, ReplyRule, ShippingRule, ShippingVariant } from '../app/features/rules/types';
import {
  clearDefaultReplyRecords,
  deleteDefaultReply,
  deleteReplyRule,
  deleteShippingRule,
  getCards,
  getDefaultReply,
  getItems,
  getShippingRules,
  updateDefaultReply,
  updateReplyRule,
  updateShippingRule,
  resolveAutomationRun,
  resolveDeferredAutomationTask,
} from '../app/features/rules/api';
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Edit,
  Layers3,
  MessageCircle,
  Plus,
  RefreshCw,
  Save,
  Search,
  Send,
  SlidersHorizontal,
  Trash2,
  X,
  Zap,
} from 'lucide-react';
import { AutomationIssuePanel } from '../app/features/rules/components/AutomationIssuePanel';
import { useRulesData } from '../app/features/rules/hooks';
import { filterAutomationIssues } from '../app/features/rules/issueState';
import { triggerMeta, triggerOrder, emptyVariant, parseJSONObject, buildReviewConfig, defaultRuleName, shouldReplaceGeneratedName, cardActionsForTrigger, actionSummary, accentClasses, statusPill, accountLabel, boolFlag } from '../app/features/rules/utils';
import type { RulesProps, RulesTab, DefaultReplyForm } from '../app/features/rules/types';
import { finishRuleSubmission, idleRuleSubmitState, startRuleSubmission, type RuleSubmitState } from '../app/features/rules/interactionState';

// Rules 是规则 feature 在旧页面目录下保留的兼容入口组件。
const Rules: React.FC<RulesProps> = ({ initialDeliveryTarget, onDeliveryTargetHandled }) => {
  // [activeTab, 解构得到当前 Hook 返回的状态和操作函数。
  const [activeTab, setActiveTab] = useState<RulesTab>('automation');
  // [selectedAccountId, 解构得到当前 Hook 返回的状态和操作函数。
  const [selectedAccountId, setSelectedAccountId] = useState('');
  // [automationSearch, 解构得到当前 Hook 返回的状态和操作函数。
  const [automationSearch, setAutomationSearch] = useState('');
  // [debouncedAutomationSearch, 解构得到当前 Hook 返回的状态和操作函数。
  const [debouncedAutomationSearch, setDebouncedAutomationSearch] = useState('');
  // [automationTriggerFilter, 解构得到当前 Hook 返回的状态和操作函数。
  const [automationTriggerFilter, setAutomationTriggerFilter] = useState<AutomationTriggerType | ''>('');
  // [automationStatusFilter, 解构得到当前 Hook 返回的状态和操作函数。
  const [automationStatusFilter, setAutomationStatusFilter] = useState<'all' | 'enabled' | 'disabled'>('all');
  // [automationPage, 解构得到当前 Hook 返回的状态和操作函数。
  const [automationPage, setAutomationPage] = useState(1);
  // [automationPageSize, 解构得到当前 Hook 返回的状态和操作函数。
  const [automationPageSize, setAutomationPageSize] = useState(10);

  // rulesData 规则列表数据，负责当前功能中的对应处理。
  const rulesData = useRulesData({
    activeTab,
    selectedAccountId,
    automationTriggerFilter,
    automationStatusFilter,
    debouncedAutomationSearch,
    automationPage,
    automationPageSize,
    setSelectedAccountId,
    onAutomationPageChange: setAutomationPage,
  });
  // 解构数据 解构得到当前 Hook 返回的状态和操作函数。
  const {
    automationRules,
    automationIssues,
    replyRules,
    defaultReplies,
    accounts,
    cards,
    items,
    loading,
    setLoading,
    automationTotal,
    automationTotalPages,
    automationTriggerCounts,
    setAutomationRules,
    setCards,
    setItems,
    loadReferenceData,
    loadAutomationRules,
    loadReplyRules,
    loadDefaultReplies,
    refresh,
  } = rulesData;

  // [showAutomationModal, 解构得到当前 Hook 返回的状态和操作函数。
  const [showAutomationModal, setShowAutomationModal] = useState(false);
  // [showReplyModal, 解构得到当前 Hook 返回的状态和操作函数。
  const [showReplyModal, setShowReplyModal] = useState(false);
  // [showDefaultModal, 解构得到当前 Hook 返回的状态和操作函数。
  const [showDefaultModal, setShowDefaultModal] = useState(false);
  // automationSubmitState 防止自动化规则保存按钮在请求期间重复提交。
  const [automationSubmitState, setAutomationSubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // replySubmitState 防止关键词回复保存按钮在请求期间重复提交。
  const [replySubmitState, setReplySubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // defaultReplySubmitState 防止默认回复保存按钮在请求期间重复提交。
  const [defaultReplySubmitState, setDefaultReplySubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // [editingAutomationRule, 解构得到当前 Hook 返回的状态和操作函数。
  const [editingAutomationRule, setEditingAutomationRule] = useState<Partial<ShippingRule> | null>(null);
  // [editingReplyRule, 解构得到当前 Hook 返回的状态和操作函数。
  const [editingReplyRule, setEditingReplyRule] = useState<Partial<ReplyRule> | null>(null);
  // [defaultForm, 解构得到当前 Hook 返回的状态和操作函数。
  const [defaultForm, setDefaultForm] = useState<DefaultReplyForm>({
    cookie_id: '',
    enabled: false,
    reply_content: '',
    reply_once: false,
    reply_image_url: '',
  });
  // selectedAccount 处理当前选择（ed账号）。
  const selectedAccount = accounts.find(/* 当前回调处理集合中的单个元素。 */ account => account.id === selectedAccountId);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
	// timer 定时器。
	const timer = window.setTimeout(/* 当前回调处理用户交互或异步状态变化。 */ () => {
	  setAutomationPage(1);
	  setDebouncedAutomationSearch(automationSearch.trim());
	}, 300);
	return /* 当前回调处理用户交互或异步状态变化。 */ () => window.clearTimeout(timer);
  }, [automationSearch]);

  useEffect(/* 当前回调处理异步操作结果。 */ () => {
	void loadReferenceData().catch(/* 当前回调处理异步操作结果。 */ error => console.error('加载规则参考数据失败', error));
  }, [loadReferenceData]);

  useEffect(/* 当前回调处理异步操作结果。 */ () => {
	void refresh().catch(/* 当前回调处理异步操作结果。 */ error => console.error('刷新规则页面失败', error));
  }, [refresh]);

  // visibleAutomationRules 可见数据自动化规则列表，负责当前功能中的对应处理。
  const visibleAutomationRules = useMemo(
    /* 当前回调处理集合中的单个元素。 */ () => automationRules.filter(/* 当前回调处理集合中的单个元素。 */ rule => !selectedAccountId || rule.cookie_id === selectedAccountId),
    [automationRules, selectedAccountId],
  );

  // visibleAutomationIssues 可见数据自动化Issues，负责当前功能中的对应处理。
  const visibleAutomationIssues = useMemo(
	/* 当前回调计算并缓存派生数据。 */ () => filterAutomationIssues(automationIssues, selectedAccountId),
	[automationIssues, selectedAccountId],
  );

  // visibleDefaultAccounts 可见数据默认账号列表，负责当前功能中的对应处理。
  const visibleDefaultAccounts = useMemo(
    /* 当前回调处理集合中的单个元素。 */ () => accounts.filter(/* 当前回调处理集合中的单个元素。 */ account => !selectedAccountId || account.id === selectedAccountId),
    [accounts, selectedAccountId],
  );

  // automationPageNumbers 自动化页码Numbers，负责当前功能中的对应处理。
  const automationPageNumbers = useMemo(/* 当前回调计算并缓存派生数据。 */ () => {
	if (automationTotalPages <= 1) return [];
	// first 首项。
	const first = Math.max(1, Math.min(automationPage - 2, automationTotalPages - 4));
	// last last，负责当前功能中的对应处理。
	const last = Math.min(automationTotalPages, first + 4);
	return Array.from({ length: last - first + 1 }, /* 当前回调处理用户交互或异步状态变化。 */ (_, index) => first + index);
  }, [automationPage, automationTotalPages]);

  // hasAutomationListFilters has自动化列表Filters，负责当前功能中的对应处理。
  const hasAutomationListFilters = Boolean(
	automationSearch.trim() || automationTriggerFilter || automationStatusFilter !== 'all',
  );

  // clearAutomationListFilters 清理自动化列表Filters，负责当前功能中的对应处理。
  const clearAutomationListFilters = () => {
	setAutomationSearch('');
	setDebouncedAutomationSearch('');
	setAutomationTriggerFilter('');
	setAutomationStatusFilter('all');
	setAutomationPage(1);
  };

  // modalAccountItems modal账号商品列表，负责当前功能中的对应处理。
  const modalAccountItems = useMemo(/* 当前回调计算并缓存派生数据。 */ () => {
    // cookieID 账号凭证标识。
    const cookieID = editingAutomationRule?.cookie_id || selectedAccountId;
    return items.filter(/* 当前回调处理集合中的单个元素。 */ item => item.cookie_id === cookieID);
  }, [editingAutomationRule?.cookie_id, items, selectedAccountId]);

  // selectedRuleItem 处理当前选择（ed规则商品）。
  const selectedRuleItem = useMemo(/* 当前回调计算并缓存派生数据。 */ () => {
    if (!editingAutomationRule?.cookie_id || !editingAutomationRule?.item_id) return undefined;
    return items.find(/* 当前回调处理集合中的单个元素。 */ item => item.cookie_id === editingAutomationRule.cookie_id && item.item_id === editingAutomationRule.item_id);
  }, [editingAutomationRule?.cookie_id, editingAutomationRule?.item_id, items]);

  // isMultiSpecRule isMultiSpec规则，负责当前功能中的对应处理。
  const isMultiSpecRule = boolFlag(selectedRuleItem?.is_multi_spec);
  // currentTrigger 当前触发条件。
  const currentTrigger = (editingAutomationRule?.trigger_type || 'order_paid') as AutomationTriggerType;
  // currentMeta 当前触发元数据。
  const currentMeta = triggerMeta[currentTrigger];
  // reviewConfig 评价配置。
  const reviewConfig = parseJSONObject(editingAutomationRule?.config_json);

  // buildAutomationDraft 构建自动化Draft，负责当前功能中的对应处理。
  const buildAutomationDraft = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ (
    trigger: AutomationTriggerType = 'order_paid',
    cookieID = selectedAccountId,
    itemID = '',
  ): Partial<ShippingRule> => {
    // item 商品。
    const item = items.find(/* 当前回调处理集合中的单个元素。 */ candidate => candidate.cookie_id === cookieID && candidate.item_id === itemID);
    // itemLabel 商品标签，负责当前功能中的对应处理。
    const itemLabel = item?.item_title || itemID;
    return {
      name: defaultRuleName(trigger, itemLabel),
      trigger_type: trigger,
      cookie_id: cookieID,
      item_id: itemID,
      item_title: item?.item_title || '',
      item_keyword: itemLabel,
      card_group_id: 0,
      priority: 100,
      enabled: true,
      config_json: trigger === 'review_missing_timeout' ? buildReviewConfig() : '{}',
      actions: cardActionsForTrigger(trigger),
      variants: trigger === 'review_missing_timeout' ? [] : [emptyVariant()],
    };
  }, [items, selectedAccountId]);

  // openAutomationRule 打开当前界面（自动化规则）。
  const openAutomationRule = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ (rule: ShippingRule) => {
    // trigger 触发条件。
    const trigger = (rule.trigger_type || 'order_paid') as AutomationTriggerType;
    setEditingAutomationRule({
      ...rule,
      trigger_type: trigger,
      config_json: trigger === 'review_missing_timeout' ? buildReviewConfig(rule.config_json) : (rule.config_json || '{}'),
      actions: rule.actions?.length ? rule.actions.map(/* 当前回调处理集合中的单个元素。 */ action => ({ ...action })) : cardActionsForTrigger(trigger, rule.card_group_id),
      variants: rule.variants?.length ? rule.variants.map(/* 当前回调处理集合中的单个元素。 */ variant => ({ ...variant })) : (trigger === 'review_missing_timeout' ? [] : [emptyVariant()]),
    });
    setShowAutomationModal(true);
  }, []);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    if (!initialDeliveryTarget) return;
    // cancelled 取消当前操作（led）。
    let cancelled = false;

    // openLinkedRule 打开当前界面（Linked规则）。
    const openLinkedRule = async () => {
      setActiveTab('automation');
      setSelectedAccountId(initialDeliveryTarget.cookieId);
      setLoading(true);
      try {
        // [ruleList, 解构得到当前 Hook 返回的状态和操作函数。
        const [ruleList, itemList, cardList] = await Promise.all([
          getShippingRules(),
          getItems(),
          getCards().catch(/* 当前回调处理异步操作结果。 */ error => {
            console.warn('加载卡密库存失败，不阻断打开自动化规则', error);
            return [];
          }),
        ]);
        if (cancelled) return;
        setAutomationRules(ruleList);
        setItems(itemList);
        setCards(cardList);
        // rule 规则。
        const rule = ruleList.find(/* 当前回调处理集合中的单个元素。 */ candidate =>
          candidate.cookie_id === initialDeliveryTarget.cookieId &&
          candidate.item_id === initialDeliveryTarget.itemId &&
          candidate.trigger_type === 'order_paid'
        );
        if (rule) {
          openAutomationRule(rule);
        } else {
          // item 商品。
          const item = itemList.find(/* 当前回调处理集合中的单个元素。 */ candidate =>
            candidate.cookie_id === initialDeliveryTarget.cookieId &&
            candidate.item_id === initialDeliveryTarget.itemId
          );
          setEditingAutomationRule({
            ...buildAutomationDraft('order_paid', initialDeliveryTarget.cookieId, initialDeliveryTarget.itemId),
            item_title: item?.item_title || '',
            item_keyword: item?.item_title || initialDeliveryTarget.itemId,
            name: defaultRuleName('order_paid', item?.item_title || initialDeliveryTarget.itemId),
          });
          setShowAutomationModal(true);
        }
      } catch (/* error 表示错误。 */ error) {
        console.error('打开商品自动化规则失败', error);
        alert('无法加载该商品的自动化规则');
      } finally {
        if (!cancelled) {
          setLoading(false);
          onDeliveryTargetHandled?.();
        }
      }
    };

    void openLinkedRule();
    return /* 当前回调处理用户交互或异步状态变化。 */ () => { cancelled = true; };
  }, [buildAutomationDraft, initialDeliveryTarget, onDeliveryTargetHandled, openAutomationRule]);

  // openNewAutomationRule 打开当前界面（New自动化规则）。
  const openNewAutomationRule = (trigger: AutomationTriggerType = 'order_paid') => {
    if (!selectedAccountId) {
      alert('请先选择账号');
      return;
    }
    setEditingAutomationRule(buildAutomationDraft(trigger));
    setShowAutomationModal(true);
  };

  // handleTriggerChange 处理当前用户操作（触发条件Change）。
  const handleTriggerChange = (trigger: AutomationTriggerType) => {
    if (!editingAutomationRule) return;
    // currentCardID 当前卡密组标识。
    const currentCardID =
      editingAutomationRule.variants?.find(/* 当前回调处理集合中的单个元素。 */ variant => variant.card_id)?.card_id ||
      editingAutomationRule.actions?.find(/* 当前回调处理集合中的单个元素。 */ action => action.action_type === 'send_card')?.card_id ||
      editingAutomationRule.card_group_id ||
      0;
    // itemLabel 商品标签，负责当前功能中的对应处理。
    const itemLabel = selectedRuleItem?.item_title || editingAutomationRule.item_title || editingAutomationRule.item_id || '';
    setEditingAutomationRule({
      ...editingAutomationRule,
      trigger_type: trigger,
      name: shouldReplaceGeneratedName(editingAutomationRule.name)
        ? defaultRuleName(trigger, itemLabel)
        : editingAutomationRule.name,
      card_group_id: currentCardID,
      config_json: trigger === 'review_missing_timeout' ? buildReviewConfig(editingAutomationRule.config_json) : '{}',
      actions: cardActionsForTrigger(trigger, currentCardID),
      variants: trigger === 'review_missing_timeout'
        ? []
        : (editingAutomationRule.variants?.length ? editingAutomationRule.variants : [{ ...emptyVariant(), card_id: currentCardID }]),
    });
  };

  // handleAutomationItemChange 处理当前用户操作（自动化商品Change）。
  const handleAutomationItemChange = (itemID: string) => {
    if (!editingAutomationRule) return;
    // item 商品。
    const item = items.find(/* 当前回调处理集合中的单个元素。 */ candidate =>
      candidate.cookie_id === (editingAutomationRule.cookie_id || selectedAccountId) &&
      candidate.item_id === itemID
    );
    // itemLabel 商品标签，负责当前功能中的对应处理。
    const itemLabel = item?.item_title || itemID;
    setEditingAutomationRule({
      ...editingAutomationRule,
      item_id: itemID,
      item_title: item?.item_title || '',
      item_keyword: itemLabel,
      name: shouldReplaceGeneratedName(editingAutomationRule.name)
        ? defaultRuleName(currentTrigger, itemLabel)
        : editingAutomationRule.name,
    });
  };

  // displayVariants 展示规格列表。
  const displayVariants = editingAutomationRule?.variants?.length
    ? editingAutomationRule.variants
    : [emptyVariant()];

  // updateVariant 更新当前数据（Variant）。
  const updateVariant = (index: number, patch: Partial<ShippingVariant>) => {
    if (!editingAutomationRule) return;
    // next 下一项。
    const next = displayVariants.map(/* 当前回调处理集合中的单个元素。 */ (variant, variantIndex) =>
      variantIndex === index ? { ...variant, ...patch } : variant
    );
    setEditingAutomationRule({
      ...editingAutomationRule,
      variants: next,
      card_group_id: next[0]?.card_id || 0,
    });
  };

  // appendDeliveryContent 追加发货内容。
  const appendDeliveryContent = () => {
    if (!editingAutomationRule) return;
    // previous 上一项。
    const previous = displayVariants[displayVariants.length - 1];
    setEditingAutomationRule({
      ...editingAutomationRule,
      variants: [
        ...displayVariants,
        {
          ...emptyVariant(),
          spec_name: isMultiSpecRule ? previous?.spec_name || '' : '',
          spec_value: isMultiSpecRule ? previous?.spec_value || '' : '',
        },
      ],
    });
  };

  // handleSaveAutomationRule 处理当前用户操作（Save自动化规则）。
  const handleSaveAutomationRule = async () => {
    if (!editingAutomationRule || automationSubmitState.submitting) return;
    // trigger 触发条件。
    const trigger = (editingAutomationRule.trigger_type || 'order_paid') as AutomationTriggerType;
    if (!editingAutomationRule.cookie_id) {
      alert('请选择账号');
      return;
    }

    // variants 规格列表。
    const variants = editingAutomationRule.variants?.length ? editingAutomationRule.variants : [];
    if (trigger !== 'review_missing_timeout') {
      if (!variants.length || variants.some(/* 当前回调处理集合中的单个元素。 */ variant => !variant.card_id)) {
        alert(trigger === 'buyer_reviewed' ? '请选择评价赠品卡密库存' : '请选择发货卡密库存');
        return;
      }
      if (isMultiSpecRule && variants.some(/* 当前回调处理集合中的单个元素。 */ variant => !variant.spec_name.trim() || !variant.spec_value.trim())) {
        alert('多规格商品必须填写每一行的规格名称和规格值');
        return;
      }
    }

    if (trigger === 'review_missing_timeout') {
      // text 文本。
      const text = editingAutomationRule.actions?.find(/* 当前回调处理集合中的单个元素。 */ action => action.action_type === 'send_text')?.message_template || '';
      if (!text.trim()) {
        alert('请填写求评价文案');
        return;
      }
    }

    // saveVariants 保存当前数据（Variants）。
    const saveVariants = trigger === 'review_missing_timeout'
      ? []
      : variants.map(variant => ({/* 当前回调处理集合中的单个元素。 */
        ...variant,
        spec_name: isMultiSpecRule ? variant.spec_name.trim() : '',
        spec_value: isMultiSpecRule ? variant.spec_value.trim() : '',
        delivery_count: Math.max(1, Number(variant.delivery_count) || 1),
        enabled: variant.enabled !== false,
      }));

    setAutomationSubmitState(startRuleSubmission(automationSubmitState));
    // succeeded 是否成功。
    let succeeded = false;
    try {
      await updateShippingRule({
        ...editingAutomationRule,
        trigger_type: trigger,
        name: (editingAutomationRule.name || '').trim() ||
          defaultRuleName(trigger, selectedRuleItem?.item_title || editingAutomationRule.item_id || ''),
        config_json: trigger === 'review_missing_timeout'
          ? buildReviewConfig(editingAutomationRule.config_json)
          : (editingAutomationRule.config_json || '{}'),
        actions: editingAutomationRule.actions?.length
          ? editingAutomationRule.actions
          : cardActionsForTrigger(trigger, saveVariants[0]?.card_id || editingAutomationRule.card_group_id || 0),
        variants: saveVariants,
      });
      setShowAutomationModal(false);
      await Promise.all([loadAutomationRules(), loadReferenceData()]);
      alert('保存成功');
      succeeded = true;
    } catch (/* error 表示错误。 */ error) {
      console.error('保存自动化规则失败:', error);
      alert('保存失败：' + (error as Error).message);
    } finally {
      setAutomationSubmitState(/* 当前回调处理用户交互或异步状态变化。 */ current => finishRuleSubmission(current, succeeded));
    }
  };

  // handleDeleteAutomation 处理当前用户操作（Delete自动化）。
  const handleDeleteAutomation = async (id: string) => {
    if (!confirm('确定删除该自动化规则吗？')) return;
    try {
      await deleteShippingRule(id);
      await loadAutomationRules();
      alert('删除成功');
    } catch (/* error 表示错误。 */ error) {
      alert('删除失败：' + (error as Error).message);
    }
  };

  // handleToggleAutomation 处理当前用户操作（Toggle自动化）。
  const handleToggleAutomation = async (rule: ShippingRule) => {
    try {
      await updateShippingRule({ ...rule, enabled: !rule.enabled });
      await loadAutomationRules();
    } catch (/* error 表示错误。 */ error) {
      alert('操作失败：' + (error as Error).message);
    }
  };

  // handleResolveRunIssue 处理当前用户操作（ResolveRun问题）。
  const handleResolveRunIssue = async (id: number, resolution: 'continue' | 'retry' | 'cancel') => {
    // prompt 提示文案。
    const prompt = resolution === 'continue'
      ? '确认外部动作已经执行成功，并跳到下一步吗？'
      : resolution === 'retry'
        ? '确认外部动作没有执行，可以安全重试吗？错误判断可能造成重复发送。'
        : '确认终止该自动化运行吗？';
    if (!confirm(prompt)) return;
    try {
      await resolveAutomationRun(id, resolution);
      await loadAutomationRules();
    } catch (/* error 表示错误。 */ error) {
      alert('处理失败：' + (error as Error).message);
    }
  };

  // handleResolveDeferredIssue 处理当前用户操作（ResolveDeferred问题）。
  const handleResolveDeferredIssue = async (id: number, resolution: 'retry' | 'dismiss') => {
    if (!confirm(resolution === 'retry' ? '确认重新执行该任务吗？' : '确认忽略并删除该异常任务吗？')) return;
    try {
      await resolveDeferredAutomationTask(id, resolution);
      await loadAutomationRules();
    } catch (/* error 表示错误。 */ error) {
      alert('处理失败：' + (error as Error).message);
    }
  };

  // handleAddReplyRule 处理当前用户操作（Add回复规则）。
  const handleAddReplyRule = () => {
    if (!selectedAccountId) {
      alert('请先选择账号');
      return;
    }
    setEditingReplyRule({
      keyword: '',
      reply_content: '',
      image_url: '',
      item_id: '',
      type: 'text',
      match_type: 'fuzzy',
      enabled: true,
    });
    setShowReplyModal(true);
  };

  // handleSaveReplyRule 处理当前用户操作（Save回复规则）。
  const handleSaveReplyRule = async () => {
    if (!editingReplyRule || !selectedAccountId || replySubmitState.submitting) return;
    // hasReplyContent 是否包含回复内容。
    const hasReplyContent = editingReplyRule.type === 'image'
      ? Boolean(editingReplyRule.image_url?.trim())
      : Boolean(editingReplyRule.reply_content?.trim());
    if (!editingReplyRule.keyword?.trim() || !hasReplyContent) {
      alert('请填写关键词和回复内容');
      return;
    }
    setReplySubmitState(startRuleSubmission(replySubmitState));
    // succeeded 是否成功。
    let succeeded = false;
    try {
      await updateReplyRule({ ...editingReplyRule, match_type: 'fuzzy', enabled: true }, selectedAccountId);
      setShowReplyModal(false);
      await loadReplyRules();
      alert('保存成功');
      succeeded = true;
    } catch (/* error 表示错误。 */ error) {
      alert('保存失败：' + (error as Error).message);
    } finally {
      setReplySubmitState(/* 当前回调处理用户交互或异步状态变化。 */ current => finishRuleSubmission(current, succeeded));
    }
  };

  // handleDeleteReply 处理当前用户操作（Delete回复）。
  const handleDeleteReply = async (id: string) => {
    if (!selectedAccountId || !confirm('确定删除该回复规则吗？')) return;
    try {
      await deleteReplyRule(id, selectedAccountId);
      await loadReplyRules();
      alert('删除成功');
    } catch (/* error 表示错误。 */ error) {
      alert('删除失败：' + (error as Error).message);
    }
  };

  // openDefaultReplyModal 打开当前界面（Default回复Modal）。
  const openDefaultReplyModal = async (cookieID = selectedAccountId) => {
    if (!cookieID) {
      alert('请先选择账号');
      return;
    }
    try {
      // data 数据。
      const data = await getDefaultReply(cookieID);
      setDefaultForm({
        cookie_id: cookieID,
        enabled: data.enabled,
        reply_content: data.reply_content,
        reply_once: data.reply_once,
        reply_image_url: data.reply_image_url || '',
      });
    } catch {
      setDefaultForm({
        cookie_id: cookieID,
        enabled: false,
        reply_content: '',
        reply_once: false,
        reply_image_url: '',
      });
    }
    setShowDefaultModal(true);
  };

  // handleSaveDefaultReply 处理当前用户操作（SaveDefault回复）。
  const handleSaveDefaultReply = async () => {
    if (defaultReplySubmitState.submitting) return;
    if (!defaultForm.cookie_id) {
      alert('请先选择账号');
      return;
    }
    if (defaultForm.enabled && !defaultForm.reply_content.trim() && !defaultForm.reply_image_url.trim()) {
      alert('启用默认回复时，请填写回复内容或图片 URL');
      return;
    }
    setDefaultReplySubmitState(startRuleSubmission(defaultReplySubmitState));
    // succeeded 是否成功。
    let succeeded = false;
    try {
      await updateDefaultReply(defaultForm.cookie_id, {
        enabled: defaultForm.enabled,
        reply_content: defaultForm.reply_content,
        reply_once: defaultForm.reply_once,
        reply_image_url: defaultForm.reply_image_url,
      });
      setShowDefaultModal(false);
      await loadDefaultReplies();
      alert('保存成功');
      succeeded = true;
    } catch (/* error 表示错误。 */ error) {
      alert('保存失败：' + (error as Error).message);
    } finally {
      setDefaultReplySubmitState(/* 当前回调处理用户交互或异步状态变化。 */ current => finishRuleSubmission(current, succeeded));
    }
  };

  // handleDeleteDefaultReply 处理当前用户操作（DeleteDefault回复）。
  const handleDeleteDefaultReply = async (cookieID: string) => {
    if (!confirm('确定删除该账号默认回复吗？')) return;
    try {
      await deleteDefaultReply(cookieID);
      await loadDefaultReplies();
      alert('删除成功');
    } catch (/* error 表示错误。 */ error) {
      alert('删除失败：' + (error as Error).message);
    }
  };

  // handleClearDefaultReplyRecords 处理当前用户操作（ClearDefault回复Records）。
  const handleClearDefaultReplyRecords = async (cookieID: string) => {
    if (!confirm('确定清空该账号的默认回复记录吗？清空后可重新对所有会话使用“只回复一次”。')) return;
    try {
      await clearDefaultReplyRecords(cookieID);
      alert('清空成功');
    } catch (/* error 表示错误。 */ error) {
      alert('清空失败：' + (error as Error).message);
    }
  };

  // primaryActionLabel 主操作按钮文案。
  const primaryActionLabel = activeTab === 'automation'
    ? '新建自动化'
    : activeTab === 'reply'
      ? '新增关键词'
      : '编辑默认回复';

  return (
    <div className="min-w-0 space-y-8 animate-fade-in">
      <div className="flex flex-col xl:flex-row justify-between xl:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">自动化规则</h2>
          <p className="text-gray-500 mt-2 font-medium">系统通知卡片只进入自动化判断；买家消息进入关键词、默认或 AI 回复。</p>
        </div>
        <div className="flex flex-col sm:flex-row sm:flex-wrap gap-3">
          <select
            value={selectedAccountId}
            onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => {
			  setSelectedAccountId(event.target.value);
			  setAutomationPage(1);
			}}
            className="ios-input w-full px-4 py-3 rounded-2xl text-sm sm:w-64"
          >
            <option value="">全部账号</option>
            {accounts.map(/* 当前回调处理集合中的单个元素。 */ account => (
              <option key={account.id} value={account.id}>{accountLabel(account)}</option>
            ))}
          </select>
          <button
            onClick={refresh}
            className="px-4 py-3 rounded-2xl font-bold bg-gray-100 hover:bg-gray-200 text-gray-700 flex items-center justify-center gap-2 whitespace-nowrap transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </button>
          <button
            onClick={activeTab === 'automation' ? /* 当前回调处理用户交互或异步状态变化。 */ () => openNewAutomationRule('order_paid') : activeTab === 'reply' ? handleAddReplyRule : /* 当前回调处理用户交互或异步状态变化。 */ () => void openDefaultReplyModal()}
            disabled={!selectedAccountId}
            className="ios-btn-primary px-5 py-3 rounded-2xl text-sm font-extrabold flex items-center justify-center gap-2 whitespace-nowrap disabled:opacity-50"
          >
            <Plus className="w-4 h-4" />
            {primaryActionLabel}
          </button>
        </div>
      </div>

      <div className="flex flex-wrap gap-2 p-2 bg-gray-100/50 rounded-2xl">
        {[
          { id: 'automation' as const, label: '交易自动化', icon: Zap },
          { id: 'reply' as const, label: '关键词回复', icon: MessageCircle },
          { id: 'default' as const, label: '账号默认回复', icon: Bot },
        ].map(/* 当前回调处理用户交互或异步状态变化。 */ tab => {
          // Icon 渲染Icon React 组件。
          const Icon = tab.icon;
          return (
            <button
              key={tab.id}
              onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setActiveTab(tab.id)}
              className={`inline-flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-bold transition-all ${
                activeTab === tab.id
                  ? 'bg-brand text-white shadow-md'
                  : 'bg-white text-gray-600 hover:text-black hover:bg-gray-50'
              }`}
            >
              <Icon className="w-4 h-4" />
              {tab.label}
            </button>
          );
        })}
      </div>

	  {activeTab === 'automation' && (visibleAutomationIssues.runs.length > 0 || visibleAutomationIssues.pending_tasks.length > 0) && (
	    <AutomationIssuePanel
	      runs={visibleAutomationIssues.runs}
	      pendingTasks={visibleAutomationIssues.pending_tasks}
	      onResolveRun={/* 当前回调处理用户交互或异步状态变化。 */ (id, resolution) => void handleResolveRunIssue(id, resolution)}
	      onResolveDeferredTask={/* 当前回调处理用户交互或异步状态变化。 */ (id, resolution) => void handleResolveDeferredIssue(id, resolution)}
	    />
	  )}

      {activeTab === 'automation' && (
        <div className="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(270px,0.72fr)_minmax(0,1.28fr)]">
          <aside className="min-w-0 space-y-4">
            <div className="bg-white rounded-xl p-5 border border-gray-100 shadow-sm">
              <h3 className="font-black text-gray-900 mb-1">新建规则</h3>
              <p className="text-sm text-gray-500 mb-4">先选自动化类型，再配置对应动作。</p>
              <div className="space-y-3">
                {triggerOrder.map(/* 当前回调处理集合中的单个元素。 */ trigger => {
                  // meta 元数据。
                  const meta = triggerMeta[trigger];
                  // Icon 渲染Icon React 组件。
                  const Icon = meta.icon;
                  return (
                    <button
                      key={trigger}
                      type="button"
                      onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => openNewAutomationRule(trigger)}
                      className={`w-full text-left rounded-2xl border p-4 transition-colors ${accentClasses(meta.accent)}`}
                    >
                      <div className="flex items-start gap-3">
                        <div className="w-10 h-10 rounded-xl bg-white/80 flex items-center justify-center shrink-0">
                          <Icon className="w-5 h-5" />
                        </div>
                        <div>
                          <div className="font-extrabold">{meta.label}</div>
                          <div className="text-xs opacity-75 mt-1 leading-5">{meta.description}</div>
                        </div>
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="bg-white rounded-xl p-5 border border-gray-100 shadow-sm">
              <div className="mb-4 flex items-center justify-between gap-3">
				<h3 className="font-black text-gray-900">筛选结果构成</h3>
				<span className="text-xs font-bold text-gray-400">共 {automationTotal} 条</span>
			  </div>
              <div className="space-y-3">
                {triggerOrder.map(/* 当前回调处理集合中的单个元素。 */ trigger => {
                  // meta 元数据。
                  const meta = triggerMeta[trigger];
                  // Icon 渲染Icon React 组件。
                  const Icon = meta.icon;
                  return (
                    <div key={trigger} className="flex items-center justify-between rounded-2xl bg-gray-50 p-3">
                      <div className="flex items-center gap-3">
                        <Icon className="w-4 h-4 text-gray-500" />
                        <span className="text-sm font-bold text-gray-700">{meta.shortLabel}</span>
                      </div>
                      <span className="text-sm font-black text-gray-900">{automationTriggerCounts[trigger] || 0}</span>
                    </div>
                  );
                })}
              </div>
            </div>
          </aside>

		  <section className="min-w-0 space-y-4">
			<div className="rounded-xl border border-gray-100 bg-surface-muted p-4 shadow-sm">
			  <div className="flex flex-col gap-3 xl:flex-row xl:items-center">
				<div className="relative min-w-0 flex-1">
				  <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
				  <input
					type="search"
					aria-label="搜索自动化规则"
					placeholder="搜索规则名、商品名或商品 ID..."
					value={automationSearch}
					onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => {
					  setAutomationSearch(event.target.value);
					  setAutomationPage(1);
					}}
					className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-10 pr-4 text-sm shadow-sm"
				  />
				</div>
				<div className="relative xl:w-52">
				  <SlidersHorizontal className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
				  <select
					aria-label="按自动化类型筛选"
					value={automationTriggerFilter}
					onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => {
					  setAutomationTriggerFilter(event.target.value as AutomationTriggerType | '');
					  setAutomationPage(1);
					}}
					className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-10 pr-9 text-sm shadow-sm"
				  >
					<option value="">全部自动化类型</option>
					{triggerOrder.map(/* 当前回调处理集合中的单个元素。 */ trigger => <option key={trigger} value={trigger}>{triggerMeta[trigger].shortLabel}</option>)}
				  </select>
				</div>
				<select
				  aria-label="按启用状态筛选"
				  value={automationStatusFilter}
				  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => {
					setAutomationStatusFilter(event.target.value as 'all' | 'enabled' | 'disabled');
					setAutomationPage(1);
				  }}
				  className="ios-input rounded-xl border-none bg-white px-4 py-2.5 text-sm shadow-sm xl:w-36"
				>
				  <option value="all">全部状态</option>
				  <option value="enabled">已启用</option>
				  <option value="disabled">已禁用</option>
				</select>
				{hasAutomationListFilters && (
				  <button
					type="button"
					onClick={clearAutomationListFilters}
					className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-white text-gray-500 shadow-sm transition-colors hover:bg-gray-100 hover:text-gray-900"
					title="清除筛选"
					aria-label="清除筛选"
				  >
					<X className="h-4 w-4" />
				  </button>
				)}
			  </div>
			  <div className="mt-3 flex items-center justify-between text-xs font-bold text-gray-400">
				<span>找到 {automationTotal} 条规则</span>
				{loading && <span className="inline-flex items-center gap-1.5"><RefreshCw className="h-3.5 w-3.5 animate-spin" />正在更新</span>}
			  </div>
			</div>

			{loading && visibleAutomationRules.length === 0 ? (
			  <div className="flex min-h-56 items-center justify-center rounded-xl border border-gray-100 bg-white text-sm font-bold text-gray-400">
				<RefreshCw className="mr-2 h-4 w-4 animate-spin" />
				正在加载规则
			  </div>
			) : visibleAutomationRules.length === 0 ? (
			  <div className="bg-white rounded-xl border border-dashed border-gray-200 p-16 text-center">
				<Zap className="w-12 h-12 text-gray-300 mx-auto mb-4" />
				<h3 className="text-xl font-black text-gray-900">{hasAutomationListFilters ? '没有匹配的自动化规则' : '还没有自动化规则'}</h3>
				<p className="text-gray-500 mt-2">{hasAutomationListFilters ? '调整或清除筛选条件后再试。' : '从左侧选择一个模板开始配置。'}</p>
			  </div>
			) : (
			  visibleAutomationRules.map(/* 当前回调处理集合中的单个元素。 */ rule => {
                // meta 元数据。
                const meta = triggerMeta[rule.trigger_type];
                // Icon 渲染Icon React 组件。
                const Icon = meta.icon;
                return (
                  <article key={rule.id} className="bg-white rounded-xl border border-gray-100 p-5 shadow-sm hover:shadow-lg transition-all">
                    <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
                      <div className="flex items-start gap-4 min-w-0">
                        <div className={`w-12 h-12 rounded-2xl flex items-center justify-center shrink-0 ${accentClasses(meta.accent, true)}`}>
                          <Icon className="w-5 h-5" />
                        </div>
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2 mb-2">
                            <h3 className="text-lg font-black text-gray-900 truncate">{rule.name}</h3>
                            <span className={`px-2.5 py-1 rounded-full text-xs font-bold ${statusPill(rule.enabled)}`}>
                              {rule.enabled ? '已启用' : '已禁用'}
                            </span>
                          </div>
                          <div className="flex flex-wrap gap-2 text-xs font-bold">
                            <span className="px-2.5 py-1 rounded-lg bg-gray-100 text-gray-600">{meta.label}</span>
                            <span className="px-2.5 py-1 rounded-lg bg-gray-100 text-gray-600">{rule.item_title || rule.item_id || '账号级规则'}</span>
                            <span className="px-2.5 py-1 rounded-lg bg-blue-50 text-blue-700">{actionSummary(rule)}</span>
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-2 shrink-0">
                        <button
                          onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => openAutomationRule(rule)}
                          className="px-4 py-2 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-700 text-sm font-bold flex items-center gap-2"
                        >
                          <Edit className="w-4 h-4" />
                          编辑
                        </button>
                        <button
                          onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => handleToggleAutomation(rule)}
                          className={`px-4 py-2 rounded-xl text-sm font-bold ${rule.enabled ? 'bg-amber-50 text-amber-700 hover:bg-amber-100' : 'bg-emerald-50 text-emerald-700 hover:bg-emerald-100'}`}
                        >
                          {rule.enabled ? '禁用' : '启用'}
                        </button>
                        <button
                          onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => handleDeleteAutomation(rule.id)}
                          className="p-2.5 rounded-xl text-red-500 hover:bg-red-50"
                          title="删除"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                  </article>
                );
              })
			)}

			{automationTotal > 0 && (
			  <div className="flex flex-col gap-3 rounded-xl border border-gray-100 bg-white px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
				<div className="flex items-center gap-3 text-sm font-medium text-gray-500">
				  <span>第 {automationPage} / {Math.max(automationTotalPages, 1)} 页</span>
				  <span className="h-4 w-px bg-gray-200" />
				  <label className="flex items-center gap-2">
					<span className="sr-only">每页显示数量</span>
					<select
					  value={automationPageSize}
					  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => {
						setAutomationPageSize(Number(event.target.value));
						setAutomationPage(1);
					  }}
					  className="ios-input rounded-lg border-none bg-gray-50 px-2.5 py-2 text-sm"
					>
					  {[10, 20, 50].map(/* 当前回调处理集合中的单个元素。 */ size => <option key={size} value={size}>{size} 条/页</option>)}
					</select>
				  </label>
				</div>
				<div className="flex items-center gap-1.5">
				  <button
					type="button"
					disabled={automationPage <= 1 || loading}
					onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setAutomationPage(/* 当前回调处理用户交互或异步状态变化。 */ page => Math.max(1, page - 1))}
					className="flex h-9 w-9 items-center justify-center rounded-lg bg-gray-50 text-gray-600 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-40"
					aria-label="上一页"
					title="上一页"
				  >
					<ChevronLeft className="h-4 w-4" />
				  </button>
				  {automationPageNumbers.map(/* 当前回调处理集合中的单个元素。 */ pageNumber => (
					<button
					  key={pageNumber}
					  type="button"
					  disabled={loading}
					  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setAutomationPage(pageNumber)}
					  className={`h-9 min-w-9 rounded-lg px-2 text-sm font-bold transition-colors ${pageNumber === automationPage ? 'bg-brand text-white' : 'bg-gray-50 text-gray-600 hover:bg-gray-100'} disabled:cursor-not-allowed disabled:opacity-60`}
					  aria-label={`第 ${pageNumber} 页`}
					  aria-current={pageNumber === automationPage ? 'page' : undefined}
					>
					  {pageNumber}
					</button>
				  ))}
				  <button
					type="button"
					disabled={automationPage >= automationTotalPages || loading}
					onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setAutomationPage(/* 当前回调处理用户交互或异步状态变化。 */ page => Math.min(automationTotalPages, page + 1))}
					className="flex h-9 w-9 items-center justify-center rounded-lg bg-gray-50 text-gray-600 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-40"
					aria-label="下一页"
					title="下一页"
				  >
					<ChevronRight className="h-4 w-4" />
				  </button>
				</div>
			  </div>
			)}
		  </section>
        </div>
      )}

      {activeTab === 'reply' && (
        <section className="bg-white rounded-xl border border-gray-100 p-6 shadow-sm">
          <div className="flex items-center gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-2 rounded-xl mb-5 w-fit">
            <AlertCircle className="w-4 h-4" />
            这里只处理买家用户消息；系统通知不会进入关键词或 AI 回复。
          </div>
          <div className="space-y-3">
            {replyRules.map(/* 当前回调处理集合中的单个元素。 */ rule => (
              <div key={rule.id} className="flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border border-gray-100 bg-surface-subtle hover:bg-white hover:shadow-lg transition-all gap-4">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-2">
                    <span className="px-3 py-1 bg-black text-white rounded-lg text-xs font-bold">包含匹配</span>
                    <h3 className="font-bold text-gray-900">“{rule.keyword}”</h3>
                  </div>
                  <div className="bg-white p-3 rounded-xl border border-gray-100 text-sm text-gray-600 leading-relaxed">
                    {rule.type === 'image' && rule.image_url ? rule.image_url : rule.reply_content}
                  </div>
                </div>
                <div className="flex items-center gap-3 border-t md:border-t-0 md:border-l border-gray-200 pt-4 md:pt-0 md:pl-6">
                  <button
                    onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => {
                      setEditingReplyRule({ ...rule });
                      setShowReplyModal(true);
                    }}
                    className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                    title="编辑"
                  >
                    <Edit className="w-4 h-4" />
                  </button>
                  <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => handleDeleteReply(rule.id)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors" title="删除">
                    <Trash2 className="w-5 h-5" />
                  </button>
                </div>
              </div>
            ))}
            {replyRules.length === 0 && <div className="text-center py-20 text-gray-400">暂无关键词回复规则</div>}
          </div>
        </section>
      )}

      {activeTab === 'default' && (
        <section className="bg-white rounded-xl border border-gray-100 p-6 shadow-sm">
          <div className="flex items-center gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-2 rounded-xl mb-5 w-fit">
            <AlertCircle className="w-4 h-4" />
            默认回复只处理买家用户消息；关键词未命中且 AI 未接管时才会使用。
          </div>
          <div className="space-y-3">
            {visibleDefaultAccounts.map(/* 当前回调处理集合中的单个元素。 */ account => {
              // defaultReply 默认Reply，负责当前功能中的对应处理。
              const defaultReply = defaultReplies[account.id];
              // enabled 启用状态。
              const enabled = Boolean(defaultReply?.enabled);
              return (
                <div key={account.id} className={`flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border transition-all gap-4 ${enabled ? 'border-purple-100 bg-purple-50/50 hover:bg-white hover:shadow-lg' : 'border-gray-100 bg-surface-subtle hover:bg-white hover:shadow-lg'}`}>
                  <div className="flex items-center gap-4 min-w-0">
                    <div className={`w-12 h-12 rounded-2xl flex items-center justify-center ${enabled ? 'bg-purple-600 text-white' : 'bg-gray-200 text-gray-400'}`}>
                      <Bot className="w-5 h-5" />
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 mb-2">
                        <h3 className="font-bold text-gray-900 text-lg truncate">{accountLabel(account)}</h3>
                        <span className={`px-2 py-0.5 rounded-lg text-xs font-bold ${enabled ? 'bg-green-100 text-green-700' : 'bg-gray-200 text-gray-500'}`}>
                          {enabled ? '已启用' : '未启用'}
                        </span>
                        {defaultReply?.reply_once && (
                          <span className="px-2 py-0.5 rounded-lg text-xs font-bold bg-purple-100 text-purple-700">只回复一次</span>
                        )}
                      </div>
                      <div className="text-sm text-gray-600 line-clamp-2">
                        {enabled ? (defaultReply.reply_content || defaultReply.reply_image_url || '已配置默认回复') : '未配置默认回复'}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 border-t md:border-t-0 md:border-l border-gray-200 pt-4 md:pt-0 md:pl-6">
                    <button
                      onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void openDefaultReplyModal(account.id)}
                      className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                      title="编辑"
                    >
                      <Edit className="w-4 h-4" />
                    </button>
                    {enabled && (
                      <>
                        <button
                          onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void handleClearDefaultReplyRecords(account.id)}
                          className="px-3 py-2 text-xs font-bold text-blue-600 hover:bg-blue-50 rounded-xl transition-colors"
                        >
                          清空记录
                        </button>
                        <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void handleDeleteDefaultReply(account.id)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors" title="删除">
                          <Trash2 className="w-5 h-5" />
                        </button>
                      </>
                    )}
                  </div>
                </div>
              );
            })}
            {visibleDefaultAccounts.length === 0 && <div className="text-center py-20 text-gray-400">暂无账号</div>}
          </div>
        </section>
      )}

      {showAutomationModal && editingAutomationRule && createPortal(
        <div className="modal-overlay">
          <div className="modal-container" style={{ maxWidth: '72rem', maxHeight: '92vh' }}>
            <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between">
              <div>
                <h3 className="text-2xl font-black text-gray-900">{editingAutomationRule.id ? '编辑自动化规则' : '新建自动化规则'}</h3>
                <p className="text-sm text-gray-500 mt-1">{currentMeta.description}</p>
              </div>
              <button
                onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowAutomationModal(false)}
                className="w-10 h-10 rounded-2xl bg-gray-100 hover:bg-gray-200 flex items-center justify-center"
                title="关闭"
              >
                <X className="w-5 h-5 text-gray-600" />
              </button>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-[320px_1fr] min-h-0">
              <aside className="bg-slate-900 text-white p-5 overflow-y-auto">
                <div className="text-xs font-bold text-slate-400 mb-3">选择自动化类型</div>
                <div className="space-y-3">
                  {triggerOrder.map(/* 当前回调处理集合中的单个元素。 */ trigger => {
                    // meta 元数据。
                    const meta = triggerMeta[trigger];
                    // Icon 渲染Icon React 组件。
                    const Icon = meta.icon;
                    // selected 处理当前选择（ed）。
                    const selected = currentTrigger === trigger;
                    return (
                      <button
                        key={trigger}
                        type="button"
                        onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => handleTriggerChange(trigger)}
                        className={`w-full rounded-2xl p-4 text-left border transition-all ${
                          selected ? 'bg-white text-slate-950 border-white' : 'bg-white/5 text-white border-white/10 hover:bg-white/10'
                        }`}
                      >
                        <div className="flex items-start gap-3">
                          <Icon className={`w-5 h-5 mt-0.5 ${selected ? 'text-brand' : 'text-white'}`} />
                          <div>
                            <div className="font-black">{meta.label}</div>
                            <div className={`text-xs mt-1 leading-5 ${selected ? 'text-gray-500' : 'text-gray-400'}`}>{meta.description}</div>
                          </div>
                        </div>
                      </button>
                    );
                  })}
                </div>

                <div className="mt-6 rounded-2xl bg-white/5 border border-white/10 p-4">
                  <div className="text-xs font-bold text-slate-400 mb-3">执行流程</div>
                  <div className="space-y-3">
                    {currentMeta.flow.map(/* 当前回调处理集合中的单个元素。 */ (step, index) => (
                      <div key={step} className="flex items-center gap-3">
                        <div className="w-6 h-6 rounded-full bg-white text-slate-950 text-xs font-black flex items-center justify-center">{index + 1}</div>
                        <span className="text-sm font-bold text-gray-100">{step}</span>
                      </div>
                    ))}
                  </div>
                </div>
              </aside>

              <div className="p-6 overflow-y-auto bg-surface-subtle">
                <div className="space-y-5">
                  <section className="bg-white rounded-3xl border border-gray-100 p-5">
                    <div className="flex items-center gap-2 mb-4">
                      <CheckCircle2 className="w-5 h-5 text-brand" />
                      <h4 className="font-black text-gray-900">生效范围</h4>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      <div className="md:col-span-2">
                        <label className="block text-sm font-bold text-gray-700 mb-2">规则名称</label>
                        <input
                          type="text"
                          value={editingAutomationRule.name || ''}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({ ...editingAutomationRule, name: event.target.value })}
                          placeholder="不填时按类型和商品自动生成"
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">闲鱼账号</label>
                        <select
                          value={editingAutomationRule.cookie_id || ''}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({
                            ...editingAutomationRule,
                            cookie_id: event.target.value,
                            item_id: '',
                            item_title: '',
                            item_keyword: '',
                          })}
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        >
                          <option value="">选择账号</option>
                          {accounts.map(/* 当前回调处理集合中的单个元素。 */ account => (
                            <option key={account.id} value={account.id}>{accountLabel(account)}</option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">关联商品</label>
                        <select
                          value={editingAutomationRule.item_id || ''}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => handleAutomationItemChange(event.target.value)}
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        >
                          <option value="">账号级规则（不限定商品）</option>
                          {modalAccountItems.map(/* 当前回调处理集合中的单个元素。 */ item => (
                            <option key={`${item.cookie_id}-${item.item_id}`} value={item.item_id}>{item.item_title || item.item_id}</option>
                          ))}
                        </select>
                      </div>
                    </div>

                    {selectedRuleItem && currentTrigger !== 'review_missing_timeout' && (
                      <div className="mt-4 rounded-2xl bg-gray-50 border border-gray-100 p-4">
                        <div className="flex flex-wrap items-center gap-2 mb-2">
                          <span className="px-3 py-1.5 rounded-lg bg-gray-100 text-gray-700 text-xs font-bold">{selectedRuleItem.item_title || selectedRuleItem.item_id}</span>
                          <span className={`px-3 py-1.5 rounded-lg text-xs font-bold ${isMultiSpecRule ? 'bg-blue-50 text-blue-700' : 'bg-gray-100 text-gray-500'}`}>
                            {isMultiSpecRule ? '多规格商品' : '普通商品'}
                          </span>
                          <span className="px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-50 text-emerald-700">按订单购买数量自动发货</span>
                        </div>
                        <p className="text-xs leading-5 text-gray-500">
                          多规格状态来自闲鱼商品本身，发布后不能在这里修改；系统会在买家付款后读取订单详情，按实际购买规格和数量匹配下面的发货规则。
                        </p>
                      </div>
                    )}
                  </section>

                  {currentTrigger !== 'review_missing_timeout' ? (
                    <section className="bg-white rounded-3xl border border-gray-100 p-5">
                      <div className="flex items-start justify-between gap-4 mb-4">
                        <div>
                          <div className="flex items-center gap-2">
                            <Layers3 className="w-5 h-5 text-brand" />
                            <h4 className="font-black text-gray-900">{currentTrigger === 'buyer_reviewed' ? '赠品库存' : '发货库存'}</h4>
                          </div>
                          <p className="text-sm text-gray-500 mt-1">
                            {isMultiSpecRule
                              ? '每条发货内容绑定一个订单规格；同一规格可添加多条内容并全部发送。'
                              : '可添加多条发货内容，买家付款后会按顺序全部发送。'}
                          </p>
                        </div>
                        <button
                          type="button"
                          onClick={appendDeliveryContent}
                          className="px-3 py-2 rounded-xl bg-gray-900 text-white text-xs font-bold hover:bg-black flex items-center gap-1.5"
                        >
                          <Plus className="w-3.5 h-3.5" />
                          添加发货内容
                        </button>
                      </div>

                      <div className="space-y-3">
                        {displayVariants.map((variant, index) => (/* 当前回调处理集合中的单个元素。 */
                          <div
                            key={variant.id || index}
                            className={`grid grid-cols-1 gap-3 items-end rounded-2xl border border-gray-200 p-4 ${isMultiSpecRule ? 'md:grid-cols-[1fr_1fr_1.4fr_110px_40px]' : 'md:grid-cols-[1.4fr_110px_40px]'}`}
                          >
                            {isMultiSpecRule && (
                              <>
                                <div>
                                  <label className="block text-xs font-bold text-gray-600 mb-2">规格名称</label>
                                  <input
                                    value={variant.spec_name}
                                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => updateVariant(index, { spec_name: event.target.value })}
                                    className="w-full ios-input px-3 py-2.5 rounded-lg"
                                    placeholder="例如：套餐"
                                  />
                                </div>
                                <div>
                                  <label className="block text-xs font-bold text-gray-600 mb-2">规格值</label>
                                  <input
                                    value={variant.spec_value}
                                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => updateVariant(index, { spec_value: event.target.value })}
                                    className="w-full ios-input px-3 py-2.5 rounded-lg"
                                    placeholder="例如：30天"
                                  />
                                </div>
                              </>
                            )}
                            <div>
                              <label className="block text-xs font-bold text-gray-600 mb-2">卡密库存</label>
                              <select
                                value={variant.card_id || ''}
                                onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => updateVariant(index, { card_id: Number(event.target.value) })}
                                className="w-full ios-input px-3 py-2.5 rounded-lg"
                              >
                                <option value="">请选择卡密库存</option>
                                {cards.filter(/* 当前回调处理集合中的单个元素。 */ card => card.enabled && card.type !== 'api').map(/* 当前回调处理集合中的单个元素。 */ card => (
                                  <option key={card.id} value={card.id}>{card.name}</option>
                                ))}
                              </select>
                            </div>
                            <div>
                              <label className="block text-xs font-bold text-gray-600 mb-2">每件份数</label>
                              <input
                                type="number"
                                min="1"
                                max="100"
                                value={variant.delivery_count}
                                onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => updateVariant(index, { delivery_count: Math.max(1, Number(event.target.value) || 1) })}
                                className="w-full ios-input px-3 py-2.5 rounded-lg"
                              />
                            </div>
                            <div className="md:col-span-full flex flex-wrap items-center gap-3 rounded-xl bg-gray-50 px-3 py-2">
                              <label className="flex items-center gap-2 text-xs font-bold text-gray-600 cursor-pointer">
                                <input
                                  type="checkbox"
                                  checked={variant.delay_override === true}
                                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => updateVariant(index, { delay_override: event.target.checked })}
                                  className="accent-brand"
                                />
                                覆盖卡密默认延时
                              </label>
                              {variant.delay_override && (
                                <input
                                  type="number"
                                  min="0"
                                  max="3600"
                                  value={variant.delay_seconds || 0}
                                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => updateVariant(index, { delay_seconds: Math.max(0, Number(event.target.value) || 0) })}
                                  className="w-28 ios-input px-2 py-1.5 rounded-lg text-xs"
                                  aria-label="动作延时秒数"
                                />
                              )}
                              <span className="text-xs text-gray-500">{variant.delay_override ? `本动作延时 ${variant.delay_seconds || 0} 秒` : '使用卡密默认延时'}</span>
                            </div>
                            <button
                              type="button"
                              disabled={displayVariants.length === 1}
                              onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setEditingAutomationRule({
                                ...editingAutomationRule,
                                variants: displayVariants.filter(/* 当前回调处理集合中的单个元素。 */ (_, variantIndex) => variantIndex !== index),
                              })}
                              className="w-10 h-10 flex items-center justify-center rounded-lg text-red-500 hover:bg-red-50 disabled:opacity-25"
                              title="删除发货内容"
                            >
                              <Trash2 className="w-4 h-4" />
                            </button>
                          </div>
                        ))}
                      </div>
                    </section>
                  ) : (
                    <section className="bg-white rounded-3xl border border-gray-100 p-5">
                      <div className="flex items-center gap-2 mb-4">
                        <Clock3 className="w-5 h-5 text-amber-600" />
                        <h4 className="font-black text-gray-900">求评价计划</h4>
                      </div>
                      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                        <div>
                          <label className="block text-sm font-bold text-gray-700 mb-2">发货后等待小时</label>
                          <input
                            type="number"
                            min="1"
                            value={Number(reviewConfig.after_shipped_hours || 72)}
                            onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              config_json: buildReviewConfig(editingAutomationRule.config_json, {
                                after_shipped_hours: Math.max(1, Number(event.target.value) || 72),
                              }),
                            })}
                            className="w-full ios-input px-4 py-3 rounded-xl"
                          />
                        </div>
                        <div>
                          <label className="block text-sm font-bold text-gray-700 mb-2">再次求评间隔小时</label>
                          <input
                            type="number"
                            min="1"
                            value={Number(reviewConfig.repeat_interval_hours || 24)}
                            onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              config_json: buildReviewConfig(editingAutomationRule.config_json, {
                                repeat_interval_hours: Math.max(1, Number(event.target.value) || 24),
                              }),
                            })}
                            className="w-full ios-input px-4 py-3 rounded-xl"
                          />
                        </div>
                        <div>
                          <label className="block text-sm font-bold text-gray-700 mb-2">最多求评次数</label>
                          <input
                            type="number"
                            min="1"
                            value={Number(reviewConfig.max_attempts || 1)}
                            onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              config_json: buildReviewConfig(editingAutomationRule.config_json, {
                                max_attempts: Math.max(1, Number(event.target.value) || 1),
                              }),
                            })}
                            className="w-full ios-input px-4 py-3 rounded-xl"
                          />
                        </div>
                        <div className="md:col-span-3">
                          <label className="block text-sm font-bold text-gray-700 mb-2">求评价文案</label>
                          <textarea
                            value={editingAutomationRule.actions?.find(/* 当前回调处理集合中的单个元素。 */ action => action.action_type === 'send_text')?.message_template || ''}
                            onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              actions: (editingAutomationRule.actions?.length ? editingAutomationRule.actions : cardActionsForTrigger('review_missing_timeout')).map(/* 当前回调处理用户交互或异步状态变化。 */ action =>
                                action.action_type === 'send_text' ? { ...action, message_template: event.target.value } : action
                              ),
                            })}
                            className="w-full ios-input px-4 py-3 rounded-xl h-28 resize-none"
                          />
                        </div>
                      </div>
                    </section>
                  )}

                  <section className="bg-white rounded-3xl border border-gray-100 p-5">
                    <div className="grid grid-cols-1 md:grid-cols-[180px_1fr] gap-4 items-end">
                      <div>
						<label className="block text-sm font-bold text-gray-700 mb-2">优先级</label>
						<p className="text-xs text-gray-500 mb-2">数字越小优先级越高；同一账号、商品和触发条件只执行优先级最高的一条规则。</p>
                        <input
                          type="number"
                          value={editingAutomationRule.priority || 100}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({ ...editingAutomationRule, priority: Number(event.target.value) || 100 })}
                          min="1"
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        />
                      </div>
                      <label className="h-[48px] flex items-center gap-3 px-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
                        <input
                          type="checkbox"
                          checked={editingAutomationRule.enabled !== false}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingAutomationRule({ ...editingAutomationRule, enabled: event.target.checked })}
                          className="w-4 h-4 rounded"
                        />
                        启用规则
                      </label>
                    </div>
                  </section>
                </div>
              </div>
            </div>

            <div className="px-6 py-4 border-t border-gray-100 bg-white flex gap-3">
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowAutomationModal(false)} className="flex-1 px-6 py-3 rounded-2xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200">
                取消
              </button>
              <button onClick={handleSaveAutomationRule} className="flex-1 ios-btn-primary px-6 py-3 rounded-2xl font-bold flex items-center justify-center gap-2">
                <Save className="w-4 h-4" />
                保存自动化规则
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {showReplyModal && editingReplyRule && createPortal(
        <div className="modal-overlay">
          <div className="modal-container">
            <div className="modal-header">
              <div className="flex items-center justify-between w-full">
                <h3 className="text-2xl font-extrabold text-gray-900">
                  {editingReplyRule.id ? '编辑回复规则' : '新增回复规则'}
                </h3>
                <button
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowReplyModal(false)}
                  className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors"
                >
                  <X className="w-5 h-5 text-gray-600" />
                </button>
              </div>
            </div>

            <div className="modal-body space-y-5">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-bold text-gray-700 mb-2">关联商品</label>
                  <select
                    value={editingReplyRule.item_id || ''}
                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingReplyRule({ ...editingReplyRule, item_id: event.target.value })}
                    className="w-full ios-input px-4 py-3 rounded-xl"
                  >
                    <option value="">账号级回复</option>
                    {items.filter(/* 当前回调处理集合中的单个元素。 */ item => !selectedAccountId || item.cookie_id === selectedAccountId).map(/* 当前回调处理集合中的单个元素。 */ item => (
                      <option key={`${item.cookie_id}-${item.item_id}`} value={item.item_id}>{item.item_title || item.item_id}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-bold text-gray-700 mb-2">回复类型</label>
                  <select
                    value={editingReplyRule.type || 'text'}
                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => {
                      // type 类型。
                      const type = event.target.value as 'text' | 'image';
                      setEditingReplyRule({
                        ...editingReplyRule,
                        type,
                        reply_content: type === 'text' ? editingReplyRule.reply_content : '',
                        image_url: type === 'image' ? editingReplyRule.image_url : '',
                      });
                    }}
                    className="w-full ios-input px-4 py-3 rounded-xl"
                  >
                    <option value="text">文字</option>
                    <option value="image">图片</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">关键词</label>
                <input
                  type="text"
                  value={editingReplyRule.keyword || ''}
                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingReplyRule({ ...editingReplyRule, keyword: event.target.value })}
                  placeholder="买家发送的关键词"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              {editingReplyRule.type === 'image' ? (
                <div>
                  <label className="block text-sm font-bold text-gray-700 mb-2">图片 URL</label>
                  <input
                    value={editingReplyRule.image_url || ''}
                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingReplyRule({ ...editingReplyRule, image_url: event.target.value })}
                    placeholder="https://..."
                    className="w-full ios-input px-4 py-3 rounded-xl"
                  />
                </div>
              ) : (
                <div>
                  <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
                  <textarea
                    value={editingReplyRule.reply_content || ''}
                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setEditingReplyRule({ ...editingReplyRule, reply_content: event.target.value })}
                    placeholder="自动回复的内容"
                    className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
                  />
                </div>
              )}

              <div className="flex gap-3 pt-4">
                <button
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowReplyModal(false)}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={handleSaveReplyRule}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                >
                  <Send className="w-4 h-4" />
                  保存规则
                </button>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}

      {showDefaultModal && createPortal(
        <div className="modal-overlay">
          <div className="modal-container">
            <div className="modal-header">
              <div className="flex items-center justify-between w-full">
                <div>
                  <h3 className="text-2xl font-extrabold text-gray-900">账号默认回复</h3>
                  <p className="text-sm text-gray-500 mt-1">关键词和 AI 都未处理时，才会使用默认回复。</p>
                </div>
                <button
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowDefaultModal(false)}
                  className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors"
                  title="关闭"
                >
                  <X className="w-5 h-5 text-gray-600" />
                </button>
              </div>
            </div>

            <div className="modal-body space-y-5">
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">闲鱼账号</label>
                <select
                  value={defaultForm.cookie_id}
                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setDefaultForm({ ...defaultForm, cookie_id: event.target.value })}
                  className="w-full ios-input px-4 py-3 rounded-xl"
                >
                  <option value="">选择账号</option>
                  {accounts.map(/* 当前回调处理集合中的单个元素。 */ account => (
                    <option key={account.id} value={account.id}>{accountLabel(account)}</option>
                  ))}
                </select>
              </div>

              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">启用默认回复</div>
                  <div className="text-xs text-gray-500 mt-1">启用后，未命中关键词时自动发送</div>
                </div>
                <button
                  type="button"
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setDefaultForm({ ...defaultForm, enabled: !defaultForm.enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${defaultForm.enabled ? 'bg-brand' : 'bg-gray-300'}`}
                >
                  <span className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 block ${defaultForm.enabled ? 'translate-x-7' : 'translate-x-1'}`} />
                </button>
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
                <textarea
                  value={defaultForm.reply_content}
                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setDefaultForm({ ...defaultForm, reply_content: event.target.value })}
                  placeholder="输入默认回复内容"
                  className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复图片 URL（可选）</label>
                <input
                  type="text"
                  value={defaultForm.reply_image_url}
                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setDefaultForm({ ...defaultForm, reply_image_url: event.target.value })}
                  placeholder="https://example.com/image.jpg"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              <label className="flex items-center justify-between p-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
                <span>
                  只回复一次
                  <span className="block text-xs text-gray-500 font-medium mt-1">同一会话只发送一次默认回复</span>
                </span>
                <input
                  type="checkbox"
                  checked={defaultForm.reply_once}
                  onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setDefaultForm({ ...defaultForm, reply_once: event.target.checked })}
                  className="w-4 h-4 rounded"
                />
              </label>

              <div className="flex gap-3 pt-4">
                <button
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowDefaultModal(false)}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={handleSaveDefaultReply}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  保存默认回复
                </button>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}
    </div>
  );
};

export default Rules;
