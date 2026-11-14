-- +goose Up
ALTER TABLE ai_reply_settings ADD COLUMN order_context_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE agent_reply_settings ADD COLUMN order_context_enabled TINYINT(1) NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE agent_reply_settings DROP COLUMN order_context_enabled;
ALTER TABLE ai_reply_settings DROP COLUMN order_context_enabled;
