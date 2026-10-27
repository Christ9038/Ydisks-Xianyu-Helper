import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import type { CardTransportForLibrary, LibraryActionGroupTransport, LibraryActionTransport, LibraryAutomationTransport, LibraryImageTransport, LibraryTemplateRequestTransport, LibraryTemplateTransport } from '../../../shared/api-contract/library';
import { ApiError, type RequestControlOptions } from '../../../shared/http/client';
import type {
  LibraryAccountOption,
  LibraryAction,
  LibraryActionGroup,
  LibraryAutomation,
  LibraryCardOption,
  LibraryDeliveryTemplateOption,
  LibraryImage,
  LibraryPublishLocation,
  LibraryPublishPreview,
  LibraryUploadResult,
  ProductTemplate,
} from './types';

/** TemplateTransport 是生成契约中的模板响应。 */
type TemplateTransport = LibraryTemplateTransport;
/** TemplateRequestTransport 是生成契约中的模板请求。 */
export type TemplateRequestTransport = LibraryTemplateRequestTransport;
/** ActionTransport 是生成契约中的自动化动作。 */
type ActionTransport = LibraryActionTransport;
/** GroupTransport 是生成契约中的自动化动作组。 */
type GroupTransport = LibraryActionGroupTransport;
/** AutomationTransport 是生成契约中的模板自动化。 */
type AutomationTransport = LibraryAutomationTransport;
/** ImageTransport 是生成契约中的图片。 */
type ImageTransport = LibraryImageTransport;

/** libraryErrorMessage 把请求错误转换为用户可读文本；图片被引用时附带模板名。 */
export const libraryErrorMessage = (error: unknown, fallback: string): string => {
  if (error instanceof ApiError) {
    // templates 是 409 冲突 details 中引用图片的模板名。
    const templates = (error.details as { /** templates 是引用图片的模板名称列表。 */ templates?: unknown } | undefined)?.templates;
    if (error.status === 409 && Array.isArray(templates) && templates.length > 0) return `${error.message}：${templates.join('、')}`;
    return error.message || fallback;
  }
  return error instanceof Error ? error.message : fallback;
};

/** libraryImageUrl 返回图片内容的同源地址，浏览器会携带登录 Cookie 直接加载。 */
export const libraryImageUrl = (id: number): string => `/api/v1/library/images/${id}/content`;

/** toImage 将图片传输对象转换为 UI 模型。 */
const toImage = (dto: ImageTransport): LibraryImage => ({ id: dto.id, filename: dto.filename, contentType: dto.content_type, sizeBytes: dto.size_bytes, createdAt: dto.created_at });

/** toAction 将自动化动作传输对象转换为 UI 模型。 */
const toAction = (dto: ActionTransport): LibraryAction => ({
  actionType: dto.action_type, cardId: dto.card_id, deliveryCount: dto.delivery_count, delaySeconds: dto.delay_seconds,
  deliveryTemplateId: dto.delivery_template_id,
  templateBindings: dto.template_bindings.map(/* binding 是传输层的卡密变量绑定。 */ binding => ({ variableKey: binding.variable_key, cardId: binding.card_id, deliveryCount: binding.delivery_count })),
  customVariables: { ...dto.custom_variables }, specName: dto.spec_name, specValue: dto.spec_value,
});

/** toGroup 将动作组传输对象转换为 UI 模型。 */
const toGroup = (dto: GroupTransport): LibraryActionGroup => ({ enabled: dto.enabled, actions: dto.actions.map(toAction) });

/** toAutomation 将自动化传输对象转换为 UI 模型。 */
const toAutomation = (dto: AutomationTransport): LibraryAutomation => ({
  paidDelivery: toGroup(dto.paid_delivery), reviewGift: toGroup(dto.review_gift),
  reviewRequest: {
    enabled: dto.review_request.enabled, afterShippedHours: dto.review_request.after_shipped_hours, message: dto.review_request.message,
    maxAttempts: dto.review_request.max_attempts, delaySeconds: dto.review_request.delay_seconds,
  },
});

/** postageModeFrom 把服务端邮费模式收敛为已知取值，未知值按包邮处理。 */
const postageModeFrom = (mode: string): ProductTemplate['postageMode'] => mode === 'fixed' || mode === 'distance' || mode === 'none' ? mode : 'free';

