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
  /** 已确认售出的卡密数量，来自服务端售出台账。 */
  sold_count?: number;
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

/** 仅在拥有者打开编辑弹窗时载入的完整配置，不进入列表缓存。 */
export interface CardAPIEditor extends Pick<CardAPIConfigSummary, 'url' | 'method' | 'content_type' | 'timeout_seconds' | 'response_path' | 'retry_enabled'> {
  /** 完整请求头模板，仅保留在当前编辑草稿。 */
  headers: string;
  /** 完整查询参数模板。 */
  params: string;
  /** 完整请求正文模板。 */
  body: string;
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
  /** 编辑 data 卡库存时打开弹窗所见的库存快照；服务端仅在库存未被自动发货改动时覆盖。 */
  expected_data_content?: string;
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

/** 售出记录状态：sold 为已确认发给买家，reserved 为已扣库存但发送结果待人工核对。 */
export type CardSaleStatus = 'sold' | 'reserved';

/** 由 adapter 归一后的卡密售出记录 UI 模型；content 为已发出的卡密明文，只在拥有者主动打开的弹窗内持有。 */
export interface CardSaleRecord {
  /** 售出记录主键。 */
  id: number;
  /** 已发出的卡密正文或 API 返回内容。 */
  content: string;
  /** 记录状态。 */
  status: CardSaleStatus;
  /** 售出时的卡密组名称快照。 */
  cardName: string;
  /** 订单所属闲鱼账号标识。 */
  cookieId: string;
  /** 平台订单号。 */
  orderId: string;
  /** 订单商品标识。 */
  itemId: string;
  /** 买家标识。 */
  buyerId: string;
  /** 确认售出的 Unix 毫秒时间；待核对记录回退为扣库存时间。 */
  soldAtMs: number;
}

/** 售出记录分页查询条件。 */
export interface CardSaleQuery {
  /** 状态筛选，空表示全部。 */
  status: CardSaleStatus | '';
  /** 订单号或卡密内容搜索词。 */
  search: string;
  /** 从 1 开始的页码。 */
  page: number;
  /** 每页数量。 */
  pageSize: number;
}

/** 一页售出记录及分页信息。 */
export interface CardSalePage {
  /** 当前页记录。 */
  records: CardSaleRecord[];
  /** 符合条件的总数。 */
  total: number;
  /** 总页数，至少为 1。 */
  totalPages: number;
}

/** 导出范围：unsold 为当前库存 TXT，sold 为售出记录 CSV。 */
export type CardExportScope = 'unsold' | 'sold';

/** 导出接口返回的可下载文件。 */
export interface CardExportFile {
  /** 文件内容。 */
  blob: Blob;
  /** 服务端建议的文件名。 */
  filename: string;
}
