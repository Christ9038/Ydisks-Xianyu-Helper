import { get, post, put, del, postForm, type RequestControlOptions } from '../request';
import {
  LoginResponse, AccountDetail, AccountSummaryResponse, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, ApiResponse, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, AccountTaskSettings, ChatSession, ChatMessage, ItemListEnvelope, AutomationIssuesEnvelope,
  CookieSettingsResponse, CookieProfileResponse, ItemDetailResponse, ItemPublishResponse, ItemSyncResponse, OrderDTOResponse, OrderDetailResponse, OrderSingleRefreshResponse, OrderBatchResponse, OrderRefreshResponse, AutomationRuleResponse, AutomationRulePageResponse, AIReplySettingsResponse, AIModelsResponse, UserSettingResponse, CardBatchResponse, CardAppendResponse, CategoryRecommendationResponse, ItemPublishBatchPreviewResponse, ItemPublishBatchListResponse, BatchIDResponse, ItemPublishBatchResponse, BatchCancelResponse, MutationIDResponse, OperationResponse, NotificationChannelResponse, NotificationBinding, AccountBindingsResponse, CardListResponse, KeywordTypedResponse, DefaultReplyResponse, AccountTaskSettingsResponse, AccountTaskRunResponseEnvelope, AdminStatsResponse, DashboardStatsResponse, OrderAnalyticsResponse, QRLoginGenerateResponse, QRLoginStatusResponse, QRLoginVerificationResponse, ValidOrderResponse, ValidOrdersResponse
} from '../types';
import { formatLocalDate } from '../dateRange';

const normalizeSettings = (settings: Record<string, any>): SystemSettings => {
  const out: Record<string, any> = { ...settings };
  if ('renewal_log_retention_days' in out) {
    const parsed = Number(out.renewal_log_retention_days);
    out.renewal_log_retention_days = Number.isFinite(parsed) ? parsed : 10;
  }
  return out as SystemSettings;
};

// Auth
export const login = async (data: { username?: string; password?: string; email?: string; verification_code?: string }): Promise<LoginResponse> => {
  return post('/api/v1/session/login', data, { skipAuthLogout: true });
};

export const initializeAdmin = async (password: string): Promise<LoginResponse> => {
  return post('/api/v1/session/initialize', { password }, { skipAuthLogout: true });
};

export const verifySession = async (): Promise<{ authenticated: boolean; initialized?: boolean; user_id?: number; username?: string; is_admin?: boolean }> => {
  return get('/api/v1/session');
};

export const logout = async (): Promise<ApiResponse> => {
  return post('/api/v1/session/logout', {});
};

export const changePassword = async (currentPassword: string, newPassword: string): Promise<ApiResponse> => {
  return post('/change-password', { current_password: currentPassword, new_password: newPassword });
};

export const updateLoginCredentials = async (data: {
  current_password: string;
  new_username: string;
  new_password?: string;
}): Promise<ApiResponse & { requires_relogin?: boolean }> => {
  return put('/account/credentials', data);
};

// Accounts
export const addAccount = async (id: string, value: string, loginMethod?: string): Promise<ApiResponse> => {
  return post('/api/v1/accounts', { id, value, login_method: loginMethod });
};

const accountAvatarURL = (item: AccountSummaryResponse, version: string): string => {
  const raw = item.avatar_url || '';
  if (!raw) return '';

  try {
    const url = new URL(raw, window.location.origin);
    if (url.hostname.endsWith('alicdn.com')) {
      url.searchParams.set('_v', version);
    }
    return url.toString();
  } catch {
    return raw;
  }
};

export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => {
  const data = await get<AccountSummaryResponse[]>('/api/v1/accounts/details', undefined, options);
  const avatarVersion = Date.now().toString();
  return data.map(item => ({
    id: item.id,
    value: '',
    cookie: '',
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
    note: item.remark,
    pause_duration: item.pause_duration,
    paused_until: Number(item.paused_until || 0),
    paused: item.paused === true,
    username: item.username || '',
    login_password: '',
    show_browser: item.show_browser === true || item.show_browser === 1 || item.show_browser === '1' || item.show_browser === 'true',
    nickname: item.nickname || item.remark || `账号 ${item.id.substring(0,6)}`,
    avatar_url: accountAvatarURL(item, avatarVersion),
    profile_error: item.profile_error || '',
    ai_enabled: false,
		auto_rate_enabled: item.auto_rate_enabled,
		rate_content: item.rate_content || '不错的买家，交易愉快',
		auto_polish_enabled: item.auto_polish_enabled,
		polish_time: item.polish_time || '03:00',
		last_rate_scan_at: Number(item.last_rate_scan_at || 0),
		last_polish_date: item.last_polish_date || '',
		last_polish_at: Number(item.last_polish_at || 0),
  }));
};

export const getAccountTaskSettings = async (id: string): Promise<AccountTaskSettingsResponse> =>
	get(`/api/v1/account-tasks/${id}`);

export const updateAccountTaskSettings = async (id: string, settings: AccountTaskSettings): Promise<AccountTaskSettingsResponse> =>
	put(`/api/v1/account-tasks/${id}`, settings);

