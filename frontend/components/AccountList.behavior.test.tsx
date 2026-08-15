// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import AccountList from './AccountList';
import { useAccountsData } from '../app/features/accounts/hooks';
import { useAccountSubmodules } from '../app/features/accounts/submoduleHooks';

vi.mock('../app/features/accounts/hooks', /* accountsHookMockFactory 提供账号列表数据状态。 */ () => ({ useAccountsData: vi.fn() }));
vi.mock('../app/features/accounts/submoduleHooks', /* submoduleHookMockFactory 提供账号弹窗子模块状态。 */ () => ({ useAccountSubmodules: vi.fn() }));
vi.mock('./RiskVerificationPanel', /* riskPanelMockFactory 隔离风控验证子组件。 */ () => ({ RiskVerificationPanel: /* riskPanelRenderer 渲染风控占位节点。 */ () => <div data-testid="risk-panel" /> }));
vi.mock('./SquareQRCode', /* qrCodeMockFactory 隔离二维码子组件。 */ () => ({ SquareQRCode: /* qrCodeRenderer 渲染二维码占位节点。 */ () => <div data-testid="qr-code" /> }));
vi.mock('./AccountAutomationModal', /* automationModalMockFactory 隔离自动化弹窗子组件。 */ () => ({ default: /* automationModalRenderer 渲染自动化弹窗占位节点。 */ () => <div data-testid="automation-modal" /> }));
vi.mock('../app/features/accounts/components/AccountEditModal', /* editModalMockFactory 隔离账号编辑弹窗子组件。 */ () => ({ AccountEditModal: /* editModalRenderer 渲染编辑弹窗占位节点。 */ () => <div data-testid="edit-modal" /> }));

// useAccountsDataMock 是账号列表数据 Hook 的可控替身。
const useAccountsDataMock = vi.mocked(useAccountsData);
// useAccountSubmodulesMock 是账号编辑子模块 Hook 的可控替身。
const useAccountSubmodulesMock = vi.mocked(useAccountSubmodules);
// emptySubmoduleState 是账号列表边界渲染所需的最小子模块状态。
const emptySubmoduleState = { longLogin: { loading: false, saving: false, canOpen: false, enabled: false, error: '' }, notifChannels: [], selectedChannelIds: [], bindingsLoaded: false, bindingsLoading: false, bindingsDirty: false, bindingsLoadError: '', aiSettings: { ai_enabled: false, max_discount_percent: 10, max_discount_amount: 100, max_bargain_rounds: 3, custom_prompts: '' }, saving: false, passwordLoginView: { sessionId: '', status: 'idle', message: '', qrCodeUrl: '' }, setAiSettings: vi.fn(), setBindingsDirty: vi.fn(), setEditForm: vi.fn(), openEditModal: vi.fn(), closeEditModal: vi.fn(), openAIModal: vi.fn(), closeAIModal: vi.fn(), loadNotificationBindings: vi.fn(), toggleNotificationChannel: vi.fn(), handleLongLoginToggle: vi.fn(), handleSaveAISettings: vi.fn(), handleSaveEdit: vi.fn(), handleRestartPause: vi.fn(), handlePasswordLogin: vi.fn(), handleCancelPasswordLogin: vi.fn() };

describe('AccountList 页面加载边界', /* 当前回调覆盖账号列表加载和空数据状态。 */ () => {
  test('加载中展示加载指示器', /* 当前回调验证账号列表初始加载分支。 */ () => {
    useAccountsDataMock.mockReturnValue({ accounts: [], setAccounts: vi.fn(), loading: true, loadAccounts: vi.fn() } as never);
    useAccountSubmodulesMock.mockReturnValue(emptySubmoduleState as never);
    render(<AccountList />);
    expect(document.querySelector('.animate-spin')).toBeTruthy();
  });

  test('没有账号时展示启用账号引导', /* 当前回调验证账号列表空状态引导。 */ () => {
    useAccountsDataMock.mockReturnValue({ accounts: [], setAccounts: vi.fn(), loading: false, loadAccounts: vi.fn() } as never);
    useAccountSubmodulesMock.mockReturnValue(emptySubmoduleState as never);
    render(<AccountList />);
    expect(screen.getByText('暂无账号')).toBeTruthy();
  });
});
