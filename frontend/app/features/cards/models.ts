// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
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
  // API 类型配置只保存服务端返回的脱敏摘要。
  api_config?: CardAPIConfigSummary;
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

/** API 卡券查询返回的脱敏摘要；请求头和参数永远不在此模型中。 */
export interface CardAPIConfigSummary {
  /** API 卡券请求地址。 */
  url: string;
  /** API 卡券请求方法。 */
  method: 'GET' | 'POST';
  /** API 请求正文的 Content-Type。 */
  content_type: string;
  /** API 请求超时时间。 */
  timeout_seconds: number;
  /** API 响应提取路径。 */
  response_path?: string;
  /** 是否启用幂等重试。 */
  retry_enabled: boolean;
  /** 是否已配置请求头模板。 */
  headers_configured: boolean;
  /** 是否已配置请求参数模板。 */
  params_configured: boolean;
  /** 当前配置是否可以被规则使用。 */
  ready: boolean;
  /** 当前配置的非敏感校验错误。 */
  validation_error?: string;
}

/** API 卡券提交用的具名配置；敏感模板为空时由服务端保留旧值。 */
export interface CardAPIConfigInput {
  /** API 卡券请求地址。 */
  url: string;
  /** API 卡券请求方法。 */
  method: 'GET' | 'POST';
  /** API 请求超时时间。 */
  timeout_seconds: number;
  /** API 请求头 JSON 文本或对象。 */
  headers?: string | Record<string, unknown>;
  /** API 请求参数 JSON 文本或对象。 */
  params?: string | Record<string, unknown>;
  /** API 请求正文 JSON 文本或对象。 */
  body?: string | Record<string, unknown>;
  /** API 请求正文的 Content-Type。 */
  content_type?: string;
  /** 请求头的三态变更意图。 */
  headers_action?: 'retain' | 'replace' | 'clear';
  /** 请求参数的三态变更意图。 */
  params_action?: 'retain' | 'replace' | 'clear';
  /** API 响应提取路径。 */
  response_path?: string;
  /** 是否启用幂等重试。 */
  retry_enabled?: boolean;
}

/** API 测试请求返回的非敏感诊断结果。 */
export interface CardAPITestResult {
  /** 远端请求是否返回 2xx。 */
  status: 'success' | 'failed';
  /** 远端 HTTP 状态码。 */
  status_code: number;
  /** 远端响应媒体类型。 */
  response_content_type: string;
  /** JSON 响应顶层字段名称。 */
  response_fields: string[];
  /** 响应提取路径命中的值。 */
  extracted_value?: string;
  /** 限长响应预览。 */
  response_preview?: string;
}

/** 卡券创建和更新所需的前端提交模型。 */
export type CardMutation = Omit<Partial<Card>, 'api_config'> & {
  /** API 卡券具名请求配置或历史 JSON 字符串。 */
  api_config?: CardAPIConfigInput | string;
};

/** 卡券批量创建接口的逐行结果。 */
/** 由当前 feature adapter 归一后的 CardBatchResult UI 模型；不直接暴露 HTTP DTO。 */
export interface CardBatchResult {
  /** 表格中的原始行号。 */
  row_no: number;
  /** 当前行是否创建成功。 */
  success: boolean;
  /** 新建卡券组主键。 */
  id?: number;
  /** 卡券组名称。 */
  name: string;
  /** 卡券类型。 */
  type?: string;
  /** 当前行失败原因。 */
  error?: string;
}

/** 卡券批量创建接口的具名响应。 */
/** 由当前 feature adapter 归一后的 CardBatchResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface CardBatchResponse {
  /** 批量处理流程是否完成。 */
  success: boolean;
  /** 解析出的总行数。 */
  total: number;
  /** 创建成功行数。 */
  created: number;
  /** 创建失败行数。 */
  failed: number;
  /** 逐行处理结果。 */
  rows: CardBatchResult[];
}

/** 卡券追加数据接口的具名响应。 */
/** 由当前 feature adapter 归一后的 CardAppendResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface CardAppendResponse {
  /** 追加操作是否完成。 */
  success: boolean;
  /** 实际追加数量。 */
  added: number;
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