export const runAccountTask = async (id: string, taskType: 'auto_rate' | 'auto_polish'): Promise<AccountTaskRunResponseEnvelope> =>
	post(`/api/v1/account-tasks/${id}/run`, { task_type: taskType }, { timeoutMs: 120_000 });

export interface ChatSessionPage { sessions: ChatSession[]; has_more: boolean; next_cursor?: number }

export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false): Promise<ChatSessionPage> => {
	const result = await get<ChatSessionPage>('/api/v1/chat/sessions', { account_id: accountId, cursor, refresh: refresh ? 1 : undefined },
		refresh ? { timeoutMs: 60_000, ...options } : options);
	return { sessions: result.sessions || [], has_more: result.has_more === true, next_cursor: result.next_cursor };
};

export const getChatSessions = async (accountId: string, options?: RequestControlOptions): Promise<ChatSession[]> =>
	(await getChatSessionPage(accountId, undefined, options)).sessions;

export interface ChatMessagePage {
	messages: ChatMessage[];
	has_more: boolean;
	next_cursor?: number;
	session?: ChatSession;
}

export const getChatMessagePage = async (accountId: string, chatId: string, cursor?: number, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessagePage> => {
	const result = await get<ChatMessagePage>('/api/v1/chat/messages', {
		account_id: accountId, chat_id: chatId, cursor, before_id: beforeId,
	}, options);
	return { messages: result.messages || [], has_more: result.has_more === true, next_cursor: result.next_cursor, session: result.session };
};

export const getChatMessages = async (accountId: string, chatId: string, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessage[]> =>
	(await getChatMessagePage(accountId, chatId, undefined, beforeId, options)).messages;

export const sendChatMessage = async (input: {
	account_id: string; chat_id: string; buyer_id: string; buyer_name?: string;
	item_id?: string; item_title?: string; text: string;
}): Promise<{message: ChatMessage}> => post('/api/v1/chat/messages', input);

export const sendChatImage = async (input: {
	account_id: string; chat_id: string; buyer_id: string; buyer_name?: string;
	buyer_avatar_url?: string; item_id?: string; item_title?: string; image: File;
}): Promise<{message: ChatMessage}> => {
	const form = new FormData();
	Object.entries(input).forEach(([key, value]) => form.append(key, value));
	return postForm('/api/v1/chat/images', form, { timeoutMs: 120_000 });
};

export const markChatRead = async (accountId: string, chatId: string): Promise<ApiResponse> =>
	post('/api/v1/chat/read', { account_id: accountId, chat_id: chatId });

export interface AccountRuntimeStatus {
  state: NonNullable<AccountDetail['runtime_state']>;
  message?: string;
  connected: boolean;
  failures: number;
  updated_at: string;
}

export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, AccountRuntimeStatus>> => {
  return get('/api/v1/accounts/runtime-status', undefined, options);
};

export const generateQRLogin = async (options?: RequestControlOptions): Promise<QRLoginGenerateResponse> => {
  // 风控后匿名 token 接口可能超过通用的 30 秒请求窗口；后端总生成窗口为 2 分钟。
  return post('/api/v1/qr-login/generate', undefined, { ...options, timeoutMs: options?.timeoutMs ?? 130_000 });
};

export const checkQRLoginStatus = async (sessionId: string, signal?: AbortSignal): Promise<QRLoginStatusResponse> => {
  return get(`/api/v1/qr-login/check/${sessionId}`, undefined, { signal, timeoutMs: 10_000 });
};

export const completeQRVerification = async (
  sessionId: string,
  targetAccountId?: string,
): Promise<QRLoginVerificationResponse> => {
  return post(`/api/v1/qr-login/complete-verification/${sessionId}`, {
    target_account_id: targetAccountId || '',
  });
};






export const updateAccountStatus = async (id: string, enabled: boolean): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/status`, { enabled });
};

export const deleteAccount = async (id: string): Promise<OperationResponse> => {
  return del(`/cookies/${id}`);
};

export const updateAccountRemark = async (id: string, remark: string): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/remark`, { remark });
};

export const updateAccountAutoConfirm = async (id: string, autoConfirm: boolean): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/auto-confirm`, { auto_confirm: autoConfirm });
};

export const updateAccountPauseDuration = async (id: string, pauseDuration: number): Promise<CookieSettingsResponse> => {
  return put(`/api/v1/accounts/${id}/pause-duration`, { pause_duration: pauseDuration });
};

export const updateAccountCookie = async (id: string, value: string, loginMethod?: string): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}`, { id, value, login_method: loginMethod });
};

export interface AccountSettingsUpdate {
  cookie?: string;
  remark?: string;
  auto_confirm?: boolean;
  pause_duration?: number;
  username?: string;
  login_password?: string;
  clear_password?: boolean;
  show_browser?: boolean;
  channel_ids?: number[];
}

export const updateAccountSettings = async (id: string, data: AccountSettingsUpdate): Promise<CookieSettingsResponse> => {
  return put(`/api/v1/accounts/${id}/settings`, data);
};

