import { get, post, put, del, postForm, type RequestControlOptions } from '../request';
import {
  SessionResponse, AccountDetail, AccountSummaryResponse, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, AccountTaskSettings, ChatSession, ChatMessage, ItemListEnvelope, AutomationIssuesEnvelope,
  CookieSettingsResponse, CookieProfileResponse, ItemDetailResponse, ItemPublishResponse, ItemSyncResponse, OrderDTOResponse, OrderDetailResponse, OrderSingleRefreshResponse, OrderBatchResponse, OrderRefreshResponse, OrderRefreshJobStartResponse, OrderRefreshJobStatusResponse, OrderRefreshJobCancelResponse, AutomationRuleResponse, AutomationRulePageResponse, AIReplySettingsResponse, AIModelsResponse, UserSettingResponse, CardBatchResponse, CardAppendResponse, CategoryRecommendationResponse, ItemPublishBatchPreviewResponse, ItemPublishBatchListResponse, BatchIDResponse, ItemPublishBatchResponse, BatchCancelResponse, MutationIDResponse, OperationResponse, NotificationChannelResponse, NotificationBinding, AccountBindingsResponse, CardListResponse, KeywordTypedResponse, DefaultReplyResponse, AccountTaskSettingsResponse, AccountTaskRunResponseEnvelope, AdminStatsResponse, DashboardStatsResponse, OrderAnalyticsResponse, QRLoginGenerateResponse, QRLoginStatusResponse, QRLoginVerificationResponse, ValidOrderResponse, ValidOrdersResponse
} from '../types';
import { formatLocalDate } from '../dateRange';

// normalizeSettings 归一化系统配置。
const normalizeSettings = (settings: Record<string, any>): SystemSettings => {
  // out 配置副本，用于当前 API 处理流程。
  const out: Record<string, any> = { ...settings };
  // sensitiveKey 是后端只返回配置状态、不返回明文的敏感配置键。
  const sensitiveKeys = ['ai_api_key', 'smtp_password', 'qq_reply_secret_key', 'captcha.remote_secret_key'];
  for (const sensitiveKey /* sensitiveKey 表示当前处理的敏感配置键。 */ of sensitiveKeys) {
    // configuredKey 是敏感配置是否已设置的状态字段。
    const configuredKey = `${sensitiveKey}_configured`;
    if (configuredKey in out) {
      out[configuredKey] = out[configuredKey] === true || out[configuredKey] === 'true';
    }
  }
  if ('renewal_log_retention_days' in out) {
    // parsed 转换后的数值，用于当前 API 处理流程。
    const parsed = Number(out.renewal_log_retention_days);
    out.renewal_log_retention_days = Number.isFinite(parsed) ? parsed : 10;
  }
  return out as SystemSettings;
};

// Auth
// login 登录，用于当前 API 处理流程。
export const login = async (data: { /** username 表示用户名。 */ username?: string; /** password 表示密码。 */ password?: string; /** email 表示邮箱。 */ email?: string; /** verification_code 表示登录验证码。 */ verification_code?: string }): Promise<SessionResponse> => {
  return post('/api/v1/session/login', data, { skipAuthLogout: true });
};

// initializeAdmin 初始化管理员并建立会话。
export const initializeAdmin = async (password: string): Promise<SessionResponse> => {
  return post('/api/v1/session/initialize', { password }, { skipAuthLogout: true });
};

// verifySession 验证会话。
export const verifySession = async (options?: RequestControlOptions): Promise<{ /** authenticated 表示当前会话是否已认证。 */ authenticated: boolean; /** initialized 表示系统是否已完成初始化。 */ initialized?: boolean; /** user_id 表示当前用户标识。 */ user_id?: number; /** username 表示用户名。 */ username?: string; /** is_admin 表示当前用户是否为管理员。 */ is_admin?: boolean }> => {
  return get('/api/v1/session', undefined, options);
};

// getHealth 读取服务健康检查和构建版本信息。
export const getHealth = async (options?: RequestControlOptions): Promise<{ /** version 表示版本。 */ version?: string; /** commit 表示提交版本。 */ commit?: string }> => {
  return get('/health', undefined, options);
};

// logout 注销当前会话。
export const logout = async (): Promise<OperationResponse> => {
  return post('/api/v1/session/logout', {});
};

// changePassword 修改当前用户密码。
export const changePassword = async (currentPassword: string, newPassword: string): Promise<OperationResponse> => {
  return post('/api/v1/session/password', { current_password: currentPassword, new_password: newPassword });
};

// updateLoginCredentials 更新当前用户登录凭据。
export const updateLoginCredentials = async (data: {
  /** current_password 表示当前密码。 */ current_password: string;
  /** new_username 表示新用户名。 */ new_username: string;
  /** new_password 表示新密码。 */ new_password?: string;
}, options?: RequestControlOptions): Promise<OperationResponse> => {
  return put('/api/v1/session/credentials', data, options);
};

// Accounts
// addAccount 新增账号。
export const addAccount = async (id: string, value: string, loginMethod?: string): Promise<OperationResponse> => {
  return post('/api/v1/accounts', { id, value, login_method: loginMethod });
};

// accountAvatarURL 生成账号头像地址。
const accountAvatarURL = (item: AccountSummaryResponse, version: string): string => {
  // raw 原始值，用于当前 API 处理流程。
  const raw = item.avatar_url || '';
  if (!raw) return '';

  try {
    // url 解析后的地址，用于当前 API 处理流程。
    const url = new URL(raw, window.location.origin);
    if (url.hostname.endsWith('alicdn.com')) {
      url.searchParams.set('_v', version);
    }
    return url.toString();
  } catch {
    return raw;
  }
};

