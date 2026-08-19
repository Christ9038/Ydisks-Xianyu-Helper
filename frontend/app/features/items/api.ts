import type { PublishLocation } from './models';
import {
AccountDetail,
BatchCancelResponse,
BatchIDResponse,
CategoryRecommendationResponse,
Item,
ItemDetailResponse,
ItemPublishBatchPreviewResponse,
ItemPublishBatchResponse,
ItemPublishResponse,ItemSyncResponse,
OperationResponse,
ShippingRule
} from './models';
import { ApiError, type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom } from '../../../shared/http/contract';
export type * from './models';
import { getPublishLocations as queryPublishLocations,type PublishLocationRequestOptions } from './amapLocation';

/** 商品账号筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => runContractRequest(/* signal 控制商品页账号摘要请求的取消和超时。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);

/** 商品页面读取自动化发货规则的兼容列表。 */
export const getShippingRules = async (options?: RequestControlOptions): Promise<ShippingRule[]> => runContractRequest(/* signal 控制商品页规则读取请求的取消和超时。 */ signal => contractClient.GET('/api/v1/automation-rules', { signal }), options) as unknown as Promise<ShippingRule[]>;

// getPublishLocations 通过 feature API 边界读取地点，并把取消/超时控制传入地图服务。
export const getPublishLocations = (longitude: number, latitude: number, options?: PublishLocationRequestOptions): Promise<PublishLocation[]> => queryPublishLocations(longitude, latitude, options);
export type { PublishLocation } from './models';

// itemErrorMessage 将统一 HTTP 错误码归一为商品 feature 可执行的用户提示。
export const itemErrorMessage = (error: unknown, fallback: string): string => {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'stock_permission_missing':
        return '发布失败：该账号没有库存发布权限，无法按库存数量发布商品。请换账号或先在闲鱼确认库存能力。';
      case 'conflict':
      case 'item_conflict':
        return '商品状态已发生变化，请刷新后重试。';
      case 'external_result_unknown':
      case 'manual_review_required':
        return '平台结果暂时无法确认，请先人工核对闲鱼状态，再决定是否重试。';
      case 'retryable':
      case 'temporarily_unavailable':
        return '平台暂时不可用，请稍后重试。';
      default:
        return error.message || fallback;
    }
  }
  if (error instanceof Error) return error.message;
  // message 保存兼容 API 错误对象中的文本说明。
  const message = typeof error === 'object' && error !== null && 'message' in error ? (error as { /** message 是兼容错误对象中的文本说明。 */ message?: unknown }).message : undefined;
  return typeof message === 'string' ? message : fallback;
};
// Items
// normalizeBooleanFlag 归一化布尔标记。
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

// getItems 读取商品列表。
export const getItems = async (cookieId?: string, options?: RequestControlOptions): Promise<Item[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制商品列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items', { params: { query: { cookie_id: cookieId } }, signal }), options) as unknown;
    // items 商品列表，用于当前 API 处理流程。
    const items = collectionFrom<Item>(res, ['items', 'data', 'results']);
    return items.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
      ...item,
      id: item.id || `${item.cookie_id}-${item.item_id}`,
      is_multi_spec: normalizeBooleanFlag(item.is_multi_spec),
      is_multi_qty_ship: normalizeBooleanFlag(item.is_multi_qty_ship ?? item.multi_quantity_delivery),
      multi_quantity_delivery: normalizeBooleanFlag(item.multi_quantity_delivery ?? item.is_multi_qty_ship),
    }));
}

