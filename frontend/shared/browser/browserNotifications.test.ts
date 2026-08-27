// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { BROWSER_NOTIFICATION_PREFERENCE_KEY,getBrowserNotificationPermission,readBrowserNotificationEnabled,showBrowserNotification,updateBrowserNotificationPreference,useBrowserNotificationPreference } from './browserNotifications';

/** FakeNotification 模拟浏览器通知构造器及用户可见的通知实例。 */
class FakeNotification {
  /** permission 保存测试中浏览器返回的系统通知权限。 */
  static permission: NotificationPermission = 'default';
  /** requestPermission 模拟用户在权限弹窗中的选择。 */
  static requestPermission = vi.fn(async (): Promise<NotificationPermission> => 'granted');
  /** instances 保存测试期间创建的系统通知，便于断言展示内容。 */
  static instances: FakeNotification[] = [];
  /** title 保存系统通知标题。 */
  title: string;
  /** options 保存系统通知正文和会话标签。 */
  options?: NotificationOptions;
  /** onclick 保存用户点击通知时的回调。 */
  onclick: (() => void) | null = null;

  /** 创建一条可观察的通知实例。 */
  constructor(title: string, options?: NotificationOptions) {
    this.title = title;
    this.options = options;
    FakeNotification.instances.push(this);
  }
}

// storageValues 保存测试用浏览器存储的键值，避免 Node 运行时缺少持久化 localStorage。
let storageValues: Record<string, string> = {};

// testLocalStorage 提供通知偏好测试需要的最小 Storage 行为。
const testLocalStorage = {
  getItem: /* 当前回调读取测试存储中的通知偏好。 */ (key: string): string | null => storageValues[key] ?? null,
  setItem: /* 当前回调写入测试存储中的通知偏好。 */ (key: string, value: string): void => { storageValues[key] = value; },
  removeItem: /* 当前回调删除测试存储中的指定偏好。 */ (key: string): void => { delete storageValues[key]; },
  clear: /* 当前回调清空测试存储，隔离不同测试用例。 */ (): void => { storageValues = {}; },
  key: /* 当前回调满足 Storage 接口但不参与本测试的索引读取。 */ (): string | null => null,
  length: 0,
} as Storage;

