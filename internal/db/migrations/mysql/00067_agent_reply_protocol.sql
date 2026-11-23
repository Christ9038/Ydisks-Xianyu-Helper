-- +goose Up
ALTER TABLE agent_reply_settings ADD COLUMN protocol VARCHAR(16) NOT NULL DEFAULT 'v1';
ALTER TABLE agent_reply_settings ADD COLUMN model VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE agent_reply_settings ADD COLUMN system_prompt VARCHAR(2000) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agent_reply_settings DROP COLUMN system_prompt;
ALTER TABLE agent_reply_settings DROP COLUMN model;
ALTER TABLE agent_reply_settings DROP COLUMN protocol;
