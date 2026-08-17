import { Clock3, Gift, PackageCheck } from 'lucide-react';
import type {
  AccountDetail,
  AutomationAction,
  AutomationTriggerType,
  ShippingRule,
  ShippingVariant,
} from '../../../shared/api-contract';
import type { TriggerMeta } from './types';

// triggerMeta 是自动化触发类型的统一展示元数据，供列表和编辑器复用。
export const triggerMeta: Record<AutomationTriggerType, TriggerMeta> = {
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

// triggerOrder 固定自动化类型在创建面板和筛选器中的排序。
export const triggerOrder: AutomationTriggerType[] = ['order_paid', 'buyer_reviewed', 'review_missing_timeout'];

// reviewRequestText 是超时未评价规则的默认提醒文案。
export const reviewRequestText = '亲，商品使用满意的话，麻烦给个评价，谢谢～';

// emptyVariant 创建一个未选择卡密库存的发货规格草稿。
export const emptyVariant = (): ShippingVariant => ({
  spec_name: '',
  spec_value: '',
  card_id: 0,
  delivery_count: 1,
  enabled: true,
  delay_override: false,
  delay_seconds: 0,
});

// parseJSONObject 安全解析规则配置 JSON，异常或非对象值统一返回空对象。
export const parseJSONObject = (raw?: string): Record<string, any> => {
  if (!raw) return {};
  try {
    // value 是解析后的 JSON 值，后续会校验其对象形态。
    const value = JSON.parse(raw);
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
  } catch {
    return {};
  }
};

// buildReviewConfig 规范化求评价规则的时间参数，并应用编辑器的局部修改。
export const buildReviewConfig = (raw?: string, patch: Record<string, number> = {}) => {
  // current 是求评价配置的已有字段。
  const current = parseJSONObject(raw);
  return JSON.stringify({
    after_shipped_hours: Number(current.after_shipped_hours || 72),
    repeat_interval_hours: Number(current.repeat_interval_hours || 24),
    max_attempts: Number(current.max_attempts || 1),
    ...patch,
  });
};

// defaultRuleName 根据触发类型和商品标签生成规则默认名称。
export const defaultRuleName = (trigger: AutomationTriggerType, itemLabel?: string) => {
  // base 是触发类型对应的默认名称。
  const base = triggerMeta[trigger]?.label || '自动化规则';
  return itemLabel ? `${base} - ${itemLabel}` : base;
};

// shouldReplaceGeneratedName 判断当前名称是否仍是系统生成的默认名称。
export const shouldReplaceGeneratedName = (name?: string) => {
  // trimmed 是去掉首尾空白后的规则名称。
  const trimmed = (name || '').trim();
  if (!trimmed) return true;
  return Object.values(triggerMeta).some(
    // 元数据匹配器判断名称是否仍由系统自动生成。
    meta => trimmed === meta.label || trimmed.startsWith(`${meta.label} -`),
  );
};

// cardActionsForTrigger 根据触发类型创建默认动作链。
export const cardActionsForTrigger = (trigger: AutomationTriggerType, cardID = 0): AutomationAction[] => {
  if (trigger === 'review_missing_timeout') {
    return [{
      action_type: 'send_text',
      message_template: reviewRequestText,
      enabled: true,
      sort_order: 1,
    }];
  }

  // sendCard 是卡密发送动作的默认配置。
  const sendCard: AutomationAction = {
    action_type: 'send_card',
    card_id: cardID,
    delivery_count: 1,
    enabled: true,
    sort_order: 1,
  };

  if (trigger === 'order_paid') {
    return [sendCard, { action_type: 'confirm_shipment', enabled: true, sort_order: 2 }];
  }
  return [sendCard];
};

// actionSummary 将规则动作转换成列表中的简短摘要。
export const actionSummary = (rule: ShippingRule) => {
  if (rule.trigger_type === 'review_missing_timeout') {
    return rule.actions?.find(
      // 文案动作匹配器只查找发送文字动作。
      action => action.action_type === 'send_text',
    )?.message_template || '发送求评价文案';
  }
  // cards 是当前规则中的卡密动作列表。
  const cards = (rule.actions || []).filter(
    // 卡密动作筛选器只保留发送卡片类型。
    action => action.action_type === 'send_card',
  );
  if (!cards.length) return '未配置卡密库存';
  return cards.map(
    // 卡密动作格式化器优先展示库存名称。
    action => action.card_name || `卡密 ${action.card_id}`,
  ).join(' / ');
};

// accentClasses 将触发类型主题色映射为规则页 Tailwind 类名。
export const accentClasses = (accent: TriggerMeta['accent'], selected = false) => {
  // map 保存每种主题色在选中和未选中状态下的样式。
  const map: Record<string, string> = {
    blue: selected ? 'border-blue-500 bg-blue-50 text-blue-700' : 'border-blue-100 bg-blue-50/60 text-blue-700 hover:border-blue-300',
    emerald: selected ? 'border-emerald-500 bg-emerald-50 text-emerald-700' : 'border-emerald-100 bg-emerald-50/60 text-emerald-700 hover:border-emerald-300',
    amber: selected ? 'border-amber-500 bg-amber-50 text-amber-700' : 'border-amber-100 bg-amber-50/60 text-amber-700 hover:border-amber-300',
  };
  return map[accent] || map.blue;
};

// statusPill 返回自动化规则启用状态对应的标签样式。
export const statusPill = (enabled: boolean) => enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-200 text-gray-500';

// accountLabel 选择账号在规则页展示时最有意义的名称。
export const accountLabel = (account?: AccountDetail) => account?.nickname || account?.remark || account?.id || '未知账号';

// boolFlag 兼容后端可能返回的布尔、数字和字符串标志值。
export const boolFlag = (value: unknown): boolean => value === true || value === 1 || value === '1';
