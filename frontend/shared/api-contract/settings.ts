// settings 只公开 OpenAPI 生成的设置传输类型和跨 feature 共用的更新契约；表单状态属于各自 feature。
import type { components } from './generated/schema';

/** SystemSettingsTransport 表示受约束动态键的设置响应。 */
export type SystemSettingsTransport = components['schemas']['SystemSettingsResponse'];
/** AIModelsTransport 表示生成的模型发现响应。 */
export type AIModelsTransport = components['schemas']['AIModelsResponse'];

/** 敏感系统设置的显式三态变更命令。 */
export type SensitiveSettingChange = {
  /** 敏感值的保存策略。 */
  action: 'retain' | 'replace' | 'clear';
  /** replace 策略需要保存的新值，只存在于提交请求中。 */
  value?: string;
};

/** 系统设置更新请求，将普通配置与敏感命令分开传输。 */
export type SystemSettingsUpdate = {
  /** 非敏感配置字段。 */
  values?: Record<string, string | number | boolean>;
  /** 由服务端解释的敏感值变更命令。 */
  secrets?: Record<string, SensitiveSettingChange>;
};

/** SENSITIVE_SYSTEM_SETTING_KEYS 标识不得写入普通 values 的秘密键。 */
export const SENSITIVE_SYSTEM_SETTING_KEYS = new Set([
  'ai_api_key',
  'smtp_password',
  'qq_reply_secret_key',
  'captcha.remote_secret_key',
]);

/** 将各 feature 的设置草稿转换为服务端要求的普通值与敏感命令结构。 */
export const normalizeSystemSettingsUpdate = (settings: Record<string, string | number | boolean | undefined> | SystemSettingsUpdate): SystemSettingsUpdate => {
  if ('values' in settings || 'secrets' in settings) return settings as SystemSettingsUpdate;
  // values 保存普通设置，永不容纳秘密文本。
  const values: Record<string, string | number | boolean> = {};
  // secrets 保存服务端需要独立处理的敏感设置命令。
  const secrets: Record<string, SensitiveSettingChange> = {};
  for (const /* key、value 是当前待分流的设置键和值。 */ [key, value] of Object.entries(settings)) {
    if (value === undefined || value === null) continue;
    if (SENSITIVE_SYSTEM_SETTING_KEYS.has(key)) {
      secrets[key] = value === '' ? { action: 'clear' } : { action: 'replace', value: String(value) };
      continue;
    }
    if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') values[key] = value;
  }
  return Object.keys(secrets).length > 0 ? { values, secrets } : values;
};
