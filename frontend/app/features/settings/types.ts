import type { SystemSettings } from '../../../types';

/** 登录凭据编辑表单。 */
export type CredentialsForm = {
  /** 新登录用户名。 */
  new_username: string;
  /** 当前密码。 */
  current_password: string;
  /** 新密码。 */
  new_password: string;
  /** 确认新密码。 */
  confirm_password: string;
};

/** 登录凭据操作提示。 */
export type CredentialsMessage = {
  /** 提示类型。 */
  type: 'success' | 'error';
  /** 提示文本。 */
  text: string;
} | null;

/** Settings feature 暴露的请求状态。 */
export type SettingsRequestStatus = 'idle' | 'loading' | 'success' | 'error';

/** Settings feature 的统一状态。 */
export type SettingsFeatureState = {
  /** 当前系统配置草稿。 */
  settings: SystemSettings | null;
  /** 系统配置加载状态。 */
  loading: boolean;
  /** 系统配置加载错误。 */
  loadError: string;
  /** 系统配置保存状态。 */
  saving: boolean;
  /** 系统配置保存错误。 */
  saveError: string;
  /** 可选 AI 模型列表。 */
  aiModels: string[];
  /** AI 模型列表加载状态。 */
  modelsLoading: boolean;
  /** AI 模型列表错误。 */
  modelError: string;
  /** 模型下拉框是否展开。 */
  modelDropdownOpen: boolean;
  /** API Key 是否明文显示。 */
  showApiKey: boolean;
  /** 远程验证秘钥是否明文显示。 */
  showCaptchaSecret: boolean;
  /** 当前密码是否明文显示。 */
  showCurrentPassword: boolean;
  /** 新密码是否明文显示。 */
  showNewPassword: boolean;
  /** 登录凭据保存状态。 */
  credentialsSaving: boolean;
  /** 登录凭据操作提示。 */
  credentialsMessage: CredentialsMessage;
  /** 登录凭据草稿。 */
  credentials: CredentialsForm;
  /** 最近一次请求状态。 */
  requestStatus: SettingsRequestStatus;
};
