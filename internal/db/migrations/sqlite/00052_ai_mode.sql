-- +goose Up
ALTER TABLE ai_reply_settings ADD COLUMN ai_mode TEXT NOT NULL DEFAULT 'bargain_only' CHECK (ai_mode IN ('bargain_only', 'full_service'));

-- +goose Down
ALTER TABLE ai_reply_settings DROP COLUMN ai_mode;
