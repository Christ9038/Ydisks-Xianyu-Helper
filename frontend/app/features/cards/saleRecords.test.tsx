// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import { downloadBlob } from '../../../shared/browser/download';
import { exportCard,getCardSaleRecords } from './api';
import type { CardSalePage } from './models';
import { useCardExport,useCardSaleRecords } from './saleRecords';

vi.mock('./api', /* saleApiMockFactory 提供售出记录 Hook 的确定性 API 替身。 */ () => ({
  getCardSaleRecords: vi.fn(),
  exportCard: vi.fn(),
}));
vi.mock('../../../shared/browser/download', /* downloadMockFactory 屏蔽真实浏览器下载。 */ () => ({ downloadBlob: vi.fn() }));

// listMock 是售出记录查询的可控替身。
const listMock = vi.mocked(getCardSaleRecords);
// exportMock 是导出请求的可控替身。
const exportMock = vi.mocked(exportCard);
// downloadMock 记录触发的浏览器下载。
const downloadMock = vi.mocked(downloadBlob);

// pageOf 构造一页只含一条记录的测试结果。
const pageOf = (content: string, totalPages = 1): CardSalePage => ({
  records: [{ id: 1, content, status: 'sold', cardName: '组A', cookieId: 'c1', orderId: 'o1', itemId: 'i1', buyerId: 'b1', soldAtMs: 1000 }],
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

describe('useCardSaleRecords', /* 当前回调验证售出记录 Hook 的加载、筛选、取消和乱序响应。 */ () => {
  beforeEach(/* 当前回调重置替身。 */ () => {
    vi.clearAllMocks();
  });

  // 用例：首次加载成功并按条件重新查询。
  test('首次加载成功并按条件重新查询', async () => {
    listMock.mockResolvedValue(pageOf('K1'));
    // result 是 Hook 的当前渲染结果引用。
    const { result } = renderHook(/* hookFactory 创建售出记录 Hook。 */ () => useCardSaleRecords(7));
    expect(result.current.loading).toBe(true);
    await waitFor(/* 等待首次加载结束。 */ () => expect(result.current.loading).toBe(false));
    expect(result.current.page?.records[0].content).toBe('K1');
    expect(listMock.mock.calls[0][1]).toEqual({ status: '', search: '', page: 1, pageSize: 20 });

    act(/* 切换到第 3 页后再改状态，应回到第 1 页。 */ () => { result.current.setPage(3); });
    await waitFor(/* 等待第 3 页请求。 */ () => expect(listMock.mock.calls.at(-1)?.[1].page).toBe(3));
    act(/* 切换状态筛选。 */ () => { result.current.setStatus('reserved'); });
    await waitFor(/* 等待筛选请求。 */ () => expect(listMock.mock.calls.at(-1)?.[1]).toMatchObject({ status: 'reserved', page: 1 }));
    act(/* 应用带空白的搜索词。 */ () => { result.current.applySearch('  o1 '); });
    await waitFor(/* 等待搜索请求。 */ () => expect(listMock.mock.calls.at(-1)?.[1]).toMatchObject({ search: 'o1', page: 1, status: 'reserved' }));
    act(/* 页码下限为 1。 */ () => { result.current.setPage(-5); });
    await waitFor(/* 等待页码回落。 */ () => expect(listMock.mock.calls.at(-1)?.[1].page).toBe(1));
  });

  // 用例：加载失败展示错误，重试成功后清除错误。
  test('加载失败展示错误，重试成功后清除错误', async () => {
    listMock.mockRejectedValueOnce(new Error('服务暂不可用')).mockResolvedValueOnce(pageOf('K2'));
    const { result } = renderHook(/* hookFactory 创建售出记录 Hook。 */ () => useCardSaleRecords(7));
    await waitFor(/* 等待失败状态。 */ () => expect(result.current.error).toBe('服务暂不可用'));
    expect(result.current.loading).toBe(false);
    act(/* 点击重试。 */ () => { result.current.reload(); });
    await waitFor(/* 等待成功。 */ () => expect(result.current.page?.records[0].content).toBe('K2'));
    expect(result.current.error).toBeNull();

    listMock.mockRejectedValueOnce('boom');
    act(/* 再次重载并遇到非 Error 失败。 */ () => { result.current.reload(); });
    await waitFor(/* 等待兜底文案。 */ () => expect(result.current.error).toBe('加载售出记录失败'));
  });

  // 用例：条件变化后旧响应晚到不覆盖当前结果，卸载会取消请求。
  test('条件变化后旧响应晚到不覆盖当前结果，卸载会取消请求', async () => {
    // first 与 second 分别是旧条件和新条件的挂起请求。
    const first = deferred<CardSalePage>();
    // second 是第二次查询的可控延迟结果，用于模拟旧响应晚到。
    const second = deferred<CardSalePage>();
    listMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    // result、unmount 分别是 Hook 渲染结果与卸载函数。
    const { result, unmount } = renderHook(/* hookFactory 创建售出记录 Hook。 */ () => useCardSaleRecords(7));
    act(/* 在首次请求未返回时切换状态，触发第二次请求。 */ () => { result.current.setStatus('sold'); });
    // firstSignal 是被取消的旧请求信号。
    const firstSignal = listMock.mock.calls[0][2]?.signal;
    await waitFor(/* 等待第二次请求发出。 */ () => expect(listMock).toHaveBeenCalledTimes(2));
    expect(firstSignal?.aborted).toBe(true);

    await act(/* 新响应先到。 */ async () => { second.resolve(pageOf('NEW')); });
    await waitFor(/* 等待新结果展示。 */ () => expect(result.current.page?.records[0].content).toBe('NEW'));
    await act(/* 旧响应晚到，必须被忽略。 */ async () => { first.resolve(pageOf('OLD')); });
    expect(result.current.page?.records[0].content).toBe('NEW');
    expect(result.current.loading).toBe(false);

    // pending 是卸载时仍在进行的请求。
    const pending = deferred<CardSalePage>();
    listMock.mockReturnValueOnce(pending.promise);
    act(/* 触发新请求后立即卸载。 */ () => { result.current.reload(); });
    // lastSignal 是卸载前最后一次请求的取消信号。
    const lastSignal = listMock.mock.calls.at(-1)?.[2]?.signal;
    unmount();
    expect(lastSignal?.aborted).toBe(true);
  });

  // 用例：旧请求失败不会污染新请求状态。
  test('旧请求失败不会污染新请求状态', /* 旧请求失败用例回调。 */ async () => {
    // stale 是将被取消后才失败的旧请求。
    const stale = deferred<CardSalePage>();
    listMock.mockReturnValueOnce(stale.promise).mockResolvedValueOnce(pageOf('FRESH'));
    const { result } = renderHook(/* hookFactory 创建售出记录 Hook。 */ () => useCardSaleRecords(7));
    act(/* 切换条件取消旧请求。 */ () => { result.current.setStatus('sold'); });
    await waitFor(/* 等待新结果。 */ () => expect(result.current.page?.records[0].content).toBe('FRESH'));
    await act(/* 旧请求取消后晚到失败。 */ async () => { stale.reject(new Error('已取消')); });
    expect(result.current.error).toBeNull();
    expect(result.current.page?.records[0].content).toBe('FRESH');
  });
});

describe('useCardExport', /* 当前回调验证导出 Hook 的成功、失败、重复点击和卸载取消。 */ () => {
  beforeEach(/* 当前回调重置替身。 */ () => {
    vi.clearAllMocks();
  });

  // 用例：导出成功触发下载并恢复空闲。
  test('导出成功触发下载并恢复空闲', async () => {
    // blob 是服务端返回的导出文件内容。
    const blob = new Blob(['K1\n']);
    exportMock.mockResolvedValue({ blob, filename: '组A-未售.txt' });
    const { result } = renderHook(/* hookFactory 创建导出 Hook。 */ () => useCardExport(7));
    await act(/* 请求未售导出。 */ async () => { await result.current.runExport('unsold'); });
    expect(exportMock.mock.calls[0][0]).toBe(7);
    expect(exportMock.mock.calls[0][1]).toBe('unsold');
    expect(downloadMock).toHaveBeenCalledWith(blob, '组A-未售.txt');
    expect(result.current.exporting).toBeNull();
    expect(result.current.exportError).toBeNull();
  });

  // 用例：导出失败展示错误且不下载，随后可再次导出。
  test('导出失败展示错误且不下载，随后可再次导出', async () => {
    exportMock.mockRejectedValueOnce(new Error('仅 data 类型支持')).mockRejectedValueOnce('boom');
    const { result } = renderHook(/* hookFactory 创建导出 Hook。 */ () => useCardExport(7));
    await act(/* 第一次失败。 */ async () => { await result.current.runExport('unsold'); });
    expect(result.current.exportError).toBe('仅 data 类型支持');
    expect(downloadMock).not.toHaveBeenCalled();
    await act(/* 第二次遇到非 Error 失败。 */ async () => { await result.current.runExport('sold'); });
    expect(result.current.exportError).toBe('导出失败');
    expect(result.current.exporting).toBeNull();
  });

  // 用例：导出进行中忽略重复点击。
  test('导出进行中忽略重复点击', async () => {
    // pending 是挂起的导出请求。
    // pending 是可控延迟的导出结果，用于保持导出进行中状态。
    const pending = deferred<{ /* blob 是导出文件内容。 */ blob: Blob; /* filename 是下载文件名。 */ filename: string }>();
    exportMock.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(/* hookFactory 创建导出 Hook。 */ () => useCardExport(7));
    act(/* 发起第一次导出。 */ () => { void result.current.runExport('sold'); });
    await waitFor(/* 等待进入导出中状态。 */ () => expect(result.current.exporting).toBe('sold'));
    await act(/* 重复点击应被忽略。 */ async () => { await result.current.runExport('sold'); });
    expect(exportMock).toHaveBeenCalledTimes(1);
    await act(/* 完成导出。 */ async () => { pending.resolve({ blob: new Blob(['x']), filename: 'a.csv' }); });
    await waitFor(/* 等待恢复空闲。 */ () => expect(result.current.exporting).toBeNull());
    expect(downloadMock).toHaveBeenCalledTimes(1);
  });

  // 用例：卸载后晚到的导出结果不再触发下载。
  test('卸载后晚到的导出结果不再触发下载', /* 卸载后晚到导出用例回调。 */ async () => {
    // pending 是卸载时仍在进行的导出请求。
    // pending 是可控延迟的导出结果，卸载后再手动完成。
    const pending = deferred<{ /* blob 是导出文件内容。 */ blob: Blob; /* filename 是下载文件名。 */ filename: string }>();
    exportMock.mockReturnValueOnce(pending.promise);
    const { result, unmount } = renderHook(/* hookFactory 创建导出 Hook。 */ () => useCardExport(7));
    act(/* 发起导出。 */ () => { void result.current.runExport('sold'); });
    // signal 是导出请求的取消信号。
    const signal = exportMock.mock.calls[0][2]?.signal;
    unmount();
    expect(signal?.aborted).toBe(true);
    await act(/* 请求在卸载后才完成。 */ async () => { pending.resolve({ blob: new Blob(['x']), filename: 'a.csv' }); });
    expect(downloadMock).not.toHaveBeenCalled();
  });
});
