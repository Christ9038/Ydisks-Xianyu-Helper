export const normalizeSMTPSettings = (settings: Record<string, any>): Record<string, any> => {
  const legacyFrom = String(settings.smtp_from || '').trim();
  const legacyIsAddress = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(legacyFrom);
  return {
    ...settings,
    smtp_from_name: settings.smtp_from_name || (legacyIsAddress ? '' : legacyFrom),
    smtp_from_address: settings.smtp_from_address || (legacyIsAddress ? legacyFrom : settings.smtp_user || ''),
  };
};
