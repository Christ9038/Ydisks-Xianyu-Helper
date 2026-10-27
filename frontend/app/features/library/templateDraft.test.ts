import { describe, expect, test } from 'vitest';
import { actionsInScope, draftFromTemplate, emptyDraft, isActionComplete, newAction, pruneAutomation, reconcileDraftSpecs, specScopes, syncTemplateBindings, toRequest, validateDraft, type TemplateDraft } from './templateDraft';
import { reconcilePublishSkuRows, type PublishSpec } from '../../../shared/ui/publish/publishSpecs';
import type { ProductTemplate } from './types';

// colorSpec 是带两个规格值的颜色规格，规格图来自图片库。
const colorSpec: PublishSpec = { specId: 's1', name: '颜色', supportImage: true, values: [{ valueId: 'v1', value: '红' }, { valueId: 'v2', value: '蓝' }] };
// sizeSpec 是第二个规格维度。
const sizeSpec: PublishSpec = { specId: 's2', name: '尺码', supportImage: false, values: [{ valueId: 'v3', value: 'S' }, { valueId: 'v4', value: 'M' }] };

/** multiDraft 构造一份合法的双规格草稿。 */
const multiDraft = (): TemplateDraft => {
  // specs 是双维度规格。
  const specs = [colorSpec, sizeSpec];
  // skuRows 为每个组合填入价格库存。
  const skuRows = reconcilePublishSkuRows(specs).map(/* row 填入价格库存。 */ row => ({ ...row, price: '9.90', quantity: '2' }));
  return { ...emptyDraft(), name: '模板', title: '标题', imageIds: [11, 12], specs, skuRows, specImageIds: { v1: 21, v2: 21 } };
};

