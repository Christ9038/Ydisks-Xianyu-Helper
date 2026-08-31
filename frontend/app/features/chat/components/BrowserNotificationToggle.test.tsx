// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { BrowserNotificationPreferenceState } from '../../../../shared/browser/browserNotifications';
import { useBrowserNotificationPreference } from '../../../../shared/browser/browserNotifications';
import BrowserNotificationToggle from './BrowserNotificationToggle';

vi.mock('../../../../shared/browser/browserNotifications', /* browserNotificationMockFactory 提供通知开关的确定性偏好状态。 */ () => ({
  useBrowserNotificationPreference: vi.fn(),
}));

// useBrowserNotificationPreferenceMock 是通知开关组件使用的偏好 Hook 替身。
const useBrowserNotificationPreferenceMock = vi.mocked(useBrowserNotificationPreference);

// createPreferenceState 创建通知开关测试所需的完整偏好状态。
const createPreferenceState = (overrides: Partial<BrowserNotificationPreferenceState> = {}): BrowserNotificationPreferenceState => ({
  enabled: false,
  supported: true,
  permission: 'default',
  updating: false,
  error: '',
  setEnabled: vi.fn(async () => undefined),
  ...overrides,
});

describe('BrowserNotificationToggle', /* 当前测试组验证聊天页通知开关的可见状态和用户操作。 */ () => {
  beforeEach(/* 当前回调重置通知偏好替身，隔离不同开关状态。 */ () => {
    useBrowserNotificationPreferenceMock.mockReset();
  });

  afterEach(/* 当前回调清理组件 DOM，避免前一个状态污染后续用例。 */ () => {
    cleanup();
  });

  test('已开启时展示开启状态并关闭本地偏好', /* 当前回调验证已开启通知的开关语义和点击回调参数。 */ () => {
    // setEnabled 保存用户点击后应调用的本地偏好更新函数。
    const setEnabled = vi.fn(/* setEnabledCallback 模拟用户关闭本地通知偏好的异步回调。 */ async () => undefined);
    useBrowserNotificationPreferenceMock.mockReturnValue(createPreferenceState({ enabled: true, permission: 'granted', setEnabled }));
    render(<BrowserNotificationToggle />);

    // toggle 保存聊天页通知开关元素。
    const toggle = screen.getByRole('switch', { name: '开启新消息系统通知' });
    expect(toggle.getAttribute('aria-checked')).toBe('true');
    // knob 保存开关圆点的固定定位基准，避免启用状态覆盖右侧文字。
    expect(toggle.querySelector('span')?.classList.contains('left-0.5')).toBe(true);
    expect(toggle.querySelector('span')?.classList.contains('translate-x-4')).toBe(true);
    expect(screen.queryByText('已开启')).not.toBeNull();
    fireEvent.click(toggle);
    expect(setEnabled).toHaveBeenCalledWith(false);
  });

  test('权限拒绝和不支持时禁用开关并展示原因', /* 当前回调验证浏览器能力不足时不会继续发起用户操作。 */ () => {
    useBrowserNotificationPreferenceMock.mockReturnValue(createPreferenceState({ supported: false, permission: 'denied', error: '通知权限已被拒绝' }));
    render(<BrowserNotificationToggle />);

    // toggle 保存浏览器不支持或拒绝权限时的禁用开关。
    const toggle = screen.getByRole('switch', { name: '开启新消息系统通知' });
    expect((toggle as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByText('权限已拒绝')).not.toBeNull();
    expect(screen.getByRole('status').textContent).toContain('通知权限已被拒绝');
  });

  test('请求权限期间禁用开关并展示进行中状态', /* 当前回调验证异步权限请求不会被重复点击。 */ () => {
    useBrowserNotificationPreferenceMock.mockReturnValue(createPreferenceState({ updating: true }));
    render(<BrowserNotificationToggle />);

    expect((screen.getByRole('switch', { name: '开启新消息系统通知' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByText('请求权限中')).not.toBeNull();
  });
});
