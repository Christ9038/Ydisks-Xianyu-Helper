import { describe, expect, test } from 'vitest';
import { reconcilePublishSkuRows, totalPublishSkuQuantity, validatePublishSpecs, type PublishSpec } from './publishSpecs';

/** specs 保存测试用的颜色和尺码规格。 */
const specs: PublishSpec[] = [
  { specId: 'color', name: '颜色', supportImage: true, values: [{ valueId: 'red', value: '红色' }, { valueId: 'blue', value: '蓝色' }] },
  { specId: 'size', name: '尺码', supportImage: false, values: [{ valueId: 's', value: 'S' }, { valueId: 'm', value: 'M' }] },
];

describe('publish specs', /* testGroup 验证规格组合工具的稳定性和校验规则。 */ () => {
  test('按官方顺序生成笛卡尔积并保留已有 SKU 输入', /* testCase 验证组合顺序和旧输入保留。 */ () => {
    // previous 保存用户已经填写过价格库存的旧组合行。
    const previous = reconcilePublishSkuRows(specs).map(/* row 修改目标组合的旧输入。 */ row => row.rowKey === 'blue::m' ? { ...row, price: '29.90', quantity: '3' } : row);
    // rows 保存规格重建后的组合行。
    const rows = reconcilePublishSkuRows(specs, previous);
    expect(rows).toHaveLength(4);
    expect(rows.map(/* row 提取组合的展示规格值。 */ row => row.valueLabels)).toEqual([['红色', 'S'], ['红色', 'M'], ['蓝色', 'S'], ['蓝色', 'M']]);
    expect(rows[3]).toMatchObject({ price: '29.90', quantity: '3' });
  });

  test('删除规格值后只保留仍然存在的组合', /* testCase 验证删除规格值后的组合收敛。 */ () => {
    // rows 保存删除颜色值后仍然有效的组合行。
    const rows = reconcilePublishSkuRows([{ ...specs[0], values: specs[0].values.slice(0, 1) }, specs[1]]);
    expect(rows.map(/* row 提取组合的稳定键。 */ row => row.rowKey)).toEqual(['red::s', 'red::m']);
  });

  test('校验官方最小值、重复值和库存规则', /* testCase 覆盖官方最小值、重复值和库存边界。 */ () => {
    // validRows 保存满足官方规则的基准 SKU 行。
    const validRows = reconcilePublishSkuRows(specs).map(/* row 填充基准价格和库存。 */ row => ({ ...row, price: '9.90', quantity: '1' }));
    expect(validatePublishSpecs(specs, validRows)).toEqual({ valid: true });
    expect(validatePublishSpecs([{ ...specs[0], values: [specs[0].values[0]] }], validRows)).toMatchObject({ valid: false, message: '规格“颜色”至少需要 2 个规格值' });
    expect(validatePublishSpecs([{ ...specs[0], values: [{ valueId: 'a', value: '同名' }, { valueId: 'b', value: '同名' }] }], validRows)).toMatchObject({ valid: false, message: '规格“颜色”的规格值不能重复' });
    expect(validatePublishSpecs(specs, validRows.map(/* row 将基准行库存改为零。 */ row => ({ ...row, quantity: '0' })))).toMatchObject({ valid: false, message: '至少需要有一个 SKU 库存大于 0' });
  });

  test('汇总多规格库存', /* testCase 验证提交前的库存汇总。 */ () => {
    expect(totalPublishSkuQuantity([{ ...reconcilePublishSkuRows(specs)[0], quantity: '2' }, { ...reconcilePublishSkuRows(specs)[1], quantity: '3' }])).toBe(5);
  });
});
