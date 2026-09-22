import type { OperationResponse,SystemSettings } from './models';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { normalizeSystemSettingsUpdate,SENSITIVE_SYSTEM_SETTING_KEYS,type SystemUpdateTransport } from '../../../shared/api-contract/settings';
import { type RequestControlOptions } from '../../../shared/http/client';
import type { SystemSettingsUpdate } from '../../../shared/api-contract/settings';
export type * from './models';
export { normalizeSystemSettingsUpdate } from '../../../shared/api-contract/settings';
export type { SensitiveSettingChange,SystemSettingsUpdate } from '../../../shared/api-contract/settings';

/** SystemUpdateSnapshot 表示管理员设置页消费的 Docker 正式版更新状态。 */
export type SystemUpdateSnapshot = SystemUpdateTransport;

/** 设置页面使用的会话状态传输契约，避免跨 feature 依赖。 */
export interface SettingsSessionStatusResponse {
  /** 当前浏览器会话是否已认证。 */
  authenticated: boolean;
  /** 服务是否已经完成首次管理员初始化。 */
  initialized?: boolean;
  /** 当前用户的数据库标识。 */
  user_id?: number;
  /** 当前用户的登录名称。 */
  username?: string;
  /** 当前用户是否具有管理员权限。 */
  is_admin?: boolean;
}

/** 将服务端可兼容的字符串和数值设置归一为 UI 可消费的只读契约。 */
const normalizeSettings = (settings: Record<string, unknown>): SystemSettings => {
  // result 是不修改原始响应的设置副本。
  const result: Record<string, unknown> = { ...settings };
  // sensitiveKey 是当前需要转为布尔状态的敏感键。
  for (const /* sensitiveKey 是当前需要读取配置状态的敏感设置键。 */ sensitiveKey of SENSITIVE_SYSTEM_SETTING_KEYS) {
    // configuredKey 是敏感键对应的配置状态字段。
    const configuredKey = `${sensitiveKey}_configured`;
    if (configuredKey in result) result[configuredKey] = result[configuredKey] === true || result[configuredKey] === 'true';
  }
  if ('renewal_log_retention_days' in result) {
    // days 是完成数值转换后的日志保留天数。
    const days = Number(result.renewal_log_retention_days);
    result.renewal_log_retention_days = Number.isFinite(days) ? days : 10;
  }
  if ('outbound_http_public_only' in result) {
    // publicOnly 保存统一出站策略的布尔状态，兼容旧服务端返回的字符串。
    result.outbound_http_public_only = result.outbound_http_public_only === true || result.outbound_http_public_only === 'true';
  }
  return result as SystemSettings;
};

/** 修改当前管理员的密码。 */
export const changePassword = async (currentPassword: string, newPassword: string): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次管理员密码更新请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/session/password', { body: { current_password: currentPassword, new_password: newPassword }, signal }));

/** 修改当前管理员的登录名称与可选密码。 */
export const updateLoginCredentials = async (data: { /** 当前密码用于服务端重新验证身份。 */ current_password: string; /** 新的管理员名称。 */ new_username: string; /** 可选的新密码。 */ new_password?: string }, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次管理员凭据更新请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/session/credentials', { body: data, signal }), options);

/** 获取系统设置；仅保留敏感值是否已配置的状态，不接收敏感明文。 */
export const getSystemSettings = async (options?: RequestControlOptions): Promise<SystemSettings> => {
  // response 是 OpenAPI 约束的脱敏系统设置键值对象。
  const response = await runContractRequest(/* signal 是本次系统设置读取请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/settings/system', { signal }), options);
  return normalizeSettings(response);
};

/** 保存普通系统配置和敏感设置命令。 */
export const updateSystemSettings = async (settings: Partial<SystemSettings> | SystemSettingsUpdate, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次系统设置更新请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/settings/system', {
    body: normalizeSystemSettingsUpdate(settings),
    signal,
  }), options);

/** 向服务端请求指定人工智能服务的可用模型列表。 */
export const fetchAIModels = async (baseURL: string, apiKey = '', options?: RequestControlOptions): Promise<string[]> => {
  // response 是模型发现接口的具名响应。
  const response = await runContractRequest(/* signal 是本次模型发现请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/settings/ai-models', {
    body: { base_url: baseURL, api_key: apiKey },
    signal,
  }), options);
  return Array.isArray(response.models) ? response.models : [];
};

/** AI 连接测试结果。 */
export interface AIConnectionTestResult {
  /** 测试是否成功。 */
  success: boolean;
  /** 实际被测试的模型名称。 */
  model: string;
  /** 请求耗时毫秒数。 */
  latency_ms: number;
  /** 模型回复摘要。 */
  reply: string;
}

/** 发送一次最小对话请求验证 AI API 地址、密钥和模型的组合是否可用。 */
export const testAIConnection = async (baseURL: string, apiKey: string, model: string, options?: RequestControlOptions): Promise<AIConnectionTestResult> => {
  // response 是由版本化 OpenAPI operation 校验后的非敏感连接诊断结果。
  const response = await runContractRequest(/* signal 是本次长时连接测试的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/settings/ai-test', {
    body: { base_url: baseURL, api_key: apiKey, model },
    signal,
  }), { timeoutMs: 60_000, ...options });
  return response;
};

/** 在保存登录凭据前读取当前会话状态。 */
export const verifySession = async (options?: RequestControlOptions): Promise<SettingsSessionStatusResponse> =>
  runContractRequest(/* signal 是本次设置页会话校验请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/session', { signal }), options);

/** 读取当前构建、最新正式版和宿主机更新能力。 */
export const getSystemUpdateStatus = async (options?: RequestControlOptions): Promise<SystemUpdateSnapshot> =>
  runContractRequest(/* signal 是本次更新状态查询的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/admin/system/update', { signal }), options);

/** 请求宿主机执行器重新检查最新正式版本。 */
export const checkSystemUpdate = async (options?: RequestControlOptions): Promise<SystemUpdateSnapshot> =>
  runContractRequest(/* signal 是本次正式版检查的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/admin/system/update/check', { signal }), options);

/** 请求更新到执行器重新验证的最新正式版；调用方不能指定镜像、版本或宿主机路径。 */
export const applyLatestStableUpdate = async (options?: RequestControlOptions): Promise<SystemUpdateSnapshot> =>
  runContractRequest(/* signal 是本次更新任务受理请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/admin/system/update/apply', { signal }), { timeoutMs: 60_000, ...options });
