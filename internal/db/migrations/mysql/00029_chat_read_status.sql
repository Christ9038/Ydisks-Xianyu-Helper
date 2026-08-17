-- +goose Up
ALTER TABLE chat_messages ADD COLUMN read_status INT NOT NULL DEFAULT 0;
ALTER TABLE chat_messages ADD COLUMN read_at BIGINT NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE chat_messages DROP COLUMN read_at;
ALTER TABLE chat_messages DROP COLUMN read_status;
