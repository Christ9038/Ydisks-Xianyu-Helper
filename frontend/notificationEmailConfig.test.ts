import { describe, expect, test } from 'vitest';
import { buildEmailChannelConfig, enableCustomSMTP, normalizeEmailChannelConfig } from './notificationEmailConfig';

describe('email notification SMTP modes', () => {
  test('recognizes legacy channel overrides as custom SMTP', () => {
    expect(normalizeEmailChannelConfig({ to_email: 'to@example.com', smtp_server: 'legacy.example.com' }).use_custom_smtp).toBe(true);
  });

  test('inherit mode removes every channel-level SMTP override', () => {
    expect(buildEmailChannelConfig({
      to_email: ' to@example.com ',
      use_custom_smtp: false,
      smtp_server: 'stale.example.com',
      smtp_use_tls: false,
    })).toEqual({ to_email: 'to@example.com', use_custom_smtp: false });
  });

  test('custom mode starts from a complete copy of system SMTP settings', () => {
    const result = enableCustomSMTP({ to_email: 'to@example.com' }, {
      smtp_server: 'smtp.example.com',
      smtp_port: 465,
      smtp_user: 'from@example.com',
      smtp_password: 'secret',
      smtp_use_tls: false,
      smtp_use_ssl: true,
    });
    expect(result).toMatchObject({
      use_custom_smtp: true,
      smtp_server: 'smtp.example.com',
      smtp_port: 465,
      smtp_from_address: 'from@example.com',
      smtp_use_tls: false,
      smtp_use_ssl: true,
    });
  });
});
