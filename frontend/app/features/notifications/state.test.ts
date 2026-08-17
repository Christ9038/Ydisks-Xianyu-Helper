import { expect, test } from 'vitest';
import type { NotificationChannel } from '../../../shared/api-contract';
import type { NotificationForm } from './types';
import { buildNotificationPayload, emptyNotificationForm, isCurrentNotificationRequest, normalizeNotificationForm, notificationErrorMessage, notificationEventSummary, validateNotificationForm } from './state';

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
