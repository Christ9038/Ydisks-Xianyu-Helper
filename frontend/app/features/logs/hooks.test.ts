// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { fetchRuntimeLogs } from './api';
import { useRuntimeLogs } from './hooks';
import type { RuntimeLogEntry, RuntimeLogPage } from './types';

vi.mock('./api', () => ({
  fetchRuntimeLogs: vi.fn(),
}));

// logsMock 是运行日志请求的可控测试替身。
const logsMock = vi.mocked(fetchRuntimeLogs);
// POLL_INTERVAL_MS 是测试使用的轮询间隔，等于 Hook 允许的最小间隔。
const POLL_INTERVAL_MS = 1000;

/** buildEntry 构造一条用于断言的运行日志。 */
const buildEntry = (/* seq 是记录序号，同时作为增量游标断言值。 */ seq: number): RuntimeLogEntry => ({
  seq,
  time: '2026-10-16T12:00:00Z',
  level: 'info',
  source: 'http',
  message: 'HTTP 请求完成',
  method: '',
  path: '',
  status: 0,
  durationMs: 0,
  requestId: '',
  api: '',
  category: '',
  ret: '',
  error: '',
  accountId: '',
  itemId: '',
  orderId: '',
  chatId: '',
  caller: '',
  func: '',
  route: '',
  runId: '',
  details: [],
});

/** buildPage 构造一次增量查询返回的日志页。 */
const buildPage = (/* entries 是本次返回的记录。 */ entries: RuntimeLogEntry[], /* nextCursor 是下一次增量读取游标。 */ nextCursor: number): RuntimeLogPage => ({
  entries,
  nextCursor,
  oldestSeq: entries.length > 0 ? entries[0].seq : 0,
  dropped: 0,
  capacity: 500,
});

/** entrySeqs 提取日志流序号，便于按顺序断言增量结果。 */
const entrySeqs = (/* entries 是当前已加载的日志流。 */ entries: RuntimeLogEntry[]): number[] => entries.map(/* entry 是一条已加载日志。 */ entry => entry.seq);

/** flushAsync 在假定时器下刷新已经解决的异步状态更新。 */
const flushAsync = async (): Promise<void> => {
  await act(/* flushAction 刷新已解决的异步状态更新。 */ async () => {});
};

/** advancePoll 推进一个轮询周期并刷新异步状态。 */
const advancePoll = async (): Promise<void> => {
  await act(/* advanceAction 推进一个轮询周期。 */ async () => {
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
  });
  await flushAsync();
};

