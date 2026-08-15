-- +goose Up
-- uncertain_at 记录外部发送成功但本地确认失败的隔离时间，避免租约过期后自动重复发送。
ALTER TABLE notification_outbox ADD COLUMN uncertain_at BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE notification_outbox DROP COLUMN uncertain_at;