// syncItemsFromAccount 从账号同步商品。
export const syncItemsFromAccount = async (cookieId: string): Promise<ItemSyncResponse> => {
    return runContractRequest(/* signal 控制商品同步请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/get-all-from-account', { body: { cookie_id: cookieId } as never, signal })) as unknown as Promise<ItemSyncResponse>;
}

// deleteItem 删除商品。
export const deleteItem = async (cookieId: string, itemId: string): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: cookieId, item_id: itemId } }, signal }));
}

// createItem 创建商品。
export const createItem = async (cookieId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/{cookie_id}', { params: { path: { cookie_id: cookieId } }, body: data as never, signal }));
}

// publishItem 发布商品。
export const publishItem = async (form: {
    /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
    /** title 表示标题。 */ title: string;
    /** description 表示描述。 */ description: string;
    /** price 表示售价。 */ price: string;
    /** original_price 表示原始售价。 */ original_price?: string;
    /** quantity 表示待发布商品的件数，提交前会转换为表单字符串。 */ quantity: string | number;
    /** postage_mode 表示运费模式。 */ postage_mode: string;
    /** postage 表示运费。 */ postage?: string;
    /** images 表示图片列表。 */ images: File[];
	/** location 表示地址。 */ location?: PublishLocation;
}): Promise<ItemPublishResponse> => {
    // body 请求体，用于当前 API 处理流程。
    const body = new FormData();
    body.set('cookie_id', form.cookie_id);
    body.set('title', form.title);
    body.set('description', form.description);
    body.set('price', form.price);
    body.set('original_price', form.original_price || '');
    body.set('quantity', String(form.quantity));
    body.set('postage_mode', form.postage_mode);
    body.set('postage', form.postage || '');
	if (form.location) body.set('location', JSON.stringify(form.location));
    for (const // file 上传文件，用于当前 API 处理流程。
file of form.images) {
      body.append('images', file);
    }
    return runContractRequest(/* signal 控制商品发布上传请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish', { body: body as never, signal })) as unknown as Promise<ItemPublishResponse>;
}

// recommendPublishCategory 推荐商品发布分类。
export const recommendPublishCategory = async (cookieId: string, keyword: string, options?: RequestControlOptions): Promise<CategoryRecommendationResponse> => {
    // 类目推荐成功响应使用共享 CategoryRecommendationResponse。
    // category 字段保留平台类目 ID、名称和频道类目 ID。
    // tb_cat_id 继续保持可选，兼容电子资料类目。
    // 请求仍携带账号 ID 和关键词。
    // 失败响应由共享 HTTP 错误结构处理。
    // 该类型收口不改变凭证刷新和错误处理。
    // 前端批量发布流程可直接复用 category。
    // 旧路径继续由现有 Vite 代理转发。
	return runContractRequest(/* signal 控制商品类目推荐请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-categories/recommend', { body: { cookie_id: cookieId, keyword } as never, signal }), options) as unknown as Promise<CategoryRecommendationResponse>;
};

// previewItemPublishBatch 预览商品批量发布。
export const previewItemPublishBatch = async (form: {
    /** file 表示上传文件。 */ file: File;
    /** imagesZip 表示图片压缩包。 */ imagesZip?: File | null;
    /** defaultCookieId 表示默认账号凭证标识。 */ defaultCookieId?: string;
    /** fallbackCategory 表示备用分类。 */ fallbackCategory: {
      /** catId 表示分类标识。 */ catId: string;
      /** catName 表示分类名称。 */ catName: string;
      /** channelCatId 表示渠道分类标识。 */ channelCatId?: string;
      /** tbCatId 表示淘宝分类标识。 */ tbCatId?: string;
    };
	/** location 表示地址。 */ location?: PublishLocation;
	/** publishIntervalSeconds 表示最终商品发布之间的最小间隔秒数。 */
	publishIntervalSeconds?: number;
}, options?: RequestControlOptions): Promise<ItemPublishBatchPreviewResponse> => {
    // body 请求体，用于当前 API 处理流程。
    const body = new FormData();
    body.set('file', form.file);
    if (form.imagesZip) body.set('images_zip', form.imagesZip);
    if (form.defaultCookieId) body.set('default_cookie_id', form.defaultCookieId);
    body.set('fallback_category_id', form.fallbackCategory.catId);
    body.set('fallback_category_name', form.fallbackCategory.catName);
    body.set('fallback_channel_category_id', form.fallbackCategory.channelCatId || '');
    body.set('fallback_tb_category_id', form.fallbackCategory.tbCatId || '');
	if (form.location) body.set('location', JSON.stringify(form.location));
	body.set('publish_interval_seconds', String(form.publishIntervalSeconds ?? 5));
	return runContractRequest(/* signal 控制批量发布预览上传的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/preview', { body: body as never, signal }), options) as unknown as Promise<ItemPublishBatchPreviewResponse>;
}

// startItemPublishBatch 启动商品批量发布。
export const startItemPublishBatch = async (previewId: string, options?: RequestControlOptions): Promise<BatchIDResponse> => {
	return runContractRequest(/* signal 控制批量发布启动请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches', { body: { preview_id: previewId } as never, signal }), options) as unknown as Promise<BatchIDResponse>;
}

// getItemPublishBatch 读取商品发布批次。
export const getItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<ItemPublishBatchResponse> => {
	return runContractRequest(/* signal 控制批量发布状态读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/publish-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options) as unknown as Promise<ItemPublishBatchResponse>;
}

// getItemPublishBatches 读取商品发布批次列表。
export const getItemPublishBatches = async (limit = 20, options?: RequestControlOptions): Promise<ItemPublishBatchResponse[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制批量发布列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/publish-batches', { params: { query: { limit } }, signal }), options) as unknown;
    return collectionFrom<ItemPublishBatchResponse>(res, ['batches', 'data', 'items']);
}

// deleteItemPublishBatch 删除商品发布批次。
export const deleteItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<OperationResponse> => {
	return runContractRequest(/* signal 控制批量发布删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/items/publish-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options);
}

// cancelItemPublishBatch 取消商品发布批次。
export const cancelItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<BatchCancelResponse> => {
	return runContractRequest(/* signal 控制批量发布取消请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/{batch_id}/cancel', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<BatchCancelResponse>;
}

// retryFailedItemPublishBatch 重试失败的商品发布任务。
export const retryFailedItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<BatchIDResponse> => {
	return runContractRequest(/* signal 控制批量发布重试请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/{batch_id}/retry-failed', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<BatchIDResponse>;
}

// updateItem 更新商品。
export const updateItem = async (cookieId: string, itemId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: cookieId, item_id: itemId } }, body: data as never, signal }));
}

/** 读取指定账号与商品的详情，用于编辑器恢复表单，并将生成传输 DTO 保持在 feature adapter 边界。 */
export const getItemDetail = async (accountID: string, itemID: string): Promise<ItemDetailResponse> => {
  // response 保存已由 OpenAPI 路径类型约束的商品详情传输响应。
  const response = await runContractRequest(/* signal 控制商品详情请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: accountID, item_id: itemID } }, signal }));
  return response;
};
