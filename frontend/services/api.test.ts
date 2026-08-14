import { afterEach, expect, test, vi } from 'vitest';
import {
  addAccount,
  cancelPasswordLogin,
  deleteItemPublishBatch, deleteItem, cancelItemPublishBatch, retryFailedItemPublishBatch,
  checkPasswordLoginStatus,
  checkQRLoginStatus, completeQRVerification, generateQRLogin,
	createNotificationChannel, getAllAISettings, getAccountAISettings, updateAccountAISettings, fetchAIModels,
  getAccountDetails, getAccountRuntimeStatuses, updateAccountStatus,
	getAutomationIssues, changePassword, createItem, deleteAccount, deleteOrder, deleteShippingRule, updateLoginCredentials,
	getItems, getItemDetail, syncItemsFromAccount,
	getItemPublishBatches, getItemPublishBatch, startItemPublishBatch,
	getNotificationChannels, getMessageNotifications, getAccountBindings,
  getOrders, getOrderDetail, updateOrder,
	getAdminStats, getDashboardStats, getOrderAnalytics,
	getReplyRules, getDefaultReplies, getDefaultReply, updateDefaultReply, deleteDefaultReply, clearDefaultReplyRecords,
  getShippingRules,
  getShippingRulesPage,
	getSystemSettings, getCards, createCard, updateCard, deleteCard, getCardDetails, batchCreateCards, appendCardData,
	getValidOrders,
	publishItem, recommendPublishCategory, previewItemPublishBatch,
	logout,
	importOrders,
  passwordLogin,
	resolveAutomationRun,
	resolveDeferredAutomationTask,
	syncOrders, syncSingleOrder, manualShipOrder,
  updateReplyRule,
  deleteReplyRule,
  updateAccountCookie,
  updateAccountLoginInfo,
	updateAccountSettings, updateAccountRemark, updateAccountAutoConfirm, updateAccountPauseDuration, getLongLoginSettings, setLongLoginSettings, refreshAccountProfile,
  updateItem,
	updateNotificationChannel, deleteNotificationChannel, setMessageNotification, deleteMessageNotification, deleteAccountNotifications, setAccountBindings, testNotificationChannel,
  updateSystemSettings,
  updateShippingRule,
	getChatSessions, getChatSessionPage,
	getChatMessages, getChatMessagePage,
	sendChatMessage, sendChatImage,
	markChatRead,
	getAccountTaskSettings, updateAccountTaskSettings,
	runAccountTask, login, initializeAdmin, verifySession,
} from './api';

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

test('updateSystemSettings uses one atomic bulk request', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
	vi.stubGlobal('fetch', fetchMock);
	await updateSystemSettings({ theme_color: 'blue', renewal_log_retention_days: 15 });
	expect(fetchMock).toHaveBeenCalledTimes(1);
	expect(fetchMock).toHaveBeenCalledWith('/api/v1/settings/system', expect.objectContaining({ method: 'PUT', credentials: 'include' }));
	expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ theme_color: 'blue', renewal_log_retention_days: 15 });
});

test('chat APIs preserve account and conversation scope', async () => {
	const fetchMock = vi.fn()
		.mockResolvedValueOnce(jsonResponse({ sessions: [{ account_id: 'a1', chat_id: 'c1' }] }))
		.mockResolvedValueOnce(jsonResponse({ messages: [{ account_id: 'a1', chat_id: 'c1', id: 1 }] }))
		.mockImplementation(() => Promise.resolve(jsonResponse({ success: true, message: { id: 2 } })));
	vi.stubGlobal('fetch', fetchMock);
	await getChatSessions('a1');
	await getChatMessages('a1', 'c1', 9);
	await sendChatMessage({ account_id: 'a1', chat_id: 'c1', buyer_id: 'b1', text: 'hi' });
	await markChatRead('a1', 'c1');
	expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/chat/sessions?account_id=a1');
	expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/chat/messages?account_id=a1&chat_id=c1&before_id=9');
	expect(JSON.parse(fetchMock.mock.calls[2][1].body)).toMatchObject({ account_id: 'a1', chat_id: 'c1', buyer_id: 'b1' });
	expect(JSON.parse(fetchMock.mock.calls[3][1].body)).toEqual({ account_id: 'a1', chat_id: 'c1' });
});

test('Chat 会话、消息和发送 API 转发外部取消信号', async () => {
  // fetchMock 验证会话切换、消息分页和文本/图片发送共享取消控制能力。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ sessions: [], has_more: false }))
    .mockResolvedValueOnce(jsonResponse({ messages: [], has_more: false }))
    .mockResolvedValueOnce(jsonResponse({ success: true, message: { message_key: 'm1' } }))
    .mockResolvedValueOnce(jsonResponse({ success: true, message: { message_key: 'm2' } }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);
  // controller 是 Chat feature Hook 使用的请求控制器。
  const controller = new AbortController();
  await getChatSessionPage('a1', undefined, { signal: controller.signal });
  await getChatMessagePage('a1', 'c1', undefined, undefined, { signal: controller.signal });
  await sendChatMessage({ account_id: 'a1', chat_id: 'c1', buyer_id: 'b1', text: 'hi' }, { signal: controller.signal });
  await sendChatImage({ account_id: 'a1', chat_id: 'c1', buyer_id: 'b1', image: new File(['image'], 'chat.png', { type: 'image/png' }) }, { signal: controller.signal });
  await markChatRead('a1', 'c1', { signal: controller.signal });
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/chat/sessions?account_id=a1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/chat/messages?account_id=a1&chat_id=c1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/chat/messages', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/chat/images', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/chat/read', expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('account task APIs keep rating and polish account-scoped', async () => {
	const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(jsonResponse({ success: true, summary: { task_type: 'auto_rate' } })));
	vi.stubGlobal('fetch', fetchMock);
	await updateAccountTaskSettings('a1', {
		account_id: 'a1', auto_rate_enabled: true, rate_content: '交易愉快',
		auto_polish_enabled: true, polish_time: '03:00',
	});
	await runAccountTask('a1', 'auto_rate');
	expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/account-tasks/a1');
	expect(fetchMock.mock.calls[0][1].method).toBe('PUT');
	expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/account-tasks/a1/run');
	expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ task_type: 'auto_rate' });
});

