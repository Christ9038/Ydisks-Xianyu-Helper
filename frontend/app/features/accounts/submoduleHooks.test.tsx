// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import type { Dispatch,SetStateAction } from 'react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { AccountDetail,AIReplySettings } from './api';
import {
cancelPasswordLogin,
checkPasswordLoginStatus,
getAccountAISettings,
getAccountBindings,
getLongLoginSettings,
getNotificationChannels,
passwordLogin,
setLongLoginSettings,
updateAccountAISettings,
updateAccountPauseDuration,
updateAccountSettings,
} from './api';
import { useAccountSubmodules,type AccountModalType } from './submoduleHooks';
import type { AccountEditForm } from './types';

vi.mock('./api', /* accountsSubmoduleApiMockFactory 提供账号子模块 Hook 的确定性 API 替身。 */ () => ({
  cancelPasswordLogin: vi.fn(),
  checkPasswordLoginStatus: vi.fn(),
  getAccountAISettings: vi.fn(),
  getAccountBindings: vi.fn(),
  getLongLoginSettings: vi.fn(),
  getNotificationChannels: vi.fn(),
  passwordLogin: vi.fn(),
  setLongLoginSettings: vi.fn(),
  updateAccountAISettings: vi.fn(),
  updateAccountPauseDuration: vi.fn(),
  updateAccountSettings: vi.fn(),
}));

// cancelPasswordMock 是取消密码登录的可控替身。
const cancelPasswordMock = vi.mocked(cancelPasswordLogin);
// passwordStatusMock 是密码登录状态查询的可控替身。
const passwordStatusMock = vi.mocked(checkPasswordLoginStatus);
// accountAIMock 是账号 AI 设置读取的可控替身。
const accountAIMock = vi.mocked(getAccountAISettings);
// bindingsMock 是账号通知绑定读取的可控替身。
const bindingsMock = vi.mocked(getAccountBindings);
// longLoginMock 是长期登录状态读取的可控替身。
const longLoginMock = vi.mocked(getLongLoginSettings);
// channelsMock 是通知渠道读取的可控替身。
const channelsMock = vi.mocked(getNotificationChannels);
// passwordLoginMock 是密码登录启动的可控替身。
const passwordLoginMock = vi.mocked(passwordLogin);
// setLongLoginMock 是长期登录保存的可控替身。
const setLongLoginMock = vi.mocked(setLongLoginSettings);
// updateAIMock 是账号 AI 设置保存的可控替身。
const updateAIMock = vi.mocked(updateAccountAISettings);
// pauseMock 是账号暂停时长更新的可控替身。
const pauseMock = vi.mocked(updateAccountPauseDuration);
// updateAccountMock 是账号编辑表单保存的可控替身。
const updateAccountMock = vi.mocked(updateAccountSettings);

// accountFixture 是账号编辑和登录流程使用的账号对象。
const accountFixture = { id: 'account-1', enabled: true, value: 'old-cookie', remark: '旧备注', auto_confirm: false, pause_duration: 0, username: 'user@example.com', login_password: 'old-password', show_browser: false } as AccountDetail;
// editFormFixture 是账号编辑弹窗的初始表单。
const editFormFixture: AccountEditForm = { remark: '新备注', cookie: 'new-cookie', auto_confirm: true, pause_duration: 60, username: 'user@example.com', login_password: 'new-password', show_browser: true, showLoginPassword: false, clear_password: false };
// aiFixture 是账号 AI 设置的服务端配置。
const aiFixture: AIReplySettings = { ai_enabled: true, max_discount_percent: 20, max_discount_amount: 50, max_bargain_rounds: 2, custom_prompts: '请礼貌回复' };

