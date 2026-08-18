import type { AccountDetail } from './api';
import type { PasswordLoginStatusResponse } from './api';
import type { AccountEditForm,PasswordLoginView } from './types';

// AccountLoginEditFields 描述账号编辑表单中参与登录信息比较的字段。
export type AccountLoginEditFields = Pick<AccountEditForm, 'username' | 'login_password' | 'show_browser'> & Pick<Partial<AccountEditForm>, 'clear_password'>;

// AccountLoginInfoPayload 描述需要提交给后端的登录信息变更。
export interface AccountLoginInfoPayload {
  // username 是新的闲鱼登录账号。
  username: string;
  // login_password 是用户本次输入的新密码。
  login_password?: string;
  // clear_password 表示清空服务端已保存密码。
  clear_password?: boolean;
  // show_browser 表示密码登录时是否展示浏览器。
  show_browser: boolean;
}

// shouldUpdateAccountPause 判断暂停时长是否真的发生变化，避免无意重启已结束的暂停。
export const shouldUpdateAccountPause = (
  requestedMinutes: number,
  account: Pick<AccountDetail, 'pause_duration' | 'paused'>,
): boolean => requestedMinutes !== (account.pause_duration || 0);

// buildAccountLoginInfoUpdate 只生成真正变化的登录字段，并拒绝提交空白密码。
export const buildAccountLoginInfoUpdate = (
  account: AccountDetail,
  form: AccountLoginEditFields,
): AccountLoginInfoPayload | null => {
  // usernameChanged 表示登录账号是否被修改。
  const usernameChanged = form.username !== (account.username || '');
  // showBrowserChanged 表示登录浏览器展示开关是否被修改。
  const showBrowserChanged = form.show_browser !== (account.show_browser || false);
  // passwordChanged 表示用户是否输入了新的非空密码。
  const passwordChanged = form.login_password !== '';
  // passwordCleared 表示用户是否明确要求清空密码。
  const passwordCleared = form.clear_password === true;
  if (!usernameChanged && !showBrowserChanged && !passwordChanged && !passwordCleared) return null;

  // payload 是提交给账号设置接口的登录信息补丁。
  const payload: AccountLoginInfoPayload = {
    username: form.username,
    show_browser: form.show_browser,
  };
  if (passwordCleared) {
    payload.clear_password = true;
  } else if (passwordChanged) {
    payload.login_password = form.login_password;
  }
  return payload;
};

// isCurrentAccountRequest 判断账号切换后异步响应是否仍属于当前请求。
export const isCurrentAccountRequest = (
  requestGeneration: number,
  currentGeneration: number,
  requestAccountId: string,
  currentAccountId: string,
): boolean => requestGeneration === currentGeneration && requestAccountId === currentAccountId;

// passwordLoginViewFromStatus 将密码登录接口状态转换为编辑弹窗状态。
export const passwordLoginViewFromStatus = (result: PasswordLoginStatusResponse): PasswordLoginView => {
  if (result.status === 'success') {
    return {
      sessionId: '',
      status: 'success',
      message: result.message || '账号密码登录成功，授权信息已更新',
      qrCodeUrl: '',
    };
  }
  if (result.status === 'processing' || result.status === 'verification_required') {
    return {
      sessionId: '',
      status: result.status,
      message: result.message || (result.status === 'verification_required' ? '账号触发风控，需要完成人脸识别' : '登录处理中'),
      qrCodeUrl: result.qr_code_url || '',
    };
  }
  return {
    sessionId: '',
    status: 'failed',
    message: result.error || result.message || '密码登录失败，请检查账号信息后重试',
    qrCodeUrl: '',
  };
};