test('getItemPublishBatches unwraps persisted batch list', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ batches: [{ id: 'batch-1', status: 'running' }] }));
	vi.stubGlobal('fetch', fetchMock);
	await expect(getItemPublishBatches(10)).resolves.toEqual([{ id: 'batch-1', status: 'running' }]);
	expect(fetchMock).toHaveBeenCalledWith('/api/v1/items/publish-batches?limit=10', expect.objectContaining({ credentials: 'include' }));
});

test('automation issue APIs expose and resolve quarantined work', async () => {
	const fetchMock = vi.fn()
		.mockResolvedValueOnce(jsonResponse({ runs: [{ id: 1 }], pending_tasks: [{ id: 2 }] }))
		.mockImplementation(() => Promise.resolve(jsonResponse({ success: true })));
	vi.stubGlobal('fetch', fetchMock);
	await expect(getAutomationIssues()).resolves.toEqual({ runs: [{ id: 1 }], pending_tasks: [{ id: 2 }] });
	await resolveAutomationRun(1, 'continue');
	await resolveDeferredAutomationTask(2, 'retry');
	expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/automation-runs/1/resolve');
	expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ resolution: 'continue' });
	expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/automation-pending-tasks/2/resolve');
});

test('order multipart requests use the shared authenticated form request path', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
	vi.stubGlobal('fetch', fetchMock);
	await syncOrders('acc1', 'pending_ship');
	await importOrders(new FormData());
	expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/orders/refresh', expect.objectContaining({ method: 'POST', credentials: 'include', body: expect.any(FormData) }));
	expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/orders/import', expect.objectContaining({ method: 'POST', credentials: 'include', body: expect.any(FormData) }));
});

test('legacy notification channel aliases are normalized for the editor', async () => {
	vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([{ id: 1, name: '旧飞书', type: 'lark', config: 'not-json', enabled: true }])));
	const result = await getNotificationChannels();
	expect(result.data?.[0]).toMatchObject({ type: 'feishu', config: {} });
});

const jsonResponse = (body: unknown) => new Response(JSON.stringify(body), {
  status: 200,
  headers: { 'content-type': 'application/json' },
});

test('getOrders normalizes backend order fields', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
    orders: [{ order_id: 'o1', order_status: 'shipped', quantity: '2' }],
    total: 1,
  }));
  vi.stubGlobal('fetch', fetchMock);
  const result = await getOrders(undefined, 'all', 1, 20, ' buyer ');
  expect(result.data[0]).toMatchObject({ id: 'o1', status: 'shipped', quantity: 2 });
  expect(result.total).toBe(1);
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/orders?page=1&page_size=20&search=buyer', expect.objectContaining({ method: 'GET' }));
});

test('getOrders maps unsupported backend statuses to unknown', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
    data: [{ order_id: 'o-unknown', order_status: 'legacy_status' }],
  })));
  const result = await getOrders();
  expect(result.data[0].status).toBe('unknown');
});

test('订单查询和导入 API 转发外部取消信号', async () => {
  // fetchMock 是同时验证订单查询和文件上传请求控制参数的替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ data: [], total_pages: 1 }))
    .mockResolvedValueOnce(jsonResponse({ success_count: 1, failed_count: 0, results: [] }));
  vi.stubGlobal('fetch', fetchMock);
  // controller 是 feature Hook 传入 API 的取消控制器。
  const controller = new AbortController();
  await getOrders(undefined, 'all', 1, 20, '', { signal: controller.signal });
  await importOrders(new FormData(), { signal: controller.signal });
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/orders?page=1&page_size=20', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/orders/import', expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('Dashboard 统计 API 转发外部取消信号', async () => {
  // fetchMock 验证 Dashboard 的概览、趋势和订单明细共用同一个取消信号。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ total_cookies: 1, active_cookies: 1, available_card_stock: 2 }))
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ revenue_stats: { total_amount: 1, total_orders: 1 }, daily_stats: [] }))
    .mockResolvedValueOnce(jsonResponse({ revenue_stats: { total_amount: 0, total_orders: 0 }, daily_stats: [] }))
    .mockResolvedValueOnce(jsonResponse({ orders: [], total: 0, truncated: false }));
  vi.stubGlobal('fetch', fetchMock);
  // controller 是 Dashboard feature Hook 传入 API 的请求控制器。
  const controller = new AbortController();
  await getDashboardStats({ signal: controller.signal });
  await getItems(undefined, { signal: controller.signal });
  await getOrderAnalytics({ start_date: '2026-08-15', end_date: '2026-08-15' }, { signal: controller.signal });
  await getValidOrders({ start_date: '2026-08-15', end_date: '2026-08-15' }, { signal: controller.signal });
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/analytics/dashboard', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, expect.stringContaining('/api/v1/analytics/orders?'), expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, expect.stringContaining('/api/v1/analytics/orders/valid?'), expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('Settings 配置、模型和凭据 API 转发外部取消信号', async () => {
  // fetchMock 验证 Settings 的读取、模型发现和凭据保存共享取消控制能力。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ data: { log_level: 'info' } }))
    .mockResolvedValueOnce(jsonResponse({ models: ['model-a'] }))
    .mockResolvedValueOnce(jsonResponse({ authenticated: true, username: 'admin' }))
    .mockResolvedValueOnce(jsonResponse({ success: true, message: '已更新' }));
  vi.stubGlobal('fetch', fetchMock);
  // controller 是 Settings feature Hook 使用的请求控制器。
  const controller = new AbortController();
  await getSystemSettings({ signal: controller.signal });
  await fetchAIModels('https://ai.example.com', 'secret', { signal: controller.signal });
  await verifySession({ signal: controller.signal });
  await updateLoginCredentials({ current_password: 'old', new_username: 'admin' }, { signal: controller.signal });
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/settings/system', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/settings/ai-models', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/session', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/session/credentials', expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('通知渠道和 SMTP API 转发外部取消信号', async () => {
  // fetchMock 验证渠道读取、保存和 SMTP 读写都支持取消控制。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ data: {} }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);
  // controller 是通知 feature 传给服务 API 的共享取消控制器。
  const controller = new AbortController();
  await getNotificationChannels({ signal: controller.signal });
  await updateNotificationChannel('channel-1', { enabled: false }, { signal: controller.signal });
  await getSystemSettings({ signal: controller.signal });
  await updateSystemSettings({ smtp_server: 'smtp.example.com' }, { signal: controller.signal });
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/notifications/channels', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/notifications/channels/channel-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/settings/system', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/settings/system', expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('getShippingRulesPage sends filters and preserves pagination metadata', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
    data: [{ id: 7, name: '付款规则', trigger_type: 'order_paid', enabled: false, actions: [] }],
    total: 21,
    page: 2,
    page_size: 20,
    total_pages: 2,
    trigger_counts: { order_paid: 8, buyer_reviewed: 7, review_missing_timeout: 6 },
  }));
  vi.stubGlobal('fetch', fetchMock);

  const result = await getShippingRulesPage({
    cookieId: 'acc1',
    triggerType: 'order_paid',
    enabled: false,
    search: '  商品 ',
    page: 2,
    pageSize: 20,
  });

  expect(result).toMatchObject({
    total: 21,
    page: 2,
    page_size: 20,
    total_pages: 2,
    trigger_counts: { order_paid: 8, buyer_reviewed: 7, review_missing_timeout: 6 },
  });
  expect(result.data[0]).toMatchObject({ id: '7', name: '付款规则', enabled: false });
  expect(fetchMock).toHaveBeenCalledWith(
	    '/api/v1/automation-rules?page=2&page_size=20&cookie_id=acc1&trigger_type=order_paid&enabled=false&search=%E5%95%86%E5%93%81',
    expect.objectContaining({ method: 'GET', credentials: 'include' }),
  );
});

