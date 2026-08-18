import { describe,expect,test } from 'vitest';
import { buildEmailChannelConfig,enableCustomSMTP,normalizeEmailChannelConfig } from './notificationEmailConfig';

describe('email notification SMTP modes', () => {
  test('recognizes legacy channel overrides as custom SMTP', () => {
    expect(normalizeEmailChannelConfig({ to_email: 'to@example.com', smtp_server: 'legacy.example.com' }).use_custom_smtp).toBe(true);
  } /* 测试回调断言旧 SMTP 覆盖字段会选择自定义模式。 */);

  test('inherit mode removes every channel-level SMTP override', () => {
    expect(buildEmailChannelConfig({
      to_email: ' to@example.com ',
      use_custom_smtp: false,
      smtp_server: 'stale.example.com',
      smtp_use_tls: false,
    })).toEqual({ to_email: 'to@example.com', use_custom_smtp: false });
  } /* 测试回调断言继承系统 SMTP 模式会清除渠道级覆盖。 */);

  test('custom mode starts from a complete copy of system SMTP settings', () => {
    const result = enableCustomSMTP({ to_email: 'to@example.com' }, {
      smtp_server: 'smtp.example.com',
      smtp_port: 465,
      smtp_user: 'from@example.com',
      smtp_password: 'secret',
      smtp_use_tls: false,
      smtp_use_ssl: true,
    }); /* result 表示处理结果。 */
    expect(result).toMatchObject({
      use_custom_smtp: true,
      smtp_server: 'smtp.example.com',
      smtp_port: 465,
      smtp_from_address: 'from@example.com',
      smtp_use_tls: false,
      smtp_use_ssl: true,
    });
  } /* 测试回调断言自定义模式复制完整的系统 SMTP 设置。 */);

  test('显式字符串开关和未知值遵循默认策略', () => {
    expect(normalizeEmailChannelConfig({ use_custom_smtp: 'true' }).use_custom_smtp).toBe(true);
    expect(normalizeEmailChannelConfig({ use_custom_smtp: 'off' }).use_custom_smtp).toBe(false);
    expect(normalizeEmailChannelConfig({ use_custom_smtp: 'unknown' }).use_custom_smtp).toBe(false);
  } /* 测试回调断言字符串开关和未知值的兼容归一化策略。 */);

  test('启用自定义 SMTP 时补齐默认端口和发件地址', () => {
    // result 是仅提供账号信息时补齐默认 SMTP 字段的配置。
    const result = enableCustomSMTP({ smtp_user: 'sender@example.com', smtp_use_tls: 'invalid', smtp_use_ssl: 'invalid' }, {});
    expect(result.smtp_port).toBe(587);
    expect(result.smtp_from_address).toBe('sender@example.com');
    expect(result.smtp_use_tls).toBe(true);
    expect(result.smtp_use_ssl).toBe(false);
  } /* 测试回调断言缺省 SMTP 端口和发件地址的补全规则。 */);

  test('构建自定义模式配置时保留完整 SMTP 覆盖字段', () => {
    // result 是自定义 SMTP 渠道的规范化配置。
    const result = buildEmailChannelConfig({ to_email: ' to@example.com ', use_custom_smtp: true, smtp_server: 'smtp.example.com', smtp_port: 465 });
    expect(result).toMatchObject({ to_email: 'to@example.com', use_custom_smtp: true, smtp_server: 'smtp.example.com', smtp_port: 465 });
  } /* 测试回调断言自定义 SMTP 配置保留所有覆盖字段。 */);
} /* 测试套件回调汇总邮件通知配置兼容契约。 */);
