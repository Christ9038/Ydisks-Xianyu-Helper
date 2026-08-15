// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail, Item } from '../../../types';
import { getAccountDetails, getItems, getOrders, importOrders } from './api';
import { useOrderImport, useOrderQuery } from './hooks';

vi.mock('./api', /* ordersApiMockFactory 提供订单 Hook 的确定性 API 替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getItems: vi.fn(),
  getOrders: vi.fn(),
  importOrders: vi.fn(),
}));

// getAccountsMock 是订单辅助账号请求的可控替身。
const getAccountsMock = vi.mocked(getAccountDetails);
// getItemsMock 是订单辅助商品请求的可控替身。
const getItemsMock = vi.mocked(getItems);
// getOrdersMock 是订单分页请求的可控替身。
const getOrdersMock = vi.mocked(getOrders);
// importOrdersMock 是订单导入请求的可控替身。
const importOrdersMock = vi.mocked(importOrders);

// accountFixture 是订单筛选账号测试对象。
const accountFixture: AccountDetail = { id: 'account-1', enabled: true, auto_confirm: false, nickname: '测试账号', remark: '账号备注' };
// itemFixture 是订单商品名称映射测试对象。
const itemFixture: Item = { id: 'item-row', cookie_id: 'account-1', item_id: 'item-1', item_title: '测试商品' };
// orderFixture 是订单列表测试使用的最小订单对象。
const orderFixture = { id: 'order-1', order_id: 'order-1', cookie_id: 'account-1', item_id: 'item-1', item_title: '', status: 'pending_ship', buyer_id: 'buyer-1' } as never;

// noopLoadOrders 是导入成功后刷新订单列表的异步替身。
const noopLoadOrders = vi.fn().mockResolvedValue(undefined);

describe('useOrderQuery 与 useOrderImport', /* 当前回调处理订单查询和导入 Hook 的请求边界。 */ () => {
  beforeEach(/* 当前回调重置订单 API 替身和浏览器提示。 */ () => {
    vi.clearAllMocks();
    getAccountsMock.mockResolvedValue([accountFixture]);
    getItemsMock.mockResolvedValue([itemFixture]);
    getOrdersMock.mockResolvedValue({ success: true, data: [orderFixture], total: 1, page: 1, page_size: 20, total_pages: 2, trigger_counts: {} });
    vi.spyOn(window, 'alert').mockImplementation(
      // alertImplementation 屏蔽订单导入成功时的浏览器提示。
      () => undefined,
    );
  });

  test('订单查询加载辅助数据并提供展示名称解析', /* 当前回调验证订单列表 Hook 成功路径。 */ async () => {
    // hook 是订单查询 Hook 的渲染结果。
    const hook = renderHook(
      // queryHookFactory 创建订单查询 Hook。
      () => useOrderQuery({ pageSize: 20 }),
    );
    await waitFor(
      // loadingAssertion 等待订单请求完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(hook.result.current.orders).toEqual([orderFixture]);
    expect(hook.result.current.accounts).toEqual([accountFixture]);
    expect(hook.result.current.items).toEqual([itemFixture]);
    expect(hook.result.current.totalPages).toBe(2);
    expect(hook.result.current.accountName('account-1')).toBe('账号备注 · accoun');
    expect(hook.result.current.accountNickname('account-1')).toBe('账号备注');
    expect(hook.result.current.getItemNameById('account-1', 'item-1')).toBe('测试商品');
    expect(hook.result.current.getItemNameById('account-1', 'missing', '订单商品标题')).toBe('订单商品标题');
    expect(hook.result.current.getItemNameById('account-1', 'missing')).toBe('未知商品');
    expect(hook.result.current.accountName('missing-account')).toBe('账号 missing-');
    expect(hook.result.current.accountNickname('missing-account')).toBe('未命名账号');
  });

  test('订单导入成功后刷新列表并关闭弹窗', /* 当前回调验证订单导入成功路径。 */ async () => {
    importOrdersMock.mockResolvedValue({ partial_failure: false, message: '导入完成', total: 1, success_count: 1, failed_count: 0, results: [{ order_id: 'order-1', success: true, message: '成功' }] });
    // hook 是订单导入 Hook 的渲染结果。
    const hook = renderHook(
      // importHookFactory 创建订单导入 Hook。
      () => useOrderImport(noopLoadOrders),
    );
    // file 是可提交的 CSV 订单文件。
    const file = new File(['order_id'], 'orders.csv', { type: 'text/csv' });
    await act(
      // openAction 打开订单导入弹窗。
      () => hook.result.current.openImportModal(),
    );
    await act(
      // fileAction 将 CSV 文件写入导入表单。
      () => hook.result.current.setImportFile(file),
    );
    await act(
      // importAction 提交成功的订单导入请求。
      async () => hook.result.current.handleImportOrders(),
    );
    expect(importOrdersMock).toHaveBeenCalledWith(expect.any(FormData), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(noopLoadOrders).toHaveBeenCalled();
    expect(hook.result.current.showImportModal).toBe(false);
    expect(hook.result.current.importError).toBe('');
    expect(window.alert).toHaveBeenCalledWith('订单导入成功，共 1 条');
  });

  test('订单导入校验和服务失败都保留错误状态', /* 当前回调验证订单导入失败与重试准备路径。 */ async () => {
    // hook 是订单导入失败场景下的 Hook 渲染结果。
    const hook = renderHook(
      // importHookFactory 创建失败场景的订单导入 Hook。
      () => useOrderImport(noopLoadOrders),
    );
    // invalidFile 是不支持的 TXT 文件。
    const invalidFile = new File(['bad'], 'orders.txt', { type: 'text/plain' });
    await act(
      // invalidFileAction 将不支持的文件写入导入表单。
      () => hook.result.current.setImportFile(invalidFile),
    );
    await act(
      // invalidImportAction 提交不支持格式并触发前端校验。
      async () => hook.result.current.handleImportOrders(),
    );
    expect(hook.result.current.importError).toContain('仅支持');
    importOrdersMock.mockRejectedValueOnce(new Error('导入服务失败'));
    // validFile 是可以进入服务请求阶段的 JSON 文件。
    const validFile = new File(['{}'], 'orders.json', { type: 'application/json' });
    await act(
      // validFileAction 将 JSON 文件写入导入表单。
      () => hook.result.current.setImportFile(validFile),
    );
    await act(
      // failedImportAction 提交并验证服务失败提示。
      async () => hook.result.current.handleImportOrders(),
    );
    expect(hook.result.current.importError).toBe('导入服务失败');
    expect(hook.result.current.importing).toBe(false);
  });

  test('部分失败导入保留结果并支持重试和关闭', /* 当前回调验证订单导入部分成功状态机。 */ async () => {
    // hook 是订单导入部分失败场景的 Hook 渲染结果。
    const hook = renderHook(
      // partialHookFactory 创建部分失败场景的订单导入 Hook。
      () => useOrderImport(noopLoadOrders),
    );
    // file 是进入服务端导入阶段的 CSV 文件。
    const file = new File(['order_id'], 'partial.csv', { type: 'text/csv' });
    importOrdersMock.mockResolvedValueOnce({ partial_failure: true, message: '部分失败', total: 2, success_count: 1, failed_count: 1, results: [{ order_id: 'order-1', success: true, message: '成功' }, { order_id: 'order-2', success: false, message: '失败' }] });
    await act(
      // openAction 打开订单导入弹窗。
      () => hook.result.current.openImportModal(),
    );
    await act(
      // fileAction 写入部分失败导入文件。
      () => hook.result.current.setImportFile(file),
    );
    await act(
      // importAction 提交部分失败导入请求。
      async () => hook.result.current.handleImportOrders(),
    );
    expect(hook.result.current.showImportModal).toBe(true);
    expect(hook.result.current.importResult).toMatchObject({ failed_count: 1, success_count: 1 });
    expect(hook.result.current.importFile).toBeNull();

    importOrdersMock.mockResolvedValueOnce({ partial_failure: false, message: '重试成功', total: 1, success_count: 1, failed_count: 0, results: [{ order_id: 'order-2', success: true, message: '成功' }] });
    await act(
      // retryFileAction 写入重试所需的导入文件。
      () => hook.result.current.setImportFile(file),
    );
    await act(
      // retryAction 重试部分失败导入。
      async () => hook.result.current.handleRetryImport(),
    );
    expect(importOrdersMock).toHaveBeenCalledTimes(2);
    await act(
      // closeAction 关闭订单导入弹窗并清理结果。
      () => hook.result.current.closeImportModal(),
    );
    expect(hook.result.current.showImportModal).toBe(false);
    expect(hook.result.current.importResult).toBeNull();
  });

  test('订单查询失败时记录错误并结束加载状态', /* 当前回调验证订单列表请求错误收口。 */ async () => {
    getOrdersMock.mockRejectedValueOnce(new Error('订单查询失败'));
    // consoleError 是订单查询错误日志的可控替身。
    const consoleError = vi.spyOn(console, 'error').mockImplementation(/* errorLogger 忽略测试日志输出。 */ () => undefined);
    // hook 是订单查询失败场景的 Hook 渲染结果。
    const hook = renderHook(
      // queryFailureHookFactory 创建订单查询失败场景的 Hook。
      () => useOrderQuery({ pageSize: 20 }),
    );
    await waitFor(
      // loadingAssertion 等待订单查询失败后的加载状态收口。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(consoleError).toHaveBeenCalledWith('加载订单失败:', expect.any(Error));
    hook.unmount();
    consoleError.mockRestore();
  });

  test('订单搜索输入经过防抖后以去空格文本重新查询', /* 当前回调验证订单搜索防抖和参数标准化。 */ async () => {
    // hook 是订单搜索防抖场景的 Hook 渲染结果。
    const hook = renderHook(
      // searchHookFactory 创建订单搜索场景的 Hook。
      () => useOrderQuery({ pageSize: 20 }),
    );
    await waitFor(
      // loadingAssertion 等待订单初始查询完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await act(
      // searchAction 写入带有首尾空格的搜索文本。
      () => hook.result.current.setSearchText('  买家  '),
    );
    await waitFor(
      // searchAssertion 等待防抖查询携带标准化搜索文本。
      () => expect(getOrdersMock).toHaveBeenLastCalledWith(undefined, 'all', 1, 20, '买家', expect.objectContaining({ signal: expect.any(AbortSignal) })),
      { timeout: 1_000 },
    );
    hook.unmount();
  });

  test('订单账号商品辅助数据失败时仅记录辅助数据错误', /* 当前回调验证订单展示辅助请求的独立失败分支。 */ async () => {
    getAccountsMock.mockRejectedValueOnce(new Error('账号辅助数据失败'));
    // consoleError 是辅助数据错误日志的可控替身。
    const consoleError = vi.spyOn(console, 'error').mockImplementation(/* errorLogger 忽略测试日志输出。 */ () => undefined);
    // hook 是订单辅助数据失败场景的 Hook 渲染结果。
    const hook = renderHook(
      // auxiliaryFailureHookFactory 创建订单辅助数据失败场景的 Hook。
      () => useOrderQuery({ pageSize: 20 }),
    );
    await waitFor(
      // loadingAssertion 等待订单主查询完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await waitFor(
      // auxiliaryErrorAssertion 等待辅助数据错误日志产生。
      () => expect(consoleError).toHaveBeenCalledWith('加载订单辅助数据失败:', expect.any(Error)),
    );
    expect(hook.result.current.orders).toEqual([orderFixture]);
    hook.unmount();
    consoleError.mockRestore();
  });

  test('没有导入文件时不会提交订单导入请求', /* 当前回调验证订单导入提交前置条件。 */ async () => {
    // hook 是没有导入文件场景的订单导入 Hook 渲染结果。
    const hook = renderHook(
      // emptyImportHookFactory 创建空文件导入场景的 Hook。
      () => useOrderImport(noopLoadOrders),
    );
    await act(
      // emptyImportAction 在没有文件时触发导入守卫。
      async () => hook.result.current.handleImportOrders(),
    );
    expect(importOrdersMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('重复查询订单时丢弃先发出的旧响应', /* 当前回调验证订单列表请求代次隔离。 */ async () => {
    // OrderPageResponse 是旧订单查询使用的最小成功响应。
    type OrderPageResponse = {
      // success 表示请求成功。
      success: true;
      // data 保存订单列表。
      data: typeof orderFixture[];
      // total 保存订单总数。
      total: number;
      // page 保存当前页码。
      page: number;
      // pageSize 保存分页大小。
      page_size: number;
      // totalPages 保存总页数。
      total_pages: number;
      // triggerCounts 保存状态聚合数量。
      trigger_counts: Record<string, number>;
    };
    // resolveFirst 是旧订单查询的完成控制器。
    let resolveFirst: (value: OrderPageResponse) => void = () => undefined;
    // firstRequest 是保持未完成的旧订单查询 Promise。
    const firstRequest = new Promise<OrderPageResponse>(/* firstExecutor 保存旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    getOrdersMock.mockReset();
    getOrdersMock.mockReturnValueOnce(firstRequest);
    getOrdersMock.mockResolvedValue({ success: true, data: [orderFixture], total: 1, page: 1, page_size: 20, total_pages: 2, trigger_counts: {} });
    // hook 是订单查询刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(
      // staleOrderHookFactory 创建订单旧响应场景的 Hook。
      () => useOrderQuery({ pageSize: 20 }),
    );
    await act(
      // refreshAction 发起第二次订单查询并使首次请求过期。
      async () => hook.result.current.loadOrders(),
    );
    resolveFirst({ success: true, data: [orderFixture], total: 1, page: 1, page_size: 20, total_pages: 2, trigger_counts: {} });
    await act(
      // staleResolveAction 完成已过期的首次订单响应。
      async () => { await firstRequest; },
    );
    expect(hook.result.current.orders).toEqual([orderFixture]);
    hook.unmount();
  });
});
