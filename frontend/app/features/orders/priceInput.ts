// AdjustPriceInputResult 描述改价金额输入的校验结果：通过时携带可直接提交的规范文本。
export type AdjustPriceInputResult =
  | { /** ok 为 true 表示输入合法。 */ ok: true; /** price 是去除空白和货币符号后的元金额文本。 */ price: string }
  | { /** ok 为 false 表示输入不合法。 */ ok: false; /** message 是面向用户的原因说明。 */ message: string };

/** 元金额文本：整数部分不含前导符号，小数最多两位，与后端 money.ParseYuanToCents 的严格格式一致。 */
const yuanPattern = /^\d+(\.\d{1,2})?$/;

/** 整数元部分的最大位数：仅是前端防误输入的合理性上限（不超过 999,999,999 元），后端另有 int64 溢出保护。 */
const maxWholeYuanDigits = 9;

// parseAdjustPriceInput 校验用户输入的改价金额；不使用浮点数，避免 0.1+0.2 一类误差进入提交内容。
// raw 是输入框原文，允许首尾空白和开头的 ¥/￥；金额必须大于 0 且最多两位小数。
export const parseAdjustPriceInput = (raw: string): AdjustPriceInputResult => {
  // text 是去除首尾空白与货币符号后的待校验文本。
  const text = raw.trim().replace(/^[¥￥]\s*/, '');
  if (text === '') return { ok: false, message: '请输入改价后的订单金额' };
  if (!yuanPattern.test(text)) return { ok: false, message: '金额只能是数字，最多两位小数' };
  // whole 是整数元部分，用于位数上限与零值判断。
  const whole = text.split('.')[0];
  if (whole.length > maxWholeYuanDigits) return { ok: false, message: '金额过大，请检查后重新输入' };
  if (/^0+(\.0{1,2})?$/.test(text)) return { ok: false, message: '金额必须大于 0' };
  return { ok: true, price: text };
};