export interface LongLoginSettings {
  can_open_long_login: boolean;
  enabled: boolean;
}

export const getLongLoginSettings = async (id: string): Promise<LongLoginSettings> => {
  return get(`/api/v1/accounts/${id}/long-login`);
};

export const setLongLoginSettings = async (id: string, enabled: boolean): Promise<LongLoginSettings> => {
  return put(`/api/v1/accounts/${id}/long-login`, { enabled });
};

export interface PasswordLoginStartResponse {
  success: boolean;
  session_id?: string;
  status?: 'processing' | 'failed';
  message?: string;
}

export interface PasswordLoginStatusResponse {
  status: 'processing' | 'success' | 'failed' | 'verification_required' | 'not_found' | 'error';
  message?: string;
  account_id?: string;
  is_new_account?: boolean;
  cookie_count?: number;
  verification_url?: string;
  screenshot_path?: string;
  qr_code_url?: string;
  error?: string;
  reason?: string;
}

export const passwordLogin = async (data: {
  account_id: string;
  account: string;
  password: string;
  show_browser?: boolean;
}): Promise<PasswordLoginStartResponse> => {
  return post('/password-login', data);
};

export const checkPasswordLoginStatus = async (sessionId: string, signal?: AbortSignal): Promise<PasswordLoginStatusResponse> => {
  return get(`/password-login/check/${sessionId}`, undefined, { signal, timeoutMs: 10_000 });
};

export const cancelPasswordLogin = async (sessionId: string): Promise<ApiResponse> => {
  return del(`/password-login/cancel/${sessionId}`);
};

export const refreshAccountProfile = async (id: string): Promise<CookieProfileResponse> => {
  return post(`/api/v1/accounts/${id}/refresh-profile`, {});
};

