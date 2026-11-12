import { expect,test } from 'vitest';
import type { NotificationChannel } from './api';
import { buildNotificationPayload,emptyNotificationForm,isCurrentNotificationRequest,normalizeNotificationForm,notificationErrorMessage,notificationEventSummary,notificationEvents,validateNotificationForm } from './state';
import type { NotificationForm } from './types';

// createForm 创建通知渠道校验使用的最小表单对象。
const createForm = (overrides: Partial<NotificationForm> = {}): NotificationForm => ({
  name: '测试渠道', type: 'bark', enabled: true, config: { server_url: 'https://api.day.app', device_key: 'device-key' }, event_types: [], ...overrides,
});

test('通知渠道校验阻止缺少名称和必填配置',
  // 渠道表单测试验证名称、渠道凭据和成功保存请求体。
  () => {
    expect(validateNotificationForm(createForm({ name: '' }))).toBe('请填写渠道名称');
    expect(validateNotificationForm(createForm({ config: {} }))).toContain('Bark 服务器');
    expect(validateNotificationForm(createForm())).toBe('');
    expect(buildNotificationPayload(createForm({ event_types: ['account_offline'] })).event_types).toEqual(['account_offline']);
  });

test('通知事件摘要为空时表示订阅全部事件',
  // 事件摘要测试验证空选择和多事件选择的展示语义。
  () => {
    expect(notificationEventSummary([])).toBe('全部事件');
    expect(notificationEventSummary(['account_offline', 'system_error'])).toBe('掉线通知、系统错误');
  });

test('旧版统一交易开关归一为自动化与人工处理开关',
  // 兼容测试验证旧渠道编辑后会展开四个自动化事件及人工处理事件。
  () => {
  // legacyChannel 是保存过旧版交易事件编码的渠道摘要。
  const legacyChannel = { id: 'legacy-channel', ...createForm({ event_types: ['delivery_result'] }) } as NotificationChannel;
  // normalized 是编辑器展示的细分事件集合。
  const normalized = normalizeNotificationForm(legacyChannel, {});
  expect(normalized.event_types).toEqual(expect.arrayContaining([
    'automation_order_created', 'automation_order_paid', 'automation_buyer_reviewed', 'automation_review_missing_timeout',
    'manual_delivery_result', 'manual_intervention_required',
  ]));
  expect(notificationEvents.filter(
    // event 是当前通知事件定义，用于统计自动化任务类别数量。
    event => event.value.startsWith('automation_'),
  )).toHaveLength(4);
  expect(notificationEventSummary(['manual_intervention_required'])).toBe('需要人工处理');
  // 买家请求人工客服是独立类别，不属于自动化任务，也不被旧版统一交易开关展开。
  expect(notificationEventSummary(['chat_manual_requested'])).toBe('人工客服');
  expect(normalized.event_types).not.toContain('chat_manual_requested');
  });

test('通知请求代次拒绝过期响应',
  // 过期响应测试验证刷新或取消后旧请求不能覆盖当前状态。
  () => {
    expect(isCurrentNotificationRequest(5, 5)).toBe(true);
    expect(isCurrentNotificationRequest(4, 5)).toBe(false);
  });

test('通知表单覆盖各渠道归一化和独立 SMTP 校验',
  // 表单分支测试验证非邮件、继承 SMTP 和独立 SMTP 三条保存路径。
  () => {
    expect(emptyNotificationForm()).toMatchObject({ name: '', type: 'bark', enabled: true, config: {}, event_types: [] });
    // webhook 是非邮件渠道配置复制测试使用的渠道对象。
    const webhook = { id: 'webhook-1', name: 'Webhook', type: 'webhook', config: { webhook_url: 'https://example.com' }, enabled: false } as NotificationChannel;
    // smtp 是继承或补全独立 SMTP 配置时使用的系统设置。
    const smtp = { smtp_server: 'smtp.example.com', smtp_port: 465, smtp_user: 'sender@example.com', smtp_password: 'secret', smtp_from_address: 'sender@example.com' };
    expect(normalizeNotificationForm(webhook, smtp)).toMatchObject({ name: 'Webhook', type: 'webhook', enabled: false, config: { webhook_url: 'https://example.com' }, event_types: [] });
    // inherited 是未开启独立 SMTP 时的邮件渠道表单。
    const inherited = normalizeNotificationForm({ id: 'email-1', name: '邮件', type: 'email', config: { to_email: 'to@example.com' }, enabled: true }, smtp);
    expect(inherited.config).toMatchObject({ to_email: 'to@example.com', use_custom_smtp: false });
    expect(validateNotificationForm({ ...createForm(), type: 'email', config: { to_email: 'to@example.com', use_custom_smtp: true } })).toBe('请填写 独立 SMTP 服务器');
    // custom 是字段完整的独立 SMTP 邮件渠道表单。
    const custom = { ...createForm(), type: 'email' as const, config: { to_email: 'to@example.com', use_custom_smtp: true, smtp_server: 'smtp.example.com', smtp_port: 465, smtp_user: 'sender@example.com', smtp_password: 'secret', smtp_from_address: 'sender@example.com' } };
    expect(validateNotificationForm(custom)).toBe('');
    expect(buildNotificationPayload(custom).config).toMatchObject({ to_email: 'to@example.com', use_custom_smtp: true });
    expect(notificationErrorMessage(new Error('网络失败'), '备用错误')).toBe('网络失败');
    expect(notificationErrorMessage({}, '备用错误')).toBe('备用错误');
  });

// 原有 SMTP 保留模式只发送收件地址，明确重新配置时才发送完整 SMTP 配置。
test('邮件编辑保留现有 SMTP，显式替换仍需完整配置', /* preservedSMTPTest 验证表单状态与请求载荷边界。 */ () => {
  // form 模拟旧版独立 SMTP 渠道，只在浏览器内保存非秘密编辑值。
  const form = createForm({ type: 'email', preserveSMTP: true, config: { to_email: 'new@example.com', use_custom_smtp: true } });
  expect(validateNotificationForm(form)).toBe('');
  expect(buildNotificationPayload(form)).toMatchObject({ email_recipient: 'new@example.com' });
  expect(buildNotificationPayload(form).config).toBeUndefined();
  expect(validateNotificationForm({ ...form, preserveSMTP: false })).toContain('SMTP 服务器');
  expect(validateNotificationForm({ ...form, config: {} })).toContain('收件邮箱');
  expect(buildNotificationPayload({ ...form, preserveSMTP: false, config: { to_email: 'new@example.com', use_custom_smtp: false } })).toMatchObject({ config: { to_email: 'new@example.com', use_custom_smtp: false } });
});
