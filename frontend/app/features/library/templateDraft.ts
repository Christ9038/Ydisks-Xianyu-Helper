import { createPublishSpecId, reconcilePublishSkuRows, totalPublishSkuQuantity, validatePublishSpecs, type PublishSkuRow, type PublishSpec } from '../../../shared/ui/publish/publishSpecs';
import type { TemplateRequestTransport } from './api';
import type { LibraryAction, LibraryActionGroup, LibraryAutomation, LibraryCategory, LibraryDeliveryTemplateOption, ProductTemplate } from './types';

/** TemplateDraft 是模板编辑器的表单状态，金额与库存保留用户输入的文本。 */
export interface TemplateDraft {
  /** name 是模板名称。 */
  name: string;
  /** title 是商品标题。 */
  title: string;
  /** description 是商品描述。 */
  description: string;
  /** price 是单规格售价文本（元）；多规格时由 SKU 推导。 */
  price: string;
  /** originalPrice 是原价文本。 */
  originalPrice: string;
  /** quantity 是单规格库存文本；多规格时由 SKU 推导。 */
  quantity: string;
  /** postageMode 是邮费模式。 */
  postageMode: 'free' | 'fixed';
  /** postage 是固定邮费文本。 */
  postage: string;
  /** category 是平台类目。 */
  category: LibraryCategory;
  /** imageIds 是主图的图片库主键，顺序即主图顺序。 */
  imageIds: number[];
  /** specs 是规格维度草稿，复用共享规格编辑器的结构。 */
  specs: PublishSpec[];
  /** skuRows 是规格组合的价格与库存。 */
  skuRows: PublishSkuRow[];
  /** specImageIds 把规格值标识映射到图片库主键。 */
  specImageIds: Record<string, number>;
  /** automation 是发布后自动化。 */
  automation: LibraryAutomation;
}

/** defaultCategory 是未选择类目时的电子资料兜底类目，与批量铺货的兜底一致。 */
export const defaultCategory: LibraryCategory = { catId: '50025386', catName: '电子资料', channelCatId: '201451202', tbCatId: '' };

/** emptyGroup 创建一个关闭的动作组。 */
const emptyGroup = (): LibraryActionGroup => ({ enabled: false, actions: [] });

/** emptyAutomation 创建全部关闭的自动化配置。 */
export const emptyAutomation = (): LibraryAutomation => ({
  paidDelivery: emptyGroup(), reviewGift: emptyGroup(),
  reviewRequest: { enabled: false, afterShippedHours: 24, message: '', maxAttempts: 1, delaySeconds: 0 },
});

/** emptyDraft 创建一份空白模板草稿。 */
export const emptyDraft = (): TemplateDraft => ({
  name: '', title: '', description: '', price: '', originalPrice: '', quantity: '1', postageMode: 'free', postage: '',
  category: { ...defaultCategory }, imageIds: [], specs: [], skuRows: [], specImageIds: {}, automation: emptyAutomation(),
});

/** draftFromTemplate 把已保存模板还原为可编辑草稿，并按规格值文本恢复 SKU 行。 */
export const draftFromTemplate = (template: ProductTemplate): TemplateDraft => {
  // specImageIds 记录每个规格值标识对应的图片库图片。
  const specImageIds: Record<string, number> = {};
  // specs 是共享编辑器使用的规格维度。
  const specs: PublishSpec[] = template.specs.map(/* spec 转换为共享规格结构。 */ spec => ({
    specId: spec.specId, name: spec.name, supportImage: spec.supportImage,
    values: spec.values.map(/* value 转换为共享规格值，并记录其图片。 */ value => {
      if (value.imageId > 0) specImageIds[value.valueId] = value.imageId;
      return { valueId: value.valueId, value: value.value };
    }),
  }));
  // previousRows 用已保存 SKU 的价格库存预填，rowKey 由规格值标识组合而成。
  const previousRows: PublishSkuRow[] = template.skus.flatMap(/* sku 定位其各维度的规格值标识。 */ sku => {
    // valueIds 是该组合在各维度上命中的规格值标识。
    const valueIds = sku.valueLabels.map(/* label、index 在对应维度中查找规格值。 */ (label, index) => specs[index]?.values.find(/* value 按文本匹配。 */ value => value.value === label)?.valueId ?? '');
    if (valueIds.some(/* id 为空表示未命中。 */ id => !id)) return [];
    return [{ rowKey: valueIds.join('::'), valueIds, valueLabels: sku.valueLabels, price: sku.price, quantity: sku.quantity }];
  });
  return {
    name: template.name, title: template.title, description: template.description,
    price: specs.length ? '' : template.price, originalPrice: template.originalPrice, quantity: specs.length ? '1' : String(template.quantity),
    postageMode: template.postageMode, postage: template.postage === '0.00' ? '' : template.postage,
    category: { ...template.category }, imageIds: [...template.imageIds], specs, skuRows: reconcilePublishSkuRows(specs, previousRows),
    specImageIds, automation: structuredClone(template.automation),
  };
};

