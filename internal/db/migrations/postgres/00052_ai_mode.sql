-- +goose Up
ALTER TABLE ai_reply_settings ADD COLUMN ai_mode VARCHAR(32) NOT NULL DEFAULT 'bargain_only';

-- +goose Down
ALTER TABLE ai_reply_settings DROP COLUMN ai_mode;
