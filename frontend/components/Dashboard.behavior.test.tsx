// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import Dashboard from './Dashboard';
import { useDashboard } from '../app/features/dashboard/hooks';

vi.mock('../app/features/dashboard/hooks', /* dashboardHookMockFactory 提供仪表盘页面的可控状态。 */ () => ({ useDashboard: vi.fn() }));
vi.mock('../app/features/dashboard/DashboardTrendChart', /* trendChartMockFactory 隔离图表渲染实现。 */ () => ({ DashboardTrendChart: /* trendChartRenderer 渲染图表占位节点。 */ () => <div data-testid="trend-chart" /> }));

// useDashboardMock 是仪表盘 Hook 的可控替身。
const useDashboardMock = vi.mocked(useDashboard);

describe('Dashboard 页面加载守卫', /* 当前回调覆盖仪表盘错误和加载分支。 */ () => {
  test('概览和分析同时缺失时展示错误并允许重新加载', /* 当前回调验证仪表盘错误状态和重试按钮。 */ () => {
    // refresh 是仪表盘重新加载动作替身。
    const refresh = vi.fn();
    useDashboardMock.mockReturnValue({ data: null, status: { overview: 'error', range: 'success', error: '加载失败' }, chartData: [], productSalesData: [], sourceData: [], categoryData: [], maxProductSales: 0, trendPercent: null, selectedRangeLabel: '最近7天', refresh });
    render(<Dashboard />);
    expect(screen.getByText('加载失败')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }));
    expect(refresh).toHaveBeenCalled();
  });

  test('数据尚未就绪时展示加载状态', /* 当前回调验证仪表盘初始加载占位。 */ () => {
    useDashboardMock.mockReturnValue({ data: null, status: { overview: 'loading', range: 'loading', error: '' }, chartData: [], productSalesData: [], sourceData: [], categoryData: [], maxProductSales: 0, trendPercent: null, selectedRangeLabel: '最近7天', refresh: vi.fn() });
    render(<Dashboard />);
    expect(document.querySelector('.animate-spin')).toBeTruthy();
  });
});
