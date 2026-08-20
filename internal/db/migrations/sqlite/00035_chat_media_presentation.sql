-- +goose Up
ALTER TABLE chat_sessions ADD COLUMN item_image_url TEXT NOT NULL DEFAULT '';
ALTER TABLE chat_messages ADD COLUMN media_duration INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE chat_messages DROP COLUMN media_duration;
ALTER TABLE chat_sessions DROP COLUMN item_image_url;
