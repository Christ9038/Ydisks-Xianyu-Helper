-- +goose Up
ALTER TABLE chat_sessions ADD COLUMN is_visible TINYINT(1) NOT NULL DEFAULT 1;
CREATE INDEX idx_chat_sessions_account_visibility_recent ON chat_sessions(cookie_id, is_visible, last_message_at DESC, chat_id DESC);

-- +goose Down
DROP INDEX idx_chat_sessions_account_visibility_recent ON chat_sessions;
ALTER TABLE chat_sessions DROP COLUMN is_visible;