export const updateAccountLoginInfo = async (id: string, data: {
  username?: string;
  login_password?: string;
  clear_password?: boolean;
  show_browser?: boolean;
}): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/login-info`, data);
};

export const getAllAISettings = async (options?: RequestControlOptions): Promise<Record<string, AIReplySettings>> => {
  return get('/api/v1/settings/ai-reply', undefined, options);
};

// Orders
const normalizeOrderStatus = (value: unknown): Order['status'] => {
  const status = String(value || '');
  if (status === 'paid') return 'pending_ship';
  return ['processing', 'pending_ship', 'shipped', 'completed', 'cancelled', 'refunding'].includes(status)
    ? status as Order['status']
    : 'unknown';
};

export const getOrders = async (
  cookieId?: string,
  status?: string,
  page: number = 1,
  pageSize: number = 20,
  search?: string,
): Promise<PaginatedResponse<Order>> => {
  const params: any = { page, page_size: pageSize };
  if (cookieId) params.cookie_id = cookieId;
  if (status && status !== 'all') params.status = status;
  if (search?.trim()) params.search = search.trim();

  const res = await get<PaginatedResponse<Order> & { orders?: Order[] /* 后端兼容的订单列表字段。 */ }>('/api/v1/orders', params);

  // Handle backend response variations
  const rawOrders = res.orders || res.data || [];
  const orders = rawOrders.map((item: any) => ({
    ...item,
    id: item.id || item.order_id,
    status: normalizeOrderStatus(item.status || item.order_status),
    quantity: Number(item.quantity || 1),
  }));
  return {
    success: true,
    data: orders,
    total: res.total || orders.length,
    page: res.page || page,
    page_size: res.page_size || pageSize,
    total_pages: res.total_pages || 1
  };
};

export const getOrderDetail = async (orderId: string): Promise<{ success: boolean; data?: OrderDTOResponse }> => {
  const result = await get<OrderDetailResponse>(`/api/v1/orders/${orderId}`);
  return {
    success: true,
    data: result.data
  };
};

export const updateOrder = async (orderId: string, data: Partial<Order>): Promise<ApiResponse> => {
  return put(`/api/v1/orders/${orderId}`, data);
};

export const deleteOrder = async (orderId: string): Promise<ApiResponse> => {
  return del(`/api/orders/${orderId}`);
};

export const syncOrders = async (cookieId?: string, status?: string): Promise<OrderRefreshResponse> => {
  const formData = new FormData();
  if (cookieId) formData.append('cookie_id', cookieId);
  if (status) formData.append('status', status);

	return postForm('/api/v1/orders/refresh', formData);
};

export const syncSingleOrder = async (orderId: string): Promise<OrderSingleRefreshResponse> => {
  return post(`/api/v1/orders/${orderId}/refresh`);
};

export const manualShipOrder = async (orderIds: string[], shipMode: 'status_only' | 'full_delivery'): Promise<OrderBatchResponse> => {
    return post('/api/v1/orders/manual-ship', {
        order_ids: orderIds,
        ship_mode: shipMode,
    });
}

export const importOrders = async (data: Partial<Order>[] | FormData): Promise<OrderBatchResponse> => {
	const isFormData = data instanceof FormData;
	return isFormData ? postForm('/api/v1/orders/import', data) : post('/api/v1/orders/import', data);
}

// Stats
export const getAdminStats = async (): Promise<AdminStatsResponse> => {
  return get('/api/v1/admin/stats');
};

export const getDashboardStats = async (): Promise<DashboardStatsResponse> => {
  return get('/api/v1/analytics/dashboard');
};

export const getOrderAnalytics = async (daysOrParams: number | {start_date: string; end_date: string} = 7): Promise<OrderAnalyticsResponse> => {
    let params: {start_date: string; end_date: string};

    if (typeof daysOrParams === 'number') {
        const endDate = new Date();
        const startDate = new Date();
        startDate.setDate(startDate.getDate() - daysOrParams);
        params = {
            start_date: formatLocalDate(startDate),
            end_date: formatLocalDate(endDate)
        };
    } else {
        params = daysOrParams;
    }

    return get('/api/v1/analytics/orders', {
        ...params,
        timezone_offset_minutes: -new Date().getTimezoneOffset(),
    });
}

export interface ValidOrdersResult {
    orders: Order[];
    total: number;
    truncated: boolean;
}

export const getValidOrders = async (dateRange: {start_date: string; end_date: string}): Promise<ValidOrdersResult> => {
    const res = await get<ValidOrdersResponse | ValidOrderResponse[]>('/api/v1/analytics/orders/valid', {
        start_date: dateRange.start_date,
        end_date: dateRange.end_date,
        timezone_offset_minutes: -new Date().getTimezoneOffset(),
    });
    const orders = Array.isArray(res) ? res : (res.orders || []);
    const normalized = orders.map((order: any) => ({
        ...order,
        id: order.id || order.order_id,
        status: normalizeOrderStatus(order.status || order.order_status),
        quantity: Number(order.quantity || 1),
    }));
    return {
        orders: normalized,
        total: Array.isArray(res) ? normalized.length : Number(res.total ?? normalized.length),
        truncated: Array.isArray(res) ? false : res.truncated === true,
    };
}

// Cards
const normalizeCard = (item: any): Card => {
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

const cardPayload = (data: Partial<Card>): Record<string, unknown> => ({
  ...data,
  api_config: data.api_config ? JSON.stringify(data.api_config) : '',
});

export const getCards = async (): Promise<Card[]> => {
  const res = await get<Card[] | CardListResponse>('/api/v1/cards');
  const cards = Array.isArray(res) ? res : (res.cards || []);
  return cards.map(normalizeCard);
};

export const createCard = async (data: Partial<Card>): Promise<MutationIDResponse> => {
  return post('/api/v1/cards', cardPayload(data));
};

export const updateCard = async (cardId: string | number, data: Partial<Card>): Promise<OperationResponse> => {
  return put(`/api/v1/cards/${cardId}`, cardPayload(data));
};

export const deleteCard = async (cardId: string | number): Promise<OperationResponse> => {
  return del(`/api/v1/cards/${cardId}`);
};

export const getCardDetails = async (cardId: string | number): Promise<Card> => {
  const card = await get<Card>(`/api/v1/cards/${cardId}/details`);
  return normalizeCard(card);
};

// 批量创建卡密组（上传表格）
export const batchCreateCards = async (file: File): Promise<CardBatchResponse> => {
  // 批量创建接口返回总行数、成功数、失败数和逐行结果。
  // CardBatchResponse 保留旧字段名称，调用方无需转换统计字段。
  // rows 中的 id 只在创建成功时返回。
  // rows 中的 error 只在对应行失败时返回。
  // 表单上传方式和接口路径保持不变。
  // 此处只收紧 TypeScript 响应契约。
  const body = new FormData();
  body.append('file', file);
  return postForm('/api/v1/cards/batch', body);
};

// 往 data 类型卡密组批量追加卡密号
export const appendCardData = async (cardId: string | number, content: string): Promise<CardAppendResponse> => {
  return post(`/api/v1/cards/${cardId}/append-data`, { content });
};

// Items
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

export const getItems = async (cookieId?: string): Promise<Item[]> => {
    const res = await get<Item[] | ItemListEnvelope>('/api/v1/items', cookieId ? { cookie_id: cookieId } : undefined);
    const items = Array.isArray(res) ? res : (res.items || []);
    return items.map((item: any) => ({
      ...item,
      id: item.id || `${item.cookie_id}-${item.item_id}`,
      is_multi_spec: normalizeBooleanFlag(item.is_multi_spec),
      is_multi_qty_ship: normalizeBooleanFlag(item.is_multi_qty_ship ?? item.multi_quantity_delivery),
      multi_quantity_delivery: normalizeBooleanFlag(item.multi_quantity_delivery ?? item.is_multi_qty_ship),
    }));
}

export const syncItemsFromAccount = async (cookieId: string): Promise<ItemSyncResponse> => {
    return post('/api/v1/items/get-all-from-account', { cookie_id: cookieId });
}

export const deleteItem = async (cookieId: string, itemId: string): Promise<OperationResponse> => {
    return del(`/api/v1/items/${cookieId}/${itemId}`);
}

export const createItem = async (cookieId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return post(`/items/${cookieId}`, data);
}

export const publishItem = async (form: {
    cookie_id: string;
    title: string;
    description: string;
    price: string;
    original_price?: string;
    quantity: string | number;
    postage_mode: string;
    postage?: string;
    images: File[];
	location?: PublishLocation;
}): Promise<ItemPublishResponse> => {
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
    for (const file of form.images) {
      body.append('images', file);
    }
    return postForm('/api/v1/items/publish', body);
}

export interface PublishLocation {
	area: string;
	city: string;
	division_id: string;
	longitude: number;
	latitude: number;
	poi_id: string;
	poi_name: string;
	province: string;
}

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

export const previewItemPublishBatch = async (form: {
    file: File;
    imagesZip?: File | null;
    defaultCookieId?: string;
    fallbackCategory: {
      catId: string;
      catName: string;
      channelCatId?: string;
      tbCatId?: string;
    };
	location?: PublishLocation;
}): Promise<ItemPublishBatchPreviewResponse> => {
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

export const startItemPublishBatch = async (previewId: string): Promise<BatchIDResponse> => {
    return post('/api/v1/items/publish-batches', { preview_id: previewId });
}

export const getItemPublishBatch = async (batchId: string): Promise<ItemPublishBatchResponse> => {
    return get(`/api/v1/items/publish-batches/${batchId}`);
}

export const getItemPublishBatches = async (limit = 20): Promise<ItemPublishBatchResponse[]> => {
    const res = await get<ItemPublishBatchListResponse>('/api/v1/items/publish-batches', { limit });
    return Array.isArray(res) ? res : (res.batches || []);
}

export const deleteItemPublishBatch = async (batchId: string): Promise<OperationResponse> => {
    return del(`/api/v1/items/publish-batches/${batchId}`);
}

export const cancelItemPublishBatch = async (batchId: string): Promise<BatchCancelResponse> => {
    return post(`/api/v1/items/publish-batches/${batchId}/cancel`, {});
}

export const retryFailedItemPublishBatch = async (batchId: string): Promise<BatchIDResponse> => {
    return post(`/api/v1/items/publish-batches/${batchId}/retry-failed`, {});
}

export const updateItem = async (cookieId: string, itemId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return put(`/api/v1/items/${cookieId}/${itemId}`, data);
}

// Rules - 自动化规则
const normalizeShippingRules = (rules: any[]): ShippingRule[] => rules.map((item: any) => ({
        id: String(item.id),
        name: item.name || '',
        trigger_type: item.trigger_type || 'order_paid',
        item_keyword: item.item_title || item.item_id || '',
        cookie_id: item.cookie_id || '',
        item_id: item.item_id || '',
        item_title: item.item_title || '',
        card_group_id: Number((item.actions || []).find((a: any) => a.action_type === 'send_card')?.card_id || 0),
        card_group_name: (item.actions || []).find((a: any) => a.action_type === 'send_card')?.card_name || '',
        priority: item.priority || 100,
        enabled: item.enabled || false,
        config_json: item.config_json || '{}',
        actions: (item.actions || []).map((action: any) => ({
          id: action.id ? String(action.id) : undefined,
          action_type: action.action_type,
          card_id: Number(action.card_id || 0),
          card_name: action.card_name || '',
          delivery_count: Number(action.delivery_count || 1),
          message_template: action.message_template || '',
          delay_seconds: Number(action.delay_seconds || 0),
          config_json: action.config_json || '{}',
          enabled: action.enabled !== false,
          sort_order: Number(action.sort_order || 0),
        })),
        variants: (item.actions || [])
          .filter((action: any) => action.action_type === 'send_card')
          .map((action: any) => {
            let cfg: any = {};
            try { cfg = JSON.parse(action.config_json || '{}'); } catch {}
            return {
              id: action.id ? String(action.id) : undefined,
              spec_name: cfg.spec_name || '',
              spec_value: cfg.spec_value || '',
              card_id: Number(action.card_id || 0),
              card_name: action.card_name || '',
              delivery_count: Number(action.delivery_count || 1),
              enabled: action.enabled !== false,
              delay_override: cfg.delay_override === true,
              delay_seconds: Number(action.delay_seconds || 0),
              config_json: action.config_json || '{}',
            };
          }),
    }));

export const getShippingRules = async (): Promise<ShippingRule[]> => {
    const res = await get<AutomationRuleResponse[] | AutomationRulePageResponse>('/automation-rules');
    const rules = Array.isArray(res) ? res : (res.data || []);
    return normalizeShippingRules(rules);
}

export interface ShippingRuleListParams {
  cookieId?: string;
  triggerType?: AutomationTriggerType | '';
  enabled?: boolean;
  search?: string;
  page?: number;
  pageSize?: number;
}

export const getShippingRulesPage = async ({
  cookieId,
  triggerType,
  enabled,
  search,
  page = 1,
  pageSize = 10,
}: ShippingRuleListParams = {}): Promise<PaginatedResponse<ShippingRule>> => {
  const res = await get<AutomationRuleResponse[] | AutomationRulePageResponse>('/automation-rules', {
    page,
    page_size: pageSize,
    cookie_id: cookieId || undefined,
    trigger_type: triggerType || undefined,
    enabled,
    search: search?.trim() || undefined,
  });
  const rules = normalizeShippingRules(Array.isArray(res) ? res : (res.data || []));
  return {
    success: true,
    data: rules,
    total: Number(Array.isArray(res) ? rules.length : (res.total ?? rules.length)),
    page: Number(Array.isArray(res) ? page : (res.page ?? page)),
    page_size: Number(Array.isArray(res) ? pageSize : (res.page_size ?? pageSize)),
    total_pages: Number(Array.isArray(res) ? (rules.length ? 1 : 0) : (res.total_pages ?? (rules.length ? 1 : 0))),
    trigger_counts: Object.fromEntries(
      Object.entries(Array.isArray(res) ? {} : res.trigger_counts || {}).map(([key, value]) => [key, Number(value)]),
    ),
  };
}

const orderAutomationActions = (triggerType: string, actions: AutomationAction[]) => {
    if (triggerType !== 'order_paid') {
      return actions.map((action, index) => ({ ...action, sort_order: action.sort_order || index + 1 }));
    }
    const sendCards = actions
      .filter(action => action.action_type === 'send_card')
      .map((action, index) => ({ ...action, sort_order: index + 1 }));
    const others = actions.filter(action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    return [
      ...sendCards,
      ...others.map((action, index) => ({ ...action, sort_order: sendCards.length + index + 1 })),
      { action_type: 'confirm_shipment' as const, enabled: true, sort_order: sendCards.length + others.length + 1 },
    ];
};

export const updateShippingRule = async (rule: Partial<ShippingRule>): Promise<OperationResponse | MutationIDResponse> => {
    const triggerType = rule.trigger_type || 'order_paid';
    const triggerName: Record<string, string> = {
      order_paid: '付款后自动发货',
      buyer_reviewed: '评价后发送赠品',
      review_missing_timeout: '超时未评价求评价',
    };
    const generatedName = [
      triggerName[triggerType] || '自动化规则',
      rule.item_title || rule.item_id || rule.cookie_id || '',
    ].filter(Boolean).join(' - ');
    const preservedNonCardActions = (rule.actions || []).filter(action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    const baseActions: AutomationAction[] = rule.variants && rule.variants.length > 0
      ? [...rule.variants.map((variant, index) => ({
            action_type: 'send_card' as const,
            card_id: variant.card_id,
            delivery_count: variant.delivery_count || 1,
            enabled: variant.enabled !== false,
            sort_order: index + 1,
            delay_seconds: variant.delay_seconds || 0,
            config_json: JSON.stringify({
              spec_name: variant.spec_name || '',
              spec_value: variant.spec_value || '',
              delay_override: variant.delay_override === true,
            }),
		  })), ...preservedNonCardActions]
      : (rule.actions && rule.actions.length > 0 ? rule.actions : [{
          action_type: 'send_card' as const,
          card_id: rule.card_group_id || 0,
          delivery_count: 1,
          enabled: true,
          sort_order: 1,
        }]);
    const actions = orderAutomationActions(triggerType, baseActions);
    const payload = {
        cookie_id: rule.cookie_id || '',
        item_id: rule.item_id || '',
        name: (rule.name || '').trim() || generatedName || '自动化规则',
        trigger_type: triggerType,
        enabled: rule.enabled ?? true,
        priority: rule.priority || 100,
        config_json: rule.config_json || '{}',
        actions: actions.map((action, index) => ({
          action_type: action.action_type,
          card_id: action.card_id || 0,
          delivery_count: action.delivery_count || 1,
          message_template: action.message_template || '',
          delay_seconds: action.delay_seconds || 0,
          config_json: action.config_json || '{}',
          enabled: action.enabled !== false,
          sort_order: action.sort_order || index + 1,
        })),
    };
    return rule.id ? put(`/automation-rules/${rule.id}`, payload) : post('/automation-rules', payload);
}

export const deleteShippingRule = async (id: string): Promise<OperationResponse> => del(`/automation-rules/${id}`);

export interface AutomationRunIssue {
  id: number;
  cookie_id: string;
  order_id: string;
  trigger_type: string;
  error_message: string;
  issue_kind: 'external_result_unknown' | 'invalid_snapshot' | 'rule_unavailable' | 'partial_failure' | 'execution_failed';
  allowed_resolutions: Array<'continue' | 'retry' | 'cancel'>;
  action_cursor: number;
  sent_count: number;
  updated_at: string;
}

export interface DeferredAutomationIssue {
  id: number;
  cookie_id: string;
  trigger_type: string;
  error_message: string;
  attempt_count: number;
  updated_at: string;
}

export const getAutomationIssues = async (): Promise<{ runs: AutomationRunIssue[]; pending_tasks: DeferredAutomationIssue[] }> => {
  const result = await get<AutomationIssuesEnvelope>('/automation-issues');
  return {
    runs: Array.isArray(result?.runs) ? result.runs : [],
    pending_tasks: Array.isArray(result?.pending_tasks) ? result.pending_tasks : [],
  };
};

export const resolveAutomationRun = async (id: number, resolution: 'continue' | 'retry' | 'cancel'): Promise<OperationResponse> =>
  post(`/automation-runs/${id}/resolve`, { resolution });

export const resolveDeferredAutomationTask = async (id: number, resolution: 'retry' | 'dismiss'): Promise<OperationResponse> =>
  post(`/automation-pending-tasks/${id}/resolve`, { resolution });

// Rules - 关键词回复规则 (使用关键词API)
type KeywordRowPayload = {
    id: string;
    keyword: string;
    reply: string;
    item_id: string;
    type: 'text' | 'image';
    image_url: string;
};

const normalizeKeywordRow = (item: any): KeywordRowPayload => ({
    id: String(item?.id || ''),
    keyword: item?.keyword || '',
    reply: item?.reply || '',
    item_id: item?.item_id || '',
    type: item?.type === 'image' ? 'image' : 'text',
    image_url: item?.image_url || '',
});

const getKeywordRowsWithType = async (cookieId: string): Promise<KeywordRowPayload[]> => {
    const existing = await get<KeywordTypedResponse[]>(`/api/v1/reply-rules/${cookieId}/typed`);
    return Array.isArray(existing) ? existing.map(normalizeKeywordRow) : [];
};

export const getReplyRules = async (cookieId?: string): Promise<ReplyRule[]> => {
    if (!cookieId) return [];
    const keywords = await getKeywordRowsWithType(cookieId);
	return keywords.map((item: any) => ({
		id: item.id,
        keyword: item.keyword || '',
        reply_content: item.reply || '',
        match_type: 'fuzzy' as const,
        enabled: true,
        item_id: item.item_id || '',
        type: item.type === 'image' ? 'image' : 'text',
        image_url: item.image_url || ''
    }));
}

export const updateReplyRule = async (rule: Partial<ReplyRule>, cookieId: string): Promise<OperationResponse> => {
	const type = rule.type || 'text';
	const payload = {
		keyword: rule.keyword || '',
		reply: type === 'text' ? (rule.reply_content || '') : '',
		item_id: rule.item_id || '',
		type,
		image_url: type === 'image' ? (rule.image_url || '') : '',
	};
	return rule.id
		? put(`/api/v1/reply-rules/${cookieId}/typed/${rule.id}`, payload)
		: post(`/api/v1/reply-rules/${cookieId}/items`, payload);
}

export const deleteReplyRule = async (id: string, cookieId: string): Promise<OperationResponse> => {
	return del(`/api/v1/reply-rules/${cookieId}/typed/${id}`);
}

// Settings
export const getSystemSettings = async (): Promise<SystemSettings> => {
    const res = await get<{data: SystemSettings}>('/api/v1/settings/system');
    return normalizeSettings(res.data || res); // handle {success:true, data: {...}} wrapper if exists
};

export const updateSystemSettings = async (settings: Partial<SystemSettings>): Promise<OperationResponse> => {
	const payload = Object.fromEntries(
		Object.entries(settings).filter(([, value]) => value !== undefined && value !== null),
	);
	return put('/api/v1/settings/system', payload);
};

export const getAccountAISettings = async (cookieId: string, options?: RequestControlOptions): Promise<AIReplySettingsResponse> => {
    return get(`/api/v1/settings/ai-reply/${cookieId}`, undefined, options);
}

export const updateAccountAISettings = async (cookieId: string, settings: Partial<AIReplySettings>): Promise<OperationResponse> => {
  const payload = {
    ai_enabled: settings.ai_enabled ?? false,
    max_discount_percent: settings.max_discount_percent ?? 10,
    max_discount_amount: settings.max_discount_amount ?? 100,
    max_bargain_rounds: settings.max_bargain_rounds ?? 3,
    custom_prompts: settings.custom_prompts ?? ''
  };
  return put(`/api/v1/settings/ai-reply/${cookieId}`, payload);
}

export const fetchAIModels = async (baseUrl: string, apiKey: string = ''): Promise<string[]> => {
  const result = await post<AIModelsResponse>('/api/v1/settings/ai-models', {
    base_url: baseUrl,
    api_key: apiKey,
  });
  return result.models || [];
};

// Notification Channels
const parseNotificationEventTypes = (raw: unknown): NotificationEventType[] => {
  if (Array.isArray(raw)) return raw.filter(Boolean) as NotificationEventType[];
  if (typeof raw !== 'string' || !raw.trim()) return [];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.filter(Boolean) as NotificationEventType[];
  } catch {
    // fall back to legacy comma/semicolon separated values
  }
  return raw.split(/[,\s;]+/).map(v => v.trim()).filter(Boolean) as NotificationEventType[];
};

const stringifyNotificationEventTypes = (events?: NotificationEventType[]): string => {
  const clean = Array.from(new Set((events || []).filter(Boolean)));
  return clean.length > 0 ? JSON.stringify(clean) : '';
};

export const getNotificationChannels = async (options?: RequestControlOptions): Promise<{ success: boolean; data?: NotificationChannel[] }> => {
  const result = await get<NotificationChannelResponse[]>('/api/v1/notifications/channels', undefined, options);
  const channels = (result || []).map((item: any) => {
    let parsedConfig;
    try {
      parsedConfig = typeof item.config === 'string' ? JSON.parse(item.config) : item.config;
    } catch {
		parsedConfig = {};
    }
    return {
      id: String(item.id),
      name: item.name,
		type: item.type === 'ding_talk' ? 'dingtalk' : (item.type === 'lark' ? 'feishu' : item.type),
      config: parsedConfig,
      event_types: parseNotificationEventTypes(item.event_types),
      enabled: item.enabled,
      created_at: item.created_at,
      updated_at: item.updated_at,
    };
  });
  return { success: true, data: channels };
}

export const createNotificationChannel = async (data: { name: string; type: string; config: Record<string, unknown>; event_types?: NotificationEventType[]; enabled?: boolean }): Promise<MutationIDResponse> => {
  return post('/api/v1/notifications/channels', {
    ...data,
    config: JSON.stringify(data.config),
    event_types: stringifyNotificationEventTypes(data.event_types)
  });
}

export const updateNotificationChannel = async (channelId: string, data: { name?: string; type?: string; config?: Record<string, unknown>; event_types?: NotificationEventType[]; enabled?: boolean }): Promise<OperationResponse> => {
  const payload: Record<string, unknown> = { ...data };
  if ('config' in data) {
    payload.config = JSON.stringify(data.config);
  }
  if ('event_types' in data) {
    payload.event_types = stringifyNotificationEventTypes(data.event_types);
  }
  return put(`/api/v1/notifications/channels/${channelId}`, payload);
}

export const deleteNotificationChannel = async (channelId: string): Promise<OperationResponse> => {
  return del(`/api/v1/notifications/channels/${channelId}`);
}

// Message Notifications
export const getMessageNotifications = async (): Promise<{ success: boolean; data?: NotificationBinding[] }> => {
  const result = await get<Record<string, NotificationBinding[]>>('/api/v1/notifications/messages');
  const notifications: NotificationBinding[] = [];
  for (const [cookieId, channelList] of Object.entries(result || {})) {
    if (Array.isArray(channelList)) {
      for (const item of channelList) {
        notifications.push({
          cookie_id: cookieId,
          channel_id: item.channel_id,
          channel_name: item.channel_name,
          enabled: item.enabled,
        });
      }
    }
  }
  return { success: true, data: notifications };
}

export const setMessageNotification = async (cookieId: string, channelId: number, enabled: boolean): Promise<OperationResponse> => {
  return post(`/api/v1/notifications/accounts/${cookieId}/bindings`, { channel_id: channelId, enabled });
}

export const deleteMessageNotification = async (notificationId: string): Promise<OperationResponse> => {
  return del(`/api/v1/notifications/messages/${notificationId}`);
}

export const deleteAccountNotifications = async (cookieId: string): Promise<OperationResponse> => {
  return del(`/api/v1/notifications/messages/account/${cookieId}`);
}

// 账号 ↔ 渠道 绑定（覆盖式）
export const getAccountBindings = async (cookieId: string, options?: RequestControlOptions): Promise<number[]> => {
  const result = await get<AccountBindingsResponse>(`/api/v1/notifications/accounts/${cookieId}/bindings`, undefined, options);
  return result?.channel_ids || [];
}

export const setAccountBindings = async (cookieId: string, channelIds: number[]): Promise<OperationResponse> => {
  return post(`/api/v1/notifications/accounts/${cookieId}/bindings`, { channel_ids: channelIds });
}

// 测试发送
export const testNotificationChannel = async (channelId: string): Promise<OperationResponse> => {
  return post(`/api/v1/notifications/channels/${channelId}/test`, {});
}

// Default Reply
export const getDefaultReplies = async (): Promise<Record<string, DefaultReplyResponse>> => {
	return get('/api/v1/default-replies');
};

export const getDefaultReply = async (cookieId: string): Promise<DefaultReply> => {
	const result = await get<DefaultReplyResponse>(`/api/v1/default-replies/${cookieId}`);
  return {
    cookie_id: cookieId,
    enabled: result.enabled || false,
    reply_content: result.reply_content || '',
    reply_once: result.reply_once || false,
    reply_image_url: result.reply_image_url || ''
  };
};

export const updateDefaultReply = async (cookieId: string, data: Partial<DefaultReply>): Promise<OperationResponse> => {
  return put(`/api/v1/default-replies/${cookieId}`, {
    enabled: data.enabled ?? false,
    reply_content: data.reply_content || '',
    reply_once: data.reply_once ?? false,
    reply_image_url: data.reply_image_url || ''
  });
};

export const deleteDefaultReply = async (cookieId: string): Promise<OperationResponse> => {
	return del(`/api/v1/default-replies/${cookieId}`);
};

export const clearDefaultReplyRecords = async (cookieId: string): Promise<OperationResponse> => {
	return post(`/api/v1/default-replies/${cookieId}/clear-records`, {});
};

// getItemDetail 获取指定账号下单个商品的详情。
export const getItemDetail = async (cookieId: string, itemId: string): Promise<ItemDetailResponse> => {
  return get(`/api/v1/items/${cookieId}/${itemId}`);
};
