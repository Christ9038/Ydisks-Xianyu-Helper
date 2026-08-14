import type { SystemSettings } from '../../../types';
import { SETTINGS_SAVE_OMIT_KEYS } from './constants';
import type { CredentialsForm, CredentialsMessage } from './types';

/** 将配置草稿裁剪为可以保存到后端的字段。 */
export const buildPersistableSettings = (settings: SystemSettings): Partial<SystemSettings> => (
  Object.fromEntries(
    Object.entries(settings).filter(
      // entry 是配置键值对，过滤掉兼容字段和空值。
      ([key, value]) => !SETTINGS_SAVE_OMIT_KEYS.has(key) && value !== undefined && value !== null,
    ),
  ) as Partial<SystemSettings>
);

/** 校验登录凭据表单并返回用户可见错误。 */
export const validateCredentials = (credentials: CredentialsForm): string => {
  // username 是去除首尾空白后的登录用户名。
  const username = credentials.new_username.trim();
  if (username.length < 3) return '用户名至少需要 3 个字符';
  if (!credentials.current_password) return '请输入当前密码确认身份';
  if (credentials.new_password && credentials.new_password.length < 8) return '新密码至少需要 8 个字符';
  if (credentials.new_password !== credentials.confirm_password) return '两次输入的新密码不一致';
  return '';
};

/** 判断设置请求响应是否仍可写入当前页面。 */
export const isCurrentSettingsRequest = (currentSequence: number, requestSequence: number, signal: AbortSignal): boolean => (
  currentSequence === requestSequence && !signal.aborted
);

/** 判断错误是否来自主动取消。 */
export const isSettingsAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 统一提取设置请求错误文本。 */
export const settingsErrorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 创建登录凭据的初始表单。 */
export const createCredentials = (username = ''): CredentialsForm => ({
  new_username: username,
  current_password: '',
  new_password: '',
  confirm_password: '',
});

/** 创建凭据保存结果提示。 */
export const createCredentialsMessage = (type: 'success' | 'error', text: string): CredentialsMessage => ({ type, text });
