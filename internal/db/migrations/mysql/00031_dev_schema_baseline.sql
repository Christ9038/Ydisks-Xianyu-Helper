-- +goose Up
CREATE TABLE IF NOT EXISTS order_reconciliations (
    id VARCHAR(36) PRIMARY KEY,
    order_id VARCHAR(255) NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    kind VARCHAR(100) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_order_reconciliations_pending (status, created_at, id)
);

CREATE INDEX idx_orders_cursor ON orders(cookie_id, deleted_at, created_at DESC, order_id DESC);

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

CREATE TABLE IF NOT EXISTS security_audit_logs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    action VARCHAR(128) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    keys_json TEXT NOT NULL,
    outcome VARCHAR(32) NOT NULL DEFAULT 'accepted',
    created_at BIGINT NOT NULL,
    INDEX idx_security_audit_logs_user_created (user_id, created_at, id)
);

ALTER TABLE notification_outbox ADD COLUMN uncertain_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE notification_outbox ADD COLUMN idempotency_key VARCHAR(255) NULL;
CREATE UNIQUE INDEX idx_notification_outbox_channel_idempotency
    ON notification_outbox(channel_id, idempotency_key);

-- +goose Down
-- InnoDB 会让 00021 的 channel_id 外键复用新增的复合索引；先移除外键，
-- 否则删除幂等索引会失败。最后按 00021 的约束名称恢复原始关系。
ALTER TABLE notification_outbox DROP FOREIGN KEY fk_notification_outbox_channel;
DROP INDEX idx_notification_outbox_channel_idempotency ON notification_outbox;
ALTER TABLE notification_outbox DROP COLUMN idempotency_key;
ALTER TABLE notification_outbox DROP COLUMN uncertain_at;
ALTER TABLE notification_outbox
    ADD CONSTRAINT fk_notification_outbox_channel
    FOREIGN KEY (channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE;
DROP TABLE IF EXISTS security_audit_logs;
DROP TABLE IF EXISTS order_refresh_jobs;
DROP INDEX idx_orders_cursor ON orders;
DROP TABLE IF EXISTS order_reconciliations;
