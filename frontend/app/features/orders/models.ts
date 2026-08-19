// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 PaginatedResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface PaginatedResponse<T> {
  /** 表示查询是否成功。 */
  success: boolean;
  /** 当前页数据。 */
  data: T[];
  /** 符合条件的总记录数。 */
  total: number;
  /** 当前页码。 */
  page: number;
  /** 每页记录数。 */
  page_size: number;
  /** 总页数。 */
  total_pages: number;
  /** 各触发类型的规则计数。 */
  trigger_counts?: Record<string, number>;
}

/** 由当前 feature adapter 归一后的 AccountDetail UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountDetail {
  /** 闲鱼账号稳定标识。 */
  id: string;
  /** 账号是否已配置平台凭证；摘要接口只返回状态，不返回 Cookie 明文。 */
  cookie_configured?: boolean;
  /** 账号是否允许运行。 */
  enabled: boolean;
  /** 是否自动确认订单。 */
  auto_confirm: boolean;
  /** 用户为账号设置的备注。 */
  remark?: string;
  /** 自动回复暂停时长，单位为分钟。 */
  pause_duration?: number;
  /** 暂停结束时间的 Unix 秒。 */
  paused_until?: number;
  /** 当前是否处于暂停状态。 */
  paused?: boolean;
  // 登录信息
  /** 用于密码登录的闲鱼用户名。 */
  username?: string;
  /** 是否已保存密码登录秘密；摘要接口只返回状态，不返回密码明文。 */
  login_password_configured?: boolean;
  /** 是否在密码登录时显示浏览器。 */
  show_browser?: boolean;
  // Frontend helpers
  /** 平台账号昵称。 */
  nickname?: string;
  /** 平台账号头像地址。 */
  avatar_url?: string;
  /** 资料刷新失败时的说明。 */
  profile_error?: string;
  /** 当前账号运行状态。 */
  runtime_state?: 'starting' | 'connecting' | 'online' | 'reconnecting' | 'auth_expired' | 'verification_required' | 'runtime_conflict' | 'error' | 'stopped' | 'disabled';
  /** 当前运行状态的用户可见说明。 */
  runtime_message?: string;
  /** 当前运行实例是否已连接。 */
  runtime_connected?: boolean;
  /** 运行状态快照的更新时间。 */
  runtime_updated_at?: string;
  // AI设置
  /** 是否启用账号 AI 回复。 */
  ai_enabled?: boolean;
  /** 允许的最大折扣比例。 */
  max_discount_percent?: number;
  /** 允许的最大折扣金额。 */
  max_discount_amount?: number;
  /** 允许的最大砍价轮次。 */
  max_bargain_rounds?: number;
  /** 账号自定义提示词。 */
  custom_prompts?: string;
	// 账号级计划任务
	/** 是否启用自动评价。 */
	auto_rate_enabled?: boolean;
	/** 自动评价使用的文案。 */
	rate_content?: string;
	/** 是否启用每日擦亮。 */
	auto_polish_enabled?: boolean;
	/** 每日擦亮执行时间。 */
	polish_time?: string;
	/** 最近一次自动评价扫描时间。 */
	last_rate_scan_at?: number;
	/** 最近一次擦亮日期。 */
	last_polish_date?: string;
	/** 最近一次擦亮时间。 */
	last_polish_at?: number;
}

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

/** 订单详情接口返回的原始具名订单 DTO。 */
/** 由当前 feature adapter 归一后的 OrderDTOResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderDTOResponse {
  /** 平台订单标识。 */
  order_id: string;
  /** 关联商品标识。 */
  item_id: string;
  /** 关联商品标题。 */
  item_title: string;
  /** 关联商品图片地址。 */
  item_image: string;
  /** 买家平台标识。 */
  buyer_id: string;
  /** 商品规格名称。 */
  spec_name: string;
  /** 商品规格值。 */
  spec_value: string;
  /** 购买数量文本。 */
  quantity: string;
  /** 实付金额文本。 */
  amount: string;
  /** 归一化订单状态。 */
  order_status: string;
  /** 兼容前端使用的订单状态别名。 */
  status: string;
  /** 所属账号标识。 */
  cookie_id: string;
  /** 是否议价订单。 */
  is_bargain: number;
  /** 是否系统发货。 */
  system_shipped: boolean;
  /** 收货人姓名。 */
  receiver_name: string;
  /** 收货人电话。 */
  receiver_phone: string;
  /** 收货地址。 */
  receiver_address: string;
  /** 收货城市。 */
  receiver_city: string;
  /** 创建时间。 */
  created_at: string;
  /** 更新时间。 */
  updated_at: string;
}

