// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getHealth } from '../app/features/system/api';
import { useNotifications } from '../app/features/notifications/hooks';
import Sidebar from './Sidebar';
import Notifications from './Notifications';

vi.mock('../app/features/system/api', /* systemApiMockFactory 提供侧边栏健康检查的确定性 API 替身。 */ () => ({ getHealth: vi.fn() }));
vi.mock('../app/features/notifications/hooks', /* notificationsHookMockFactory 提供通知页面状态替身。 */ () => ({ useNotifications: vi.fn() }));
vi.mock('../app/features/notifications/components/NotificationChannelList', /* channelListMockFactory 隔离通知列表子组件。 */ () => ({ NotificationChannelList: /* channelListRenderer 渲染通知列表占位节点。 */ () => <div data-testid="channel-list" /> }));
vi.mock('../app/features/notifications/components/NotificationChannelModal', /* channelModalMockFactory 隔离通知编辑子组件。 */ () => ({ NotificationChannelModal: /* channelModalRenderer 渲染通知编辑占位节点。 */ () => <div data-testid="channel-modal" /> }));
vi.mock('../app/features/notifications/components/NotificationSmtpSettings', /* smtpSettingsMockFactory 隔离SMTP子组件。 */ () => ({ NotificationSmtpSettings: /* smtpSettingsRenderer 渲染SMTP占位节点。 */ () => <div data-testid="smtp-settings" /> }));

// healthMock 是侧边栏健康检查的可控替身。
const healthMock = vi.mocked(getHealth);
// useNotificationsMock 是通知页面 Hook 的可控替身。
const useNotificationsMock = vi.mocked(useNotifications);

describe('基础页面组件', /* 当前回调覆盖侧边栏和通知页面的结构分支。 */ () => {
  beforeEach(/* 当前回调重置页面组件 API 替身。 */ () => {
    vi.clearAllMocks();
    healthMock.mockResolvedValue({ version: '1.2.3', commit: 'abc123' });
  });

  test('侧边栏展示管理员菜单并触发导航、折叠和注销', /* 当前回调验证侧边栏导航交互和版本信息。 */ async () => {
    // onNavigate 是侧边栏导航回调替身。
    const onNavigate = vi.fn();
    // onToggleCollapsed 是侧边栏折叠回调替身。
    const onToggleCollapsed = vi.fn();
    // onLogout 是侧边栏注销回调替身。
    const onLogout = vi.fn();
    render(<Sidebar activeTab="dashboard" isAdmin collapsed={false} onToggleCollapsed={onToggleCollapsed} onNavigate={onNavigate} onLogout={onLogout} />);
    await waitFor(
      // versionAssertion 等待健康检查版本展示完成。
      () => expect(screen.getByText('v1.2.3')).toBeTruthy(),
    );
    fireEvent.click(screen.getByRole('button', { name: '账号管理' }));
    fireEvent.click(screen.getByRole('button', { name: '收起侧边栏' }));
    fireEvent.click(screen.getByRole('button', { name: '退出登录' }));
    expect(onNavigate).toHaveBeenCalledWith('accounts');
    expect(onToggleCollapsed).toHaveBeenCalled();
    expect(onLogout).toHaveBeenCalled();
    expect(screen.getByRole('button', { name: '系统与AI' })).toBeTruthy();
  });

  test('通知页面按加载、管理员和提示消息状态渲染', /* 当前回调验证通知页面的权限和状态分支。 */ async () => {
    // loadChannels 是通知刷新动作替身。
    const loadChannels = vi.fn();
    // openCreate 是通知新建动作替身。
    const openCreate = vi.fn();
    // notificationState 是通知页面 Hook 的完整可控状态。
    const notificationState = {
      loadChannels, openCreate, loading: false, channels: [], testingId: null, openEdit: vi.fn(), handleDelete: vi.fn(), handleToggleEnabled: vi.fn(), handleTest: vi.fn(), smtp: {}, setSmtp: vi.fn(), smtpSaving: false, showSmtpPassword: false, setShowSmtpPassword: vi.fn(), handleSaveSmtp: vi.fn(), showModal: false, editing: null, form: {}, setForm: vi.fn(), showChannelSmtpPassword: false, setShowChannelSmtpPassword: vi.fn(), saving: false, closeModal: vi.fn(), handleSave: vi.fn(), toast: { type: 'success', text: '已保存' },
    };
    useNotificationsMock.mockReturnValue(notificationState as never);
    render(<Notifications isAdmin />);
    expect(screen.getByTestId('channel-list')).toBeTruthy();
    expect(screen.getByTestId('smtp-settings')).toBeTruthy();
    expect(screen.getByText('已保存')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    fireEvent.click(screen.getByRole('button', { name: /新建渠道/ }));
    expect(loadChannels).toHaveBeenCalled();
    expect(openCreate).toHaveBeenCalled();
  });
});
