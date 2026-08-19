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

/** AI 模型发现接口的具名响应。 */
/** 由当前 feature adapter 归一后的 AIModelsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AIModelsResponse {
  /** 远端可用模型名称。 */
  models: string[];
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
