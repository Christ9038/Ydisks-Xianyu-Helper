// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { NotificationChannel, SystemSettings } from '../../../types';
import { createNotificationChannel, deleteNotificationChannel, getNotificationChannels, getSystemSettings, testNotificationChannel, updateNotificationChannel, updateSystemSettings } from './api';
import { useNotifications } from './hooks';

vi.mock('./api', /* notificationsApiMockFactory 提供通知 Hook 的确定性 API 替身。 */ () => ({
  createNotificationChannel: vi.fn(),
  deleteNotificationChannel: vi.fn(),
  getNotificationChannels: vi.fn(),
  getSystemSettings: vi.fn(),
  testNotificationChannel: vi.fn(),
  updateNotificationChannel: vi.fn(),
  updateSystemSettings: vi.fn(),
}));

// createChannelMock 是新建通知渠道请求的可控替身。
const createChannelMock = vi.mocked(createNotificationChannel);
// deleteChannelMock 是删除通知渠道请求的可控替身。
const deleteChannelMock = vi.mocked(deleteNotificationChannel);
// getChannelsMock 是通知渠道列表请求的可控替身。
const getChannelsMock = vi.mocked(getNotificationChannels);
// getSmtpMock 是管理员 SMTP 设置请求的可控替身。
const getSmtpMock = vi.mocked(getSystemSettings);
// testChannelMock 是测试通知发送请求的可控替身。
const testChannelMock = vi.mocked(testNotificationChannel);
// updateChannelMock 是通知渠道更新请求的可控替身。
const updateChannelMock = vi.mocked(updateNotificationChannel);
// updateSmtpMock 是系统 SMTP 保存请求的可控替身。
const updateSmtpMock = vi.mocked(updateSystemSettings);

// channelFixture 是覆盖启用状态和事件订阅的通知渠道对象。
const channelFixture: NotificationChannel = { id: 'channel-1', name: '测试渠道', type: 'bark', config: { server_url: 'https://api.day.app', device_key: 'device-key' }, event_types: ['system_error'], enabled: true };
// smtpFixture 是管理员 SMTP 配置对象。
const smtpFixture: SystemSettings = { smtp_server: 'smtp.example.com', smtp_port: 587, smtp_user: 'sender@example.com', smtp_password: 'secret' };

describe('useNotifications', /* 当前回调处理通知渠道、SMTP 和动作状态。 */ () => {
  beforeEach(/* 当前回调重置通知 API 替身和浏览器确认框。 */ () => {
    vi.clearAllMocks();
    getChannelsMock.mockResolvedValue({ success: true, data: [channelFixture] });
    getSmtpMock.mockResolvedValue(smtpFixture);
    createChannelMock.mockResolvedValue({ success: true, id: 2 });
    deleteChannelMock.mockResolvedValue({ success: true });
    testChannelMock.mockResolvedValue({ success: true });
    updateChannelMock.mockResolvedValue({ success: true });
    updateSmtpMock.mockResolvedValue({ success: true });
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  test('管理员加载渠道和 SMTP 后可以新建、切换和保存', /* 当前回调验证通知 Hook 的成功动作路径。 */ async () => {
    // hook 是管理员通知 Hook 的渲染结果。
    const hook = renderHook(
      // adminHookFactory 创建管理员通知 Hook。
      () => useNotifications(true),
    );
    await waitFor(
      // loadingAssertion 等待通知渠道加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(hook.result.current.channels).toEqual([channelFixture]);
    expect(hook.result.current.smtp).toEqual(smtpFixture);

    await act(
      // openCreateAction 打开新建渠道表单。
      () => hook.result.current.openCreate(),
    );
    await act(
      // formAction 写入完整的渠道表单。
      () => hook.result.current.setForm({ name: '新渠道', type: 'bark', enabled: true, config: { server_url: 'https://api.day.app', device_key: 'new-key' }, event_types: [] }),
    );
    await act(
      // saveAction 提交新建渠道请求。
      async () => hook.result.current.handleSave(),
    );
    expect(createChannelMock).toHaveBeenCalledWith(expect.objectContaining({ name: '新渠道', type: 'bark' }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.showModal).toBe(false);
    expect(hook.result.current.toast).toEqual({ type: 'success', text: '已创建' });

    await act(
      // toggleAction 切换渠道启用状态。
      async () => hook.result.current.handleToggleEnabled(channelFixture),
    );
    expect(updateChannelMock).toHaveBeenCalledWith('channel-1', { enabled: false }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    await act(
      // testAction 发送渠道测试通知。
      async () => hook.result.current.handleTest(channelFixture),
    );
    expect(testChannelMock).toHaveBeenCalledWith('channel-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    await act(
      // smtpAction 保存系统 SMTP 配置。
      async () => hook.result.current.handleSaveSmtp(),
    );
    expect(updateSmtpMock).toHaveBeenCalledWith(expect.objectContaining({ smtp_port: 587 }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });

  test('渠道校验失败和删除成功都保持明确状态', /* 当前回调验证通知 Hook 的校验与删除路径。 */ async () => {
    // hook 是渠道校验和删除场景下的通知 Hook 渲染结果。
    const hook = renderHook(
      // userHookFactory 创建普通用户通知 Hook。
      () => useNotifications(false),
    );
    await waitFor(
      // loadingAssertion 等待普通用户渠道加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(hook.result.current.smtp).toEqual({});
    await act(
      // openCreateAction 打开校验场景的渠道表单。
      () => hook.result.current.openCreate(),
    );
    await act(
      // invalidFormAction 写入缺少必填字段的表单。
      () => hook.result.current.setForm({ name: '', type: 'bark', enabled: true, config: {}, event_types: [] }),
    );
    await act(
      // invalidSaveAction 提交非法渠道表单。
      async () => hook.result.current.handleSave(),
    );
    expect(createChannelMock).not.toHaveBeenCalled();
    expect(hook.result.current.toast?.type).toBe('error');
    await act(
      // deleteAction 删除已存在的渠道。
      async () => hook.result.current.handleDelete(channelFixture),
    );
    expect(deleteChannelMock).toHaveBeenCalledWith('channel-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.toast).toEqual({ type: 'success', text: '已删除' });
  });

  test('测试通知失败时清理测试状态并展示错误', /* 当前回调验证通知发送失败路径。 */ async () => {
    testChannelMock.mockRejectedValueOnce(new Error('通知服务失败'));
    // hook 是通知测试失败场景下的 Hook 渲染结果。
    const hook = renderHook(
      // failureHookFactory 创建测试通知失败场景的 Hook。
      () => useNotifications(false),
    );
    await waitFor(
      // loadingAssertion 等待失败场景的渠道列表加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await act(
      // failedTestAction 提交会失败的测试通知请求。
      async () => hook.result.current.handleTest(channelFixture),
    );
    expect(hook.result.current.testingId).toBe('');
    expect(hook.result.current.toast).toEqual({ type: 'error', text: '通知服务失败' });
  });
});
