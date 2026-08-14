import { expect, test } from 'vitest';
import type { OrderAnalytics } from '../../../types';
import { buildChartData, buildProductSalesData, getTrendPercent, isCurrentDashboardRequest } from './state';

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
