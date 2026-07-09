-- +goose Up
ALTER TABLE cookies ADD COLUMN login_method VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE cookies ADD COLUMN last_login_at BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS account_login_logs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(255) NOT NULL,
    user_id BIGINT NOT NULL DEFAULT 0,
    owner_id BIGINT NOT NULL DEFAULT 0,
    account_pk BIGINT NOT NULL DEFAULT 0,
    account_identifier VARCHAR(80) NOT NULL DEFAULT '',
    username VARCHAR(255) NOT NULL DEFAULT '',
    method VARCHAR(32) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    message TEXT NOT NULL,
    trigger_reason VARCHAR(128) NOT NULL DEFAULT '',
    failure_reason VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL,
    updated_cookie_names VARCHAR(500) NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);

CREATE INDEX idx_account_login_logs_cookie_created
    ON account_login_logs(cookie_id, created_at DESC);
CREATE INDEX idx_account_login_logs_identifier_status_created
    ON account_login_logs(account_identifier, status, created_at DESC);
CREATE INDEX idx_account_login_logs_owner_created
    ON account_login_logs(owner_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS account_login_logs;
ALTER TABLE cookies DROP COLUMN last_login_at;
ALTER TABLE cookies DROP COLUMN login_method;
