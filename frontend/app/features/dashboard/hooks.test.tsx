// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { DashboardStats,Item,OrderAnalyticsResponse } from './api';
import { getDashboardStats,getItems,getOrderAnalytics,getValidOrders } from './api';
import type { UseDashboardOptions } from './hooks';
import { useDashboard } from './hooks';

vi.mock('./api', /* dashboardApiMockFactory 提供仪表盘 Hook 的确定性 API 替身。 */ () => ({
  getDashboardStats: vi.fn(),
  getItems: vi.fn(),
  getOrderAnalytics: vi.fn(),
  getValidOrders: vi.fn(),
}));

// getStatsMock 是读取仪表盘概览统计的可控请求替身。
const getStatsMock = vi.mocked(getDashboardStats);
// getItemsMock 是读取商品列表的可控请求替身。
const getItemsMock = vi.mocked(getItems);
// getAnalyticsMock 是读取订单分析数据的可控请求替身。
const getAnalyticsMock = vi.mocked(getOrderAnalytics);
// getValidOrdersMock 是读取有效订单的可控请求替身。
const getValidOrdersMock = vi.mocked(getValidOrders);

// statsFixture 是仪表盘概览统计测试数据。
const statsFixture: DashboardStats = { total_cookies: 2, active_cookies: 1, total_cards: 3, total_keywords: 4, total_orders: 5, available_card_stock: 6 };
// itemsFixture 是用于建立商品名称索引的测试数据。
const itemsFixture: Item[] = [{ id: 'item-1', cookie_id: 'account-1', item_id: 'item-1', item_title: '测试商品' }];
// analyticsFixture 是覆盖趋势图和商品排行的分析数据。
const analyticsFixture: OrderAnalyticsResponse = { revenue_stats: { total_amount: 100, total_orders: 2, avg_amount: 50, unique_buyers: 2, unique_items: 1 }, daily_stats: [{ date: '2026-08-15', amount: 100, order_count: 2 }], status_stats: [], city_stats: [], item_stats: [{ item_id: 'item-1', order_count: 2, total_amount: 100, avg_amount: 50 }] };
// validOrdersFixture 是有效订单查询的最小返回结果。
const validOrdersFixture = { orders: [], total: 0, truncated: false };
// dashboardOptions 是今天范围的仪表盘 Hook 参数。
const dashboardOptions: UseDashboardOptions = { range: 'today', customStartDate: '', customEndDate: '', customRangeVersion: 0 };

// renderDashboardHook 是渲染仪表盘 Hook 的具名回调。
const renderDashboardHook = () => useDashboard(dashboardOptions);

