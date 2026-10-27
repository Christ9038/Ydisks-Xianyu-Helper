import { describe, expect, test } from 'vitest';
import { defaultTemplateFilters, isTemplateFilterActive, queryTemplates, templateEffectivePrice, templateTotalStock } from './templateQuery';
import type { ProductTemplate } from './types';

/** makeTemplate 构造测试模板，只覆盖关心的字段；paid 控制付款发货是否启用。 */
const makeTemplate = (id: number, patch: Partial<ProductTemplate>, paid = false): ProductTemplate => ({
  id, name: `模板${id}`, title: `标题${id}`, description: '', price: '10', originalPrice: '', quantity: 1, postageMode: 'free', postage: '',
  category: { catId: '1', catName: '电子资料', channelCatId: '', tbCatId: '' }, imageIds: [], specs: [], skus: [],
  automation: { paidDelivery: { enabled: paid, actions: [] }, reviewGift: { enabled: false, actions: [] }, reviewRequest: { enabled: false, afterShippedHours: 0, message: '', maxAttempts: 0, delaySeconds: 0 } },
  updatedAt: `2026-01-0${id}T00:00:00Z`, ...patch,
});

// multiSpec 是带规格维度和两个 SKU 的多规格模板，最低价 5、库存合计 7。
const multiSpec = makeTemplate(3, {
  name: 'Zeta', specs: [{ specId: 's', name: '版本', supportImage: false, values: [] }],
  skus: [{ valueLabels: ['a'], price: '5', quantity: '3' }, { valueLabels: ['b'], price: '8', quantity: '4' }],
}, true);
// templates 是三个特征各异的模板：id 1 单规格 10 元、id 2 固定邮费 20 元、id 3 多规格。
const templates = [
  makeTemplate(1, { name: 'Alpha', description: '含 KEYWORD 描述', price: '10', quantity: 5 }),
  makeTemplate(2, { name: 'Beta', price: '20', quantity: 9, postageMode: 'fixed', postage: '6', category: { catId: '2', catName: '游戏账号', channelCatId: '', tbCatId: '' } }),
  multiSpec,
];

/** ids 取出模板主键序列，方便断言顺序。 */
const ids = (list: ProductTemplate[]): number[] => list.map(/* template 取主键。 */ template => template.id);

describe('templateQuery', /* 模板搜索、筛选与排序。 */ () => {
  test('多规格价格取最低价、库存取合计，单规格取自身，非法价格按 0', /* 派生值。 */ () => {
    expect(templateEffectivePrice(multiSpec)).toBe(5);
    expect(templateTotalStock(multiSpec)).toBe(7);
    expect(templateEffectivePrice(templates[0])).toBe(10);
    expect(templateTotalStock(templates[0])).toBe(5);
    expect(templateEffectivePrice(makeTemplate(4, { price: 'abc' }))).toBe(0);
    expect(templateTotalStock(makeTemplate(5, { skus: [{ valueLabels: ['x'], price: '1', quantity: 'oops' }] }))).toBe(0);
  });

  test('搜索覆盖模板名、标题、描述和类目名，空词返回全部', /* 搜索字段。 */ () => {
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, query: '  ' }))).toHaveLength(3);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, query: 'alpha' }))).toEqual([1]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, query: '标题2' }))).toEqual([2]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, query: 'keyword' }))).toEqual([1]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, query: '游戏' }))).toEqual([2]);
    expect(queryTemplates(templates, { ...defaultTemplateFilters, query: '不存在' })).toEqual([]);
  });

  test('规格、付款发货和邮费筛选', /* 三类筛选。 */ () => {
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, specs: 'single' }))).toEqual([2, 1]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, specs: 'multi' }))).toEqual([3]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, delivery: 'enabled' }))).toEqual([3]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, delivery: 'disabled' }))).toEqual([2, 1]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, postage: 'fixed' }))).toEqual([2]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, postage: 'none' }))).toEqual([]);
  });

  test('覆盖全部排序方式且不修改入参', /* 排序分支。 */ () => {
    expect(ids(queryTemplates(templates, defaultTemplateFilters))).toEqual([3, 2, 1]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, sort: 'updated-asc' }))).toEqual([1, 2, 3]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, sort: 'name-asc' }))).toEqual([1, 2, 3]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, sort: 'price-asc' }))).toEqual([3, 1, 2]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, sort: 'price-desc' }))).toEqual([2, 1, 3]);
    expect(ids(queryTemplates(templates, { ...defaultTemplateFilters, sort: 'stock-desc' }))).toEqual([2, 3, 1]);
    expect(ids(templates)).toEqual([1, 2, 3]);
  });

  test('非法更新时间按 0 排序；isTemplateFilterActive 不把排序算作筛选', /* 边界。 */ () => {
    // broken 是更新时间非法的模板。
    const broken = makeTemplate(4, { updatedAt: 'bad' });
    expect(ids(queryTemplates([broken, templates[0]], defaultTemplateFilters))).toEqual([1, 4]);
    expect(isTemplateFilterActive({ ...defaultTemplateFilters, sort: 'price-asc' })).toBe(false);
    expect(isTemplateFilterActive({ ...defaultTemplateFilters, query: 'x' })).toBe(true);
    expect(isTemplateFilterActive({ ...defaultTemplateFilters, specs: 'multi' })).toBe(true);
    expect(isTemplateFilterActive({ ...defaultTemplateFilters, delivery: 'enabled' })).toBe(true);
    expect(isTemplateFilterActive({ ...defaultTemplateFilters, postage: 'free' })).toBe(true);
  });
});
