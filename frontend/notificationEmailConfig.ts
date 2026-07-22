const smtpOverrideKeys = [
  'smtp_server',
  'smtp_port',
  'smtp_user',
  'smtp_password',
  'smtp_from_name',
  'smtp_from_address',
  'smtp_use_tls',
  'smtp_use_ssl',
] as const;

const parseBoolean = (value: unknown, fallback: boolean): boolean => {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'string') {
    if (['true', '1', 'yes', 'on'].includes(value.toLowerCase())) return true;
    if (['false', '0', 'no', 'off'].includes(value.toLowerCase())) return false;
  }
  return fallback;
};

export const normalizeEmailChannelConfig = (config: Record<string, unknown>): Record<string, unknown> => {
  const hasExplicitMode = Object.prototype.hasOwnProperty.call(config, 'use_custom_smtp');
  const hasLegacyOverrides = smtpOverrideKeys.some(key => String(config[key] ?? '').trim() !== '');
  return {
    ...config,
    use_custom_smtp: hasExplicitMode
      ? parseBoolean(config.use_custom_smtp, false)
      : hasLegacyOverrides,
  };
};

export const enableCustomSMTP = (
  config: Record<string, unknown>,
  systemSettings: Record<string, unknown>,
): Record<string, unknown> => {
  const next: Record<string, unknown> = { ...config, use_custom_smtp: true };
  for (const key of smtpOverrideKeys) {
    if (String(next[key] ?? '').trim() === '' && systemSettings[key] !== undefined) {
      next[key] = systemSettings[key];
    }
  }
  next.smtp_port ||= 587;
  next.smtp_from_address ||= next.smtp_user || '';
  next.smtp_use_tls = parseBoolean(next.smtp_use_tls, true);
  next.smtp_use_ssl = parseBoolean(next.smtp_use_ssl, false);
  return next;
};

export const buildEmailChannelConfig = (config: Record<string, unknown>): Record<string, unknown> => {
  const normalized = normalizeEmailChannelConfig(config);
  const result: Record<string, unknown> = {
    to_email: String(normalized.to_email ?? '').trim(),
    use_custom_smtp: normalized.use_custom_smtp === true,
  };
  if (result.use_custom_smtp) {
    for (const key of smtpOverrideKeys) result[key] = normalized[key];
  }
  return result;
};