test('getValidOrders accepts wrapped responses', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
    orders: [{ order_id: 'o2', order_status: 'completed', quantity: '3' }],
	}));
	vi.stubGlobal('fetch', fetchMock);
	vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(-480);
  const result = await getValidOrders({ start_date: '2026-01-01', end_date: '2026-01-02' });
  expect(result).toEqual({
    orders: [expect.objectContaining({ id: 'o2', status: 'completed', quantity: 3 })],
    total: 1,
    truncated: false,
  });
	expect(fetchMock.mock.calls[0][0]).toContain('timezone_offset_minutes=480');
});

test('getOrderAnalytics sends the browser timezone offset', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ revenue_stats: {}, daily_stats: [], status_stats: [], city_stats: [] }));
	vi.stubGlobal('fetch', fetchMock);
	vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(-330);
	await getOrderAnalytics({ start_date: '2026-01-01', end_date: '2026-01-02' });
	expect(fetchMock.mock.calls[0][0]).toContain('timezone_offset_minutes=330');
});

test('paid orders are normalized to pending shipment', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: [{ order_id: 'o-paid', order_status: 'paid' }] })));
  const result = await getOrders();
  expect(result.data[0].status).toBe('pending_ship');
});

test('completeQRVerification sends only the immutable target account', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, account_id: 'acc1' }));
  vi.stubGlobal('fetch', fetchMock);
  await completeQRVerification('session-1', 'acc1');
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/qr-login/complete-verification/session-1', expect.objectContaining({
    method: 'POST',
    body: JSON.stringify({ target_account_id: 'acc1' }),
  }));
});

test('deleteItemPublishBatch removes an abandoned preview', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);
  await deleteItemPublishBatch('preview-1');
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/items/publish-batches/preview-1', expect.objectContaining({
    method: 'DELETE',
    credentials: 'include',
  }));
});

test('publishItem allows virtual publishing without an optional location', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);
  await publishItem({
    cookie_id: 'acc1',
    title: '虚拟商品',
    description: '',
    price: '12.50',
    quantity: 1,
    postage_mode: 'none',
    images: [],
  });
  const body = fetchMock.mock.calls[0][1].body as FormData;
  expect(body.get('location')).toBeNull();
});

test('getItems normalizes multi-spec flags from backend values', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([{
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    item_title: '普通商品',
    is_multi_spec: '0',
    multi_quantity_delivery: 0,
  }, {
    cookie_id: 'cookie-1',
    item_id: 'item-2',
    item_title: '多规格商品',
    is_multi_spec: '1',
    multi_quantity_delivery: 1,
  }])));

  const items = await getItems();
  expect(items[0]).toMatchObject({
    id: 'cookie-1-item-1',
    is_multi_spec: false,
    is_multi_qty_ship: false,
    multi_quantity_delivery: false,
  });
  expect(items[1]).toMatchObject({
    id: 'cookie-1-item-2',
    is_multi_spec: true,
    is_multi_qty_ship: true,
    multi_quantity_delivery: true,
  });
});

test('getItems forwards the selected account filter', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse([]));
  vi.stubGlobal('fetch', fetchMock);

  await getItems('account-2');

  expect(fetchMock).toHaveBeenCalledWith('/api/v1/items?cookie_id=account-2', expect.objectContaining({
    method: 'GET',
    credentials: 'include',
  }));
});

test('getSystemSettings normalizes numeric renewal retention', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
    ai_model: 'qwen-plus',
    renewal_log_retention_days: 'invalid',
  })));

  const settings = await getSystemSettings();
  expect(settings.ai_model).toBe('qwen-plus');
  expect(settings.renewal_log_retention_days).toBe(10);
});

test('logout calls backend session invalidation route', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await logout();
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/session/logout', expect.objectContaining({
    method: 'POST',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({});
});

