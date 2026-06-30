import { get, post, put, del, postForm } from '../request';
import {
  LoginResponse, AccountDetail, Order, PaginatedResponse,
  AdminStats, Card, SystemSettings, ApiResponse, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction
} from '../types';

// Auth
export const login = async (data: { username?: string; password?: string; email?: string; verification_code?: string }): Promise<LoginResponse> => {
  return post('/login', data);
};

export const verifySession = async (): Promise<{ authenticated: boolean; initialized?: boolean; user_id?: number; username?: string; is_admin?: boolean }> => {
  return get('/verify');
};

export const logout = async (): Promise<ApiResponse> => {
  return post('/logout', {});
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
export const addAccount = async (id: string, value: string): Promise<ApiResponse> => {
  return post('/cookies', { id, value });
};

const accountAvatarURL = (item: any, version: string): string => {
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

export const getAccountDetails = async (): Promise<AccountDetail[]> => {
  const data = await get<any[]>('/cookies/details');
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
    username: item.username || '',
    login_password: '',
    show_browser: item.show_browser,
    nickname: item.nickname || item.remark || `账号 ${item.id.substring(0,6)}`,
    avatar_url: accountAvatarURL(item, avatarVersion),
    profile_error: item.profile_error || '',
    ai_enabled: false,
  }));
};

export interface AccountRuntimeStatus {
  state: NonNullable<AccountDetail['runtime_state']>;
  message?: string;
  connected: boolean;
  failures: number;
  updated_at: string;
}

export const getAccountRuntimeStatuses = async (): Promise<Record<string, AccountRuntimeStatus>> => {
  return get('/cookies/runtime-status');
};

export const generateQRLogin = async (): Promise<{ success: boolean; session_id?: string; qr_code_url?: string }> => {
  return post('/qr-login/generate');
};

export const checkQRLoginStatus = async (sessionId: string): Promise<any> => {
  return get(`/qr-login/check/${sessionId}`);
};

export const completeQRVerification = async (sessionId: string): Promise<{ success: boolean; cookies?: string; unb?: string; message?: string }> => {
  return post(`/qr-login/complete-verification/${sessionId}`, {});
};

export const updateAccountStatus = async (id: string, enabled: boolean): Promise<any> => {
  return put(`/cookies/${id}/status`, { enabled });
};

export const deleteAccount = async (id: string): Promise<any> => {
  return del(`/cookies/${id}`);
};

export const updateAccountRemark = async (id: string, remark: string): Promise<any> => {
  return put(`/cookies/${id}/remark`, { remark });
};

export const updateAccountAutoConfirm = async (id: string, autoConfirm: boolean): Promise<any> => {
  return put(`/cookies/${id}/auto-confirm`, { auto_confirm: autoConfirm });
};

export const updateAccountPauseDuration = async (id: string, pauseDuration: number): Promise<any> => {
  return put(`/cookies/${id}/pause-duration`, { pause_duration: pauseDuration });
};

export const updateAccountCookie = async (id: string, value: string): Promise<any> => {
  return put(`/cookies/${id}`, { id, value });
};

export const refreshAccountProfile = async (id: string): Promise<any> => {
  return post(`/cookies/${id}/refresh-profile`, {});
};

export const updateAccountLoginInfo = async (id: string, data: {
  username?: string;
  login_password?: string;
  show_browser?: boolean;
}): Promise<any> => {
  return put(`/cookies/${id}/login-info`, data);
};

export const getAllAISettings = async (): Promise<Record<string, AIReplySettings>> => {
  return get('/ai-reply-settings');
};