/** specScope 是自动化动作绑定的规格范围；单规格商品使用空范围。 */
export interface SpecScope {
  /** specName 是规范化规格名列，维度之间使用中文分号。 */
  specName: string;
  /** specValue 是规范化规格值列，维度之间使用中文分号。 */
  specValue: string;
  /** label 是界面展示文本。 */
  label: string;
}

/** specScopes 列出草稿的自动化范围：单规格只有一个空范围，多规格每个 SKU 组合一个范围。 */
export const specScopes = (draft: TemplateDraft): SpecScope[] => {
  // names 是有效规格维度的名称列。
  const names = draft.specs.filter(/* spec 保留已命名的维度。 */ spec => spec.name.trim()).map(/* spec 提取名称。 */ spec => spec.name.trim());
  if (names.length === 0 || draft.skuRows.length === 0) return [{ specName: '', specValue: '', label: '全部订单' }];
  return draft.skuRows.map(/* row 转换为该组合的范围。 */ row => ({ specName: names.join('；'), specValue: row.valueLabels.join('；'), label: row.valueLabels.join(' / ') }));
};

/** newAction 创建指定类型的空动作并绑定到规格范围。 */
export const newAction = (actionType: LibraryAction['actionType'], scope: SpecScope): LibraryAction => ({
  actionType, cardId: 0, deliveryCount: 1, delaySeconds: 0, deliveryTemplateId: 0, templateBindings: [], customVariables: {},
  specName: scope.specName, specValue: scope.specValue,
});

/** actionsInScope 返回属于某规格范围的动作及其在全量列表中的下标。 */
export const actionsInScope = (group: LibraryActionGroup, scope: SpecScope): Array<{ /** action 是动作本身。 */ action: LibraryAction; /** index 是动作在动作组中的下标。 */ index: number }> =>
  group.actions.map(/* action、index 保留下标以便原位更新。 */ (action, index) => ({ action, index })).filter(/* entry 按规格范围过滤。 */ entry => entry.action.specName === scope.specName && entry.action.specValue === scope.specValue);

/** pruneAutomation 删除不再对应任何 SKU 范围的动作，规格变化后避免提交过期绑定。 */
export const pruneAutomation = (automation: LibraryAutomation, scopes: SpecScope[]): LibraryAutomation => {
  // known 是仍然有效的规格范围键集合。
  const known = new Set(scopes.map(/* scope 生成范围键。 */ scope => `${scope.specName}\u0000${scope.specValue}`));
  /** keep 保留范围仍有效的动作。 */
  const keep = (group: LibraryActionGroup): LibraryActionGroup => ({ ...group, actions: group.actions.filter(/* action 判断范围是否仍存在。 */ action => known.has(`${action.specName}\u0000${action.specValue}`)) });
  return { ...automation, paidDelivery: keep(automation.paidDelivery), reviewGift: keep(automation.reviewGift) };
};

