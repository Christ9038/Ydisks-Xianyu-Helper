// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
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

/** 由当前 feature adapter 归一后的 AccountTaskSettings UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskSettings {
	/** 账号稳定标识。 */
	account_id: string;
	/** 是否启用自动评价。 */
	auto_rate_enabled: boolean;
	/** 自动评价文案。 */
	rate_content: string;
	/** 是否启用每日擦亮。 */
	auto_polish_enabled: boolean;
	/** 每日擦亮执行时间。 */
	polish_time: string;
	/** 最近一次自动评价扫描时间。 */
	last_rate_scan_at?: number;
	/** 最近一次擦亮日期。 */
	last_polish_date?: string;
	/** 最近一次擦亮时间。 */
	last_polish_at?: number;
}

/** 由当前 feature adapter 归一后的 AccountTaskSummary UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskSummary {
	/** 任务类型。 */
	task_type: 'auto_rate' | 'auto_polish';
	/** 发现的任务数量。 */
	found: number;
	/** 成功处理的任务数量。 */
	success: number;
	/** 处理失败的任务数量。 */
	failed: number;
	/** 跳过的任务数量。 */
	skipped: number;
	/** 批量处理结果说明。 */
	message?: string;
}

/** 由当前 feature adapter 归一后的 AIReplySettings UI 模型；不直接暴露 HTTP DTO。 */
export interface AIReplySettings {
  /** 是否启用账号 AI 回复。 */
  ai_enabled: boolean;
  /** 最大折扣比例。 */
  max_discount_percent: number;
  /** 最大折扣金额。 */
  max_discount_amount?: number;
  /** 最大砍价轮次。 */
  max_bargain_rounds: number;
  /** 自定义提示词。 */
  custom_prompts: string;
}

/** 由当前 feature adapter 归一后的 NotificationChannelType UI 模型；不直接暴露 HTTP DTO。 */
export type NotificationChannelType = 'dingtalk' | 'feishu' | 'bark' | 'webhook' | 'wechat' | 'telegram' | 'email';
/** 由当前 feature adapter 归一后的 NotificationEventType UI 模型；不直接暴露 HTTP DTO。 */
export type NotificationEventType =
  | 'account_offline'
  | 'account_recovered'
  | 'account_disabled'
  | 'security_verification'
  | 'token_renewal'
  | 'delivery_result'
  | 'system_error';

/** 由当前 feature adapter 归一后的 NotificationChannel UI 模型；不直接暴露 HTTP DTO。 */
export interface NotificationChannel {
  /** 通知渠道标识。 */
  id: string;
  /** 通知渠道名称。 */
  name: string;
  /** 通知渠道类型。 */
  type: NotificationChannelType;
  /** 通知渠道配置。 */
  config: Record<string, unknown>;
  /** 渠道绑定的事件类型。 */
  event_types?: NotificationEventType[];
  /** 通知渠道是否启用。 */
  enabled: boolean;
  /** 创建时间。 */
  created_at?: string;
  /** 更新时间。 */
  updated_at?: string;
}

/** 账号编辑器读取的通知渠道传输结果，仅用于适配为 NotificationChannel UI 模型。 */
export interface NotificationChannelResponse {
  /** 通知渠道数值标识。 */ id: number;
  /** 用户可见渠道名称。 */ name: string;
  /** 服务端保存的渠道类型。 */ type: string;
  /** 兼容 JSON 文本的事件订阅列表。 */ event_types?: string;
  /** 渠道是否参与投递。 */ enabled: boolean;
}

/** 账号列表接口返回的非敏感具名 DTO。 */
/** 由当前 feature adapter 归一后的 AccountSummaryResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 CookieSettingsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface CookieSettingsResponse {
  /** 表示设置是否保存成功。 */
  success: boolean;
  /** 暂停结束 Unix 秒。 */
  paused_until: number;
  /** 表示账号当前是否暂停。 */
  paused: boolean;
}

/** 账号资料刷新接口的具名响应。 */
/** 由当前 feature adapter 归一后的 CookieProfileResponse UI 模型；不直接暴露 HTTP DTO。 */
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
/** 由当前 feature adapter 归一后的 PauseDurationResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface PauseDurationResponse {
  /** 暂停时长，单位为分钟。 */
  pause_duration: number;
  /** 暂停结束 Unix 秒。 */
  paused_until: number;
  /** 表示账号当前是否暂停。 */
  paused: boolean;
}

/** 账号 AI 回复设置接口的具名响应。 */
/** 由当前 feature adapter 归一后的 AIReplySettingsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AIReplySettingsResponse {
  /** 账号稳定标识；默认配置响应可能省略。 */
  cookie_id?: string;
  /** AI 回复是否启用。 */
  ai_enabled: boolean;
  /** 最大折扣比例。 */
  max_discount_percent: number;
  /** 最大折扣金额。 */
  max_discount_amount: number;
  /** 最大砍价轮次。 */
  max_bargain_rounds: number;
  /** 自定义提示词。 */
  custom_prompts: string;
}