/** specIdCounter 为从服务端恢复的规格生成草稿内稳定标识。 */
let specIdCounter = 0;
/** nextSpecId 生成不依赖时间的草稿标识，保证恢复结果可预测。 */
const nextSpecId = (prefix: string): string => `${prefix}-loaded-${++specIdCounter}`;

/** toTemplate 将模板传输对象转换为 UI 模型，并把 spec_image_ids 还原为规格值上的图片主键。 */
export const toTemplate = (dto: TemplateTransport): ProductTemplate => ({
  id: dto.id, name: dto.name, title: dto.title, description: dto.description, price: dto.price, originalPrice: dto.original_price,
  quantity: dto.quantity, postageMode: postageModeFrom(dto.postage_mode), postage: dto.postage,
  category: { catId: dto.category.cat_id, catName: dto.category.cat_name, channelCatId: dto.category.channel_cat_id ?? '', tbCatId: dto.category.tb_cat_id ?? '' },
  imageIds: [...dto.image_ids],
  specs: dto.specs.specs.map(/* spec 是传输层规格维度。 */ spec => ({
    specId: nextSpecId('spec'), name: spec.property_name, supportImage: spec.support_image,
    values: spec.values.map(/* value 是传输层规格值。 */ value => ({
      valueId: nextSpecId('value'), value: value.value,
      imageId: value.image_index >= 0 ? dto.specs.spec_image_ids[value.image_index] ?? 0 : 0,
    })),
  })),
  skus: dto.specs.skus.map(/* sku 是传输层规格组合。 */ sku => ({ valueLabels: sku.property_list.map(/* property 是组合中的一个规格值。 */ property => property.value_text), price: sku.price, quantity: String(sku.quantity) })),
  automation: toAutomation(dto.automation), updatedAt: dto.updated_at,
});