test('account cookie APIs include login_method when provided', async () => {
  const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(jsonResponse({ success: true })));
  vi.stubGlobal('fetch', fetchMock);

  await addAccount('acc1', 'unb=acc1', 'qr_scan');
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/accounts', expect.objectContaining({
    method: 'POST',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    id: 'acc1',
    value: 'unb=acc1',
    login_method: 'qr_scan',
  });

  await updateAccountCookie('acc1', 'unb=acc1; x=1', 'qr_scan');
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/accounts/acc1', expect.objectContaining({
    method: 'PUT',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({
    id: 'acc1',
    value: 'unb=acc1; x=1',
    login_method: 'qr_scan',
  });
});

test('account editor settings use one aggregate request', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
	vi.stubGlobal('fetch', fetchMock);
	await updateAccountSettings('acc1', {
	  remark: 'main', auto_confirm: false, pause_duration: 5,
	  username: 'user', show_browser: true, channel_ids: [1, 2],
	});
	expect(fetchMock).toHaveBeenCalledTimes(1);
	expect(fetchMock).toHaveBeenCalledWith('/api/v1/accounts/acc1/settings', expect.objectContaining({ method: 'PUT' }));
	expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
	  remark: 'main', auto_confirm: false, pause_duration: 5,
	  username: 'user', show_browser: true, channel_ids: [1, 2],
	});
});

test('getAccountDetails normalizes show_browser and never exposes password', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([{
    id: 'acc1',
    enabled: true,
    auto_confirm: true,
    remark: '主账号',
    pause_duration: 0,
    paused_until: 1780000000,
    paused: true,
    username: 'login-user',
    show_browser: '1',
    login_password: 'should-not-leak',
  }])));

  const accounts = await getAccountDetails();
  expect(accounts[0]).toMatchObject({
    id: 'acc1',
    username: 'login-user',
    show_browser: true,
    login_password: '',
    paused_until: 1780000000,
    paused: true,
  });
});

test('updateAccountLoginInfo sends exactly provided fields', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await updateAccountLoginInfo('acc1', { username: 'login-user', show_browser: false });
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/accounts/acc1/login-info', expect.objectContaining({
    method: 'PUT',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    username: 'login-user',
    show_browser: false,
  });
});

test('updateAccountLoginInfo can request explicit password clearing', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await updateAccountLoginInfo('acc1', { username: 'login-user', clear_password: true, show_browser: false });
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    username: 'login-user',
    clear_password: true,
    show_browser: false,
  });
});

test('updateItem sends only the fields selected by the editor', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await updateItem('acc1', 'item1', { item_title: '改名商品' });
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/items/acc1/item1', expect.objectContaining({
    method: 'PUT',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    item_title: '改名商品',
  });
});

test('password login service uses upstream-compatible routes', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true, session_id: 'sid', status: 'processing' }))
    .mockResolvedValueOnce(jsonResponse({ status: 'success', account_id: 'acc1', cookie_count: 2 }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await passwordLogin({ account_id: 'acc1', account: 'u', password: 'p' });
	  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/password-login', expect.objectContaining({
    method: 'POST',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    account_id: 'acc1',
    account: 'u',
    password: 'p',
  });

  const status = await checkPasswordLoginStatus('sid');
  expect(status.status).toBe('success');
	  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/password-login/check/sid', expect.objectContaining({ method: 'GET' }));

  await cancelPasswordLogin('sid');
	  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/password-login/cancel/sid', expect.objectContaining({ method: 'DELETE' }));
});

test('getShippingRules exposes buyer reviewed gift rules as automation rules', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([{
    id: 12,
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    item_title: '测试商品',
    name: '评价后发送赠品 - 测试商品',
    trigger_type: 'buyer_reviewed',
    enabled: true,
    priority: 90,
    config_json: '{}',
    actions: [{
      id: 33,
      action_type: 'send_card',
      card_id: 7,
      card_name: '赠品库存',
      delivery_count: 1,
      config_json: '{"spec_name":"套餐","spec_value":"赠品"}',
      enabled: true,
      sort_order: 1,
    }],
  }])));

  const rules = await getShippingRules();
  expect(rules[0]).toMatchObject({
    id: '12',
    trigger_type: 'buyer_reviewed',
    card_group_id: 7,
    card_group_name: '赠品库存',
  });
  expect(rules[0].variants[0]).toMatchObject({
    spec_name: '套餐',
    spec_value: '赠品',
    card_id: 7,
  });
});

test('getReplyRules labels keyword matching according to engine contains behavior', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse([{
    id: 42,
    keyword: '发货',
    reply: '马上安排',
    type: 'image',
    image_url: 'https://img.example/reply.png',
  }]));
  vi.stubGlobal('fetch', fetchMock);

  const rules = await getReplyRules('acc1');
	expect(fetchMock).toHaveBeenCalledWith('/api/v1/reply-rules/acc1/typed', expect.objectContaining({ method: 'GET' }));
  expect(rules[0]).toMatchObject({
    id: '42',
    keyword: '发货',
    reply_content: '马上安排',
    match_type: 'fuzzy',
    type: 'image',
    image_url: 'https://img.example/reply.png',
  });
});

test('updateReplyRule preserves keyword image metadata when saving text edits', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await updateReplyRule({ id: '42', keyword: '发货', reply_content: '稍后安排', item_id: 'item-1' }, 'acc1');

  expect(fetchMock).toHaveBeenCalledTimes(1);
	expect(fetchMock).toHaveBeenCalledWith('/api/v1/reply-rules/acc1/typed/42', expect.objectContaining({
    method: 'PUT',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    keyword: '发货', reply: '稍后安排', item_id: 'item-1', type: 'text', image_url: '',
  });
});

test('updateReplyRule clears stale content when switching reply type', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await updateReplyRule({ id: '42', keyword: '发货', type: 'image', image_url: 'https://img.example/new.png' }, 'acc1');
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    keyword: '发货', reply: '', item_id: '', type: 'image', image_url: 'https://img.example/new.png',
  });
});

test('deleteReplyRule deletes one stable keyword row instead of replacing the list', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await deleteReplyRule('42', 'acc1');
  expect(fetchMock).toHaveBeenCalledTimes(1);
	expect(fetchMock).toHaveBeenCalledWith('/api/v1/reply-rules/acc1/typed/42', expect.objectContaining({
    method: 'DELETE',
    credentials: 'include',
  }));
});