describe('useDashboard', /* 当前回调处理仪表盘并行请求和派生数据状态。 */ () => {
  beforeEach(/* 当前回调重置仪表盘 API 替身。 */ () => {
    vi.clearAllMocks();
    getStatsMock.mockResolvedValue(statsFixture);
    getItemsMock.mockResolvedValue(itemsFixture);
    getAnalyticsMock.mockResolvedValue(analyticsFixture);
    getValidOrdersMock.mockResolvedValue(validOrdersFixture);
  });

  test('并行加载概览和时间范围数据后生成派生结果', /* 当前回调验证仪表盘成功加载路径。 */ async () => {
    // hook 是仪表盘 Hook 的渲染结果。
    const hook = renderHook(renderDashboardHook);
    await waitFor(
      // overviewAssertion 等待概览请求成功。
      () => expect(hook.result.current.status.overview).toBe('success'),
    );
    await waitFor(
      // rangeAssertion 等待时间范围请求成功。
      () => expect(hook.result.current.status.range).toBe('success'),
    );
    expect(hook.result.current.data).toMatchObject({ stats: statsFixture, items: itemsFixture, itemNames: { 'item-1': '测试商品' } });
    expect(hook.result.current.chartData[0]).toMatchObject({ name: '08-15', orders: 2 });
    expect(hook.result.current.productSalesData).toEqual([{ name: '测试商品', sales: 2 }]);
    expect(hook.result.current.selectedRangeLabel).toBe('今天');
    expect(hook.result.current.maxProductSales).toBe(2);
    expect(getAnalyticsMock).toHaveBeenCalledTimes(2);
    expect(getValidOrdersMock).toHaveBeenCalledTimes(1);

    await act(
      // refreshAction 触发概览和范围数据重新加载。
      async () => hook.result.current.refresh(),
    );
    await waitFor(
      // refreshAssertion 等待刷新后的概览请求发出。
      () => expect(getStatsMock).toHaveBeenCalledTimes(2),
    );
  });

  test('概览请求失败时展示错误状态并保留范围请求', /* 当前回调验证概览错误分支。 */ async () => {
    getStatsMock.mockRejectedValueOnce(new Error('概览服务失败'));
    // hook 是概览请求失败场景下的仪表盘 Hook 渲染结果。
    const hook = renderHook(renderDashboardHook);
    await waitFor(
      // errorAssertion 等待概览错误状态写入。
      () => expect(hook.result.current.status.overview).toBe('error'),
    );
    expect(hook.result.current.status.error).toBe('概览服务失败');
    await waitFor(
      // rangeAssertion 等待概览失败时仍完成的范围请求。
      () => expect(hook.result.current.status.range).toBe('success'),
    );
  });

  test('自定义日期倒置时阻断范围请求并提示错误', /* 当前回调验证日期范围校验错误分支。 */ async () => {
    // invalidOptions 是开始日期晚于结束日期的非法范围参数。
    const invalidOptions: UseDashboardOptions = { range: 'custom', customStartDate: '2026-08-20', customEndDate: '2026-08-01', customRangeVersion: 1 };
    // renderInvalidHook 是非法日期范围 Hook 的渲染回调。
    const renderInvalidHook = () => useDashboard(invalidOptions);
    // hook 是非法日期范围 Hook 的渲染结果。
    const hook = renderHook(renderInvalidHook);
    await waitFor(
      // rangeAssertion 等待日期范围错误状态写入。
      () => expect(hook.result.current.status.range).toBe('error'),
    );
    expect(hook.result.current.status.error).toBe('开始日期不能晚于结束日期');
    expect(getAnalyticsMock).not.toHaveBeenCalled();
    expect(getValidOrdersMock).not.toHaveBeenCalled();
  });

  test('经营数据请求失败时展示范围错误', /* 当前回调验证趋势和有效订单请求失败分支。 */ async () => {
    getAnalyticsMock.mockRejectedValueOnce(new Error('经营数据服务失败'));
    // hook 是经营数据请求失败场景的仪表盘 Hook 渲染结果。
    const hook = renderHook(renderDashboardHook);
    await waitFor(
      // rangeErrorAssertion 等待范围请求错误状态写入。
      () => expect(hook.result.current.status.range).toBe('error'),
    );
    expect(hook.result.current.status.error).toBe('经营数据服务失败');
  });

  test('刷新期间丢弃旧经营数据响应', /* 当前回调验证仪表盘范围请求代次隔离。 */ async () => {
    // resolveFirst 是旧经营数据请求的完成控制器。
    let resolveFirst: (value: OrderAnalyticsResponse) => void = () => undefined;
    // firstRequest 是保持未完成的旧经营数据请求 Promise。
    const firstRequest = new Promise<OrderAnalyticsResponse>(/* firstExecutor 保存旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    getAnalyticsMock.mockReset();
    getAnalyticsMock.mockReturnValueOnce(firstRequest);
    getAnalyticsMock.mockResolvedValue(analyticsFixture);
    // hook 是仪表盘经营数据刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(renderDashboardHook);
    await act(
      // refreshAction 发起第二次经营数据刷新并使首次请求过期。
      () => hook.result.current.refresh(),
    );
    await waitFor(
      // rangeAssertion 等待第二次经营数据请求成功。
      () => expect(hook.result.current.status.range).toBe('success'),
    );
    resolveFirst(analyticsFixture);
    await act(
      // staleResolveAction 完成已过期的首次经营数据响应。
      async () => { await firstRequest; },
    );
    expect(hook.result.current.status.range).toBe('success');
    hook.unmount();
  });

  test('刷新期间丢弃忽略取消信号的旧概览响应', /* 当前回调验证概览请求即使底层实现没有响应取消，也不能覆盖刷新后的统计数据。 */ async () => {
    // resolveFirstStats 是首次概览统计请求的完成控制器。
    let resolveFirstStats: (value: DashboardStats) => void = () => undefined;
    // firstStatsRequest 是故意忽略 AbortSignal 的旧统计请求。
    const firstStatsRequest = new Promise<DashboardStats>(/* firstStatsExecutor 保存旧统计请求的完成函数。 */ resolve => { resolveFirstStats = resolve; });
    // refreshedStats 是刷新后应保留在页面中的权威概览数据。
    const refreshedStats = { ...statsFixture, total_orders: 99 };
    getStatsMock.mockReset();
    getStatsMock.mockReturnValueOnce(firstStatsRequest);
    getStatsMock.mockResolvedValueOnce(refreshedStats);
    // hook 是概览刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(renderDashboardHook);
    await act(
      // refreshAction 发起第二次概览加载并使首个请求过期。
      () => hook.result.current.refresh(),
    );
    await waitFor(
      // refreshedOverviewAssertion 等待新概览统计写入页面状态。
      () => expect(hook.result.current.data?.stats).toEqual(refreshedStats),
    );
    resolveFirstStats(statsFixture);
    await act(
      // staleOverviewResolveAction 完成被取消但仍返回的旧统计请求。
      async () => { await firstStatsRequest; },
    );
    expect(hook.result.current.data?.stats).toEqual(refreshedStats);
    hook.unmount();
  });
});
