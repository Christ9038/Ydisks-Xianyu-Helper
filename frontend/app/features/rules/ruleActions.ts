import { useCallback,useEffect,useMemo,useState,type Dispatch,type SetStateAction } from 'react';
import {
clearDefaultReplyRecords,
deleteDefaultReply,
deleteReplyRule,
deleteShippingRule,
getCards,
getDefaultReply,
getItems,
getShippingRules,
resolveAutomationRun,
resolveDeferredAutomationTask,
updateDefaultReply,
updateReplyRule,
updateShippingRule,
} from './api';
import { finishRuleSubmission,idleRuleSubmitState,startRuleSubmission,type RuleSubmitState } from './interactionState';
import type { AutomationTriggerType,Card,DefaultReplyForm,Item,ReplyRule,RulesProps,RulesTab,ShippingRule,ShippingVariant } from './types';
import { boolFlag,buildReviewConfig,cardActionsForTrigger,defaultRuleName,emptyVariant,parseJSONObject,shouldReplaceGeneratedName,triggerMeta } from './utils';

// RuleActionsOptions 描述规则动作协调器依赖的页面数据、刷新函数和外部联动目标。
export interface RuleActionsOptions {
  // selectedAccountId 保存当前规则页面选择的账号。
  selectedAccountId: string;
  // setSelectedAccountId 更新当前规则页面选择的账号。
  setSelectedAccountId: Dispatch<SetStateAction<string>>;
  // setActiveTab 将外部联动切换到自动化页签。
  setActiveTab: Dispatch<SetStateAction<RulesTab>>;
  // items 保存规则编辑器可绑定的商品列表。
  items: Item[];
  // setAutomationRules 写入外部联动场景加载的自动化规则。
  setAutomationRules: Dispatch<SetStateAction<ShippingRule[]>>;
  // setCards 写入外部联动场景加载的卡密库存。
  setCards: Dispatch<SetStateAction<Card[]>>;
  // setItems 写入外部联动场景加载的商品列表。
  setItems: Dispatch<SetStateAction<Item[]>>;
  // setLoading 更新规则页面的加载指示器。
  setLoading: Dispatch<SetStateAction<boolean>>;
  // loadAutomationRules 刷新自动化规则和异常任务。
  loadAutomationRules: () => Promise<void>;
  // loadReferenceData 刷新规则编辑器参考数据。
  loadReferenceData: () => Promise<void>;
  // loadReplyRules 刷新关键词回复规则。
  loadReplyRules: () => Promise<void>;
  // loadDefaultReplies 刷新账号默认回复。
  loadDefaultReplies: () => Promise<void>;
  // initialDeliveryTarget 保存商品页跳转到规则页的目标。
  initialDeliveryTarget?: RulesProps['initialDeliveryTarget'];
  // onDeliveryTargetHandled 通知父页面外部跳转已完成。
  onDeliveryTargetHandled?: () => void;
}

