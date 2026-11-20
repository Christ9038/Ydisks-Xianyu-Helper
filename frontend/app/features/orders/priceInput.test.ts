import { describe,expect,test } from 'vitest';
import { parseAdjustPriceInput } from './priceInput';

describe('parseAdjustPriceInput 改价金额校验', /* 验证合法与非法金额文本的边界。 */ () => {
  test.each([
    ['8', '8'],
    ['8.8', '8.8'],
    ['8.80', '8.80'],
    ['0.01', '0.01'],
    ['  12.5  ', '12.5'],
    ['¥9.90', '9.90'],
    ['￥ 9.90', '9.90'],
    ['999999999.99', '999999999.99'],
  ])('接受 %s', /* raw 是输入原文，expected 是规范化后的提交文本。 */ (raw, expected) => {
    expect(parseAdjustPriceInput(raw)).toEqual({ ok: true, price: expected });
  });

  test.each([
    ['', '请输入改价后的订单金额'],
    ['   ', '请输入改价后的订单金额'],
    ['¥', '请输入改价后的订单金额'],
    ['abc', '金额只能是数字，最多两位小数'],
    ['-1', '金额只能是数字，最多两位小数'],
    ['1.234', '金额只能是数字，最多两位小数'],
    ['1.', '金额只能是数字，最多两位小数'],
    ['.5', '金额只能是数字，最多两位小数'],
    ['1e3', '金额只能是数字，最多两位小数'],
    ['1,000', '金额只能是数字，最多两位小数'],
    ['0', '金额必须大于 0'],
    ['0.0', '金额必须大于 0'],
    ['00.00', '金额必须大于 0'],
    ['1000000000', '金额过大，请检查后重新输入'],
  ])('拒绝 %j', /* raw 是非法输入，message 是应展示给用户的原因。 */ (raw, message) => {
    expect(parseAdjustPriceInput(raw)).toEqual({ ok: false, message });
  });
});
