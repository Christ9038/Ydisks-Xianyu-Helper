/** AI 服务默认兼容 API 地址。 */
export const DEFAULT_AI_API_URL = 'https://dashscope.aliyuncs.com/compatible-mode/v1';

/** 日志等级选择项。 */
export const LOG_LEVELS = [
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
];

/** 不应通过系统配置批量保存的兼容字段集合。 */
export const SETTINGS_SAVE_OMIT_KEYS = new Set([
  'smtp_server',
  'smtp_port',
  'smtp_user',
  'smtp_password',
  'smtp_from',
  'smtp_from_name',
  'smtp_from_address',
  'registration_enabled',
  'show_default_login_info',
  'login_captcha_enabled',
  'item_sync_enabled',
  'item_sync_interval',
  'item_sync_max_pages',
  'default_reply',
]);
