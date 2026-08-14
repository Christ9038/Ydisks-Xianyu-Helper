import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getDateRange, getPreviousDateRange } from '../../../dateRange';
import type { TimeRange } from '../../../dateRange';
import { buildCategoryData, buildChartData, buildItemNameMap, buildProductSalesData, buildSourceData, getMaxProductSales, getRangeLabel, getTrendPercent, isCurrentDashboardRequest } from './state';
import { getDashboardStats, getItems, getOrderAnalytics, getValidOrders } from './api';
import type { DashboardData, DashboardRangeSelection, DashboardStatus } from './types';

/** Dashboard Hook 的输入参数。 */
export type UseDashboardOptions = DashboardRangeSelection & { customRangeVersion: number };

/** Dashboard Hook 暴露给页面的可视化数据与操作。 */
export type UseDashboardResult = {
  data: DashboardData | null;
  status: DashboardStatus;
  chartData: ReturnType<typeof buildChartData>;
  productSalesData: ReturnType<typeof buildProductSalesData>;
  sourceData: ReturnType<typeof buildSourceData>;
  categoryData: ReturnType<typeof buildCategoryData>;
  maxProductSales: number;
  trendPercent: string | null;
  selectedRangeLabel: string;
  refresh: () => void;
};

/** 判断请求错误是否来自用户主动取消。 */
const isAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 统一转换 Dashboard 请求错误。 */
const errorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 加载 Dashboard 概览、趋势和订单明细，并隔离过期响应。 */
export const useDashboard = (options: UseDashboardOptions): UseDashboardResult => {
  const { range, customStartDate, customEndDate, customRangeVersion } = options;
  const [overview, setOverview] = useState<Pick<DashboardData, 'stats' | 'items' | 'itemNames'> | null>(null);
  const [rangeData, setRangeData] = useState<Pick<DashboardData, 'analytics' | 'previousAnalytics' | 'validOrders' | 'dateRange'> | null>(null);
  const [status, setStatus] = useState<DashboardStatus>({ overview: 'idle', range: 'idle', error: '' });
  const [refreshKey, setRefreshKey] = useState(0);
  const requestSequence = useRef(0);
  const rangeSelection = useMemo(
    // selection 是当前时间范围的不可变快照。
    () => ({ range, customStartDate, customEndDate }),
    [customEndDate, customStartDate, range],
  );

  const refresh = useCallback(
    // 重新加载按钮回调只递增版本号，实际请求由两个 effect 统一管理。
    () => setRefreshKey(value => value + 1),
    [],
  );

  useEffect(() => {
    const controller = new AbortController();
    setStatus(current => ({ ...current, overview: 'loading', error: '' }));
    Promise.all([
      getDashboardStats({ signal: controller.signal }),
      getItems(undefined, { signal: controller.signal }),
    ]).then(([stats, items]) => {
      if (controller.signal.aborted) return;
      setOverview({ stats, items, itemNames: buildItemNameMap(items) });
      setStatus(current => ({ ...current, overview: 'success' }));
    }).catch(error => {
      if (controller.signal.aborted || isAbortError(error)) return;
      setStatus(current => ({ ...current, overview: 'error', error: errorMessage(error, '概览加载失败') }));
    });
    return () => controller.abort();
  }, [refreshKey]);

  useEffect(() => {
    let dateRange;
    try {
      dateRange = getDateRange(range, new Date(), customStartDate, customEndDate);
    } catch (error) {
      setStatus(current => ({ ...current, range: 'error', error: errorMessage(error, '日期范围无效') }));
      return;
    }
    const previousRange = getPreviousDateRange(dateRange);
    const sequence = ++requestSequence.current;
    const controller = new AbortController();
    setStatus(current => ({ ...current, range: 'loading', error: '' }));
    Promise.all([
      getOrderAnalytics({ start_date: dateRange.startDate, end_date: dateRange.endDate }, { signal: controller.signal }),
      getOrderAnalytics({ start_date: previousRange.startDate, end_date: previousRange.endDate }, { signal: controller.signal }),
      getValidOrders({ start_date: dateRange.startDate, end_date: dateRange.endDate }, { signal: controller.signal }),
    ]).then(([analytics, previousAnalytics, validOrders]) => {
      if (!isCurrentDashboardRequest(requestSequence.current, sequence, controller.signal)) return;
      setRangeData({ analytics, previousAnalytics, validOrders, dateRange });
      setStatus(current => ({ ...current, range: 'success' }));
    }).catch(error => {
      if (!isCurrentDashboardRequest(requestSequence.current, sequence, controller.signal) || isAbortError(error)) return;
      setStatus(current => ({ ...current, range: 'error', error: errorMessage(error, '经营数据加载失败') }));
    });
    return () => controller.abort();
  }, [customRangeVersion, range, refreshKey]);

  const data = useMemo<DashboardData | null>(
    // dashboardData 只有在概览和时间范围请求都成功后才对页面可见。
    () => overview && rangeData ? { ...overview, ...rangeData } : null,
    [overview, rangeData],
  );
  const itemNames = overview?.itemNames || {};
  const analytics = rangeData?.analytics || null;
  const chartData = useMemo(() => analytics ? buildChartData(analytics) : [], [analytics]);
  const productSalesData = useMemo(() => analytics ? buildProductSalesData(analytics, itemNames) : [], [analytics, itemNames]);
  const colors = useMemo(
    // colors 是图表使用的主题颜色序列。
    () => ['rgb(var(--color-brand))', 'rgb(var(--color-brand-highlight))', 'rgb(var(--color-success-500))', 'rgb(var(--color-warning-500))', 'rgb(var(--color-accent-500))'],
    [],
  );
  const sourceData = useMemo(() => analytics ? buildSourceData(analytics, itemNames, colors) : [], [analytics, colors, itemNames]);
  const categoryData = useMemo(() => analytics ? buildCategoryData(analytics, itemNames, colors) : [], [analytics, colors, itemNames]);
  const maxProductSales = useMemo(() => getMaxProductSales(productSalesData), [productSalesData]);

  return {
    data,
    status,
    chartData,
    productSalesData,
    sourceData,
    categoryData,
    maxProductSales,
    trendPercent: getTrendPercent(analytics, data?.previousAnalytics || null),
    selectedRangeLabel: getRangeLabel(rangeSelection),
    refresh,
  };
};
