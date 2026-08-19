// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 OrderStatus UI 模型；不直接暴露 HTTP DTO。 */
export type OrderStatus =
  | 'processing'
  | 'pending_ship'
  | 'shipped'
  | 'completed'
  | 'cancelled'
  | 'refunding'
  | 'unknown';

/** 由当前 feature adapter 归一后的 Order UI 模型；不直接暴露 HTTP DTO。 */
export interface Order {
  /** 订单本地数据库主键。 */
  id: string;
  /** 平台订单标识。 */
  order_id: string;
  /** 订单所属账号标识。 */
  cookie_id: string;
  /** 订单关联商品标识。 */
  item_id: string;
  /** 商品标题。 */
  item_title?: string;
  /** 商品图片地址。 */
  item_image?: string;
  /** 商品价格文本。 */
  item_price?: string;
  /** 买家平台标识。 */
  buyer_id: string;
  /** 购买数量。 */
  quantity: number;
  /** 订单金额文本。 */
  amount: string;
  /** 前端归一化后的订单状态。 */
  status: OrderStatus;
  /** 服务端原始订单状态。 */
  order_status?: OrderStatus;
  /** 收货人姓名。 */
  receiver_name?: string;
  /** 收货人电话。 */
  receiver_phone?: string;
  /** 收货地址。 */
  receiver_address?: string;
  /** 订单创建时间。 */
  created_at?: string;
  /** 订单更新时间。 */
  updated_at?: string;
}

/** 由当前 feature adapter 归一后的 Item UI 模型；不直接暴露 HTTP DTO。 */
export interface Item {
  /** 本地商品数据库主键。 */
  id: string | number;
  /** 商品所属账号标识。 */
  cookie_id: string;
  /** 平台商品标识。 */
  item_id: string;
  /** 商品标题。 */
  item_title?: string;
  /** 商品描述。 */
  item_description?: string;
  /** 商品价格文本。 */
  item_price?: string;
  /** 商品主图地址。 */
  item_image?: string; // Inferred from common usage, though not explicitly in list model sometimes
  /** 商品分类标识。 */
  item_category?: string;
  /** 商品详情原始 JSON。 */
  item_detail?: string;
  /** 是否启用多规格。 */
  is_multi_spec?: number | boolean;
  /** 是否按数量发货。 */
  multi_quantity_delivery?: number | boolean;
  /** 是否启用多数量发货兼容字段。 */
  is_multi_qty_ship?: number | boolean;
}

/** 仪表盘兼容历史 items 包装时使用的最小响应模型。 */
export interface ItemListEnvelope {
  /** 历史包装中的商品数组。 */ items?: Item[];
}

/** 由当前 feature adapter 归一后的 AdminStats UI 模型；不直接暴露 HTTP DTO。 */
export interface AdminStats {
  /** 用户总数。 */
  total_users: number;
  /** 账号总数。 */
  total_cookies: number;
  /** 启用账号数。 */
  active_cookies: number;
  /** 卡券组总数。 */
  total_cards: number;
  /** 关键词规则总数。 */
  total_keywords: number;
  /** 订单总数。 */
  total_orders: number;
}

/** 由当前 feature adapter 归一后的 DashboardStats UI 模型；不直接暴露 HTTP DTO。 */
export interface DashboardStats {
  /** 账号总数。 */
  total_cookies: number;
  /** 启用账号数。 */
  active_cookies: number;
  /** 卡券组总数。 */
  total_cards: number;
  /** 关键词规则总数。 */
  total_keywords: number;
  /** 订单总数。 */
  total_orders: number;
  /** 可用卡券库存量。 */
  available_card_stock: number;
}

/** 由当前 feature adapter 归一后的 OrderAnalytics UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderAnalytics {
  /** 收入汇总。 */
  revenue_stats: {
    /** 总收入金额。 */
    total_amount: number;
    /** 总订单数。 */
    total_orders: number;
  };
  /** 按日期聚合的订单统计。 */
  daily_stats: Array<{
    /** 统计日期。 */
    date: string;
    /** 当日订单金额。 */
    amount: number;
    /** 当日订单数量。 */
    order_count: number;
  }>;
  /** 按商品聚合的订单统计。 */
  item_stats?: Array<{
    /** 商品标识。 */
    item_id: string;
    /** 商品订单数。 */
    order_count: number;
    /** 商品订单总金额。 */
    total_amount: number;
    /** 商品订单平均金额。 */
    avg_amount: number;
  }>;
}

