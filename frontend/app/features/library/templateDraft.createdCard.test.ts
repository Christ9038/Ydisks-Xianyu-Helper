import { describe, expect, test } from 'vitest';
import { applyCreatedCard, emptyAutomation, newAction, specScopes, emptyDraft } from './templateDraft';
import type { LibraryAutomation } from './types';

// scope 是单规格商品唯一的规格范围。
const scope = specScopes(emptyDraft())[0];

// automationWith 构造付款发货含一个指定动作的自动化配置。
const automationWith = (action: ReturnType<typeof newAction>): LibraryAutomation => ({ ...emptyAutomation(), paidDelivery: { enabled: true, actions: [action] } });

describe('applyCreatedCard 回填新建卡密', /* 覆盖发卡动作、模板变量绑定与目标失效。 */ () => {
  test('回填发卡动作的卡密组', /* 发卡动作路径。 */ () => {
    // next 是回填后的自动化配置。
    const next = applyCreatedCard(automationWith(newAction('send_card', scope)), { group: 'paidDelivery', index: 0 }, 7);
    expect(next.paidDelivery.actions[0].cardId).toBe(7);
  });

  test('回填评价赠品组的动作', /* 另一个动作组。 */ () => {
    // base 是评价赠品含一个发卡动作的配置。
    const base: LibraryAutomation = { ...emptyAutomation(), reviewGift: { enabled: true, actions: [newAction('send_card', scope)] } };
    expect(applyCreatedCard(base, { group: 'reviewGift', index: 0 }, 3).reviewGift.actions[0].cardId).toBe(3);
  });

  test('回填发货模板指定变量的绑定，不影响其他变量', /* 绑定路径。 */ () => {
    // action 是带两个变量绑定的模板动作。
    const action = { ...newAction('send_template', scope), templateBindings: [{ variableKey: 'a', cardId: 0, deliveryCount: 1 }, { variableKey: 'b', cardId: 2, deliveryCount: 1 }] };
    // next 是回填变量 a 后的配置。
    const next = applyCreatedCard(automationWith(action), { group: 'paidDelivery', index: 0, variableKey: 'a' }, 9);
    expect(next.paidDelivery.actions[0].templateBindings.map(/* binding 提取卡密组。 */ binding => binding.cardId)).toEqual([9, 2]);
  });

  test('目标动作已删除或类型、变量不符时原样返回', /* 避免误写。 */ () => {
    // base 是只有一个发卡动作的配置。
    const base = automationWith(newAction('send_card', scope));
    expect(applyCreatedCard(base, { group: 'paidDelivery', index: 5 }, 7)).toBe(base);
    expect(applyCreatedCard(base, { group: 'paidDelivery', index: 0, variableKey: 'a' }, 7)).toBe(base);
    // template 是没有变量绑定的模板动作。
    const template = automationWith(newAction('send_template', scope));
    expect(applyCreatedCard(template, { group: 'paidDelivery', index: 0 }, 7)).toBe(template);
    expect(applyCreatedCard(template, { group: 'paidDelivery', index: 0, variableKey: 'zzz' }, 7)).toBe(template);
  });
});