/** listLibraryImages 读取图片库。 */
export const listLibraryImages = async (options?: RequestControlOptions): Promise<LibraryImage[]> => {
  // response 是图片列表传输对象。
  const response = await runContractRequest(/* signal 控制图片列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/library/images', { signal }), options);
  return response.map(toImage);
};

/** uploadLibraryImages 上传一批图片，逐文件返回结果。 */
export const uploadLibraryImages = async (files: File[], options?: RequestControlOptions): Promise<LibraryUploadResult[]> => {
  // body 是 multipart 上传体，字段名 files 与契约一致。
  const body = new FormData();
  // file 是待上传的图片文件。
  for (const /* file 是当前待上传的图片文件。 */ file of files) body.append('files', file);
  // response 是逐文件上传结果。
  const response = await runContractRequest(/* signal 控制图片上传请求的取消和超时。 */ signal => contractClient.POST('/api/v1/library/images', { body: contractMultipartBody(body), signal }), { timeoutMs: 120_000, ...options });
  return response.results.map(/* item 是单个文件的传输结果。 */ item => ({ filename: item.filename, duplicate: item.duplicate, error: item.error, image: item.image ? toImage(item.image) : null }));
};

/** deleteLibraryImage 删除图片；被模板引用时抛出带模板名的 409 错误。 */
export const deleteLibraryImage = async (id: number, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制图片删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/library/images/{image_id}', { params: { path: { image_id: id } }, signal }), options);
};

/** fetchLibraryImageFile 经契约客户端取回图片字节并包装成可上传的 File，供单品发布复用图片库。 */
export const fetchLibraryImageFile = async (image: LibraryImage, signal?: AbortSignal): Promise<File> => {
  // blob 是图片字节；parseAs 让契约客户端按二进制读取响应。
  const blob = await runContractRequest(/* requestSignal 控制图片读取请求的取消和超时。 */ requestSignal => contractClient.GET('/api/v1/library/images/{image_id}/content', { params: { path: { image_id: image.id } }, parseAs: 'blob', signal: requestSignal }) as never, { signal }) as unknown as Blob;
  return new File([blob], image.filename, { type: image.contentType });
};

/** listProductTemplates 读取商品模板。 */
export const listProductTemplates = async (options?: RequestControlOptions): Promise<ProductTemplate[]> => {
  // response 是模板列表传输对象。
  const response = await runContractRequest(/* signal 控制模板列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/library/product-templates', { signal }), options);
  return response.map(toTemplate);
};

/** saveProductTemplate 创建（id 为空）或更新商品模板。 */
export const saveProductTemplate = async (id: number | null, body: TemplateRequestTransport, options?: RequestControlOptions): Promise<ProductTemplate> => {
  // response 是保存后的模板传输对象。
  const response = id === null
    ? await runContractRequest(/* signal 控制模板创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/library/product-templates', { body, signal }), options)
    : await runContractRequest(/* signal 控制模板更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/library/product-templates/{template_id}', { params: { path: { template_id: id } }, body, signal }), options);
  return toTemplate(response);
};

/** deleteProductTemplate 删除商品模板。 */
export const deleteProductTemplate = async (id: number, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制模板删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/library/product-templates/{template_id}', { params: { path: { template_id: id } }, signal }), options);
};

/** previewTemplatePublish 把所选模板展开到所选账号并生成批量铺货预检，不访问平台。 */
export const previewTemplatePublish = async (input: {
  /** templateIds 是要发布的模板主键。 */ templateIds: number[];
  /** cookieIds 是目标账号标识。 */ cookieIds: string[];
  /** location 是可选发货地，缺省使用账号平台默认。 */ location?: LibraryPublishLocation;
  /** intervalSeconds 是发布间隔秒数。 */ intervalSeconds: number;
}, options?: RequestControlOptions): Promise<LibraryPublishPreview> => {
  // response 是预检传输对象。
  const response = await runContractRequest(/* signal 控制发布预检请求的取消和超时。 */ signal => contractClient.POST('/api/v1/library/product-templates/publish-preview', {
    body: {
      template_ids: input.templateIds, cookie_ids: input.cookieIds, publish_interval_seconds: input.intervalSeconds,
      ...(input.location ? { location: { province: input.location.province, city: input.location.city, area: input.location.area, division_id: input.location.divisionId } } : {}),
    },
    signal,
  }), options);
  return {
    previewId: response.preview_id, total: response.total, valid: response.valid, invalid: response.invalid,
    rows: response.rows.map(/* row 是预检传输行。 */ row => ({ rowNo: row.row_no, valid: row.valid, errors: row.errors ?? [], cookieId: row.cookie_id, title: row.title, price: row.price, quantity: row.quantity })),
  };
};

/** startPublishBatch 确认预检并启动既有的批量铺货批次，返回批次标识。 */
export const startPublishBatch = async (previewId: string, options?: RequestControlOptions): Promise<string> => {
  // response 是批次启动结果。
  const response = await runContractRequest(/* signal 控制批次启动请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches', { body: { preview_id: previewId } as never, signal }), options) as unknown as { /** batch_id 是新批次标识。 */ batch_id?: string; /** id 是兼容批次标识。 */ id?: string };
  return response.batch_id ?? response.id ?? '';
};

/** listCardOptions 读取可用于自动发货的卡密组。 */
export const listCardOptions = async (options?: RequestControlOptions): Promise<LibraryCardOption[]> => {
  // response 是卡密列表传输对象。
  const response = await runContractRequest(/* signal 控制卡密列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/cards', { signal }), options) as unknown as CardTransportForLibrary[] | { /** data 是兼容包装下的卡密列表。 */ data?: CardTransportForLibrary[] };
  // cards 是归一后的卡密数组。
  const cards = Array.isArray(response) ? response : response.data ?? [];
  return cards.filter(/* card 仅保留启用的卡密组。 */ card => card.enabled).map(/* card 转换为选项。 */ card => ({ id: card.id, name: card.name, type: card.type }));
};

/** listDeliveryTemplateOptions 读取可用于自动发货的发货模板。 */
export const listDeliveryTemplateOptions = async (options?: RequestControlOptions): Promise<LibraryDeliveryTemplateOption[]> => {
  // response 是发货模板列表传输对象。
  const response = await runContractRequest(/* signal 控制发货模板列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/delivery-templates', { signal }), options);
  return (response.data ?? []).filter(/* template 仅保留启用的模板。 */ template => template.enabled).map(/* template 转换为选项。 */ template => ({ id: template.id, name: template.name, keys: template.keys ?? [], customKeys: template.custom_keys ?? [] }));
};

/** listAccountOptions 读取可发布的账号摘要。 */
export const listAccountOptions = async (options?: RequestControlOptions): Promise<LibraryAccountOption[]> => {
  // response 是账号摘要传输对象。
  const response = await runContractRequest(/* signal 控制账号列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);
  return response.filter(/* account 仅保留启用且有凭证的账号。 */ account => account.enabled && account.has_cookie).map(/* account 转换为选项。 */ account => ({ id: account.id, label: account.remark || account.nickname || account.id }));
};
