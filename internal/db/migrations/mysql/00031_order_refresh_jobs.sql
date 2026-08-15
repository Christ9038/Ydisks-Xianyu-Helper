-- +goose Up
CREATE TABLE IF NOT EXISTS order_refresh_jobs (
    id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    filter_status VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    result_json LONGTEXT NOT NULL,
    error_message TEXT NOT NULL,
    worker_token VARCHAR(128) NOT NULL DEFAULT '',
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_order_refresh_jobs_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_order_refresh_jobs_user (user_id, created_at, id),
    INDEX idx_order_refresh_jobs_lease (status, lease_expires_at)
);

-- +goose Down
DROP TABLE IF EXISTS order_refresh_jobs;
