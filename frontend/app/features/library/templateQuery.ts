import type { ProductTemplate } from './types';

/** TemplateSort 是模板排序方式。 */
export type TemplateSort = 'updated-desc' | 'updated-asc' | 'name-asc' | 'price-asc' | 'price-desc' | 'stock-desc';

/** TemplateFilters 是模板列表的搜索、筛选与排序条件，由界面持有并交给 queryTemplates 推导可见列表。 */
export interface TemplateFilters {
  /** query 是搜索词，匹配模板名、标题、描述和类目名。 */
  query: string;
  /** specs 筛选单规格或多规格模板。 */
  specs: 'all' | 'single' | 'multi';
  /** delivery 筛选付款后自动发货是否启用。 */
  delivery: 'all' | 'enabled' | 'disabled';
  /** postage 筛选邮费模式。 */
  postage: 'all' | ProductTemplate['postageMode'];
  /** sort 是排序方式。 */
  sort: TemplateSort;
}

/** defaultTemplateFilters 是默认条件：不搜索、不筛选、最近更新在前。 */
export const defaultTemplateFilters: TemplateFilters = { query: '', specs: 'all', delivery: 'all', postage: 'all', sort: 'updated-desc' };

/** isTemplateFilterActive 判断是否有搜索或筛选生效（排序不算筛选），用于决定是否展示“重置”。 */
export const isTemplateFilterActive = (filters: TemplateFilters): boolean =>
  filters.query.trim() !== '' || filters.specs !== 'all' || filters.delivery !== 'all' || filters.postage !== 'all';

/** priceNumber 把价格文本转成数字，空串或非法文本按 0 处理。 */
const priceNumber = (text: string): number => {
  // value 是解析后的价格（元）。
  const value = Number(text);
  return Number.isFinite(value) ? value : 0;
};

/** templateEffectivePrice 返回用于排序的价格：多规格取 SKU 最低价，单规格取模板售价。 */
export const templateEffectivePrice = (template: ProductTemplate): number =>
  template.skus.length > 0 ? Math.min(...template.skus.map(/* sku 取该组合的价格。 */ sku => priceNumber(sku.price))) : priceNumber(template.price);

/** templateTotalStock 返回用于排序的库存：多规格为 SKU 库存合计，单规格为模板库存。 */
export const templateTotalStock = (template: ProductTemplate): number =>
  template.skus.length > 0 ? template.skus.reduce(/* total 是累计库存，sku 是当前组合。 */ (total, sku) => total + (Number(sku.quantity) || 0), 0) : template.quantity;

/** updatedTime 把更新时间转为毫秒数；无法解析时按 0 处理，保证排序稳定。 */
const updatedTime = (template: ProductTemplate): number => {
  // time 是解析出的毫秒时间戳，非法时间为 NaN。
  const time = Date.parse(template.updatedAt);
  return Number.isNaN(time) ? 0 : time;
};

/** compareTemplates 返回指定排序方式的比较器；主键作为最后的平局依据，让同值排序可预测。 */
const compareTemplates = (sort: TemplateSort): ((a: ProductTemplate, b: ProductTemplate) => number) => {
  switch (sort) {
    case 'updated-asc': return /* 较早更新在前。 */ (a, b) => updatedTime(a) - updatedTime(b) || a.id - b.id;
    case 'name-asc': return /* 模板名升序，使用中文友好的本地化比较。 */ (a, b) => a.name.localeCompare(b.name, 'zh-CN') || a.id - b.id;
    case 'price-asc': return /* 价格低的在前。 */ (a, b) => templateEffectivePrice(a) - templateEffectivePrice(b) || a.id - b.id;
    case 'price-desc': return /* 价格高的在前。 */ (a, b) => templateEffectivePrice(b) - templateEffectivePrice(a) || a.id - b.id;
    case 'stock-desc': return /* 库存多的在前。 */ (a, b) => templateTotalStock(b) - templateTotalStock(a) || a.id - b.id;
    default: return /* 较新更新在前。 */ (a, b) => updatedTime(b) - updatedTime(a) || b.id - a.id;
  }
};

/** queryTemplates 依次应用搜索、三类筛选和排序，返回新数组，不修改入参。 */
export const queryTemplates = (templates: ProductTemplate[], filters: TemplateFilters): ProductTemplate[] => {
  // keyword 是去除首尾空白并小写化的搜索词。
  const keyword = filters.query.trim().toLowerCase();
  // matched 是同时通过搜索与全部筛选条件的模板。
  const matched = templates.filter(/* template 逐项检查搜索与筛选条件。 */ template => {
    if (keyword && ![template.name, template.title, template.description, template.category.catName].some(/* field 是参与搜索的文本字段。 */ field => field.toLowerCase().includes(keyword))) return false;
    // multi 表示模板配置了规格维度。
    const multi = template.specs.length > 0;
    if (filters.specs === 'single' && multi) return false;
    if (filters.specs === 'multi' && !multi) return false;
    // delivery 表示模板启用了付款后自动发货。
    const delivery = template.automation.paidDelivery.enabled;
    if (filters.delivery === 'enabled' && !delivery) return false;
    if (filters.delivery === 'disabled' && delivery) return false;
    return filters.postage === 'all' || template.postageMode === filters.postage;
  });
  return [...matched].sort(compareTemplates(filters.sort));
};
