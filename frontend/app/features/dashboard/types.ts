import type { DateRange,TimeRange } from '../../../dateRange';
import type { DashboardStats,Item,Order,OrderAnalytics } from './api';

/** Dashboard 请求状态。 */
export type DashboardRequestState = 'idle' | 'loading' | 'success' | 'error';

/** Dashboard 当前选中的时间范围。 */
export type DashboardRangeSelection = {
  /** 当前选择的快捷时间范围。 */
  range: TimeRange;
  /** 自定义范围起始日期。 */
  customStartDate: string;
  /** 自定义范围结束日期。 */
  customEndDate: string;
};

/** Dashboard 订单明细分页结果。 */
export type DashboardValidOrders = {
  /** 当前范围内参与统计的订单。 */
  orders: Order[];
  /** 后端匹配到的订单总数。 */
  total: number;
  /** 是否因为上限而截断订单明细。 */
  truncated: boolean;
};

/** Dashboard 页面请求得到的完整数据。 */
export type DashboardData = {
  /** 概览统计数据。 */
  stats: DashboardStats;
  /** 当前时间范围的订单分析。 */
  analytics: OrderAnalytics;
  /** 上一个等长时间范围的订单分析。 */
  previousAnalytics: OrderAnalytics;
  /** 当前范围的有效订单明细。 */
  validOrders: DashboardValidOrders;
  /** 页面商品列表。 */
  items: Item[];
  /** 商品名称索引。 */
  itemNames: Record<string, string>;
  /** 当前生效的日期范围。 */
  dateRange: DateRange;
};

/** Dashboard 各类请求的可观察状态。 */
export type DashboardStatus = {
  /** 概览统计请求状态。 */
  overview: DashboardRequestState;
  /** 趋势和订单请求状态。 */
  range: DashboardRequestState;
  /** 最近一次可展示的错误。 */
  error: string;
};
