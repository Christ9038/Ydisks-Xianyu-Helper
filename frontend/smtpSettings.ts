export const normalizeSMTPSettings = (settings: Record<string, any>): Record<string, any> => {
  const legacyFrom = String(settings.smtp_from || '').trim();
  const legacyIsAddress = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(legacyFrom);
	return {
    ...settings,
    smtp_from_name: settings.smtp_from_name || (legacyIsAddress ? '' : legacyFrom),
		smtp_from_address: settings.smtp_from_address || (legacyIsAddress ? legacyFrom : settings.smtp_user || ''),
		smtp_use_tls: parseSettingBoolean(settings.smtp_use_tls, true),
		smtp_use_ssl: parseSettingBoolean(settings.smtp_use_ssl, false),
	};
};

const parseSettingBoolean = (value: unknown, fallback: boolean): boolean => {
	if (typeof value === 'boolean') return value;
	if (typeof value === 'string') {
		if (['true', '1', 'yes', 'on'].includes(value.toLowerCase())) return true;
		if (['false', '0', 'no', 'off'].includes(value.toLowerCase())) return false;
	}
	return fallback;
};
