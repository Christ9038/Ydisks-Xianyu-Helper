-- +goose Up
ALTER TABLE chat_sessions ADD COLUMN is_visible BOOLEAN NOT NULL DEFAULT TRUE;
CREATE INDEX idx_chat_sessions_account_visibility_recent ON chat_sessions(cookie_id, is_visible, last_message_at DESC, chat_id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_chat_sessions_account_visibility_recent;
ALTER TABLE chat_sessions DROP COLUMN IF EXISTS is_visible;