/** syncTemplateBindings 在选中发货模板后按其变量重建绑定与自定义变量。 */
export const syncTemplateBindings = (action: LibraryAction, template: LibraryDeliveryTemplateOption | undefined): LibraryAction => {
  if (!template) return { ...action, deliveryTemplateId: 0, templateBindings: [], customVariables: {} };
  return {
    ...action, deliveryTemplateId: template.id,
    templateBindings: template.keys.map(/* key 为每个卡密变量创建绑定。 */ key => action.templateBindings.find(/* binding 沿用同名变量的已有选择。 */ binding => binding.variableKey === key) ?? { variableKey: key, cardId: 0, deliveryCount: 1 }),
    customVariables: Object.fromEntries(template.customKeys.map(/* key 为每个自定义变量保留已填值。 */ key => [key, action.customVariables[key] ?? ''])),
  };
};

/** isActionComplete 判断动作是否已填写完整。 */
export const isActionComplete = (action: LibraryAction): boolean => {
  if (action.actionType === 'send_card') return action.cardId > 0 && action.deliveryCount > 0;
  return action.deliveryTemplateId > 0
    && action.templateBindings.every(/* binding 必须选择卡密组。 */ binding => binding.cardId > 0 && binding.deliveryCount > 0)
    && Object.values(action.customVariables).every(/* value 自定义变量不能为空。 */ value => value.trim() !== '');
};

/** validateDraft 在提交前做用户可读的基础校验；完整校验仍由服务端权威执行。 */
export const validateDraft = (draft: TemplateDraft): string => {
  if (!draft.name.trim()) return '请填写模板名称';
  if (!draft.title.trim()) return '请填写商品标题';
  if (draft.imageIds.length < 1 || draft.imageIds.length > 9) return '请从图片库选择 1-9 张商品主图';
  if (draft.specs.length > 0) {
    // specCheck 是共享规格校验结果。
    const specCheck = validatePublishSpecs(draft.specs, draft.skuRows);
    if (!specCheck.valid) return specCheck.message ?? '规格填写有误';
  } else {
    if (!(Number(draft.price) > 0)) return '请填写大于 0 的售价';
    if (!/^\d+$/.test(draft.quantity.trim()) || Number(draft.quantity) <= 0) return '请填写大于 0 的库存';
  }
  if (draft.postageMode === 'fixed' && !(Number(draft.postage) > 0)) return '固定邮费必须大于 0';
  // scopes 是自动化需要覆盖的规格范围。
  const scopes = specScopes(draft);
  // groupName、group 逐个检查启用的动作组。
  for (const /* groupName、group 是动作组名称及其配置。 */ [groupName, group] of [['付款发货', draft.automation.paidDelivery], ['评价赠品', draft.automation.reviewGift]] as const) {
    if (!group.enabled) continue;
    if (group.actions.some(/* action 必须填写完整。 */ action => !isActionComplete(action))) return `${groupName}存在未填写完整的动作`;
    if (groupName === '付款发货' && scopes.some(/* scope 付款发货必须覆盖每个 SKU。 */ scope => actionsInScope(group, scope).length === 0)) return '多规格商品的付款发货必须为每个规格组合配置动作';
  }
  return '';
};

