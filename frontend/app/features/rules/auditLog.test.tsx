// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { getAuditOrderDeliveredCards,getAutomationAuditEvents } from './api';
import { auditAutoRefreshMs,useAutomationAuditLog,useDeliveredCardsReveal,useRunTimeline } from './auditLog';
import type { AutomationAuditPage } from './auditModels';

vi.mock('./api', /* auditApiMockFactory 提供规则日志 Hook 的确定性 API 替身。 */ () => ({
  getAutomationAuditEvents: vi.fn(),
  getAuditOrderDeliveredCards: vi.fn(),
}));

// listMock 是规则日志查询的可控替身。
const listMock = vi.mocked(getAutomationAuditEvents);
// cardsMock 是已发卡密读取的可控替身。
const cardsMock = vi.mocked(getAuditOrderDeliveredCards);

// pageOf 构造一页只含一条事件的测试结果。
const pageOf = (summary: string, totalPages = 1): AutomationAuditPage => ({
  events: [{ id: 1, cookieId: 'c1', orderId: 'o1', itemId: 'i1', ruleId: 2, runId: 3, triggerType: 'order_paid', source: 'ws', eventType: 'run_finished', status: 'success', actionType: '', summary, reason: '', createdAtMs: 1000 }],
  total: 1,
  totalPages,
});

