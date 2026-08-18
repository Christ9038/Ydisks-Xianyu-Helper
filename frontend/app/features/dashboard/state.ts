import type { TimeRange } from '../../../dateRange';
import type { Item,OrderAnalytics } from './api';
import type { DashboardRangeSelection } from './types';

/** 趋势图中的单个日期数据点。 */
export type DashboardChartPoint = {
  /** 图表横轴显示的日期文本。 */
  name: string;
  /** 当日营收金额。 */
  amount: number;
  /** 当日订单数。 */
  orders: number;
  /** 当日平均订单金额。 */
  avgAmount: number | string;
};

/** 商品排行图中的单个数据点。 */
export type DashboardProductPoint = {
  /** 商品展示名称。 */
  name: string;
  /** 商品订单数。 */
  sales: number;
};

/** 商品占比图中的单个数据点。 */
export type DashboardSourcePoint = {
  /** 商品展示名称。 */
  name: string;
  /** 商品订单数。 */
  value: number;
  /** 商品订单占比。 */
  percent: number;
  /** 图表主题颜色。 */
  color: string;
};

/** 商品金额图中的单个数据点。 */
export type DashboardCategoryPoint = {
  /** 商品展示名称。 */
  name: string;
  /** 商品总金额。 */
  value: number;
  /** 商品订单数。 */
  orderCount: number;
  /** 商品金额占比文本。 */
  percentage: string;
  /** 图表主题颜色。 */
  color: string;
};

/** 将商品列表转换为商品 ID 到商品名称的索引。 */
export const buildItemNameMap = (items: Item[]): Record<string, string> => {
  // itemNames 是商品 ID 到展示名称的索引。
  const itemNames: Record<string, string> = {};
  items.forEach(
    // item 是待建立索引的商品记录。
    item => {
      itemNames[item.item_id] = item.item_title || item.item_id;
    },
  );
  return itemNames;
};

/** 将订单分析数据转换为趋势图需要的平面数据。 */
export const buildChartData = (analytics: OrderAnalytics): DashboardChartPoint[] => (
  analytics.daily_stats?.map(
    // dailyStat 是某一天的订单统计。
    dailyStat => ({
      name: dailyStat.date.slice(5),
      amount: dailyStat.amount,
      orders: dailyStat.order_count,
      avgAmount: dailyStat.order_count > 0 ? (dailyStat.amount / dailyStat.order_count).toFixed(2) : 0,
    }),
  ) || []
);

/** 将商品统计转换为销量排行数据。 */
export const buildProductSalesData = (analytics: OrderAnalytics, itemNames: Record<string, string>): DashboardProductPoint[] => (
  (analytics.item_stats || [])
    .map(
      // item 是单个商品的销售统计。
      item => {
        // itemName 是商品优先使用标题的展示名称。
        const itemName = itemNames[item.item_id] || item.item_id;
        return { name: itemName.length > 12 ? `${itemName.substring(0, 12)}...` : itemName, sales: item.order_count };
      },
    )
    .sort(/* 当前回调处理集合中的单个元素。 */ (left, right) => right.sales - left.sales)
    .slice(0, 10)
);

/** 将商品统计转换为订单来源占比数据。 */
export const buildSourceData = (analytics: OrderAnalytics, itemNames: Record<string, string>, colors: string[]): DashboardSourcePoint[] => {
  // totalOrders 是当前分析周期的订单总数。
  const totalOrders = analytics.revenue_stats.total_orders || 0;
  return (analytics.item_stats || [])
    .map(
      // item 是单个商品的订单统计。
      item => {
        // itemName 是商品优先使用标题的展示名称。
        const itemName = itemNames[item.item_id] || item.item_id;
        return {
          name: itemName.length > 10 ? `${itemName.substring(0, 10)}...` : itemName,
          value: item.order_count,
          percent: totalOrders > 0 ? (item.order_count / totalOrders) * 100 : 0,
        };
      },
    )
    .sort(/* 当前回调处理集合中的单个元素。 */ (left, right) => right.value - left.value)
    .slice(0, 6)
    .map(
      // item 是截取后的商品占比数据，index 用于选择稳定颜色。
      (item, index) => ({ ...item, color: colors[index % colors.length] }),
    );
};

/** 将商品统计转换为金额排行数据。 */
export const buildCategoryData = (analytics: OrderAnalytics, itemNames: Record<string, string>, colors: string[]): DashboardCategoryPoint[] => {
  // totalAmount 是当前分析周期的营收总额。
  const totalAmount = analytics.revenue_stats.total_amount || 0;
  return (analytics.item_stats || [])
    .map(
      // item 是单个商品的金额统计。
      item => {
        // itemName 是商品优先使用标题的展示名称。
        const itemName = itemNames[item.item_id] || item.item_id;
        return {
          name: itemName.length > 12 ? `${itemName.substring(0, 12)}...` : itemName,
          value: item.total_amount,
          orderCount: item.order_count,
          percentage: totalAmount > 0 ? (item.total_amount / totalAmount) * 100 : 0,
        };
      },
    )
    .sort(/* 当前回调处理集合中的单个元素。 */ (left, right) => right.value - left.value)
    .slice(0, 5)
    .map(
      // item 是截取后的金额排行数据，index 用于选择稳定颜色。
      (item, index) => ({ ...item, color: colors[index % colors.length], percentage: item.percentage.toFixed(1) }),
    );
};

/** 根据当前与上一个周期的营收计算趋势百分比。 */
export const getTrendPercent = (analytics: OrderAnalytics | null, previousAnalytics: OrderAnalytics | null): string | null => {
  if (!analytics || !previousAnalytics) return null;
  // currentAmount 是本周期的营收金额。
  const currentAmount = analytics.revenue_stats.total_amount;
  // previousAmount 是上一个等长周期的营收金额。
  const previousAmount = previousAnalytics.revenue_stats.total_amount;
  if (previousAmount === 0) return currentAmount > 0 ? '+100%' : '0%';
  // percent 是两个周期之间的营收变化比例。
  const percent = ((currentAmount - previousAmount) / previousAmount) * 100;
  return `${percent >= 0 ? '+' : ''}${percent.toFixed(1)}%`;
};

/** 返回时间范围选择器显示文本。 */
export const getRangeLabel = (selection: DashboardRangeSelection): string => {
  // labels 是时间范围到中文展示文本的映射。
  const labels: Record<TimeRange, string> = {
    today: '今天',
    yesterday: '昨天',
    '3days': '三天内',
    '7days': '7天内',
    '30days': '一个月内',
    custom: '自定义',
  };
  return labels[selection.range] || '所选范围';
};

/** 计算销量排行中用于进度条归一化的最大值。 */
export const getMaxProductSales = (productSales: DashboardProductPoint[]): number => Math.max(...productSales.map(/* 当前回调处理集合中的单个元素。 */ item => item.sales), 1);

/** 判断旧的日期范围请求结果是否仍然可以写入当前页面。 */
export const isCurrentDashboardRequest = (currentSequence: number, requestSequence: number, signal: AbortSignal): boolean => (
  currentSequence === requestSequence && !signal.aborted
);
