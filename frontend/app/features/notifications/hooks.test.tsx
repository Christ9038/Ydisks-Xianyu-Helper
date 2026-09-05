// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { NotificationChannel,SystemSettings } from './api';
import { createNotificationChannel,deleteNotificationChannel,getNotificationChannel,getNotificationChannels,getSystemSettings,testNotificationChannel,updateNotificationChannel,updateSystemSettings } from './api';
import { useNotifications } from './hooks';

vi.mock('./api', /* notificationsApiMockFactory 提供通知 Hook 的确定性 API 替身。 */ () => ({
  createNotificationChannel: vi.fn(),
  deleteNotificationChannel: vi.fn(),
  getNotificationChannel: vi.fn(),
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
// getChannelMock 是通知渠道编辑配置请求的可控替身。
const getChannelMock = vi.mocked(getNotificationChannel);
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
    getChannelMock.mockResolvedValue({ id: 1, name: '测试渠道', type: 'bark', enabled: true });
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

  test('编辑邮件渠道时回显服务端保存的收件邮箱', /* 当前回调验证编辑配置异步加载不会丢失收件地址。 */ async () => {
    // emailChannel 是编辑收件邮箱回显场景使用的渠道摘要。
    const emailChannel: NotificationChannel = { id: 'email-1', name: '邮件通知', type: 'email', config: {}, enabled: true, event_types: [] };
    getChannelMock.mockResolvedValueOnce({ id: 1, name: '邮件通知', type: 'email', enabled: true, to_email: 'receiver@example.com', use_custom_smtp: false });
    // hook 是邮件渠道编辑场景的 Hook 渲染结果。
    const hook = renderHook(/* editHookFactory 创建邮件渠道编辑场景的 Hook。 */ () => useNotifications(false));
    await waitFor(/* loadingAssertion 等待渠道摘要加载完成。 */ () => expect(hook.result.current.loading).toBe(false));

    await act(/* editAction 读取脱敏编辑配置并打开邮件渠道弹窗。 */ async () => hook.result.current.openEdit(emailChannel));
    expect(hook.result.current.form.config).toMatchObject({ to_email: 'receiver@example.com', use_custom_smtp: false });
    expect(getChannelMock).toHaveBeenCalledWith('email-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    hook.unmount();
  });

  // 脱敏 SMTP 配置保存测试验证缺省密码不会被意外清空。
  test('脱敏 SMTP 配置保存时不会意外清空服务端密码', /* 当前回调验证敏感字段保留语义。 */ async () => {
    getSmtpMock.mockResolvedValueOnce({
      smtp_server: 'smtp.example.com', smtp_port: 587, smtp_user: 'sender@example.com', smtp_password_configured: true,
    });
    // hook 是脱敏 SMTP 配置场景下的通知 Hook 渲染结果。
    const hook = renderHook(/* hookFactory 创建管理员通知 Hook。 */ () => useNotifications(true));
    // loadingAssertion 等待脱敏 SMTP 配置加载完成。
    await waitFor(/* loadingAssertion 检查异步加载状态。 */ () => expect(hook.result.current.loading).toBe(false));

    // saveAction 提交未修改密码的 SMTP 配置。
    await act(/* saveAction 执行 SMTP 保存动作。 */ async () => hook.result.current.handleSaveSmtp());
    // payload 是发送给服务端的 SMTP 配置载荷。
    const payload = updateSmtpMock.mock.calls.at(-1)?.[0] as SystemSettings;
    expect(payload.smtp_password).toBeUndefined();
    expect(payload.smtp_server).toBe('smtp.example.com');
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

  test('渠道加载、保存、切换和 SMTP 保存失败时展示错误', /* 当前回调验证通知 Hook 的业务错误分支。 */ async () => {
    // hook 是管理员通知错误场景的 Hook 渲染结果。
    const hook = renderHook(
      // errorHookFactory 创建管理员通知错误场景的 Hook。
      () => useNotifications(true),
    );
    await waitFor(
      // loadingAssertion 等待首次通知渠道加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );

    getChannelsMock.mockRejectedValueOnce(new Error('渠道读取失败'));
    await act(
      // loadErrorAction 触发通知渠道读取错误。
      async () => hook.result.current.loadChannels(),
    );

    await act(
      // openEditAction 打开已有渠道编辑表单。
      () => hook.result.current.openEdit(channelFixture),
    );
    await act(
      // formAction 写入编辑渠道表单。
      () => hook.result.current.setForm({ name: '修改渠道', type: 'bark', enabled: true, config: { server_url: 'https://api.day.app', device_key: 'key' }, event_types: [] }),
    );
    updateChannelMock.mockRejectedValueOnce(new Error('渠道保存失败'));
    await act(
      // saveErrorAction 触发渠道保存错误。
      async () => hook.result.current.handleSave(),
    );
    expect(hook.result.current.toast).toEqual({ type: 'error', text: '渠道保存失败' });

    updateChannelMock.mockRejectedValueOnce(new Error('切换失败'));
    await act(
      // toggleErrorAction 触发渠道状态切换错误。
      async () => hook.result.current.handleToggleEnabled(channelFixture),
    );
    expect(hook.result.current.toast).toEqual({ type: 'error', text: '切换失败' });

    updateSmtpMock.mockRejectedValueOnce(new Error('SMTP保存失败'));
    await act(
      // smtpErrorAction 触发系统 SMTP 保存错误。
      async () => hook.result.current.handleSaveSmtp(),
    );
    expect(hook.result.current.toast).toEqual({ type: 'error', text: 'SMTP保存失败' });
    hook.unmount();
  });

  test('SMTP 初始加载失败时记录错误并保持渠道加载完成', /* 当前回调验证管理员 SMTP 初始化错误分支。 */ async () => {
    getSmtpMock.mockRejectedValueOnce(new Error('SMTP读取失败'));
    // consoleError 是 SMTP 加载错误日志的可控替身。
    const consoleError = vi.spyOn(console, 'error').mockImplementation(/* errorLogger 忽略测试日志输出。 */ () => undefined);
    // hook 是 SMTP 初始化失败场景的通知 Hook 渲染结果。
    const hook = renderHook(
      // smtpFailureHookFactory 创建 SMTP 初始化失败场景的 Hook。
      () => useNotifications(true),
    );
    await waitFor(
      // loadingAssertion 等待通知渠道列表加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(consoleError).toHaveBeenCalledWith('加载 SMTP 配置失败', expect.any(Error));
    hook.unmount();
    consoleError.mockRestore();
  });

  test('提示自动消失、弹窗关闭和删除拒绝均清理状态', /* 当前回调验证通知 Hook 的定时器与守卫分支。 */ async () => {
    vi.useFakeTimers();
    try {
      // hook 是通知提示和弹窗状态场景的 Hook 渲染结果。
      const hook = renderHook(
        // toastHookFactory 创建通知提示场景的 Hook。
        () => useNotifications(false),
      );
      await act(
        // toastAction 展示一条短暂提示。
        () => hook.result.current.showToast('success', '短暂提示'),
      );
      expect(hook.result.current.toast).toEqual({ type: 'success', text: '短暂提示' });
      await act(
        // toastTimerAction 推进三秒自动清理提示。
        async () => { await vi.advanceTimersByTimeAsync(3_000); },
      );
      expect(hook.result.current.toast).toBeNull();
      await act(
        // openAction 打开渠道弹窗。
        () => hook.result.current.openCreate(),
      );
      await act(
        // closeAction 关闭渠道弹窗并取消动作。
        () => hook.result.current.closeModal(),
      );
      expect(hook.result.current.showModal).toBe(false);
      // confirmMock 是用户取消删除时的浏览器确认框替身。
      vi.mocked(window.confirm).mockReturnValue(false);
      await act(
        // rejectedDeleteAction 触发用户拒绝删除。
        async () => hook.result.current.handleDelete(channelFixture),
      );
      expect(deleteChannelMock).not.toHaveBeenCalled();
      vi.mocked(window.confirm).mockReturnValue(true);
      deleteChannelMock.mockRejectedValueOnce(new Error('删除失败'));
      await act(
        // failedDeleteAction 触发删除请求失败。
        async () => hook.result.current.handleDelete(channelFixture),
      );
      expect(hook.result.current.toast).toEqual({ type: 'error', text: '删除失败' });
      await act(
        // cleanupToastAction 创建未到期提示后卸载 Hook，验证定时器清理。
        () => hook.result.current.showToast('success', '卸载清理'),
      );
      hook.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  test('渠道刷新时丢弃先发出的过期响应', /* 当前回调验证通知渠道请求代次隔离。 */ async () => {
    // ChannelResponse 是旧渠道请求使用的最小成功响应。
    type ChannelResponse = {
      // success 表示请求成功。
      success: true;
      // data 保存渠道列表。
      data: NotificationChannel[];
    };
    // resolveFirst 是旧渠道请求的完成控制器。
    let resolveFirst: (value: ChannelResponse) => void = () => undefined;
    // firstRequest 是保持未完成的旧渠道请求 Promise。
    const firstRequest = new Promise<ChannelResponse>(/* firstExecutor 保存旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    getChannelsMock.mockReset();
    getChannelsMock.mockReturnValueOnce(firstRequest);
    getChannelsMock.mockResolvedValueOnce({ success: true, data: [channelFixture] });
    // hook 是通知渠道刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(
      // staleHookFactory 创建通知渠道旧响应场景的 Hook。
      () => useNotifications(false),
    );
    await act(
      // refreshAction 发起第二次渠道刷新并使首次请求过期。
      async () => hook.result.current.loadChannels(),
    );
    resolveFirst({ success: true, data: [] });
    await act(
      // staleResolveAction 完成已过期的首次响应。
      async () => { await firstRequest; },
    );
    expect(hook.result.current.channels).toEqual([channelFixture]);
    hook.unmount();
  });
});

// emailEditorFixture 是不携带 SMTP 秘密的邮件摘要，验证先读取再编辑的交互边界。
const emailEditorFixture: NotificationChannel = { id: '1', name: '邮件', type: 'email', config: {}, enabled: true };

// 每个迟到响应场景都验证弹窗是否属于当前操作，避免测试只检查请求是否发出。
test.each(['ready', 'close', 'create', 'switch', 'failure'] as const)('编辑加载隔离：%s', /* editorScenario 检查当前 mode 的加载、取消与新编辑竞争。 */ async mode => {
  vi.resetAllMocks();
  updateChannelMock.mockResolvedValue({ success: true });
  getChannelsMock.mockResolvedValue({ success: true, data: [] });
  // finishEditor 和 failEditor 由测试控制首个编辑响应，模拟慢请求和网络错误。
  let finishEditor!: (value: Awaited<ReturnType<typeof getNotificationChannel>>) => void;
  let failEditor!: (reason: Error) => void;
  getChannelMock.mockImplementationOnce(/* pendingEditorResponse 暂存读取响应的控制函数。 */ () => new Promise(/* pendingEditorExecutor 等待测试完成首个读取。 */ (resolve, reject) => { finishEditor = resolve; failEditor = reject; }));
  // hook 是本场景独立的通知状态，卸载会取消仍在等待的请求。
  const hook = renderHook(/* editorHookFactory 创建无系统 SMTP 查询的编辑状态。 */ () => useNotifications(false));
  await waitFor(/* editorLoadedAssertion 等待初始渠道列表加载完成。 */ () => expect(hook.result.current.loading).toBe(false));
  // pending 保存首个异步编辑，用于等待它完成后确认没有旧响应覆盖。
  let pending!: Promise<void>;
  act(/* beginEditorAction 发起慢读取，尚未开放表单。 */ () => { pending = hook.result.current.openEdit(emailEditorFixture); });
  expect(hook.result.current.showModal).toBe(false);
  if (mode === 'close') act(/* cancelEditorAction 取消尚未完成的打开操作。 */ () => hook.result.current.closeModal());
  if (mode === 'create') act(/* createDuringLoadAction 新建渠道使旧编辑读取失效。 */ () => hook.result.current.openCreate());
  if (mode === 'switch') {
    getChannelMock.mockResolvedValueOnce({ id: 2, name: '其他邮件', type: 'email', enabled: true, to_email: 'other@example.com' });
    await act(/* switchEditorAction 切换到另一渠道并先完成读取。 */ async () => hook.result.current.openEdit({ ...emailEditorFixture, id: '2' }));
  }
  await act(/* finishEditorAction 让旧读取最后完成或失败。 */ async () => {
    if (mode === 'failure') failEditor(new Error('读取失败'));
    else finishEditor({ id: 1, name: '邮件', type: 'email', enabled: true, to_email: 'old@example.com', use_custom_smtp: true });
    await pending;
  });
  if (mode === 'ready') {
    expect(hook.result.current.form.preserveSMTP).toBe(true);
    expect(hook.result.current.showModal).toBe(true);
    act(/* editRecipientAction 读取完成后用户修改收件地址，之后不会再有该读取的回填。 */ () => hook.result.current.setForm(/* recipientUpdater 只改变当前收件地址。 */ current => ({ ...current, config: { ...current.config, to_email: 'new@example.com' } })));
    await act(/* saveRecipientAction 单独更新收件地址并保留服务器 SMTP。 */ async () => hook.result.current.handleSave());
    expect(updateChannelMock).toHaveBeenLastCalledWith('1', expect.objectContaining({ email_recipient: 'new@example.com' }), expect.anything());
    expect(updateChannelMock.mock.calls.at(-1)?.[1].config).toBeUndefined();
  } else if (mode === 'create') {
    expect(hook.result.current.editing).toBeNull();
    expect(hook.result.current.form.type).toBe('bark');
  } else if (mode === 'switch') {
    expect(hook.result.current.editing?.id).toBe('2');
    expect(hook.result.current.form.config.to_email).toBe('other@example.com');
  } else {
    expect(hook.result.current.showModal).toBe(false);
    if (mode === 'failure') expect(hook.result.current.toast?.type).toBe('error');
  }
  hook.unmount();
});
