import { get, post, put, del, postForm, type RequestControlOptions } from '../../../shared/http/client';
import {
  SessionResponse, AccountDetail, AccountSummaryResponse, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, AccountTaskSettings, ChatSession, ChatMessage, ItemListEnvelope, AutomationIssuesEnvelope,
  CookieSettingsResponse, CookieProfileResponse, ItemDetailResponse, ItemPublishResponse, ItemSyncResponse, OrderDTOResponse, OrderDetailResponse, OrderSingleRefreshResponse, OrderBatchResponse, OrderRefreshResponse, OrderRefreshJobStartResponse, OrderRefreshJobStatusResponse, OrderRefreshJobCancelResponse, AutomationRuleResponse, AutomationRulePageResponse, AIReplySettingsResponse, AIModelsResponse, UserSettingResponse, CardBatchResponse, CardAppendResponse, CategoryRecommendationResponse, ItemPublishBatchPreviewResponse, ItemPublishBatchListResponse, BatchIDResponse, ItemPublishBatchResponse, BatchCancelResponse, MutationIDResponse, OperationResponse, NotificationChannelResponse, NotificationBinding, AccountBindingsResponse, CardListResponse, KeywordTypedResponse, DefaultReplyResponse, AccountTaskSettingsResponse, AccountTaskRunResponseEnvelope, AdminStatsResponse, DashboardStatsResponse, OrderAnalyticsResponse, QRLoginGenerateResponse, QRLoginStatusResponse, QRLoginVerificationResponse, ValidOrderResponse, ValidOrdersResponse
} from '../../../shared/api-contract';
import { collectionFrom, objectFrom } from '../../../shared/http/contract';

/** 聊天账号选择器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => get('/api/v1/accounts/details', undefined, options);

/** 聊天运行提示读取账号连接状态索引。 */
export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, { /** 当前连接状态。 */ state: NonNullable<AccountDetail['runtime_state']>; /** 状态说明。 */ message?: string; /** 是否已连接。 */ connected: boolean; /** 连续失败次数。 */ failures: number; /** 最近更新时间。 */ updated_at: string }>> => get('/api/v1/accounts/runtime-status', undefined, options);
export interface ChatSessionPage { /** sessions 表示聊天会话列表。 */ sessions: ChatSession[]; /** has_more 表示是否存在更多数据。 */ has_more: boolean; /** next_cursor 表示下一页游标。 */ next_cursor?: number }

// getChatSessionPage 分页读取聊天会话。
export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false): Promise<ChatSessionPage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await get<unknown>('/api/v1/chat/sessions', { account_id: accountId, cursor, refresh: refresh ? 1 : undefined },
		refresh ? { timeoutMs: 60_000, ...options } : options);
	// result 是兼容直接分页对象和 data 包裹后的聊天会话分页。
	const result = objectFrom<Partial<ChatSessionPage>>(response, ['data', 'result']) || {};
	return { sessions: result.sessions || [], has_more: result.has_more === true, next_cursor: result.next_cursor };
};

// getChatSessions 读取聊天会话列表。
export const getChatSessions = async (accountId: string, options?: RequestControlOptions): Promise<ChatSession[]> =>
	(await getChatSessionPage(accountId, undefined, options)).sessions;

export interface ChatMessagePage {
	/** messages 表示聊天消息列表。 */ messages: ChatMessage[];
	/** has_more 表示是否存在更多数据。 */ has_more: boolean;
	/** next_cursor 表示下一页游标。 */ next_cursor?: number;
	/** session 表示会话。 */ session?: ChatSession;
}

// getChatMessagePage 分页读取聊天消息。
export const getChatMessagePage = async (accountId: string, chatId: string, cursor?: number, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessagePage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await get<unknown>('/api/v1/chat/messages', {
		account_id: accountId, chat_id: chatId, cursor, before_id: beforeId,
	}, options);
	// result 是兼容直接分页对象和 data 包裹后的聊天消息分页。
	const result = objectFrom<Partial<ChatMessagePage>>(response, ['data', 'result']) || {};
	return { messages: result.messages || [], has_more: result.has_more === true, next_cursor: result.next_cursor, session: result.session };
};

// getChatMessages 读取聊天消息列表。
export const getChatMessages = async (accountId: string, chatId: string, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessage[]> =>
	(await getChatMessagePage(accountId, chatId, undefined, beforeId, options)).messages;

// sendChatMessage 发送聊天文本消息。
export const sendChatMessage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** buyer_id 表示买家标识。 */ buyer_id: string; /** buyer_name 表示买家名称。 */ buyer_name?: string;
  /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** text 表示文本。 */ text: string;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> => post('/api/v1/chat/messages', input, options);

// sendChatImage 发送聊天图片消息。
export const sendChatImage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** buyer_id 表示买家标识。 */ buyer_id: string; /** buyer_name 表示买家名称。 */ buyer_name?: string;
  /** buyer_avatar_url 表示买家头像地址。 */ buyer_avatar_url?: string; /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** image 表示图片数据。 */ image: File;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> => {
	// form 消息表单，用于当前 API 处理流程。
	const form = new FormData();
	Object.entries(input).forEach(/* 当前回调用于处理集合元素或接口响应。 */ ([key, value]) => form.append(key, value));
	return postForm('/api/v1/chat/images', form, { timeoutMs: 120_000, ...options });
};

// markChatRead 标记聊天消息已读。
export const markChatRead = async (accountId: string, chatId: string, options?: RequestControlOptions): Promise<OperationResponse> =>
	post('/api/v1/chat/read', { account_id: accountId, chat_id: chatId }, options);
