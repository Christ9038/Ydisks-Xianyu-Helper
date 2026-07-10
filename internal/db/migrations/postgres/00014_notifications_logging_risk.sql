-- +goose Up
ALTER TABLE notification_channels ADD COLUMN IF NOT EXISTS event_types TEXT NOT NULL DEFAULT '';
ALTER TABLE message_notifications ADD COLUMN IF NOT EXISTS event_types TEXT NOT NULL DEFAULT '';

ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN IF NOT EXISTS step_details TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN IF NOT EXISTS renew_method TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN IF NOT EXISTS duration_ms BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN IF NOT EXISTS request_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN IF NOT EXISTS response_content TEXT NOT NULL DEFAULT '';

ALTER TABLE scheduled_login_renew_log ADD COLUMN IF NOT EXISTS step_details TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_login_renew_log ADD COLUMN IF NOT EXISTS renew_method TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_login_renew_log ADD COLUMN IF NOT EXISTS duration_ms BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_login_renew_log ADD COLUMN IF NOT EXISTS request_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_login_renew_log ADD COLUMN IF NOT EXISTS response_content TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_login_renew_log ADD COLUMN IF NOT EXISTS updated_cookie_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN IF NOT EXISTS step_details TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN IF NOT EXISTS renew_method TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN IF NOT EXISTS duration_ms BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN IF NOT EXISTS request_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS risk_control_logs (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL DEFAULT 'slider_captcha',
    event_description TEXT NOT NULL DEFAULT '',
    processing_result TEXT NOT NULL DEFAULT '',
    processing_status TEXT NOT NULL DEFAULT 'processing',
    captcha_engine TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO system_settings (key, value, description) VALUES
    ('log_level', 'info', '日志输出等级：debug/info/warn/error'),
    ('log_format', 'text', '日志输出格式：text/json'),
    ('renewal_log_retention_days', '10', '续期日志保留天数')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS risk_control_logs;
DELETE FROM system_settings WHERE key IN ('log_level', 'log_format', 'renewal_log_retention_days');

ALTER TABLE notification_channels DROP COLUMN IF EXISTS event_types;
ALTER TABLE message_notifications DROP COLUMN IF EXISTS event_types;
ALTER TABLE scheduled_cookies_refresh_log DROP COLUMN IF EXISTS step_details;
ALTER TABLE scheduled_cookies_refresh_log DROP COLUMN IF EXISTS renew_method;
ALTER TABLE scheduled_cookies_refresh_log DROP COLUMN IF EXISTS duration_ms;
ALTER TABLE scheduled_cookies_refresh_log DROP COLUMN IF EXISTS request_count;
ALTER TABLE scheduled_cookies_refresh_log DROP COLUMN IF EXISTS response_content;
ALTER TABLE scheduled_login_renew_log DROP COLUMN IF EXISTS step_details;
ALTER TABLE scheduled_login_renew_log DROP COLUMN IF EXISTS renew_method;
ALTER TABLE scheduled_login_renew_log DROP COLUMN IF EXISTS duration_ms;
ALTER TABLE scheduled_login_renew_log DROP COLUMN IF EXISTS request_count;
ALTER TABLE scheduled_login_renew_log DROP COLUMN IF EXISTS response_content;
ALTER TABLE scheduled_login_renew_log DROP COLUMN IF EXISTS updated_cookie_count;
ALTER TABLE scheduled_api_cookie_renew_log DROP COLUMN IF EXISTS step_details;
ALTER TABLE scheduled_api_cookie_renew_log DROP COLUMN IF EXISTS renew_method;
ALTER TABLE scheduled_api_cookie_renew_log DROP COLUMN IF EXISTS duration_ms;
ALTER TABLE scheduled_api_cookie_renew_log DROP COLUMN IF EXISTS request_count;
