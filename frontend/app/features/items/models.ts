// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 PublishLocation UI 模型；不直接暴露 HTTP DTO。 */
export interface PublishLocation {
  /** 行政区划展示名称。 */
  area: string;
  /** 城市展示名称。 */
  city: string;
  /** 平台使用的行政区划标识。 */
  division_id: string;
  /** 地点经度。 */
  longitude: number;
  /** 地点纬度。 */
  latitude: number;
  /** 平台兴趣点标识。 */
  poi_id: string;
  /** 平台兴趣点展示名称。 */
  poi_name: string;
  /** 省份展示名称。 */
  province: string;
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
export type AutomationTriggerType = 'order_paid' | 'buyer_reviewed' | 'review_missing_timeout';
/** 由当前 feature adapter 归一后的 AutomationActionType UI 模型；不直接暴露 HTTP DTO。 */
export type AutomationActionType = 'confirm_shipment' | 'send_card' | 'send_text';

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
  card_type?: 'api' | 'text' | 'data' | 'image';
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
}

/** 单个本地商品详情接口的具名响应。 */
/** 由当前 feature adapter 归一后的 ItemDetailResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 ItemPublishResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 ItemSyncResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 ItemPageSyncResponse UI 模型；不直接暴露 HTTP DTO。 */
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

/** 商品类目推荐接口的具名响应。 */
/** 由当前 feature adapter 归一后的 CategoryRecommendationResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface CategoryRecommendationResponse {
  /** 类目推荐是否成功。 */
  success: boolean;
  /** 推荐商品类目。 */
  category: {
    /** 平台类目主键。 */
    cat_id: string;
    /** 平台类目名称。 */
    cat_name: string;
    /** 频道类目主键。 */
    channel_cat_id: string;
    /** 淘宝类目主键。 */
    tb_cat_id?: string;
  };
}

/** 商品批量发布预检逐行结果。 */
/** 由当前 feature adapter 归一后的 ItemPublishBatchPreviewRow UI 模型；不直接暴露 HTTP DTO。 */
export interface ItemPublishBatchPreviewRow {
  /** 上传表格行号。 */
  row_no: number;
  /** 当前行是否通过预检。 */
  valid: boolean;
  /** 当前行校验错误列表。 */
  errors?: string[];
  /** 发布目标账号标识。 */
  cookie_id: string;
  /** 商品标题。 */
  title: string;
  /** 商品价格文本。 */
  price: string;
  /** 商品库存数量。 */
  quantity: number;
  /** 商品图片引用列表。 */
  images: string[];
  /** 商品发布类目。 */
  category: CategoryRecommendationResponse['category'];
  /** 发布后自动化配置。 */
  automation?: Record<string, unknown>;
}

/** 商品批量发布预检响应。 */
/** 由当前 feature adapter 归一后的 ItemPublishBatchPreviewResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ItemPublishBatchPreviewResponse {
  /** 预检流程是否完成。 */
  success: boolean;
  /** 后续启动发布使用的预检批次标识。 */
  preview_id: string;
  /** 预检总行数。 */
  total: number;
  /** 通过预检行数。 */
  valid: number;
  /** 未通过预检行数。 */
  invalid: number;
  /** 逐行预检结果。 */
  rows: ItemPublishBatchPreviewRow[];
}

/** 商品批量发布任务启动或重试响应。 */
/** 由当前 feature adapter 归一后的 BatchIDResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface BatchIDResponse {
  /** 任务操作是否完成。 */
  success: boolean;
  /** 商品批量任务标识。 */
  batch_id: string;
}

/** 商品批量发布任务取消响应。 */
/** 由当前 feature adapter 归一后的 BatchCancelResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface BatchCancelResponse {
  /** 取消请求是否完成。 */
  success: boolean;
  /** 取消后的任务状态。 */
  status: string;
}

/** 商品批量发布任务逐行详情。 */
/** 由当前 feature adapter 归一后的 ItemPublishBatchRowResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ItemPublishBatchRowResponse {
  /** 明细行主键。 */
  id: number;
  /** 导入表格行号。 */
  row_no: number;
  /** 发布目标账号标识。 */
  cookie_id: string;
  /** 商品标题。 */
  title: string;
  /** 商品价格文本。 */
  price: string;
  /** 商品库存数量。 */
  quantity: number;
  /** 商品图片引用列表。 */
  images: string[];
  /** 商品发布类目。 */
  category: CategoryRecommendationResponse['category'];
  /** 发布后自动化配置。 */
  automation: Record<string, unknown>;
  /** 明细行状态。 */
  status: string;
  /** 发布成功后的平台商品标识。 */
  item_id: string;
  /** 发布成功后的商品地址。 */
  item_url: string;
  /** 明细行失败原因。 */
  error_message: string;
  /** 明细行失败类型。 */
  failure_kind: string;
}

/** 商品批量发布任务详情响应。 */
/** 由当前 feature adapter 归一后的 ItemPublishBatchResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ItemPublishBatchResponse {
  /** 批量任务标识。 */
  id: string;
  /** 批量任务状态。 */
  status: string;
  /** 原始上传文件名。 */
  filename: string;
  /** 明细行总数。 */
  total: number;
  /** 成功发布数量。 */
  success: number;
  /** 失败数量。 */
  failed: number;
  /** 待处理数量。 */
  pending: number;
  /** 运行中数量。 */
  running: number;
  /** 可重试数量。 */
  retryable: number;
  /** 明细行结果。 */
  rows: ItemPublishBatchRowResponse[];
  /** 批次统一发货地。 */
  location?: Record<string, unknown>;
  /** 最终商品发布之间的最小间隔秒数。 */
  publish_interval_seconds?: number;
  /** 创建时间。 */
  created_at: string;
  /** 更新时间。 */
  updated_at: string;
}

/** 商品批量发布任务列表响应。 */
/** 由当前 feature adapter 归一后的 ItemPublishBatchListResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface ItemPublishBatchListResponse {
  /** 当前用户的批量任务列表。 */
  batches: ItemPublishBatchResponse[];
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
