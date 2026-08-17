-- +goose Up
CREATE TABLE IF NOT EXISTS order_reconciliations (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL,
    cookie_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_reconciliations_pending
    ON order_reconciliations(status, created_at, id);

CREATE INDEX IF NOT EXISTS idx_orders_cursor ON orders(cookie_id, deleted_at, created_at DESC, order_id DESC);

CREATE TABLE IF NOT EXISTS order_refresh_jobs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cookie_id TEXT NOT NULL DEFAULT '',
    filter_status TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    result_json TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    worker_token TEXT NOT NULL DEFAULT '',
    lease_expires_at INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_refresh_jobs_user ON order_refresh_jobs(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_order_refresh_jobs_lease ON order_refresh_jobs(status, lease_expires_at);

CREATE TABLE IF NOT EXISTS security_audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    keys_json TEXT NOT NULL DEFAULT '[]',
    outcome TEXT NOT NULL DEFAULT 'accepted',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_security_audit_logs_user_created
    ON security_audit_logs(user_id, created_at DESC, id DESC);

ALTER TABLE notification_outbox ADD COLUMN uncertain_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_outbox ADD COLUMN idempotency_key TEXT NULL;
CREATE UNIQUE INDEX idx_notification_outbox_channel_idempotency
    ON notification_outbox(channel_id, idempotency_key);

-- +goose Down
DROP INDEX IF EXISTS idx_notification_outbox_channel_idempotency;
ALTER TABLE notification_outbox DROP COLUMN idempotency_key;
ALTER TABLE notification_outbox DROP COLUMN uncertain_at;
DROP TABLE IF EXISTS security_audit_logs;
DROP INDEX IF EXISTS idx_order_refresh_jobs_lease;
DROP INDEX IF EXISTS idx_order_refresh_jobs_user;
DROP TABLE IF EXISTS order_refresh_jobs;
DROP INDEX IF EXISTS idx_orders_cursor;
DROP TABLE IF EXISTS order_reconciliations;
