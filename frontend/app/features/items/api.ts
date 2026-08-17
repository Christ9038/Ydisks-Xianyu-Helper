import { get, post, put, del, postForm, type RequestControlOptions } from '../../../shared/http/client';
import {
  SessionResponse, AccountDetail, AccountSummaryResponse, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, AccountTaskSettings, ChatSession, ChatMessage, ItemListEnvelope, AutomationIssuesEnvelope,
  CookieSettingsResponse, CookieProfileResponse, ItemDetailResponse, ItemPublishResponse, ItemSyncResponse, OrderDTOResponse, OrderDetailResponse, OrderSingleRefreshResponse, OrderBatchResponse, OrderRefreshResponse, OrderRefreshJobStartResponse, OrderRefreshJobStatusResponse, OrderRefreshJobCancelResponse, AutomationRuleResponse, AutomationRulePageResponse, AIReplySettingsResponse, AIModelsResponse, UserSettingResponse, CardBatchResponse, CardAppendResponse, CategoryRecommendationResponse, ItemPublishBatchPreviewResponse, ItemPublishBatchListResponse, BatchIDResponse, ItemPublishBatchResponse, BatchCancelResponse, MutationIDResponse, OperationResponse, NotificationChannelResponse, NotificationBinding, AccountBindingsResponse, CardListResponse, KeywordTypedResponse, DefaultReplyResponse, AccountTaskSettingsResponse, AccountTaskRunResponseEnvelope, AdminStatsResponse, DashboardStatsResponse, OrderAnalyticsResponse, QRLoginGenerateResponse, QRLoginStatusResponse, QRLoginVerificationResponse, ValidOrderResponse, ValidOrdersResponse
} from '../../../shared/api-contract';
import { collectionFrom, objectFrom } from '../../../shared/http/contract';
import { getPublishLocations } from '../../../services/amapLocation';
import type { PublishLocation } from '../../../shared/api-contract';

/** 商品账号筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => get('/api/v1/accounts/details', undefined, options);

/** 商品页面读取自动化发货规则的兼容列表。 */
export const getShippingRules = async (): Promise<ShippingRule[]> => get('/api/v1/automation-rules');

export { getPublishLocations };
export type { PublishLocation } from '../../../shared/api-contract';
// Items
// normalizeBooleanFlag 归一化布尔标记。
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

// getItems 读取商品列表。
export const getItems = async (cookieId?: string, options?: RequestControlOptions): Promise<Item[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await get<unknown>('/api/v1/items', cookieId ? { cookie_id: cookieId } : undefined, options);
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
    return post('/api/v1/items/get-all-from-account', { cookie_id: cookieId });
}

// deleteItem 删除商品。
export const deleteItem = async (cookieId: string, itemId: string): Promise<OperationResponse> => {
    return del(`/api/v1/items/${cookieId}/${itemId}`);
}

// createItem 创建商品。
export const createItem = async (cookieId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return post(`/api/v1/items/${cookieId}`, data);
}

// publishItem 发布商品。
export const publishItem = async (form: {
    /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
    /** title 表示标题。 */ title: string;
    /** description 表示描述。 */ description: string;
    /** price 表示售价。 */ price: string;
    /** original_price 表示原始售价。 */ original_price?: string;
    /** quantity 表示数量。 */ quantity: string | number;
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
    return postForm('/api/v1/items/publish', body);
}

// recommendPublishCategory 推荐商品发布分类。
export const recommendPublishCategory = async (cookieId: string, keyword: string): Promise<CategoryRecommendationResponse> => {
    // 类目推荐成功响应使用共享 CategoryRecommendationResponse。
    // category 字段保留平台类目 ID、名称和频道类目 ID。
    // tb_cat_id 继续保持可选，兼容电子资料类目。
    // 请求仍携带账号 ID 和关键词。
    // 失败响应由共享 HTTP 错误结构处理。
    // 该类型收口不改变凭证刷新和错误处理。
    // 前端批量发布流程可直接复用 category。
    // 旧路径继续由现有 Vite 代理转发。
    return post('/api/v1/items/publish-categories/recommend', { cookie_id: cookieId, keyword });
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
}): Promise<ItemPublishBatchPreviewResponse> => {
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
    return postForm('/api/v1/items/publish-batches/preview', body);
}

// startItemPublishBatch 启动商品批量发布。
export const startItemPublishBatch = async (previewId: string): Promise<BatchIDResponse> => {
    return post('/api/v1/items/publish-batches', { preview_id: previewId });
}

// getItemPublishBatch 读取商品发布批次。
export const getItemPublishBatch = async (batchId: string): Promise<ItemPublishBatchResponse> => {
    return get(`/api/v1/items/publish-batches/${batchId}`);
}

// getItemPublishBatches 读取商品发布批次列表。
export const getItemPublishBatches = async (limit = 20): Promise<ItemPublishBatchResponse[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await get<unknown>('/api/v1/items/publish-batches', { limit });
    return collectionFrom<ItemPublishBatchResponse>(res, ['batches', 'data', 'items']);
}

// deleteItemPublishBatch 删除商品发布批次。
export const deleteItemPublishBatch = async (batchId: string): Promise<OperationResponse> => {
    return del(`/api/v1/items/publish-batches/${batchId}`);
}

// cancelItemPublishBatch 取消商品发布批次。
export const cancelItemPublishBatch = async (batchId: string): Promise<BatchCancelResponse> => {
    return post(`/api/v1/items/publish-batches/${batchId}/cancel`, {});
}

// retryFailedItemPublishBatch 重试失败的商品发布任务。
export const retryFailedItemPublishBatch = async (batchId: string): Promise<BatchIDResponse> => {
    return post(`/api/v1/items/publish-batches/${batchId}/retry-failed`, {});
}

// updateItem 更新商品。
export const updateItem = async (cookieId: string, itemId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return put(`/api/v1/items/${cookieId}/${itemId}`, data);
}

/** 读取指定账号与商品的详情，用于编辑器恢复表单。 */
export const getItemDetail = async (accountID: string, itemID: string): Promise<ItemDetailResponse> => get(`/api/v1/items/${accountID}/${itemID}`);
