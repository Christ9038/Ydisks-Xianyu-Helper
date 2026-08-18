import {
AIReplySettings,
AIReplySettingsResponse,
AccountBindingsResponse,
AccountDetail,AccountSummaryResponse,
AccountTaskRunResponseEnvelope,
AccountTaskSettings,
AccountTaskSettingsResponse,
CookieProfileResponse,
CookieSettingsResponse,
NotificationChannel,
NotificationChannelResponse,
NotificationEventType,
OperationResponse,
QRLoginGenerateResponse,QRLoginStatusResponse,QRLoginVerificationResponse
} from '../../../shared/api-contract/accounts';
import { del,get,post,put,type RequestControlOptions } from '../../../shared/http/client';
import { collectionFrom,objectFrom } from '../../../shared/http/contract';
export type * from '../../../shared/api-contract/accounts';
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
  const response = await get<unknown>('/api/v1/accounts/details', undefined, options);
  // data 是兼容数组、null 和历史 data 包裹后的账号摘要列表。
  const data = collectionFrom<AccountSummaryResponse>(response, ['data', 'accounts', 'details']);
  // avatarVersion 头像缓存版本，用于当前 API 处理流程。
  const avatarVersion = Date.now().toString();
  return data.map(/* 当前回调用于处理集合元素或接口响应。 */ item => ({
    id: item.id,
    cookie_configured: item.has_cookie === true,
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
    pause_duration: item.pause_duration,
    paused_until: Number(item.paused_until || 0),
    paused: item.paused === true,
    username: item.username || '',
    login_password_configured: undefined,
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


export interface AccountRuntimeStatus {
  /** state 表示状态。 */ state: NonNullable<AccountDetail['runtime_state']>;
  /** message 表示消息数据。 */ message?: string;
  /** connected 表示连接状态。 */ connected: boolean;
  /** failures 表示失败次数。 */ failures: number;
  /** updated_at 表示最后更新时间。 */ updated_at: string;
}

// getAccountRuntimeStatuses 读取账号运行状态。
export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, AccountRuntimeStatus>> => {
  const response = await get<unknown>('/api/v1/accounts/runtime-status', undefined, options);
  // statuses 是兼容直接映射、statuses 包裹和 null 的运行状态索引。
  const statuses = objectFrom<Record<string, AccountRuntimeStatus>>(response, ['statuses', 'data', 'result']);
  return statuses || {};
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
  /** error 保存密码登录流程返回的失败说明；前端只用于界面提示，不应记录或序列化登录凭证。 */ error?: string;
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
  const response = await get<unknown>('/api/v1/settings/ai-reply', undefined, options);
  // settings 是兼容直接映射、data 包裹和 null 的账号 AI 设置索引。
  return objectFrom<Record<string, AIReplySettings>>(response, ['settings', 'data', 'result']) || {};
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

// getNotificationChannels 读取通知渠道。
export const getNotificationChannels = async (options?: RequestControlOptions): Promise<{ /** success 表示是否成功。 */ success: boolean; /** data 表示数据。 */ data?: NotificationChannel[] }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<unknown>('/api/v1/notifications/channels', undefined, options);
  // channels 通知渠道列表，用于当前 API 处理流程。
  const channels = collectionFrom<NotificationChannelResponse>(result, ['data', 'channels', 'items']).map(/* 当前回调用于处理集合元素或接口响应。 */ (item: NotificationChannelResponse) => {
    // parsedConfig 解析后的通知配置，用于当前 API 处理流程。
    let parsedConfig;
    try {
      parsedConfig = typeof item.config === 'string' ? JSON.parse(item.config) : item.config;
    } catch {
		parsedConfig = {};
    }
    // normalizedType 是兼容旧渠道别名后的前端渠道类型。
    const normalizedType = (item.type === 'ding_talk' ? 'dingtalk' : (item.type === 'lark' ? 'feishu' : item.type)) as NotificationChannel['type'];
    return {
      id: String(item.id),
      name: item.name,
		type: normalizedType,
      config: parsedConfig,
      event_types: parseNotificationEventTypes(item.event_types),
      enabled: item.enabled,
      created_at: item.created_at,
      updated_at: item.updated_at,
    };
  });
  return { success: true, data: channels };
}


// 账号 ↔ 渠道 绑定（覆盖式）
export const getAccountBindings = async (cookieId: string, options?: RequestControlOptions): Promise<number[]> => {
  // response 是兼容直接绑定对象、data 包裹和 null 的通知绑定响应。
  const response = await get<unknown>(`/api/v1/notifications/accounts/${cookieId}/bindings`, undefined, options);
  // result 是去除历史包裹后的账号通知绑定对象。
  const result = objectFrom<Partial<AccountBindingsResponse>>(response, ['data', 'result']) || {};
  return Array.isArray(result.channel_ids) ? result.channel_ids : [];
}
