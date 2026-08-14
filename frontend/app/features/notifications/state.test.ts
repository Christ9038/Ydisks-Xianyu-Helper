import { expect, test } from 'vitest';
import type { NotificationForm } from './types';
import { buildNotificationPayload, isCurrentNotificationRequest, notificationEventSummary, validateNotificationForm } from './state';

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
