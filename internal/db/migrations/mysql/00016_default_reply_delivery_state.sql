-- +goose Up
ALTER TABLE default_reply_records
    ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'sent',
    ADD COLUMN text_sent INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN image_sent INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN last_error TEXT NULL,
    ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- +goose Down
ALTER TABLE default_reply_records
    DROP COLUMN status,
    DROP COLUMN text_sent,
    DROP COLUMN image_sent,
    DROP COLUMN last_error,
    DROP COLUMN updated_at;
