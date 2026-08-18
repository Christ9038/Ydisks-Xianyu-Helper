import { describe, expect, test } from 'vitest';
import { normalizeSMTPSettings } from './smtpSettings';

describe('normalizeSMTPSettings', () => {
  test('migrates a legacy sender email as the envelope address', () => {
    expect(normalizeSMTPSettings({ smtp_from: 'sender@example.com', smtp_user: 'login@example.com' })).toMatchObject({
      smtp_from_name: '',
      smtp_from_address: 'sender@example.com',
    });
  } /* 测试回调断言旧发件人字段迁移为 envelope 地址。 */);

  test('migrates a legacy display name and falls back to the SMTP user address', () => {
    expect(normalizeSMTPSettings({ smtp_from: '闲鱼助手', smtp_user: 'login@example.com' })).toMatchObject({
      smtp_from_name: '闲鱼助手',
      smtp_from_address: 'login@example.com',
    });
  } /* 测试回调断言旧显示名和 SMTP 用户地址的回退规则。 */);

  test('preserves explicit split sender fields', () => {
    expect(normalizeSMTPSettings({
      smtp_from: 'legacy@example.com',
      smtp_from_name: '新名称',
      smtp_from_address: 'new@example.com',
    })).toMatchObject({
      smtp_from_name: '新名称',
      smtp_from_address: 'new@example.com',
    });
  } /* 测试回调断言显式拆分后的发件字段优先保留。 */);

	test('normalizes persisted SMTP transport strings', () => {
		expect(normalizeSMTPSettings({ smtp_use_tls: 'false', smtp_use_ssl: 'true' })).toMatchObject({
			smtp_use_tls: false,
			smtp_use_ssl: true,
		});
	} /* 测试回调断言持久化 SMTP 字符串会归一化为布尔值。 */);

  test('保留布尔值并对未知字符串使用默认值', () => {
    expect(normalizeSMTPSettings({ smtp_use_tls: true, smtp_use_ssl: false })).toMatchObject({ smtp_use_tls: true, smtp_use_ssl: false });
    expect(normalizeSMTPSettings({ smtp_use_tls: 'unknown', smtp_use_ssl: 'unknown' })).toMatchObject({ smtp_use_tls: true, smtp_use_ssl: false });
  } /* 测试回调断言布尔值和未知字符串的默认策略。 */);
} /* 测试套件回调汇总 SMTP 配置迁移契约。 */);
