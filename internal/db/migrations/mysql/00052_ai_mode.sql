-- +goose Up
ALTER TABLE ai_reply_settings ADD COLUMN ai_mode VARCHAR(32) NOT NULL DEFAULT 'bargain_only',
    ADD CONSTRAINT chk_ai_reply_settings_mode CHECK (ai_mode IN ('bargain_only', 'full_service'));

-- +goose Down
ALTER TABLE ai_reply_settings DROP CHECK chk_ai_reply_settings_mode, DROP COLUMN ai_mode;
