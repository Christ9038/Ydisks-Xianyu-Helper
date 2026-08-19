// session 只公开 OpenAPI 生成的会话传输类型；页面会话状态属于 session feature。
import type { components } from './generated/schema';

/** SessionTransport 表示生成的登录或初始化响应。 */
export type SessionTransport = components['schemas']['SessionResponse'];