describe('useRuntimeLogs', /* 当前测试组验证运行日志轮询、暂停、过滤、过期响应和错误恢复。 */ () => {
  beforeEach(/* setupAction 为每个用例启用假定时器并重置请求替身。 */ () => {
    vi.useFakeTimers();
    vi.clearAllMocks();
  });

  afterEach(/* teardownAction 在用例结束后恢复真实定时器。 */ () => {
    vi.useRealTimers();
  });

  test('首次挂载立即加载并按间隔追加增量', /* 当前回调验证初始加载与游标推进。 */ async () => {
    logsMock
      .mockResolvedValueOnce(buildPage([buildEntry(1)], 1))
      .mockResolvedValueOnce(buildPage([buildEntry(2)], 2));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    expect(entrySeqs(hook.result.current.entries)).toEqual([1]);
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.error).toBe('');
    await advancePoll();
    expect(entrySeqs(hook.result.current.entries)).toEqual([1, 2]);
    expect(logsMock).toHaveBeenCalledTimes(2);
    expect(logsMock.mock.calls[1][1]).toBe(1);
    hook.unmount();
  });

  test('暂停停止轮询，恢复后从当前游标继续', /* 当前回调验证暂停不丢记录且不重复拉取。 */ async () => {
    logsMock
      .mockResolvedValueOnce(buildPage([buildEntry(1)], 1))
      .mockResolvedValueOnce(buildPage([buildEntry(2)], 2));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    act(/* pauseAction 暂停自动轮询。 */ () => hook.result.current.setPaused(true));
    await advancePoll();
    await advancePoll();
    expect(logsMock).toHaveBeenCalledTimes(1);
    act(/* resumeAction 恢复自动轮询。 */ () => hook.result.current.setPaused(false));
    await flushAsync();
    expect(logsMock).toHaveBeenCalledTimes(2);
    expect(logsMock.mock.calls[1][1]).toBe(1);
    expect(entrySeqs(hook.result.current.entries)).toEqual([1, 2]);
    hook.unmount();
  });

  test('过滤条件变化重置游标并清空已加载日志', /* 当前回调验证条件变化不会沿用旧游标。 */ async () => {
    logsMock
      .mockResolvedValueOnce(buildPage([buildEntry(1)], 1))
      .mockResolvedValueOnce(buildPage([buildEntry(9)], 9));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    act(/* queryChangeAction 切换最低等级过滤条件。 */ () => hook.result.current.setQuery({ level: 'error' }));
    await flushAsync();
    expect(hook.result.current.query.level).toBe('error');
    expect(logsMock.mock.calls[1][1]).toBe(0);
    expect(entrySeqs(hook.result.current.entries)).toEqual([9]);
    hook.unmount();
  });

  test('过期响应不覆盖最新日志流', /* 当前回调验证旧代次响应被丢弃。 */ async () => {
    // firstResolve 保存第一轮挂起请求的兑现函数。
    let firstResolve: ((page: RuntimeLogPage) => void) | undefined;
    logsMock.mockImplementationOnce(
      /* pendingFactory 挂起第一轮请求，直到测试显式放行。 */ () => new Promise(/* resolve 在测试放行时兑现第一轮挂起结果。 */ resolve => { firstResolve = resolve; }),
    );
    logsMock.mockResolvedValueOnce(buildPage([buildEntry(9)], 9));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    act(/* queryChangeAction 切换来源过滤以开启新一轮日志流。 */ () => hook.result.current.setQuery({ source: 'mtop' }));
    await flushAsync();
    expect(entrySeqs(hook.result.current.entries)).toEqual([9]);
    await act(/* lateResponseAction 放行第一轮挂起响应并等待状态刷新。 */ async () => {
      firstResolve?.(buildPage([buildEntry(5)], 5));
    });
    expect(entrySeqs(hook.result.current.entries)).toEqual([9]);
    hook.unmount();
  });

  test('请求失败写入错误，重新加载后恢复', /* 当前回调验证失败提示与手动恢复路径。 */ async () => {
    logsMock
      .mockRejectedValueOnce(new Error('接口失败'))
      .mockResolvedValueOnce(buildPage([buildEntry(1)], 1));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    expect(hook.result.current.error).toContain('接口失败');
    expect(hook.result.current.entries).toEqual([]);
    act(/* reloadAction 触发从零游标重新加载。 */ () => hook.result.current.reload());
    await flushAsync();
    expect(hook.result.current.error).toBe('');
    expect(entrySeqs(hook.result.current.entries)).toEqual([1]);
    hook.unmount();
  });

  test('主动取消不写入错误状态', /* 当前回调验证取消语义不会被当成失败提示。 */ async () => {
    logsMock.mockRejectedValueOnce(new Error('请求已取消'));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    expect(hook.result.current.error).toBe('');
    hook.unmount();
  });

  test('清屏只清空已展示日志并保留游标', /* 当前回调验证清屏不会重复拉取旧记录。 */ async () => {
    logsMock
      .mockResolvedValueOnce(buildPage([buildEntry(1), buildEntry(2)], 2))
      .mockResolvedValueOnce(buildPage([buildEntry(3)], 3));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    act(/* clearAction 清空已展示日志。 */ () => hook.result.current.clear());
    expect(hook.result.current.entries).toEqual([]);
    await advancePoll();
    expect(logsMock.mock.calls[1][1]).toBe(2);
    expect(entrySeqs(hook.result.current.entries)).toEqual([3]);
    hook.unmount();
  });

  test('未启用时不发起请求', /* 当前回调验证页面不可见时不会轮询。 */ async () => {
    // hook 保存禁用状态下运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建禁用状态的运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ enabled: false, pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    await advancePoll();
    expect(logsMock).not.toHaveBeenCalled();
    expect(hook.result.current.entries).toEqual([]);
    hook.unmount();
  });

  test('服务端缓冲区边界写入状态用于保留提示', /* 当前回调验证容量与丢弃计数被透传给页面。 */ async () => {
    logsMock.mockResolvedValueOnce({ entries: [], nextCursor: 0, oldestSeq: 0, dropped: 7, capacity: 100 });
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    expect(hook.result.current.dropped).toBe(7);
    expect(hook.result.current.capacity).toBe(100);
    hook.unmount();
  });

  test('暂停期间刷新会恢复轮询并重新加载', /* 当前回调验证暂停状态下刷新不会清空列表且不发出请求。 */ async () => {
    logsMock
      .mockResolvedValueOnce(buildPage([buildEntry(1)], 1))
      .mockResolvedValueOnce(buildPage([buildEntry(2)], 2));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    act(/* pauseAction 暂停自动轮询。 */ () => hook.result.current.setPaused(true));
    await advancePoll();
    expect(logsMock).toHaveBeenCalledTimes(1);
    act(/* reloadAction 在暂停状态下触发刷新。 */ () => hook.result.current.reload());
    await flushAsync();
    expect(hook.result.current.paused).toBe(false);
    expect(logsMock).toHaveBeenCalledTimes(2);
    expect(logsMock.mock.calls[1][1]).toBe(0);
    expect(entrySeqs(hook.result.current.entries)).toEqual([2]);
    hook.unmount();
  });

  test('取消在途请求后复位加载状态', /* 当前回调验证暂停后页面不再显示加载中。 */ async () => {
    // pendingResolve 保存挂起请求的兑现函数。
    let pendingResolve: ((page: RuntimeLogPage) => void) | undefined;
    logsMock.mockImplementationOnce(
      /* pendingFactory 挂起首轮请求，直到测试显式放行。 */ () => new Promise(/* resolve 在测试放行时兑现挂起结果。 */ resolve => { pendingResolve = resolve; }),
    );
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    expect(hook.result.current.loading).toBe(true);
    act(/* pauseAction 在请求未返回时暂停自动轮询。 */ () => hook.result.current.setPaused(true));
    expect(hook.result.current.loading).toBe(false);
    await act(/* lateResponseAction 放行已取消请求的迟到响应。 */ async () => {
      pendingResolve?.(buildPage([buildEntry(1)], 1));
    });
    expect(hook.result.current.entries).toEqual([]);
    expect(hook.result.current.loading).toBe(false);
    hook.unmount();
  });

  test('上一轮请求未返回时跳过本轮轮询，响应乱序不会让游标倒退', /* 当前回调验证同一日志流内增量请求不会并发。 */ async () => {
    // firstResolve 保存首轮挂起请求的兑现函数。
    let firstResolve: ((page: RuntimeLogPage) => void) | undefined;
    logsMock
      .mockImplementationOnce(/* pendingFactory 挂起首轮请求，直到测试显式放行。 */ () => new Promise(/* resolve 在测试放行时兑现首轮结果。 */ resolve => { firstResolve = resolve; }))
      .mockResolvedValueOnce(buildPage([buildEntry(4)], 4));
    // hook 保存运行日志 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建运行日志 Hook 测试实例。 */ () => useRuntimeLogs({ pollIntervalMs: POLL_INTERVAL_MS }));
    await flushAsync();
    await advancePoll();
    await advancePoll();
    expect(logsMock).toHaveBeenCalledTimes(1);
    await act(/* firstResponseAction 放行首轮请求。 */ async () => { firstResolve?.(buildPage([buildEntry(3)], 3)); });
    await advancePoll();
    expect(logsMock).toHaveBeenCalledTimes(2);
    expect(logsMock.mock.calls[1][1]).toBe(3);
    expect(entrySeqs(hook.result.current.entries)).toEqual([3, 4]);
    hook.unmount();
  });
});
