import { describe,expect,test } from 'vitest';
import type { ItemStock } from './models';
import { applyStockUpdate,buildStockUpdate,parseStockInput,STOCK_MAX } from './stockInput';

// singleStock 是单规格库存样本，当前库存为 10。
const singleStock: ItemStock = { itemId: 'item-1', multiSpec: false, quantity: 10, skus: [] };
// multiStock 是两行规格的多规格库存样本，总库存为 7。
const multiStock: ItemStock = { itemId: 'item-2', multiSpec: true, quantity: 7, skus: [
  { key: 'sku:a', label: '红色', priceCents: 100, quantity: 3 },
  { key: 'sku:b', label: '蓝色', priceCents: 200, quantity: 4 },
] };

describe('库存输入解析', /* parseSuite 验证只接受纯数字且不超过上限。 */ () => {
  test('接受 0 到上限的整数并忽略首尾空白', /* acceptCase 覆盖边界值。 */ () => {
    expect(parseStockInput(' 12 ')).toBe(12);
    expect(parseStockInput('0')).toBe(0);
    expect(parseStockInput(String(STOCK_MAX))).toBe(STOCK_MAX);
  });

  test('拒绝小数、负数、科学计数法、空串和超限值', /* rejectCase 覆盖会被 Number 宽松转换放过的输入。 */ () => {
    for (const value /* value 是应被拒绝的输入文本。 */ of ['', ' ', '1.5', '-1', '1e3', '0x10', 'abc', String(STOCK_MAX + 1), '12345678']) {
      expect(parseStockInput(value)).toBeNull();
    }
  });
});

describe('库存修改构造', /* buildSuite 验证与服务端一致的校验规则。 */ () => {
  test('单规格：带原库存生成修改', /* singleSuccessCase 验证 expectedQuantity 取自快照。 */ () => {
    expect(buildStockUpdate(singleStock, '25', {})).toEqual({ update: { quantity: 25, expectedQuantity: 10 } });
  });

  test('单规格：非法、为 0 和未变化都不生成修改', /* singleRejectCase 覆盖三类本地拒绝。 */ () => {
    expect(buildStockUpdate(singleStock, 'x', {}).error).toContain('1 到');
    expect(buildStockUpdate(singleStock, '0', {}).error).toContain('下架');
    expect(buildStockUpdate(singleStock, '10', {}).error).toBe('库存没有变化。');
  });

  test('多规格：整体提交全部规格行并携带各自原库存', /* multiSuccessCase 验证每行 expectedQuantity。 */ () => {
    expect(buildStockUpdate(multiStock, '', { 'sku:a': '0', 'sku:b': '9' })).toEqual({ update: { skus: [
      { key: 'sku:a', quantity: 0, expectedQuantity: 3 },
      { key: 'sku:b', quantity: 9, expectedQuantity: 4 },
    ] } });
  });

  test('多规格：非法行、全部为 0 和未变化都不生成修改', /* multiRejectCase 覆盖三类本地拒绝。 */ () => {
    expect(buildStockUpdate(multiStock, '', { 'sku:a': 'x', 'sku:b': '4' }).error).toContain('红色');
    expect(buildStockUpdate(multiStock, '', { 'sku:b': '4' }).error).toContain('红色');
    expect(buildStockUpdate(multiStock, '', { 'sku:a': '0', 'sku:b': '0' }).error).toContain('至少一个规格');
    expect(buildStockUpdate(multiStock, '', { 'sku:a': '3', 'sku:b': '4' }).error).toBe('库存没有变化。');
  });

  test('规格没有名称时用标识提示', /* labelFallbackCase 验证错误文案回退到 key。 */ () => {
    // unnamed 是规格行缺少展示名称的库存样本。
    const unnamed: ItemStock = { itemId: 'i', multiSpec: true, quantity: 1, skus: [{ key: 'sku:z', label: '', priceCents: 0, quantity: 1 }] };
    expect(buildStockUpdate(unnamed, '', { 'sku:z': 'x' }).error).toContain('sku:z');
  });
});

describe('库存修改应用到快照', /* applySuite 验证成功后的原库存更新。 */ () => {
  test('单规格更新顶层库存', /* applySingleCase 验证 quantity 覆盖。 */ () => {
    expect(applyStockUpdate(singleStock, { quantity: 4, expectedQuantity: 10 }).quantity).toBe(4);
    expect(applyStockUpdate(singleStock, {}).quantity).toBe(10);
  });

  test('多规格按行更新并重算总库存，未出现的行保持不变', /* applyMultiCase 验证总库存等于各行之和。 */ () => {
    // updated 是只修改红色规格后的快照。
    const updated = applyStockUpdate(multiStock, { skus: [{ key: 'sku:a', quantity: 9, expectedQuantity: 3 }] });
    expect(updated.skus.map(/* sku 是更新后的规格行。 */ sku => sku.quantity)).toEqual([9, 4]);
    expect(updated.quantity).toBe(13);
    expect(applyStockUpdate(multiStock, {}).quantity).toBe(7);
  });
});
