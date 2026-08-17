import { get, post, put, del, postForm, type RequestControlOptions } from '../../../shared/http/client';
import {
  SessionResponse, AccountDetail, AccountSummaryResponse, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, AccountTaskSettings, ChatSession, ChatMessage, ItemListEnvelope, AutomationIssuesEnvelope,
  CookieSettingsResponse, CookieProfileResponse, ItemDetailResponse, ItemPublishResponse, ItemSyncResponse, OrderDTOResponse, OrderDetailResponse, OrderSingleRefreshResponse, OrderBatchResponse, OrderRefreshResponse, OrderRefreshJobStartResponse, OrderRefreshJobStatusResponse, OrderRefreshJobCancelResponse, AutomationRuleResponse, AutomationRulePageResponse, AIReplySettingsResponse, AIModelsResponse, UserSettingResponse, CardBatchResponse, CardAppendResponse, CategoryRecommendationResponse, ItemPublishBatchPreviewResponse, ItemPublishBatchListResponse, BatchIDResponse, ItemPublishBatchResponse, BatchCancelResponse, MutationIDResponse, OperationResponse, NotificationChannelResponse, NotificationBinding, AccountBindingsResponse, CardListResponse, KeywordTypedResponse, DefaultReplyResponse, AccountTaskSettingsResponse, AccountTaskRunResponseEnvelope, AdminStatsResponse, DashboardStatsResponse, OrderAnalyticsResponse, QRLoginGenerateResponse, QRLoginStatusResponse, QRLoginVerificationResponse, ValidOrderResponse, ValidOrdersResponse
} from '../../../shared/api-contract';
import { collectionFrom, objectFrom } from '../../../shared/http/contract';
// Cards
// normalizeCard 归一化卡密数据。
const normalizeCard = (item: any): Card => {
  // apiConfig 卡密接口配置，用于当前 API 处理流程。
  let apiConfig = item.api_config;
  if (typeof apiConfig === 'string' && apiConfig.trim()) {
    try {
      apiConfig = JSON.parse(apiConfig);
    } catch {
      apiConfig = undefined;
    }
  }
  return {...item, api_config: apiConfig || undefined} as Card;
};

// cardPayload 卡密请求载荷，用于当前 API 处理流程。
const cardPayload = (data: Partial<Card>): Record<string, unknown> => ({
  ...data,
  api_config: data.api_config ? JSON.stringify(data.api_config) : '',
});

// getCards 读取卡密列表。
export const getCards = async (options?: RequestControlOptions): Promise<Card[]> => {
  // res 接口响应结果，用于当前 API 处理流程。
  const res = await get<unknown>('/api/v1/cards', undefined, options);
  // cards 卡密列表，用于当前 API 处理流程。
  const cards = collectionFrom<Card>(res, ['cards', 'data', 'items']);
  return cards.map(normalizeCard);
};

// createCard 创建卡密组。
export const createCard = async (data: Partial<Card>): Promise<MutationIDResponse> => {
  return post('/api/v1/cards', cardPayload(data));
};

// updateCard 更新卡密组。
export const updateCard = async (cardId: string | number, data: Partial<Card>): Promise<OperationResponse> => {
  return put(`/api/v1/cards/${cardId}`, cardPayload(data));
};

// deleteCard 删除卡密组。
export const deleteCard = async (cardId: string | number): Promise<OperationResponse> => {
  return del(`/api/v1/cards/${cardId}`);
};

// getCardDetails 读取卡密组详情。
export const getCardDetails = async (cardId: string | number): Promise<Card> => {
  // card 卡密详情，用于当前 API 处理流程。
  const card = await get<Card>(`/api/v1/cards/${cardId}/details`);
  return normalizeCard(card);
};

// 批量创建卡密组（上传表格）
export const batchCreateCards = async (file: File, options?: RequestControlOptions): Promise<CardBatchResponse> => {
  // 批量创建接口返回总行数、成功数、失败数和逐行结果。
  // CardBatchResponse 保留旧字段名称，调用方无需转换统计字段。
  // rows 中的 id 只在创建成功时返回。
  // rows 中的 error 只在对应行失败时返回。
  // 表单上传方式和接口路径保持不变。
  // 此处只收紧 TypeScript 响应契约。
  const body = new FormData();
  body.append('file', file);
  return postForm('/api/v1/cards/batch', body, options);
};

// 往 data 类型卡密组批量追加卡密号
export const appendCardData = async (cardId: string | number, content: string, options?: RequestControlOptions): Promise<CardAppendResponse> => {
  return post(`/api/v1/cards/${cardId}/append-data`, { content }, options);
};
