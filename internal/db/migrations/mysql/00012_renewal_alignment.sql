-- +goose Up
ALTER TABLE cookie_refresh_schedules ADD COLUMN last_status VARCHAR(32);
ALTER TABLE cookie_refresh_schedules ADD COLUMN last_error_message TEXT;
ALTER TABLE cookie_refresh_schedules ADD COLUMN last_refresh_at BIGINT NOT NULL DEFAULT 0;

ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN batch_id VARCHAR(36);
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN updated_cookie_count INT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN next_expire_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_cookies_refresh_log ADD COLUMN error_message TEXT;

ALTER TABLE scheduled_login_renew_log ADD COLUMN batch_id VARCHAR(36);
ALTER TABLE scheduled_login_renew_log ADD COLUMN error_message TEXT;

ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN batch_id VARCHAR(36);
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN updated_cookie_count INT NOT NULL DEFAULT 0;
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN response_content TEXT;
ALTER TABLE scheduled_api_cookie_renew_log ADD COLUMN error_message TEXT;

-- +goose Down
SELECT 1;
