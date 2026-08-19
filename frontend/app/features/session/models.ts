// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 SessionResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface SessionResponse {
  /** 表示登录或初始化是否成功。 */
  success: boolean;
  /** 登录成功后的会话 Token。 */
  token?: string;
  /** 服务端返回的操作说明。 */
  message?: string;
  /** 当前用户 ID。 */
  user_id?: number;
  /** 当前用户名。 */
  username?: string;
  /** 当前用户是否为管理员。 */
  is_admin?: boolean;
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
