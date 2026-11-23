-- +goose Up
ALTER TABLE agent_reply_settings ADD COLUMN protocol TEXT NOT NULL DEFAULT 'v1';
ALTER TABLE agent_reply_settings ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_reply_settings ADD COLUMN system_prompt TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agent_reply_settings DROP COLUMN system_prompt;
ALTER TABLE agent_reply_settings DROP COLUMN model;
ALTER TABLE agent_reply_settings DROP COLUMN protocol;
