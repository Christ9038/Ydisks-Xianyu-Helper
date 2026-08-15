// @vitest-environment jsdom
import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail } from '../types';

// accountListMocks 保存 AccountList 页面测试使用的 API、Hook 和轮询替身。
const accountListMocks = vi.hoisted(/* accountListMockFactory 创建页面测试共享替身。 */ () => ({
  accounts: [
    { id: 'account-1', nickname: 'Alpha', remark: '主账号', enabled: true, runtime_message: '在线' },
    { id: 'account-2', nickname: 'Beta', remark: '备用账号', enabled: false, runtime_message: '已停用' },
  ] as AccountDetail[],
  setAccounts: vi.fn(),
  loadAccounts: vi.fn(),
  updateAccountStatus: vi.fn(),
  deleteAccount: vi.fn(),
  generateQRLogin: vi.fn(),
  checkQRLoginStatus: vi.fn(),
  completeQRVerification: vi.fn(),
  refreshAccountProfile: vi.fn(),
  pollerStart: vi.fn(),
  pollerStop: vi.fn(),
  requestNext: vi.fn(),
  requestIsCurrent: vi.fn(),
  requestCancel: vi.fn(),
  pollCallbacks: null as Record<string, (...args: any[]) => void> | null,
}));

vi.mock('../app/features/accounts/hooks', /* accountsHookMockFactory 提供账号列表数据 Hook 替身。 */ () => ({
  useAccountsData: /* useAccountsDataMock 返回固定账号列表和刷新操作。 */ () => ({
    accounts: accountListMocks.accounts,
    setAccounts: accountListMocks.setAccounts,
    loading: false,
    loadAccounts: accountListMocks.loadAccounts,
  }),
}));

vi.mock('../app/features/accounts/submoduleHooks', /* submoduleHookMockFactory 提供账号弹窗状态 Hook 替身。 */ () => ({
  useAccountSubmodules: /* useAccountSubmodulesMock 返回不阻断页面渲染的子模块状态。 */ () => ({
    longLogin: false,
    notifChannels: [],
    selectedChannelIds: [],
    bindingsLoaded: true,
    bindingsLoading: false,
    bindingsLoadError: '',
    aiSettings: {},
    saving: false,
    passwordLoginView: { status: 'idle', message: '' },
    setAiSettings: vi.fn(),
    setBindingsDirty: vi.fn(),
    openEditModal: vi.fn(),
    closeEditModal: vi.fn(),
    openAIModal: vi.fn(),
    closeAIModal: vi.fn(),
    loadNotificationBindings: vi.fn(),
    toggleNotificationChannel: vi.fn(),
    handleLongLoginToggle: vi.fn(),
    handleSaveAISettings: vi.fn(),
    handleSaveEdit: vi.fn(),
    handleRestartPause: vi.fn(),
    handlePasswordLogin: vi.fn(),
    handleCancelPasswordLogin: vi.fn(),
  }),
}));

vi.mock('../app/features/accounts/api', /* accountApiMockFactory 提供账号页面 API 替身。 */ () => ({
  updateAccountStatus: accountListMocks.updateAccountStatus,
  deleteAccount: accountListMocks.deleteAccount,
  generateQRLogin: accountListMocks.generateQRLogin,
  checkQRLoginStatus: accountListMocks.checkQRLoginStatus,
  completeQRVerification: accountListMocks.completeQRVerification,
  refreshAccountProfile: accountListMocks.refreshAccountProfile,
}));

vi.mock('../components/qrPolling', /* qrPollingMockFactory 提供二维码轮询器替身。 */ () => ({
  createLatestRequestGate: /* requestGateFactory 创建二维码请求代次门禁。 */ () => ({
    next: accountListMocks.requestNext,
    isCurrent: accountListMocks.requestIsCurrent,
    cancel: accountListMocks.requestCancel,
  }),
  createQRLoginPoller: /* qrPollerFactory 创建二维码轮询控制器。 */ () => ({
    start: accountListMocks.pollerStart,
    stop: accountListMocks.pollerStop,
  }),
}));

