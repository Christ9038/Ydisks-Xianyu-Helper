// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 SystemSettings UI 模型；不直接暴露 HTTP DTO。 */
export interface SystemSettings {
  /** 默认 AI 模型名称。 */
  ai_model?: string;
  /** 全局 AI API 密钥。 */
  ai_api_key?: string;
  /** 全局 AI API 密钥是否已在服务端配置。 */
  ai_api_key_configured?: boolean;
  /** 全局 AI API 地址。 */
  ai_api_url?: string;
  /** 全局 AI 基础地址。 */
  ai_base_url?: string;
  /** 系统默认回复文案。 */
  default_reply?: string;
  /** 是否允许注册新用户。 */
  registration_enabled?: boolean;
  /** 系统 SMTP 服务器地址。 */
  smtp_server?: string;
  /** 系统 SMTP 密码是否已在服务端配置。 */
  smtp_password_configured?: boolean;
  /** 服务端日志级别。 */
  log_level?: 'debug' | 'info' | 'warn' | 'error' | string;
  /** 服务端日志格式。 */
  log_format?: 'text' | 'json' | string;
  /** 续期日志保留天数。 */
  renewal_log_retention_days?: number;
  /** 远程验证码服务地址。 */
  'captcha.remote_service_url'?: string;
  /** 远程验证码服务密钥。 */
  'captcha.remote_secret_key'?: string;
  /** 远程验证码服务密钥是否已在服务端配置。 */
  'captcha.remote_secret_key_configured'?: boolean;
  /** 远程验证码服务 Cookie 配置。 */
  'captcha.remote_pass_cookies'?: boolean | string;
  /** 兼容未来配置键的扩展字段。 */
  /** 未知设置键只能承载服务端声明的标量值，敏感值不进入该 UI 模型。 */
  [key: string]: string | number | boolean | undefined;
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

/** 通知渠道接口返回的原始具名 DTO。 */
/** 由当前 feature adapter 归一后的 NotificationChannelResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface NotificationChannelResponse {
  /** 通知渠道主键。 */
  id: number;
  /** 通知渠道名称。 */
  name: string;
  /** 通知渠道类型。 */
  type: string;
	/** 订阅事件类型 JSON 或兼容文本。 */
	event_types?: string;
  /** 通知渠道是否启用。 */
  enabled: boolean;
  /** 所属用户主键。 */
  user_id?: number;
}
