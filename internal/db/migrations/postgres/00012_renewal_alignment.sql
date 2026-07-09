-- +goose Up
ALTER TABLE cookie_refresh_schedules ADD COLUMN last_status TEXT DEFAULT '';
ALTER TABLE cookie_refresh_schedules ADD COLUMN last_error_message TEXT DEFAULT '';
ALTER TABLE cookie_refresh_schedules ADD COLUMN last_refresh_at BIGINT NOT NULL DEFAULT 0;

ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN batch_id TEXT DEFAULT '';
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN updated_cookie_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN next_expire_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN error_message TEXT DEFAULT '';

ALTER TABLE scheduled_login_renew_log ADD COLUMN batch_id TEXT DEFAULT '';
ALTER TABLE scheduled_login_renew_log ADD COLUMN error_message TEXT DEFAULT '';

ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN batch_id TEXT DEFAULT '';
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN updated_cookie_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN response_content TEXT DEFAULT '';
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN error_message TEXT DEFAULT '';

-- +goose Down
SELECT 1;
