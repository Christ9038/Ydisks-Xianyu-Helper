// admin 只公开 OpenAPI 生成的管理统计传输类型；仪表盘展示模型由 feature 维护。
import type { components } from './generated/schema';

/** DashboardStatsTransport 表示生成的仪表盘概览响应。 */
export type DashboardStatsTransport = components['schemas']['DashboardStatsResponse'];
/** AnalyticsTransport 表示生成的订单分析响应。 */
export type AnalyticsTransport = components['schemas']['AnalyticsResponse'];
