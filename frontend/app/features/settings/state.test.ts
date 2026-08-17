import { expect, test } from 'vitest';
import type { SystemSettings } from '../../../shared/api-contract';
import { buildPersistableSettings, createCredentials, createCredentialsMessage, isCurrentSettingsRequest, validateCredentials } from './state';

// settingsFixture 是覆盖敏感字段过滤和系统配置保留字段的最小草稿。
const settingsFixture: SystemSettings = {
  log_level: 'info',
  smtp_password: 'secret',
  ai_api_key: 'api-key',
  renewal_log_retention_days: 15,
};

test('Settings 保存草稿只提交允许的系统配置字段',
  // 配置裁剪测试验证 SMTP 等兼容字段不会被系统设置批量接口覆盖。
  () => {
    expect(buildPersistableSettings(settingsFixture)).toEqual({ log_level: 'info', ai_api_key: 'api-key', renewal_log_retention_days: 15 });
  });

test('登录凭据校验覆盖用户名、密码和确认密码边界',
  // 凭据校验测试验证所有前端可直接阻断的错误都在请求前返回。
  () => {
    expect(validateCredentials(createCredentials('ab'))).toContain('用户名');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: '' })).toContain('当前密码');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: 'old', new_password: 'short', confirm_password: 'short' })).toContain('8 个字符');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: 'old', new_password: 'long-password', confirm_password: 'different' })).toContain('不一致');
    expect(validateCredentials({ ...createCredentials('admin'), current_password: 'old' })).toBe('');
    expect(createCredentialsMessage('success', '已保存')).toEqual({ type: 'success', text: '已保存' });
  });

test('Settings 请求代次拒绝过期响应和取消响应',
  // 请求边界测试验证刷新或组件卸载后旧响应不会覆盖当前状态。
  () => {
    // controller 是模拟组件卸载取消的控制器。
    const controller = new AbortController();
    expect(isCurrentSettingsRequest(2, 2, controller.signal)).toBe(true);
    expect(isCurrentSettingsRequest(1, 2, controller.signal)).toBe(false);
    controller.abort();
    expect(isCurrentSettingsRequest(2, 2, controller.signal)).toBe(false);
  });
