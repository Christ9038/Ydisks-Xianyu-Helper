-- +goose Up
CREATE TABLE notification_outbox (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    channel_id BIGINT NOT NULL,
    event_type VARCHAR(64) NOT NULL DEFAULT '',
    body LONGTEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at BIGINT NOT NULL DEFAULT 0,
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    worker_token VARCHAR(128) NOT NULL DEFAULT '',
    last_error TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_notification_outbox_channel FOREIGN KEY (channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE
);
CREATE INDEX idx_notification_outbox_due ON notification_outbox(status, next_attempt_at, lease_expires_at);

-- +goose Down
DROP TABLE IF EXISTS notification_outbox;