describe('useAccountSubmodules', /* 当前回调处理账号编辑、AI、通知绑定和密码登录。 */ () => {
  beforeEach(/* 当前回调重置账号子模块 API 替身。 */ () => {
    vi.clearAllMocks();
    channelsMock.mockResolvedValue({ success: true, data: [{ id: '1', name: '邮件', type: 'email', enabled: true } as never] });
    bindingsMock.mockResolvedValue([1]);
    longLoginMock.mockResolvedValue({ can_open_long_login: true, enabled: false });
    setLongLoginMock.mockResolvedValue({ can_open_long_login: true, enabled: true });
    accountAIMock.mockResolvedValue(aiFixture as never);
    updateAIMock.mockResolvedValue({ success: true });
    updateAccountMock.mockResolvedValue({ success: true } as never);
    pauseMock.mockResolvedValue({ success: true, paused: true, paused_until: 12345 } as never);
    passwordLoginMock.mockResolvedValue({ success: true, session_id: 'session-1', status: 'processing', message: '处理中' });
    passwordStatusMock.mockResolvedValue({ status: 'success', message: '登录成功' });
    cancelPasswordMock.mockResolvedValue({ success: true });
  });

  test('打开编辑和AI弹窗后可以保存账号、绑定、长期登录和暂停设置', /* 当前回调验证账号编辑子模块的成功路径。 */ async () => {
    // setEditingAccount 是编辑账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是编辑弹窗状态替身。
    const setActiveModal = vi.fn();
    // setEditForm 是编辑表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是保存成功后的账号列表刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    // hook 是账号子模块 Hook 的渲染结果。
    const hook = renderHook(
      // editHookFactory 创建账号编辑场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );

    await act(
      // editAction 打开账号编辑弹窗并加载关联数据。
      async () => hook.result.current.openEditModal(accountFixture),
    );
    expect(hook.result.current.longLogin.canOpen).toBe(true);
    expect(hook.result.current.notifChannels).toHaveLength(1);
    expect(hook.result.current.selectedChannelIds).toEqual([1]);
    expect(setEditForm).toHaveBeenCalledWith(expect.objectContaining({ cookie: '', login_password: '' }));
    await act(
      // toggleAction 切换通知渠道绑定。
      () => hook.result.current.toggleNotificationChannel(2),
    );
    expect(hook.result.current.bindingsDirty).toBe(true);
    await act(
      // longLoginAction 切换长期登录设置。
      async () => hook.result.current.handleLongLoginToggle(),
    );
    expect(setLongLoginMock).toHaveBeenCalledWith('account-1', true);
    await act(
      // saveEditAction 保存账号表单和通知绑定。
      async () => hook.result.current.handleSaveEdit(),
    );
    expect(updateAccountMock).toHaveBeenCalledWith('account-1', expect.objectContaining({ remark: '新备注', cookie: 'new-cookie', channel_ids: [1, 2] }));
    expect(loadAccounts).toHaveBeenCalled();

    await act(
      // aiAction 打开 AI 设置弹窗并加载账号配置。
      async () => hook.result.current.openAIModal(accountFixture),
    );
    expect(hook.result.current.aiSettings).toEqual(aiFixture);
    await act(
      // saveAIAction 保存 AI 设置并关闭弹窗。
      async () => hook.result.current.handleSaveAISettings(),
    );
    expect(updateAIMock).toHaveBeenCalledWith('account-1', aiFixture);
    await act(
      // pauseAction 重新暂停账号并刷新列表。
      async () => hook.result.current.handleRestartPause(),
    );
    expect(pauseMock).toHaveBeenCalledWith('account-1', 60);
    // pauseUpdater 是暂停成功后写回编辑账号状态的函数式更新器。
    let pauseUpdater: ((current: AccountDetail | null) => AccountDetail | null) | undefined;
    for (const call /* call 是编辑账号状态替身记录的一次调用参数。 */ of setEditingAccount.mock.calls) {
      if (typeof call[0] === 'function') {
        pauseUpdater = call[0] as (current: AccountDetail | null) => AccountDetail | null;
        break;
      }
    }
    expect(pauseUpdater?.(accountFixture)).toMatchObject({ pause_duration: 60, paused: true, paused_until: 12345 });
    expect(pauseUpdater?.(null)).toBeNull();

    setLongLoginMock.mockRejectedValueOnce(new Error('长登录保存失败'));
    await act(
      // longLoginErrorAction 验证长期登录保存失败提示。
      async () => hook.result.current.handleLongLoginToggle(),
    );
    expect(hook.result.current.longLogin.error).toBe('长登录保存失败');

    updateAIMock.mockRejectedValueOnce(new Error('AI保存失败'));
    await act(
      // aiSaveErrorAction 验证 AI 设置保存失败后的状态清理。
      async () => hook.result.current.handleSaveAISettings(),
    );
    expect(hook.result.current.saving).toBe(false);

    updateAccountMock.mockRejectedValueOnce(new Error('账号保存失败'));
    await act(
      // editSaveErrorAction 验证账号编辑保存失败后的状态清理。
      async () => hook.result.current.handleSaveEdit(),
    );
    expect(hook.result.current.saving).toBe(false);

    pauseMock.mockRejectedValueOnce(new Error('暂停失败'));
    await act(
      // pauseErrorAction 验证重新暂停失败后的状态清理。
      async () => hook.result.current.handleRestartPause(),
    );
    expect(hook.result.current.saving).toBe(false);
    hook.unmount();
  });

  test('密码登录成功后刷新账号，取消动作可以终止会话', /* 当前回调验证密码登录轮询和取消路径。 */ async () => {
    // setEditingAccount 是密码登录测试所需的账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是密码登录测试所需的弹窗状态替身。
    const setActiveModal = vi.fn() as unknown as Dispatch<SetStateAction<AccountModalType>>;
    // setEditForm 是密码登录成功后清理密码的状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是密码登录成功后的列表刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    // hook 是密码登录场景的 Hook 渲染结果。
    const hook = renderHook(
      // passwordHookFactory 创建密码登录场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );
    await act(
      // passwordAction 启动密码登录并等待成功状态。
      async () => hook.result.current.handlePasswordLogin(),
    );
    expect(passwordLoginMock).toHaveBeenCalledWith(expect.objectContaining({ account_id: 'account-1', account: 'user@example.com' }), expect.anything());
    expect(hook.result.current.passwordLoginView.status).toBe('success');
    expect(loadAccounts).toHaveBeenCalled();
    // passwordUpdater 是密码登录成功后清空密码字段的函数式更新器。
    let passwordUpdater: ((current: AccountEditForm) => AccountEditForm) | undefined;
    for (const call /* call 是密码表单状态替身记录的一次调用参数。 */ of setEditForm.mock.calls) {
      if (typeof call[0] === 'function') {
        passwordUpdater = call[0] as (current: AccountEditForm) => AccountEditForm;
        break;
      }
    }
    expect(passwordUpdater?.(editFormFixture)).toMatchObject({ login_password: '', showLoginPassword: false });

    await act(
      // cancelAction 取消当前密码登录会话并回到空闲状态。
      async () => hook.result.current.handleCancelPasswordLogin(),
    );
    expect(cancelPasswordMock).toHaveBeenCalledWith('session-1');
    expect(hook.result.current.passwordLoginView.status).toBe('idle');
    hook.unmount();
  });

  test('密码登录启动失败和状态查询失败时展示错误', /* 当前回调验证密码登录的异常状态转换。 */ async () => {
    // setEditingAccount 是密码登录异常测试的账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是密码登录异常测试的弹窗状态替身。
    const setActiveModal = vi.fn() as unknown as Dispatch<SetStateAction<AccountModalType>>;
    // setEditForm 是密码登录异常测试的表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是密码登录异常测试的列表刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    // hook 是密码登录异常场景的 Hook 渲染结果。
    const hook = renderHook(
      // passwordErrorHookFactory 创建密码登录异常场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );

    passwordLoginMock.mockResolvedValueOnce({ success: false, message: '账号密码错误' });
    await act(
      // startErrorAction 验证密码登录启动失败状态。
      async () => hook.result.current.handlePasswordLogin(),
    );
    expect(hook.result.current.passwordLoginView.status).toBe('failed');
    expect(hook.result.current.passwordLoginView.message).toBe('账号密码错误');

    passwordLoginMock.mockResolvedValueOnce({ success: true, session_id: 'session-2', status: 'processing', message: '处理中' });
    passwordStatusMock.mockRejectedValueOnce(new Error('状态查询失败'));
    await act(
      // statusErrorAction 验证密码登录状态查询失败转换。
      async () => hook.result.current.handlePasswordLogin(),
    );
    expect(hook.result.current.passwordLoginView.status).toBe('failed');
    expect(hook.result.current.passwordLoginView.message).toBe('状态查询失败');
    hook.unmount();
  });

  test('绑定、长期登录和AI请求失败时保留可恢复状态', /* 当前回调验证账号子模块的失败隔离分支。 */ async () => {
    // setEditingAccount 是失败场景的账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是失败场景的弹窗状态替身。
    const setActiveModal = vi.fn();
    // setEditForm 是失败场景的表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是失败场景的列表刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    channelsMock.mockRejectedValueOnce(new Error('渠道失败'));
    bindingsMock.mockRejectedValueOnce(new Error('绑定失败'));
    longLoginMock.mockRejectedValueOnce(new Error('长登录失败'));
    // hook 是账号子模块失败场景的渲染结果。
    const hook = renderHook(
      // errorHookFactory 创建失败隔离场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );
    await act(
      // editErrorAction 打开编辑弹窗并等待失败结果收口。
      async () => hook.result.current.openEditModal(accountFixture),
    );
    expect(hook.result.current.bindingsLoadError).toContain('绑定加载失败');
    expect(hook.result.current.longLogin.error).toContain('无法读取');
    await act(
      // aiErrorAction 打开AI弹窗并隔离读取失败。
      async () => hook.result.current.openAIModal(accountFixture),
    );
    accountAIMock.mockRejectedValueOnce(new Error('AI读取失败'));
    await act(
      // aiRetryAction 再次读取AI设置以覆盖异常路径。
      async () => hook.result.current.openAIModal(accountFixture),
    );
    expect(hook.result.current.saving).toBe(false);
    hook.unmount();
  });

  test('通知渠道单独失败并可关闭 AI 弹窗', /* 当前回调验证通知渠道隔离和弹窗清理边界。 */ async () => {
    // setEditingAccount 是通知渠道边界测试的账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是通知渠道边界测试的弹窗状态替身。
    const setActiveModal = vi.fn();
    // setEditForm 是通知渠道边界测试的表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是通知渠道边界测试的列表刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    channelsMock.mockRejectedValueOnce(new Error('渠道读取失败'));
    // hook 是通知渠道单独失败场景的 Hook 渲染结果。
    const hook = renderHook(
      // bindingsErrorHookFactory 创建通知渠道单独失败场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );
    await act(
      // editAction 打开编辑弹窗并等待通知渠道请求完成。
      async () => hook.result.current.openEditModal(accountFixture),
    );
    expect(hook.result.current.bindingsLoadError).toContain('通知渠道列表加载失败');
    await act(
      // aiAction 打开 AI 设置弹窗。
      async () => hook.result.current.openAIModal(accountFixture),
    );
    await act(
      // closeAIAction 关闭 AI 设置弹窗并清理请求代次。
      () => hook.result.current.closeAIModal(),
    );
    expect(setActiveModal).toHaveBeenCalledWith(null);
    hook.unmount();
  });

  test('密码登录处理中和失败状态都能关闭会话', /* 当前回调验证密码登录中间状态与关闭清理。 */ async () => {
    vi.useFakeTimers();
    // setEditingAccount 是密码登录中间状态的账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是密码登录中间状态的弹窗状态替身。
    const setActiveModal = vi.fn() as unknown as Dispatch<SetStateAction<AccountModalType>>;
    // setEditForm 是密码登录中间状态的表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是密码登录中间状态的列表刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    // hook 是密码登录中间状态场景的 Hook 渲染结果。
    const hook = renderHook(
      // processingHookFactory 创建密码登录中间状态场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );
    passwordStatusMock.mockResolvedValueOnce({ status: 'processing', message: '处理中' });
    await act(
      // processingAction 启动并进入密码登录处理中状态。
      async () => hook.result.current.handlePasswordLogin(),
    );
    expect(hook.result.current.passwordLoginView.status).toBe('processing');
    await act(
      // pollTimerAction 推进密码登录状态轮询定时器。
      async () => { await vi.advanceTimersByTimeAsync(1_500); },
    );
    expect(hook.result.current.passwordLoginView.status).toBe('success');
    passwordLoginMock.mockResolvedValueOnce({ success: true, session_id: 'session-2', status: 'processing', message: '处理中' });
    passwordStatusMock.mockResolvedValueOnce({ status: 'processing', message: '处理中' });
    await act(
      // secondProcessingAction 再次进入处理中状态以验证关闭时取消会话。
      async () => hook.result.current.handlePasswordLogin(),
    );
    cancelPasswordMock.mockRejectedValueOnce(new Error('取消失败'));
    await act(
      // closeAction 关闭编辑弹窗并取消处理中会话。
      async () => hook.result.current.closeEditModal(),
    );
    expect(cancelPasswordMock).toHaveBeenCalledWith('session-2');
    hook.unmount();
    vi.useRealTimers();
  });

  test('通知绑定可移除且 AI 默认值与密码登录终态可归一化', /* 当前回调验证账号子模块的派生默认值和终态分支。 */ async () => {
    // setEditingAccount 是派生默认值测试的编辑账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是派生默认值测试的弹窗状态替身。
    const setActiveModal = vi.fn();
    // setEditForm 是派生默认值测试的表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是派生默认值测试的账号刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    // hook 是通知绑定移除和密码终态场景的 Hook 渲染结果。
    const hook = renderHook(
      // derivedHookFactory 创建派生默认值场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: accountFixture, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );
    await act(
      // editAction 打开编辑弹窗并加载通知绑定。
      async () => hook.result.current.openEditModal(accountFixture),
    );
    await act(
      // removeBindingAction 移除已经绑定的通知渠道。
      () => hook.result.current.toggleNotificationChannel(1),
    );
    expect(hook.result.current.selectedChannelIds).toEqual([]);

    accountAIMock.mockResolvedValueOnce({ ai_enabled: undefined, max_discount_percent: undefined, max_discount_amount: undefined, max_bargain_rounds: undefined, custom_prompts: undefined } as never);
    await act(
      // defaultAIAction 读取缺少字段的 AI 设置并应用默认值。
      async () => hook.result.current.openAIModal(accountFixture),
    );
    expect(hook.result.current.aiSettings).toMatchObject({ ai_enabled: false, max_discount_percent: 10, max_discount_amount: 100, max_bargain_rounds: 3, custom_prompts: '' });

    passwordLoginMock.mockResolvedValueOnce({ success: true, session_id: 'session-failed', status: 'processing', message: '处理中' });
    passwordStatusMock.mockResolvedValueOnce({ status: 'failed', message: '密码登录失败' });
    await act(
      // failedStatusAction 验证密码登录失败终态不再继续轮询。
      async () => hook.result.current.handlePasswordLogin(),
    );
    expect(hook.result.current.passwordLoginView).toMatchObject({ status: 'failed', message: '密码登录失败' });
    hook.unmount();
  });

  test('没有编辑账号时阻止编辑、AI、暂停和密码登录动作', /* 当前回调验证账号子模块空上下文守卫。 */ async () => {
    // setEditingAccount 是空上下文守卫所需的编辑账号状态替身。
    const setEditingAccount = vi.fn();
    // setActiveModal 是空上下文守卫所需的弹窗状态替身。
    const setActiveModal = vi.fn();
    // setEditForm 是空上下文守卫所需的表单状态替身。
    const setEditForm = vi.fn();
    // loadAccounts 是空上下文守卫所需的账号刷新替身。
    const loadAccounts = vi.fn().mockResolvedValue(undefined);
    // hook 是空编辑账号场景的子模块 Hook 渲染结果。
    const hook = renderHook(
      // emptyAccountHookFactory 创建空编辑账号场景的子模块 Hook。
      () => useAccountSubmodules({ editingAccount: null, setEditingAccount, setActiveModal, editForm: editFormFixture, setEditForm, loadAccounts }),
    );
    await act(
      // longLoginGuardAction 阻止空账号长期登录操作。
      async () => hook.result.current.handleLongLoginToggle(),
    );
    await act(
      // aiGuardAction 阻止空账号 AI 保存操作。
      async () => hook.result.current.handleSaveAISettings(),
    );
    await act(
      // editGuardAction 阻止空账号编辑保存操作。
      async () => hook.result.current.handleSaveEdit(),
    );
    await act(
      // pauseGuardAction 阻止空账号暂停操作。
      async () => hook.result.current.handleRestartPause(),
    );
    await act(
      // passwordGuardAction 阻止空账号密码登录操作。
      async () => hook.result.current.handlePasswordLogin(),
    );
    expect(setLongLoginMock).not.toHaveBeenCalled();
    expect(updateAIMock).not.toHaveBeenCalled();
    expect(updateAccountMock).not.toHaveBeenCalled();
    expect(pauseMock).not.toHaveBeenCalled();
    expect(passwordLoginMock).not.toHaveBeenCalled();
    hook.unmount();
  });
});
