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

test('编辑子模块共享同一账号边界，旧绑定、AI 和密码登录响应全部失效',
  // 三类异步子模块都使用相同的代次与账号判定，防止旧弹窗响应污染新账号。
  () => {
    const currentGeneration = 4;
    const currentAccountId = 'account-4';
    expect(isCurrentAccountRequest(3, currentGeneration, 'account-3', currentAccountId)).toBe(false);
    expect(isCurrentAccountRequest(currentGeneration, currentGeneration, currentAccountId, currentAccountId)).toBe(true);
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

test('登录字段更新不会用空白密码覆盖已有凭证',
  // 账号或浏览器开关变化时只提交真正变化的非敏感字段。
  () => {
    expect(buildAccountLoginInfoUpdate(account({ username: 'old-user', show_browser: true }), {
      username: 'new-user',
      login_password: '',
      show_browser: false,
    })).toEqual({ username: 'new-user', show_browser: false });
  });

test('登录字段更新只在用户输入新密码时提交密码',
  // 用户输入新密码后才允许将密码字段写入设置补丁。
  () => {
    expect(buildAccountLoginInfoUpdate(account(), {
      username: 'old-user',
      login_password: 'new-secret',
      show_browser: false,
    })).toEqual({ username: 'old-user', login_password: 'new-secret', show_browser: false });
  });

test('登录字段没有变化时不生成更新补丁',
  // 没有任何登录字段变化时避免发起无意义的设置请求。
  () => {
    expect(buildAccountLoginInfoUpdate(account(), {
      username: 'old-user',
      login_password: '',
      show_browser: false,
    })).toBeNull();
  });

test('登录字段支持明确清空密码',
  // 清空密码必须由用户显式勾选，不能由空白输入隐式触发。
  () => {
    expect(buildAccountLoginInfoUpdate(account(), {
      username: 'old-user',
      login_password: '',
      show_browser: false,
      clear_password: true,
    })).toEqual({ username: 'old-user', show_browser: false, clear_password: true });
  });

test('明确清空密码优先于同时输入的新密码',
  // 同时存在清空和输入时遵循显式清空意图，避免写入新密码。
  () => {
    expect(buildAccountLoginInfoUpdate(account(), {
      username: 'old-user',
      login_password: 'new-secret',
      show_browser: false,
      clear_password: true,
    })).toEqual({ username: 'old-user', show_browser: false, clear_password: true });
  });

test('暂停时长相同不会重新启动账号暂停',
  // 暂停时长没有变化时保持当前状态，避免重复写入暂停记录。
  () => {
    expect(shouldUpdateAccountPause(60, { pause_duration: 60, paused: false })).toBe(false);
    expect(shouldUpdateAccountPause(60, { pause_duration: 60, paused: true })).toBe(false);
  });
