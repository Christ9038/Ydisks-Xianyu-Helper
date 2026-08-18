import type { OperationResponse,SessionResponse } from '../../../shared/api-contract/session';
import { get,post,type RequestControlOptions } from '../../../shared/http/client';
export type * from '../../../shared/api-contract/session';

/** 会话状态接口返回的只读传输契约。 */
export interface SessionStatusResponse {
  /** 当前浏览器会话是否已认证。 */
  authenticated: boolean;
  /** 服务是否已经完成首次管理员初始化。 */
  initialized?: boolean;
  /** 当前登录用户的数据库标识。 */
  user_id?: number;
  /** 当前登录用户的名称。 */
  username?: string;
  /** 当前登录用户是否具备管理员权限。 */
  is_admin?: boolean;
}

/** 登录请求的具名传输 DTO。 */
export interface LoginRequest {
  /** 用户名登录方式使用的账号名称。 */
  username?: string;
  /** 用户名登录方式使用的密码，仅在本次请求体中存在。 */
  password?: string;
  /** 邮箱验证码登录方式使用的邮箱地址。 */
  email?: string;
  /** 邮箱验证码登录方式使用的一次性验证码。 */
  verification_code?: string;
}

/** 以用户名密码或邮箱验证码建立管理会话。 */
export const login = async (data: LoginRequest): Promise<SessionResponse> => post('/api/v1/session/login', data, { skipAuthLogout: true });

/** 用首次设置的管理员密码完成系统初始化并建立会话。 */
export const initializeAdmin = async (password: string): Promise<SessionResponse> => post('/api/v1/session/initialize', { password }, { skipAuthLogout: true });

/** 读取当前浏览器的认证与初始化状态，调用者可取消页面卸载前的请求。 */
export const verifySession = async (options?: RequestControlOptions): Promise<SessionStatusResponse> => get('/api/v1/session', undefined, options);

/** 结束当前浏览器会话。 */
export const logout = async (): Promise<OperationResponse> => post('/api/v1/session/logout', {});
