// @vitest-environment jsdom
import type { ReactNode } from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { initializeAdmin, login, logout, verifySession } from '../features/session/api';
import { SessionProvider, useSession } from './SessionProvider';

vi.mock('../features/session/api', /* sessionApiMockFactory 提供会话 Provider 的确定性 API 替身。 */ () => ({
  initializeAdmin: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  verifySession: vi.fn(),
}));

// initializeMock 是首次管理员初始化请求的可控替身。
const initializeMock = vi.mocked(initializeAdmin);
// loginMock 是登录请求的可控替身。
const loginMock = vi.mocked(login);
// logoutMock 是注销请求的可控替身。
const logoutMock = vi.mocked(logout);
// verifySessionMock 是会话校验请求的可控替身。
const verifySessionMock = vi.mocked(verifySession);

// sessionWrapper 将被测 Hook 装配到真实的 SessionProvider 中。
const sessionWrapper = ({ children }: { /** children 表示测试组件树的 React 子节点。 */ children: ReactNode }) => (
  <SessionProvider>{children}</SessionProvider>
);

describe('SessionProvider', /* 当前回调验证认证 Provider 的状态和生命周期。 */ () => {
  beforeEach(/* 当前回调重置会话 API 替身和默认响应。 */ () => {
    vi.clearAllMocks();
    verifySessionMock.mockResolvedValue({ authenticated: false, initialized: true });
    loginMock.mockResolvedValue({ success: false, message: '登录失败' });
    initializeMock.mockResolvedValue({ success: false, message: '初始化失败' });
    logoutMock.mockResolvedValue({ success: true });
  });

  test('校验管理员会话并响应全局注销事件', /* 当前回调验证会话校验和全局注销事件。 */ async () => {
    verifySessionMock.mockResolvedValueOnce({ authenticated: true, initialized: true, is_admin: true });
    // hook 是已认证会话 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建已认证会话 Hook。 */ () => useSession(), { wrapper: sessionWrapper });

    await waitFor(/* authReadyAssertion 等待会话校验完成。 */ () => expect(hook.result.current.checkingAuth).toBe(false));
    expect(hook.result.current.isLoggedIn).toBe(true);
    expect(hook.result.current.isAdmin).toBe(true);
    expect(hook.result.current.needsInit).toBe(false);

    act(/* logoutEventAction 发布全局注销事件。 */ () => window.dispatchEvent(new Event('auth:logout')));
    expect(hook.result.current.isLoggedIn).toBe(false);
    expect(hook.result.current.isAdmin).toBe(false);
    hook.unmount();
  });

  test('未初始化系统会进入首次初始化状态', /* 当前回调验证首次管理员初始化分支。 */ async () => {
    verifySessionMock.mockResolvedValueOnce({ authenticated: false, initialized: false });
    // hook 是未初始化会话 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建未初始化会话 Hook。 */ () => useSession(), { wrapper: sessionWrapper });

    await waitFor(/* authReadyAssertion 等待未初始化校验完成。 */ () => expect(hook.result.current.checkingAuth).toBe(false));
    expect(hook.result.current.needsInit).toBe(true);
    expect(hook.result.current.isLoggedIn).toBe(false);
    hook.unmount();
  });

  test('登录和首次初始化成功后更新认证状态', /* 当前回调验证登录和初始化成功状态。 */ async () => {
    // hook 是认证操作 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建认证操作 Hook。 */ () => useSession(), { wrapper: sessionWrapper });
    await waitFor(/* authReadyAssertion 等待认证操作测试准备完成。 */ () => expect(hook.result.current.checkingAuth).toBe(false));

    loginMock.mockResolvedValueOnce({ success: true, username: 'admin', is_admin: true });
    await act(/* signInAction 执行登录操作。 */ async () => {
      // response 表示登录操作返回的认证结果。
      const response = await hook.result.current.signIn({ username: 'admin', password: 'password' });
      expect(response.success).toBe(true);
    });
    expect(hook.result.current.isLoggedIn).toBe(true);
    expect(hook.result.current.isAdmin).toBe(true);

    initializeMock.mockResolvedValueOnce({ success: true, username: 'admin', is_admin: true });
    await act(/* initializeAction 执行首次初始化操作。 */ async () => {
      // response 表示首次初始化返回的认证结果。
      const response = await hook.result.current.initialize('new-password');
      expect(response.success).toBe(true);
    });
    expect(hook.result.current.needsInit).toBe(false);
    expect(initializeMock).toHaveBeenCalledWith('new-password');
    hook.unmount();
  });

  test('注销请求失败时仍清理本地认证状态并向调用方抛错', /* 当前回调验证注销失败后的本地收束。 */ async () => {
    loginMock.mockResolvedValueOnce({ success: true, username: 'admin', is_admin: false });
    // hook 是注销失败场景 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建注销失败场景 Hook。 */ () => useSession(), { wrapper: sessionWrapper });
    await waitFor(/* authReadyAssertion 等待注销测试准备完成。 */ () => expect(hook.result.current.checkingAuth).toBe(false));
    await act(/* signInAction 建立待注销的已认证状态。 */ async () => {
      await hook.result.current.signIn({ username: 'admin', password: 'password' });
    });
    logoutMock.mockRejectedValueOnce(new Error('网络不可用'));

    await act(/* signOutAction 执行会失败但应清理本地状态的注销操作。 */ async () => {
      await expect(hook.result.current.signOut()).rejects.toThrow('网络不可用');
    });
    expect(hook.result.current.isLoggedIn).toBe(false);
    expect(hook.result.current.isAdmin).toBe(false);
    hook.unmount();
  });
});