// Orders
export const getOrders = async (
  cookieId?: string,
  status?: string,
  page: number = 1,
  pageSize: number = 20
): Promise<PaginatedResponse<Order>> => {
  const params: any = { page, page_size: pageSize };
  if (cookieId) params.cookie_id = cookieId;
  if (status && status !== 'all') params.status = status;

  const res = await get<any>('/api/orders', params);

  // Handle backend response variations
  const rawOrders = res.orders || res.data || [];
  const orders = rawOrders.map((item: any) => ({
    ...item,
    id: item.id || item.order_id,
    status: item.status || item.order_status || 'processing',
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

export const getOrderDetail = async (orderId: string): Promise<{ success: boolean; data?: Order }> => {
  const result = await get<{ order?: Order; data?: Order }>(`/api/orders/${orderId}`);
  return {
    success: true,
    data: result.order || result.data
  };
};

export const updateOrder = async (orderId: string, data: Partial<Order>): Promise<ApiResponse> => {
  return put(`/api/orders/${orderId}`, data);
};

export const deleteOrder = async (orderId: string): Promise<ApiResponse> => {
  return del(`/api/orders/${orderId}`);
};

export const syncOrders = async (cookieId?: string, status?: string): Promise<any> => {
  const formData = new FormData();
  if (cookieId) formData.append('cookie_id', cookieId);
  if (status) formData.append('status', status);

  // 使用 fetch 来发送 FormData（Cookie 会话，自动携带凭证）
  const response = await fetch('/api/orders/refresh', {
    method: 'POST',
    credentials: 'include',
    body: formData
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.detail || result.message || `订单同步失败: ${response.status}`);
  }
  return result;
};

export const syncSingleOrder = async (orderId: string): Promise<any> => {
  return post(`/api/orders/${orderId}/refresh`);
};

export const manualShipOrder = async (orderIds: string[], shipMode: 'status_only' | 'full_delivery', content?: string): Promise<any> => {
    return post('/api/orders/manual-ship', {
        order_ids: orderIds,
        ship_mode: shipMode,
        custom_content: content
    });
}

export const importOrders = async (data: Partial<Order>[] | FormData): Promise<any> => {
  const isFormData = data instanceof FormData;
  const response = await fetch('/api/orders/import', {
    method: 'POST',
    credentials: 'include',
    headers: {
      ...(isFormData ? {} : { 'Content-Type': 'application/json' }),
    },
    body: isFormData ? data : JSON.stringify(data)
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.detail || result.message || `订单导入失败: ${response.status}`);
  }
  return result;
}

// Stats
export const getAdminStats = async (): Promise<AdminStats> => {
  return get('/admin/stats');
};

export const getOrderAnalytics = async (daysOrParams: number | {start_date: string; end_date: string} = 7): Promise<OrderAnalytics> => {
    let params: {start_date: string; end_date: string};

    if (typeof daysOrParams === 'number') {
        const endDate = new Date();
        const startDate = new Date();
        startDate.setDate(startDate.getDate() - daysOrParams);
        params = {
            start_date: startDate.toISOString().split('T')[0],
            end_date: endDate.toISOString().split('T')[0]
        };
    } else {
        params = daysOrParams;
    }

    return get('/analytics/orders', params);
}

export const getValidOrders = async (dateRange: {start_date: string; end_date: string}): Promise<Order[]> => {
    const res = await get<any>('/analytics/orders/valid', {
        start_date: dateRange.start_date,
        end_date: dateRange.end_date
    });
    const orders = Array.isArray(res) ? res : (res.orders || []);
    return orders.map((order: any) => ({
        ...order,
        id: order.id || order.order_id,
        status: order.status || order.order_status || 'processing',
        quantity: Number(order.quantity || 1),
    }));
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
  const res = await get<any>('/cards');
  const cards = Array.isArray(res) ? res : (res.cards || []);
  return cards.map(normalizeCard);
};

export const createCard = async (data: Partial<Card>): Promise<{ id: number; message: string }> => {
  return post('/cards', cardPayload(data));
};

export const updateCard = async (cardId: string | number, data: Partial<Card>): Promise<ApiResponse> => {
  return put(`/cards/${cardId}`, cardPayload(data));
};

export const deleteCard = async (cardId: string | number): Promise<ApiResponse> => {
  return del(`/cards/${cardId}`);
};

export const getCardDetails = async (cardId: string | number): Promise<any> => {
  const card = await get<any>(`/cards/${cardId}/details`);
  return normalizeCard(card);
};

// Items
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

export const getItems = async (): Promise<Item[]> => {
    const res = await get<any>('/items');
    const items = Array.isArray(res) ? res : (res.items || []);
    return items.map((item: any) => ({
      ...item,
      id: item.id || `${item.cookie_id}-${item.item_id}`,
      is_multi_spec: normalizeBooleanFlag(item.is_multi_spec),
      is_multi_qty_ship: normalizeBooleanFlag(item.is_multi_qty_ship ?? item.multi_quantity_delivery),
      multi_quantity_delivery: normalizeBooleanFlag(item.multi_quantity_delivery ?? item.is_multi_qty_ship),
    }));
}

export const syncItemsFromAccount = async (cookieId: string): Promise<any> => {
    return post('/items/get-all-from-account', { cookie_id: cookieId });
}

export const deleteItem = async (cookieId: string, itemId: string): Promise<any> => {
    return del(`/items/${cookieId}/${itemId}`);
}

export const createItem = async (cookieId: string, data: any): Promise<any> => {
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
}): Promise<any> => {
    const body = new FormData();
    body.set('cookie_id', form.cookie_id);
    body.set('title', form.title);
    body.set('description', form.description);
    body.set('price', form.price);
    body.set('original_price', form.original_price || '');
    body.set('quantity', String(form.quantity));
    body.set('postage_mode', form.postage_mode);
    body.set('postage', form.postage || '');
    for (const file of form.images) {
      body.append('images', file);
    }
    return postForm('/items/publish', body);
}

export const previewItemPublishBatch = async (form: {
    file: File;
    imagesZip?: File | null;
    defaultCookieId?: string;
}): Promise<any> => {
    const body = new FormData();
    body.set('file', form.file);
    if (form.imagesZip) body.set('images_zip', form.imagesZip);
    if (form.defaultCookieId) body.set('default_cookie_id', form.defaultCookieId);
    return postForm('/items/publish-batches/preview', body);
}

export const startItemPublishBatch = async (previewId: string): Promise<any> => {
    return post('/items/publish-batches', { preview_id: previewId });
}

export const getItemPublishBatch = async (batchId: string): Promise<any> => {
    return get(`/items/publish-batches/${batchId}`);
}

export const cancelItemPublishBatch = async (batchId: string): Promise<any> => {
    return post(`/items/publish-batches/${batchId}/cancel`, {});
}

export const retryFailedItemPublishBatch = async (batchId: string): Promise<any> => {
    return post(`/items/publish-batches/${batchId}/retry-failed`, {});
}

export const updateItem = async (cookieId: string, itemId: string, data: any): Promise<any> => {
    return put(`/items/${cookieId}/${itemId}`, data);
}

// Rules - 自动化规则
export const getShippingRules = async (): Promise<ShippingRule[]> => {
    const res = await get<any>('/automation-rules');
    const rules = Array.isArray(res) ? res : (res.data || res.rules || []);
    return rules.map((item: any) => ({
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
              config_json: action.config_json || '{}',
            };
          }),
    }));
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

export const updateShippingRule = async (rule: Partial<ShippingRule>): Promise<any> => {
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
    const baseActions: AutomationAction[] = rule.variants && rule.variants.length > 0
      ? rule.variants.map((variant, index) => ({
            action_type: 'send_card' as const,
            card_id: variant.card_id,
            delivery_count: variant.delivery_count || 1,
            enabled: variant.enabled !== false,
            sort_order: index + 1,
            config_json: JSON.stringify({ spec_name: variant.spec_name || '', spec_value: variant.spec_value || '' }),
          }))
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

export const deleteShippingRule = async (id: string): Promise<any> => del(`/automation-rules/${id}`);

// Rules - 关键词回复规则 (使用关键词API)
export const getReplyRules = async (cookieId?: string): Promise<ReplyRule[]> => {
    if (!cookieId) return [];
    const res = await get<any>(`/keywords-with-item-id/${cookieId}`);
    const keywords = Array.isArray(res) ? res : [];
    return keywords.map((item: any, index: number) => ({
        id: String(index),
        keyword: item.keyword || '',
        reply_content: item.reply || '',
        match_type: 'exact' as const,
        enabled: true
    }));
}

export const updateReplyRule = async (rule: Partial<ReplyRule>, cookieId: string): Promise<any> => {
    // 获取现有关键词
    const existing = await get<any>(`/keywords-with-item-id/${cookieId}`);
    const keywords = Array.isArray(existing) ? existing : [];

    // 更新或添加关键词
    if (rule.id) {
        const index = parseInt(rule.id);
        if (index >= 0 && index < keywords.length) {
            keywords[index] = {
                keyword: rule.keyword,
                reply: rule.reply_content,
                item_id: ''
            };
        }
    } else {
        keywords.push({
            keyword: rule.keyword,
            reply: rule.reply_content,
            item_id: ''
        });
    }

    return post(`/keywords-with-item-id/${cookieId}`, { keywords });
}

export const deleteReplyRule = async (id: string, cookieId: string): Promise<any> => {
    const existing = await get<any>(`/keywords-with-item-id/${cookieId}`);
    const keywords = Array.isArray(existing) ? existing : [];
    const index = parseInt(id);
    if (index >= 0 && index < keywords.length) {
        keywords.splice(index, 1);
    }
    return post(`/keywords-with-item-id/${cookieId}`, { keywords });
}

// Settings
export const getSystemSettings = async (): Promise<SystemSettings> => {
    const res = await get<{data: SystemSettings}>('/system-settings');
    return res.data || res; // handle {success:true, data: {...}} wrapper if exists
};

export const updateSystemSettings = async (settings: Partial<SystemSettings>): Promise<ApiResponse> => {
    // API expects individual PUTs, but we'll loop in the service for convenience or assume bulk endpoint if updated
    // Based on docs 12.2, we iterate.
    const promises = Object.entries(settings).map(([key, value]) => {
         return put(`/system-settings/${key}`, { value: String(value) });
    });
    await Promise.all(promises);
    return { success: true, message: 'Settings saved' };
};

export const getAccountAISettings = async (cookieId: string): Promise<AIReplySettings> => {
    return get(`/ai-reply-settings/${cookieId}`);
}

export const updateAccountAISettings = async (cookieId: string, settings: Partial<AIReplySettings>): Promise<ApiResponse> => {
  const payload = {
    ai_enabled: settings.ai_enabled ?? false,
    max_discount_percent: settings.max_discount_percent ?? 10,
    max_discount_amount: settings.max_discount_amount ?? 100,
    max_bargain_rounds: settings.max_bargain_rounds ?? 3,
    custom_prompts: settings.custom_prompts ?? ''
  };
  return put(`/ai-reply-settings/${cookieId}`, payload);
}

export const fetchAIModels = async (baseUrl: string, apiKey: string = ''): Promise<string[]> => {
  const result = await post<{ models?: string[] }>('/ai-models', {
    base_url: baseUrl,
    api_key: apiKey,
  });
  return result.models || [];
};

export const testAIConnection = async (cookieId: string): Promise<ApiResponse> => {
  const result = await post<{ success?: boolean; message?: string; reply?: string }>(`/ai-reply-test/${cookieId}`, {
    message: '你好，这是一条测试消息',
  });
  if (result.reply) {
    return { success: true, message: `AI 回复: ${result.reply}` };
  }
  return { success: result.success ?? true, message: result.message || 'AI 连接测试成功' };
}

// Notification Channels
export const getNotificationChannels = async (): Promise<{ success: boolean; data?: any[] }> => {
  const result = await get<any[]>('/notification-channels');
  const channels = (result || []).map((item: any) => {
    let parsedConfig;
    try {
      parsedConfig = JSON.parse(item.config);
    } catch {
      parsedConfig = undefined;
    }
    return {
      id: String(item.id),
      name: item.name,
      type: item.type,
      config: parsedConfig,
      enabled: item.enabled,
      created_at: item.created_at,
      updated_at: item.updated_at,
    };
  });
  return { success: true, data: channels };
}

export const createNotificationChannel = async (data: { name: string; type: string; config: Record<string, unknown> }): Promise<ApiResponse> => {
  return post('/notification-channels', {
    ...data,
    config: JSON.stringify(data.config)
  });
}

export const updateNotificationChannel = async (channelId: string, data: { name?: string; config?: Record<string, unknown>; enabled?: boolean }): Promise<ApiResponse> => {
  const payload: Record<string, unknown> = { ...data };
  if ('config' in data) {
    payload.config = JSON.stringify(data.config);
  }
  return put(`/notification-channels/${channelId}`, payload);
}

export const deleteNotificationChannel = async (channelId: string): Promise<ApiResponse> => {
  return del(`/notification-channels/${channelId}`);
}

// Message Notifications
export const getMessageNotifications = async (): Promise<{ success: boolean; data?: any[] }> => {
  const result = await get<Record<string, any[]>>('/message-notifications');
  const notifications = [];
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

export const setMessageNotification = async (cookieId: string, channelId: number, enabled: boolean): Promise<ApiResponse> => {
  return post(`/message-notifications/${cookieId}`, { channel_id: channelId, enabled });
}

export const deleteMessageNotification = async (notificationId: string): Promise<ApiResponse> => {
  return del(`/message-notifications/${notificationId}`);
}

export const deleteAccountNotifications = async (cookieId: string): Promise<ApiResponse> => {
  return del(`/message-notifications/account/${cookieId}`);
}

// Default Reply
export const getDefaultReplies = async (): Promise<Record<string, DefaultReply>> => {
  return get('/api/default-replies');
};

export const getDefaultReply = async (cookieId: string): Promise<DefaultReply> => {
  const result = await get<any>(`/api/default-reply/${cookieId}`);
  return {
    cookie_id: cookieId,
    enabled: result.enabled || false,
    reply_content: result.reply_content || '',
    reply_once: result.reply_once || false,
    reply_image_url: result.reply_image_url || ''
  };
};

export const updateDefaultReply = async (cookieId: string, data: Partial<DefaultReply>): Promise<ApiResponse> => {
  return put(`/api/default-reply/${cookieId}`, {
    enabled: data.enabled ?? false,
    reply_content: data.reply_content || '',
    reply_once: data.reply_once ?? false,
    reply_image_url: data.reply_image_url || ''
  });
};

export const deleteDefaultReply = async (cookieId: string): Promise<ApiResponse> => {
  return del(`/api/default-reply/${cookieId}`);
};

export const clearDefaultReplyRecords = async (cookieId: string): Promise<ApiResponse> => {
  return post(`/api/default-reply/${cookieId}/clear-records`, {});
};
