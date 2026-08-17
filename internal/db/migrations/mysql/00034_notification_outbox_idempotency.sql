-- +goose Up
-- idempotency_key 让同一自动化运行终态对同一渠道只保留一条 outbox 记录。
-- NULL 保持历史普通通知可重复投递；uncertain 记录保留原键，从而禁止恢复时自动重新入队。
ALTER TABLE notification_outbox ADD COLUMN idempotency_key VARCHAR(255) NULL;
CREATE UNIQUE INDEX idx_notification_outbox_channel_idempotency
    ON notification_outbox(channel_id, idempotency_key);

-- +goose Down
DROP INDEX idx_notification_outbox_channel_idempotency ON notification_outbox;
ALTER TABLE notification_outbox DROP COLUMN idempotency_key;
