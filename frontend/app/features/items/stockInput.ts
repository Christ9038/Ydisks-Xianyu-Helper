import type { ItemStock, ItemStockUpdate } from './models';

/** STOCK_MAX 是单个库存数的上限，与服务端和闲鱼发布页保持一致。 */
export const STOCK_MAX = 999_999;

/**
 * parseStockInput 把输入框文本解析为 0 到 STOCK_MAX 的整数。
 * 只接受纯数字，拒绝小数、负数、科学计数法与空串，避免被 Number() 宽松转换放过。
 */
export const parseStockInput = (value: string): number | null => {
  // text 是去掉首尾空白后的输入。
  const text = value.trim();
  if (!/^\d{1,7}$/.test(text)) return null;
  // parsed 是解析出的整数，可能超过上限，由调用方继续校验范围。
  const parsed = Number(text);
  return parsed <= STOCK_MAX ? parsed : null;
};

/** StockUpdateResult 是 buildStockUpdate 的结果：要么得到可提交的修改，要么得到面向用户的错误说明。 */
export type StockUpdateResult = { /** 可提交的库存修改。 */ update: ItemStockUpdate; /** 无错误。 */ error?: undefined } | { /** 无可提交内容。 */ update?: undefined; /** 面向用户的错误说明。 */ error: string };

/**
 * buildStockUpdate 校验输入草稿并生成提交内容；规则与服务端一致：
 * 单规格 1..STOCK_MAX；多规格每行 0..STOCK_MAX 且至少一行大于 0；没有任何变化时拒绝提交以免无谓的平台请求。
 */
export const buildStockUpdate = (stock: ItemStock, singleInput: string, skuInputs: Record<string, string>): StockUpdateResult => {
  if (!stock.multiSpec) {
    // quantity 是解析后的新库存。
    const quantity = parseStockInput(singleInput);
    if (quantity === null || quantity < 1) return { error: `库存必须是 1 到 ${STOCK_MAX} 的整数；要让商品售罄请使用下架。` };
    if (quantity === stock.quantity) return { error: '库存没有变化。' };
    return { update: { quantity, expectedQuantity: stock.quantity } };
  }
  // rows 保存全部规格行的修改；多规格必须整体提交。
  const rows: NonNullable<ItemStockUpdate['skus']> = [];
  // changed 表示是否至少有一行库存与原值不同。
  let changed = false;
  // positive 统计新库存大于 0 的规格行数。
  let positive = 0;
  // sku 是当前遍历的规格行。
  for (const sku /* sku 是当前遍历的规格行。 */ of stock.skus) {
    // quantity 是该规格行解析后的新库存。
    const quantity = parseStockInput(skuInputs[sku.key] ?? '');
    if (quantity === null) return { error: `「${sku.label || sku.key}」的库存必须是 0 到 ${STOCK_MAX} 的整数。` };
    if (quantity !== sku.quantity) changed = true;
    if (quantity > 0) positive += 1;
    rows.push({ key: sku.key, quantity, expectedQuantity: sku.quantity });
  }
  if (positive === 0) return { error: '至少一个规格的库存需要大于 0；要让商品售罄请使用下架。' };
  if (!changed) return { error: '库存没有变化。' };
  return { update: { skus: rows } };
};

/** applyStockUpdate 在修改成功后把新库存应用到本地快照，使后续修改以新值作为并发校验的原库存。 */
export const applyStockUpdate = (stock: ItemStock, update: ItemStockUpdate): ItemStock => {
  if (!stock.multiSpec) return { ...stock, quantity: update.quantity ?? stock.quantity };
  // changes 是规格行标识到新库存的映射。
  const changes = new Map((update.skus ?? []).map(/* row 是一行规格库存修改。 */ row => [row.key, row.quantity]));
  // skus 是应用修改后的规格行。
  const skus = stock.skus.map(/* sku 是当前规格行。 */ sku => ({ ...sku, quantity: changes.get(sku.key) ?? sku.quantity }));
  return { ...stock, skus, quantity: skus.reduce(/* sum 是累计库存，sku 是当前规格行。 */ (sum, sku) => sum + sku.quantity, 0) };
};
