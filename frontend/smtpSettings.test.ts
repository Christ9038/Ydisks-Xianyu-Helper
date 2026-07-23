import { describe, expect, test } from 'vitest';
import { normalizeSMTPSettings } from './smtpSettings';

describe('normalizeSMTPSettings', () => {
  test('migrates a legacy sender email as the envelope address', () => {
    expect(normalizeSMTPSettings({ smtp_from: 'sender@example.com', smtp_user: 'login@example.com' })).toMatchObject({
      smtp_from_name: '',
      smtp_from_address: 'sender@example.com',
    });
  });

  test('migrates a legacy display name and falls back to the SMTP user address', () => {
    expect(normalizeSMTPSettings({ smtp_from: '闲鱼助手', smtp_user: 'login@example.com' })).toMatchObject({
      smtp_from_name: '闲鱼助手',
      smtp_from_address: 'login@example.com',
    });
  });

  test('preserves explicit split sender fields', () => {
    expect(normalizeSMTPSettings({
      smtp_from: 'legacy@example.com',
      smtp_from_name: '新名称',
      smtp_from_address: 'new@example.com',
    })).toMatchObject({
      smtp_from_name: '新名称',
      smtp_from_address: 'new@example.com',
    });
  });

	test('normalizes persisted SMTP transport strings', () => {
		expect(normalizeSMTPSettings({ smtp_use_tls: 'false', smtp_use_ssl: 'true' })).toMatchObject({
			smtp_use_tls: false,
			smtp_use_ssl: true,
		});
	});
});