vi.mock('../app/features/accounts/components/AccountCard', /* accountCardMockFactory 提供账号卡片交互替身。 */ () => {
  // AccountCardMock 渲染页面测试需要的账号操作入口。
  const AccountCardMock: React.FC<any> = (props /* props 表示账号卡片及其事件回调。 */) => (
    <div data-testid={`account-card-${props.account.id}`}>
      <span>{props.account.nickname}</span>
      <button onClick={/* deleteAction 触发账号删除确认。 */ () => props.onDelete(props.account)}>删除</button>
      <button onClick={/* toggleAction 切换账号启用状态。 */ () => props.onToggle(props.account.id, props.account.enabled)}>切换</button>
      <button onClick={/* refreshAction 刷新账号资料。 */ () => props.onRefreshProfile(props.account)}>刷新资料</button>
      <button onClick={/* reauthorizeAction 启动账号二维码重新授权。 */ () => props.onReauthorize(props.account)}>重新授权</button>
    </div>
  );
  return { AccountCard: AccountCardMock };
});

vi.mock('../app/features/accounts/components/AccountDeleteDialog', /* deleteDialogMockFactory 提供删除确认弹窗替身。 */ () => {
  // AccountDeleteDialogMock 展示删除错误并暴露确认/关闭操作。
  const AccountDeleteDialogMock: React.FC<any> = (props /* props 表示删除弹窗及其操作回调。 */) => (
    <div data-testid="delete-dialog">
      <span>{props.error}</span>
      <button onClick={/* confirmAction 确认删除当前账号。 */ () => props.onConfirm()}>确认删除</button>
      <button onClick={/* closeAction 关闭删除确认弹窗。 */ () => props.onClose()}>取消</button>
    </div>
  );
  return { AccountDeleteDialog: AccountDeleteDialogMock };
});

vi.mock('../app/features/accounts/components/AccountQRCodeModal', /* qrModalMockFactory 提供二维码弹窗替身。 */ () => {
  // AccountQRCodeModalMock 展示二维码状态并暴露关闭操作。
  const AccountQRCodeModalMock: React.FC<any> = (props /* props 表示二维码弹窗及其操作回调。 */) => (
    <div data-testid="qr-modal">
      <span>{props.status}</span>
      <button onClick={/* closeAction 关闭二维码登录弹窗。 */ () => props.onClose()}>关闭二维码</button>
    </div>
  );
  return { AccountQRCodeModal: AccountQRCodeModalMock };
});

vi.mock('../app/features/accounts/components/AccountEditModal', /* editModalMockFactory 提供账号编辑弹窗替身。 */ () => ({
  AccountEditModal: /* AccountEditModalMock 表示账号编辑弹窗替身。 */ () => null,
}));

vi.mock('../app/features/accounts/components/AccountAISettingsModal', /* aiModalMockFactory 提供 AI 设置弹窗替身。 */ () => ({
  AccountAISettingsModal: /* AccountAISettingsModalMock 表示 AI 设置弹窗替身。 */ () => null,
}));

vi.mock('../components/AccountAutomationModal', /* automationModalMockFactory 提供账号任务弹窗替身。 */ () => ({
  default: /* AccountAutomationModalMock 表示账号自动化弹窗替身。 */ () => null,
}));

import AccountList from './AccountList';

