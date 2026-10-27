// publishSpecs 提供普通发布表单的规格值、SKU 组合与字段校验工具。

/** 单个商品规格值及其可选的规格图片。 */
export interface PublishSpecValue {
  /** valueId 是前端草稿内稳定的规格值标识，用于重建组合时保留用户输入。 */
  valueId: string;
  /** value 是展示给买家的规格值文本。 */
  value: string;
  /** image 是该规格值绑定的可选图片，仅在支持规格图片时上传。 */
  image?: File | null;
}

/** 一个商品规格维度及其可选值。 */
export interface PublishSpec {
  /** specId 是前端草稿内稳定的规格维度标识。 */
  specId: string;
  /** name 是闲鱼规格名称，例如颜色或尺码。 */
  name: string;
  /** customName 表示名称来自用户自定义输入，而不是官方预置选项。 */
  customName?: boolean;
  /** supportImage 表示规格值是否允许绑定图片。 */
  supportImage: boolean;
  /** values 保存该维度下的规格值草稿。 */
  values: PublishSpecValue[];
}

/** 一个规格组合对应的价格、库存和规格值标识。 */
export interface PublishSkuRow {
  /** rowKey 是由规格值标识组成的稳定组合键。 */
  rowKey: string;
  /** valueIds 保存组合中的规格值标识，顺序与规格维度一致。 */
  valueIds: string[];
  /** valueLabels 保存组合中的展示文本，避免 UI 依赖可变对象引用。 */
  valueLabels: string[];
  /** price 是该 SKU 的售价文本，单位为元。 */
  price: string;
  /** quantity 是该 SKU 的库存文本。 */
  quantity: string;
}

/** 规格值与 SKU 的前端校验结果。 */
export interface PublishSpecsValidation {
  /** valid 表示规格草稿是否可以提交。 */
  valid: boolean;
  /** message 是校验失败时直接展示给用户的原因。 */
  message?: string;
}

/** 生成前端草稿使用的唯一标识，不承担平台业务语义。 */
export const createPublishSpecId = (prefix: string): string => `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

/** 生成规格值的笛卡尔积，顺序与闲鱼官方组合表一致。 */
const cartesianProduct = <T>(groups: T[][]): T[][] => {
  if (groups.length === 0) return [];
  return groups.reduce<T[][]>(/* combinations、group 逐步构造规格组合。 */ (combinations, group) => combinations.flatMap(/* combination 承载当前已构造的部分组合。 */ combination => group.map(/* value 追加当前维度的一个规格值。 */ value => [...combination, value])), [[]]);
};

/** 根据规格草稿重建 SKU 行，并按组合键保留已有价格与库存。 */
export const reconcilePublishSkuRows = (specs: PublishSpec[], previousRows: PublishSkuRow[] = []): PublishSkuRow[] => {
  // activeSpecs 保存已经填写名称且至少包含一个非空值的规格维度。
  const activeSpecs = specs.filter(/* spec 判断规格维度是否已经开始填写。 */ spec => spec.name.trim() && spec.values.some(/* value 判断规格值是否非空。 */ value => value.value.trim()));
  if (activeSpecs.length === 0) return [];
  // groups 保存每个有效规格维度的非空规格值组。
  const groups = activeSpecs.map(/* spec 提取当前维度的有效规格值。 */ spec => spec.values.filter(/* value 筛选当前维度的非空规格值。 */ value => value.value.trim()));
  // previousByKey 按稳定组合键索引旧 SKU 行，以保留用户已填价格库存。
  const previousByKey = new Map(previousRows.map(/* row 建立旧 SKU 行的组合键索引。 */ row => [row.rowKey, row]));
  return cartesianProduct(groups).map(/* values 表示一个规格组合的值列表。 */ values => {
    // valueIds 保存组合中各规格值的稳定标识。
    const valueIds = values.map(/* value 提取组合中的规格值标识。 */ value => value.valueId);
    // rowKey 保存当前组合的稳定键。
    const rowKey = valueIds.join('::');
    // previous 保存当前组合先前的价格库存输入。
    const previous = previousByKey.get(rowKey);
    return {
      rowKey,
      valueIds,
      valueLabels: values.map(/* value 提取当前组合的展示文本。 */ value => value.value.trim()),
      price: previous?.price || '',
      quantity: previous?.quantity || '',
    };
  });
};

/** 校验发布规格是否满足闲鱼官方的最多两维、每维至少两个值和组合数限制。 */
export const validatePublishSpecs = (specs: PublishSpec[], rows: PublishSkuRow[]): PublishSpecsValidation => {
  if (specs.length === 0) return { valid: true };
  if (specs.length > 2) return { valid: false, message: '商品规格最多支持 2 个规格类型' };
  // names 保存已使用的规格名称，防止重复维度。
  const names = new Set<string>();
  for (const /* spec 表示当前待校验的规格维度。 */ spec of specs) {
    // name 保存去除首尾空白后的规格名称。
    const name = spec.name.trim();
    if (!name) return { valid: false, message: '请先选择或填写规格类型' };
    if (names.has(name)) return { valid: false, message: '规格类型不能重复' };
    names.add(name);
    // values 保存当前维度的非空规格值文本。
    const values = spec.values.map(/* value 提取规格值文本。 */ value => value.value.trim()).filter(/* valueText 筛选非空规格值文本。 */ Boolean);
    if (values.length < 2) return { valid: false, message: `规格“${name}”至少需要 2 个规格值` };
    if (new Set(values).size !== values.length) return { valid: false, message: `规格“${name}”的规格值不能重复` };
  }
  if (rows.length === 0 || rows.length > 1500) return { valid: false, message: '规格组合数量必须在 1 到 1500 之间' };
  if (!rows.some(/* row 检查是否存在可售库存的 SKU 行。 */ row => Number(row.quantity) > 0)) return { valid: false, message: '至少需要有一个 SKU 库存大于 0' };
  for (const /* row 表示当前待校验价格库存的 SKU 行。 */ row of rows) {
    if (!row.price.trim() || Number(row.price) <= 0) return { valid: false, message: '请填写每个 SKU 的售价' };
    if (!/^\d+$/.test(row.quantity.trim()) || Number(row.quantity) <= 0) return { valid: false, message: '请填写每个 SKU 的库存' };
  }
  return { valid: true };
};

/** 计算多规格商品提交给平台的总库存。 */
export const totalPublishSkuQuantity = (rows: PublishSkuRow[]): number => rows.reduce(/* total、row 汇总各 SKU 的库存文本。 */ (total, row) => total + (Number(row.quantity) || 0), 0);