/** 管理员全局统计响应。 */
/** 由当前 feature adapter 归一后的 AdminStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AdminStatsResponse extends AdminStats {}

/** 当前用户数据概览响应。 */
/** 由当前 feature adapter 归一后的 DashboardStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface DashboardStatsResponse extends DashboardStats {}

/** 订单收益统计响应。 */
/** 由当前 feature adapter 归一后的 AnalyticsRevenueStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AnalyticsRevenueStatsResponse {
  /** 统计范围内的订单数。 */
  total_orders: number;
  /** 统计范围内的订单总金额。 */
  total_amount: number;
  /** 订单平均金额。 */
  avg_amount: number;
  /** 买家数量。 */
  unique_buyers: number;
  /** 商品数量。 */
  unique_items: number;
}

/** 按日期聚合的订单统计响应。 */
/** 由当前 feature adapter 归一后的 AnalyticsDailyStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AnalyticsDailyStatsResponse {
  /** 用户本地日期。 */
  date: string;
  /** 当天订单数。 */
  order_count: number;
  /** 当天订单金额。 */
  amount: number;
}

/** 按订单状态聚合的统计响应。 */
/** 由当前 feature adapter 归一后的 AnalyticsStatusStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AnalyticsStatusStatsResponse {
  /** 归一化后的订单状态。 */
  status: string;
  /** 该状态订单数。 */
  count: number;
  /** 该状态订单金额。 */
  amount: number;
}

/** 按收货城市聚合的统计响应。 */
/** 由当前 feature adapter 归一后的 AnalyticsCityStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AnalyticsCityStatsResponse {
  /** 收货城市。 */
  city: string;
  /** 该城市订单数。 */
  order_count: number;
  /** 该城市订单金额。 */
  total_amount: number;
}

/** 按商品聚合的统计响应。 */
/** 由当前 feature adapter 归一后的 AnalyticsItemStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AnalyticsItemStatsResponse {
  /** 商品平台标识。 */
  item_id: string;
  /** 该商品订单数。 */
  order_count: number;
  /** 该商品订单金额。 */
  total_amount: number;
  /** 该商品订单平均金额。 */
  avg_amount: number;
}

/** 订单分析接口响应。 */
/** 由当前 feature adapter 归一后的 OrderAnalyticsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderAnalyticsResponse {
  /** 收益统计。 */
  revenue_stats: AnalyticsRevenueStatsResponse;
  /** 按日统计。 */
  daily_stats: AnalyticsDailyStatsResponse[];
  /** 按状态统计。 */
  status_stats: AnalyticsStatusStatsResponse[];
  /** 按城市统计。 */
  city_stats: AnalyticsCityStatsResponse[];
  /** 按商品统计。 */
  item_stats: AnalyticsItemStatsResponse[];
}

/** 有效订单明细响应。 */
/** 由当前 feature adapter 归一后的 ValidOrderResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ValidOrderResponse {
  /** 平台订单标识。 */
  order_id: string;
  /** 商品平台标识。 */
  item_id: string;
  /** 买家平台标识。 */
  buyer_id: string;
  /** 商品标题。 */
  item_title: string;
  /** 商品图片地址。 */
  item_image: string;
  /** 订单数量文本。 */
  quantity: string;
  /** 订单金额文本。 */
  amount: string;
  /** 兼容保留的订单状态。 */
  order_status: string;
  /** 归一化后的订单状态。 */
  status: string;
  /** 订单所属账号标识。 */
  cookie_id: string;
  /** 订单创建时间。 */
  created_at: string;
}

/** 有效订单分页响应。 */
/** 由当前 feature adapter 归一后的 ValidOrdersResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ValidOrdersResponse {
  /** 当前页有效订单。 */
  orders: ValidOrderResponse[];
  /** 符合条件的订单总数。 */
  total: number;
  /** 当前页码。 */
  page: number;
  /** 当前页大小。 */
  page_size: number;
  /** 是否还有未返回的订单。 */
  truncated: boolean;
}