// RuleActionsState 暴露规则页弹窗状态、编辑草稿和业务动作。
export interface RuleActionsState {
  // showAutomationModal 表示自动化规则弹窗是否打开。
  showAutomationModal: boolean;
  // setShowAutomationModal 更新自动化规则弹窗展示状态。
  setShowAutomationModal: Dispatch<SetStateAction<boolean>>;
  // showReplyModal 表示关键词回复弹窗是否打开。
  showReplyModal: boolean;
  // setShowReplyModal 更新关键词回复弹窗展示状态。
  setShowReplyModal: Dispatch<SetStateAction<boolean>>;
  // showDefaultModal 表示默认回复弹窗是否打开。
  showDefaultModal: boolean;
  // setShowDefaultModal 更新默认回复弹窗展示状态。
  setShowDefaultModal: Dispatch<SetStateAction<boolean>>;
  // automationSubmitState 保存自动化规则提交状态。
  automationSubmitState: RuleSubmitState;
  // replySubmitState 保存关键词回复提交状态。
  replySubmitState: RuleSubmitState;
  // defaultReplySubmitState 保存默认回复提交状态。
  defaultReplySubmitState: RuleSubmitState;
  // editingAutomationRule 保存当前自动化规则草稿。
  editingAutomationRule: Partial<ShippingRule> | null;
  // setEditingAutomationRule 更新自动化规则草稿。
  setEditingAutomationRule: Dispatch<SetStateAction<Partial<ShippingRule> | null>>;
  // editingReplyRule 保存当前关键词回复草稿。
  editingReplyRule: Partial<ReplyRule> | null;
  // setEditingReplyRule 更新关键词回复草稿。
  setEditingReplyRule: Dispatch<SetStateAction<Partial<ReplyRule> | null>>;
  // defaultForm 保存当前默认回复草稿。
  defaultForm: DefaultReplyForm;
  // setDefaultForm 更新默认回复草稿。
  setDefaultForm: Dispatch<SetStateAction<DefaultReplyForm>>;
  // selectedRuleItem 保存当前规则草稿绑定的商品。
  selectedRuleItem: Item | undefined;
  // isMultiSpecRule 表示当前商品是否需要多规格字段。
  isMultiSpecRule: boolean;
  // currentTrigger 保存当前规则草稿的触发类型。
  currentTrigger: AutomationTriggerType;
  // currentMeta 保存当前触发类型的展示元数据。
  currentMeta: typeof triggerMeta[AutomationTriggerType];
  // reviewConfig 保存当前评价规则配置对象。
  reviewConfig: Record<string, unknown>;
  // displayVariants 保存至少包含一行的规格草稿。
  displayVariants: ShippingVariant[];
  // buildAutomationDraft 创建指定触发类型的规则草稿。
  buildAutomationDraft: (trigger?: AutomationTriggerType, cookieID?: string, itemID?: string) => Partial<ShippingRule>;
  // openAutomationRule 打开指定自动化规则的编辑弹窗。
  openAutomationRule: (rule: ShippingRule) => void;
  // openNewAutomationRule 打开新建自动化规则弹窗。
  openNewAutomationRule: (trigger?: AutomationTriggerType) => void;
  // handleTriggerChange 切换当前规则的触发类型。
  handleTriggerChange: (trigger: AutomationTriggerType) => void;
  // handleAutomationItemChange 切换当前规则绑定的商品。
  handleAutomationItemChange: (itemID: string) => void;
  // updateVariant 更新指定发货规格的字段。
  updateVariant: (index: number, patch: Partial<ShippingVariant>) => void;
  // appendDeliveryContent 追加一行发货规格。
  appendDeliveryContent: () => void;
  // handleSaveAutomationRule 保存当前自动化规则。
  handleSaveAutomationRule: () => Promise<void>;
  // handleDeleteAutomation 删除指定自动化规则。
  handleDeleteAutomation: (id: string) => Promise<void>;
  // handleToggleAutomation 切换指定自动化规则启用状态。
  handleToggleAutomation: (rule: ShippingRule) => Promise<void>;
  // handleResolveRunIssue 处理暂停中的自动化运行。
  handleResolveRunIssue: (id: number, resolution: 'continue' | 'retry' | 'cancel') => Promise<void>;
  // handleResolveDeferredIssue 处理等待重试的自动化任务。
  handleResolveDeferredIssue: (id: number, resolution: 'retry' | 'dismiss') => Promise<void>;
  // handleAddReplyRule 打开新增关键词回复弹窗。
  handleAddReplyRule: () => void;
  // handleSaveReplyRule 保存当前关键词回复规则。
  handleSaveReplyRule: () => Promise<void>;
  // handleDeleteReply 删除指定关键词回复规则。
  handleDeleteReply: (id: string) => Promise<void>;
  // openDefaultReplyModal 打开指定账号的默认回复弹窗。
  openDefaultReplyModal: (cookieID?: string) => Promise<void>;
  // handleSaveDefaultReply 保存当前默认回复配置。
  handleSaveDefaultReply: () => Promise<void>;
  // handleDeleteDefaultReply 删除指定账号的默认回复配置。
  handleDeleteDefaultReply: (cookieID: string) => Promise<void>;
  // handleClearDefaultReplyRecords 清空指定账号的默认回复记录。
  handleClearDefaultReplyRecords: (cookieID: string) => Promise<void>;
}

