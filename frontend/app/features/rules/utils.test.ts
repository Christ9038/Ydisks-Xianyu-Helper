import { describe, expect, test } from 'vitest';
import type { AccountDetail, ShippingRule } from '../../../shared/api-contract';
import {
  accentClasses,
  accountLabel,
  actionSummary,
  boolFlag,
  buildReviewConfig,
  cardActionsForTrigger,
  defaultRuleName,
  emptyVariant,
  parseJSONObject,
  shouldReplaceGeneratedName,
  statusPill,
} from './utils';

// rule 是规则工具测试使用的最小规则对象。
const rule = (overrides: Partial<ShippingRule> = {}): ShippingRule => ({
  id: 'rule-1',
  name: '测试规则',
  trigger_type: 'order_paid',
  item_keyword: '',
  card_group_id: 0,
  priority: 1,
  enabled: true,
  actions: [],
  variants: [],
  ...overrides,
});

describe('规则工具函数', /* 当前回调处理规则配置和展示状态。 */ () => {
  test('安全解析规则配置并规范化求评价参数', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(parseJSONObject()).toEqual({});
    expect(parseJSONObject('{bad')).toEqual({});
    expect(parseJSONObject('[]')).toEqual({});
    expect(parseJSONObject('{"max_attempts": 3}')).toEqual({ max_attempts: 3 });
    expect(buildReviewConfig('{"after_shipped_hours": 48}')).toBe('{"after_shipped_hours":48,"repeat_interval_hours":24,"max_attempts":1}');
    expect(buildReviewConfig(undefined, { max_attempts: 4 })).toBe('{"after_shipped_hours":72,"repeat_interval_hours":24,"max_attempts":4}');
  });

  test('生成规则名称并识别可替换的系统名称', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(defaultRuleName('order_paid')).toBe('付款后自动发货');
    expect(defaultRuleName('buyer_reviewed', '数字商品')).toBe('评价后发送赠品 - 数字商品');
    expect(defaultRuleName('unknown' as never)).toBe('自动化规则');
    expect(shouldReplaceGeneratedName()).toBe(true);
    expect(shouldReplaceGeneratedName('  ')).toBe(true);
    expect(shouldReplaceGeneratedName('付款后自动发货')).toBe(true);
    expect(shouldReplaceGeneratedName('付款后自动发货 - 旧商品')).toBe(true);
    expect(shouldReplaceGeneratedName('我自定义的规则')).toBe(false);
  });

  test('创建三类触发器的默认动作链', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(cardActionsForTrigger('review_missing_timeout')).toEqual([{
      action_type: 'send_text', message_template: '亲，商品使用满意的话，麻烦给个评价，谢谢～', enabled: true, sort_order: 1,
    }]);
    expect(cardActionsForTrigger('order_paid', 7)).toEqual([
      { action_type: 'send_card', card_id: 7, delivery_count: 1, enabled: true, sort_order: 1 },
      { action_type: 'confirm_shipment', enabled: true, sort_order: 2 },
    ]);
    expect(cardActionsForTrigger('buyer_reviewed', 8)).toEqual([
      { action_type: 'send_card', card_id: 8, delivery_count: 1, enabled: true, sort_order: 1 },
    ]);
  });

  test('汇总动作、主题样式和布尔标志', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(actionSummary(rule({ trigger_type: 'review_missing_timeout', actions: [{ action_type: 'send_text', message_template: '请评价', enabled: true }] }))).toBe('请评价');
    expect(actionSummary(rule({ trigger_type: 'review_missing_timeout', actions: [] }))).toBe('发送求评价文案');
    expect(actionSummary(rule({ actions: [] }))).toBe('未配置卡密库存');
    expect(actionSummary(rule({ actions: [
      { action_type: 'send_card', card_id: 1, card_name: '库存一', enabled: true },
      { action_type: 'send_card', card_id: 2, enabled: true },
      { action_type: 'send_text', message_template: '忽略', enabled: true },
    ] }))).toBe('库存一 / 卡密 2');
    expect(accentClasses('blue', true)).toContain('border-blue-500');
    expect(accentClasses('amber')).toContain('hover:border-amber-300');
    expect(accentClasses('unknown' as never)).toContain('border-blue-100');
    expect(statusPill(true)).toContain('bg-emerald-100');
    expect(statusPill(false)).toContain('bg-gray-200');
    expect(boolFlag(true)).toBe(true);
    expect(boolFlag(1)).toBe(true);
    expect(boolFlag('1')).toBe(true);
    expect(boolFlag(false)).toBe(false);
    expect(boolFlag('true')).toBe(false);
  });

  test('创建空规格并选择账号展示名称', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(emptyVariant()).toEqual({ spec_name: '', spec_value: '', card_id: 0, delivery_count: 1, enabled: true, delay_override: false, delay_seconds: 0 });
    // idOnly 是仅包含平台账号标识的最小账号对象。
    const idOnly = { id: 'account-1' } as AccountDetail;
    expect(accountLabel({ id: 'a', nickname: '昵称' } as AccountDetail)).toBe('昵称');
    expect(accountLabel({ id: 'a', remark: '备注' } as AccountDetail)).toBe('备注');
    expect(accountLabel(idOnly)).toBe('account-1');
    expect(accountLabel()).toBe('未知账号');
  });
});
