// orders 只公开 OpenAPI 生成的订单传输类型；订单 UI 模型属于 orders feature。
import type { components } from './generated/schema';

/** OrderTransport 表示生成的订单行。 */
export type OrderTransport = components['schemas']['OrderDTO'];
/** OrderRefreshTransport 表示生成的订单刷新结果。 */
export type OrderRefreshTransport = components['schemas']['OrderRefreshResponse'];
