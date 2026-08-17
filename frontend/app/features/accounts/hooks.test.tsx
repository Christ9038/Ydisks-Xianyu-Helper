// @vitest-environment jsdom
import { renderHook, waitFor } from '@testing-library/react';
import { act } from 'react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail } from '../../../shared/api-contract';
import { getAccountDetails, getAccountRuntimeStatuses, getAllAISettings } from './api';
import { useAccountsData } from './hooks';

vi.mock('./api', /* accountsApiMockFactory 提供账号列表 Hook 的确定性 API 替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getAccountRuntimeStatuses: vi.fn(),
  getAllAISettings: vi.fn(),
}));

// getDetailsMock 是账号详情请求的可控替身。
const getDetailsMock = vi.mocked(getAccountDetails);
// getRuntimeMock 是账号运行状态轮询请求的可控替身。
const getRuntimeMock = vi.mocked(getAccountRuntimeStatuses);
// getAISettingsMock 是账号 AI 配置请求的可控替身。
const getAISettingsMock = vi.mocked(getAllAISettings);

// accountFixture 是账号列表 Hook 测试使用的基础账号对象。
const accountFixture: AccountDetail = { id: 'account-1', enabled: true, auto_confirm: false, remark: '测试账号' };

describe('useAccountsData', /* 当前回调处理账号详情、AI 配置和运行状态轮询。 */ () => {
  beforeEach(/* 当前回调重置账号 API 替身和日志输出。 */ () => {
    vi.clearAllMocks();
    getDetailsMock.mockResolvedValue([accountFixture]);
    getAISettingsMock.mockResolvedValue({ 'account-1': { ai_enabled: true, max_discount_percent: 20, max_discount_amount: 30, max_bargain_rounds: 4, custom_prompts: '提示词' } } as never);
    getRuntimeMock.mockResolvedValue({ 'account-1': { state: 'online', connected: true, failures: 0, message: '在线', updated_at: '2026-08-15T00:00:00Z' } });
    vi.spyOn(console, 'error').mockImplementation(
      // errorImplementation 屏蔽轮询失败测试中的日志输出。
      () => undefined,
    );
  });

  test('加载账号并合并 AI 与运行状态', /* 当前回调验证账号列表成功加载路径。 */ async () => {
    // hook 是账号数据 Hook 的渲染结果。
    const hook = renderHook(
      // accountsHookFactory 创建账号数据 Hook。
      () => useAccountsData(),
    );
    await waitFor(
      // loadingAssertion 等待账号详情和 AI 配置请求完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await waitFor(
      // runtimeAssertion 等待首次运行状态轮询完成。
      () => expect(getRuntimeMock).toHaveBeenCalled(),
    );
    expect(hook.result.current.accounts[0]).toMatchObject({ ai_enabled: true, max_discount_percent: 20, runtime_state: 'online', runtime_connected: true });
    hook.unmount();
  });

  test('AI 配置失败不阻断账号列表，详情失败则结束加载', /* 当前回调验证两类并行请求的错误隔离。 */ async () => {
    getAISettingsMock.mockRejectedValueOnce(new Error('AI 服务失败'));
    // aiHook 是 AI 配置失败场景的账号数据 Hook 渲染结果。
    const aiHook = renderHook(
      // aiHookFactory 创建 AI 配置失败场景的 Hook。
      () => useAccountsData(),
    );
    await waitFor(
      // loadingAssertion 等待 AI 失败但账号详情成功的结果。
      () => expect(aiHook.result.current.loading).toBe(false),
    );
    expect(aiHook.result.current.accounts).toHaveLength(1);
    expect(aiHook.result.current.accounts[0].ai_enabled).toBe(false);
    aiHook.unmount();

    getDetailsMock.mockRejectedValueOnce(new Error('账号服务失败'));
    // detailsHook 是账号详情失败场景的账号数据 Hook 渲染结果。
    const detailsHook = renderHook(
      // detailsHookFactory 创建账号详情失败场景的 Hook。
      () => useAccountsData(),
    );
    await waitFor(
      // loadingAssertion 等待账号详情失败状态收束。
      () => expect(detailsHook.result.current.loading).toBe(false),
    );
    expect(detailsHook.result.current.accounts).toEqual([]);
    expect(console.error).toHaveBeenCalled();
    detailsHook.unmount();
  });

  test('运行状态轮询失败时保留账号列表并记录错误', /* 当前回调验证运行状态轮询异常分支。 */ async () => {
    getRuntimeMock.mockRejectedValueOnce(new Error('运行状态服务失败'));
    // hook 是运行状态轮询失败场景的账号数据 Hook 渲染结果。
    const hook = renderHook(
      // runtimeErrorHookFactory 创建运行状态轮询失败场景的 Hook。
      () => useAccountsData(),
    );
    await waitFor(
      // loadingAssertion 等待账号详情加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(hook.result.current.accounts).toHaveLength(1);
    expect(console.error).toHaveBeenCalledWith('加载账号运行状态失败:', expect.any(Error));
    hook.unmount();
  });

  test('运行状态轮询完成后按计划安排下一轮查询', /* 当前回调验证账号状态轮询定时器回调。 */ async () => {
    vi.useFakeTimers();
    try {
      // hook 是账号轮询定时器场景的 Hook 渲染结果。
      const hook = renderHook(
        // timerHookFactory 创建账号轮询定时器场景的 Hook。
        () => useAccountsData(),
      );
      await act(
        // initialFlushAction 刷新初始账号和运行状态 Promise。
        async () => { await Promise.resolve(); await Promise.resolve(); },
      );
      await act(
        // timerAction 推进两秒轮询定时器。
        async () => { await vi.advanceTimersByTimeAsync(2_000); },
      );
      expect(getRuntimeMock).toHaveBeenCalledTimes(2);
      hook.unmount();
    } finally {
      vi.useRealTimers();
    }
  });
});
