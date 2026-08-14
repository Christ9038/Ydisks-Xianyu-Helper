
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