describe('AccountList 页面组合行为', /* 当前回调验证账号列表页面的搜索、删除、资料和二维码流程。 */ () => {
  beforeEach(/* 当前回调重置账号页面 API、请求门禁和浏览器提示替身。 */ () => {
    vi.clearAllMocks();
    accountListMocks.requestNext.mockReturnValue(1);
    accountListMocks.requestIsCurrent.mockReturnValue(true);
    accountListMocks.generateQRLogin.mockResolvedValue({ success: true, qr_code_url: 'qr-url', session_id: 'session-1' });
    accountListMocks.completeQRVerification.mockResolvedValue({ success: true, account_id: 'account-1' });
    accountListMocks.updateAccountStatus.mockResolvedValue({ success: true });
    accountListMocks.deleteAccount.mockResolvedValue({ success: true });
    accountListMocks.refreshAccountProfile.mockResolvedValue({ success: true });
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽页面资料刷新提示。 */ () => undefined);
    accountListMocks.pollerStart.mockImplementation(/* pollerStartImplementation 保存二维码轮询回调。 */ (_sessionId: string, _check: unknown, callbacks: Record<string, (...args: any[]) => void>) => {
      accountListMocks.pollCallbacks = callbacks;
    });
  });

  afterEach(/* 当前回调清理页面 DOM 和浏览器提示替身。 */ () => {
    cleanup();
    vi.restoreAllMocks();
  });

  test('搜索只展示匹配账号并保留总数提示', /* 当前回调验证账号搜索过滤行为。 */ () => {
    render(<AccountList />);

    expect(screen.getByTestId('account-card-account-1')).toBeTruthy();
    expect(screen.getByTestId('account-card-account-2')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('搜索昵称 / 备注 / 账号ID'), { target: { value: 'Alpha' } });

    expect(screen.getByTestId('account-card-account-1')).toBeTruthy();
    expect(screen.queryByTestId('account-card-account-2')).toBeNull();
    expect(screen.getByText('当前显示 1 / 2 个账号')).toBeTruthy();
  });

  test('删除确认成功后调用 API 并移除目标账号', /* 当前回调验证删除确认和本地列表更新边界。 */ async () => {
    render(<AccountList />);
    fireEvent.click(screen.getAllByText('删除')[0]);
    expect(screen.getByTestId('delete-dialog')).toBeTruthy();

    fireEvent.click(screen.getByText('确认删除'));
    await waitFor(/* deleteAssertion 等待删除请求完成。 */ () => expect(accountListMocks.deleteAccount).toHaveBeenCalledWith('account-1'));
    expect(accountListMocks.setAccounts).toHaveBeenCalledTimes(1);
    // nextAccounts 是页面删除逻辑提交给状态 Setter 的过滤函数结果。
    const nextAccounts = accountListMocks.setAccounts.mock.calls[0][0](accountListMocks.accounts);
    expect(nextAccounts).toEqual([accountListMocks.accounts[1]]);
  });

  test('账号卡片操作分别触发状态切换和资料刷新', /* 当前回调验证账号卡片向页面协调器转发操作。 */ async () => {
    render(<AccountList />);
    fireEvent.click(screen.getAllByText('切换')[0]);
    await waitFor(/* toggleAssertion 等待账号状态更新请求完成。 */ () => expect(accountListMocks.updateAccountStatus).toHaveBeenCalledWith('account-1', false));
    expect(accountListMocks.loadAccounts).toHaveBeenCalled();

    fireEvent.click(screen.getAllByText('刷新资料')[0]);
    await waitFor(/* refreshAssertion 等待资料刷新请求完成。 */ () => expect(accountListMocks.refreshAccountProfile).toHaveBeenCalledWith('account-1'));
    expect(accountListMocks.loadAccounts).toHaveBeenCalledTimes(2);
  });

  test('二维码授权启动轮询并在成功后保存会话结果', /* 当前回调验证二维码生成、轮询成功和延迟收束流程。 */ async () => {
    render(<AccountList />);
    fireEvent.click(screen.getByText('扫码添加新账号'));
    expect(await screen.findByTestId('qr-modal')).toBeTruthy();
    await waitFor(/* pollerAssertion 等待二维码轮询器启动。 */ () => expect(accountListMocks.pollerStart).toHaveBeenCalledWith('session-1', accountListMocks.checkQRLoginStatus, expect.anything()));
    expect(screen.getByText('waiting')).toBeTruthy();

    await accountListMocks.pollCallbacks?.onSuccess();
    expect(accountListMocks.completeQRVerification).toHaveBeenCalledWith('session-1', undefined);
    await waitFor(/* successAssertion 等待二维码成功状态渲染。 */ () => expect(screen.getByText('success')).toBeTruthy());
    await waitFor(/* reloadAssertion 等待成功后的账号列表刷新。 */ () => expect(accountListMocks.loadAccounts).toHaveBeenCalled(), { timeout: 2_000 });
  });

  test('关闭二维码弹窗会停止轮询并取消请求', /* 当前回调验证页面卸载前的二维码资源收束。 */ async () => {
    // view 是关闭二维码资源后用于卸载页面的渲染结果。
    const view = render(<AccountList />);
    fireEvent.click(screen.getByText('扫码添加新账号'));
    expect(await screen.findByTestId('qr-modal')).toBeTruthy();
    fireEvent.click(screen.getByText('关闭二维码'));

    expect(accountListMocks.pollerStop).toHaveBeenCalled();
    expect(accountListMocks.requestCancel).toHaveBeenCalled();
    view.unmount();
  });
});
