import { formatLocalDate } from '../../../dateRange';
import type { DashboardStatsResponse,Item,ItemListEnvelope,Order,OrderAnalyticsResponse,OrderStatus,ValidOrderResponse,ValidOrdersResponse } from './models';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { collectionFrom, objectFrom } from '../../../shared/http/contract';
export type * from './models';

/** 仪表盘订单明细接口转换后的分页结果。 */
export interface ValidOrdersResult {
  /** 当前时间范围的订单明细。 */
  orders: Order[];
  /** 服务端匹配到的订单总数。 */
  total: number;
  /** 是否存在未返回的后续订单。 */
  truncated: boolean;
}

/** 将服务端订单状态映射为仪表盘可稳定展示的状态集合。 */
const normalizeOrderStatus = (value: unknown): OrderStatus => {
  // status 是服务端返回的原始或历史订单状态。
  const status = String(value || '');
  if (status === 'paid') return 'pending_ship';
  return ['processing', 'pending_ship', 'shipped', 'completed', 'cancelled', 'refunding'].includes(status) ? status as OrderStatus : 'unknown';
};

/** 读取仪表盘概览统计。 */
export const getDashboardStats = async (options?: RequestControlOptions): Promise<DashboardStatsResponse> =>
  runContractRequest(/* signal 是本次仪表盘统计请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/analytics/dashboard', { signal }), options);

/** 读取仪表盘商品索引，兼容历史 items 包装。 */
export const getItems = async (accountID?: string, options?: RequestControlOptions): Promise<Item[]> => {
  // response 是商品列表的原始 transport 响应。
  const response = await runContractRequest(/* signal 控制仪表盘商品读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items', { params: { query: { cookie_id: accountID } }, signal }), options) as unknown as Item[] | ItemListEnvelope;
  // items 是从直接数组或历史包装中取出的商品集合。
  const items = collectionFrom<Item>(response, ['items', 'data', 'results']);
  return items.map(/* item 是当前需要兼容布尔标记的商品传输记录。 */ item => ({ ...item, id: item.id || `${item.cookie_id}-${item.item_id}`, is_multi_spec: item.is_multi_spec === true || item.is_multi_spec === 1, multi_quantity_delivery: item.multi_quantity_delivery === true || item.multi_quantity_delivery === 1 }));
};

/** 读取指定自然日范围的订单统计，并显式传递浏览器时区偏移。 */
export const getOrderAnalytics = async (range: number | { /** 统计起始日期。 */ start_date: string; /** 统计结束日期。 */ end_date: string } = 7, options?: RequestControlOptions): Promise<OrderAnalyticsResponse> => {
  // dateRange 是标准化后的统计日期范围。
  let dateRange: { /** 统计起始日期。 */ start_date: string; /** 统计结束日期。 */ end_date: string };
  if (typeof range === 'number') {
    // endDate 是计算默认区间使用的当前本地日期。
    const endDate = new Date();
    // startDate 是向前回溯指定天数后的本地日期。
    const startDate = new Date();
    startDate.setDate(startDate.getDate() - range);
    dateRange = { start_date: formatLocalDate(startDate), end_date: formatLocalDate(endDate) };
  } else {
    dateRange = range;
  }
  return runContractRequest(/* signal 是本次订单分析请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/analytics/orders', {
    params: { query: { ...dateRange, timezone_offset_minutes: -new Date().getTimezoneOffset() } },
    signal,
  }), options);
};

/** 读取可参与仪表盘统计的订单并适配历史响应形状。 */
export const getValidOrders = async (dateRange: { /** 统计起始日期。 */ start_date: string; /** 统计结束日期。 */ end_date: string }, options?: RequestControlOptions): Promise<ValidOrdersResult> => {
  // response 是有效订单接口的未知兼容响应。
  const response = await runContractRequest(/* signal 是本次有效订单查询请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/analytics/orders/valid', {
    params: { query: { ...dateRange, timezone_offset_minutes: -new Date().getTimezoneOffset() } },
    signal,
  }), options);
  // page 是生成契约或历史 data/result 包装后的有效订单分页元数据。
  const page = objectFrom<Partial<ValidOrdersResponse>>(response as unknown, ['data', 'result']) || {};
  // rows 是当前分页或兼容包装中的有效订单 transport DTO。
  const rows = collectionFrom<ValidOrderResponse>(response as unknown, ['orders', 'data', 'items']);
  // orders 是转换状态、数量和稳定标识后的仪表盘订单模型。
  const orders = rows.map(/* row 是当前需要归一化状态与数量的有效订单 DTO。 */ row => ({ ...row, id: row.order_id, status: normalizeOrderStatus(row.status || row.order_status), order_status: normalizeOrderStatus(row.order_status), quantity: Number(row.quantity || 1) }));
  return { orders, total: Number(page.total ?? orders.length), truncated: page.truncated === true };
};
