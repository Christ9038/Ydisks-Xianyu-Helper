-- +goose Up
ALTER TABLE cookies ADD COLUMN login_method TEXT NOT NULL DEFAULT '';
ALTER TABLE cookies ADD COLUMN last_login_at INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS account_login_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    owner_id INTEGER NOT NULL DEFAULT 0,
    account_pk INTEGER NOT NULL DEFAULT 0,
    account_identifier TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    method TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    trigger_reason TEXT NOT NULL DEFAULT '',
    failure_reason TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    updated_cookie_names TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_account_login_logs_cookie_created
    ON account_login_logs(cookie_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_login_logs_identifier_status_created
    ON account_login_logs(account_identifier, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_login_logs_owner_created
    ON account_login_logs(owner_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_account_login_logs_owner_created;
DROP INDEX IF EXISTS idx_account_login_logs_identifier_status_created;
DROP INDEX IF EXISTS idx_account_login_logs_cookie_created;
DROP TABLE IF EXISTS account_login_logs;
