import { AccountDetail } from '../types';

export interface AccountLoginEditForm {
  username: string;
  login_password: string;
  show_browser: boolean;
}

export interface AccountLoginInfoPayload {
  username: string;
  login_password?: string;
  show_browser: boolean;
}

export const buildAccountLoginInfoUpdate = (
  account: AccountDetail,
  form: AccountLoginEditForm,
): AccountLoginInfoPayload | null => {
  const usernameChanged = form.username !== (account.username || '');
  const showBrowserChanged = form.show_browser !== (account.show_browser || false);
  const passwordChanged = form.login_password !== '';
  if (!usernameChanged && !showBrowserChanged && !passwordChanged) {
    return null;
  }

  const payload: AccountLoginInfoPayload = {
    username: form.username,
    show_browser: form.show_browser,
  };
  if (passwordChanged) {
    payload.login_password = form.login_password;
  }
  return payload;
};
