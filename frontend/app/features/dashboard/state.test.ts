import { expect, test } from 'vitest';
import type { OrderAnalytics } from '../../../shared/api-contract';
import { buildCategoryData, buildChartData, buildItemNameMap, buildProductSalesData, buildSourceData, getMaxProductSales, getRangeLabel, getTrendPercent, isCurrentDashboardRequest } from './state';

// analyticsFixture 是覆盖趋势、商品排行和零值边界的最小分析数据。
const analyticsFixture: OrderAnalytics = {
  revenue_stats: { total_amount: 120, total_orders: 3 },
  daily_stats: [{ date: '2026-08-15', amount: 120, order_count: 3 }],
  item_stats: [{ item_id: 'item-1', order_count: 3, total_amount: 120, avg_amount: 40 }],
};

test('Dashboard 派生数据保持趋势和排行口径一致',
  // 派生数据测试验证日期、商品名称和营收趋势不会在组件渲染中漂移。
  () => {
    expect(buildChartData(analyticsFixture)[0]).toMatchObject({ name: '08-15', orders: 3, avgAmount: '40.00' });
    expect(buildProductSalesData(analyticsFixture, { 'item-1': '测试商品' })).toEqual([{ name: '测试商品', sales: 3 }]);
    expect(getTrendPercent(analyticsFixture, { ...analyticsFixture, revenue_stats: { total_amount: 100, total_orders: 2 } })).toBe('+20.0%');
  });

test('Dashboard 派生数据覆盖空值、零值和名称截断分支',
  // 边界数据测试验证没有订单或缺少商品标题时仍能稳定渲染。
  () => {
    // empty 是没有任何订单的分析结果。
    const empty = { revenue_stats: { total_amount: 0, total_orders: 0 }, daily_stats: [], item_stats: [] } as OrderAnalytics;
    // longName 用于验证商品名称超过展示长度时会被截断。
    const longName = '这是一个超过十二个字符的商品名称';
    // analytics 是包含一个商品统计的非空分析结果。
    const analytics = {
      ...empty,
      revenue_stats: { total_amount: 10, total_orders: 2 },
      item_stats: [{ item_id: 'item-long', order_count: 2, total_amount: 10, avg_amount: 5 }],
    } as OrderAnalytics;
    expect(buildItemNameMap([{ item_id: 'item-long', item_title: '' } as never])).toEqual({ 'item-long': 'item-long' });
    expect(buildChartData(empty)).toEqual([]);
    expect(buildChartData({ ...empty, daily_stats: [{ date: '2026-08-15', amount: 0, order_count: 0 }] })).toEqual([{ name: '08-15', amount: 0, orders: 0, avgAmount: 0 }]);
    expect(buildProductSalesData({ ...analytics, item_stats: [{ ...analytics.item_stats![0], item_id: 'item-long' }] }, { 'item-long': longName })[0].name).toBe('这是一个超过十二个字符的...');
    // rankedAnalytics 是包含多个商品的排行数据，用于覆盖排序比较器。
    const rankedAnalytics = { ...analytics, revenue_stats: { total_amount: 40, total_orders: 4 }, item_stats: [{ item_id: 'item-a', order_count: 1, total_amount: 10, avg_amount: 10 }, { item_id: 'item-b', order_count: 3, total_amount: 30, avg_amount: 10 }] } as OrderAnalytics;
    expect(buildProductSalesData(rankedAnalytics, { 'item-a': '商品A', 'item-b': '商品B' })).toEqual([{ name: '商品B', sales: 3 }, { name: '商品A', sales: 1 }]);
    expect(buildSourceData(rankedAnalytics, { 'item-a': '商品A', 'item-b': '商品B' }, ['red', 'blue'])[0]).toMatchObject({ name: '商品B', percent: 75, color: 'red' });
    expect(buildCategoryData(rankedAnalytics, { 'item-a': '商品A', 'item-b': '商品B' }, ['blue', 'red'])[0]).toMatchObject({ name: '商品B', percentage: '75.0', color: 'blue' });
    expect(buildSourceData(analytics, {}, ['red'])[0]).toMatchObject({ name: 'item-long', percent: 100, color: 'red' });
    expect(buildCategoryData(analytics, {}, ['blue'])[0]).toMatchObject({ name: 'item-long', percentage: '100.0', color: 'blue' });
    expect(getTrendPercent(null, analytics)).toBeNull();
    expect(getTrendPercent(analytics, null)).toBeNull();
    expect(getTrendPercent(analytics, empty)).toBe('+100%');
    expect(getTrendPercent(empty, empty)).toBe('0%');
    expect(getTrendPercent({ ...analytics, revenue_stats: { total_amount: 50, total_orders: 1 } }, analytics)).toBe('+400.0%');
    expect(getRangeLabel({ range: 'today' } as never)).toBe('今天');
    expect(getRangeLabel({ range: 'custom' } as never)).toBe('自定义');
    expect(getRangeLabel({ range: 'unknown' } as never)).toBe('所选范围');
    expect(getMaxProductSales([])).toBe(1);
    expect(getMaxProductSales([{ name: 'x', sales: 4 }])).toBe(4);
  });
test('Dashboard 请求代次和取消信号拒绝过期响应',
  // 请求边界测试验证刷新后的旧请求不能覆盖新数据，主动取消也不会写入状态。
  () => {
    // controller 是模拟页面生命周期取消的控制器。
    const controller = new AbortController();
    expect(isCurrentDashboardRequest(4, 4, controller.signal)).toBe(true);
    expect(isCurrentDashboardRequest(3, 4, controller.signal)).toBe(false);
    controller.abort();
    expect(isCurrentDashboardRequest(4, 4, controller.signal)).toBe(false);
  });
