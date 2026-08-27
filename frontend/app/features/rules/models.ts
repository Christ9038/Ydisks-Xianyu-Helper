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

/** 由当前 feature adapter 归一后的 Card UI 模型；不直接暴露 HTTP DTO。 */
export interface Card {
  /** 卡券组数值主键。 */
  id: number;
  /** 卡券组名称。 */
  name: string;
  /** 卡券组类型。 */
  type: 'api' | 'text' | 'data' | 'image';
  /** 卡券组说明。 */
  description?: string;
  /** 卡券组是否启用。 */
  enabled: boolean;
  // 文本类型
  text_content?: string;
  // 批量数据类型
  data_content?: string;
  // API 类型配置
  api_config?: {
    /** API 卡券请求地址。 */
    url: string;
    /** API 卡券请求方法。 */
    method: 'GET' | 'POST';
    /** API 请求超时时间。 */
    timeout_seconds: number;
    /** API 响应提取路径。 */
    response_path?: string;
    /** 是否启用幂等重试。 */
    retry_enabled: boolean;
    /** 是否配置了请求头模板；具体值不会进入前端。 */
    headers_configured: boolean;
    /** 是否配置了请求参数模板；具体值不会进入前端。 */
    params_configured: boolean;
    /** 配置是否可用于规则发货。 */
    ready: boolean;
    /** 配置无效时的脱敏错误说明。 */
    validation_error?: string;
  };
  // 图片类型
  /** 图片卡券地址。 */
  image_url?: string;
  // 通用配置
  /** 卡券发送延迟秒数。 */
  delay_seconds?: number;
  // 多规格配置
  /** 是否支持多规格。 */
  is_multi_spec?: boolean;
  /** 规格名称。 */
  spec_name?: string;
  /** 规格值。 */
  spec_value?: string;
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

/** 由当前 feature adapter 归一后的 AutomationTriggerType UI 模型；不直接暴露 HTTP DTO。 */
export type AutomationTriggerType = 'order_created' | 'order_paid' | 'buyer_reviewed' | 'review_missing_timeout';
/** 由当前 feature adapter 归一后的 AutomationActionType UI 模型；不直接暴露 HTTP DTO。 */
export type AutomationActionType = 'confirm_shipment' | 'send_card' | 'send_template' | 'send_text' | 'adjust_price';

/** 发货模板中的一条顺序消息。 */
export interface DeliveryTemplateMessage {
  /** 消息主键。 */
  id: number;
  /** 消息在模板中的发送顺序。 */
  sort_order: number;
  /** 消息正文，可包含模板变量。 */
  content: string;
}

/** 当前用户可用于自动化规则的发货模板。 */
export interface DeliveryTemplate {
  /** 模板主键。 */
  id: number;
  /** 模板名称。 */
  name: string;
  /** 模板是否允许被自动化使用。 */
  enabled: boolean;
  /** 模板按顺序发送的消息。 */
  messages: DeliveryTemplateMessage[];
  /** 模板正文中出现的变量键。 */
  keys: string[];
  /** 模板正文中引用的自定义变量键。 */
  custom_keys?: string[];
  /** 创建时间。 */
  created_at: string;
  /** 最后更新时间。 */
  updated_at: string;
}

/** 发货模板变量与卡密库存的绑定。 */
export interface DeliveryTemplateBinding {
  /** 模板变量键。 */
  variable_key: string;
  /** 变量使用的卡密库存 ID。 */
  card_id: number;
  /** 每个订单为该变量取出的卡密数量。 */
  delivery_count: number;
}

// Rules
/** 由当前 feature adapter 归一后的 ShippingRule UI 模型；不直接暴露 HTTP DTO。 */
export interface ShippingRule {
  /** 规则标识。 */
  id: string;
  /** 规则名称。 */
  name: string;
  /** 规则触发类型。 */
  trigger_type: AutomationTriggerType;
  /** 规则匹配关键词。 */
  item_keyword: string; // Legacy UI helper
  /** 规则限定的账号标识。 */
  cookie_id?: string;
  /** 规则限定的商品标识。 */
  item_id?: string;
  /** 规则限定的商品标题。 */
  item_title?: string;
  /** 首个发卡动作使用的卡券组 ID。 */
  card_group_id: number; // First send_card action card id
  /** 首个发卡动作使用的卡券组名称。 */
  card_group_name?: string; // UI helper
  /** 规则优先级。 */
  priority: number;
	/** 规则是否启用。 */
	enabled: boolean;
	/** 规则多 SKU 迁移状态。 */
	sku_migration_status?: 'pending' | 'ready' | 'needs_reconfiguration';
  /** 规则原始配置 JSON。 */
  config_json?: string;
  /** 规则动作列表。 */
  actions: AutomationAction[];
  /** 规则规格变体列表。 */
  variants: ShippingVariant[];
}

/** 由当前 feature adapter 归一后的 AutomationAction UI 模型；不直接暴露 HTTP DTO。 */
export interface AutomationAction {
  /** 动作标识。 */
  id?: string;
  /** 动作类型。 */
  action_type: AutomationActionType;
  /** 动作使用的卡券组 ID。 */
  card_id?: number;
  /** 动作使用的卡券组名称。 */
  card_name?: string;
  /** 本次发放数量。 */
  delivery_count?: number;
  /** 文本消息模板。 */
  message_template?: string;
  /** 动作延迟秒数。 */
  delay_seconds?: number;
  /** 动作原始配置 JSON。 */
  config_json?: string;
  /** 动作是否启用。 */
  enabled: boolean;
  /** 动作排序序号。 */
  sort_order?: number;
  /** 动作使用的发货模板 ID。 */
  delivery_template_id?: number;
  /** 动作使用的发货模板名称。 */
  delivery_template_name?: string;
  /** 动作引用的模板变量键。 */
  template_keys?: string[];
  /** 动作的模板变量卡密绑定。 */
  template_bindings?: DeliveryTemplateBinding[];
  /** 传给发货模板的自定义字符串键值表。 */
  custom_variables?: Record<string, string>;
}

/** 由当前 feature adapter 归一后的 ShippingVariant UI 模型；不直接暴露 HTTP DTO。 */
export interface ShippingVariant {
  /** 规格变体标识。 */
  id?: string;
  /** 规格名称。 */
  spec_name: string;
  /** 规格值。 */
  spec_value: string;
  /** 变体使用的卡券组 ID。 */
  card_id: number;
  /** 变体使用的卡券组名称。 */
  card_name?: string;
  /** 卡券类型。 */
  card_type?: Card['type'];
  /** 变体发放数量。 */
  delivery_count: number;
  /** 变体是否启用。 */
  enabled: boolean;
  /** 是否覆盖动作级延迟。 */
  delay_override?: boolean;
  /** 变体延迟秒数。 */
  delay_seconds?: number;
  /** 变体原始配置 JSON。 */
  config_json?: string;
  /** 发货方式，默认为单卡密库存。 */
  delivery_mode?: 'card' | 'template';
  /** 变体使用的发货模板 ID。 */
  delivery_template_id?: number;
  /** 发货模板变量绑定。 */
  template_bindings?: DeliveryTemplateBinding[];
  /** 传给发货模板的自定义字符串键值表，按 custom.<键名> 对应。 */
  custom_variables?: Record<string, string>;
}

/** 由当前 feature adapter 归一后的 ReplyRule UI 模型；不直接暴露 HTTP DTO。 */
export interface ReplyRule {
  /** 回复规则标识。 */
  id: string;
  /** 触发关键词。 */
  keyword: string;
  /** 回复正文。 */
  reply_content: string;
  /** 关键词匹配方式。 */
  match_type: 'exact' | 'fuzzy';
  /** 规则是否启用。 */
  enabled: boolean;
  /** 规则限定的商品标识。 */
  item_id?: string;
  /** 回复类型。 */
  type?: 'text' | 'image';
  /** 图片回复地址。 */
  image_url?: string;
}

/** 由当前 feature adapter 归一后的 DefaultReply UI 模型；不直接暴露 HTTP DTO。 */
export interface DefaultReply {
  /** 账号稳定标识。 */
  cookie_id: string;
  /** 默认回复是否启用。 */
  enabled: boolean;
  /** 默认回复正文。 */
  reply_content: string;
  /** 是否对同一会话只回复一次。 */
  reply_once: boolean;
  /** 默认图片回复地址。 */
  reply_image_url?: string;
}

/** 自动化规则动作的原始具名 DTO。 */
/** 由当前 feature adapter 归一后的 AutomationActionResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 AutomationRuleResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 AutomationRulePageResponse UI 模型；不直接暴露 HTTP DTO。 */
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

/** 传统关键词列表项响应。 */
/** 由当前 feature adapter 归一后的 KeywordBasicResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface KeywordBasicResponse {
  /** 匹配关键词。 */
  keyword: string;
  /** 文字回复内容。 */
  reply: string;
}

