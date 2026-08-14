
// API Response Bases
export interface ApiResponse {
  success?: boolean;
  message?: string;
  msg?: string;
}

export interface PaginatedResponse<T> {
  success: boolean;
  data: T[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  trigger_counts?: Record<string, number>;
}

// Auth
export interface LoginResponse {
  success: boolean;
  token?: string;
  message?: string;
  user_id?: number;
  username?: string;
  is_admin?: boolean;
}

// Accounts
export interface AccountDetail {
  id: string;
  value?: string; // cookie value from backend
  cookie?: string; // alias for value
  enabled: boolean;
  auto_confirm: boolean;
  remark?: string;
  note?: string; // alias for remark
  pause_duration?: number;
  paused_until?: number;
  paused?: boolean;
  // 登录信息
  username?: string;
  login_password?: string;
  show_browser?: boolean;
  // Frontend helpers
  nickname?: string;
  avatar_url?: string;
  profile_error?: string;
  runtime_state?: 'starting' | 'connecting' | 'online' | 'reconnecting' | 'auth_expired' | 'verification_required' | 'error' | 'stopped' | 'disabled';
  runtime_message?: string;
  runtime_connected?: boolean;
  runtime_updated_at?: string;
  // AI设置
  ai_enabled?: boolean;
  max_discount_percent?: number;
  max_discount_amount?: number;
  max_bargain_rounds?: number;
  custom_prompts?: string;
	// 账号级计划任务
	auto_rate_enabled?: boolean;
	rate_content?: string;
	auto_polish_enabled?: boolean;
	polish_time?: string;
	last_rate_scan_at?: number;
	last_polish_date?: string;
	last_polish_at?: number;
}

export interface AccountTaskSettings {
	account_id: string;
	auto_rate_enabled: boolean;
	rate_content: string;
	auto_polish_enabled: boolean;
	polish_time: string;
	last_rate_scan_at?: number;
	last_polish_date?: string;
	last_polish_at?: number;
}

export interface AccountTaskSummary {
	task_type: 'auto_rate' | 'auto_polish';
	found: number;
	success: number;
	failed: number;
	skipped: number;
	message?: string;
}

export interface ChatSession {
	account_id: string;
	chat_id: string;
	buyer_id: string;
	buyer_name: string;
	buyer_avatar_url?: string;
	item_id?: string;
	item_title?: string;
	last_message: string;
	last_message_at: number;
	unread_count: number;
}

export interface ChatMessage {
	id: number;
	account_id: string;
	chat_id: string;
	message_key: string;
	direction: 'incoming' | 'outgoing';
	sender_id: string;
	sender_name: string;
	/** text/image/video are peer messages; system is an official platform notice or trade card. */
	message_type: 'text' | 'image' | 'video' | 'system';
	content: string;
	status: 'received' | 'sending' | 'sent' | 'failed';
	sent_at: number;
}

// Orders
export type OrderStatus = 
  | 'processing'      
  | 'pending_ship'    
  | 'shipped'         
  | 'completed'       
  | 'cancelled'       
  | 'refunding'
  | 'unknown';

export interface Order {
  id: string;
  order_id: string;
  cookie_id: string;
  item_id: string;
  item_title?: string;
  item_image?: string;
  item_price?: string;
  buyer_id: string;
  quantity: number;
  amount: string;
  status: OrderStatus;
  order_status?: OrderStatus;
  receiver_name?: string;
  receiver_phone?: string;
  receiver_address?: string;
  created_at?: string;
  updated_at?: string;
}

// Cards
export interface Card {
  id: number;
  name: string;
  type: 'api' | 'text' | 'data' | 'image';
  description?: string;
  enabled: boolean;
  // 文本类型
  text_content?: string;
  // 批量数据类型
  data_content?: string;
  // API 类型配置
  api_config?: {
    url: string;
    method: 'GET' | 'POST';
    timeout?: number;
    headers?: string;
    params?: string;
  };
  // 图片类型
  image_url?: string;
  // 通用配置
  delay_seconds?: number;
  // 多规格配置
  is_multi_spec?: boolean;
  spec_name?: string;
  spec_value?: string;
  created_at: string;
  updated_at: string;
}

// Items
export interface Item {
  id: string | number;
  cookie_id: string;
  item_id: string;
  item_title?: string;
  item_description?: string;
  item_price?: string;
  item_image?: string; // Inferred from common usage, though not explicitly in list model sometimes
  item_category?: string;
  item_detail?: string;
  is_multi_spec?: number | boolean;
  multi_quantity_delivery?: number | boolean;
  is_multi_qty_ship?: number | boolean;
  created_at?: string;
}

export type AutomationTriggerType = 'order_paid' | 'buyer_reviewed' | 'review_missing_timeout';
export type AutomationActionType = 'confirm_shipment' | 'send_card' | 'send_text';

// Rules
export interface ShippingRule {
  id: string;
  name: string;
  trigger_type: AutomationTriggerType;
  item_keyword: string; // Legacy UI helper
  cookie_id?: string;
  item_id?: string;
  item_title?: string;
  card_group_id: number; // First send_card action card id
  card_group_name?: string; // UI helper
  priority: number;
  enabled: boolean;
  config_json?: string;
  actions: AutomationAction[];
  variants: ShippingVariant[];
}

export interface AutomationAction {
  id?: string;
  action_type: AutomationActionType;
  card_id?: number;
  card_name?: string;
  delivery_count?: number;
  message_template?: string;
  delay_seconds?: number;
  config_json?: string;
  enabled: boolean;
  sort_order?: number;
}

export interface ShippingVariant {
  id?: string;
  spec_name: string;
  spec_value: string;
  card_id: number;
  card_name?: string;
  card_type?: Card['type'];
  delivery_count: number;
  enabled: boolean;
  delay_override?: boolean;
  delay_seconds?: number;
  config_json?: string;
}

export interface ReplyRule {
  id: string;
  keyword: string;
  reply_content: string;
  match_type: 'exact' | 'fuzzy';
  enabled: boolean;
  item_id?: string;
  type?: 'text' | 'image';
  image_url?: string;
}

// Stats
export interface AdminStats {
  total_users: number;
  total_cookies: number;
  active_cookies: number;
  total_cards: number;
  total_keywords: number;
  total_orders: number;
}

export interface DashboardStats {
  total_cookies: number;
  active_cookies: number;
  total_cards: number;
  total_keywords: number;
  total_orders: number;
  available_card_stock: number;
}

export interface OrderAnalytics {
  revenue_stats: {
    total_amount: number;
    total_orders: number;
  };
  daily_stats: Array<{ date: string; amount: number; order_count: number }>;
  item_stats?: Array<{
    item_id: string;
    order_count: number;
    total_amount: number;
    avg_amount: number;
  }>;
}

// Settings
export interface SystemSettings {
  ai_model?: string;
  ai_api_key?: string;
  ai_api_url?: string;
  ai_base_url?: string;
  default_reply?: string;
  registration_enabled?: boolean;
  smtp_server?: string;
  log_level?: 'debug' | 'info' | 'warn' | 'error' | string;
  log_format?: 'text' | 'json' | string;
  renewal_log_retention_days?: number;
  'captcha.remote_service_url'?: string;
  'captcha.remote_secret_key'?: string;
  'captcha.remote_pass_cookies'?: boolean | string;
  [key: string]: any;
}

export interface AIReplySettings {
  ai_enabled: boolean;
  model_name?: string;
  api_key?: string;
  base_url?: string;
  max_discount_percent: number;
  max_discount_amount?: number;
  max_bargain_rounds: number;
  custom_prompts: string;
}

// Default Reply
export interface DefaultReply {
  cookie_id: string;
  enabled: boolean;
  reply_content: string;
  reply_once: boolean;
  reply_image_url?: string;
}

// 通知渠道
export type NotificationChannelType = 'dingtalk' | 'feishu' | 'bark' | 'webhook' | 'wechat' | 'telegram' | 'email';
export type NotificationEventType =
  | 'account_offline'
  | 'account_recovered'
  | 'account_disabled'
  | 'security_verification'
  | 'token_renewal'
  | 'delivery_result'
  | 'system_error';

export interface NotificationChannel {
  id: string;
  name: string;
  type: NotificationChannelType;
  config: Record<string, unknown>;
  event_types?: NotificationEventType[];
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

/** 统一 HTTP 失败响应，客户端不再依赖 detail 或 msg 别名。 */
export interface ApiErrorResponse {
  /** 稳定的机器可读错误码。 */
  code: string;
  /** 可直接展示的错误说明。 */
  message: string;
  /** 可选的服务端请求追踪标识。 */
  request_id?: string;
  /** 仅供恢复或审计使用的结构化附加信息。 */
  details?: Record<string, unknown>;
}

/** 账号列表接口返回的非敏感具名 DTO。 */
export interface AccountSummaryResponse {
  /** 闲鱼账号稳定标识。 */
  id: string;
  /** 数据库中是否存在账号记录。 */
  has_cookie: boolean;
  /** 账号是否允许运行。 */
  enabled: boolean;
  /** 是否自动确认订单。 */
  auto_confirm: boolean;
  /** 账号备注。 */
  remark: string;
  /** 自动回复暂停时长，单位为分钟。 */
  pause_duration: number;
  /** 暂停结束 Unix 秒。 */
  paused_until: number;
  /** 当前是否仍处于暂停状态。 */
  paused: boolean;
  /** 密码登录用户名。 */
  username: string;
  /** 是否允许密码登录显示浏览器；兼容旧服务端的 0/1 字符串值。 */
  show_browser: boolean | number | string;
  /** 平台昵称缓存。 */
  nickname: string;
  /** 平台头像地址。 */
  avatar_url: string;
  /** 最近一次成功登录方式。 */
  login_method: string;
  /** 最近一次成功登录时间。 */
  last_login_at: number;
  /** 资料刷新错误说明。 */
  profile_error: string;
  /** 账号级 AI 回复开关。 */
  ai_enabled: boolean;
  /** 自动评价计划开关。 */
  auto_rate_enabled: boolean;
  /** 自动评价文案。 */
  rate_content: string;
  /** 自动擦亮计划开关。 */
  auto_polish_enabled: boolean;
  /** 自动擦亮本地时间。 */
  polish_time: string;
  /** 最近一次自动评价扫描时间。 */
  last_rate_scan_at: number;
  /** 最近一次自动擦亮日期。 */
  last_polish_date: string;
  /** 最近一次自动擦亮时间。 */
  last_polish_at: number;
}

/** 账号设置变更接口的具名成功响应。 */
export interface CookieSettingsResponse {
  /** 表示设置是否保存成功。 */
  success: boolean;
  /** 暂停结束 Unix 秒。 */
  paused_until: number;
  /** 表示账号当前是否暂停。 */
  paused: boolean;
}

/** 账号资料刷新接口的具名响应。 */
export interface CookieProfileResponse {
  /** 表示资料刷新是否成功。 */
  success: boolean;
  /** 账号稳定标识。 */
  id: string;
  /** 平台账号昵称。 */
  nickname: string;
  /** 平台账号头像地址。 */
  avatar_url: string;
  /** 资料刷新错误说明。 */
  profile_error: string;
}

/** 账号暂停时长查询接口的具名响应。 */
export interface PauseDurationResponse {
  /** 暂停时长，单位为分钟。 */
  pause_duration: number;
  /** 暂停结束 Unix 秒。 */
  paused_until: number;
  /** 表示账号当前是否暂停。 */
  paused: boolean;
}

/** 单个本地商品详情接口的具名响应。 */
export interface ItemDetailResponse {
  /** 商品所属账号标识。 */
  cookie_id: string;
  /** 平台商品标识。 */
  item_id: string;
  /** 商品标题。 */
  item_title: string;
  /** 商品描述。 */
  item_description: string;
  /** 商品分类标识。 */
  item_category: string;
  /** 商品价格文本。 */
  item_price: string;
  /** 商品详情原始 JSON。 */
  item_detail: string;
  /** 是否有多规格。 */
  is_multi_spec: boolean;
  /** 是否按数量发货。 */
  multi_quantity_delivery: boolean;
}

/** 商品发布接口的具名成功响应。 */
export interface ItemPublishResponse {
  /** 表示商品是否发布成功。 */
  success: boolean;
  /** 发布结果说明。 */
  message: string;
  /** 新商品的平台标识。 */
  item_id: string;
  /** 新商品的平台详情地址。 */
  item_url: string;
  /** 新商品主图地址。 */
  item_image: string;
  /** 新商品标题。 */
  item_title: string;
  /** 新商品价格文本。 */
  item_price: string;
  /** 新商品库存数量。 */
  quantity: number;
  /** 新商品分类标识。 */
  category_id: string;
  /** 新商品分类名称。 */
  category_name: string;
}

/** 商品全集同步接口的具名响应。 */
export interface ItemSyncResponse {
  /** 表示同步是否完成。 */
  success: boolean;
  /** 同步结果说明。 */
  message: string;
  /** 平台返回的商品总数。 */
  total_count: number;
  /** 平台商品总页数。 */
  total_pages: number;
  /** 本地保存的商品数量。 */
  saved_count: number;
  /** 本地删除标记的商品数量。 */
  deleted_count: number;
}

/** 商品分页同步接口的具名响应。 */
export interface ItemPageSyncResponse {
  /** 表示同步是否完成。 */
  success: boolean;
  /** 同步结果说明。 */
  message: string;
  /** 当前同步页码。 */
  page_number: number;
  /** 当前同步页大小。 */
  page_size: number;
  /** 当前页商品数量。 */
  current_count: number;
  /** 本地保存的商品数量。 */
  saved_count: number;
}

/** 订单详情接口返回的原始具名订单 DTO。 */
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
export interface OrderDetailResponse extends OrderDTOResponse {
  /** 表示查询是否完成。 */
  success: boolean;
  /** 新版客户端读取的订单对象。 */
  data: OrderDTOResponse;
}

/** 订单单条刷新返回的远端详情。 */
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
export interface OrderSingleRefreshResponse {
  /** 表示刷新是否完成。 */
  success: boolean;
  /** 刷新结果说明。 */
  message: string;
  /** 刷新后的订单详情。 */
  order: OrderRefreshDetailResponse;
}

/** 自动化规则动作的原始具名 DTO。 */
export interface AutomationActionResponse {
  /** 动作稳定标识。 */
  id: number;
  /** 动作类型。 */
  action_type: string;
  /** 关联卡券组标识。 */
  card_id: number;
  /** 关联卡券组名称。 */
  card_name: string;
  /** 发送数量。 */
  delivery_count: number;
  /** 消息模板。 */
  message_template: string;
  /** 延迟秒数。 */
  delay_seconds: number;
  /** 扩展配置 JSON。 */
  config_json: string;
  /** 是否启用。 */
  enabled: boolean;
  /** 执行顺序。 */
  sort_order: number;
}

/** 自动化规则的原始具名 DTO。 */
export interface AutomationRuleResponse {
  /** 规则稳定标识。 */
  id: number;
  /** 所属账号标识。 */
  cookie_id: string;
  /** 关联商品标识。 */
  item_id: string;
  /** 关联商品标题。 */
  item_title: string;
  /** 规则名称。 */
  name: string;
  /** 触发类型。 */
  trigger_type: string;
  /** 是否启用。 */
  enabled: boolean;
  /** 规则优先级。 */
  priority: number;
  /** 扩展配置 JSON。 */
  config_json: string;
  /** 规则动作列表。 */
  actions: AutomationActionResponse[];
  /** 创建时间。 */
  created_at: string;
  /** 更新时间。 */
  updated_at: string;
}

/** 自动化规则分页接口的具名响应。 */
export interface AutomationRulePageResponse {
  /** 表示查询是否完成。 */
  success: boolean;
  /** 当前页规则列表。 */
  data: AutomationRuleResponse[];
  /** 规则总数。 */
  total: number;
  /** 当前页码。 */
  page: number;
  /** 当前页大小。 */
  page_size: number;
  /** 总页数。 */
  total_pages: number;
  /** 各触发类型规则数量。 */
  trigger_counts: Record<string, number>;
}

/** 订单批量变更接口的具名响应。 */
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
export interface OrderBatchResult {
  /** 订单平台标识。 */
  order_id?: string;
  /** 表示该订单是否处理成功。 */
  success?: boolean;
  /** 该订单处理结果说明。 */
  message: string;
  /** 兼容接口可能返回的账号标识。 */
  cookie_id?: string;
  /** 兼容接口可能返回的处理阶段。 */
  stage?: string;
  /** 允许后端保留尚未结构化的扩展字段。 */
  [key: string]: unknown;
}