// useRuleActions 集中管理规则页三类规则的编辑、保存、删除和异常恢复动作。
export const useRuleActions = ({
  selectedAccountId,
  setSelectedAccountId,
  setActiveTab,
  items,
  setAutomationRules,
  setCards,
  setItems,
  setLoading,
  loadAutomationRules,
  loadReferenceData,
  loadReplyRules,
  loadDefaultReplies,
  initialDeliveryTarget,
  onDeliveryTargetHandled,
}: RuleActionsOptions): RuleActionsState => {
  // showAutomationModal 表示自动化规则弹窗是否打开。
  const [showAutomationModal, setShowAutomationModal] = useState(false);
  // showReplyModal 表示关键词回复弹窗是否打开。
  const [showReplyModal, setShowReplyModal] = useState(false);
  // showDefaultModal 表示默认回复弹窗是否打开。
  const [showDefaultModal, setShowDefaultModal] = useState(false);
  // automationSubmitState 保存自动化规则提交状态。
  const [automationSubmitState, setAutomationSubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // replySubmitState 保存关键词回复提交状态。
  const [replySubmitState, setReplySubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // defaultReplySubmitState 保存默认回复提交状态。
  const [defaultReplySubmitState, setDefaultReplySubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // editingAutomationRule 保存当前自动化规则草稿。
  const [editingAutomationRule, setEditingAutomationRule] = useState<Partial<ShippingRule> | null>(null);
  // editingReplyRule 保存当前关键词回复草稿。
  const [editingReplyRule, setEditingReplyRule] = useState<Partial<ReplyRule> | null>(null);
  // defaultForm 保存当前默认回复草稿。
  const [defaultForm, setDefaultForm] = useState<DefaultReplyForm>({ cookie_id: '', enabled: false, reply_content: '', reply_once: false, reply_image_url: '' });

  // selectedRuleItem 查找当前自动化规则草稿绑定的商品。
  const selectedRuleItem = useMemo(
    /* 当前回调计算规则草稿绑定的商品。 */ () => editingAutomationRule?.cookie_id && editingAutomationRule.item_id
      ? items.find(/* 当前回调查找匹配的商品。 */ item => item.cookie_id === editingAutomationRule.cookie_id && item.item_id === editingAutomationRule.item_id)
      : undefined,
    [editingAutomationRule?.cookie_id, editingAutomationRule?.item_id, items],
  );
  // isMultiSpecRule 表示当前商品是否需要填写规格名称和值。
  const isMultiSpecRule = boolFlag(selectedRuleItem?.is_multi_spec);
  // currentTrigger 保存当前规则草稿的触发类型。
  const currentTrigger = (editingAutomationRule?.trigger_type || 'order_paid') as AutomationTriggerType;
  // currentMeta 保存当前触发类型的页面展示元数据。
  const currentMeta = triggerMeta[currentTrigger];
  // reviewConfig 将当前评价规则配置解析为对象。
  const reviewConfig = parseJSONObject(editingAutomationRule?.config_json);
  // displayVariants 为编辑器提供至少一行规格数据。
  const displayVariants = editingAutomationRule?.variants?.length ? editingAutomationRule.variants : [emptyVariant()];

  // buildAutomationDraft 创建指定触发类型的默认规则草稿。
  const buildAutomationDraft = useCallback(/* buildDraftAction 创建规则默认草稿。 */ (trigger: AutomationTriggerType = 'order_paid', cookieID = selectedAccountId, itemID = ''): Partial<ShippingRule> => {
    // item 保存规则绑定的商品。
    const item = items.find(/* 当前回调查找规则绑定的商品。 */ candidate => candidate.cookie_id === cookieID && candidate.item_id === itemID);
    // itemLabel 保存规则名称使用的商品标签。
    const itemLabel = item?.item_title || itemID;
    return {
      name: defaultRuleName(trigger, itemLabel), trigger_type: trigger, cookie_id: cookieID, item_id: itemID,
      item_title: item?.item_title || '', item_keyword: itemLabel, card_group_id: 0, priority: 100, enabled: true,
      config_json: trigger === 'review_missing_timeout' ? buildReviewConfig() : '{}', actions: cardActionsForTrigger(trigger),
      variants: trigger === 'review_missing_timeout' ? [] : [emptyVariant()],
    };
  }, [items, selectedAccountId]);

  // openAutomationRule 将已有规则复制为草稿并打开编辑弹窗。
  const openAutomationRule = useCallback(/* openRuleAction 打开已有规则草稿。 */ (rule: ShippingRule) => {
    // trigger 保存已有规则的触发类型。
    const trigger = (rule.trigger_type || 'order_paid') as AutomationTriggerType;
    setEditingAutomationRule({
      ...rule,
      trigger_type: trigger,
      config_json: trigger === 'review_missing_timeout' ? buildReviewConfig(rule.config_json) : (rule.config_json || '{}'),
      actions: rule.actions?.length ? rule.actions.map(/* 当前回调复制自动化动作。 */ action => ({ ...action })) : cardActionsForTrigger(trigger, rule.card_group_id),
      variants: rule.variants?.length ? rule.variants.map(/* 当前回调复制发货规格。 */ variant => ({ ...variant })) : (trigger === 'review_missing_timeout' ? [] : [emptyVariant()]),
    });
    setShowAutomationModal(true);
  }, []);

  // useEffect 处理商品页跳转到规则页时的异步加载和取消。
  useEffect(/* 当前回调处理外部商品联动。 */ () => {
    if (!initialDeliveryTarget) return;
    // cancelled 标记外部跳转加载是否已经失效。
    let cancelled = false;
    // openLinkedRule 加载跳转目标对应的规则、商品和卡密数据。
    const openLinkedRule = async () => {
      setActiveTab('automation');
      setSelectedAccountId(initialDeliveryTarget.cookieId);
      setLoading(true);
      try {
        // ruleList 保存当前账号的自动化规则。
        const [ruleList, itemList, cardList] = await Promise.all([
          getShippingRules(), getItems(), getCards().catch(/* error 表示卡密参考数据加载异常。 */ error => {
            console.warn('加载卡密库存失败，不阻断打开自动化规则', error);
            return [];
          }),
        ]);
        if (cancelled) return;
        setAutomationRules(ruleList);
        setItems(itemList);
        setCards(cardList);
        // rule 保存跳转商品已有的订单支付规则。
        const rule = ruleList.find(/* 当前回调查找跳转目标规则。 */ candidate => candidate.cookie_id === initialDeliveryTarget.cookieId && candidate.item_id === initialDeliveryTarget.itemId && candidate.trigger_type === 'order_paid');
        if (rule) {
          openAutomationRule(rule);
        } else {
          // item 保存跳转商品的参考数据。
          const item = itemList.find(/* 当前回调查找跳转目标商品。 */ candidate => candidate.cookie_id === initialDeliveryTarget.cookieId && candidate.item_id === initialDeliveryTarget.itemId);
          setEditingAutomationRule({
            ...buildAutomationDraft('order_paid', initialDeliveryTarget.cookieId, initialDeliveryTarget.itemId),
            item_title: item?.item_title || '', item_keyword: item?.item_title || initialDeliveryTarget.itemId,
            name: defaultRuleName('order_paid', item?.item_title || initialDeliveryTarget.itemId),
          });
          setShowAutomationModal(true);
        }
      } catch (/* error 表示外部联动加载异常。 */ error) {
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
    return /* 当前回调取消外部联动请求。 */ () => { cancelled = true; };
  }, [buildAutomationDraft, initialDeliveryTarget, onDeliveryTargetHandled, openAutomationRule, setActiveTab, setAutomationRules, setCards, setItems, setLoading, setSelectedAccountId]);

  // openNewAutomationRule 创建指定类型的规则草稿并打开弹窗。
  const openNewAutomationRule = useCallback(/* openNewAction 创建新规则草稿。 */ (trigger: AutomationTriggerType = 'order_paid') => {
    if (!selectedAccountId) {
      alert('请先选择账号');
      return;
    }
    setEditingAutomationRule(buildAutomationDraft(trigger));
    setShowAutomationModal(true);
  }, [buildAutomationDraft, selectedAccountId]);

  // handleTriggerChange 切换规则触发类型并保留当前卡密组。
  const handleTriggerChange = useCallback(/* triggerAction 切换规则触发类型。 */ (trigger: AutomationTriggerType) => {
    if (!editingAutomationRule) return;
    // currentCardID 保存当前规则选中的卡密组。
    const currentCardID = editingAutomationRule.variants?.find(/* 当前回调查找当前卡密组。 */ variant => variant.card_id)?.card_id || editingAutomationRule.actions?.find(/* 当前回调查找发卡动作。 */ action => action.action_type === 'send_card')?.card_id || editingAutomationRule.card_group_id || 0;
    // itemLabel 保存当前规则商品名称。
    const itemLabel = selectedRuleItem?.item_title || editingAutomationRule.item_title || editingAutomationRule.item_id || '';
    setEditingAutomationRule({
      ...editingAutomationRule, trigger_type: trigger,
      name: shouldReplaceGeneratedName(editingAutomationRule.name) ? defaultRuleName(trigger, itemLabel) : editingAutomationRule.name,
      card_group_id: currentCardID, config_json: trigger === 'review_missing_timeout' ? buildReviewConfig(editingAutomationRule.config_json) : '{}',
      actions: cardActionsForTrigger(trigger, currentCardID),
      variants: trigger === 'review_missing_timeout' ? [] : (editingAutomationRule.variants?.length ? editingAutomationRule.variants : [{ ...emptyVariant(), card_id: currentCardID }]),
    });
  }, [editingAutomationRule, selectedRuleItem]);

  // handleAutomationItemChange 切换规则商品并同步商品标题和默认名称。
  const handleAutomationItemChange = useCallback(/* itemAction 切换规则绑定商品。 */ (itemID: string) => {
    if (!editingAutomationRule) return;
    // item 保存当前账号下选择的商品。
    const item = items.find(/* 当前回调查找切换后的商品。 */ candidate => candidate.cookie_id === (editingAutomationRule.cookie_id || selectedAccountId) && candidate.item_id === itemID);
    // itemLabel 保存商品名称或商品 ID。
    const itemLabel = item?.item_title || itemID;
    setEditingAutomationRule({
      ...editingAutomationRule, item_id: itemID, item_title: item?.item_title || '', item_keyword: itemLabel,
      name: shouldReplaceGeneratedName(editingAutomationRule.name) ? defaultRuleName(currentTrigger, itemLabel) : editingAutomationRule.name,
    });
  }, [currentTrigger, editingAutomationRule, items, selectedAccountId]);

  // updateVariant 更新规则中的一行发货规格。
  const updateVariant = useCallback(/* variantAction 更新规则规格。 */ (index: number, patch: Partial<ShippingVariant>) => {
    if (!editingAutomationRule) return;
    // next 保存合并后的规格列表。
    const next = displayVariants.map(/* 当前回调合并指定规格字段。 */ (variant, variantIndex) => variantIndex === index ? { ...variant, ...patch } : variant);
    setEditingAutomationRule({ ...editingAutomationRule, variants: next, card_group_id: next[0]?.card_id || 0 });
  }, [displayVariants, editingAutomationRule]);

  // appendDeliveryContent 在规格编辑器末尾追加一行。
  const appendDeliveryContent = useCallback(/* appendVariantAction 追加规则规格。 */ () => {
    if (!editingAutomationRule) return;
    // previous 保存上一行规格，用于多规格商品复制规格名称和值。
    const previous = displayVariants[displayVariants.length - 1];
    setEditingAutomationRule({
      ...editingAutomationRule,
      variants: [...displayVariants, { ...emptyVariant(), spec_name: isMultiSpecRule ? previous?.spec_name || '' : '', spec_value: isMultiSpecRule ? previous?.spec_value || '' : '' }],
    });
  }, [displayVariants, editingAutomationRule, isMultiSpecRule]);

  // handleSaveAutomationRule 校验并保存当前自动化规则。
  const handleSaveAutomationRule = useCallback(/* saveRuleAction 保存自动化规则。 */ async () => {
    if (!editingAutomationRule || automationSubmitState.submitting) return;
    // trigger 保存当前规则触发类型。
    const trigger = (editingAutomationRule.trigger_type || 'order_paid') as AutomationTriggerType;
    if (!editingAutomationRule.cookie_id) return alert('请选择账号');
    // variants 保存当前规格列表。
    const variants = editingAutomationRule.variants?.length ? editingAutomationRule.variants : [];
    if (trigger !== 'review_missing_timeout') {
      if (!variants.length || variants.some(/* 当前回调校验卡密组是否已选择。 */ variant => !variant.card_id)) return alert(trigger === 'buyer_reviewed' ? '请选择评价赠品卡密库存' : '请选择发货卡密库存');
      if (isMultiSpecRule && variants.some(/* 当前回调校验多规格字段。 */ variant => !variant.spec_name.trim() || !variant.spec_value.trim())) return alert('多规格商品必须填写每一行的规格名称和规格值');
    }
    if (trigger === 'review_missing_timeout') {
      // text 保存求评价动作的文案。
      const text = editingAutomationRule.actions?.find(/* 当前回调查找求评价文案动作。 */ action => action.action_type === 'send_text')?.message_template || '';
      if (!text.trim()) return alert('请填写求评价文案');
    }
    // saveVariants 保存归一化后的发货规格。
    const saveVariants = trigger === 'review_missing_timeout' ? [] : variants.map(/* 当前回调归一化发货规格字段。 */ variant => ({ ...variant, spec_name: isMultiSpecRule ? variant.spec_name.trim() : '', spec_value: isMultiSpecRule ? variant.spec_value.trim() : '', delivery_count: Math.max(1, Number(variant.delivery_count) || 1), enabled: variant.enabled !== false }));
    setAutomationSubmitState(startRuleSubmission(automationSubmitState));
    // succeeded 记录保存是否成功。
    let succeeded = false;
    try {
      await updateShippingRule({ ...editingAutomationRule, trigger_type: trigger, name: (editingAutomationRule.name || '').trim() || defaultRuleName(trigger, selectedRuleItem?.item_title || editingAutomationRule.item_id || ''), config_json: trigger === 'review_missing_timeout' ? buildReviewConfig(editingAutomationRule.config_json) : (editingAutomationRule.config_json || '{}'), actions: editingAutomationRule.actions?.length ? editingAutomationRule.actions : cardActionsForTrigger(trigger, saveVariants[0]?.card_id || editingAutomationRule.card_group_id || 0), variants: saveVariants });
      setShowAutomationModal(false);
      await Promise.all([loadAutomationRules(), loadReferenceData()]);
      alert('保存成功');
      succeeded = true;
    } catch (/* error 表示自动化规则保存异常。 */ error) {
      console.error('保存自动化规则失败:', error);
      alert('保存失败：' + (error as Error).message);
    } finally {
      setAutomationSubmitState(/* current 保存自动化提交状态。 */ current => finishRuleSubmission(current, succeeded));
    }
  }, [automationSubmitState, editingAutomationRule, isMultiSpecRule, loadAutomationRules, loadReferenceData, selectedRuleItem]);

  // handleDeleteAutomation 删除自动化规则并刷新列表。
  const handleDeleteAutomation = useCallback(/* deleteRuleAction 删除自动化规则。 */ async (id: string) => {
    if (!confirm('确定删除该自动化规则吗？')) return;
    try { await deleteShippingRule(id); await loadAutomationRules(); alert('删除成功'); } catch (/* error 表示自动化规则删除异常。 */ error) { alert('删除失败：' + (error as Error).message); }
  }, [loadAutomationRules]);

  // handleToggleAutomation 切换规则启用状态并刷新列表。
  const handleToggleAutomation = useCallback(/* toggleRuleAction 切换自动化规则状态。 */ async (rule: ShippingRule) => {
    try { await updateShippingRule({ ...rule, enabled: !rule.enabled }); await loadAutomationRules(); } catch (/* error 表示自动化规则状态更新异常。 */ error) { alert('操作失败：' + (error as Error).message); }
  }, [loadAutomationRules]);

  // handleResolveRunIssue 处理自动化运行的人工恢复决策。
  const handleResolveRunIssue = useCallback(/* resolveRunAction 处理自动化运行异常。 */ async (id: number, resolution: 'continue' | 'retry' | 'cancel') => {
    // prompt 保存当前恢复操作的确认文案。
    const prompt = resolution === 'continue' ? '确认外部动作已经执行成功，并跳到下一步吗？' : resolution === 'retry' ? '确认外部动作没有执行，可以安全重试吗？错误判断可能造成重复发送。' : '确认终止该自动化运行吗？';
    if (!confirm(prompt)) return;
    try { await resolveAutomationRun(id, resolution); await loadAutomationRules(); } catch (/* error 表示自动化运行恢复异常。 */ error) { alert('处理失败：' + (error as Error).message); }
  }, [loadAutomationRules]);

  // handleResolveDeferredIssue 处理延迟自动化任务的重试或忽略。
  const handleResolveDeferredIssue = useCallback(/* resolveDeferredAction 处理延迟任务异常。 */ async (id: number, resolution: 'retry' | 'dismiss') => {
    if (!confirm(resolution === 'retry' ? '确认重新执行该任务吗？' : '确认忽略并删除该异常任务吗？')) return;
    try { await resolveDeferredAutomationTask(id, resolution); await loadAutomationRules(); } catch (/* error 表示延迟任务恢复异常。 */ error) { alert('处理失败：' + (error as Error).message); }
  }, [loadAutomationRules]);

  // handleAddReplyRule 打开一个空的关键词回复草稿。
  const handleAddReplyRule = useCallback(/* addReplyAction 创建关键词回复草稿。 */ () => {
    if (!selectedAccountId) return alert('请先选择账号');
    setEditingReplyRule({ keyword: '', reply_content: '', image_url: '', item_id: '', type: 'text', match_type: 'fuzzy', enabled: true });
    setShowReplyModal(true);
  }, [selectedAccountId]);

  // handleSaveReplyRule 校验并保存关键词回复规则。
  const handleSaveReplyRule = useCallback(/* saveReplyAction 保存关键词回复。 */ async () => {
    if (!editingReplyRule || !selectedAccountId || replySubmitState.submitting) return;
    // hasReplyContent 表示当前回复是否填写了文字或图片。
    const hasReplyContent = editingReplyRule.type === 'image' ? Boolean(editingReplyRule.image_url?.trim()) : Boolean(editingReplyRule.reply_content?.trim());
    if (!editingReplyRule.keyword?.trim() || !hasReplyContent) return alert('请填写关键词和回复内容');
    setReplySubmitState(startRuleSubmission(replySubmitState));
    // succeeded 记录保存是否成功。
    let succeeded = false;
    try { await updateReplyRule({ ...editingReplyRule, match_type: 'fuzzy', enabled: true }, selectedAccountId); setShowReplyModal(false); await loadReplyRules(); alert('保存成功'); succeeded = true; } catch (/* error 表示关键词回复保存异常。 */ error) { alert('保存失败：' + (error as Error).message); } finally { setReplySubmitState(/* current 保存关键词提交状态。 */ current => finishRuleSubmission(current, succeeded)); }
  }, [editingReplyRule, loadReplyRules, replySubmitState, selectedAccountId]);

  // handleDeleteReply 删除指定关键词回复并刷新列表。
  const handleDeleteReply = useCallback(/* deleteReplyAction 删除关键词回复。 */ async (id: string) => {
    if (!selectedAccountId || !confirm('确定删除该回复规则吗？')) return;
    try { await deleteReplyRule(id, selectedAccountId); await loadReplyRules(); alert('删除成功'); } catch (/* error 表示关键词回复删除异常。 */ error) { alert('删除失败：' + (error as Error).message); }
  }, [loadReplyRules, selectedAccountId]);

  // openDefaultReplyModal 加载指定账号的默认回复配置并打开弹窗。
  const openDefaultReplyModal = useCallback(/* openDefaultAction 加载默认回复草稿。 */ async (cookieID = selectedAccountId) => {
    if (!cookieID) return alert('请先选择账号');
    try {
      // data 保存服务端返回的默认回复配置。
      const data = await getDefaultReply(cookieID);
      setDefaultForm({ cookie_id: cookieID, enabled: data.enabled, reply_content: data.reply_content, reply_once: data.reply_once, reply_image_url: data.reply_image_url || '' });
    } catch {
      setDefaultForm({ cookie_id: cookieID, enabled: false, reply_content: '', reply_once: false, reply_image_url: '' });
    }
    setShowDefaultModal(true);
  }, [selectedAccountId]);

  // handleSaveDefaultReply 校验并保存账号默认回复。
  const handleSaveDefaultReply = useCallback(/* saveDefaultAction 保存默认回复。 */ async () => {
    if (defaultReplySubmitState.submitting) return;
    if (!defaultForm.cookie_id) return alert('请先选择账号');
    if (defaultForm.enabled && !defaultForm.reply_content.trim() && !defaultForm.reply_image_url.trim()) return alert('启用默认回复时，请填写回复内容或图片 URL');
    setDefaultReplySubmitState(startRuleSubmission(defaultReplySubmitState));
    // succeeded 记录保存是否成功。
    let succeeded = false;
    try { await updateDefaultReply(defaultForm.cookie_id, { enabled: defaultForm.enabled, reply_content: defaultForm.reply_content, reply_once: defaultForm.reply_once, reply_image_url: defaultForm.reply_image_url }); setShowDefaultModal(false); await loadDefaultReplies(); alert('保存成功'); succeeded = true; } catch (/* error 表示默认回复保存异常。 */ error) { alert('保存失败：' + (error as Error).message); } finally { setDefaultReplySubmitState(/* current 保存默认回复提交状态。 */ current => finishRuleSubmission(current, succeeded)); }
  }, [defaultForm, defaultReplySubmitState, loadDefaultReplies]);

  // handleDeleteDefaultReply 删除账号默认回复配置。
  const handleDeleteDefaultReply = useCallback(/* deleteDefaultAction 删除默认回复。 */ async (cookieID: string) => {
    if (!confirm('确定删除该账号默认回复吗？')) return;
    try { await deleteDefaultReply(cookieID); await loadDefaultReplies(); alert('删除成功'); } catch (/* error 表示默认回复删除异常。 */ error) { alert('删除失败：' + (error as Error).message); }
  }, [loadDefaultReplies]);

  // handleClearDefaultReplyRecords 清空账号默认回复的会话记录。
  const handleClearDefaultReplyRecords = useCallback(/* clearDefaultRecordsAction 清空默认回复记录。 */ async (cookieID: string) => {
    if (!confirm('确定清空该账号的默认回复记录吗？清空后可重新对所有会话使用“只回复一次”。')) return;
    try { await clearDefaultReplyRecords(cookieID); alert('清空成功'); } catch (/* error 表示默认回复记录清理异常。 */ error) { alert('清空失败：' + (error as Error).message); }
  }, []);

  return {
    showAutomationModal, setShowAutomationModal, showReplyModal, setShowReplyModal, showDefaultModal, setShowDefaultModal,
    automationSubmitState, replySubmitState, defaultReplySubmitState, editingAutomationRule, setEditingAutomationRule,
    editingReplyRule, setEditingReplyRule, defaultForm, setDefaultForm, selectedRuleItem, isMultiSpecRule, currentTrigger,
    currentMeta, reviewConfig, displayVariants, buildAutomationDraft, openAutomationRule, openNewAutomationRule,
    handleTriggerChange, handleAutomationItemChange, updateVariant, appendDeliveryContent, handleSaveAutomationRule,
    handleDeleteAutomation, handleToggleAutomation, handleResolveRunIssue, handleResolveDeferredIssue, handleAddReplyRule,
    handleSaveReplyRule, handleDeleteReply, openDefaultReplyModal, handleSaveDefaultReply, handleDeleteDefaultReply,
    handleClearDefaultReplyRecords,
  };
};
