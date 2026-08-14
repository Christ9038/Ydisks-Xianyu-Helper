import { expect, test } from 'vitest';
import type { AccountDetail } from '../../../types';
import {
  buildAccountLoginInfoUpdate,
  isCurrentAccountRequest,
  passwordLoginViewFromStatus,
  shouldUpdateAccountPause,
} from './state';

// account 是账号编辑状态测试使用的最小领域对象。
const account = (overrides: Partial<AccountDetail> = {}): AccountDetail => ({
  id: 'account-1',
  enabled: true,
  auto_confirm: false,
  username: 'old-user',
  show_browser: false,
  pause_duration: 60,
  ...overrides,
});

test('账号切换后只接受当前请求代次和账号的响应',
  // 旧账号响应即使更晚返回，也不能覆盖当前编辑账号。
  () => {
    expect(isCurrentAccountRequest(2, 2, 'account-2', 'account-2')).toBe(true);
    expect(isCurrentAccountRequest(1, 2, 'account-2', 'account-2')).toBe(false);
    expect(isCurrentAccountRequest(2, 2, 'account-1', 'account-2')).toBe(false);
  });

test('暂停时长未变化时不会重复启动已结束的暂停',
  // 相同的时长只保留当前状态，不自动重新提交暂停请求。
  () => {
    expect(shouldUpdateAccountPause(60, account({ paused: false }))).toBe(false);
    expect(shouldUpdateAccountPause(30, account({ paused: true }))).toBe(true);
    expect(shouldUpdateAccountPause(0, account({ paused: true }))).toBe(true);
  });

test('凭证编辑不提交空白密码，并支持明确清空密码',
  // 登录账号或浏览器开关变化时，空白密码不能覆盖已有凭证。
  () => {
    expect(buildAccountLoginInfoUpdate(account(), {
      username: 'new-user',
      login_password: '',
      show_browser: true,
      clear_password: false,
    })).toEqual({ username: 'new-user', show_browser: true });
    expect(buildAccountLoginInfoUpdate(account(), {
      username: 'old-user',
      login_password: '',
      show_browser: false,
      clear_password: true,
    })).toEqual({ username: 'old-user', show_browser: false, clear_password: true });
  });

test('密码登录风控和失败状态统一转换且不暴露验证链接',
  // 风控状态只展示二维码和提示，失败状态展示后端错误说明。
  () => {
    expect(passwordLoginViewFromStatus({
      status: 'verification_required',
      message: '需要人脸验证',
      verification_url: 'https://verification.example',
      qr_code_url: 'https://qr.example',
    })).toEqual({
      sessionId: '',
      status: 'verification_required',
      message: '需要人脸验证',
      qrCodeUrl: 'https://qr.example',
    });
    expect(passwordLoginViewFromStatus({ status: 'failed', error: '凭证失效' })).toMatchObject({
      status: 'failed',
      message: '凭证失效',
      qrCodeUrl: '',
    });
  });