// getAccountDetails 读取账号详情。
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => {
  // data 数据，用于当前 API 处理流程。
  const data = await get<AccountSummaryResponse[]>('/api/v1/accounts/details', undefined, options);
  // avatarVersion 头像缓存版本，用于当前 API 处理流程。
  const avatarVersion = Date.now().toString();
  return data.map(/* 当前回调用于处理集合元素或接口响应。 */ item => ({
    id: item.id,
    value: '',
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
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

// getAccountTaskSettings 读取账号计划任务设置。
export const getAccountTaskSettings = async (id: string, options?: RequestControlOptions): Promise<AccountTaskSettingsResponse> =>
	get(`/api/v1/account-tasks/${id}`, undefined, options);

// updateAccountTaskSettings 更新账号计划任务设置。
export const updateAccountTaskSettings = async (id: string, settings: AccountTaskSettings, options?: RequestControlOptions): Promise<AccountTaskSettingsResponse> =>
	put(`/api/v1/account-tasks/${id}`, settings, options);

// runAccountTask 立即执行账号计划任务。
export const runAccountTask = async (id: string, taskType: 'auto_rate' | 'auto_polish', options?: RequestControlOptions): Promise<AccountTaskRunResponseEnvelope> =>
	post(`/api/v1/account-tasks/${id}/run`, { task_type: taskType }, { timeoutMs: 120_000, ...options });

export interface ChatSessionPage { /** sessions 表示聊天会话列表。 */ sessions: ChatSession[]; /** has_more 表示是否存在更多数据。 */ has_more: boolean; /** next_cursor 表示下一页游标。 */ next_cursor?: number }

// getChatSessionPage 分页读取聊天会话。
export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false): Promise<ChatSessionPage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const result = await get<ChatSessionPage>('/api/v1/chat/sessions', { account_id: accountId, cursor, refresh: refresh ? 1 : undefined },
		refresh ? { timeoutMs: 60_000, ...options } : options);
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
	const result = await get<ChatMessagePage>('/api/v1/chat/messages', {
		account_id: accountId, chat_id: chatId, cursor, before_id: beforeId,
	}, options);
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

export interface AccountRuntimeStatus {
  /** state 表示状态。 */ state: NonNullable<AccountDetail['runtime_state']>;
  /** message 表示消息数据。 */ message?: string;
  /** connected 表示连接状态。 */ connected: boolean;
  /** failures 表示失败次数。 */ failures: number;
  /** updated_at 表示最后更新时间。 */ updated_at: string;
}

// getAccountRuntimeStatuses 读取账号运行状态。
export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, AccountRuntimeStatus>> => {
  return get('/api/v1/accounts/runtime-status', undefined, options);
};

// generateQRLogin 生成二维码登录会话。
export const generateQRLogin = async (options?: RequestControlOptions): Promise<QRLoginGenerateResponse> => {
  // 风控后匿名 token 接口可能超过通用的 30 秒请求窗口；后端总生成窗口为 2 分钟。
  return post('/api/v1/qr-login/generate', undefined, { ...options, timeoutMs: options?.timeoutMs ?? 130_000 });
};

// checkQRLoginStatus 查询二维码登录状态。
export const checkQRLoginStatus = async (sessionId: string, signal?: AbortSignal): Promise<QRLoginStatusResponse> => {
  return get(`/api/v1/qr-login/check/${sessionId}`, undefined, { signal, timeoutMs: 10_000 });
};

// completeQRVerification 完成二维码登录验证。
export const completeQRVerification = async (
  sessionId: string,
  targetAccountId?: string,
): Promise<QRLoginVerificationResponse> => {
  return post(`/api/v1/qr-login/complete-verification/${sessionId}`, {
    target_account_id: targetAccountId || '',
  });
};






// updateAccountStatus 更新账号状态。
export const updateAccountStatus = async (id: string, enabled: boolean): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/status`, { enabled });
};

// deleteAccount 删除账号。
export const deleteAccount = async (id: string): Promise<OperationResponse> => {
  return del(`/api/v1/accounts/${id}`);
};

// updateAccountRemark 更新账号备注。
export const updateAccountRemark = async (id: string, remark: string): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/remark`, { remark });
};

// updateAccountAutoConfirm 更新账号自动确认设置。
export const updateAccountAutoConfirm = async (id: string, autoConfirm: boolean): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/auto-confirm`, { auto_confirm: autoConfirm });
};

// updateAccountPauseDuration 更新账号暂停时长。
export const updateAccountPauseDuration = async (id: string, pauseDuration: number, options?: RequestControlOptions): Promise<CookieSettingsResponse> => {
  return put(`/api/v1/accounts/${id}/pause-duration`, { pause_duration: pauseDuration }, options);
};

// updateAccountCookie 更新账号登录凭证。
export const updateAccountCookie = async (id: string, value: string, loginMethod?: string): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}`, { id, value, login_method: loginMethod });
};

export interface AccountSettingsUpdate {
  /** cookie 表示登录凭证。 */ cookie?: string;
  /** remark 表示备注。 */ remark?: string;
  /** auto_confirm 表示自动确认状态。 */ auto_confirm?: boolean;
  /** pause_duration 表示暂停时长。 */ pause_duration?: number;
  /** username 表示用户名。 */ username?: string;
  /** login_password 表示登录密码。 */ login_password?: string;
  /** clear_password 表示是否清理登录密码。 */ clear_password?: boolean;
  /** show_browser 表示是否显示浏览器。 */ show_browser?: boolean;
  /** channel_ids 表示通知渠道标识列表。 */ channel_ids?: number[];
}

// updateAccountSettings 更新账号设置。
export const updateAccountSettings = async (id: string, data: AccountSettingsUpdate, options?: RequestControlOptions): Promise<CookieSettingsResponse> => {
  return put(`/api/v1/accounts/${id}/settings`, data, options);
};

export interface LongLoginSettings {
  /** can_open_long_login 表示是否允许开启长期登录。 */ can_open_long_login: boolean;
  /** enabled 表示启用状态。 */ enabled: boolean;
}

