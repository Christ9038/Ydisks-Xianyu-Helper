-- +goose Up
CREATE TABLE IF NOT EXISTS order_refresh_jobs (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cookie_id TEXT NOT NULL DEFAULT '',
    filter_status TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    result_json TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    worker_token TEXT NOT NULL DEFAULT '',
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_refresh_jobs_user ON order_refresh_jobs(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_order_refresh_jobs_lease ON order_refresh_jobs(status, lease_expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_order_refresh_jobs_lease;
DROP INDEX IF EXISTS idx_order_refresh_jobs_user;
DROP TABLE IF EXISTS order_refresh_jobs;