/** 订单详情接口的具名响应。 */
/** 由当前 feature adapter 归一后的 OrderDetailResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderDetailResponse extends OrderDTOResponse {
  /** 表示查询是否完成。 */
  success: boolean;
  /** 新版客户端读取的订单对象。 */
  data: OrderDTOResponse;
}

/** 订单单条刷新返回的远端详情。 */
/** 由当前 feature adapter 归一后的 OrderRefreshDetailResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshDetailResponse {
  /** 购买数量文本。 */
  quantity: string;
  /** 商品规格名称。 */
  spec_name: string;
  /** 商品规格值。 */
  spec_value: string;
  /** 归一化订单状态。 */
  order_status: string;
  /** 实付金额文本。 */
  amount: string;
}

/** 订单单条刷新接口的具名响应。 */
/** 由当前 feature adapter 归一后的 OrderSingleRefreshResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderSingleRefreshResponse {
  /** 表示刷新是否完成。 */
  success: boolean;
  /** 刷新结果说明。 */
  message: string;
  /** 刷新后的订单详情。 */
  order: OrderRefreshDetailResponse;
}

/** 订单批量变更接口的具名响应。 */
/** 由当前 feature adapter 归一后的 OrderBatchResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderBatchResponse {
  /** 表示批量操作是否存在部分失败。 */
  partial_failure: boolean;
  /** 批量操作结果说明。 */
  message: string;
  /** 订单总数，导入接口提供。 */
  total?: number;
  /** 成功处理数量。 */
  success_count: number;
  /** 失败处理数量。 */
  failed_count: number;
	/** 逐订单兼容结果行。 */
	results: OrderBatchResult[];
}

/** 订单批量接口的逐订单结果行。 */
/** 由当前 feature adapter 归一后的 OrderBatchResult UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderBatchResult {
  /** 订单平台标识。 */
  order_id?: string;
  /** 订单动作状态：failed、succeeded 或 reconciliation_required。 */
  status?: 'failed' | 'succeeded' | 'reconciliation_required';
  /** 表示该订单是否处理成功。 */
  success?: boolean;
  /** 该订单处理结果说明。 */
  message: string;
  /** 兼容接口可能返回的账号标识。 */
  cookie_id?: string;
  /** 待补偿记录标识。 */
  reconciliation_id?: string;
  /** 本地状态或补偿记录写入警告。 */
  reconciliation_warning?: string;
  /** 兼容接口可能返回的处理阶段。 */
  stage?: string;
  /** 允许后端保留尚未结构化的扩展字段。 */
  [key: string]: unknown;
}

/** 简单资源创建接口的数值主键响应。 */
/** 由当前 feature adapter 归一后的 MutationIDResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface MutationIDResponse {
  /** 资源创建是否完成。 */
  success: boolean;
  /** 新资源数值主键。 */
  id: number;
}

/** 简单变更接口的统一成功响应。 */
/** 由当前 feature adapter 归一后的 OperationResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OperationResponse {
  /** 操作是否完成。 */
  success: boolean;
  /** 可选的操作说明。 */
  message?: string;
  /** 操作完成后是否需要重新登录。 */
  requires_relogin?: boolean;
}

/** 管理员全局统计响应。 */
/** 由当前 feature adapter 归一后的 AdminStats UI 模型；不直接暴露 HTTP DTO。 */
export interface AdminStats {
  /** 管理员可见的用户总数。 */ total_users: number;
  /** 当前用户拥有的账号总数。 */ total_cookies: number;
  /** 已启用账号数量。 */ active_cookies: number;
  /** 卡券组总数。 */ total_cards: number;
  /** 关键词规则总数。 */ total_keywords: number;
  /** 订单总数。 */ total_orders: number;
}