// getLongLoginSettings 读取长期登录设置。
export const getLongLoginSettings = async (id: string, options?: RequestControlOptions): Promise<LongLoginSettings> => {
  return get(`/api/v1/accounts/${id}/long-login`, undefined, options);
};

// setLongLoginSettings 设置长期登录开关。
export const setLongLoginSettings = async (id: string, enabled: boolean, options?: RequestControlOptions): Promise<LongLoginSettings> => {
  return put(`/api/v1/accounts/${id}/long-login`, { enabled }, options);
};

export interface PasswordLoginStartResponse {
  /** success 表示是否成功。 */ success: boolean;
  /** session_id 表示会话标识。 */ session_id?: string;
  /** status 表示状态值。 */ status?: 'processing' | 'failed';
  /** message 表示消息数据。 */ message?: string;
}

export interface PasswordLoginStatusResponse {
  /** status 表示状态值。 */ status: 'processing' | 'success' | 'failed' | 'verification_required' | 'not_found' | 'error';
  /** message 表示消息数据。 */ message?: string;
  /** account_id 表示账号标识。 */ account_id?: string;
  /** is_new_account 表示是否为新账号。 */ is_new_account?: boolean;
  /** cookie_count 表示登录凭证数量。 */ cookie_count?: number;
  /** verification_url 表示验证地址。 */ verification_url?: string;
  /** screenshot_path 表示验证截图路径。 */ screenshot_path?: string;
  /** qr_code_url 表示二维码地址。 */ qr_code_url?: string;
  /** error 表示错误信息。 */ error?: string;
  /** reason 表示失败原因。 */ reason?: string;
}

// passwordLogin 执行密码登录。
export const passwordLogin = async (data: {
  /** account_id 表示账号标识。 */ account_id: string;
  /** account 表示账号。 */ account: string;
  /** password 表示密码。 */ password: string;
  /** show_browser 表示是否显示浏览器。 */ show_browser?: boolean;
}, options?: RequestControlOptions): Promise<PasswordLoginStartResponse> => {
  return post('/api/v1/password-login', data, options);
};

// checkPasswordLoginStatus 查询密码登录状态。
export const checkPasswordLoginStatus = async (sessionId: string, signal?: AbortSignal): Promise<PasswordLoginStatusResponse> => {
  return get(`/api/v1/password-login/check/${sessionId}`, undefined, { signal, timeoutMs: 10_000 });
};

// cancelPasswordLogin 取消密码登录。
export const cancelPasswordLogin = async (sessionId: string, options?: RequestControlOptions): Promise<OperationResponse> => {
  return del(`/api/v1/password-login/cancel/${sessionId}`, undefined, options);
};

// refreshAccountProfile 刷新账号资料。
export const refreshAccountProfile = async (id: string): Promise<CookieProfileResponse> => {
  return post(`/api/v1/accounts/${id}/refresh-profile`, {});
};

// updateAccountLoginInfo 更新账号登录信息。
export const updateAccountLoginInfo = async (id: string, data: {
  /** username 表示用户名。 */ username?: string;
  /** login_password 表示登录密码。 */ login_password?: string;
  /** clear_password 表示是否清理登录密码。 */ clear_password?: boolean;
  /** show_browser 表示是否显示浏览器。 */ show_browser?: boolean;
}): Promise<OperationResponse> => {
  return put(`/api/v1/accounts/${id}/login-info`, data);
};

// getAllAISettings 读取全部人工智能设置。
export const getAllAISettings = async (options?: RequestControlOptions): Promise<Record<string, AIReplySettings>> => {
  return get('/api/v1/settings/ai-reply', undefined, options);
};

// Orders
// normalizeOrderStatus 归一化订单状态。
const normalizeOrderStatus = (value: unknown): Order['status'] => {
  // status 状态值，用于当前 API 处理流程。
  const status = String(value || '');
  if (status === 'paid') return 'pending_ship';
  return ['processing', 'pending_ship', 'shipped', 'completed', 'cancelled', 'refunding'].includes(status)
    ? status as Order['status']
    : 'unknown';
};

// getOrders 读取订单列表。
export const getOrders = async (
  cookieId?: string,
  status?: string,
  page: number = 1,
  pageSize: number = 20,
  search?: string,
  options?: RequestControlOptions,
): Promise<PaginatedResponse<Order>> => {
  // params 请求参数，用于当前 API 处理流程。
  const params: any = { page, page_size: pageSize };
  if (cookieId) params.cookie_id = cookieId;
  if (status && status !== 'all') params.status = status;
  if (search?.trim()) params.search = search.trim();

  // res 接口响应结果，用于当前 API 处理流程。
  const res = await get<PaginatedResponse<Order> & { orders?: Order[] /* 后端兼容的订单列表字段。 */ }>('/api/v1/orders', params, options);

  // Handle backend response variations
  // rawOrders 原始订单列表，用于当前 API 处理流程。
  const rawOrders = res.orders || res.data || [];
  // orders 订单列表，用于当前 API 处理流程。
  const orders = rawOrders.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
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

// getOrderDetail 读取订单详情。
export const getOrderDetail = async (orderId: string): Promise<{ /** success 表示是否成功。 */ success: boolean; /** data 表示数据。 */ data?: OrderDTOResponse }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<OrderDetailResponse>(`/api/v1/orders/${orderId}`);
  return {
    success: true,
    data: result.data
  };
};

// updateOrder 更新订单。
export const updateOrder = async (orderId: string, data: Partial<Order>): Promise<OperationResponse> => {
  return put(`/api/v1/orders/${orderId}`, data);
};

// deleteOrder 删除订单。
export const deleteOrder = async (orderId: string): Promise<OperationResponse> => {
  return del(`/api/v1/orders/${orderId}`);
};