// deferred 创建可由测试手动完成的 Promise，用于构造乱序响应。
const deferred = <T,>() => {
  // resolve 与 reject 在测试中手动触发。
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  // promise 是挂起的请求。
  const promise = new Promise<T>(/* settle 捕获 Promise 的完成函数。 */ (res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};

describe('useAutomationAuditLog', /* 当前回调验证规则日志 Hook 的加载、筛选、取消、乱序响应和自动刷新。 */ () => {
  beforeEach(/* 当前回调重置替身。 */ () => {
    vi.clearAllMocks();
  });
  afterEach(/* 当前回调恢复真实计时器。 */ () => {
    vi.useRealTimers();
  });

  // 用例：首次加载带账号，筛选与翻页都会重新查询并重置页码。
  test('首次加载带账号，筛选与翻页都会重新查询并重置页码', async () => {
    listMock.mockResolvedValue(pageOf('A'));
    // result 是 Hook 的当前渲染结果引用。
    const { result } = renderHook(/* hookFactory 创建规则日志 Hook。 */ () => useAutomationAuditLog('acc-1'));
    expect(result.current.loading).toBe(true);
    await waitFor(/* 等待首次加载结束。 */ () => expect(result.current.loading).toBe(false));
    expect(result.current.page?.events[0].summary).toBe('A');
    expect(listMock.mock.calls[0][0]).toEqual({ cookieId: 'acc-1', runId: 0, pageSize: 20, status: '', triggerType: '', search: '', page: 1 });

    act(/* 翻到第 3 页后再改状态，应回到第 1 页。 */ () => { result.current.setPage(3); });
    await waitFor(/* 等待第 3 页请求。 */ () => expect(listMock.mock.calls.at(-1)?.[0].page).toBe(3));
    act(/* 切换状态筛选。 */ () => { result.current.setStatus('failed'); });
    await waitFor(/* 等待筛选请求。 */ () => expect(listMock.mock.calls.at(-1)?.[0]).toMatchObject({ status: 'failed', page: 1 }));
    act(/* 切换触发类型。 */ () => { result.current.setTriggerType('order_paid'); });
    await waitFor(/* 等待触发类型请求。 */ () => expect(listMock.mock.calls.at(-1)?.[0]).toMatchObject({ triggerType: 'order_paid', status: 'failed' }));
    act(/* 应用搜索词。 */ () => { result.current.applySearch('o1'); });
    await waitFor(/* 等待搜索请求。 */ () => expect(listMock.mock.calls.at(-1)?.[0]).toMatchObject({ search: 'o1', page: 1 }));
    act(/* 页码下限为 1。 */ () => { result.current.setPage(-5); });
    await waitFor(/* 等待页码回落。 */ () => expect(listMock.mock.calls.at(-1)?.[0].page).toBe(1));
  });

  // 用例：加载失败展示错误，重试成功后清除错误；非 Error 失败使用兜底文案。
  test('加载失败展示错误，重试成功后清除错误', async () => {
    listMock.mockRejectedValueOnce(new Error('服务暂不可用')).mockResolvedValueOnce(pageOf('B'));
    const { result } = renderHook(/* hookFactory 创建规则日志 Hook。 */ () => useAutomationAuditLog(''));
    await waitFor(/* 等待失败状态。 */ () => expect(result.current.error).toBe('服务暂不可用'));
    expect(result.current.loading).toBe(false);
    act(/* 点击重试。 */ () => { result.current.reload(); });
    await waitFor(/* 等待成功。 */ () => expect(result.current.page?.events[0].summary).toBe('B'));
    expect(result.current.error).toBeNull();
    listMock.mockRejectedValueOnce('boom');
    act(/* 再次重载并遇到非 Error 失败。 */ () => { result.current.reload(); });
    await waitFor(/* 等待兜底文案。 */ () => expect(result.current.error).toBe('读取规则日志失败'));
  });

  // 用例：条件变化后旧响应晚到不覆盖当前结果，卸载会取消请求。
  test('条件变化后旧响应晚到不覆盖当前结果，卸载会取消请求', async () => {
    // first 与 second 分别是旧条件和新条件的挂起请求。
    const first = deferred<AutomationAuditPage>();
    // second 是新条件的挂起请求。
    const second = deferred<AutomationAuditPage>();
    listMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    // result、unmount 分别是 Hook 渲染结果与卸载函数。
    const { result, unmount } = renderHook(/* hookFactory 创建规则日志 Hook。 */ () => useAutomationAuditLog(''));
    act(/* 在首次请求未返回时切换状态，触发第二次请求。 */ () => { result.current.setStatus('skipped'); });
    // firstSignal 是被取消的旧请求信号。
    const firstSignal = listMock.mock.calls[0][1]?.signal;
    await waitFor(/* 等待第二次请求发出。 */ () => expect(listMock).toHaveBeenCalledTimes(2));
    expect(firstSignal?.aborted).toBe(true);
    await act(/* 新响应先到。 */ async () => { second.resolve(pageOf('NEW')); });
    await waitFor(/* 等待新结果展示。 */ () => expect(result.current.page?.events[0].summary).toBe('NEW'));
    await act(/* 旧响应晚到，必须被忽略。 */ async () => { first.resolve(pageOf('OLD')); });
    expect(result.current.page?.events[0].summary).toBe('NEW');
    expect(result.current.loading).toBe(false);
    // pending 是卸载时仍在进行的请求。
    const pending = deferred<AutomationAuditPage>();
    listMock.mockReturnValueOnce(pending.promise);
    act(/* 触发新请求后立即卸载。 */ () => { result.current.reload(); });
    // lastSignal 是卸载前最后一次请求的取消信号。
    const lastSignal = listMock.mock.calls.at(-1)?.[1]?.signal;
    unmount();
    expect(lastSignal?.aborted).toBe(true);
  });

  // 用例：旧请求失败不会污染新请求状态。
  test('旧请求失败不会污染新请求状态', /* 当前回调断言旧请求取消失败不污染新状态。 */ async () => {
    // first 是旧条件的挂起请求。
    const first = deferred<AutomationAuditPage>();
    listMock.mockReturnValueOnce(first.promise).mockResolvedValueOnce(pageOf('OK'));
    // result 是 Hook 渲染结果。
    const { result } = renderHook(/* hookFactory 创建规则日志 Hook。 */ () => useAutomationAuditLog(''));
    act(/* 切换筛选触发第二次请求。 */ () => { result.current.setStatus('queued'); });
    await waitFor(/* 等待新结果展示。 */ () => expect(result.current.page?.events[0].summary).toBe('OK'));
    await act(/* 旧请求以 AbortError 失败。 */ async () => { first.reject(new DOMException('aborted', 'AbortError')); });
    expect(result.current.error).toBeNull();
  });

  // 用例：开启自动刷新后按间隔重新查询，关闭后停止。
  test('开启自动刷新后按间隔重新查询，关闭后停止', /* 当前回调断言自动刷新的开启与关闭。 */ async () => {
    listMock.mockResolvedValue(pageOf('T'));
    const { result } = renderHook(/* hookFactory 创建规则日志 Hook。 */ () => useAutomationAuditLog(''));
    await waitFor(/* 等待首次加载。 */ () => expect(result.current.loading).toBe(false));
    vi.useFakeTimers();
    act(/* 开启自动刷新。 */ () => { result.current.setAutoRefresh(true); });
    // before 是推进计时器前的请求次数。
    const before = listMock.mock.calls.length;
    await act(/* 推进一个刷新周期。 */ async () => { await vi.advanceTimersByTimeAsync(auditAutoRefreshMs); });
    expect(listMock.mock.calls.length).toBe(before + 1);
    act(/* 关闭自动刷新。 */ () => { result.current.setAutoRefresh(false); });
    await act(/* 再推进两个周期。 */ async () => { await vi.advanceTimersByTimeAsync(auditAutoRefreshMs * 2); });
    expect(listMock.mock.calls.length).toBe(before + 1);
  });
});

describe('useRunTimeline', /* 当前回调验证运行时间线的读取、失败和切换取消。 */ () => {
  beforeEach(/* 当前回调重置替身。 */ () => {
    vi.clearAllMocks();
  });

  // 用例：按运行读取时间线，失败展示错误，切换运行取消旧请求。
  test('按运行读取时间线，失败展示错误，切换运行取消旧请求', /* 当前回调断言时间线读取、失败与切换取消。 */ async () => {
    listMock.mockResolvedValueOnce(pageOf('TL'));
    // result、rerender 分别是 Hook 渲染结果与重新渲染函数。
    const { result, rerender } = renderHook(/* hookFactory 创建时间线 Hook。 */ ({ runId }: { /** runId 是要读取的运行。 */ runId: number }) => useRunTimeline(runId), { initialProps: { runId: 3 } });
    expect(result.current.loading).toBe(true);
    await waitFor(/* 等待时间线加载。 */ () => expect(result.current.page?.events[0].summary).toBe('TL'));
    expect(listMock.mock.calls[0][0]).toMatchObject({ runId: 3, page: 1, pageSize: 100 });

    // pending 是切换运行时仍在进行的请求。
    const pending = deferred<AutomationAuditPage>();
    listMock.mockReturnValueOnce(pending.promise);
    rerender({ runId: 4 });
    rerender({ runId: 5 });
    // staleSignal 是运行 4 的请求信号，切换到运行 5 后必须被取消。
    const staleSignal = listMock.mock.calls[1][1]?.signal;
    expect(staleSignal?.aborted).toBe(true);

    listMock.mockRejectedValueOnce(new Error('读取失败'));
    rerender({ runId: 6 });
    await waitFor(/* 等待失败状态。 */ () => expect(result.current.error).toBe('读取失败'));
    expect(result.current.loading).toBe(false);
    listMock.mockRejectedValueOnce('boom');
    rerender({ runId: 7 });
    await waitFor(/* 等待兜底文案。 */ () => expect(result.current.error).toBe('读取运行时间线失败'));
  });
});

describe('useDeliveredCardsReveal', /* 当前回调验证已发卡密默认遮罩、按需读取、隐藏清空和切换订单清空。 */ () => {
  beforeEach(/* 当前回调重置替身。 */ () => {
    vi.clearAllMocks();
  });

  // 用例：默认不读取，reveal 后展示，hide 立即清空。
  test('默认不读取，reveal 后展示，hide 立即清空', async () => {
    cardsMock.mockResolvedValue([{ id: 1, cardName: '组A', content: 'SECRET', deliveredAtMs: 1 }]);
    // result 是 Hook 渲染结果。
    const { result } = renderHook(/* hookFactory 创建按需查看 Hook。 */ () => useDeliveredCardsReveal('o1'));
    expect(result.current.cards).toBeNull();
    expect(cardsMock).not.toHaveBeenCalled();
    act(/* 用户点击查看。 */ () => { result.current.reveal(); });
    await waitFor(/* 等待卡密展示。 */ () => expect(result.current.cards?.[0].content).toBe('SECRET'));
    expect(cardsMock.mock.calls[0][0]).toBe('o1');
    act(/* 用户点击隐藏。 */ () => { result.current.hide(); });
    expect(result.current.cards).toBeNull();
  });

  // 用例：读取失败展示错误；切换订单清空已展示的明文并取消读取。
  test('读取失败展示错误，切换订单清空明文并取消读取', /* 当前回调断言读取失败与切换订单清空明文。 */ async () => {
    cardsMock.mockRejectedValueOnce(new Error('无权查看'));
    // result、rerender 分别是 Hook 渲染结果与重新渲染函数。
    const { result, rerender } = renderHook(/* hookFactory 创建按需查看 Hook。 */ ({ orderId }: { /** orderId 是要查看的订单。 */ orderId: string }) => useDeliveredCardsReveal(orderId), { initialProps: { orderId: 'o1' } });
    act(/* 用户点击查看。 */ () => { result.current.reveal(); });
    await waitFor(/* 等待失败状态。 */ () => expect(result.current.error).toBe('无权查看'));
    cardsMock.mockRejectedValueOnce('boom');
    act(/* 再次查看遇到非 Error 失败。 */ () => { result.current.reveal(); });
    await waitFor(/* 等待兜底文案。 */ () => expect(result.current.error).toBe('读取已发卡密失败'));

    cardsMock.mockResolvedValueOnce([{ id: 2, cardName: '组B', content: 'SECRET-2', deliveredAtMs: 1 }]);
    act(/* 再次查看成功。 */ () => { result.current.reveal(); });
    await waitFor(/* 等待卡密展示。 */ () => expect(result.current.cards).not.toBeNull());
    rerender({ orderId: 'o2' });
    expect(result.current.cards).toBeNull();
    expect(result.current.error).toBeNull();

    // pending 是切换订单前仍在进行的读取，晚到结果不得展示。
    const pending = deferred<Awaited<ReturnType<typeof getAuditOrderDeliveredCards>>>();
    cardsMock.mockReturnValueOnce(pending.promise);
    act(/* 对新订单发起读取。 */ () => { result.current.reveal(); });
    // signal 是该读取的取消信号。
    const signal = cardsMock.mock.calls.at(-1)?.[1]?.signal;
    rerender({ orderId: 'o3' });
    expect(signal?.aborted).toBe(true);
    await act(/* 旧读取晚到，必须被忽略。 */ async () => { pending.resolve([{ id: 9, cardName: 'X', content: 'LATE', deliveredAtMs: 1 }]); });
    expect(result.current.cards).toBeNull();
  });
});