/** 订单域兼容页面使用的数据概览字段。 */
export interface DashboardStats {
  /** 当前用户的账号总数。 */ total_cookies: number;
  /** 已启用账号数量。 */ active_cookies: number;
  /** 卡券组总数。 */ total_cards: number;
  /** 关键词规则总数。 */ total_keywords: number;
  /** 订单总数。 */ total_orders: number;
  /** 可用卡券库存。 */ available_card_stock: number;
}

/** 管理员全局统计响应。 */
/** 由当前 feature adapter 归一后的 AdminStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AdminStatsResponse extends AdminStats {}

/** 当前用户数据概览响应。 */
/** 由当前 feature adapter 归一后的 DashboardStatsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface DashboardStatsResponse extends DashboardStats {}

/** 订单列表刷新逐项结果。 */
/** 由当前 feature adapter 归一后的 OrderRefreshResultResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshResultResponse {
  /** 结果所属账号标识。 */
  cookie_id?: string;
  /** 当前处理阶段。 */
  stage?: string;
  /** 当前项是否处理成功。 */
  success: boolean;
  /** 结果说明。 */
  message?: string;
  /** 发现的新订单数量。 */
  discovered?: number;
  /** 更新的订单数量。 */
  updated?: number;
  /** 标记删除的订单数量。 */
  /** soft_deleted 表示该订单是否已被本次刷新标记为软删除。 */
  soft_deleted?: boolean;
  /** 订单平台标识。 */
  order_id?: string;
  /** 结果错误说明。 */
  error?: string;
}

/** 订单列表刷新统计摘要。 */
/** 由当前 feature adapter 归一后的 OrderRefreshSummaryResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshSummaryResponse {
  /** 发现的新订单数量。 */
  discovered: number;
  /** 订单列表更新数量。 */
  list_updated: number;
  /** 标记删除数量。 */
  soft_deleted: number;
  /** 需要补全详情的订单数量。 */
  detail_total: number;
  /** 本次处理订单总数。 */
  total: number;
  /** 状态发生变化数量。 */
  updated: number;
  /** 状态未变化数量。 */
  no_change: number;
  /** 刷新失败数量。 */
  failed: number;
}

/** 订单列表刷新响应。 */
/** 由当前 feature adapter 归一后的 OrderRefreshResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshResponse {
  /** 是否存在部分失败。 */
  partial_failure: boolean;
  /** 刷新结果说明。 */
  message: string;
  /** 刷新统计摘要。 */
  summary: OrderRefreshSummaryResponse;
  /** 逐项兼容结果。 */
  results: OrderRefreshResultResponse[];
}

/** 创建订单刷新后台任务的响应。 */
/** 由当前 feature adapter 归一后的 OrderRefreshJobStartResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshJobStartResponse {
  /** 任务是否创建成功。 */
  success: boolean;
  /** 后台任务标识。 */
  job_id: string;
  /** 任务当前状态。 */
  status: 'queued' | 'running';
}

/** 查询订单刷新后台任务的响应。 */
/** 由当前 feature adapter 归一后的 OrderRefreshJobStatusResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshJobStatusResponse {
  /** 查询是否成功。 */
  success: boolean;
  /** 后台任务标识。 */
  job_id: string;
  /** 任务当前状态。 */
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';
  /** 任务失败原因。 */
  error_message?: string;
  /** 任务成功后的订单刷新结果。 */
  result?: OrderRefreshResponse;
}

/** 取消订单刷新后台任务的响应。 */
/** 由当前 feature adapter 归一后的 OrderRefreshJobCancelResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OrderRefreshJobCancelResponse {
  /** 取消命令是否成功应用。 */
  success: boolean;
  /** 被取消的任务标识。 */
  job_id: string;
  /** 取消后的任务状态。 */
  status: 'cancelled';
}
