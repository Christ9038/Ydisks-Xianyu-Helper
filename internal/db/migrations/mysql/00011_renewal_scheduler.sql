-- +goose Up
ALTER TABLE cookies ADD COLUMN metadata_json TEXT;
ALTER TABLE cookies ADD COLUMN last_refresh_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE cookie_status ADD COLUMN disable_reason TEXT;

CREATE TABLE IF NOT EXISTS cookie_refresh_schedules (
    cookie_id VARCHAR(191) PRIMARY KEY,
    expire_at BIGINT NOT NULL DEFAULT 0,
    disabled TINYINT(1) NOT NULL DEFAULT 0,
    consecutive_failures INT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_cookie_refresh_schedules_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS scheduled_cookies_refresh_log (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(191) NOT NULL,
    status VARCHAR(32) NOT NULL,
    message TEXT NOT NULL,
    updated_cookie_names TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scheduled_login_renew_log (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(191) NOT NULL,
    status VARCHAR(32) NOT NULL,
    message TEXT NOT NULL,
    updated_cookie_names TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scheduled_api_cookie_renew_log (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(191) NOT NULL,
    status VARCHAR(32) NOT NULL,
    message TEXT NOT NULL,
    updated_cookie_names TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS scheduled_api_cookie_renew_log;
DROP TABLE IF EXISTS scheduled_login_renew_log;
DROP TABLE IF EXISTS scheduled_cookies_refresh_log;
DROP TABLE IF EXISTS cookie_refresh_schedules;