describe('templateDraft', /* 草稿转换与校验的纯逻辑测试。 */ () => {
  test('单规格请求不携带规格并保留金额文本', /* 单规格路径。 */ () => {
    // body 是单规格草稿转换结果。
    const body = toRequest({ ...emptyDraft(), name: ' 模板 ', title: '标题', price: '12.5', quantity: '3', imageIds: [1] });
    expect(body.name).toBe('模板');
    expect(body.price).toBe('12.5');
    expect(body.quantity).toBe(3);
    expect(body.specs).toEqual({ specs: [], skus: [], spec_image_ids: [] });
  });

  test('多规格请求汇总库存、去重规格图并用 -1 表示无图', /* 多规格路径。 */ () => {
    // body 是双规格草稿转换结果。
    const body = toRequest(multiDraft());
    expect(body.quantity).toBe(8);
    expect(body.specs.spec_image_ids).toEqual([21]);
    expect(body.specs.specs[0].values.map(/* value 取图片下标。 */ value => value.image_index)).toEqual([0, 0]);
    expect(body.specs.specs[1].values.map(/* value 取图片下标。 */ value => value.image_index)).toEqual([-1, -1]);
    expect(body.specs.skus[0].property_list).toEqual([{ property_text: '颜色', value_text: '红' }, { property_text: '尺码', value_text: 'S' }]);
  });

  test('规格范围使用中文分号拼接，单规格为空范围', /* 范围推导。 */ () => {
    expect(specScopes(emptyDraft())).toEqual([{ specName: '', specValue: '', label: '全部订单' }]);
    // scopes 是双规格的四个组合。
    const scopes = specScopes(multiDraft());
    expect(scopes).toHaveLength(4);
    expect(scopes[0]).toMatchObject({ specName: '颜色；尺码', specValue: '红；S' });
  });

  test('付款发货必须覆盖每个规格组合', /* 校验覆盖。 */ () => {
    // draft 启用付款发货但只配置第一个组合。
    const draft = multiDraft();
    draft.automation.paidDelivery = { enabled: true, actions: [{ ...newAction('send_card', specScopes(draft)[0]), cardId: 5 }] };
    expect(validateDraft(draft)).toContain('每个规格组合');
    // complete 为所有组合补齐动作。
    draft.automation.paidDelivery.actions = specScopes(draft).map(/* scope 为每个组合配置发卡。 */ scope => ({ ...newAction('send_card', scope), cardId: 5 }));
    expect(validateDraft(draft)).toBe('');
  });

  test('基础校验覆盖名称、主图、售价、库存与邮费', /* 校验分支。 */ () => {
    expect(validateDraft(emptyDraft())).toContain('模板名称');
    expect(validateDraft({ ...emptyDraft(), name: 'a' })).toContain('标题');
    expect(validateDraft({ ...emptyDraft(), name: 'a', title: 'b' })).toContain('主图');
    // base 是缺少售价的单规格草稿。
    const base = { ...emptyDraft(), name: 'a', title: 'b', imageIds: [1] };
    expect(validateDraft(base)).toContain('售价');
    expect(validateDraft({ ...base, price: '1', quantity: '0' })).toContain('库存');
    expect(validateDraft({ ...base, price: '1', postageMode: 'fixed', postage: '' })).toContain('邮费');
    expect(validateDraft({ ...base, price: '1' })).toBe('');
    expect(validateDraft({ ...base, price: '1', postageMode: 'none' })).toBe('');
    expect(toRequest({ ...base, price: '1', postageMode: 'none', postage: '9' })).toMatchObject({ postage_mode: 'none', postage: '0' });
  });

  test('规格变化后清理失效规格图与自动化范围', /* 规格对账。 */ () => {
    // draft 已为 4 个组合配置动作。
    const draft = multiDraft();
    draft.automation.paidDelivery = { enabled: true, actions: specScopes(draft).map(/* scope 配置发卡。 */ scope => ({ ...newAction('send_card', scope), cardId: 5 })) };
    // reduced 删除“蓝”后只剩 2 个组合。
    const specs = [{ ...colorSpec, values: [colorSpec.values[0], { valueId: 'v9', value: '' }] }, sizeSpec];
    // next 是规格对账后的草稿。
    const next = reconcileDraftSpecs(draft, specs, reconcilePublishSkuRows(specs, draft.skuRows));
    expect(next.skuRows).toHaveLength(2);
    expect(next.specImageIds).toEqual({ v1: 21 });
    expect(next.automation.paidDelivery.actions).toHaveLength(2);
    expect(pruneAutomation(next.automation, [{ specName: '', specValue: '', label: '' }]).paidDelivery.actions).toHaveLength(0);
  });

  test('发货模板动作按变量重建绑定并校验完整性', /* 模板动作。 */ () => {
    // scope 是单规格范围。
    const scope = specScopes(emptyDraft())[0];
    // action 选择了带一个卡密变量和一个自定义变量的模板。
    const action = syncTemplateBindings(newAction('send_template', scope), { id: 7, name: 'T', keys: ['main'], customKeys: ['note'] });
    expect(action.templateBindings).toEqual([{ variableKey: 'main', cardId: 0, deliveryCount: 1 }]);
    expect(isActionComplete(action)).toBe(false);
    // filled 补齐绑定与自定义变量。
    const filled = { ...action, templateBindings: [{ variableKey: 'main', cardId: 3, deliveryCount: 1 }], customVariables: { note: 'x' } };
    expect(isActionComplete(filled)).toBe(true);
    expect(syncTemplateBindings(filled, undefined).deliveryTemplateId).toBe(0);
    expect(actionsInScope({ enabled: true, actions: [filled] }, scope)).toHaveLength(1);
    expect(isActionComplete({ ...newAction('send_card', scope), cardId: 2 })).toBe(true);
  });

  test('已保存模板还原为草稿并保留规格价格与规格图', /* 往返。 */ () => {
    // template 是服务端返回的双规格模板。
    const template: ProductTemplate = {
      id: 1, name: 'n', title: 't', description: '', price: '9.90', originalPrice: '', quantity: 8, postageMode: 'free', postage: '0.00',
      category: { catId: '1', catName: 'c', channelCatId: '', tbCatId: '' }, imageIds: [11],
      specs: [{ specId: 'a', name: '颜色', supportImage: true, values: [{ valueId: 'x', value: '红', imageId: 21 }, { valueId: 'y', value: '蓝', imageId: 0 }] }],
      skus: [{ valueLabels: ['红'], price: '5.00', quantity: '3' }, { valueLabels: ['蓝'], price: '6.00', quantity: '4' }],
      automation: emptyDraft().automation, updatedAt: '',
    };
    // draft 是还原结果。
    const draft = draftFromTemplate(template);
    expect(draft.specImageIds).toEqual({ x: 21 });
    expect(draft.skuRows.map(/* row 取价格库存。 */ row => [row.price, row.quantity])).toEqual([['5.00', '3'], ['6.00', '4']]);
    expect(draft.price).toBe('');
  });
});
