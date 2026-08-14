import { afterEach, expect, test, vi } from 'vitest';
import {
  addAccount,
  cancelPasswordLogin,
  deleteItemPublishBatch,
  checkPasswordLoginStatus,
  completeQRVerification,
  createNotificationChannel,
  getAccountDetails,
	getAutomationIssues,
  getItems,
	getItemPublishBatches,
	getNotificationChannels,
  getOrders,
	getOrderAnalytics,
  getReplyRules,
  getShippingRules,
  getShippingRulesPage,
  getSystemSettings,
	getValidOrders,
	publishItem,
	logout,
	importOrders,
  passwordLogin,
	resolveAutomationRun,
	resolveDeferredAutomationTask,
	syncOrders,
  updateReplyRule,
  deleteReplyRule,
  updateAccountCookie,
  updateAccountLoginInfo,
	updateAccountSettings,
  updateItem,
  updateNotificationChannel,
  updateSystemSettings,
  updateShippingRule,
	getChatSessions,
	getChatMessages,
	sendChatMessage,
	markChatRead,
	updateAccountTaskSettings,
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
	expect(fetchMock).toHaveBeenCalledWith('/system-settings', expect.objectContaining({ method: 'PUT', credentials: 'include' }));
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
	expect(fetchMock.mock.calls[0][0]).toBe('/api/chat/sessions?account_id=a1');
	expect(fetchMock.mock.calls[1][0]).toBe('/api/chat/messages?account_id=a1&chat_id=c1&before_id=9');
	expect(JSON.parse(fetchMock.mock.calls[2][1].body)).toMatchObject({ account_id: 'a1', chat_id: 'c1', buyer_id: 'b1' });
	expect(JSON.parse(fetchMock.mock.calls[3][1].body)).toEqual({ account_id: 'a1', chat_id: 'c1' });
});

test('account task APIs keep rating and polish account-scoped', async () => {
	const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(jsonResponse({ success: true, summary: { task_type: 'auto_rate' } })));
	vi.stubGlobal('fetch', fetchMock);
	await updateAccountTaskSettings('a1', {
		account_id: 'a1', auto_rate_enabled: true, rate_content: '交易愉快',
		auto_polish_enabled: true, polish_time: '03:00',
	});
	await runAccountTask('a1', 'auto_rate');
	expect(fetchMock.mock.calls[0][0]).toBe('/api/account-tasks/a1');
	expect(fetchMock.mock.calls[0][1].method).toBe('PUT');
	expect(fetchMock.mock.calls[1][0]).toBe('/api/account-tasks/a1/run');
	expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ task_type: 'auto_rate' });
});

test('getItemPublishBatches unwraps persisted batch list', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ batches: [{ id: 'batch-1', status: 'running' }] }));
	vi.stubGlobal('fetch', fetchMock);
	await expect(getItemPublishBatches(10)).resolves.toEqual([{ id: 'batch-1', status: 'running' }]);
	expect(fetchMock).toHaveBeenCalledWith('/items/publish-batches?limit=10', expect.objectContaining({ credentials: 'include' }));
});

test('automation issue APIs expose and resolve quarantined work', async () => {
	const fetchMock = vi.fn()
		.mockResolvedValueOnce(jsonResponse({ runs: [{ id: 1 }], pending_tasks: [{ id: 2 }] }))
		.mockImplementation(() => Promise.resolve(jsonResponse({ success: true })));
	vi.stubGlobal('fetch', fetchMock);
	await expect(getAutomationIssues()).resolves.toEqual({ runs: [{ id: 1 }], pending_tasks: [{ id: 2 }] });
	await resolveAutomationRun(1, 'continue');
	await resolveDeferredAutomationTask(2, 'retry');
	expect(fetchMock.mock.calls[1][0]).toBe('/automation-runs/1/resolve');
	expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ resolution: 'continue' });
	expect(fetchMock.mock.calls[2][0]).toBe('/automation-pending-tasks/2/resolve');
});

test('order multipart requests use the shared authenticated form request path', async () => {
	const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
	vi.stubGlobal('fetch', fetchMock);
	await syncOrders('acc1', 'pending_ship');
	await importOrders(new FormData());
	expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/orders/refresh', expect.objectContaining({ method: 'POST', credentials: 'include', body: expect.any(FormData) }));
	expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/orders/import', expect.objectContaining({ method: 'POST', credentials: 'include', body: expect.any(FormData) }));
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
  expect(fetchMock).toHaveBeenCalledWith('/api/orders?page=1&page_size=20&search=buyer', expect.objectContaining({ method: 'GET' }));
});

test('getOrders maps unsupported backend statuses to unknown', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
    data: [{ order_id: 'o-unknown', order_status: 'legacy_status' }],
  })));
  const result = await getOrders();
  expect(result.data[0].status).toBe('unknown');
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
    '/automation-rules?page=2&page_size=20&cookie_id=acc1&trigger_type=order_paid&enabled=false&search=%E5%95%86%E5%93%81',
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
  expect(fetchMock).toHaveBeenCalledWith('/qr-login/complete-verification/session-1', expect.objectContaining({
    method: 'POST',
    body: JSON.stringify({ target_account_id: 'acc1' }),
  }));
});

test('deleteItemPublishBatch removes an abandoned preview', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true }));
  vi.stubGlobal('fetch', fetchMock);
  await deleteItemPublishBatch('preview-1');
  expect(fetchMock).toHaveBeenCalledWith('/items/publish-batches/preview-1', expect.objectContaining({
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

  expect(fetchMock).toHaveBeenCalledWith('/items?cookie_id=account-2', expect.objectContaining({
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
  expect(fetchMock).toHaveBeenCalledWith('/cookies', expect.objectContaining({
    method: 'POST',
    credentials: 'include',
  }));
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    id: 'acc1',
    value: 'unb=acc1',
    login_method: 'qr_scan',
  });

  await updateAccountCookie('acc1', 'unb=acc1; x=1', 'qr_scan');
  expect(fetchMock).toHaveBeenCalledWith('/cookies/acc1', expect.objectContaining({
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
	expect(fetchMock).toHaveBeenCalledWith('/cookies/acc1/settings', expect.objectContaining({ method: 'PUT' }));
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
  expect(fetchMock).toHaveBeenCalledWith('/cookies/acc1/login-info', expect.objectContaining({
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
  expect(fetchMock).toHaveBeenCalledWith('/items/acc1/item1', expect.objectContaining({
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
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/password-login', expect.objectContaining({
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
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/password-login/check/sid', expect.objectContaining({ method: 'GET' }));

  await cancelPasswordLogin('sid');
  expect(fetchMock).toHaveBeenNthCalledWith(3, '/password-login/cancel/sid', expect.objectContaining({ method: 'DELETE' }));
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
  expect(fetchMock).toHaveBeenCalledWith('/keywords-with-type/acc1', expect.objectContaining({ method: 'GET' }));
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
  expect(fetchMock).toHaveBeenCalledWith('/keywords-with-type/acc1/42', expect.objectContaining({
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
  expect(fetchMock).toHaveBeenCalledWith('/keywords-with-type/acc1/42', expect.objectContaining({
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

  expect(fetchMock).toHaveBeenCalledWith('/notification-channels/7', expect.objectContaining({
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

  expect(fetchMock).toHaveBeenCalledWith('/automation-rules', expect.objectContaining({
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