// syncOrders 同步订单。
export const syncOrders = async (cookieId?: string, status?: string, options?: RequestControlOptions): Promise<OrderRefreshResponse> => {
  // formData 表单数据，用于当前 API 处理流程。
  const formData = new FormData();
  if (cookieId) formData.append('cookie_id', cookieId);
  if (status) formData.append('status', status);

	// start 表示后台订单刷新任务创建响应。
	const start = await postForm<OrderRefreshJobStartResponse>('/api/v1/orders/refresh', formData, options);
	// cancelOnAbort 在调用方取消轮询时通知服务端停止同一后台任务。
	const cancelOnAbort = () => {
		void cancelOrderRefreshJob(start.job_id).catch(/* 取消请求失败时忽略网络错误，主请求仍按取消语义结束。 */ () => undefined);
	};
	options?.signal?.addEventListener('abort', cancelOnAbort, { once: true });
	// pollLimit 限制前端等待后台任务的轮询次数。
	const pollLimit = 180;
	// pollIndex 表示当前订单刷新任务状态轮询次数。
	let pollIndex = 0;
	try {
		while (pollIndex < pollLimit) {
		// job 表示当前轮询得到的后台任务状态。
		const job = await get<OrderRefreshJobStatusResponse>(`/api/v1/orders/refresh/${start.job_id}`, undefined, options);
		if (job.status === 'succeeded' && job.result) {
			return job.result;
		}
		if (job.status === 'failed' || job.status === 'cancelled') {
			throw new Error(job.error_message || '订单刷新任务失败');
		}
		// waitMs 是下一次任务状态轮询前的等待时间。
		const waitMs = 500;
		await new Promise<void>(/* 轮询等待器负责等待下一次任务状态查询。 */ (resolve, reject) => {
			// abort 负责响应调用方取消轮询。
			const abort = () => {
				globalThis.clearTimeout(timer);
				reject(new Error('请求已取消'));
			};
			// timer 表示当前轮询等待定时器。
			const timer = globalThis.setTimeout(/* 轮询完成回调清理取消监听并结束等待。 */ () => {
				options?.signal?.removeEventListener('abort', abort);
				resolve();
			}, waitMs);
			if (!options?.signal) return;
			if (options.signal.aborted) abort();
			else options.signal.addEventListener('abort', abort, { once: true });
		});
		pollIndex += 1;
		}
	} finally {
		options?.signal?.removeEventListener('abort', cancelOnAbort);
	}
	throw new Error('订单刷新任务等待超时');
};

// cancelOrderRefreshJob 请求取消当前用户的订单刷新后台任务。
export const cancelOrderRefreshJob = async (jobId: string): Promise<OrderRefreshJobCancelResponse> => {
	return del(`/api/v1/orders/refresh/${jobId}`);
};

// syncSingleOrder 同步单个订单。
export const syncSingleOrder = async (orderId: string): Promise<OrderSingleRefreshResponse> => {
  return post(`/api/v1/orders/${orderId}/refresh`);
};

// manualShipOrder 手动发货订单。
export const manualShipOrder = async (orderIds: string[], shipMode: 'status_only' | 'full_delivery'): Promise<OrderBatchResponse> => {
    return post('/api/v1/orders/manual-ship', {
        order_ids: orderIds,
        ship_mode: shipMode,
    });
}

// importOrders 导入订单。
export const importOrders = async (data: Partial<Order>[] | FormData, options?: RequestControlOptions): Promise<OrderBatchResponse> => {
	// isFormData 是否为表单请求，用于当前 API 处理流程。
	const isFormData = data instanceof FormData;
	return isFormData ? postForm('/api/v1/orders/import', data, options) : post('/api/v1/orders/import', data, options);
}

// Stats
// getAdminStats 读取管理员统计数据。
export const getAdminStats = async (): Promise<AdminStatsResponse> => {
  return get('/api/v1/admin/stats');
};

// getDashboardStats 读取仪表盘统计数据。
export const getDashboardStats = async (options?: RequestControlOptions): Promise<DashboardStatsResponse> => {
  return get('/api/v1/analytics/dashboard', undefined, options);
};

// getOrderAnalytics 读取订单分析数据。
export const getOrderAnalytics = async (daysOrParams: number | {/** start_date 表示开始日期。 */ start_date: string; /** end_date 表示结束日期。 */ end_date: string} = 7, options?: RequestControlOptions): Promise<OrderAnalyticsResponse> => {
    // params 请求参数，用于当前 API 处理流程。
    let params: {/** start_date 表示开始日期。 */ start_date: string; /** end_date 表示结束日期。 */ end_date: string};

    if (typeof daysOrParams === 'number') {
        // endDate 结束日期，用于当前 API 处理流程。
        const endDate = new Date();
        // startDate 启动日期。
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
    }, options);
}

export interface ValidOrdersResult {
    /** orders 表示订单列表。 */ orders: Order[];
    /** total 表示总数。 */ total: number;
    /** truncated 表示是否截断。 */ truncated: boolean;
}

