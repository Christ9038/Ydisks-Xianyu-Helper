import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  AccountDetail,
  AutomationAction,
  AutomationTriggerType,
  Card,
  DefaultReply,
  Item,
  ReplyRule,
  ShippingRule,
  ShippingVariant,
} from '../types';
import {
  clearDefaultReplyRecords,
  deleteDefaultReply,
  deleteReplyRule,
  deleteShippingRule,
  getAccountDetails,
  getCards,
  getDefaultReplies,
  getDefaultReply,
  getItems,
  getReplyRules,
  getShippingRules,
  updateDefaultReply,
  updateReplyRule,
  updateShippingRule,
} from '../services/api';
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  Clock3,
  Edit,
  Gift,
  Layers3,
  MessageCircle,
  PackageCheck,
  Plus,
  RefreshCw,
  Save,
  Send,
  Trash2,
  X,
  Zap,
} from 'lucide-react';

type RulesTab = 'automation' | 'reply' | 'default';

interface RulesProps {
  initialDeliveryTarget?: {
    cookieId: string;
    itemId: string;
    requestId: number;
  };
  onDeliveryTargetHandled?: () => void;
}

interface DefaultReplyForm {
  cookie_id: string;
  enabled: boolean;
  reply_content: string;
  reply_once: boolean;
  reply_image_url: string;
}

interface TriggerMeta {
  label: string;
  shortLabel: string;
  description: string;
  flow: string[];
  accent: string;
  icon: React.ElementType;
}

const triggerMeta: Record<AutomationTriggerType, TriggerMeta> = {
  order_paid: {
    label: '付款后自动发货',
    shortLabel: '自动发货',
    description: '闲鱼付款系统卡片进入自动化中心后，先发送卡密，成功后再确认发货。',
    flow: ['付款系统卡片', '匹配商品/规格', '发送卡密', '确认发货'],
    accent: 'blue',
    icon: PackageCheck,
  },
  buyer_reviewed: {
    label: '评价后发送赠品',
    shortLabel: '评价赠品',
    description: '闲鱼评价系统卡片进入自动化中心后，给买家发送赠品卡密。',
    flow: ['评价系统卡片', '匹配商品/规格', '发送赠品'],
    accent: 'emerald',
    icon: Gift,
  },
  review_missing_timeout: {
    label: '超时未评价求评价',
    shortLabel: '求评价',
    description: '计划任务扫描已发货未评价订单，到期后发送求评价文案。',
    flow: ['计划任务扫描', '已发货未评价', '达到等待时间', '发送提醒'],
    accent: 'amber',
    icon: Clock3,
  },
};

const triggerOrder: AutomationTriggerType[] = ['order_paid', 'buyer_reviewed', 'review_missing_timeout'];
const reviewRequestText = '亲，商品使用满意的话，麻烦给个评价，谢谢～';

const emptyVariant = (): ShippingVariant => ({
  spec_name: '',
  spec_value: '',
  card_id: 0,
  delivery_count: 1,
  enabled: true,
});

const parseJSONObject = (raw?: string): Record<string, any> => {
  if (!raw) return {};
  try {
    const value = JSON.parse(raw);
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
  } catch {
    return {};
  }
};

const buildReviewConfig = (raw?: string, patch: Record<string, number> = {}) => {
  const current = parseJSONObject(raw);
  return JSON.stringify({
    after_shipped_hours: Number(current.after_shipped_hours || 72),
    max_attempts: Number(current.max_attempts || 1),
    ...patch,
  });
};

const defaultRuleName = (trigger: AutomationTriggerType, itemLabel?: string) => {
  const base = triggerMeta[trigger]?.label || '自动化规则';
  return itemLabel ? `${base} - ${itemLabel}` : base;
};

const shouldReplaceGeneratedName = (name?: string) => {
  const trimmed = (name || '').trim();
  if (!trimmed) return true;
  return Object.values(triggerMeta).some(meta => trimmed === meta.label || trimmed.startsWith(`${meta.label} -`));
};