describe('browser notification preference', /* 当前测试组验证浏览器通知偏好、权限申请和展示边界。 */ () => {
  beforeEach(/* 当前回调重置浏览器存储和通知构造器状态。 */ async () => {
    Object.defineProperty(window, 'localStorage', { configurable: true, value: testLocalStorage });
    testLocalStorage.clear();
    FakeNotification.permission = 'default';
    FakeNotification.requestPermission.mockReset();
    FakeNotification.requestPermission.mockResolvedValue('granted');
    FakeNotification.instances = [];
    vi.stubGlobal('Notification', FakeNotification);
    await updateBrowserNotificationPreference(false);
  });

  afterEach(/* 当前回调清理浏览器全局替身，避免影响其他测试。 */ () => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    testLocalStorage.clear();
  });

  test('开启开关时请求权限并持久化本地偏好', /* 当前回调验证设置页用户手势可以开启系统通知。 */ async () => {
    // hook 保存通知设置 Hook 的当前状态。
    // renderPreferenceHook 创建通知设置 Hook 的测试实例。
    const renderPreferenceHook = /* 当前回调创建通知设置 Hook 的测试实例。 */ () => useBrowserNotificationPreference();
    const hook = renderHook(renderPreferenceHook);
    // enableAction 模拟用户点击开关并等待浏览器权限请求完成。
    const enableAction = /* 当前回调模拟设置页用户主动开启通知。 */ async (): Promise<void> => hook.result.current.setEnabled(true);
    await act(enableAction);
    expect(FakeNotification.requestPermission).toHaveBeenCalledTimes(1);
    expect(hook.result.current.enabled).toBe(true);
    expect(hook.result.current.permission).toBe('granted');
    expect(window.localStorage.getItem(BROWSER_NOTIFICATION_PREFERENCE_KEY)).toBe('true');
    hook.unmount();
  });

  test('权限被拒绝时保持关闭并展示错误', /* 当前回调验证浏览器拒绝权限不会误显示已开启。 */ async () => {
    FakeNotification.requestPermission.mockResolvedValueOnce('denied');
    // hook 保存权限拒绝场景的通知设置 Hook 状态。
    // renderPreferenceHook 创建权限拒绝场景的通知设置 Hook。
    const renderPreferenceHook = /* 当前回调创建权限拒绝场景的通知设置 Hook。 */ () => useBrowserNotificationPreference();
    const hook = renderHook(renderPreferenceHook);
    // enableAction 模拟用户点击开关并等待拒绝结果写入状态。
    const enableAction = /* 当前回调模拟设置页用户主动开启通知。 */ async (): Promise<void> => hook.result.current.setEnabled(true);
    await act(enableAction);
    expect(hook.result.current.enabled).toBe(false);
    expect(hook.result.current.error).toContain('拒绝通知');
    expect(readBrowserNotificationEnabled()).toBe(false);
    hook.unmount();
  });

  test('系统权限已授予且开关开启时展示通知内容', /* 当前回调验证实时聊天消息可以进入系统通知构造器。 */ async () => {
    FakeNotification.permission = 'granted';
    window.localStorage.setItem(BROWSER_NOTIFICATION_PREFERENCE_KEY, 'true');
    // created 表示通知工具是否成功创建了当前消息对应的系统通知。
    const created = showBrowserNotification({ title: '买家发来新消息', body: '请问还在吗', tag: 'chat-account-1-chat-1' });
    expect(created).toBe(true);
    expect(FakeNotification.instances[0]).toMatchObject({ title: '买家发来新消息', options: { body: '请问还在吗', tag: 'chat-account-1-chat-1' } });
    expect(getBrowserNotificationPermission()).toBe('granted');
  });

  test('本地偏好开启但系统权限拒绝时界面保持关闭', /* 当前回调验证外部撤销权限不会显示虚假的开启状态。 */ () => {
    FakeNotification.permission = 'denied';
    window.localStorage.setItem(BROWSER_NOTIFICATION_PREFERENCE_KEY, 'true');
    // hook 保存通知偏好 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建通知偏好 Hook 测试实例。 */ () => useBrowserNotificationPreference());
    expect(hook.result.current.enabled).toBe(false);
    expect(hook.result.current.permission).toBe('denied');
    hook.unmount();
  });

  test('页面重新获得焦点时同步外部权限变化', /* 当前回调验证用户在浏览器设置中撤销权限后页面状态及时关闭。 */ () => {
    FakeNotification.permission = 'granted';
    window.localStorage.setItem(BROWSER_NOTIFICATION_PREFERENCE_KEY, 'true');
    // hook 保存通知偏好 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建通知偏好 Hook 测试实例。 */ () => useBrowserNotificationPreference());
    expect(hook.result.current.enabled).toBe(true);
    FakeNotification.permission = 'denied';
    act(/* focusAction 模拟页面重新获得焦点。 */ () => window.dispatchEvent(new Event('focus')));
    expect(hook.result.current.enabled).toBe(false);
    expect(hook.result.current.permission).toBe('denied');
    hook.unmount();
  });

  test('本地存储写入失败时仍可在本页面展示通知', /* 当前回调验证隐私模式阻止 localStorage 时不会出现“已开启但无法通知”。 */ async () => {
    FakeNotification.permission = 'granted';
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(/* 当前回调模拟隐私模式禁止写入本地存储。 */ () => { throw new Error('storage blocked'); });
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(/* 当前回调模拟隐私模式禁止读取本地存储。 */ () => { throw new Error('storage blocked'); });
    await updateBrowserNotificationPreference(true);
    expect(readBrowserNotificationEnabled()).toBe(true);
    expect(showBrowserNotification({ title: '存储受限消息', body: '仍应展示' })).toBe(true);
  });
});