// getValidOrders 读取有效订单列表。
export const getValidOrders = async (dateRange: {/** start_date 表示开始日期。 */ start_date: string; /** end_date 表示结束日期。 */ end_date: string}, options?: RequestControlOptions): Promise<ValidOrdersResult> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await get<ValidOrdersResponse | ValidOrderResponse[]>('/api/v1/analytics/orders/valid', {
        start_date: dateRange.start_date,
        end_date: dateRange.end_date,
        timezone_offset_minutes: -new Date().getTimezoneOffset(),
    }, options);
    // orders 订单列表，用于当前 API 处理流程。
    const orders = Array.isArray(res) ? res : (res.orders || []);
    // normalized 归一化结果，用于当前 API 处理流程。
    const normalized = orders.map(/* 当前回调用于处理集合元素或接口响应。 */ (order: any) => ({
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
  const res = await get<Card[] | CardListResponse>('/api/v1/cards', undefined, options);
  // cards 卡密列表，用于当前 API 处理流程。
  const cards = Array.isArray(res) ? res : (res?.cards || []);
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

// Items
// normalizeBooleanFlag 归一化布尔标记。
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

// getItems 读取商品列表。
export const getItems = async (cookieId?: string, options?: RequestControlOptions): Promise<Item[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await get<Item[] | ItemListEnvelope>('/api/v1/items', cookieId ? { cookie_id: cookieId } : undefined, options);
    // items 商品列表，用于当前 API 处理流程。
    const items = Array.isArray(res) ? res : (res.items || []);
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

export interface PublishLocation {
	/** area 表示地区。 */ area: string;
	/** city 表示城市。 */ city: string;
	/** division_id 表示行政区划标识。 */ division_id: string;
	/** longitude 表示经度。 */ longitude: number;
	/** latitude 表示纬度。 */ latitude: number;
	/** poi_id 表示地点标识。 */ poi_id: string;
	/** poi_name 表示地点名称。 */ poi_name: string;
	/** province 表示省份。 */ province: string;
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
    const res = await get<ItemPublishBatchListResponse>('/api/v1/items/publish-batches', { limit });
    return Array.isArray(res) ? res : (res.batches || []);
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

// Rules - 自动化规则
const normalizeShippingRules = (rules: any[]): ShippingRule[] => rules.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
        id: String(item.id),
        name: item.name || '',
        trigger_type: item.trigger_type || 'order_paid',
        item_keyword: item.item_title || item.item_id || '',
        cookie_id: item.cookie_id || '',
        item_id: item.item_id || '',
        item_title: item.item_title || '',
        card_group_id: Number((item.actions || []).find(/* 当前回调用于处理集合元素或接口响应。 */ (a: any) => a.action_type === 'send_card')?.card_id || 0),
        card_group_name: (item.actions || []).find(/* 当前回调用于处理集合元素或接口响应。 */ (a: any) => a.action_type === 'send_card')?.card_name || '',
        priority: item.priority || 100,
        enabled: item.enabled || false,
        config_json: item.config_json || '{}',
        actions: (item.actions || []).map(/* 当前回调用于处理集合元素或接口响应。 */ (action: any) => ({
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
          .filter(/* 当前回调用于处理集合元素或接口响应。 */ (action: any) => action.action_type === 'send_card')
          .map(/* 当前回调用于处理集合元素或接口响应。 */ (action: any) => {
            // cfg 动作配置，用于当前 API 处理流程。
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

// getShippingRules 读取发货规则列表。
export const getShippingRules = async (): Promise<ShippingRule[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await get<AutomationRuleResponse[] | AutomationRulePageResponse>('/api/v1/automation-rules');
    // rules 规则列表，用于当前 API 处理流程。
    const rules = Array.isArray(res) ? res : (res.data || []);
    return normalizeShippingRules(rules);
}

export interface ShippingRuleListParams {
  /** cookieId 表示登录凭证标识。 */ cookieId?: string;
  /** triggerType 表示触发类型。 */ triggerType?: AutomationTriggerType | '';
  /** enabled 表示启用状态。 */ enabled?: boolean;
  /** search 表示搜索条件。 */ search?: string;
  /** page 表示页码。 */ page?: number;
  /** pageSize 表示每页数量。 */ pageSize?: number;
}

// getShippingRulesPage 分页读取发货规则。
export const getShippingRulesPage = async ({
  cookieId,
  triggerType,
  enabled,
  search,
  page = 1,
  pageSize = 10,
}: ShippingRuleListParams = {}): Promise<PaginatedResponse<ShippingRule>> => {
  // res 接口响应结果，用于当前 API 处理流程。
  const res = await get<AutomationRuleResponse[] | AutomationRulePageResponse>('/api/v1/automation-rules', {
    page,
    page_size: pageSize,
    cookie_id: cookieId || undefined,
    trigger_type: triggerType || undefined,
    enabled,
    search: search?.trim() || undefined,
  });
  // rules 规则列表，用于当前 API 处理流程。
  const rules = normalizeShippingRules(Array.isArray(res) ? res : (res.data || []));
  return {
    success: true,
    data: rules,
    total: Number(Array.isArray(res) ? rules.length : (res.total ?? rules.length)),
    page: Number(Array.isArray(res) ? page : (res.page ?? page)),
    page_size: Number(Array.isArray(res) ? pageSize : (res.page_size ?? pageSize)),
    total_pages: Number(Array.isArray(res) ? (rules.length ? 1 : 0) : (res.total_pages ?? (rules.length ? 1 : 0))),
    trigger_counts: Object.fromEntries(
      Object.entries(Array.isArray(res) ? {} : res.trigger_counts || {}).map(/* 当前回调用于处理集合元素或接口响应。 */ ([key, value]) => [key, Number(value)]),
    ),
  };
}

// orderAutomationActions 构建订单自动化动作。
const orderAutomationActions = (triggerType: string, actions: AutomationAction[]) => {
    if (triggerType !== 'order_paid') {
      return actions.map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({ ...action, sort_order: action.sort_order || index + 1 }));
    }
    // sendCards 发送Cards。
    const sendCards = actions
      .filter(/* 当前回调用于处理集合元素或接口响应。 */ action => action.action_type === 'send_card')
      .map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({ ...action, sort_order: index + 1 }));
    // others 其他动作列表，用于当前 API 处理流程。
    const others = actions.filter(/* 当前回调用于处理集合元素或接口响应。 */ action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    return [
      ...sendCards,
      ...others.map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({ ...action, sort_order: sendCards.length + index + 1 })),
      { action_type: 'confirm_shipment' as const, enabled: true, sort_order: sendCards.length + others.length + 1 },
    ];
};

// updateShippingRule 更新发货规则。
export const updateShippingRule = async (rule: Partial<ShippingRule>): Promise<OperationResponse | MutationIDResponse> => {
    // triggerType 触发类型，用于当前 API 处理流程。
    const triggerType = rule.trigger_type || 'order_paid';
    // triggerName 触发名称，用于当前 API 处理流程。
    const triggerName: Record<string, string> = {
      order_paid: '付款后自动发货',
      buyer_reviewed: '评价后发送赠品',
      review_missing_timeout: '超时未评价求评价',
    };
    // generatedName 生成d名称。
    const generatedName = [
      triggerName[triggerType] || '自动化规则',
      rule.item_title || rule.item_id || rule.cookie_id || '',
    ].filter(Boolean).join(' - ');
    // preservedNonCardActions 保留的非卡密动作，用于当前 API 处理流程。
    const preservedNonCardActions = (rule.actions || []).filter(/* 当前回调用于处理集合元素或接口响应。 */ action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    // baseActions 基础动作列表，用于当前 API 处理流程。
    const baseActions: AutomationAction[] = rule.variants && rule.variants.length > 0
      ? [...rule.variants.map(/* 当前回调用于处理集合元素或接口响应。 */ (variant, index) => ({
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
    // actions 动作列表，用于当前 API 处理流程。
    const actions = orderAutomationActions(triggerType, baseActions);
    // payload 请求载荷，用于当前 API 处理流程。
    const payload = {
        cookie_id: rule.cookie_id || '',
        item_id: rule.item_id || '',
        name: (rule.name || '').trim() || generatedName || '自动化规则',
        trigger_type: triggerType,
        enabled: rule.enabled ?? true,
        priority: rule.priority || 100,
        config_json: rule.config_json || '{}',
        actions: actions.map(/* 当前回调用于处理集合元素或接口响应。 */ (action, index) => ({
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
    return rule.id ? put(`/api/v1/automation-rules/${rule.id}`, payload) : post('/api/v1/automation-rules', payload);
}

// deleteShippingRule 删除发货规则。
export const deleteShippingRule = async (id: string): Promise<OperationResponse> => del(`/api/v1/automation-rules/${id}`);

export interface AutomationRunIssue {
  /** id 表示标识。 */ id: number;
  /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
  /** order_id 表示订单标识。 */ order_id: string;
  /** trigger_type 表示触发条件类型。 */ trigger_type: string;
  /** error_message 表示错误消息。 */ error_message: string;
  /** issue_kind 表示问题类型。 */ issue_kind: 'external_result_unknown' | 'invalid_snapshot' | 'rule_unavailable' | 'partial_failure' | 'execution_failed';
  /** allowed_resolutions 表示允许的解决方式。 */ allowed_resolutions: Array<'continue' | 'retry' | 'cancel'>;
  /** action_cursor 表示动作游标。 */ action_cursor: number;
  /** sent_count 表示已发送数量。 */ sent_count: number;
  /** updated_at 表示最后更新时间。 */ updated_at: string;
}

export interface DeferredAutomationIssue {
  /** id 表示标识。 */ id: number;
  /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
  /** trigger_type 表示触发条件类型。 */ trigger_type: string;
  /** error_message 表示错误消息。 */ error_message: string;
  /** attempt_count 表示尝试次数。 */ attempt_count: number;
  /** updated_at 表示最后更新时间。 */ updated_at: string;
}

// getAutomationIssues 读取自动化问题列表。
export const getAutomationIssues = async (): Promise<{ /** runs 表示运行记录。 */ runs: AutomationRunIssue[]; /** pending_tasks 表示待处理任务列表。 */ pending_tasks: DeferredAutomationIssue[] }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<AutomationIssuesEnvelope>('/api/v1/automation-issues');
  return {
    runs: Array.isArray(result?.runs) ? result.runs : [],
    pending_tasks: Array.isArray(result?.pending_tasks) ? result.pending_tasks : [],
  };
};

// resolveAutomationRun 处理自动化运行记录。
export const resolveAutomationRun = async (id: number, resolution: 'continue' | 'retry' | 'cancel'): Promise<OperationResponse> =>
  post(`/api/v1/automation-runs/${id}/resolve`, { resolution });

// resolveDeferredAutomationTask 处理延迟自动化任务。
export const resolveDeferredAutomationTask = async (id: number, resolution: 'retry' | 'dismiss'): Promise<OperationResponse> =>
  post(`/api/v1/automation-pending-tasks/${id}/resolve`, { resolution });

// Rules - 关键词回复规则 (使用关键词API)
type KeywordRowPayload = {
    /** id 表示标识。 */ id: string;
    /** keyword 表示关键词。 */ keyword: string;
    /** reply 表示回复内容。 */ reply: string;
    /** item_id 表示商品标识。 */ item_id: string;
    /** type 表示规则类型。 */ type: 'text' | 'image';
    /** image_url 表示图片地址。 */ image_url: string;
};

// normalizeKeywordRow 归一化关键词规则。
const normalizeKeywordRow = (item: any): KeywordRowPayload => ({
    id: String(item?.id || ''),
    keyword: item?.keyword || '',
    reply: item?.reply || '',
    item_id: item?.item_id || '',
    type: item?.type === 'image' ? 'image' : 'text',
    image_url: item?.image_url || '',
});

// getKeywordRowsWithType 读取带类型的关键词规则。
const getKeywordRowsWithType = async (cookieId: string): Promise<KeywordRowPayload[]> => {
    // existing 已有规则，用于当前 API 处理流程。
    const existing = await get<KeywordTypedResponse[]>(`/api/v1/reply-rules/${cookieId}/typed`);
    return Array.isArray(existing) ? existing.map(normalizeKeywordRow) : [];
};

// getReplyRules 读取回复规则。
export const getReplyRules = async (cookieId?: string): Promise<ReplyRule[]> => {
    if (!cookieId) return [];
    // keywords keywords，用于当前 API 处理流程。
    const keywords = await getKeywordRowsWithType(cookieId);
	return keywords.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
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

// updateReplyRule 更新回复规则。
export const updateReplyRule = async (rule: Partial<ReplyRule>, cookieId: string): Promise<OperationResponse> => {
	// type 规则类型，用于当前 API 处理流程。
	const type = rule.type || 'text';
	// payload 请求载荷，用于当前 API 处理流程。
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

// deleteReplyRule 删除回复规则。
export const deleteReplyRule = async (id: string, cookieId: string): Promise<OperationResponse> => {
	return del(`/api/v1/reply-rules/${cookieId}/typed/${id}`);
}

// Settings
/** 敏感系统设置的显式三态变更命令。 */
export type SensitiveSettingChange = {
  /** 敏感设置的三态动作。 */
  action: 'retain' | 'replace' | 'clear';
  /** replace 动作要保存的新秘密。 */
  value?: string;
};

/** 系统设置批量更新请求，敏感值只能通过 secrets 命令提交。 */
export type SystemSettingsUpdate = {
  /** 普通系统设置字段集合。 */
  values?: Record<string, unknown>;
  /** 敏感系统设置命令集合。 */
  secrets?: Record<string, SensitiveSettingChange>;
};

// SENSITIVE_SYSTEM_SETTING_KEYS 保存不能进入普通 values 的秘密配置键。
const SENSITIVE_SYSTEM_SETTING_KEYS = new Set(['ai_api_key', 'smtp_password', 'qq_reply_secret_key', 'captcha.remote_secret_key']);

/** 将兼容的设置草稿转换为普通值与敏感命令分离的请求。 */
const normalizeSystemSettingsUpdate = (settings: Partial<SystemSettings> | SystemSettingsUpdate): Record<string, unknown> => {
  if ('values' in settings || 'secrets' in settings) return settings as Record<string, unknown>;
  // values 保存不会泄露秘密的普通设置。
  const values: Record<string, unknown> = {};
  // secrets 保存需要服务端执行的敏感设置命令。
  const secrets: Record<string, SensitiveSettingChange> = {};
  // entry 表示当前遍历的设置键值对。
  for (const entry /* entry 表示当前遍历的设置键值对。 */ of Object.entries(settings)) {
    // key、value 是当前设置键和值。
    const [key, value] = entry;
    if (value === undefined || value === null) continue;
    if (SENSITIVE_SYSTEM_SETTING_KEYS.has(key)) {
      secrets[key] = value === '' ? { action: 'clear' } : { action: 'replace', value: String(value) };
    } else {
      values[key] = value;
    }
  }
  return Object.keys(secrets).length > 0 ? { values, secrets } : values;
};

// getSystemSettings 读取系统设置。
export const getSystemSettings = async (options?: RequestControlOptions): Promise<SystemSettings> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await get<{/** data 表示数据。 */ data: SystemSettings}>('/api/v1/settings/system', undefined, options);
    return normalizeSettings(res.data || res); // handle {success:true, data: {...}} wrapper if exists
};

// updateSystemSettings 更新系统设置。
export const updateSystemSettings = async (settings: Partial<SystemSettings> | SystemSettingsUpdate, options?: RequestControlOptions): Promise<OperationResponse> => {
	// payload 是普通设置与敏感变更命令分离后的请求体。
	const payload = normalizeSystemSettingsUpdate(settings);
	return put('/api/v1/settings/system', payload, options);
};

// getAccountAISettings 读取账号人工智能设置。
export const getAccountAISettings = async (cookieId: string, options?: RequestControlOptions): Promise<AIReplySettingsResponse> => {
    return get(`/api/v1/settings/ai-reply/${cookieId}`, undefined, options);
}

// updateAccountAISettings 更新账号人工智能设置。
export const updateAccountAISettings = async (cookieId: string, settings: Partial<AIReplySettings>, options?: RequestControlOptions): Promise<OperationResponse> => {
  // payload 请求载荷，用于当前 API 处理流程。
  const payload = {
    ai_enabled: settings.ai_enabled ?? false,
    max_discount_percent: settings.max_discount_percent ?? 10,
    max_discount_amount: settings.max_discount_amount ?? 100,
    max_bargain_rounds: settings.max_bargain_rounds ?? 3,
    custom_prompts: settings.custom_prompts ?? ''
  };
  return put(`/api/v1/settings/ai-reply/${cookieId}`, payload, options);
}

// fetchAIModels 读取人工智能模型列表。
export const fetchAIModels = async (baseUrl: string, apiKey: string = '', options?: RequestControlOptions): Promise<string[]> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await post<AIModelsResponse>('/api/v1/settings/ai-models', {
    base_url: baseUrl,
    api_key: apiKey,
  }, options);
  return result.models || [];
};

// Notification Channels
// parseNotificationEventTypes 解析通知事件类型。
const parseNotificationEventTypes = (raw: unknown): NotificationEventType[] => {
  if (Array.isArray(raw)) return raw.filter(Boolean) as NotificationEventType[];
  if (typeof raw !== 'string' || !raw.trim()) return [];
  try {
    // parsed 转换后的数值，用于当前 API 处理流程。
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.filter(Boolean) as NotificationEventType[];
  } catch {
    // fall back to legacy comma/semicolon separated values
  }
  return raw.split(/[,\s;]+/).map(/* 当前回调用于处理集合元素或接口响应。 */ v => v.trim()).filter(Boolean) as NotificationEventType[];
};

// stringifyNotificationEventTypes 序列化通知事件类型。
const stringifyNotificationEventTypes = (events?: NotificationEventType[]): string => {
  // clean 清理后的事件类型列表，用于当前 API 处理流程。
  const clean = Array.from(new Set((events || []).filter(Boolean)));
  return clean.length > 0 ? JSON.stringify(clean) : '';
};

// getNotificationChannels 读取通知渠道。
export const getNotificationChannels = async (options?: RequestControlOptions): Promise<{ /** success 表示是否成功。 */ success: boolean; /** data 表示数据。 */ data?: NotificationChannel[] }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<NotificationChannelResponse[]>('/api/v1/notifications/channels', undefined, options);
  // channels 通知渠道列表，用于当前 API 处理流程。
  const channels = (result || []).map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => {
    // parsedConfig 解析后的通知配置，用于当前 API 处理流程。
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

// createNotificationChannel 创建通知渠道。
export const createNotificationChannel = async (data: { /** name 表示名称。 */ name: string; /** type 表示规则类型。 */ type: string; /** config 表示配置对象。 */ config: Record<string, unknown>; /** event_types 表示通知事件类型列表。 */ event_types?: NotificationEventType[]; /** enabled 表示启用状态。 */ enabled?: boolean }, options?: RequestControlOptions): Promise<MutationIDResponse> => {
  return post('/api/v1/notifications/channels', {
    ...data,
    config: JSON.stringify(data.config),
    event_types: stringifyNotificationEventTypes(data.event_types)
  }, options);
}

// updateNotificationChannel 更新通知渠道。
export const updateNotificationChannel = async (channelId: string, data: { /** name 表示名称。 */ name?: string; /** type 表示规则类型。 */ type?: string; /** config 表示配置对象。 */ config?: Record<string, unknown>; /** event_types 表示通知事件类型列表。 */ event_types?: NotificationEventType[]; /** enabled 表示启用状态。 */ enabled?: boolean }, options?: RequestControlOptions): Promise<OperationResponse> => {
  // payload 请求载荷，用于当前 API 处理流程。
  const payload: Record<string, unknown> = { ...data };
  if ('config' in data) {
    payload.config = JSON.stringify(data.config);
  }
  if ('event_types' in data) {
    payload.event_types = stringifyNotificationEventTypes(data.event_types);
  }
  return put(`/api/v1/notifications/channels/${channelId}`, payload, options);
}

// deleteNotificationChannel 删除通知渠道。
export const deleteNotificationChannel = async (channelId: string, options?: RequestControlOptions): Promise<OperationResponse> => {
  return del(`/api/v1/notifications/channels/${channelId}`, undefined, options);
}

// Message Notifications
// getMessageNotifications 读取消息通知。
export const getMessageNotifications = async (): Promise<{ /** success 表示是否成功。 */ success: boolean; /** data 表示数据。 */ data?: NotificationBinding[] }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<Record<string, NotificationBinding[]>>('/api/v1/notifications/messages');
  // notifications 通知列表，用于当前 API 处理流程。
  const notifications: NotificationBinding[] = [];
  for (const // [cookieId, channelList] [cookieId,通知渠道List]，用于当前 API 处理流程。
[cookieId, channelList] of Object.entries(result || {})) {
    if (Array.isArray(channelList)) {
      for (const // item 当前条目，用于当前 API 处理流程。
item of channelList) {
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

// setMessageNotification 设置消息通知。
export const setMessageNotification = async (cookieId: string, channelId: number, enabled: boolean): Promise<OperationResponse> => {
  return post(`/api/v1/notifications/accounts/${cookieId}/bindings`, { channel_id: channelId, enabled });
}

// deleteMessageNotification 删除消息通知。
export const deleteMessageNotification = async (notificationId: string): Promise<OperationResponse> => {
  return del(`/api/v1/notifications/messages/${notificationId}`);
}

// deleteAccountNotifications 删除账号通知。
export const deleteAccountNotifications = async (cookieId: string): Promise<OperationResponse> => {
  return del(`/api/v1/notifications/messages/account/${cookieId}`);
}

// 账号 ↔ 渠道 绑定（覆盖式）
export const getAccountBindings = async (cookieId: string, options?: RequestControlOptions): Promise<number[]> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<AccountBindingsResponse>(`/api/v1/notifications/accounts/${cookieId}/bindings`, undefined, options);
  return result?.channel_ids || [];
}

// setAccountBindings 设置账号通知绑定。
export const setAccountBindings = async (cookieId: string, channelIds: number[]): Promise<OperationResponse> => {
  return post(`/api/v1/notifications/accounts/${cookieId}/bindings`, { channel_ids: channelIds });
}

// 测试发送
export const testNotificationChannel = async (channelId: string, options?: RequestControlOptions): Promise<OperationResponse> => {
  return post(`/api/v1/notifications/channels/${channelId}/test`, {}, options);
}

// Default Reply
// getDefaultReplies 读取默认回复列表。
export const getDefaultReplies = async (): Promise<Record<string, DefaultReplyResponse>> => {
	return get('/api/v1/default-replies');
};

// getDefaultReply 读取默认回复。
export const getDefaultReply = async (cookieId: string): Promise<DefaultReply> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const result = await get<DefaultReplyResponse>(`/api/v1/default-replies/${cookieId}`);
  return {
    cookie_id: cookieId,
    enabled: result.enabled || false,
    reply_content: result.reply_content || '',
    reply_once: result.reply_once || false,
    reply_image_url: result.reply_image_url || ''
  };
};

// updateDefaultReply 更新默认回复。
export const updateDefaultReply = async (cookieId: string, data: Partial<DefaultReply>): Promise<OperationResponse> => {
  return put(`/api/v1/default-replies/${cookieId}`, {
    enabled: data.enabled ?? false,
    reply_content: data.reply_content || '',
    reply_once: data.reply_once ?? false,
    reply_image_url: data.reply_image_url || ''
  });
};

// deleteDefaultReply 删除默认回复。
export const deleteDefaultReply = async (cookieId: string): Promise<OperationResponse> => {
	return del(`/api/v1/default-replies/${cookieId}`);
};

// clearDefaultReplyRecords 清理默认回复记录。
export const clearDefaultReplyRecords = async (cookieId: string): Promise<OperationResponse> => {
	return post(`/api/v1/default-replies/${cookieId}/clear-records`, {});
};

// getItemDetail 获取指定账号下单个商品的详情。
export const getItemDetail = async (cookieId: string, itemId: string): Promise<ItemDetailResponse> => {
  return get(`/api/v1/items/${cookieId}/${itemId}`);
};