const cardActionsForTrigger = (trigger: AutomationTriggerType, cardID = 0): AutomationAction[] => {
  if (trigger === 'review_missing_timeout') {
    return [{
      action_type: 'send_text',
      message_template: reviewRequestText,
      enabled: true,
      sort_order: 1,
    }];
  }

  const sendCard: AutomationAction = {
    action_type: 'send_card',
    card_id: cardID,
    delivery_count: 1,
    enabled: true,
    sort_order: 1,
  };

  if (trigger === 'order_paid') {
    return [
      sendCard,
      { action_type: 'confirm_shipment', enabled: true, sort_order: 2 },
    ];
  }
  return [sendCard];
};

const actionSummary = (rule: ShippingRule) => {
  if (rule.trigger_type === 'review_missing_timeout') {
    return rule.actions?.find(action => action.action_type === 'send_text')?.message_template || '发送求评价文案';
  }
  const cards = (rule.actions || []).filter(action => action.action_type === 'send_card');
  if (!cards.length) return '未配置卡密库存';
  return cards.map(action => action.card_name || `卡密 ${action.card_id}`).join(' / ');
};

const accentClasses = (accent: TriggerMeta['accent'], selected = false) => {
  const map: Record<string, string> = {
    blue: selected ? 'border-blue-500 bg-blue-50 text-blue-700' : 'border-blue-100 bg-blue-50/60 text-blue-700 hover:border-blue-300',
    emerald: selected ? 'border-emerald-500 bg-emerald-50 text-emerald-700' : 'border-emerald-100 bg-emerald-50/60 text-emerald-700 hover:border-emerald-300',
    amber: selected ? 'border-amber-500 bg-amber-50 text-amber-700' : 'border-amber-100 bg-amber-50/60 text-amber-700 hover:border-amber-300',
  };
  return map[accent] || map.blue;
};

const statusPill = (enabled: boolean) =>
  enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-200 text-gray-500';

const accountLabel = (account?: AccountDetail) => account?.nickname || account?.remark || account?.id || '未知账号';

const boolFlag = (value: unknown): boolean => value === true || value === 1 || value === '1';