/** 带商品范围的关键词列表项响应。 */
/** 由当前 feature adapter 归一后的 KeywordItemResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface KeywordItemResponse extends KeywordBasicResponse {
  /** 限定的商品标识。 */
  item_id: string;
}

/** 带类型和主键的关键词列表项响应。 */
/** 由当前 feature adapter 归一后的 KeywordTypedResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface KeywordTypedResponse extends KeywordItemResponse {
  /** 关键词规则主键。 */
  id: number;
  /** 回复类型。 */
  type: 'text' | 'image';
  /** 图片回复地址。 */
  image_url: string;
}

/** 指定商品回复项响应。 */
/** 由当前 feature adapter 归一后的 ItemReplyResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ItemReplyResponse {
  /** 商品平台标识。 */
  item_id?: string;
  /** 账号稳定标识。 */
  cookie_id?: string;
  /** 指定商品的回复内容。 */
  reply_content: string;
}

/** 默认回复查询响应。 */
/** 由当前 feature adapter 归一后的 DefaultReplyResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface DefaultReplyResponse extends DefaultReply {
  /** 账号稳定标识。 */
  cookie_id: string;
}

// AutomationIssuesEnvelope 是自动化异常接口的兼容响应。
/** 由当前 feature adapter 归一后的 AutomationIssuesEnvelope UI 模型；不直接暴露 HTTP DTO。 */
export interface AutomationIssuesEnvelope {
  /** runs 是待处理的自动化运行记录。 */
  runs?: Array<{
    /** 记录标识。 */
    id: number;
    /** 所属账号标识。 */
    cookie_id: string;
    /** 所属订单标识。 */
    order_id: string;
    /** 自动化触发类型。 */
    trigger_type: string;
    /** 外部错误说明。 */
    error_message: string;
    /** 异常类别。 */
    issue_kind: 'external_result_unknown' | 'invalid_snapshot' | 'rule_unavailable' | 'partial_failure' | 'execution_failed';
    /** 允许的处理动作。 */
    allowed_resolutions: Array<'continue' | 'retry' | 'cancel'>;
    /** 当前动作游标。 */
    action_cursor: number;
    /** 已发送数量。 */
    sent_count: number;
    /** 更新时间。 */
    updated_at: string;
  }>;
  /** pending_tasks 是延迟自动化任务列表。 */
  pending_tasks?: Array<{
    /** 任务标识。 */
    id: number;
    /** 所属账号标识。 */
    cookie_id: string;
    /** 自动化触发类型。 */
    trigger_type: string;
    /** 错误说明。 */
    error_message: string;
    /** 当前重试次数。 */
    attempt_count: number;
    /** 更新时间。 */
    updated_at: string;
  }>;
}