test('createNotificationChannel persists email recipient as to_email config', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await createNotificationChannel({
    name: '邮件通知',
    type: 'email',
    config: {
      smtp_server: 'smtp.example.com',
      smtp_port: 587,
      smtp_user: 'from@example.com',
      smtp_password: 'secret',
      to_email: 'to@example.com',
    },
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.type).toBe('email');
  expect(JSON.parse(body.config)).toMatchObject({
    to_email: 'to@example.com',
  });
  expect(JSON.parse(body.config)).not.toHaveProperty('from');
});

test('createNotificationChannel allows email channel to rely on system SMTP settings', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await createNotificationChannel({
    name: '邮件通知',
    type: 'email',
    config: {
      to_email: 'to@example.com',
    },
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.type).toBe('email');
  expect(JSON.parse(body.config)).toEqual({
    to_email: 'to@example.com',
  });
});

test('updateNotificationChannel supports partial enabled updates', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await updateNotificationChannel('7', { enabled: false });

	expect(fetchMock).toHaveBeenCalledWith('/api/v1/notifications/channels/7', expect.objectContaining({
    method: 'PUT',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    enabled: false,
  });
});

test('updateShippingRule posts buyer reviewed gift payload to automation-rules', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 1 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'buyer_reviewed',
    enabled: true,
    variants: [{
      spec_name: '',
      spec_value: '',
      card_id: 7,
      delivery_count: 1,
      enabled: true,
    }],
  });

	  expect(fetchMock).toHaveBeenCalledWith('/api/v1/automation-rules', expect.objectContaining({
    method: 'POST',
    credentials: 'include',
  }));
  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body).toMatchObject({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    name: '评价后发送赠品 - item-1',
    trigger_type: 'buyer_reviewed',
  });
  expect(body.actions).toEqual([
    expect.objectContaining({
      action_type: 'send_card',
      card_id: 7,
      sort_order: 1,
    }),
  ]);
});

test('updateShippingRule posts every matching card action before confirm shipment', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 3 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'order_paid',
    enabled: true,
    variants: [
      {
        spec_name: '套餐',
        spec_value: '30天',
        card_id: 8,
        delivery_count: 1,
        enabled: true,
      },
      {
        spec_name: '套餐',
        spec_value: '30天',
        card_id: 9,
        delivery_count: 2,
        enabled: true,
        delay_override: true,
        delay_seconds: 0,
      },
    ],
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.trigger_type).toBe('order_paid');
  expect(body.actions).toEqual([
    expect.objectContaining({
      action_type: 'send_card',
      card_id: 8,
      sort_order: 1,
    }),
    expect.objectContaining({
      action_type: 'send_card',
      card_id: 9,
      delivery_count: 2,
      sort_order: 2,
    }),
    expect.objectContaining({
      action_type: 'confirm_shipment',
      sort_order: 3,
    }),
  ]);
  expect(JSON.parse(body.actions[0].config_json)).toEqual({ spec_name: '套餐', spec_value: '30天', delay_override: false });
  expect(JSON.parse(body.actions[1].config_json)).toEqual({ spec_name: '套餐', spec_value: '30天', delay_override: true });
  expect(body.actions[1].delay_seconds).toBe(0);
});

test('updateShippingRule preserves text actions while editing card variants', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 4 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    id: '4',
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'order_paid',
    variants: [{ spec_name: '', spec_value: '', card_id: 8, delivery_count: 1, enabled: true }],
    actions: [{ action_type: 'send_text', message_template: '发货提示', enabled: true, sort_order: 2 }],
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.actions.map((action: { action_type: string }) => action.action_type)).toEqual([
    'send_card',
    'send_text',
    'confirm_shipment',
  ]);
  expect(body.actions[1].message_template).toBe('发货提示');
});

test('updateShippingRule posts review request text action without card requirement', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 2 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'review_missing_timeout',
    enabled: true,
    config_json: '{"after_shipped_hours":48,"max_attempts":2}',
    actions: [{
      action_type: 'send_text',
      message_template: '亲，方便的话麻烦给个评价～',
      enabled: true,
      sort_order: 1,
    }],
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.trigger_type).toBe('review_missing_timeout');
  expect(body.actions).toEqual([
    expect.objectContaining({
      action_type: 'send_text',
      card_id: 0,
      message_template: '亲，方便的话麻烦给个评价～',
    }),
  ]);
});

// 会话 API 使用版本化兼容入口。
const runVersionedSessionAPITest = async () => {
  // fetchMock 是会话 API 请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true, authenticated: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, authenticated: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, authenticated: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, authenticated: true }));
  vi.stubGlobal('fetch', fetchMock);

  await login({ username: 'admin', password: 'pw' });
  await initializeAdmin('long-password');
  await verifySession();
  await logout();

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/session/login', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/session/initialize', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/session', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/session/logout', expect.objectContaining({ method: 'POST' }));
};

test('session APIs use versioned compatibility routes', runVersionedSessionAPITest);