/** toRequest 把草稿转换为接口请求体；规格图片按出现顺序去重后写入 spec_image_ids。 */
export const toRequest = (draft: TemplateDraft): TemplateRequestTransport => {
  // multi 表示是否为多规格。
  const multi = draft.specs.length > 0;
  // specImageIds 是去重后的规格图片列表，value.image_index 指向其下标。
  const specImageIds: number[] = [];
  // specs 是请求中的规格维度。
  const specs = draft.specs.map(/* spec 转换为请求规格。 */ spec => ({
    property_name: spec.name.trim(), support_image: spec.supportImage,
    values: spec.values.filter(/* value 丢弃空规格值。 */ value => value.value.trim()).map(/* value 转换为请求规格值。 */ value => {
      // imageId 是该规格值绑定的图片。
      const imageId = spec.supportImage ? draft.specImageIds[value.valueId] ?? 0 : 0;
      if (imageId <= 0) return { value: value.value.trim(), image_index: -1 };
      // existing 是该图片已在列表中的下标。
      const existing = specImageIds.indexOf(imageId);
      if (existing >= 0) return { value: value.value.trim(), image_index: existing };
      specImageIds.push(imageId);
      return { value: value.value.trim(), image_index: specImageIds.length - 1 };
    }),
  }));
  // names 是 SKU 属性使用的规格名称列。
  const names = draft.specs.map(/* spec 提取名称。 */ spec => spec.name.trim());
  return {
    name: draft.name.trim(), title: draft.title.trim(), description: draft.description.trim(),
    price: multi ? '0' : draft.price.trim(), original_price: draft.originalPrice.trim(),
    quantity: multi ? totalPublishSkuQuantity(draft.skuRows) : Number(draft.quantity),
    postage_mode: draft.postageMode, postage: draft.postageMode === 'fixed' ? draft.postage.trim() : '0',
    category: { cat_id: draft.category.catId, cat_name: draft.category.catName, channel_cat_id: draft.category.channelCatId, tb_cat_id: draft.category.tbCatId },
    image_ids: draft.imageIds,
    specs: {
      specs: multi ? specs : [], spec_image_ids: multi ? specImageIds : [],
      skus: multi ? draft.skuRows.map(/* row 转换为请求 SKU。 */ row => ({ price: row.price.trim(), quantity: Number(row.quantity), property_list: row.valueLabels.map(/* label、index 配对规格名与值。 */ (label, index) => ({ property_text: names[index] ?? '', value_text: label })) })) : [],
    },
    automation: {
      paid_delivery: groupRequest(draft.automation.paidDelivery), review_gift: groupRequest(draft.automation.reviewGift),
      review_request: {
        enabled: draft.automation.reviewRequest.enabled, after_shipped_hours: draft.automation.reviewRequest.afterShippedHours, message: draft.automation.reviewRequest.message,
        max_attempts: draft.automation.reviewRequest.maxAttempts, delay_seconds: draft.automation.reviewRequest.delaySeconds,
      },
    },
  };
};

/** groupRequest 把动作组转换为请求结构。 */
const groupRequest = (group: LibraryActionGroup): TemplateRequestTransport['automation']['paid_delivery'] => ({
  enabled: group.enabled,
  actions: group.actions.map(/* action 转换为请求动作。 */ action => ({
    action_type: action.actionType, card_id: action.actionType === 'send_card' ? action.cardId : 0, delivery_count: action.deliveryCount, delay_seconds: action.delaySeconds,
    delivery_template_id: action.actionType === 'send_template' ? action.deliveryTemplateId : 0,
    template_bindings: action.actionType === 'send_template' ? action.templateBindings.map(/* binding 转换为请求绑定。 */ binding => ({ variable_key: binding.variableKey, card_id: binding.cardId, delivery_count: binding.deliveryCount })) : [],
    custom_variables: action.actionType === 'send_template' ? action.customVariables : {}, spec_name: action.specName, spec_value: action.specValue,
  })),
});

/** reconcileDraftSpecs 在规格变化后重建 SKU 行、清理失效的规格图片与自动化范围。 */
export const reconcileDraftSpecs = (draft: TemplateDraft, specs: PublishSpec[], skuRows: PublishSkuRow[]): TemplateDraft => {
  // alive 是仍存在的规格值标识集合。
  const alive = new Set(specs.flatMap(/* spec 展开其规格值标识。 */ spec => spec.values.map(/* value 提取标识。 */ value => value.valueId)));
  // next 是应用规格变化后的草稿。
  const next: TemplateDraft = {
    ...draft, specs, skuRows,
    specImageIds: Object.fromEntries(Object.entries(draft.specImageIds).filter(/* [valueId] 过滤已删除的规格值。 */ ([valueId]) => alive.has(valueId))),
  };
  return { ...next, automation: pruneAutomation(next.automation, specScopes(next)) };
};

/** newSpecId 为新规格值生成草稿标识，供图片选择等流程复用。 */
export const newSpecId = createPublishSpecId;