/** 通知绑定列表中的单条记录。 */
/** 由当前 feature adapter 归一后的 NotificationBinding UI 模型；不直接暴露 HTTP DTO。 */
export interface NotificationBinding {
  /** 账号稳定标识，列表归一化后补充。 */
  cookie_id?: string;
  /** 绑定记录主键。 */
  id?: number;
  /** 通知渠道主键。 */
  channel_id: number;
  /** 通知渠道名称。 */
  channel_name: string;
  /** 绑定是否启用。 */
  enabled: boolean;
}

/** 账号通知渠道绑定查询响应。 */
/** 由当前 feature adapter 归一后的 AccountBindingsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountBindingsResponse {
  /** 账号稳定标识。 */
  cookie_id: string;
  /** 已绑定通知渠道主键列表。 */
  channel_ids: number[];
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

/** 账号任务设置响应。 */
/** 由当前 feature adapter 归一后的 AccountTaskSettingsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskSettingsResponse {
  /** 账号稳定标识。 */
  account_id: string;
  /** 是否启用自动评价。 */
  auto_rate_enabled: boolean;
  /** 自动评价文案。 */
  rate_content: string;
  /** 是否启用自动擦亮。 */
  auto_polish_enabled: boolean;
  /** 自动擦亮本地时间。 */
  polish_time: string;
  /** 最近一次评价扫描时间。 */
  last_rate_scan_at: number;
  /** 最近一次擦亮日期。 */
  last_polish_date: string;
  /** 最近一次擦亮时间。 */
  last_polish_at: number;
}

/** 账号任务执行记录响应。 */
/** 由当前 feature adapter 归一后的 AccountTaskRunResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskRunResponse {
  /** 任务执行记录主键。 */
  id: number;
  /** 任务幂等键。 */
  run_key: string;
  /** 账号稳定标识。 */
  account_id: string;
  /** 任务类型。 */
  task_type: string;
  /** 任务目标标识。 */
  target_id: string;
  /** 任务业务日期。 */
  run_date: string;
  /** 任务执行状态。 */
  status: string;
  /** 任务成功数量。 */
  success_count: number;
  /** 任务失败数量。 */
  failed_count: number;
  /** 任务失败说明。 */
  error_message: string;
  /** 下一次重试时间。 */
  next_retry_at: number;
  /** 任务开始时间。 */
  started_at: number;
  /** 任务完成时间。 */
  finished_at: number;
}

/** 账号任务执行记录列表响应。 */
/** 由当前 feature adapter 归一后的 AccountTaskRunsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskRunsResponse {
  /** 当前账号的任务执行记录。 */
  runs: AccountTaskRunResponse[];
}

/** 手动执行账号任务的统计响应。 */
/** 由当前 feature adapter 归一后的 AccountTaskSummaryResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskSummaryResponse extends AccountTaskSummary {
  /** 任务结果说明。 */
  message?: string;
}

/** 手动执行账号任务的成功响应。 */
/** 由当前 feature adapter 归一后的 AccountTaskRunResponseEnvelope UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountTaskRunResponseEnvelope {
  /** 任务请求是否成功完成。 */
  success: boolean;
  /** 账号任务执行统计。 */
  summary: AccountTaskSummaryResponse;
}

/** 扫码登录二维码生成响应。 */
/** 由当前 feature adapter 归一后的 QRLoginGenerateResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface QRLoginGenerateResponse {
  /** 二维码是否生成成功。 */
  success: boolean;
  /** 扫码登录会话标识。 */
  session_id: string;
  /** 二维码图片地址。 */
  qr_code_url: string;
  /** 可选的提示文本。 */
  message?: string;
}

/** 二维码登录状态响应。 */
/** 由当前 feature adapter 归一后的 QRLoginStatusResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface QRLoginStatusResponse {
  /** 当前二维码会话状态。 */
  status: string;
  /** 风控验证页面截图地址，在人脸二维码不可用时作为展示兜底。 */
  verification_screenshot?: string;
  /** 闲鱼人脸风控验证二维码地址，验证页面优先展示。 */
  face_qr_url?: string;
  /** 扫码登录会话标识。 */
  session_id?: string;
	/** 持久化后的本地账号标识。 */
  account_id?: string;
  /** 是否新建了本地账号。 */
  is_new_account?: boolean;
  /** 状态提示文本。 */
  message?: string;
  /** 兼容上游可能扩展的非敏感状态字段。 */
  [key: string]: unknown;
}

/** 二维码验证完成响应。 */
/** 由当前 feature adapter 归一后的 QRLoginVerificationResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface QRLoginVerificationResponse {
  /** 验证结果是否成功。 */
  success: boolean;
  /** 平台账号标识。 */
  unb?: string;
  /** 持久化后的本地账号标识。 */
  account_id?: string;
  /** 是否新建了本地账号。 */
  is_new_account?: boolean;
}