// 账号摘要、详情和状态 API 使用版本化兼容入口。
const runVersionedAccountAPITest = async () => {
  // fetchMock 是账号 API 请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse([{ id: 'acc1', enabled: true, remark: '主账号' }]))
    .mockResolvedValueOnce(jsonResponse({ acc1: { state: 'error', connected: false, failures: 0, updated_at: '' } }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await getAccountDetails();
  await getAccountRuntimeStatuses();
  await updateAccountStatus('acc1', false);

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/accounts/details', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/accounts/runtime-status', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/accounts/acc1/status', expect.objectContaining({ method: 'PUT' }));
};

test('account summary and status APIs use versioned compatibility routes', runVersionedAccountAPITest);

// 账号设置、长登录和资料 API 使用版本化兼容入口。
const runVersionedAccountSettingsAPITest = async () => {
  // fetchMock 是账号设置与资料请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true, paused_until: 0, paused: false }))
    .mockResolvedValueOnce(jsonResponse({ can_open_long_login: true, enabled: false }))
    .mockResolvedValueOnce(jsonResponse({ can_open_long_login: true, enabled: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, id: 'acc1', nickname: '主账号', avatar_url: '', profile_error: '' }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, paused_until: 0, paused: false }));
  vi.stubGlobal('fetch', fetchMock);

  await updateAccountSettings('acc1', { remark: '主账号' });
  await getLongLoginSettings('acc1');
  await setLongLoginSettings('acc1', true);
  await refreshAccountProfile('acc1');
  await updateAccountRemark('acc1', '新的备注');
  await updateAccountAutoConfirm('acc1', true);
  await updateAccountPauseDuration('acc1', 15);

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/accounts/acc1/settings', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/accounts/acc1/long-login', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/accounts/acc1/long-login', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/accounts/acc1/refresh-profile', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/accounts/acc1/remark', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/v1/accounts/acc1/auto-confirm', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/v1/accounts/acc1/pause-duration', expect.objectContaining({ method: 'PUT' }));
};

test('account settings and profile APIs use versioned compatibility routes', runVersionedAccountSettingsAPITest);

// 订单列表、详情和更新 API 使用版本化兼容入口。
const runVersionedOrderAPITest = async () => {
  // fetchMock 是订单 API 请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ data: [{ order_id: 'order-1', order_status: 'pending_ship' }], total: 1 }))
    .mockResolvedValueOnce(jsonResponse({ success: true, order_id: 'order-1', data: { order_id: 'order-1', order_status: 'pending_ship' } }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await getOrders();
  await getOrderDetail('order-1');
  await updateOrder('order-1', { status: 'shipped' });

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/orders?page=1&page_size=20', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/orders/order-1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/orders/order-1', expect.objectContaining({ method: 'PUT' }));
};

test('order list, detail, and update APIs use versioned compatibility routes', runVersionedOrderAPITest);

// 订单刷新与批量操作 API 使用版本化兼容入口。
const runVersionedOrderRefreshAPITest = async () => {
  // fetchMock 是订单刷新与批量请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await syncSingleOrder('order-1');
  await manualShipOrder(['order-1'], 'status_only');

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/orders/order-1/refresh', expect.objectContaining({ method: 'POST', credentials: 'include' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/orders/manual-ship', expect.objectContaining({ method: 'POST', credentials: 'include' }));
};

test('order refresh and batch APIs use versioned compatibility routes', runVersionedOrderRefreshAPITest);

// 商品列表、详情、发布、更新和删除 API 使用版本化兼容入口。
const runVersionedItemAPITest = async () => {
  // fetchMock 是商品 API 请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ cookie_id: 'acc1', item_id: 'item-1', item_title: '商品' }))
    .mockResolvedValueOnce(jsonResponse({ success: true, item_id: 'item-1' }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await getItems('acc1');
  await getItemDetail('acc1', 'item-1');
  await publishItem({
    cookie_id: 'acc1', title: '商品', description: '', price: '1.00', quantity: 1,
    postage_mode: 'none', images: [],
  });
  await updateItem('acc1', 'item-1', { item_title: '新商品名' });
  await deleteItem('acc1', 'item-1');

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/items?cookie_id=acc1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/items/acc1/item-1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/items/publish', expect.objectContaining({ method: 'POST', body: expect.any(FormData) }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/items/acc1/item-1', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/items/acc1/item-1', expect.objectContaining({ method: 'DELETE' }));
};

test('item list, detail, publish, update, and delete APIs use versioned compatibility routes', runVersionedItemAPITest);

// 商品同步、类目推荐和批量发布 API 使用版本化兼容入口。
const runVersionedItemBatchAPITest = async () => {
  // fetchMock 是商品同步和批量发布请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, category: { cat_id: 'cat-1' } }))
    .mockResolvedValueOnce(jsonResponse({ success: true, preview_id: 'preview-1', total: 0, valid: 0, invalid: 0, rows: [] }))
    .mockResolvedValueOnce(jsonResponse({ success: true, batch_id: 'batch-1' }))
    .mockResolvedValueOnce(jsonResponse({ batches: [] }))
    .mockResolvedValueOnce(jsonResponse({ id: 'batch-1', status: 'preview', rows: [] }))
    .mockResolvedValueOnce(jsonResponse({ success: true, status: 'canceled' }))
    .mockResolvedValueOnce(jsonResponse({ success: true, batch_id: 'batch-1' }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await syncItemsFromAccount('acc1');
  await recommendPublishCategory('acc1', '资料');
  await previewItemPublishBatch({
    file: new File(['order_id\nitem-1'], 'items.csv', { type: 'text/csv' }),
    fallbackCategory: { catId: 'cat-1', catName: '资料', channelCatId: 'channel-1' },
  });
  await startItemPublishBatch('preview-1');
  await getItemPublishBatches(10);
  await getItemPublishBatch('batch-1');
  await cancelItemPublishBatch('batch-1');
  await retryFailedItemPublishBatch('batch-1');
  await deleteItemPublishBatch('batch-1');

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/items/get-all-from-account', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/items/publish-categories/recommend', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/items/publish-batches/preview', expect.objectContaining({ method: 'POST', body: expect.any(FormData) }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/items/publish-batches', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/items/publish-batches?limit=10', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/v1/items/publish-batches/batch-1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/v1/items/publish-batches/batch-1/cancel', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/v1/items/publish-batches/batch-1/retry-failed', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/v1/items/publish-batches/batch-1', expect.objectContaining({ method: 'DELETE' }));
};

test('item sync and batch publish APIs use versioned compatibility routes', runVersionedItemBatchAPITest);

// 设置、卡券和通知 API 使用版本化兼容入口。
const runVersionedSettingsCardNotificationAPITest = async () => {
  // fetchMock 是设置、卡券和通知请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ data: {} }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({}))
    .mockResolvedValueOnce(jsonResponse({ ai_enabled: false }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ models: ['qwen-plus'] }))
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ success: true, id: 1 }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ id: 1, name: '卡券', type: 'text', text_content: 'CARD' }))
    .mockResolvedValueOnce(jsonResponse({ success: true, total: 0, created: 0, failed: 0, rows: [] }))
    .mockResolvedValueOnce(jsonResponse({ success: true, added: 1 }))
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ success: true, id: 1 }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({}))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ channel_ids: [] }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await getSystemSettings();
  await updateSystemSettings({ theme_color: 'blue' });
  await getAllAISettings();
  await getAccountAISettings('acc1');
  await updateAccountAISettings('acc1', { ai_enabled: true });
  await fetchAIModels('https://ai.example.com');
  await getCards();
  await createCard({ name: '卡券', type: 'text', text_content: 'CARD' });
  await updateCard(1, { enabled: false });
  await deleteCard(1);
  await getCardDetails(1);
  await batchCreateCards(new File(['name,type,content\n卡券,text,CARD'], 'cards.csv', { type: 'text/csv' }));
  await appendCardData(1, 'CARD-2');
  await getNotificationChannels();
  await createNotificationChannel({ name: '通知', type: 'bark', config: {} });
  await updateNotificationChannel('1', { enabled: false });
  await deleteNotificationChannel('1');
  await getMessageNotifications();
  await setMessageNotification('acc1', 1, true);
  await deleteMessageNotification('1');
  await deleteAccountNotifications('acc1');
  await getAccountBindings('acc1');
  await setAccountBindings('acc1', [1]);
  await testNotificationChannel('1');

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/settings/system', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/settings/system', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/settings/ai-reply', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/settings/ai-reply/acc1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/settings/ai-reply/acc1', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/v1/settings/ai-models', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/v1/cards', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/v1/cards', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/v1/cards/1', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/v1/cards/1', expect.objectContaining({ method: 'DELETE' }));
  expect(fetchMock).toHaveBeenNthCalledWith(11, '/api/v1/cards/1/details', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(12, '/api/v1/cards/batch', expect.objectContaining({ method: 'POST', body: expect.any(FormData) }));
  expect(fetchMock).toHaveBeenNthCalledWith(13, '/api/v1/cards/1/append-data', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(14, '/api/v1/notifications/channels', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(15, '/api/v1/notifications/channels', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(16, '/api/v1/notifications/channels/1', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(17, '/api/v1/notifications/channels/1', expect.objectContaining({ method: 'DELETE' }));
  expect(fetchMock).toHaveBeenNthCalledWith(18, '/api/v1/notifications/messages', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(19, '/api/v1/notifications/accounts/acc1/bindings', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(20, '/api/v1/notifications/messages/1', expect.objectContaining({ method: 'DELETE' }));
  expect(fetchMock).toHaveBeenNthCalledWith(21, '/api/v1/notifications/messages/account/acc1', expect.objectContaining({ method: 'DELETE' }));
  expect(fetchMock).toHaveBeenNthCalledWith(22, '/api/v1/notifications/accounts/acc1/bindings', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(23, '/api/v1/notifications/accounts/acc1/bindings', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(24, '/api/v1/notifications/channels/1/test', expect.objectContaining({ method: 'POST' }));
};

test('settings, card, and notification APIs use versioned compatibility routes', runVersionedSettingsCardNotificationAPITest);

// 聊天和账号任务 API 使用版本化兼容入口。
const runVersionedChatTaskAPITest = async () => {
  // fetchMock 是聊天和账号任务请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ sessions: [], has_more: false }))
    .mockResolvedValueOnce(jsonResponse({ messages: [], has_more: false }))
    .mockResolvedValueOnce(jsonResponse({ sessions: [], has_more: false }))
    .mockResolvedValueOnce(jsonResponse({ messages: [], has_more: false }))
    .mockResolvedValueOnce(jsonResponse({ message: { message_key: 'message-1' } }))
    .mockResolvedValueOnce(jsonResponse({ message: { message_key: 'message-2' } }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ account_id: 'acc1' }))
    .mockResolvedValueOnce(jsonResponse({ account_id: 'acc1' }))
    .mockResolvedValueOnce(jsonResponse({ success: true, summary: { task_type: 'auto_rate' } }));
  vi.stubGlobal('fetch', fetchMock);

  await getChatSessionPage('acc1', 3, undefined, true);
  await getChatMessagePage('acc1', 'chat-1', 4, 9);
  await getChatSessions('acc1');
  await getChatMessages('acc1', 'chat-1', 9);
  await sendChatMessage({ account_id: 'acc1', chat_id: 'chat-1', buyer_id: 'buyer-1', text: '你好' });
  await sendChatImage({ account_id: 'acc1', chat_id: 'chat-1', buyer_id: 'buyer-1', image: new File(['image'], 'chat.png', { type: 'image/png' }) });
  await markChatRead('acc1', 'chat-1');
  await getAccountTaskSettings('acc1');
  await updateAccountTaskSettings('acc1', { account_id: 'acc1', auto_rate_enabled: true, rate_content: '交易愉快', auto_polish_enabled: false, polish_time: '03:00' });
  await runAccountTask('acc1', 'auto_rate');

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/chat/sessions?account_id=acc1&cursor=3&refresh=1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/chat/messages?account_id=acc1&chat_id=chat-1&cursor=4&before_id=9', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/chat/sessions?account_id=acc1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/chat/messages?account_id=acc1&chat_id=chat-1&before_id=9', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/chat/messages', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/v1/chat/images', expect.objectContaining({ method: 'POST', body: expect.any(FormData) }));
  expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/v1/chat/read', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/v1/account-tasks/acc1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/v1/account-tasks/acc1', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/v1/account-tasks/acc1/run', expect.objectContaining({ method: 'POST' }));
};

test('chat and account task APIs use versioned compatibility routes', runVersionedChatTaskAPITest);

// 关键词回复、指定商品回复和默认回复 API 使用版本化兼容入口。
const runVersionedReplyAPITest = async () => {
  // fetchMock 是关键词和默认回复请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, id: 7 }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({}))
    .mockResolvedValueOnce(jsonResponse({ enabled: true, reply_content: '欢迎', reply_once: false, reply_image_url: '' }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await getReplyRules('acc1');
  await updateReplyRule({ id: '42', keyword: '发货', reply_content: '稍后安排' }, 'acc1');
  await updateReplyRule({ keyword: '售后', reply_content: '请联系客服' }, 'acc1');
  await deleteReplyRule('42', 'acc1');
  await getDefaultReplies();
  await getDefaultReply('acc1');
  await updateDefaultReply('acc1', { enabled: true, reply_content: '欢迎' });
  await deleteDefaultReply('acc1');
  await clearDefaultReplyRecords('acc1');

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/reply-rules/acc1/typed', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/reply-rules/acc1/typed/42', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/reply-rules/acc1/items', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/reply-rules/acc1/typed/42', expect.objectContaining({ method: 'DELETE' }));
  expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/v1/default-replies', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/v1/default-replies/acc1', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/v1/default-replies/acc1', expect.objectContaining({ method: 'PUT' }));
  expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/v1/default-replies/acc1', expect.objectContaining({ method: 'DELETE' }));
  expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/v1/default-replies/acc1/clear-records', expect.objectContaining({ method: 'POST' }));
};

test('keyword and default reply APIs use versioned compatibility routes', runVersionedReplyAPITest);

// 管理员、仪表盘和订单分析 API 使用版本化兼容入口。
const runVersionedAdminAnalyticsAPITest = async () => {
  // fetchMock 是管理员和统计分析请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ total_users: 1, total_cookies: 1, active_cookies: 1, total_cards: 0, total_keywords: 0, total_orders: 0 }))
    .mockResolvedValueOnce(jsonResponse({ total_cookies: 1, active_cookies: 1, total_cards: 0, available_card_stock: 0, total_keywords: 0, total_orders: 0 }))
    .mockResolvedValueOnce(jsonResponse({ revenue_stats: {}, daily_stats: [], status_stats: [], city_stats: [], item_stats: [] }))
    .mockResolvedValueOnce(jsonResponse({ orders: [], total: 0, page: 1, page_size: 500, truncated: false }));
  vi.stubGlobal('fetch', fetchMock);

  await getAdminStats();
  await getDashboardStats();
  await getOrderAnalytics({ start_date: '2026-01-01', end_date: '2026-01-02' });
  await getValidOrders({ start_date: '2026-01-01', end_date: '2026-01-02' });

  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/admin/stats', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/analytics/dashboard', expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(3, expect.stringContaining('/api/v1/analytics/orders?start_date=2026-01-01&end_date=2026-01-02'), expect.objectContaining({ method: 'GET' }));
  expect(fetchMock).toHaveBeenNthCalledWith(4, expect.stringContaining('/api/v1/analytics/orders/valid?start_date=2026-01-01&end_date=2026-01-02'), expect.objectContaining({ method: 'GET' }));
};

test('admin and analytics APIs use versioned compatibility routes', runVersionedAdminAnalyticsAPITest);

// 二维码生成和状态轮询使用版本化兼容入口。
const runVersionedQRLoginAPITest = async () => {
  // fetchMock 是二维码生成和状态轮询请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true, session_id: 'session-1', qr_code_url: 'data:image/png;base64,abc' }))
    .mockResolvedValueOnce(jsonResponse({ status: 'waiting', session_id: 'session-1' }));
  vi.stubGlobal('fetch', fetchMock);
  await generateQRLogin();
  await checkQRLoginStatus('session-1');
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/qr-login/generate', expect.objectContaining({ method: 'POST' }));
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/qr-login/check/session-1', expect.objectContaining({ method: 'GET' }));
};

test('QR login generation and polling use versioned routes', runVersionedQRLoginAPITest);

// 密码登录、会话凭证、账号删除、自动化以及剩余订单商品调用使用版本化入口。
const runVersionedRemainingAPITest = async () => {
  // fetchMock 是剩余公共 API 请求的测试替身。
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true, session_id: 'sid' }))
    .mockResolvedValueOnce(jsonResponse({ status: 'failed' }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse([]))
    .mockResolvedValueOnce(jsonResponse({ data: [], total: 0, page: 1, page_size: 10, total_pages: 0 }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ runs: [], pending_tasks: [] }))
    .mockResolvedValueOnce(jsonResponse({ success: true }))
    .mockResolvedValueOnce(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);

  await changePassword('old-password', 'new-password');
  await updateLoginCredentials({ current_password: 'old-password', new_username: 'new-user' });
  await deleteAccount('acc1');
  await passwordLogin({ account_id: 'acc1', account: 'user', password: 'password' });
  await checkPasswordLoginStatus('sid');
  await cancelPasswordLogin('sid');
  await deleteOrder('order-1');
  await createItem('acc1', { item_title: '新商品' });
  await getShippingRules();
  await getShippingRulesPage();
  await updateShippingRule({ cookie_id: 'acc1', trigger_type: 'order_paid' });
  await deleteShippingRule('7');
  await getAutomationIssues();
  await resolveAutomationRun(1, 'retry');
  await resolveDeferredAutomationTask(2, 'dismiss');

  // paths 是请求层实际发出的版本化 URL 顺序。
  const paths: unknown[] = [];
  // index 是当前请求调用在模拟调用列表中的位置。
  let index = 0;
  for (index = 0; index < fetchMock.mock.calls.length; index += 1) {
    paths.push(fetchMock.mock.calls[index][0]);
  }
  expect(paths).toEqual([
    '/api/v1/session/password',
    '/api/v1/session/credentials',
    '/api/v1/accounts/acc1',
    '/api/v1/password-login',
    '/api/v1/password-login/check/sid',
    '/api/v1/password-login/cancel/sid',
    '/api/v1/orders/order-1',
    '/api/v1/items/acc1',
    '/api/v1/automation-rules',
    '/api/v1/automation-rules?page=1&page_size=10',
    '/api/v1/automation-rules',
    '/api/v1/automation-rules/7',
    '/api/v1/automation-issues',
    '/api/v1/automation-runs/1/resolve',
    '/api/v1/automation-pending-tasks/2/resolve',
  ]);
};

test('remaining public APIs use versioned compatibility routes', runVersionedRemainingAPITest);
