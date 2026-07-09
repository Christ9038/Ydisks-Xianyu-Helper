-- +goose Up
ALTER TABLE cookies ADD COLUMN metadata_json TEXT DEFAULT '';
ALTER TABLE cookies ADD COLUMN last_refresh_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE cookie_status ADD COLUMN disable_reason TEXT DEFAULT '';

CREATE TABLE IF NOT EXISTS cookie_refresh_schedules (
    cookie_id TEXT PRIMARY KEY REFERENCES cookies(id) ON DELETE CASCADE,
    expire_at BIGINT NOT NULL DEFAULT 0,
    disabled INTEGER NOT NULL DEFAULT 0,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scheduled_cookies_refresh_log (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    updated_cookie_names TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scheduled_login_renew_log (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    updated_cookie_names TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scheduled_api_cookie_renew_log (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    updated_cookie_names TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS scheduled_api_cookie_renew_log;
DROP TABLE IF EXISTS scheduled_login_renew_log;
DROP TABLE IF EXISTS scheduled_cookies_refresh_log;
DROP TABLE IF EXISTS cookie_refresh_schedules;