const Rules: React.FC<RulesProps> = ({ initialDeliveryTarget, onDeliveryTargetHandled }) => {
  const [activeTab, setActiveTab] = useState<RulesTab>('automation');
  const [automationRules, setAutomationRules] = useState<ShippingRule[]>([]);
  const [replyRules, setReplyRules] = useState<ReplyRule[]>([]);
  const [defaultReplies, setDefaultReplies] = useState<Record<string, DefaultReply>>({});
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [cards, setCards] = useState<Card[]>([]);
  const [items, setItems] = useState<Item[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedAccountId, setSelectedAccountId] = useState('');

  const [showAutomationModal, setShowAutomationModal] = useState(false);
  const [showReplyModal, setShowReplyModal] = useState(false);
  const [showDefaultModal, setShowDefaultModal] = useState(false);
  const [editingAutomationRule, setEditingAutomationRule] = useState<Partial<ShippingRule> | null>(null);
  const [editingReplyRule, setEditingReplyRule] = useState<Partial<ReplyRule> | null>(null);
  const [defaultForm, setDefaultForm] = useState<DefaultReplyForm>({
    cookie_id: '',
    enabled: false,
    reply_content: '',
    reply_once: false,
    reply_image_url: '',
  });
  const selectedAccount = accounts.find(account => account.id === selectedAccountId);

  const loadReferenceData = useCallback(async () => {
    const [accountList, cardList, itemList, defaultReplyMap] = await Promise.all([
      getAccountDetails(),
      getCards(),
      getItems(),
      getDefaultReplies(),
    ]);
    setAccounts(accountList);
    setCards(cardList);
    setItems(itemList);
    setDefaultReplies(defaultReplyMap);
    setSelectedAccountId(current => current || accountList[0]?.id || '');
  }, []);

  const loadAutomationRules = useCallback(async () => {
    setAutomationRules(await getShippingRules());
  }, []);

  const loadReplyRules = useCallback(async () => {
    if (!selectedAccountId) {
      setReplyRules([]);
      return;
    }
    setReplyRules(await getReplyRules(selectedAccountId));
  }, [selectedAccountId]);

  const loadDefaultReplies = useCallback(async () => {
    setDefaultReplies(await getDefaultReplies());
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      if (activeTab === 'automation') {
        await loadAutomationRules();
      } else if (activeTab === 'reply') {
        await loadReplyRules();
      } else {
        await loadDefaultReplies();
      }
    } finally {
      setLoading(false);
    }
  }, [activeTab, loadAutomationRules, loadDefaultReplies, loadReplyRules]);

  useEffect(() => {
    void loadReferenceData();
  }, [loadReferenceData]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const visibleAutomationRules = useMemo(
    () => automationRules.filter(rule => !selectedAccountId || rule.cookie_id === selectedAccountId),
    [automationRules, selectedAccountId],
  );

  const visibleDefaultAccounts = useMemo(
    () => accounts.filter(account => !selectedAccountId || account.id === selectedAccountId),
    [accounts, selectedAccountId],
  );

  const rulesByTrigger = useMemo(() => {
    const grouped: Record<AutomationTriggerType, ShippingRule[]> = {
      order_paid: [],
      buyer_reviewed: [],
      review_missing_timeout: [],
    };
    for (const rule of visibleAutomationRules) {
      grouped[rule.trigger_type]?.push(rule);
    }
    return grouped;
  }, [visibleAutomationRules]);

  const modalAccountItems = useMemo(() => {
    const cookieID = editingAutomationRule?.cookie_id || selectedAccountId;
    return items.filter(item => item.cookie_id === cookieID);
  }, [editingAutomationRule?.cookie_id, items, selectedAccountId]);

  const selectedRuleItem = useMemo(() => {
    if (!editingAutomationRule?.cookie_id || !editingAutomationRule?.item_id) return undefined;
    return items.find(item => item.cookie_id === editingAutomationRule.cookie_id && item.item_id === editingAutomationRule.item_id);
  }, [editingAutomationRule?.cookie_id, editingAutomationRule?.item_id, items]);

  const isMultiSpecRule = boolFlag(selectedRuleItem?.is_multi_spec);
  const currentTrigger = (editingAutomationRule?.trigger_type || 'order_paid') as AutomationTriggerType;
  const currentMeta = triggerMeta[currentTrigger];
  const reviewConfig = parseJSONObject(editingAutomationRule?.config_json);

  const buildAutomationDraft = useCallback((
    trigger: AutomationTriggerType = 'order_paid',
    cookieID = selectedAccountId,
    itemID = '',
  ): Partial<ShippingRule> => {
    const item = items.find(candidate => candidate.cookie_id === cookieID && candidate.item_id === itemID);
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

  const openAutomationRule = useCallback((rule: ShippingRule) => {
    const trigger = (rule.trigger_type || 'order_paid') as AutomationTriggerType;
    setEditingAutomationRule({
      ...rule,
      trigger_type: trigger,
      config_json: trigger === 'review_missing_timeout' ? buildReviewConfig(rule.config_json) : (rule.config_json || '{}'),
      actions: rule.actions?.length ? rule.actions.map(action => ({ ...action })) : cardActionsForTrigger(trigger, rule.card_group_id),
      variants: rule.variants?.length ? rule.variants.map(variant => ({ ...variant })) : (trigger === 'review_missing_timeout' ? [] : [emptyVariant()]),
    });
    setShowAutomationModal(true);
  }, []);

  useEffect(() => {
    if (!initialDeliveryTarget) return;
    let cancelled = false;

    const openLinkedRule = async () => {
      setActiveTab('automation');
      setSelectedAccountId(initialDeliveryTarget.cookieId);
      setLoading(true);
      try {
        const [ruleList, itemList, cardList] = await Promise.all([
          getShippingRules(),
          getItems(),
          getCards().catch(error => {
            console.warn('加载卡密库存失败，不阻断打开自动化规则', error);
            return [];
          }),
        ]);
        if (cancelled) return;
        setAutomationRules(ruleList);
        setItems(itemList);
        setCards(cardList);
        const rule = ruleList.find(candidate =>
          candidate.cookie_id === initialDeliveryTarget.cookieId &&
          candidate.item_id === initialDeliveryTarget.itemId &&
          candidate.trigger_type === 'order_paid'
        );
        if (rule) {
          openAutomationRule(rule);
        } else {
          const item = itemList.find(candidate =>
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
      } catch (error) {
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
    return () => { cancelled = true; };
  }, [buildAutomationDraft, initialDeliveryTarget, onDeliveryTargetHandled, openAutomationRule]);

  const openNewAutomationRule = (trigger: AutomationTriggerType = 'order_paid') => {
    if (!selectedAccountId) {
      alert('请先选择账号');
      return;
    }
    setEditingAutomationRule(buildAutomationDraft(trigger));
    setShowAutomationModal(true);
  };

  const handleTriggerChange = (trigger: AutomationTriggerType) => {
    if (!editingAutomationRule) return;
    const currentCardID =
      editingAutomationRule.variants?.find(variant => variant.card_id)?.card_id ||
      editingAutomationRule.actions?.find(action => action.action_type === 'send_card')?.card_id ||
      editingAutomationRule.card_group_id ||
      0;
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

  const handleAutomationItemChange = (itemID: string) => {
    if (!editingAutomationRule) return;
    const item = items.find(candidate =>
      candidate.cookie_id === (editingAutomationRule.cookie_id || selectedAccountId) &&
      candidate.item_id === itemID
    );
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

  const displayVariants = editingAutomationRule?.variants?.length
    ? editingAutomationRule.variants
    : [emptyVariant()];

  const updateVariant = (index: number, patch: Partial<ShippingVariant>) => {
    if (!editingAutomationRule) return;
    const next = displayVariants.map((variant, variantIndex) =>
      variantIndex === index ? { ...variant, ...patch } : variant
    );
    setEditingAutomationRule({
      ...editingAutomationRule,
      variants: next,
      card_group_id: next[0]?.card_id || 0,
    });
  };

  const appendDeliveryContent = () => {
    if (!editingAutomationRule) return;
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

  const handleSaveAutomationRule = async () => {
    if (!editingAutomationRule) return;
    const trigger = (editingAutomationRule.trigger_type || 'order_paid') as AutomationTriggerType;
    if (!editingAutomationRule.cookie_id) {
      alert('请选择账号');
      return;
    }

    const variants = editingAutomationRule.variants?.length ? editingAutomationRule.variants : [];
    if (trigger !== 'review_missing_timeout') {
      if (!variants.length || variants.some(variant => !variant.card_id)) {
        alert(trigger === 'buyer_reviewed' ? '请选择评价赠品卡密库存' : '请选择发货卡密库存');
        return;
      }
      if (isMultiSpecRule && variants.some(variant => !variant.spec_name.trim() || !variant.spec_value.trim())) {
        alert('多规格商品必须填写每一行的规格名称和规格值');
        return;
      }
    }

    if (trigger === 'review_missing_timeout') {
      const text = editingAutomationRule.actions?.find(action => action.action_type === 'send_text')?.message_template || '';
      if (!text.trim()) {
        alert('请填写求评价文案');
        return;
      }
    }

    const saveVariants = trigger === 'review_missing_timeout'
      ? []
      : variants.map(variant => ({
        ...variant,
        spec_name: isMultiSpecRule ? variant.spec_name.trim() : '',
        spec_value: isMultiSpecRule ? variant.spec_value.trim() : '',
        delivery_count: Math.max(1, Number(variant.delivery_count) || 1),
        enabled: variant.enabled !== false,
      }));

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
    } catch (error) {
      console.error('保存自动化规则失败:', error);
      alert('保存失败：' + (error as Error).message);
    }
  };

  const handleDeleteAutomation = async (id: string) => {
    if (!confirm('确定删除该自动化规则吗？')) return;
    try {
      await deleteShippingRule(id);
      await loadAutomationRules();
      alert('删除成功');
    } catch (error) {
      alert('删除失败：' + (error as Error).message);
    }
  };

  const handleToggleAutomation = async (rule: ShippingRule) => {
    try {
      await updateShippingRule({ ...rule, enabled: !rule.enabled });
      await loadAutomationRules();
    } catch (error) {
      alert('操作失败：' + (error as Error).message);
    }
  };

  const handleAddReplyRule = () => {
    if (!selectedAccountId) {
      alert('请先选择账号');
      return;
    }
    setEditingReplyRule({
      keyword: '',
      reply_content: '',
      match_type: 'fuzzy',
      enabled: true,
    });
    setShowReplyModal(true);
  };

  const handleSaveReplyRule = async () => {
    if (!editingReplyRule || !selectedAccountId) return;
    const hasReplyContent = Boolean(editingReplyRule.reply_content?.trim() || editingReplyRule.image_url?.trim());
    if (!editingReplyRule.keyword?.trim() || !hasReplyContent) {
      alert('请填写关键词和回复内容');
      return;
    }
    try {
      await updateReplyRule({ ...editingReplyRule, match_type: 'fuzzy', enabled: true }, selectedAccountId);
      setShowReplyModal(false);
      await loadReplyRules();
      alert('保存成功');
    } catch (error) {
      alert('保存失败：' + (error as Error).message);
    }
  };

  const handleDeleteReply = async (id: string) => {
    if (!selectedAccountId || !confirm('确定删除该回复规则吗？')) return;
    try {
      await deleteReplyRule(id, selectedAccountId);
      await loadReplyRules();
      alert('删除成功');
    } catch (error) {
      alert('删除失败：' + (error as Error).message);
    }
  };

  const openDefaultReplyModal = async (cookieID = selectedAccountId) => {
    if (!cookieID) {
      alert('请先选择账号');
      return;
    }
    try {
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

  const handleSaveDefaultReply = async () => {
    if (!defaultForm.cookie_id) {
      alert('请先选择账号');
      return;
    }
    if (defaultForm.enabled && !defaultForm.reply_content.trim() && !defaultForm.reply_image_url.trim()) {
      alert('启用默认回复时，请填写回复内容或图片 URL');
      return;
    }
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
    } catch (error) {
      alert('保存失败：' + (error as Error).message);
    }
  };

  const handleDeleteDefaultReply = async (cookieID: string) => {
    if (!confirm('确定删除该账号默认回复吗？')) return;
    try {
      await deleteDefaultReply(cookieID);
      await loadDefaultReplies();
      alert('删除成功');
    } catch (error) {
      alert('删除失败：' + (error as Error).message);
    }
  };

  const handleClearDefaultReplyRecords = async (cookieID: string) => {
    if (!confirm('确定清空该账号的默认回复记录吗？清空后可重新对所有会话使用“只回复一次”。')) return;
    try {
      await clearDefaultReplyRecords(cookieID);
      alert('清空成功');
    } catch (error) {
      alert('清空失败：' + (error as Error).message);
    }
  };

  const primaryActionLabel = activeTab === 'automation'
    ? '新建自动化'
    : activeTab === 'reply'
      ? '新增关键词'
      : '编辑默认回复';

  return (
    <div className="space-y-8 animate-fade-in">
      <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">自动化规则</h2>
          <p className="text-gray-500 mt-2 font-medium">系统通知卡片只进入自动化判断；买家消息进入关键词、默认或 AI 回复。</p>
        </div>
        <div className="flex flex-col sm:flex-row gap-3">
          <select
            value={selectedAccountId}
            onChange={event => setSelectedAccountId(event.target.value)}
            className="ios-input px-4 py-3 rounded-2xl text-sm min-w-64"
          >
            <option value="">全部账号</option>
            {accounts.map(account => (
              <option key={account.id} value={account.id}>{accountLabel(account)}</option>
            ))}
          </select>
          <button
            onClick={refresh}
            className="px-4 py-3 rounded-2xl font-bold bg-gray-100 hover:bg-gray-200 text-gray-700 flex items-center justify-center gap-2 transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </button>
          <button
            onClick={activeTab === 'automation' ? () => openNewAutomationRule('order_paid') : activeTab === 'reply' ? handleAddReplyRule : () => void openDefaultReplyModal()}
            disabled={!selectedAccountId}
            className="ios-btn-primary px-5 py-3 rounded-2xl text-sm font-extrabold flex items-center justify-center gap-2 disabled:opacity-50"
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
        ].map(tab => {
          const Icon = tab.icon;
          return (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`inline-flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-bold transition-all ${
                activeTab === tab.id
                  ? 'bg-[#0094f7] text-white shadow-md'
                  : 'bg-white text-gray-600 hover:text-black hover:bg-gray-50'
              }`}
            >
              <Icon className="w-4 h-4" />
              {tab.label}
            </button>
          );
        })}
      </div>

      {activeTab === 'automation' && (
        <div className="grid grid-cols-1 2xl:grid-cols-[360px_1fr] gap-6">
          <aside className="space-y-4">
            <div className="bg-white rounded-xl p-5 border border-gray-100 shadow-sm">
              <h3 className="font-black text-gray-900 mb-1">新建规则</h3>
              <p className="text-sm text-gray-500 mb-4">先选自动化类型，再配置对应动作。</p>
              <div className="space-y-3">
                {triggerOrder.map(trigger => {
                  const meta = triggerMeta[trigger];
                  const Icon = meta.icon;
                  return (
                    <button
                      key={trigger}
                      type="button"
                      onClick={() => openNewAutomationRule(trigger)}
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
              <h3 className="font-black text-gray-900 mb-4">规则概览</h3>
              <div className="space-y-3">
                {triggerOrder.map(trigger => {
                  const meta = triggerMeta[trigger];
                  const Icon = meta.icon;
                  return (
                    <div key={trigger} className="flex items-center justify-between rounded-2xl bg-gray-50 p-3">
                      <div className="flex items-center gap-3">
                        <Icon className="w-4 h-4 text-gray-500" />
                        <span className="text-sm font-bold text-gray-700">{meta.shortLabel}</span>
                      </div>
                      <span className="text-sm font-black text-gray-900">{rulesByTrigger[trigger].length}</span>
                    </div>
                  );
                })}
              </div>
            </div>
          </aside>

          <section className="space-y-4">
            {visibleAutomationRules.length === 0 ? (
              <div className="bg-white rounded-xl border border-dashed border-gray-200 p-16 text-center">
                <Zap className="w-12 h-12 text-gray-300 mx-auto mb-4" />
                <h3 className="text-xl font-black text-gray-900">还没有自动化规则</h3>
                <p className="text-gray-500 mt-2">从左侧选择一个模板开始配置。</p>
              </div>
            ) : (
              visibleAutomationRules.map(rule => {
                const meta = triggerMeta[rule.trigger_type];
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
                          onClick={() => openAutomationRule(rule)}
                          className="px-4 py-2 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-700 text-sm font-bold flex items-center gap-2"
                        >
                          <Edit className="w-4 h-4" />
                          编辑
                        </button>
                        <button
                          onClick={() => handleToggleAutomation(rule)}
                          className={`px-4 py-2 rounded-xl text-sm font-bold ${rule.enabled ? 'bg-amber-50 text-amber-700 hover:bg-amber-100' : 'bg-emerald-50 text-emerald-700 hover:bg-emerald-100'}`}
                        >
                          {rule.enabled ? '禁用' : '启用'}
                        </button>
                        <button
                          onClick={() => handleDeleteAutomation(rule.id)}
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
            {replyRules.map(rule => (
              <div key={rule.id} className="flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border border-gray-100 bg-[#F7F8FA] hover:bg-white hover:shadow-lg transition-all gap-4">
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
                    onClick={() => {
                      setEditingReplyRule({ ...rule });
                      setShowReplyModal(true);
                    }}
                    className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                    title="编辑"
                  >
                    <Edit className="w-4 h-4" />
                  </button>
                  <button onClick={() => handleDeleteReply(rule.id)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors" title="删除">
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
            {visibleDefaultAccounts.map(account => {
              const defaultReply = defaultReplies[account.id];
              const enabled = Boolean(defaultReply?.enabled);
              return (
                <div key={account.id} className={`flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border transition-all gap-4 ${enabled ? 'border-purple-100 bg-purple-50/50 hover:bg-white hover:shadow-lg' : 'border-gray-100 bg-[#F7F8FA] hover:bg-white hover:shadow-lg'}`}>
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
                      onClick={() => void openDefaultReplyModal(account.id)}
                      className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                      title="编辑"
                    >
                      <Edit className="w-4 h-4" />
                    </button>
                    {enabled && (
                      <>
                        <button
                          onClick={() => void handleClearDefaultReplyRecords(account.id)}
                          className="px-3 py-2 text-xs font-bold text-blue-600 hover:bg-blue-50 rounded-xl transition-colors"
                        >
                          清空记录
                        </button>
                        <button onClick={() => void handleDeleteDefaultReply(account.id)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors" title="删除">
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
                onClick={() => setShowAutomationModal(false)}
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
                  {triggerOrder.map(trigger => {
                    const meta = triggerMeta[trigger];
                    const Icon = meta.icon;
                    const selected = currentTrigger === trigger;
                    return (
                      <button
                        key={trigger}
                        type="button"
                        onClick={() => handleTriggerChange(trigger)}
                        className={`w-full rounded-2xl p-4 text-left border transition-all ${
                          selected ? 'bg-white text-slate-950 border-white' : 'bg-white/5 text-white border-white/10 hover:bg-white/10'
                        }`}
                      >
                        <div className="flex items-start gap-3">
                          <Icon className={`w-5 h-5 mt-0.5 ${selected ? 'text-[#0094f7]' : 'text-white'}`} />
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
                    {currentMeta.flow.map((step, index) => (
                      <div key={step} className="flex items-center gap-3">
                        <div className="w-6 h-6 rounded-full bg-white text-slate-950 text-xs font-black flex items-center justify-center">{index + 1}</div>
                        <span className="text-sm font-bold text-gray-100">{step}</span>
                      </div>
                    ))}
                  </div>
                </div>
              </aside>

              <div className="p-6 overflow-y-auto bg-[#F7F8FA]">
                <div className="space-y-5">
                  <section className="bg-white rounded-3xl border border-gray-100 p-5">
                    <div className="flex items-center gap-2 mb-4">
                      <CheckCircle2 className="w-5 h-5 text-[#0094f7]" />
                      <h4 className="font-black text-gray-900">生效范围</h4>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      <div className="md:col-span-2">
                        <label className="block text-sm font-bold text-gray-700 mb-2">规则名称</label>
                        <input
                          type="text"
                          value={editingAutomationRule.name || ''}
                          onChange={event => setEditingAutomationRule({ ...editingAutomationRule, name: event.target.value })}
                          placeholder="不填时按类型和商品自动生成"
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">闲鱼账号</label>
                        <select
                          value={editingAutomationRule.cookie_id || ''}
                          onChange={event => setEditingAutomationRule({
                            ...editingAutomationRule,
                            cookie_id: event.target.value,
                            item_id: '',
                            item_title: '',
                            item_keyword: '',
                          })}
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        >
                          <option value="">选择账号</option>
                          {accounts.map(account => (
                            <option key={account.id} value={account.id}>{accountLabel(account)}</option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">关联商品</label>
                        <select
                          value={editingAutomationRule.item_id || ''}
                          onChange={event => handleAutomationItemChange(event.target.value)}
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        >
                          <option value="">账号级规则（不限定商品）</option>
                          {modalAccountItems.map(item => (
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
                            <Layers3 className="w-5 h-5 text-[#0094f7]" />
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
                        {displayVariants.map((variant, index) => (
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
                                    onChange={event => updateVariant(index, { spec_name: event.target.value })}
                                    className="w-full ios-input px-3 py-2.5 rounded-lg"
                                    placeholder="例如：套餐"
                                  />
                                </div>
                                <div>
                                  <label className="block text-xs font-bold text-gray-600 mb-2">规格值</label>
                                  <input
                                    value={variant.spec_value}
                                    onChange={event => updateVariant(index, { spec_value: event.target.value })}
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
                                onChange={event => updateVariant(index, { card_id: Number(event.target.value) })}
                                className="w-full ios-input px-3 py-2.5 rounded-lg"
                              >
                                <option value="">请选择卡密库存</option>
                                {cards.filter(card => card.enabled).map(card => (
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
                                onChange={event => updateVariant(index, { delivery_count: Math.max(1, Number(event.target.value) || 1) })}
                                className="w-full ios-input px-3 py-2.5 rounded-lg"
                              />
                            </div>
                            <button
                              type="button"
                              disabled={displayVariants.length === 1}
                              onClick={() => setEditingAutomationRule({
                                ...editingAutomationRule,
                                variants: displayVariants.filter((_, variantIndex) => variantIndex !== index),
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
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                          <label className="block text-sm font-bold text-gray-700 mb-2">发货后等待小时</label>
                          <input
                            type="number"
                            min="1"
                            value={Number(reviewConfig.after_shipped_hours || 72)}
                            onChange={event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              config_json: buildReviewConfig(editingAutomationRule.config_json, {
                                after_shipped_hours: Math.max(1, Number(event.target.value) || 72),
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
                            onChange={event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              config_json: buildReviewConfig(editingAutomationRule.config_json, {
                                max_attempts: Math.max(1, Number(event.target.value) || 1),
                              }),
                            })}
                            className="w-full ios-input px-4 py-3 rounded-xl"
                          />
                        </div>
                        <div className="md:col-span-2">
                          <label className="block text-sm font-bold text-gray-700 mb-2">求评价文案</label>
                          <textarea
                            value={editingAutomationRule.actions?.find(action => action.action_type === 'send_text')?.message_template || ''}
                            onChange={event => setEditingAutomationRule({
                              ...editingAutomationRule,
                              actions: (editingAutomationRule.actions?.length ? editingAutomationRule.actions : cardActionsForTrigger('review_missing_timeout')).map(action =>
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
                        <input
                          type="number"
                          value={editingAutomationRule.priority || 100}
                          onChange={event => setEditingAutomationRule({ ...editingAutomationRule, priority: Number(event.target.value) || 100 })}
                          min="1"
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        />
                      </div>
                      <label className="h-[48px] flex items-center gap-3 px-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
                        <input
                          type="checkbox"
                          checked={editingAutomationRule.enabled !== false}
                          onChange={event => setEditingAutomationRule({ ...editingAutomationRule, enabled: event.target.checked })}
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
              <button onClick={() => setShowAutomationModal(false)} className="flex-1 px-6 py-3 rounded-2xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200">
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
                  onClick={() => setShowReplyModal(false)}
                  className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors"
                >
                  <X className="w-5 h-5 text-gray-600" />
                </button>
              </div>
            </div>

            <div className="modal-body space-y-5">
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">关键词</label>
                <input
                  type="text"
                  value={editingReplyRule.keyword || ''}
                  onChange={event => setEditingReplyRule({ ...editingReplyRule, keyword: event.target.value })}
                  placeholder="买家发送的关键词"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
                <textarea
                  value={editingReplyRule.reply_content || ''}
                  onChange={event => setEditingReplyRule({ ...editingReplyRule, reply_content: event.target.value })}
                  placeholder="自动回复的内容"
                  className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
                />
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  onClick={() => setShowReplyModal(false)}
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
                  onClick={() => setShowDefaultModal(false)}
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
                  onChange={event => setDefaultForm({ ...defaultForm, cookie_id: event.target.value })}
                  className="w-full ios-input px-4 py-3 rounded-xl"
                >
                  <option value="">选择账号</option>
                  {accounts.map(account => (
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
                  onClick={() => setDefaultForm({ ...defaultForm, enabled: !defaultForm.enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${defaultForm.enabled ? 'bg-[#0094f7]' : 'bg-gray-300'}`}
                >
                  <span className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 block ${defaultForm.enabled ? 'translate-x-7' : 'translate-x-1'}`} />
                </button>
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
                <textarea
                  value={defaultForm.reply_content}
                  onChange={event => setDefaultForm({ ...defaultForm, reply_content: event.target.value })}
                  placeholder="输入默认回复内容"
                  className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复图片 URL（可选）</label>
                <input
                  type="text"
                  value={defaultForm.reply_image_url}
                  onChange={event => setDefaultForm({ ...defaultForm, reply_image_url: event.target.value })}
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
                  onChange={event => setDefaultForm({ ...defaultForm, reply_once: event.target.checked })}
                  className="w-4 h-4 rounded"
                />
              </label>

              <div className="flex gap-3 pt-4">
                <button
                  onClick={() => setShowDefaultModal(false)}
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
